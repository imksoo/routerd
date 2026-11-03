// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
)

// This fixture exercises the generated source list and a real local rsync.
// Closing the writer during snapshot transfer must leave a readable replica,
// independent of the live database WAL/SHM lifetime.
func TestLeaseSyncSQLiteWriterClosesDuringSnapshotTransfer(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync unavailable: local transfer observation not acquired")
	}
	sourceDir, receiverDir := t.TempDir(), t.TempDir()
	lease := filepath.Join(sourceDir, "dnsmasq.leases")
	if err := os.WriteFile(lease, []byte("1 aa:bb:cc:dd:ee:ff 192.0.2.20 client *\n"), 0600); err != nil {
		t.Fatal(err)
	}
	controller := FileSyncController{Store: mapStore{}, Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{LeaseFile: lease, StickyHoldDays: 3}},
	}}}}
	job, err := controller.fileSyncJobFromDHCPv4ServerLeaseSync(api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "replica"},
		Spec: api.DHCPv4ServerLeaseSyncSpec{Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"}, Targets: []api.LeaseSyncTargetSpec{{Host: "unused.invalid"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range job.Sources {
		job.Sources[i].Path = filepath.Join(sourceDir, filepath.Base(job.Sources[i].Path))
	}
	dbPath := filepath.Join(sourceDir, "dhcp-sticky.db")
	writer, err := logstore.OpenDHCPStickyLog(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close() })
	observed := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	if err := writer.RecordLeaseEvent(context.Background(), "added", "aa:bb:cc:dd:ee:ff", "192.0.2.20", "client", 3, observed); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if info, err := os.Stat(dbPath + suffix); err != nil || info.Size() == 0 {
			t.Fatalf("fixture must have populated %s: %v", suffix, err)
		}
	}
	var transferred []string
	controller.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		source := args[len(args)-2]
		transferred = append(transferred, filepath.Base(source))
		out, err := exec.CommandContext(ctx, name, "-a", "--delay-updates", source, filepath.Join(receiverDir, filepath.Base(source))).CombinedOutput()
		if err == nil && filepath.Base(source) == "dhcp-sticky.db" {
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dbPath + "-shm"); !os.IsNotExist(err) {
				t.Fatalf("fixture SHM did not disappear on last writer close: %v", err)
			}
		}
		return out, err
	}
	if err := controller.reconcileJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	status := controller.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name)
	if status["phase"] != "Synced" {
		t.Fatalf("transfer must succeed after transient SHM disappears: %#v", status)
	}
	if len(transferred) != 2 || strings.Contains(strings.Join(transferred, ","), "-shm") {
		t.Fatalf("unexpected transferred files: %v", transferred)
	}
	receiverPath := filepath.Join(receiverDir, "dhcp-sticky.db")
	if _, err := os.Stat(receiverPath + "-shm"); !os.IsNotExist(err) {
		t.Fatalf("receiver SHM was transferred: %v", err)
	}
	receiver, err := sql.Open("sqlite", "file:"+receiverPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	var hostname string
	var lastRequest int64
	if err := receiver.QueryRow("SELECT hostname,last_request_at FROM dhcp_sticky_leases WHERE mac=? AND ip=?", "aa:bb:cc:dd:ee:ff", "192.0.2.20").Scan(&hostname, &lastRequest); err != nil {
		t.Fatal(err)
	}
	if hostname != "client" || lastRequest != observed.UnixNano() {
		t.Fatalf("committed lease data not retained: %s %d", hostname, lastRequest)
	}
	var integrity string
	if err := receiver.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("receiver database invalid: %q %v", integrity, err)
	}
	if _, err := os.Stat(receiverPath + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("standalone snapshot unexpectedly requires receiver WAL: %v", err)
	}
}

// Snapshot files are private and temporary, including on transfer failure.
func TestLeaseSyncSnapshotLifetimeAndDestination(t *testing.T) {
	for _, failTransfer := range []bool{false, true} {
		t.Run(fmt.Sprint(failTransfer), func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "dhcp-sticky.db")
			writer, err := logstore.OpenDHCPStickyLog(source)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			if err := writer.RecordLeaseEvent(t.Context(), "added", "aa:bb:cc:dd:ee:ff", "192.0.2.20", "client", 3, time.Now()); err != nil {
				t.Fatal(err)
			}
			job := fileSyncJob{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync", Name: "replica", Sources: []fileSyncSource{{Name: "stickyLog", Path: source, SQLiteSnapshot: true}}, Targets: []fileSyncTarget{{Host: "unused.invalid"}, {Host: "second.invalid"}}}
			var snapshots []string
			controller := FileSyncController{Store: mapStore{}, Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				snapshot := args[len(args)-2]
				snapshots = append(snapshots, snapshot)
				if snapshot == source || !strings.HasSuffix(args[len(args)-1], ":"+source) {
					t.Fatalf("wrong source/destination: %v", args)
				}
				info, err := os.Stat(snapshot)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("private snapshot: %v %v", info, err)
				}
				dir, err := os.Stat(filepath.Dir(snapshot))
				if err != nil || dir.Mode().Perm() != 0700 {
					t.Fatalf("private directory: %v %v", dir, err)
				}
				if failTransfer {
					return nil, errors.New("fixture transfer failed")
				}
				return nil, nil
			}}
			if err := controller.reconcileJob(t.Context(), job); err != nil {
				t.Fatal(err)
			}
			wantPhase := "Synced"
			wantCalls := 2
			if failTransfer {
				wantPhase = "Error"
				wantCalls = 1
			}
			if len(snapshots) != wantCalls || controller.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name)["phase"] != wantPhase {
				t.Fatalf("calls=%v status=%v", snapshots, controller.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name))
			}
			for _, snapshot := range snapshots {
				if _, err := os.Stat(filepath.Dir(snapshot)); !os.IsNotExist(err) {
					t.Fatalf("snapshot directory retained: %s %v", snapshot, err)
				}
			}
			if !failTransfer && snapshots[0] != snapshots[1] {
				t.Fatal("targets received different snapshots")
			}
		})
	}
}

func TestLeaseSyncSnapshotFailureDoesNotTransferOrAcknowledge(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint(canceled), func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "dhcp-sticky.db")
			if err := os.WriteFile(source, []byte("not a database"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if canceled {
				cancel()
			}
			job := fileSyncJob{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync", Name: "replica", Sources: []fileSyncSource{{Name: "stickyLog", Path: source, SQLiteSnapshot: true}}, Targets: []fileSyncTarget{{Host: "unused.invalid"}}}
			controller := FileSyncController{Store: mapStore{}, Command: func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("transferred an incomplete snapshot")
				return nil, nil
			}}
			err := controller.reconcileJob(ctx, job)
			if canceled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation not returned: %v", err)
				}
				if status := controller.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name); len(status) != 0 {
					t.Fatalf("canceled generation published snapshot failure: %v", status)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			status := controller.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name)
			if status["phase"] != "Error" || status["reason"] != "SnapshotFailed" || status["source"] != source || status["syncedAt"] != nil {
				t.Fatalf("snapshot error hidden: %v", status)
			}
		})
	}
}
