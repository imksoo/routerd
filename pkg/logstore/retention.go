// SPDX-License-Identifier: BSD-3-Clause

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
	File         string
	Retention    time.Duration
	Signals      []string
	BeforeDelete func(RetentionRecord) error
}

// RetentionTable describes an operational-log table. State tables are
// intentionally absent: LogRetention must never prune data required to run
// the router.
type RetentionTable struct {
	Signal string
	Name   string
	Column string
	Kind   string
}

const (
	RetentionSignalEvents           = "events"
	RetentionSignalAccessLogs       = "accessLogs"
	RetentionSignalPluginRuns       = "pluginRuns"
	RetentionSignalDNSQueries       = "dnsQueries"
	RetentionSignalTrafficFlows     = "trafficFlows"
	RetentionSignalFirewallEvents   = "firewallEvents"
	RetentionSignalDHCPFingerprints = "dhcpFingerprints"
)

var retentionTablesBySignal = map[string][]RetentionTable{
	RetentionSignalEvents:           {{Name: "events", Column: "created_at", Kind: "rfc3339"}},
	RetentionSignalAccessLogs:       {{Name: "access_logs", Column: "ts", Kind: "rfc3339"}},
	RetentionSignalPluginRuns:       {{Name: "plugin_runs", Column: "started_at", Kind: "rfc3339"}},
	RetentionSignalDNSQueries:       {{Name: "dns_queries", Column: "ts", Kind: "unix_ns"}},
	RetentionSignalTrafficFlows:     {{Name: "flows", Column: "ts_started", Kind: "unix_ns"}},
	RetentionSignalFirewallEvents:   {{Name: "firewall_logs", Column: "ts", Kind: "unix_ns"}},
	RetentionSignalDHCPFingerprints: {{Name: "dhcp_fingerprint", Column: "observed_at", Kind: "unix_ns"}},
}

// RetentionTables returns the registered tables for signals. Empty keeps the
// legacy all-operational-logs behaviour for callers outside the controller.
func RetentionTables(signals []string) []RetentionTable {
	if len(signals) == 0 {
		signals = []string{RetentionSignalEvents, RetentionSignalAccessLogs, RetentionSignalPluginRuns, RetentionSignalDNSQueries, RetentionSignalTrafficFlows, RetentionSignalFirewallEvents, RetentionSignalDHCPFingerprints}
	}
	seen := map[string]bool{}
	var out []RetentionTable
	for _, signal := range signals {
		for _, table := range retentionTablesBySignal[signal] {
			table.Signal = signal
			if !seen[table.Name] {
				seen[table.Name] = true
				out = append(out, table)
			}
		}
	}
	return out
}

type RetentionResult struct {
	File          string `json:"file"`
	Deleted       int64  `json:"deleted"`
	Skipped       bool   `json:"skipped,omitempty"`
	Vacuumed      bool   `json:"vacuumed,omitempty"`
	FreelistPages int64  `json:"freelistPages,omitempty"`
}

type RetentionRecord struct {
	Signal string
	Table  string
	Values map[string]any
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
	deleted, err := deleteExpired(ctx, db, cutoff, RetentionTables(target.Signals), target.BeforeDelete)
	if err != nil {
		return result, err
	}
	result.Deleted = deleted
	if incrementalVacuum {
		vacuumed, freelistPages, err := vacuumAfterRetention(ctx, db)
		if err != nil {
			return result, err
		}
		result.Vacuumed = vacuumed
		result.FreelistPages = freelistPages
	}
	return result, nil
}

func vacuumAfterRetention(ctx context.Context, db *sql.DB) (bool, int64, error) {
	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`)

	freelistPages := int64(0)
	if err := db.QueryRowContext(ctx, `PRAGMA freelist_count;`).Scan(&freelistPages); err != nil {
		return false, 0, err
	}
	if freelistPages == 0 {
		return false, 0, nil
	}

	autoVacuum := 0
	if err := db.QueryRowContext(ctx, `PRAGMA auto_vacuum;`).Scan(&autoVacuum); err != nil {
		return false, freelistPages, err
	}
	if autoVacuum == 0 {
		if _, err := db.ExecContext(ctx, `VACUUM;`); err != nil {
			return false, freelistPages, err
		}
	} else {
		if _, err := db.ExecContext(ctx, `PRAGMA incremental_vacuum;`); err != nil {
			return false, freelistPages, err
		}
	}
	_, _ = db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`)
	return true, freelistPages, nil
}

func deleteExpired(ctx context.Context, db *sql.DB, cutoff time.Time, tables []RetentionTable, beforeDelete func(RetentionRecord) error) (int64, error) {
	total := int64(0)
	for _, table := range tables {
		if !tableExists(ctx, db, table.Name) {
			continue
		}
		var result sql.Result
		var err error
		if beforeDelete != nil {
			if err := exportExpired(ctx, db, table, cutoff, beforeDelete); err != nil {
				return total, err
			}
		}
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

func exportExpired(ctx context.Context, db *sql.DB, table RetentionTable, cutoff time.Time, emit func(RetentionRecord) error) error {
	cutoffValue := any(cutoff.UnixNano())
	if table.Kind == "rfc3339" {
		cutoffValue = cutoff.Format(time.RFC3339Nano)
	}
	rows, err := db.QueryContext(ctx, `SELECT * FROM `+table.Name+` WHERE `+table.Column+` < ?`, cutoffValue)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		record := make(map[string]any, len(columns))
		for i, value := range values {
			if bytes, ok := value.([]byte); ok {
				record[columns[i]] = string(bytes)
			} else {
				record[columns[i]] = value
			}
		}
		if err := emit(RetentionRecord{Signal: table.Signal, Table: table.Name, Values: record}); err != nil {
			return err
		}
	}
	return rows.Err()
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
