// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSQLiteProtectsExistingAndRecreatedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "state-link.db")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	store, err := OpenSQLite(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	files := []string{path, path + "-wal", path + "-shm"}
	for _, name := range files {
		if err := os.Chmod(name, 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Reopening an existing permissive database must protect its live sidecars
	// before any configuration history is written, even through a symlink.
	reopened, err := OpenSQLite(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for _, name := range files {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unprotected %s: %v", filepath.Base(name), err)
		}
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, name := range files {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("recreated %s is not protected: %v", filepath.Base(name), err)
		}
	}
}
