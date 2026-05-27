// SPDX-License-Identifier: BSD-3-Clause

package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLedgerRememberForgetAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifacts.json")
	ledger, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("load missing ledger: %v", err)
	}
	defer func() { _ = ledger.Close() }()
	artifact := Artifact{Kind: "nft.table", Name: "routerd_nat", Owner: "net.routerd.net/v1alpha1/NAT44Rule/lan"}
	ledger.Remember([]Artifact{artifact})
	if !ledger.Owns(artifact) {
		t.Fatal("ledger does not own remembered artifact")
	}
	if err := ledger.Save(path); err != nil {
		t.Fatalf("save ledger: %v", err)
	}
	loaded, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	defer func() { _ = loaded.Close() }()
	if !loaded.Owns(artifact) {
		t.Fatal("reloaded ledger does not own artifact")
	}
	loaded.Forget([]Artifact{artifact})
	if loaded.Owns(artifact) {
		t.Fatal("ledger still owns forgotten artifact")
	}
}

// TestSQLiteLedgerCloseReleasesFD is a regression test for issue #39:
// repeated LoadLedger/Close cycles must not leak SQLite file descriptors
// against the routerd.db (or its -wal/-shm companions). Linux-only because
// it inspects /proc/self/fd to count fds that resolve to the ledger path.
func TestSQLiteLedgerCloseReleasesFD(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/self/fd only available on linux")
	}
	path := filepath.Join(t.TempDir(), "routerd.db")
	// Warm up: open and close once so any one-time init fd churn settles
	// before we measure the baseline.
	warm, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("warmup load: %v", err)
	}
	if err := warm.Close(); err != nil {
		t.Fatalf("warmup close: %v", err)
	}
	base := countFDsPointingToPath(t, path)
	for i := 0; i < 10; i++ {
		ledger, err := LoadLedger(path)
		if err != nil {
			t.Fatalf("iter %d load: %v", i, err)
		}
		_ = ledger.All()
		if err := ledger.Close(); err != nil {
			t.Fatalf("iter %d close: %v", i, err)
		}
	}
	after := countFDsPointingToPath(t, path)
	if after > base {
		t.Fatalf("fd leak after 10 open/close cycles: before=%d after=%d", base, after)
	}
}

// countFDsPointingToPath walks /proc/self/fd and counts the file descriptors
// whose readlink target equals `path`, `path-journal`, `path-wal`, or
// `path-shm`. SQLite (modernc.org/sqlite, WAL mode) can hold all of these
// at once, so we treat any of them as a ledger-owned fd.
func countFDsPointingToPath(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read /proc/self/fd: %v", err)
	}
	suffixes := []string{"", "-journal", "-wal", "-shm"}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%s", entry.Name()))
		if err != nil {
			// fd vanished between ReadDir and Readlink; skip.
			continue
		}
		for _, suffix := range suffixes {
			if strings.HasSuffix(target, path+suffix) {
				count++
				break
			}
		}
	}
	return count
}
