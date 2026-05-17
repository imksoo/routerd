// SPDX-License-Identifier: BSD-3-Clause

package conntrack

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSnapshotUsesProcCountAndMax(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	maxPath := filepath.Join(dir, "max")
	entriesPath := filepath.Join(dir, "entries")
	if err := os.WriteFile(countPath, []byte("3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maxPath, []byte("1024\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entriesPath, []byte("ignored\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadSnapshot(Paths{Entries: entriesPath, Count: countPath, Max: maxPath})
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snapshot.Count != 3 || snapshot.Max != 1024 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestReadSnapshotFallsBackToEntryCount(t *testing.T) {
	dir := t.TempDir()
	entriesPath := filepath.Join(dir, "entries")
	if err := os.WriteFile(entriesPath, []byte("a\n\nb\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ReadSnapshot(Paths{Entries: entriesPath, Count: filepath.Join(dir, "missing"), Max: filepath.Join(dir, "missing-max")})
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snapshot.Count != 2 {
		t.Fatalf("count = %d, want 2", snapshot.Count)
	}
}

func TestReadSnapshotUnavailableWhenCountAndEntriesMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadSnapshot(Paths{
		Entries: filepath.Join(dir, "missing-entries"),
		Count:   filepath.Join(dir, "missing-count"),
		Max:     filepath.Join(dir, "missing-max"),
	})
	if err == nil {
		t.Fatal("expected unavailable error")
	}
	if !IsUnavailable(err) {
		t.Fatalf("IsUnavailable = false for %T: %v", err, err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error does not unwrap to path error: %T: %v", err, err)
	}
}
