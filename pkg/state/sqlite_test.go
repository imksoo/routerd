// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
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
	err = db.QueryRow(`SELECT json_extract(status, '$.lastPrefix') FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, "net.routerd.net/v1alpha1", "DHCPv6PrefixDelegation", "wan-pd").Scan(&prefix)
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

func TestSQLiteStoreObjectApplySource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()
	if err := store.SaveObjectApplySource("net.routerd.net/v1alpha1", "Interface", "wan", "/etc/routerd/wan.yaml"); err != nil {
		t.Fatalf("save apply source: %v", err)
	}
	if got := store.ObjectApplySource("net.routerd.net/v1alpha1", "Interface", "wan"); got != "/etc/routerd/wan.yaml" {
		t.Fatalf("apply source = %q", got)
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

func TestSQLiteStoreMaintenance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	oldTime := time.Now().Add(-48 * time.Hour).UTC()
	newTime := time.Now().UTC()
	store.now = func() time.Time { return oldTime }
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "Interface", "wan", "Normal", "OldEvent", "old event"); err != nil {
		t.Fatalf("record old event: %v", err)
	}
	store.now = func() time.Time { return newTime }
	if err := store.RecordEvent("net.routerd.net/v1alpha1", "Interface", "wan", "Normal", "NewEvent", "new event"); err != nil {
		t.Fatalf("record new event: %v", err)
	}

	result, err := store.IntegrityCheck()
	if err != nil {
		t.Fatalf("integrity check: %v", err)
	}
	if result != "ok" {
		t.Fatalf("integrity check = %q, want ok", result)
	}
	matched, err := store.CountEventsOlderThan(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("count old events: %v", err)
	}
	if matched != 1 {
		t.Fatalf("old event count = %d, want 1", matched)
	}
	deleted, err := store.PruneEventsOlderThan(time.Now().Add(-24 * time.Hour))
	if err != nil {
		t.Fatalf("prune old events: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted events = %d, want 1", deleted)
	}
	events := store.Events("net.routerd.net/v1alpha1", "Interface", "wan", 10)
	if len(events) != 1 || events[0].Reason != "NewEvent" {
		t.Fatalf("remaining events = %+v", events)
	}

	backupPath := filepath.Join(dir, "backup.db")
	if err := store.BackupTo(backupPath); err != nil {
		t.Fatalf("backup state: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if err := store.BackupTo(backupPath); err == nil {
		t.Fatal("backup to existing file succeeded")
	}
	backup, err := OpenSQLiteReadOnly(backupPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	if result, err := backup.IntegrityCheck(); err != nil || result != "ok" {
		t.Fatalf("backup integrity = %q err=%v", result, err)
	}
	if err := backup.Close(); err != nil {
		t.Fatalf("close backup: %v", err)
	}

	if err := store.Vacuum(); err != nil {
		t.Fatalf("vacuum: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close state: %v", err)
	}
}

func TestSQLiteStoreDynamicConfigParts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	gen1 := DynamicConfigPartRecord{
		Source:         "cloudedge",
		Generation:     1,
		ObservedAt:     now.Add(-2 * time.Hour),
		ExpiresAt:      now.Add(time.Hour),
		Digest:         "sha256:old",
		ResourcesJSON:  `[{"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","metadata":{"name":"wan"},"spec":{}}]`,
		DirectivesJSON: `[]`,
		Status:         "active",
	}
	if err := store.UpsertDynamicConfigPart(gen1); err != nil {
		t.Fatalf("upsert generation 1: %v", err)
	}
	gen1.Digest = "sha256:updated"
	if err := store.UpsertDynamicConfigPart(gen1); err != nil {
		t.Fatalf("replace generation 1: %v", err)
	}
	gen2 := DynamicConfigPartRecord{
		Source:         "cloudedge",
		Generation:     2,
		ObservedAt:     now.Add(-time.Hour),
		ExpiresAt:      now.Add(-time.Minute),
		Digest:         "sha256:new",
		ResourcesJSON:  `[]`,
		DirectivesJSON: `[{"op":"mask","target":{"apiVersion":"net.routerd.net/v1alpha1","kind":"Interface","name":"wan"},"reason":"test"}]`,
		Status:         "active",
	}
	if err := store.UpsertDynamicConfigPart(gen2); err != nil {
		t.Fatalf("upsert generation 2: %v", err)
	}

	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("list dynamic config parts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("parts len = %d, want 2: %+v", len(parts), parts)
	}
	if parts[0].Generation != 2 || parts[1].Generation != 1 {
		t.Fatalf("parts ordering = %+v, want generation 2 then 1", parts)
	}
	if parts[1].Digest != "sha256:updated" {
		t.Fatalf("generation 1 digest = %q, want replacement value", parts[1].Digest)
	}
	if parts[0].Status != "active" || parts[0].EffectiveStatus(now) != "expired" {
		t.Fatalf("expired status raw=%q effective=%q", parts[0].Status, parts[0].EffectiveStatus(now))
	}
	if got := parts[1].EffectiveStatus(now); got != "active" {
		t.Fatalf("active effective status = %q", got)
	}

	sourceParts, err := store.GetDynamicConfigPartsBySource("cloudedge")
	if err != nil {
		t.Fatalf("get dynamic config parts by source: %v", err)
	}
	if len(sourceParts) != 2 || sourceParts[0].Generation != 2 || sourceParts[1].Generation != 1 {
		t.Fatalf("source parts = %+v, want generation 2 then 1", sourceParts)
	}
}

func TestSQLiteStoreDynamicConfigPartActionPlansRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	const actionPlansJSON = `[{"name":"claim-secondary","provider":"oci","action":"assign-secondary-ip","mode":"dry-run","riskLevel":"low","target":{"address":"10.0.0.5","nicRef":"vnic-abc"},"expectedEffects":["secondary IP attached"]}]`
	rec := DynamicConfigPartRecord{
		Source:          "EventSubscription/sam/abc123",
		Generation:      1,
		ObservedAt:      now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		Digest:          "sha256:plans",
		ResourcesJSON:   `[]`,
		DirectivesJSON:  `[]`,
		ActionPlansJSON: actionPlansJSON,
		Status:          "active",
	}
	if err := store.UpsertDynamicConfigPart(rec); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	parts, err := store.GetDynamicConfigPartsBySource("EventSubscription/sam/abc123")
	if err != nil {
		t.Fatalf("get by source: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("parts len = %d, want 1", len(parts))
	}
	if parts[0].ActionPlansJSON != actionPlansJSON {
		t.Fatalf("actionPlansJSON round-trip = %q, want %q", parts[0].ActionPlansJSON, actionPlansJSON)
	}

	listed, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].ActionPlansJSON != actionPlansJSON {
		t.Fatalf("list actionPlansJSON = %+v", listed)
	}

	// A record with no action plans must round-trip as empty (column NULL).
	empty := DynamicConfigPartRecord{
		Source:         "EventSubscription/sam/empty",
		Generation:     1,
		ObservedAt:     now.Add(-time.Minute),
		ExpiresAt:      now.Add(time.Hour),
		Digest:         "sha256:none",
		ResourcesJSON:  `[]`,
		DirectivesJSON: `[]`,
		Status:         "active",
	}
	if err := store.UpsertDynamicConfigPart(empty); err != nil {
		t.Fatalf("upsert empty: %v", err)
	}
	emptyParts, err := store.GetDynamicConfigPartsBySource("EventSubscription/sam/empty")
	if err != nil {
		t.Fatalf("get empty by source: %v", err)
	}
	if len(emptyParts) != 1 || emptyParts[0].ActionPlansJSON != "" {
		t.Fatalf("empty actionPlansJSON = %q, want empty", emptyParts[0].ActionPlansJSON)
	}
}

func TestSQLiteStorePluginRunsListNewestFirstAndFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	firstID, err := store.RecordPluginRun(PluginRunRecord{
		Plugin:      "cloud",
		TriggerType: "manual",
		StartedAt:   now.Add(-time.Minute),
		Status:      "running",
	})
	if err != nil {
		t.Fatalf("record first run: %v", err)
	}
	firstExit := 1
	if err := store.CompletePluginRun(firstID, now.Add(-30*time.Second), &firstExit, "failed", "sha256:first", "boom", "exit 1"); err != nil {
		t.Fatalf("complete first run: %v", err)
	}
	secondID, err := store.RecordPluginRun(PluginRunRecord{
		Plugin:       "cloud",
		TriggerType:  "event",
		TriggerTopic: "routerd.test",
		StartedAt:    now,
		Status:       "running",
	})
	if err != nil {
		t.Fatalf("record second run: %v", err)
	}
	secondExit := 0
	if err := store.CompletePluginRun(secondID, now.Add(time.Second), &secondExit, "succeeded", "sha256:second", "", ""); err != nil {
		t.Fatalf("complete second run: %v", err)
	}
	otherID, err := store.RecordPluginRun(PluginRunRecord{
		Plugin:      "other",
		TriggerType: "manual",
		StartedAt:   now.Add(time.Minute),
		Status:      "running",
	})
	if err != nil {
		t.Fatalf("record other run: %v", err)
	}
	if err := store.CompletePluginRun(otherID, now.Add(time.Minute+time.Second), nil, "failed", "", "", "validate failed"); err != nil {
		t.Fatalf("complete other run: %v", err)
	}

	cloudRuns, err := store.ListPluginRuns("cloud")
	if err != nil {
		t.Fatalf("list cloud plugin runs: %v", err)
	}
	if len(cloudRuns) != 2 {
		t.Fatalf("cloud runs len = %d, want 2: %+v", len(cloudRuns), cloudRuns)
	}
	if cloudRuns[0].ID != secondID || cloudRuns[1].ID != firstID {
		t.Fatalf("cloud run ordering = %+v, want newest first", cloudRuns)
	}
	if cloudRuns[0].Status != "succeeded" || !cloudRuns[0].HasExitCode || cloudRuns[0].ExitCode != 0 || cloudRuns[0].TriggerTopic != "routerd.test" {
		t.Fatalf("latest cloud run = %+v", cloudRuns[0])
	}
	if cloudRuns[1].Status != "failed" || cloudRuns[1].Error != "exit 1" || cloudRuns[1].Stderr != "boom" {
		t.Fatalf("older cloud run = %+v", cloudRuns[1])
	}

	allRuns, err := store.ListPluginRuns("")
	if err != nil {
		t.Fatalf("list all plugin runs: %v", err)
	}
	if len(allRuns) != 3 || allRuns[0].Plugin != "other" || allRuns[1].ID != secondID {
		t.Fatalf("all runs = %+v, want newest-first across plugins", allRuns)
	}
}

func TestSQLiteStoreClosedAccessIsBenign(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	generation, err := store.BeginGeneration("abc123")
	if err != nil {
		t.Fatalf("begin generation: %v", err)
	}
	if err := store.RecordGenerationConfig(generation, "kind: Router\nmetadata:\n  name: lab\n"); err != nil {
		t.Fatalf("record generation config: %v", err)
	}
	if err := store.SaveObjectStatus("net.routerd.net/v1alpha1", "Interface", "wan", map[string]any{"state": "ready"}); err != nil {
		t.Fatalf("save object status before close: %v", err)
	}
	if got := store.ObjectStatus("net.routerd.net/v1alpha1", "Interface", "wan"); got["state"] != "ready" {
		t.Fatalf("object status before close = %+v", got)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second close sqlite store: %v", err)
	}
	if err := store.SaveObjectStatus("net.routerd.net/v1alpha1", "Interface", "wan", map[string]any{"state": "stopping"}); err != nil {
		t.Fatalf("save object status after close: %v", err)
	}
	if got := store.ObjectStatus("net.routerd.net/v1alpha1", "Interface", "wan"); len(got) != 0 {
		t.Fatalf("object status after close = %+v, want empty", got)
	}
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		t.Fatalf("list object statuses after close: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("object statuses after close = %+v, want empty", statuses)
	}
	configYAML, ok, err := store.GenerationConfig(generation)
	if err != nil {
		t.Fatalf("generation config after close: %v", err)
	}
	if ok || configYAML != "" {
		t.Fatalf("generation config after close ok=%t yaml=%q, want empty", ok, configYAML)
	}
}
