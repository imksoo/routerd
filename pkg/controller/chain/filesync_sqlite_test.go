// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
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
// Closing the SQLite writer after the WAL copy reproduces a disappearing SHM
// without changing host networking. It verifies this lifecycle, not atomic
// replication of a database under arbitrary concurrent writes.
func TestLeaseSyncSQLiteClosesAfterWALTransfer(t *testing.T) {
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
		if err == nil && strings.HasSuffix(source, "-wal") {
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
	if len(transferred) != 3 || strings.Contains(strings.Join(transferred, ","), "-shm") {
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
	if _, err := os.Stat(receiverPath + "-shm"); err != nil {
		t.Fatalf("SQLite did not regenerate its receiver-local index: %v", err)
	}
}

// Missing WAL is a real data-transfer failure, not an ignorable index race.
func TestLeaseSyncDoesNotIgnoreVanishedWAL(t *testing.T) {
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync unavailable: local transfer observation not acquired")
	}
	dir := t.TempDir()
	wal := filepath.Join(dir, "dhcp-sticky.db-wal")
	if err := os.WriteFile(wal, []byte("transfer fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	job := fileSyncJob{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync", Name: "replica", Sources: []fileSyncSource{{Name: "stickyLogWAL", Path: wal}}, Targets: []fileSyncTarget{{Host: "unused.invalid"}}}
	controller := FileSyncController{Store: mapStore{}, Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if err := os.Remove(wal); err != nil {
			t.Fatal(err)
		}
		return exec.CommandContext(ctx, name, "-a", args[len(args)-2], filepath.Join(dir, "received-wal")).CombinedOutput()
	}}
	if err := controller.reconcileJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	status := controller.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name)
	if status["phase"] != "Error" || status["reason"] != "SyncFailed" || status["exitCode"] != 23 || status["source"] != wal {
		t.Fatalf("WAL failure must remain observable: %#v", status)
	}
}
