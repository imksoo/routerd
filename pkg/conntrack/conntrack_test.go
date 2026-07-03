// SPDX-License-Identifier: BSD-3-Clause

package conntrack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestCleanupAddressUsesOnlyScopedDeletes(t *testing.T) {
	var calls [][]string
	result := CleanupAddressWithRunner(context.Background(), "10.88.60.10/32", func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		return []byte("1 flow entries have been deleted\n"), nil
	})
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
	want := [][]string{
		{"-D", "-f", "ipv4", "-d", "10.88.60.10"},
		{"-D", "-f", "ipv4", "-s", "10.88.60.10"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for _, call := range calls {
		if len(call) == 0 || call[0] != "-D" {
			t.Fatalf("unexpected non-delete conntrack call: %#v", call)
		}
		if len(call) == 1 {
			t.Fatalf("unscoped conntrack delete must be impossible: %#v", call)
		}
	}
}

func TestCleanupAddressWarnsInsteadOfFailing(t *testing.T) {
	result := CleanupAddressWithRunner(context.Background(), "10.88.60.10", func(_ context.Context, args ...string) ([]byte, error) {
		return []byte("conntrack unavailable"), errors.New("exit status 1")
	})
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %#v, want two warning-only failures", result.Warnings)
	}
}
