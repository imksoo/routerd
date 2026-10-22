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

// Model a checkpoint between copying the DB and its WAL. The next reconciliation
// must transfer the checkpointed DB rather than skip it using size/mtime alone.
// This proves that retry case, not atomic replication under concurrent writes.
func TestLeaseSyncRetryAfterWALCloseCopiesCheckpointedDatabase(t *testing.T) {
	srcDir, dstDir := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "dhcp-sticky.db")
	writer, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "CREATE TABLE leases (id INTEGER PRIMARY KEY, name TEXT)", "PRAGMA wal_checkpoint(TRUNCATE)", "INSERT INTO leases VALUES (1,'committed-lease')"} {
		if _, err := writer.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	stamp := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(src, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	job := fileSyncJob{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync", Name: "replica", Sources: []fileSyncSource{{Name: "stickyLog", Path: src}, {Name: "stickyLogWAL", Path: src + "-wal"}}, Targets: []fileSyncTarget{{Host: "unused.invalid"}}}
	transfer := localLeaseSyncCommand(t, dstDir)
	closed := false
	c := FileSyncController{Store: mapStore{}, Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := transfer(ctx, name, args...)
		if err == nil && args[len(args)-2] == src && !closed {
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			closed = true
			if err := os.Chtimes(src, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(src)
			if err != nil || after.Size() != before.Size() {
				t.Fatalf("fixture must retain DB size: %v", err)
			}
			if _, err := os.Stat(src + "-wal"); !os.IsNotExist(err) {
				t.Fatalf("fixture WAL still exists: %v", err)
			}
		}
		return out, err
	}}
	if err := c.reconcileJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if s := c.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name); s["phase"] != "Error" || s["exitCode"] != 23 || s["source"] != src+"-wal" {
		t.Fatalf("vanished WAL must fail: %#v", s)
	}
	if err := c.reconcileJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if s := c.Store.ObjectStatus(job.APIVersion, job.Kind, job.Name); s["phase"] != "Synced" {
		t.Fatalf("retry failed: %#v", s)
	}
	receiver, err := sql.Open("sqlite", "file:"+filepath.Join(dstDir, "dhcp-sticky.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	var name, integrity string
	if err := receiver.QueryRow("SELECT name FROM leases WHERE id=1").Scan(&name); err != nil || name != "committed-lease" {
		t.Fatalf("retry lost committed lease: %q %v", name, err)
	}
	if err := receiver.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("replica invalid: %q %v", integrity, err)
	}
}
