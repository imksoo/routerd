// SPDX-License-Identifier: BSD-3-Clause
package storage

import (
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestInitializeRejectsOldDatabaseWithoutChangingItsData(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE old_state(value TEXT); INSERT INTO old_state VALUES ('keep')"); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(db); !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("old schema accepted: %v", err)
	}
	var value string
	if err := db.QueryRow("SELECT value FROM old_state").Scan(&value); err != nil || value != "keep" {
		t.Fatalf("old data changed: %q %v", value, err)
	}
}
func TestInitializeAndReopenCurrentFormat(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "current.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Initialize(db); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(db); err != nil {
		t.Fatal(err)
	}
	if err := Check(db); err != nil {
		t.Fatal(err)
	}
}
