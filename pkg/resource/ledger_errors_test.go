// SPDX-License-Identifier: BSD-3-Clause

package resource

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func errorTestLedger(t *testing.T) *SQLiteLedger {
	t.Helper()
	l, err := OpenSQLiteLedger(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func execLedgerSQL(t *testing.T, l *SQLiteLedger, query string) {
	t.Helper()
	if _, err := l.db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func ledgerErrorArtifacts() []Artifact {
	return []Artifact{
		{Kind: "nft.table", Name: "a", Owner: "net.routerd.net/v1alpha1/NAT44Rule/lan"},
		{Kind: "nft.table", Name: "b", Owner: "net.routerd.net/v1alpha1/NAT44Rule/lan"},
	}
}

func TestSQLiteLedgerClosedDatabaseErrors(t *testing.T) {
	l := errorTestLedger(t)
	artifacts := ledgerErrorArtifacts()
	if owns, err := l.Owns(artifacts[0]); err != nil || owns {
		t.Fatalf("missing artifact: owns=%v err=%v", owns, err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Remember(artifacts); err == nil {
		t.Fatal("Remember hid closed database")
	}
	if err := l.Forget(artifacts); err == nil {
		t.Fatal("Forget hid closed database")
	}
	if owns, err := l.Owns(artifacts[0]); err == nil || owns {
		t.Fatalf("Owns: %v, %v", owns, err)
	}
	if all, err := l.All(); err == nil || all != nil {
		t.Fatalf("All: %v, %v", all, err)
	}
}

func TestSQLiteLedgerReadOnlyWritesFail(t *testing.T) {
	l := errorTestLedger(t)
	artifacts := ledgerErrorArtifacts()
	if err := l.Remember(artifacts); err != nil {
		t.Fatal(err)
	}
	execLedgerSQL(t, l, `PRAGMA query_only = ON`)
	if err := l.Remember(artifacts); err == nil {
		t.Fatal("Remember hid read-only error")
	}
	if err := l.Forget(artifacts); err == nil {
		t.Fatal("Forget hid read-only error")
	}
	if all, err := l.All(); err != nil || len(all) != 2 {
		t.Fatalf("read-only writes changed ledger: %v, %v", all, err)
	}
}

func TestSQLiteLedgerBatchRollback(t *testing.T) {
	l := errorTestLedger(t)
	artifacts := ledgerErrorArtifacts()
	execLedgerSQL(t, l, `CREATE TRIGGER fail_insert BEFORE INSERT ON artifacts WHEN NEW.name = 'b' BEGIN SELECT RAISE(FAIL, 'injected insert failure'); END`)
	if err := l.Remember(artifacts); err == nil {
		t.Fatal("Remember hid insert error")
	}
	if all, err := l.All(); err != nil || len(all) != 0 {
		t.Fatalf("partial insert committed: %v, %v", all, err)
	}
	execLedgerSQL(t, l, `DROP TRIGGER fail_insert`)
	if err := l.Remember(artifacts); err != nil {
		t.Fatal(err)
	}
	execLedgerSQL(t, l, `CREATE TRIGGER fail_delete BEFORE DELETE ON artifacts WHEN OLD.name = 'b' BEGIN SELECT RAISE(FAIL, 'injected delete failure'); END`)
	if err := l.Forget(artifacts); err == nil {
		t.Fatal("Forget hid delete error")
	}
	if all, err := l.All(); err != nil || len(all) != 2 {
		t.Fatalf("partial delete committed: %v, %v", all, err)
	}
}

func TestSQLiteLedgerAllRejectsInvalidRows(t *testing.T) {
	for _, query := range []string{
		`UPDATE artifacts SET attributes = '{invalid' WHERE name = 'b'`,
		`ALTER TABLE artifacts RENAME TO old_artifacts; CREATE TABLE artifacts AS SELECT * FROM old_artifacts; UPDATE artifacts SET kind = NULL WHERE name = 'b'`,
	} {
		t.Run(query, func(t *testing.T) {
			l := errorTestLedger(t)
			if err := l.Remember(ledgerErrorArtifacts()); err != nil {
				t.Fatal(err)
			}
			execLedgerSQL(t, l, query)
			if all, err := l.All(); err == nil || all != nil {
				t.Fatalf("All returned partial/corrupt ownership: %v, %v", all, err)
			}
		})
	}
}

func TestSQLiteLedgerLockedWritesFail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	l, err := OpenSQLiteLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	artifacts := ledgerErrorArtifacts()
	if err := l.Remember(artifacts); err != nil {
		t.Fatal(err)
	}
	execLedgerSQL(t, l, `PRAGMA busy_timeout = 1`)
	other, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	defer other.Exec(`ROLLBACK`)
	if err := l.Remember(artifacts); err == nil {
		t.Fatal("Remember hid lock error")
	}
	if err := l.Forget(artifacts); err == nil {
		t.Fatal("Forget hid lock error")
	}
}

func TestSQLiteLedgerAllReportsIterationFailure(t *testing.T) {
	l := errorTestLedger(t)
	if err := l.Remember(ledgerErrorArtifacts()); err != nil {
		t.Fatal(err)
	}
	execLedgerSQL(t, l, `ALTER TABLE artifacts RENAME TO stored_artifacts;
 CREATE VIEW artifacts AS SELECT artifact_id, kind, name, owner_api_version, owner_kind, owner_name,
 CASE WHEN name = 'b' THEN json_extract('malformed', '$') ELSE attributes END AS attributes FROM stored_artifacts`)
	if all, err := l.All(); err == nil || all != nil {
		t.Fatalf("iteration failure returned partial success: %v, %v", all, err)
	}
}
