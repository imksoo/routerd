// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestListObjectStatusesReportsUninitializedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uninitialized.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE placeholder (id INTEGER)`); err != nil {
		t.Fatalf("create placeholder table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close sqlite: %v", err)
	}

	store, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatalf("open read-only sqlite: %v", err)
	}
	defer func() { _ = store.Close() }()
	_, err = store.ListObjectStatuses()
	if !errors.Is(err, ErrSchemaNotInitialized) {
		t.Fatalf("ListObjectStatuses error = %v, want ErrSchemaNotInitialized", err)
	}
	if strings.Contains(err.Error(), "SQL logic error") || strings.Contains(err.Error(), "no such table") {
		t.Fatalf("ListObjectStatuses leaked raw SQLite error: %v", err)
	}
}

func TestSQLiteStoreConfiguresWALCheckpoint(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()

	var autoCheckpoint int
	if err := store.db.QueryRow(`PRAGMA wal_autocheckpoint`).Scan(&autoCheckpoint); err != nil {
		t.Fatal(err)
	}
	if autoCheckpoint != stateWALAutoCheckpointPages {
		t.Fatalf("wal_autocheckpoint = %d, want %d", autoCheckpoint, stateWALAutoCheckpointPages)
	}

	var journalSizeLimit int64
	if err := store.db.QueryRow(`PRAGMA journal_size_limit`).Scan(&journalSizeLimit); err != nil {
		t.Fatal(err)
	}
	if journalSizeLimit != stateJournalSizeLimitBytes {
		t.Fatalf("journal_size_limit = %d, want %d", journalSizeLimit, stateJournalSizeLimitBytes)
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
	lease, ok, err := store.PDLease("ipv6PrefixDelegation.wan-pd")
	if err != nil || !ok || lease.LastPrefix != "2001:db8:1200:1210::/60" {
		t.Fatalf("lease = %+v ok=%v err=%v", lease, ok, err)
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
	lease, ok, err := store.PDLease("ipv6PrefixDelegation.wan-pd")
	if err != nil || !ok || lease.LastPrefix != "2001:db8:1200:1210::/60" {
		t.Fatalf("lease = %+v ok=%v err=%v", lease, ok, err)
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
VALUES('net.routerd.net/v1alpha1','DHCPv6PrefixDelegation','wan-pd','net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd','{}','2026-05-01T00:00:00Z','2026-05-01T00:00:00Z');`)
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
	if err := db.QueryRow(`SELECT count(*) FROM objects WHERE api_version = 'net.routerd.net/v1alpha1' AND kind = 'DHCPv6PrefixDelegation' AND name = 'wan-pd'`).Scan(&count); err != nil {
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
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd", "Normal", "PrefixObserved", "observed prefix"); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.FinishGeneration(generation, "Healthy", []string{"warning"}); err != nil {
		t.Fatalf("finish generation: %v", err)
	}
	if got := store.LatestGeneration(); got != generation {
		t.Fatalf("latest generation = %d, want %d", got, generation)
	}
	if err := store.RecordGenerationConfig(generation, "kind: Router\nmetadata:\n  name: lab\n"); err != nil {
		t.Fatalf("record generation config: %v", err)
	}
	records, err := store.ListGenerations(10)
	if err != nil {
		t.Fatalf("list generations: %v", err)
	}
	if len(records) != 1 || !records[0].HasYAML || records[0].ConfigHash != "abc123" {
		t.Fatalf("generation records = %+v", records)
	}
	configYAML, ok, err := store.GenerationConfig(generation)
	if err != nil {
		t.Fatalf("generation config: %v", err)
	}
	if !ok || !strings.Contains(configYAML, "kind: Router") {
		t.Fatalf("generation config ok=%t yaml=%q", ok, configYAML)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite directly: %v", err)
	}
	defer db.Close()
	var observedGeneration int64
	if err := db.QueryRow(`SELECT observed_generation FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, "net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd").Scan(&observedGeneration); err != nil {
		t.Fatalf("read observed generation: %v", err)
	}
	if observedGeneration != generation {
		t.Fatalf("observed generation = %d, want %d", observedGeneration, generation)
	}
	events := store.Events("net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd", 10)
	if len(events) != 1 || events[0].Generation != generation || events[0].Reason != "PrefixObserved" {
		t.Fatalf("events = %+v", events)
	}
}

func TestSQLiteStoreBeginGenerationIfChanged(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer func() { _ = store.Close() }()

	first, changed, err := store.BeginGenerationIfChanged("normalized-a", "kind: Router\n")
	if err != nil {
		t.Fatalf("begin first generation: %v", err)
	}
	if !changed || first == 0 {
		t.Fatalf("first generation = %d changed=%t, want a new generation", first, changed)
	}
	if err := store.FinishGeneration(first, "Healthy", nil); err != nil {
		t.Fatalf("finish first generation: %v", err)
	}

	duplicate, changed, err := store.BeginGenerationIfChanged("normalized-a", "kind: Router\n")
	if err != nil {
		t.Fatalf("begin duplicate generation: %v", err)
	}
	if changed || duplicate != first {
		t.Fatalf("duplicate generation = %d changed=%t, want existing generation %d", duplicate, changed, first)
	}
	if got := store.CurrentGeneration(); got != first {
		t.Fatalf("current generation after duplicate = %d, want %d", got, first)
	}
	if err := store.SaveObjectStatus("net.routerd.net/v1alpha1", "Hostname", "lan", map[string]any{"observed": "unchanged-config-reconcile"}); err != nil {
		t.Fatalf("save observed status: %v", err)
	}
	if got := store.ObjectGeneration("net.routerd.net/v1alpha1", "Hostname", "lan"); got != first {
		t.Fatalf("observed status generation = %d, want %d", got, first)
	}

	failed, changed, err := store.BeginGenerationIfChanged("normalized-b", "kind: Router\nmetadata: {}\n")
	if err != nil {
		t.Fatalf("begin failed generation: %v", err)
	}
	if !changed || failed <= first {
		t.Fatalf("failed generation = %d changed=%t, want generation after %d", failed, changed, first)
	}
	if err := store.FinishGeneration(failed, "Errored", nil); err != nil {
		t.Fatalf("finish failed generation: %v", err)
	}

	retry, changed, err := store.BeginGenerationIfChanged("normalized-b", "kind: Router\nmetadata: {}\n")
	if err != nil {
		t.Fatalf("retry changed generation: %v", err)
	}
	if !changed || retry <= failed {
		t.Fatalf("retry generation = %d changed=%t, want generation after %d", retry, changed, failed)
	}
	if err := store.FinishGeneration(retry, "Healthy", nil); err != nil {
		t.Fatalf("finish retry generation: %v", err)
	}
	duplicate, changed, err = store.BeginGenerationIfChanged("normalized-b", "kind: Router\nmetadata: {}\n")
	if err != nil {
		t.Fatalf("begin successful duplicate generation: %v", err)
	}
	if changed || duplicate != retry {
		t.Fatalf("successful duplicate generation = %d changed=%t, want existing generation %d", duplicate, changed, retry)
	}
	if records, err := store.ListGenerations(10); err != nil {
		t.Fatalf("list generations: %v", err)
	} else if len(records) != 3 {
		t.Fatalf("generation records = %+v, want 3", records)
	}
}

func TestOpenSQLiteReadOnlyImmutableReadsReadonlyDirectoryState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	if got := store.Set("manual.mode", "keep", "seed"); got.Status != StatusSet || got.Value != "keep" {
		t.Fatalf("seed state = %+v, want keep", got)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chmod(dir, 0755)
		_ = os.Chmod(path, 0644)
	})
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatalf("chmod state: %v", err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod state dir: %v", err)
	}

	readonly, err := OpenSQLiteReadOnlyImmutable(path)
	if err != nil {
		t.Fatalf("open immutable read-only sqlite: %v", err)
	}
	defer func() { _ = readonly.Close() }()
	if got := readonly.Get("manual.mode"); got.Status != StatusSet || got.Value != "keep" {
		t.Fatalf("read-only state = %+v, want keep", got)
	}
	vars := readonly.Variables()
	if got := vars["manual.mode"]; got.Status != StatusSet || got.Value != "keep" {
		t.Fatalf("read-only variables manual.mode = %+v, want keep", got)
	}
}

