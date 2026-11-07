// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDHCPLeaseMemoryCannotAdvertiseFilesystemSource(t *testing.T) {
	t.Chdir(t.TempDir())
	store, err := OpenSQLite(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.RecordBusEvent(t.Context(), namedLeaseEvent("added", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	path, err := store.DHCPLeaseJournalPath()
	if err == nil || path != "" {
		t.Fatalf("memory lease source was advertised as a physical file: %q %v", path, err)
	}
}

func TestDHCPLeaseJournalSourceAcrossProcess(t *testing.T) {
	args := os.Args
	if len(args) >= 3 && args[len(args)-2] == "--dhcp-source-file" {
		path := args[len(args)-1]
		if !filepath.IsAbs(path) {
			t.Fatal("child received a relative source")
		}
		store, err := OpenSQLiteReadOnly(path)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		rows, err := store.ReadDHCPLeaseObservations(t.Context())
		if err != nil || len(rows) != 1 || rows[0].Source != "DHCPv4Server/lan" || rows[0].Action != "added" || rows[0].IP != "192.0.2.55" {
			t.Fatalf("child did not read the named observation: %+v %v", rows, err)
		}
		return
	}
	opened := t.TempDir()
	t.Chdir(opened)
	store, err := OpenSQLite("relative-state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.RecordBusEvent(t.Context(), namedLeaseEvent("added", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	path, err := store.DHCPLeaseJournalPath()
	if err != nil || path != filepath.Join(opened, "relative-state.db") {
		t.Fatalf("opened source=%q err=%v", path, err)
	}
	childDir := t.TempDir()
	t.Chdir(childDir)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestDHCPLeaseJournalSourceAcrossProcess$", "-test.v", "--", "--dhcp-source-file", path)
	command.Dir = childDir
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "--- PASS: TestDHCPLeaseJournalSourceAcrossProcess") || strings.Contains(string(output), "--- SKIP:") {
		t.Fatalf("independent readonly source process: %v\n%s", err, output)
	}
}
