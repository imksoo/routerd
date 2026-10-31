// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogRetentionPathUsesOpenedSQLiteIdentity(t *testing.T) {
	t.Run("relative-file-after-chdir", func(t *testing.T) {
		opened := t.TempDir()
		t.Chdir(opened)
		store, err := OpenSQLite("state.db")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		moved := filepath.Join(opened, "other")
		if err := os.Mkdir(moved, 0700); err != nil {
			t.Fatal(err)
		}
		t.Chdir(moved)
		path, err := store.LogRetentionPath()
		want := filepath.Join(opened, "state.db")
		if err != nil || path != want {
			t.Fatalf("opened database identity=%q want=%q err=%v", path, want, err)
		}
	})
	t.Run("memory-has-no-file", func(t *testing.T) {
		t.Chdir(t.TempDir())
		store, err := OpenSQLite(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		var file string
		if err := store.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&file); err != nil || file != "" {
			t.Fatalf("fixture is not an in-memory SQLite database: %q %v", file, err)
		}
		path, err := store.LogRetentionPath()
		if err == nil || path != "" {
			t.Fatalf("memory database was mistaken for a file: %q %v", path, err)
		}
	})
}
