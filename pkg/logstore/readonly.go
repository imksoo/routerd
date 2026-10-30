// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"

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

// sqliteFileURI treats configured paths as filesystem names, not SQLite DSNs.
func sqliteFileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}
