// SPDX-License-Identifier: BSD-3-Clause

// Package storage owns the single SQLite format shared by state and ownership.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"
)

// This branch starts a new format. Earlier prototype databases are not migrated.
const Version = 1501

var ErrSchemaVersion = errors.New("unsupported routerd database format; initialize a fresh routerd 1.5 database")

//go:embed schema.sql
var schema string

func Check(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != Version {
		return fmt.Errorf("%w (found %d)", ErrSchemaVersion, version)
	}
	return nil
}

// Initialize creates an empty database atomically. The transaction also
// serializes concurrent state/ledger opens; neither owner performs migrations.
func Initialize(db *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	var version, tables int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == Version {
		_, err = conn.ExecContext(ctx, "COMMIT")
		return err
	}
	if version != 0 {
		return fmt.Errorf("%w (found %d)", ErrSchemaVersion, version)
	}
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return err
	}
	if tables != 0 {
		return ErrSchemaVersion
	}
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", Version)); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}
