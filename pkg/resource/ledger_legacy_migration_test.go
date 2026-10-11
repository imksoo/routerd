// SPDX-License-Identifier: BSD-3-Clause

package resource

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// These offline database tests require failed migrations to preserve the old
// schema and every ownership row, and successful retries to recover ownership.
// The deliberately modified fixtures inject database failures; they do not
// establish behavior during an actual process crash or host network apply.
func legacyMigrationTestLedger(t *testing.T) *SQLiteLedger {
	t.Helper()
	path := filepath.Join(t.TempDir(), "routerd.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close legacy migration fixture: %v", err)
		}
	})
	l := &SQLiteLedger{path: path, db: db}
	// Omit the old primary key so a duplicate ID can deliberately fail the
	// second INSERT into the new schema, after one ownership row was copied.
	execLedgerSQL(t, l, `CREATE TABLE artifacts(id TEXT, kind TEXT, name TEXT, owner TEXT, attributes TEXT, source TEXT, generation INTEGER, observed_at TEXT)`)
	for _, artifact := range ledgerErrorArtifacts() {
		if _, err := db.Exec(`INSERT INTO artifacts(id,kind,name,owner,attributes,source,generation,observed_at) VALUES(?,?,?,?,?,?,?,?)`,
			artifact.Identity(), artifact.Kind, artifact.Name, artifact.Owner, `{}`, "routerd", 1, "2026-10-11T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func requireLegacyMigrationSource(t *testing.T, l *SQLiteLedger) {
	t.Helper()
	hasOld, err := l.tableHasColumn("artifacts", "id")
	if err != nil || !hasOld {
		t.Fatalf("old schema lost: id column=%v err=%v", hasOld, err)
	}
	hasNew, err := l.tableHasColumn("artifacts", "artifact_id")
	if err != nil || hasNew {
		t.Fatalf("partial new schema survived: artifact_id column=%v err=%v", hasNew, err)
	}
	if exists, err := l.tableExists("artifacts_legacy"); err != nil || exists {
		t.Fatalf("migration left renamed source: exists=%v err=%v", exists, err)
	}
	var count int
	if err := l.db.QueryRow(`SELECT count(*) FROM artifacts`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("old ownership rows: count=%d err=%v", count, err)
	}
}

func TestSQLiteLedgerLegacyTableMigrationRollbackAndRetry(t *testing.T) {
	for _, tt := range []struct {
		name, corrupt, repair, wantError string
	}{
		{
			name:      "copy failure after first insert",
			corrupt:   `UPDATE artifacts SET id = 'nft.table/a' WHERE name = 'b'`,
			repair:    `UPDATE artifacts SET id = 'nft.table/b' WHERE name = 'b'`,
			wantError: "copy legacy owned artifact",
		},
		{
			name:      "scan failure closes rows",
			corrupt:   `UPDATE artifacts SET kind = NULL WHERE name = 'b'`,
			repair:    `UPDATE artifacts SET kind = 'nft.table' WHERE name = 'b'`,
			wantError: "read legacy owned artifact",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := legacyMigrationTestLedger(t)
			execLedgerSQL(t, l, tt.corrupt)
			if err := l.migrateLegacyArtifactsTable(); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("migration did not report injected failure: %v", err)
			}
			// This read also requires the sole connection to be released when a
			// scan fails; unclosed rows must not strand the migration transaction.
			requireLegacyMigrationSource(t, l)
			if err := l.Close(); err != nil {
				t.Fatal(err)
			}
			if reopened, err := OpenSQLiteLedger(l.path); err == nil {
				_ = reopened.Close()
				t.Fatal("reopen accepted an incomplete migration")
			} else if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("reopen did not retry the original migration: %v", err)
			}
			// Repair only the deliberately injected fault, then retry through
			// the public entry point against the persisted database.
			db, err := sql.Open("sqlite", l.path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Errorf("close repaired legacy fixture: %v", err)
				}
			})
			repairLedger := &SQLiteLedger{path: l.path, db: db}
			requireLegacyMigrationSource(t, repairLedger)
			execLedgerSQL(t, repairLedger, tt.repair)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			recovered, err := OpenSQLiteLedger(l.path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := recovered.Close(); err != nil {
					t.Errorf("close recovered ownership ledger: %v", err)
				}
			})
			all, err := recovered.All()
			if err != nil || len(all) != 2 {
				t.Fatalf("retry lost ownership: artifacts=%+v err=%v", all, err)
			}
			for i, artifact := range ledgerErrorArtifacts() {
				owns, err := recovered.Owns(artifact)
				if err != nil || !owns {
					t.Fatalf("retry lost %s: owns=%v err=%v", artifact.Identity(), owns, err)
				}
				if all[i].Identity() != artifact.Identity() || all[i].Owner != artifact.Owner {
					t.Fatalf("retry changed ownership: got=%+v want=%+v", all[i], artifact)
				}
			}
			if exists, err := recovered.tableExists("artifacts_legacy"); err != nil || exists {
				t.Fatalf("successful migration retained old table: exists=%v err=%v", exists, err)
			}
		})
	}
}

func TestSQLiteLedgerRejectsPreviouslyInterruptedTableMigration(t *testing.T) {
	for _, partialCurrentTable := range []bool{false, true} {
		name := "renamed source only"
		if partialCurrentTable {
			name = "partially copied current table"
		}
		t.Run(name, func(t *testing.T) {
			l := legacyMigrationTestLedger(t)
			execLedgerSQL(t, l, `ALTER TABLE artifacts RENAME TO artifacts_legacy`)
			if partialCurrentTable {
				execLedgerSQL(t, l, `CREATE TABLE artifacts(artifact_id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL);
INSERT INTO artifacts VALUES('nft.table/a','nft.table','a')`)
			}
			if opened, err := OpenSQLiteLedger(l.path); err == nil {
				_ = opened.Close()
				t.Fatal("open accepted a previously interrupted migration")
			} else if !strings.Contains(err.Error(), "unfinished ownership migration: artifacts_legacy exists") {
				t.Fatalf("unexpected open error: %v", err)
			}
			var count int
			if err := l.db.QueryRow(`SELECT count(*) FROM artifacts_legacy`).Scan(&count); err != nil || count != 2 {
				t.Fatalf("interrupted source changed: count=%d err=%v", count, err)
			}
			wantCurrent := 0
			if partialCurrentTable {
				wantCurrent = 1
			}
			if err := l.db.QueryRow(`SELECT count(*) FROM artifacts`).Scan(&count); err != nil || count != wantCurrent {
				t.Fatalf("current ownership changed: count=%d want=%d err=%v", count, wantCurrent, err)
			}
		})
	}
}
