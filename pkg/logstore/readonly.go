// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func openReadOnlySQLite(path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 250;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// sqliteFileURI escapes filesystem names; explicit :memory: retains SQLite's
// native memory semantics and is never interpreted as a literal filename.
func sqliteFileURI(path string) (string, error) {
	if strings.IndexByte(path, 0) >= 0 {
		return "", fmt.Errorf("log database path contains NUL")
	}
	if path == ":memory:" {
		return (&url.URL{Scheme: "file", Opaque: path}).String(), nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Path: absolute}).String(), nil
}
