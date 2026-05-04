package logstore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type RetentionTarget struct {
	File      string
	Retention time.Duration
}

type RetentionResult struct {
	File    string `json:"file"`
	Deleted int64  `json:"deleted"`
	Skipped bool   `json:"skipped,omitempty"`
}

func ApplyRetention(ctx context.Context, target RetentionTarget, incrementalVacuum bool) (RetentionResult, error) {
	result := RetentionResult{File: target.File}
	if target.File == "" || target.Retention <= 0 {
		result.Skipped = true
		return result, nil
	}
	if _, err := os.Stat(target.File); err != nil {
		if os.IsNotExist(err) {
			result.Skipped = true
			return result, nil
		}
		return result, err
	}
	if err := os.MkdirAll(filepath.Dir(target.File), 0o755); err != nil {
		return result, err
	}
	db, err := sql.Open("sqlite", target.File)
	if err != nil {
		return result, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout = 5000;`); err != nil {
		return result, err
	}
	cutoff := time.Now().Add(-target.Retention).UTC()
	deleted, err := deleteExpired(ctx, db, cutoff)
	if err != nil {
		return result, err
	}
	result.Deleted = deleted
	if incrementalVacuum {
		_, _ = db.ExecContext(ctx, `PRAGMA incremental_vacuum;`)
	}
	return result, nil
}

func deleteExpired(ctx context.Context, db *sql.DB, cutoff time.Time) (int64, error) {
	total := int64(0)
	for _, table := range []struct {
		Name   string
		Column string
		Kind   string
	}{
		{Name: "dns_queries", Column: "ts", Kind: "unix_ns"},
		{Name: "flows", Column: "ts_started", Kind: "unix_ns"},
		{Name: "firewall_logs", Column: "ts", Kind: "unix_ns"},
		{Name: "events", Column: "created_at", Kind: "rfc3339"},
	} {
		if !tableExists(ctx, db, table.Name) {
			continue
		}
		var result sql.Result
		var err error
		switch table.Kind {
		case "unix_ns":
			result, err = db.ExecContext(ctx, `DELETE FROM `+table.Name+` WHERE `+table.Column+` < ?`, cutoff.UnixNano())
		case "rfc3339":
			result, err = db.ExecContext(ctx, `DELETE FROM `+table.Name+` WHERE `+table.Column+` < ?`, cutoff.Format(time.RFC3339Nano))
		}
		if err != nil {
			return total, err
		}
		if affected, err := result.RowsAffected(); err == nil {
			total += affected
		}
	}
	return total, nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) bool {
	var got string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&got)
	return err == nil && got == name
}

func ParseRetention(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}