func TestOpenSQLiteReadOnlyReadsLiveWALGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	writer, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite writer: %v", err)
	}
	defer func() { _ = writer.Close() }()

	generation, err := writer.BeginGeneration("hash-1")
	if err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	if err := writer.FinishGeneration(generation, "Applied", nil); err != nil {
		t.Fatalf("finish generation: %v", err)
	}

	readonly, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatalf("open read-only sqlite: %v", err)
	}
	defer func() { _ = readonly.Close() }()
	if got := readonly.LatestGeneration(); got != generation {
		t.Fatalf("read-only latest generation = %d, want %d", got, generation)
	}
}

func TestSQLiteStoreMergeObjectStatusPreservesExistingFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()

	if err := store.SaveObjectStatus("mobility.routerd.net/v1alpha1", "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase": "Planned",
	}); err != nil {
		t.Fatalf("save planner status: %v", err)
	}
	if err := store.MergeObjectStatus("mobility.routerd.net/v1alpha1", "MobilityPool", "cloudedge", map[string]any{
		"discoveryPhase":                "Observed",
		"providerActionFailedAddresses": []string{"10.88.60.21/32"},
	}); err != nil {
		t.Fatalf("merge discovery status: %v", err)
	}

	status := store.ObjectStatus("mobility.routerd.net/v1alpha1", "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "Planned" || status["discoveryPhase"] != "Observed" {
		t.Fatalf("merged status = %#v", status)
	}
	if got, ok := status["providerActionFailedAddresses"].([]any); !ok || len(got) != 1 || got[0] != "10.88.60.21/32" {
		t.Fatalf("providerActionFailedAddresses = %#v", status["providerActionFailedAddresses"])
	}
}
