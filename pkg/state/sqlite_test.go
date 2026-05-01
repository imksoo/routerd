package state

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSQLiteStorePersistsAndSupportsJSON1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	lease := PDLease{
		LastPrefix:     "2001:db8:1200:1210::/60",
		LastObservedAt: time.Now().UTC().Format(time.RFC3339),
	}
	store.Set("ipv6PrefixDelegation.wan-pd.lease", EncodePDLease(lease), "test")
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	defer db.Close()
	var prefix string
	err = db.QueryRow(`SELECT json_extract(status, '$.lastPrefix') FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, "net.routerd.net/v1alpha1", "IPv6PrefixDelegation", "wan-pd").Scan(&prefix)
	if err != nil {
		t.Fatalf("json_extract lease prefix: %v", err)
	}
	if prefix != lease.LastPrefix {
		t.Fatalf("json prefix = %q, want %q", prefix, lease.LastPrefix)
	}
}

func TestSQLiteStoreMigratesLegacyJSONAndRenames(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "state.json")
	if err := os.WriteFile(legacy, []byte(`{
  "variables": {
    "ipv6PrefixDelegation.wan-pd.lease": {
      "status": "set",
      "value": "{\"lastPrefix\":\"2001:db8:1200:1210::/60\"}",
      "since": "2026-04-28T00:00:00Z",
      "updatedAt": "2026-04-28T00:00:00Z"
    }
  }
}
`), 0644); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	store, err := OpenSQLite(filepath.Join(dir, "routerd.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()
	lease, ok := DecodePDLease(store.Get("ipv6PrefixDelegation.wan-pd.lease").Value)
	if !ok || lease.LastPrefix != "2001:db8:1200:1210::/60" {
		t.Fatalf("lease = %+v ok=%v", lease, ok)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy state still present: %v", err)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Fatalf("migrated state missing: %v", err)
	}
}

func TestSQLiteStoreMigratesTwoTableSQLiteState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE state(key TEXT PRIMARY KEY, value TEXT, status TEXT, reason TEXT, since TEXT, updated_at TEXT);
INSERT INTO state(key,value,status,reason,since,updated_at) VALUES('ipv6PrefixDelegation.wan-pd.lease','{"lastPrefix":"2001:db8:1200:1210::/60"}','set','test','2026-04-28T00:00:00Z','2026-04-28T00:00:00Z');`)
	if err != nil {
		t.Fatalf("seed fixture db: %v", err)
	}
	_ = db.Close()

	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()
	lease, ok := DecodePDLease(store.Get("ipv6PrefixDelegation.wan-pd.lease").Value)
	if !ok || lease.LastPrefix != "2001:db8:1200:1210::/60" {
		t.Fatalf("lease = %+v ok=%v", lease, ok)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen fixture db: %v", err)
	}
	defer db.Close()
	var tableName string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='state'`).Scan(&tableName)
	if err == nil {
		t.Fatal("legacy state table still exists")
	}
}

func TestSQLiteStoreAddsLastAppliedPathColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE objects (
  api_version TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  uid TEXT,
  resource_version INTEGER NOT NULL DEFAULT 1,
  observed_generation INTEGER,
  status TEXT,
  created_at TEXT NOT NULL,
  modified_at TEXT NOT NULL,
  PRIMARY KEY(api_version, kind, name)
);
INSERT INTO objects(api_version,kind,name,uid,status,created_at,modified_at)
VALUES('net.routerd.net/v1alpha1','IPv6PrefixDelegation','wan-pd','net.routerd.net/v1alpha1/IPv6PrefixDelegation/wan-pd','{}','2026-05-01T00:00:00Z','2026-05-01T00:00:00Z');`)
	if err != nil {
		t.Fatalf("seed fixture db: %v", err)
	}
	_ = db.Close()

	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()

	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen fixture db: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`PRAGMA table_info(objects)`)
	if err != nil {
		t.Fatalf("table info: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		if name == "last_applied_path" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("scan columns: %v", err)
	}
	if !found {
		t.Fatal("objects.last_applied_path column was not added")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM objects WHERE api_version = 'net.routerd.net/v1alpha1' AND kind = 'IPv6PrefixDelegation' AND name = 'wan-pd'`).Scan(&count); err != nil {
		t.Fatalf("count existing object: %v", err)
	}
	if count != 1 {
		t.Fatalf("existing objects count = %d, want 1", count)
	}
}

func TestSQLiteStoreGenerationsAndEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()
	generation, err := store.BeginGeneration("abc123")
	if err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	store.Set("ipv6PrefixDelegation.wan-pd.lease", EncodePDLease(PDLease{LastPrefix: "2001:db8:1200:1210::/60"}), "test")
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "IPv6PrefixDelegation", "wan-pd", "Normal", "PrefixObserved", "observed prefix"); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.FinishGeneration(generation, "Healthy", []string{"warning"}); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	defer db.Close()
	var observedGeneration int64
	if err := db.QueryRow(`SELECT observed_generation FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, "net.routerd.net/v1alpha1", "IPv6PrefixDelegation", "wan-pd").Scan(&observedGeneration); err != nil {
		t.Fatalf("read observed generation: %v", err)
	}
	if observedGeneration != generation {
		t.Fatalf("observed generation = %d, want %d", observedGeneration, generation)
	}
	events := store.Events("net.routerd.net/v1alpha1", "IPv6PrefixDelegation", "wan-pd", 10)
	if len(events) != 1 || events[0].Generation != generation || events[0].Reason != "PrefixObserved" {
		t.Fatalf("events = %+v", events)
	}
}
