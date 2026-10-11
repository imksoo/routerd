// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"strings"
	"testing"
)

// These offline fixtures prove that OpenSQLite uses the ownership migration's
// rollback and fail-closed contracts. They do not simulate a process crash.
func artifactMigrationTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store := mustOpenStore(t)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close ownership migration fixture: %v", err)
		}
	})
	// The missing primary key deliberately permits duplicate IDs to inject
	// an INSERT failure after the first row has reached the new table.
	execArtifactMigrationSQL(t, store, `DROP TABLE artifacts;
CREATE TABLE artifacts(id TEXT, kind TEXT, name TEXT, owner TEXT, attributes TEXT, source TEXT, generation INTEGER, observed_at TEXT);
INSERT INTO artifacts VALUES('nft.table/a','nft.table','a','net.routerd.net/v1alpha1/NAT44Rule/lan','{}','routerd',1,'2026-10-11T00:00:00Z');
INSERT INTO artifacts VALUES('nft.table/a','nft.table','b','net.routerd.net/v1alpha1/NAT44Rule/lan','{}','routerd',1,'2026-10-11T00:00:00Z');`)
	return store
}

func execArtifactMigrationSQL(t *testing.T, store *SQLiteStore, query string) {
	t.Helper()
	if _, err := store.db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteStoreArtifactMigrationRollbackAndRetry(t *testing.T) {
	fixture := artifactMigrationTestStore(t)
	if store, err := OpenSQLite(fixture.path); err == nil {
		_ = store.Close()
		t.Fatal("state store accepted a failed ownership migration")
	} else if !strings.Contains(err.Error(), "copy legacy owned artifact") {
		t.Fatalf("state store did not report the injected copy failure: %v", err)
	}
	if old, err := fixture.tableHasColumn("artifacts", "id"); err != nil || !old {
		t.Fatalf("state store lost original ownership schema: old=%v err=%v", old, err)
	}
	if legacy, err := fixture.tableExists("artifacts_legacy"); err != nil || legacy {
		t.Fatalf("state store left a partial migration: legacy=%v err=%v", legacy, err)
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT count(*) FROM artifacts`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("rollback lost source ownership: count=%d err=%v", count, err)
	}
	// Repair only the deliberate duplicate ID and retry the public entry.
	execArtifactMigrationSQL(t, fixture, `UPDATE artifacts SET id = 'nft.table/b' WHERE name = 'b'`)
	recovered, err := OpenSQLite(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := recovered.Close(); err != nil {
			t.Errorf("close recovered state store: %v", err)
		}
	})
	if err := recovered.db.QueryRow(`SELECT count(*) FROM artifacts WHERE artifact_id IN ('nft.table/a','nft.table/b') AND owner_api_version = 'net.routerd.net/v1alpha1' AND owner_kind = 'NAT44Rule' AND owner_name = 'lan'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("retry did not preserve both owners: count=%d err=%v", count, err)
	}
	if legacy, err := recovered.tableExists("artifacts_legacy"); err != nil || legacy {
		t.Fatalf("retry retained old ownership table: legacy=%v err=%v", legacy, err)
	}
}

func TestSQLiteStoreRejectsInterruptedOwnershipMigration(t *testing.T) {
	fixture := artifactMigrationTestStore(t)
	execArtifactMigrationSQL(t, fixture, `ALTER TABLE artifacts RENAME TO artifacts_legacy;
CREATE TABLE artifacts(artifact_id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, owner_api_version TEXT, owner_kind TEXT, owner_name TEXT);
INSERT INTO artifacts VALUES('nft.table/a','nft.table','a','net.routerd.net/v1alpha1','NAT44Rule','new-owner');`)
	if store, err := OpenSQLite(fixture.path); err == nil {
		_ = store.Close()
		t.Fatal("state store accepted previously interrupted ownership migration")
	} else if !strings.Contains(err.Error(), "unfinished ownership migration: artifacts_legacy exists") {
		t.Fatalf("unexpected state store error: %v", err)
	}
	var owner string
	if err := fixture.db.QueryRow(`SELECT owner_name FROM artifacts WHERE artifact_id = 'nft.table/a'`).Scan(&owner); err != nil || owner != "new-owner" {
		t.Fatalf("state store replayed stale ownership: owner=%q err=%v", owner, err)
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT count(*) FROM artifacts_legacy`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("state store changed interrupted source: count=%d err=%v", count, err)
	}
}
