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

// Keep generated transfer options; replace only the remote transport and destination.
func localLeaseSyncCommand(t *testing.T, receiverDir string) fileSyncCommandFunc {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync unavailable: local transfer observation not acquired")
	}
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		var local []string
		for i := 0; i < len(args)-2; i++ {
			if args[i] == "-e" {
				i++
				continue
			}
			if strings.HasPrefix(args[i], "--rsync-path=") {
				continue
			}
			local = append(local, args[i])
		}
		source := args[len(args)-2]
		local = append(local, source, filepath.Join(receiverDir, filepath.Base(source)))
		return exec.CommandContext(ctx, name, local...).CombinedOutput()
	}
}

// A successful sync must copy changed contents even if size and mtime are equal.
func TestLeaseSyncTransfersEqualSizeAndMtimeContent(t *testing.T) {
	for _, name := range []string{"dnsmasq.leases", "dhcp-sticky.db", "dhcp-sticky.db-wal", "pd-lease.json"} {
		t.Run(name, func(t *testing.T) {
			srcDir, dstDir := t.TempDir(), t.TempDir()
			src, dst := filepath.Join(srcDir, name), filepath.Join(dstDir, name)
			stamp := time.Unix(1700000000, 123456789)
			for file, data := range map[string]string{src: "new-data", dst: "old-data"} {
				if err := os.WriteFile(file, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(file, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			job := fileSyncJob{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync", Name: "replica", Sources: []fileSyncSource{{Name: name, Path: src, Required: true}}, Targets: []fileSyncTarget{{Host: "unused.invalid"}}}
			c := FileSyncController{Store: mapStore{}, Command: localLeaseSyncCommand(t, dstDir)}
			if err := c.reconcileJob(context.Background(), job); err != nil {
				t.Fatal(err)
			}
			if status := c.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name); status["phase"] != "Synced" {
				t.Fatalf("sync failed: %#v", status)
			}
			got, err := os.ReadFile(dst)
			if err != nil || string(got) != "new-data" {
				t.Fatalf("successful sync retained stale content: %q, %v", got, err)
			}
		})
	}
}

// A checkpoint can truncate (rather than remove) WAL after the DB transfer.
// Synced must still include leases committed before synchronization began.
func TestLeaseSyncCheckpointCannotAcknowledgeMissingCommittedLease(t *testing.T) {
	sourceDir, receiverDir := t.TempDir(), t.TempDir()
	leasePath := filepath.Join(sourceDir, "dnsmasq.leases")
	if err := os.WriteFile(leasePath, []byte("1 aa:bb:cc:dd:ee:ff 192.0.2.20 client *\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sourceDB := filepath.Join(sourceDir, "dhcp-sticky.db")
	writer, err := logstore.OpenDHCPStickyLog(sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	checkpoint, err := sql.Open("sqlite", sourceDB)
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoint.Close()
	if _, err := checkpoint.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	observed := time.Now().UTC()
	if err := writer.RecordLeaseEvent(t.Context(), "added", "aa:bb:cc:dd:ee:ff", "192.0.2.20", "acknowledged-client", 3, observed); err != nil {
		t.Fatal(err)
	}
	controller := FileSyncController{Store: mapStore{}, Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{LeaseFile: leasePath, StickyHoldDays: 3}},
	}}}}
	job, err := controller.fileSyncJobFromDHCPv4ServerLeaseSync(api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "replica"}, Spec: api.DHCPv4ServerLeaseSyncSpec{Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"}, Targets: []api.LeaseSyncTargetSpec{{Host: "unused.invalid"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range job.Sources {
		job.Sources[i].Path = filepath.Join(sourceDir, filepath.Base(job.Sources[i].Path))
	}
	transfer := localLeaseSyncCommand(t, receiverDir)
	checkpointed := false
	controller.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := transfer(ctx, name, args...)
		if err == nil && filepath.Base(args[len(args)-2]) == "dhcp-sticky.db" && !checkpointed {
			var busy, logFrames, checkpointedFrames int
			if err := checkpoint.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointedFrames); err != nil || busy != 0 {
				t.Fatalf("fixture checkpoint failed: busy=%d %v", busy, err)
			}
			info, err := os.Stat(sourceDB + "-wal")
			if err != nil || info.Size() != 0 {
				t.Fatalf("fixture must retain a truncated WAL: %v", err)
			}
			checkpointed = true
		}
		return out, err
	}
	if err := controller.reconcileJob(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if !checkpointed {
		t.Fatal("fixture never exercised DB/WAL checkpoint boundary")
	}
	status := controller.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name)
	if status["phase"] != "Synced" {
		t.Fatalf("sync failed: %#v", status)
	}
	receiver, err := sql.Open("sqlite", "file:"+filepath.Join(receiverDir, "dhcp-sticky.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	var hostname string
	if err := receiver.QueryRow("SELECT hostname FROM dhcp_sticky_leases WHERE mac=? AND ip=?", "aa:bb:cc:dd:ee:ff", "192.0.2.20").Scan(&hostname); err != nil || hostname != "acknowledged-client" {
		t.Fatalf("Synced lost an already committed lease: hostname=%q error=%v", hostname, err)
	}
}
