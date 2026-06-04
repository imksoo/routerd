// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

const (
	stateAPIVersion = "state.routerd.net/v1alpha1"
	stateKind       = "StateVariable"
)

type SQLiteStore struct {
	path       string
	db         *sql.DB
	now        func() time.Time
	generation int64
	closed     bool
	mu         sync.RWMutex
}

type objectRef struct {
	APIVersion string
	Kind       string
	Name       string
	Field      string
}

type objectStatus map[string]any

func OpenSQLite(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000; PRAGMA journal_mode = WAL;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &SQLiteStore{path: path, db: db, now: time.Now}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrateLegacyJSON(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func OpenSQLiteReadOnly(path string) (*SQLiteStore, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteStore{path: path, db: db, now: time.Now}, nil
}

func (s *SQLiteStore) init() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS generations (
  generation INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  phase TEXT,
  warnings TEXT,
  config_hash TEXT,
  config_yaml TEXT
);
CREATE TABLE IF NOT EXISTS objects (
  api_version TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  uid TEXT,
  resource_version INTEGER NOT NULL DEFAULT 1,
  observed_generation INTEGER,
  last_applied_path TEXT,
  status TEXT,
  created_at TEXT NOT NULL,
  modified_at TEXT NOT NULL,
  PRIMARY KEY(api_version, kind, name)
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  api_version TEXT NOT NULL,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  reason TEXT NOT NULL,
  message TEXT NOT NULL,
  generation INTEGER,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_type ON events(type, id);
CREATE TABLE IF NOT EXISTS access_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT,
  user TEXT,
  method TEXT,
  path TEXT,
  status_code INTEGER,
  duration_ms INTEGER,
  generation INTEGER
);
CREATE TABLE IF NOT EXISTS dynamic_config_parts (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL,
  generation INTEGER NOT NULL,
  observed_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  digest TEXT NOT NULL,
  resources_json TEXT,
  directives_json TEXT,
  actionplans_json TEXT,
  status TEXT NOT NULL,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(source, generation)
);
CREATE TABLE IF NOT EXISTS plugin_runs (
  id INTEGER PRIMARY KEY,
  plugin TEXT NOT NULL,
  trigger_type TEXT NOT NULL,
  trigger_topic TEXT,
  started_at TEXT NOT NULL,
  completed_at TEXT,
  exit_code INTEGER,
  status TEXT NOT NULL,
  stdout_digest TEXT,
  stderr TEXT,
  error TEXT
);
CREATE TABLE IF NOT EXISTS federation_events (
  id TEXT PRIMARY KEY,
  group_name TEXT NOT NULL,
  source_node TEXT,
  type TEXT NOT NULL,
  subject TEXT,
  dedupe_key TEXT,
  payload TEXT,
  observed_at INTEGER,
  expires_at INTEGER,
  recorded_at INTEGER
);
CREATE INDEX IF NOT EXISTS federation_events_group ON federation_events(group_name, observed_at);
CREATE TABLE IF NOT EXISTS event_deliveries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL,
  peer TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  last_attempt_at INTEGER,
  last_error TEXT,
  delivered_at INTEGER,
  UNIQUE(event_id, peer)
);
CREATE INDEX IF NOT EXISTS event_deliveries_peer ON event_deliveries(peer, status);
CREATE TABLE IF NOT EXISTS event_subscription_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  subscription TEXT NOT NULL,
  event_id TEXT NOT NULL,
  event_group TEXT NOT NULL,
  plugin TEXT NOT NULL,
  status TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  started_at TEXT NOT NULL,
  completed_at TEXT,
  dynamic_source TEXT,
  dynamic_generation INTEGER,
  error TEXT,
  UNIQUE(subscription, event_id)
);
CREATE INDEX IF NOT EXISTS event_subscription_runs_sub ON event_subscription_runs(subscription, status);
CREATE TABLE IF NOT EXISTS action_executions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  idempotency_key TEXT NOT NULL UNIQUE,
  source TEXT,
  provider TEXT NOT NULL,
  provider_ref TEXT,
  action TEXT NOT NULL,
  target_json TEXT,
  parameters_json TEXT,
  undo_json TEXT,
  risk_level TEXT,
  status TEXT NOT NULL,
  approved_by TEXT,
  approved_at TEXT,
  executed_at TEXT,
  result_message TEXT,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS action_executions_status ON action_executions(status, id);
`); err != nil {
		return err
	}
	if err := s.migrateLegacyStateTable(); err != nil {
		return err
	}
	if err := s.ensureObjectColumns(); err != nil {
		return err
	}
	if err := s.ensureEventColumns(); err != nil {
		return err
	}
	if err := s.ensureGenerationColumns(); err != nil {
		return err
	}
	if err := s.ensureDynamicConfigPartColumns(); err != nil {
		return err
	}
	if err := s.ensureArtifactsTable(); err != nil {
		return err
	}
	if err := s.ensureActionExecutionColumns(); err != nil {
		return err
	}
	return nil
}

// ensureDynamicConfigPartColumns additively adds columns introduced after the
// dynamic_config_parts table shipped, so existing databases upgrade in place.
func (s *SQLiteStore) ensureDynamicConfigPartColumns() error {
	columns := map[string]string{
		// actionplans_json persists plugin-proposed, display-only ActionPlans so
		// EventSubscription-driven runs stay reviewable. routerd never executes
		// them; missing/empty column simply means no action plans.
		"actionplans_json": "TEXT",
	}
	for column, typ := range columns {
		hasColumn, err := s.tableHasColumn("dynamic_config_parts", column)
		if err != nil {
			return err
		}
		if !hasColumn {
			if _, err := s.db.Exec(`ALTER TABLE dynamic_config_parts ADD COLUMN ` + column + ` ` + typ); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLiteStore) ensureGenerationColumns() error {
	columns := map[string]string{
		"config_yaml": "TEXT",
	}
	for column, typ := range columns {
		hasColumn, err := s.tableHasColumn("generations", column)
		if err != nil {
			return err
		}
		if !hasColumn {
			if _, err := s.db.Exec(`ALTER TABLE generations ADD COLUMN ` + column + ` ` + typ); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SQLiteStore) ensureEventColumns() error {
	columns := map[string]string{
		"topic":                "TEXT",
		"source_kind":          "TEXT",
		"source_instance":      "TEXT",
		"resource_api_version": "TEXT",
		"resource_kind":        "TEXT",
		"resource_name":        "TEXT",
		"severity":             "TEXT",
		"attributes":           "TEXT",
	}
	for column, typ := range columns {
		hasColumn, err := s.tableHasColumn("events", column)
		if err != nil {
			return err
		}
		if !hasColumn {
			if _, err := s.db.Exec(`ALTER TABLE events ADD COLUMN ` + column + ` ` + typ); err != nil {
				return err
			}
		}
	}
	_, err := s.db.Exec(`
CREATE INDEX IF NOT EXISTS events_topic ON events(topic, id);
CREATE INDEX IF NOT EXISTS events_resource ON events(resource_kind, resource_name, id);
`)
	return err
}

func (s *SQLiteStore) ensureObjectColumns() error {
	hasLastAppliedPath, err := s.tableHasColumn("objects", "last_applied_path")
	if err != nil {
		return err
	}
	if !hasLastAppliedPath {
		if _, err := s.db.Exec(`ALTER TABLE objects ADD COLUMN last_applied_path TEXT`); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) ensureArtifactsTable() error {
	old, err := s.tableHasColumn("artifacts", "id")
	if err != nil {
		return err
	}
	newTable, err := s.tableHasColumn("artifacts", "artifact_id")
	if err != nil {
		return err
	}
	if old && !newTable {
		if _, err := s.db.Exec(`ALTER TABLE artifacts RENAME TO artifacts_legacy`); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS artifacts (
  artifact_id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  owner_api_version TEXT,
  owner_kind TEXT,
  owner_name TEXT,
  attributes TEXT,
  source TEXT,
  generation INTEGER,
  observed_at TEXT
);
`); err != nil {
		return err
	}
	exists, err := s.tableExists("artifacts_legacy")
	if err != nil || !exists {
		return err
	}
	rows, err := s.db.Query(`SELECT id,kind,name,coalesce(owner,''),coalesce(attributes,'{}'),coalesce(source,''),generation,coalesce(observed_at,'') FROM artifacts_legacy`)
	if err != nil {
		return err
	}
	type legacyArtifact struct {
		id, kind, name, owner, attrs, source, observed string
		generation                                     sql.NullInt64
	}
	var legacyArtifacts []legacyArtifact
	for rows.Next() {
		var item legacyArtifact
		if err := rows.Scan(&item.id, &item.kind, &item.name, &item.owner, &item.attrs, &item.source, &item.generation, &item.observed); err != nil {
			return err
		}
		legacyArtifacts = append(legacyArtifacts, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range legacyArtifacts {
		ownerAPI, ownerKind, ownerName := splitOwner(item.owner)
		if _, err := s.db.Exec(`INSERT INTO artifacts(artifact_id,kind,name,owner_api_version,owner_kind,owner_name,attributes,source,generation,observed_at)
VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(artifact_id) DO UPDATE SET kind=excluded.kind,name=excluded.name,owner_api_version=excluded.owner_api_version,owner_kind=excluded.owner_kind,owner_name=excluded.owner_name,attributes=excluded.attributes,source=excluded.source,generation=excluded.generation,observed_at=excluded.observed_at`,
			item.id, item.kind, item.name, ownerAPI, ownerKind, ownerName, item.attrs, item.source, item.generation, item.observed); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`DROP TABLE artifacts_legacy`)
	return err
}

func (s *SQLiteStore) migrateLegacyStateTable() error {
	exists, err := s.tableExists("state")
	if err != nil || !exists {
		return err
	}
	rows, err := s.db.Query(`SELECT key,coalesce(value,''),status,coalesce(reason,''),since,updated_at FROM state ORDER BY key`)
	if err != nil {
		return err
	}
	type legacyState struct {
		key   string
		value Value
	}
	var legacyRows []legacyState
	for rows.Next() {
		var key, since, updated string
		var value Value
		if err := rows.Scan(&key, &value.Value, &value.Status, &value.Reason, &since, &updated); err != nil {
			return err
		}
		value.Since, _ = time.Parse(time.RFC3339Nano, since)
		value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		legacyRows = append(legacyRows, legacyState{key: key, value: value})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range legacyRows {
		if err := s.put(row.key, row.value); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`DROP TABLE state`)
	return err
}

func (s *SQLiteStore) tableExists(name string) (bool, error) {
	var found string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *SQLiteStore) tableHasColumn(table, column string) (bool, error) {
	exists, err := s.tableExists(table)
	if err != nil || !exists {
		return false, err
	}
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *SQLiteStore) migrateLegacyJSON() error {
	legacy := filepath.Join(filepath.Dir(s.path), "state.json")
	if _, err := os.Stat(legacy); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM objects`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	jsonStore, err := LoadJSON(legacy)
	if err != nil {
		return err
	}
	for key, value := range jsonStore.Variables() {
		if err := s.put(key, value); err != nil {
			return err
		}
	}
	return os.Rename(legacy, legacy+".migrated")
}

func (s *SQLiteStore) Save(path string) error { return nil }

func (s *SQLiteStore) Get(name string) Value {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return Value{}
	}
	ref := objectRefForKey(name)
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if err != nil {
		now := s.now().UTC()
		return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
	}
	if value, ok := valueFromStatus(status, ref.Field); ok {
		return value
	}
	now := s.now().UTC()
	return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
}

func (s *SQLiteStore) Set(name, value, reason string) Value {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Value{}
	}
	if value == "" {
		return s.setLocked(name, StatusUnset, "", reason)
	}
	return s.setLocked(name, StatusSet, value, reason)
}

func (s *SQLiteStore) Unset(name, reason string) Value {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Value{}
	}
	return s.setLocked(name, StatusUnset, "", reason)
}

func (s *SQLiteStore) Forget(name, reason string) Value {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Value{}
	}
	return s.setLocked(name, StatusUnknown, "", reason)
}

func (s *SQLiteStore) Delete(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	ref := objectRefForKey(name)
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if err != nil {
		return
	}
	deleteStatusField(status, ref.Field)
	if len(status) == 0 {
		_, _ = s.db.Exec(`DELETE FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, ref.APIVersion, ref.Kind, ref.Name)
		return
	}
	_ = s.saveStatus(ref, status)
}

func (s *SQLiteStore) setLocked(name, status, value, reason string) Value {
	now := s.now().UTC()
	current := s.getLocked(name)
	since := current.Since
	if current.Status != status || current.Value != value || since.IsZero() {
		since = now
	}
	next := Value{Status: status, Value: value, Reason: reason, Since: since, UpdatedAt: now}
	_ = s.put(name, next)
	return next
}

func (s *SQLiteStore) getLocked(name string) Value {
	ref := objectRefForKey(name)
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if err != nil {
		now := s.now().UTC()
		return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
	}
	if value, ok := valueFromStatus(status, ref.Field); ok {
		return value
	}
	now := s.now().UTC()
	return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
}

func (s *SQLiteStore) put(name string, value Value) error {
	if value.Since.IsZero() {
		value.Since = s.now().UTC()
	}
	if value.UpdatedAt.IsZero() {
		value.UpdatedAt = s.now().UTC()
	}
	ref := objectRefForKey(name)
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if err != nil {
		status = objectStatus{}
	}
	setStatusField(status, ref.Field, value)
	return s.saveStatus(ref, status)
}

func (s *SQLiteStore) loadStatus(apiVersion, kind, name string) (objectStatus, int64, error) {
	var raw string
	var generation sql.NullInt64
	err := s.db.QueryRow(`SELECT coalesce(status,'{}'), observed_generation FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name).Scan(&raw, &generation)
	if err != nil {
		return nil, 0, err
	}
	status := objectStatus{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &status)
	}
	return status, generation.Int64, nil
}

func (s *SQLiteStore) saveStatus(ref objectRef, status objectStatus) error {
	now := s.now().UTC().Format(time.RFC3339Nano)
	data, _ := json.Marshal(status)
	uid := ref.APIVersion + "/" + ref.Kind + "/" + ref.Name
	_, err := s.db.Exec(`INSERT INTO objects(api_version,kind,name,uid,resource_version,observed_generation,status,created_at,modified_at)
VALUES(?,?,?,?,1,?,?,?,?)
ON CONFLICT(api_version,kind,name) DO UPDATE SET resource_version=resource_version+1,observed_generation=excluded.observed_generation,status=excluded.status,modified_at=excluded.modified_at`,
		ref.APIVersion, ref.Kind, ref.Name, uid, nullGeneration(s.generation), string(data), now, now)
	return err
}

func nullGeneration(generation int64) any {
	if generation == 0 {
		return nil
	}
	return generation
}

func (s *SQLiteStore) Age(name string) time.Duration { return s.now().UTC().Sub(s.Get(name).Since) }
func (s *SQLiteStore) Now() time.Time                { return s.now().UTC() }

func (s *SQLiteStore) Variables() map[string]Value {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return map[string]Value{}
	}
	rows, err := s.db.Query(`SELECT api_version,kind,name,coalesce(status,'{}') FROM objects ORDER BY api_version,kind,name`)
	if err != nil {
		return map[string]Value{}
	}
	defer rows.Close()
	out := map[string]Value{}
	for rows.Next() {
		var apiVersion, kind, name, raw string
		if err := rows.Scan(&apiVersion, &kind, &name, &raw); err != nil {
			continue
		}
		status := objectStatus{}
		_ = json.Unmarshal([]byte(raw), &status)
		for key, value := range variablesFromObject(apiVersion, kind, name, status) {
			out[key] = value
		}
	}
	return out
}

type DynamicConfigPartRecord struct {
	ID             int64     `json:"id" yaml:"id"`
	Source         string    `json:"source" yaml:"source"`
	Generation     int64     `json:"generation" yaml:"generation"`
	ObservedAt     time.Time `json:"observedAt" yaml:"observedAt"`
	ExpiresAt      time.Time `json:"expiresAt" yaml:"expiresAt"`
	Digest         string    `json:"digest" yaml:"digest"`
	ResourcesJSON  string    `json:"resourcesJson,omitempty" yaml:"resourcesJson,omitempty"`
	DirectivesJSON string    `json:"directivesJson,omitempty" yaml:"directivesJson,omitempty"`
	// ActionPlansJSON holds the JSON-encoded display-only ActionPlans. routerd
	// never executes these; empty means none.
	ActionPlansJSON string `json:"actionPlansJson,omitempty" yaml:"actionPlansJson,omitempty"`
	// Status is the writer-provided state stored in SQLite. Readers should call
	// EffectiveStatus so active records age into expired without a rewrite.
	Status    string    `json:"status" yaml:"status"`
	Error     string    `json:"error,omitempty" yaml:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt" yaml:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt" yaml:"updatedAt"`
}

func (r DynamicConfigPartRecord) EffectiveStatus(now time.Time) string {
	if !r.ExpiresAt.IsZero() && !now.UTC().Before(r.ExpiresAt.UTC()) {
		return "expired"
	}
	return "active"
}

type PluginRunRecord struct {
	ID           int64     `json:"id" yaml:"id"`
	Plugin       string    `json:"plugin" yaml:"plugin"`
	TriggerType  string    `json:"triggerType" yaml:"triggerType"`
	TriggerTopic string    `json:"triggerTopic,omitempty" yaml:"triggerTopic,omitempty"`
	StartedAt    time.Time `json:"startedAt" yaml:"startedAt"`
	CompletedAt  time.Time `json:"completedAt,omitempty" yaml:"completedAt,omitempty"`
	ExitCode     int       `json:"exitCode,omitempty" yaml:"exitCode,omitempty"`
	HasExitCode  bool      `json:"hasExitCode,omitempty" yaml:"hasExitCode,omitempty"`
	Status       string    `json:"status" yaml:"status"`
	StdoutDigest string    `json:"stdoutDigest,omitempty" yaml:"stdoutDigest,omitempty"`
	Stderr       string    `json:"stderr,omitempty" yaml:"stderr,omitempty"`
	Error        string    `json:"error,omitempty" yaml:"error,omitempty"`
}

func (s *SQLiteStore) UpsertDynamicConfigPart(part DynamicConfigPartRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	now := s.now().UTC()
	if part.ObservedAt.IsZero() {
		part.ObservedAt = now
	}
	if part.CreatedAt.IsZero() {
		part.CreatedAt = now
	}
	if part.UpdatedAt.IsZero() {
		part.UpdatedAt = now
	}
	_, err := s.db.Exec(`INSERT INTO dynamic_config_parts(source,generation,observed_at,expires_at,digest,resources_json,directives_json,actionplans_json,status,error,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(source,generation) DO UPDATE SET observed_at=excluded.observed_at,expires_at=excluded.expires_at,digest=excluded.digest,resources_json=excluded.resources_json,directives_json=excluded.directives_json,actionplans_json=excluded.actionplans_json,status=excluded.status,error=excluded.error,updated_at=excluded.updated_at`,
		part.Source, part.Generation, formatStateTime(part.ObservedAt), formatStateTime(part.ExpiresAt), part.Digest, nullableString(part.ResourcesJSON), nullableString(part.DirectivesJSON), nullableString(part.ActionPlansJSON), part.Status, nullableString(part.Error), formatStateTime(part.CreatedAt), formatStateTime(part.UpdatedAt))
	return err
}

func (s *SQLiteStore) ListDynamicConfigParts() ([]DynamicConfigPartRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []DynamicConfigPartRecord{}, nil
	}
	rows, err := s.db.Query(`SELECT id,source,generation,observed_at,expires_at,digest,coalesce(resources_json,''),coalesce(directives_json,''),coalesce(actionplans_json,''),status,coalesce(error,''),created_at,updated_at FROM dynamic_config_parts ORDER BY observed_at DESC,generation DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDynamicConfigPartRecords(rows)
}

func (s *SQLiteStore) GetDynamicConfigPartsBySource(source string) ([]DynamicConfigPartRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []DynamicConfigPartRecord{}, nil
	}
	rows, err := s.db.Query(`SELECT id,source,generation,observed_at,expires_at,digest,coalesce(resources_json,''),coalesce(directives_json,''),coalesce(actionplans_json,''),status,coalesce(error,''),created_at,updated_at FROM dynamic_config_parts WHERE source = ? ORDER BY generation DESC,observed_at DESC,id DESC`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDynamicConfigPartRecords(rows)
}

func scanDynamicConfigPartRecords(rows *sql.Rows) ([]DynamicConfigPartRecord, error) {
	var out []DynamicConfigPartRecord
	for rows.Next() {
		var rec DynamicConfigPartRecord
		var observed, expires, created, updated string
		if err := rows.Scan(&rec.ID, &rec.Source, &rec.Generation, &observed, &expires, &rec.Digest, &rec.ResourcesJSON, &rec.DirectivesJSON, &rec.ActionPlansJSON, &rec.Status, &rec.Error, &created, &updated); err != nil {
			return nil, err
		}
		rec.ObservedAt = parseStateTime(observed)
		rec.ExpiresAt = parseStateTime(expires)
		rec.CreatedAt = parseStateTime(created)
		rec.UpdatedAt = parseStateTime(updated)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteStore) RecordPluginRun(run PluginRunRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, nil
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = s.now().UTC()
	}
	result, err := s.db.Exec(`INSERT INTO plugin_runs(plugin,trigger_type,trigger_topic,started_at,completed_at,exit_code,status,stdout_digest,stderr,error)
VALUES(?,?,?,?,?,?,?,?,?,?)`,
		run.Plugin, run.TriggerType, nullableString(run.TriggerTopic), formatStateTime(run.StartedAt), nullableStateTime(run.CompletedAt), nullableExitCode(run), run.Status, nullableString(run.StdoutDigest), nullableString(run.Stderr), nullableString(run.Error))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *SQLiteStore) CompletePluginRun(id int64, completedAt time.Time, exitCode *int, status, stdoutDigest, stderrText, runError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if completedAt.IsZero() {
		completedAt = s.now().UTC()
	}
	var exit any
	if exitCode != nil {
		exit = *exitCode
	}
	_, err := s.db.Exec(`UPDATE plugin_runs SET completed_at = ?, exit_code = ?, status = ?, stdout_digest = ?, stderr = ?, error = ? WHERE id = ?`,
		formatStateTime(completedAt), exit, status, nullableString(stdoutDigest), nullableString(stderrText), nullableString(runError), id)
	return err
}

func (s *SQLiteStore) ListPluginRuns(plugin string) ([]PluginRunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []PluginRunRecord{}, nil
	}
	query := `SELECT id,plugin,trigger_type,coalesce(trigger_topic,''),started_at,coalesce(completed_at,''),exit_code,status,coalesce(stdout_digest,''),coalesce(stderr,''),coalesce(error,'') FROM plugin_runs`
	var args []any
	if strings.TrimSpace(plugin) != "" {
		query += ` WHERE plugin = ?`
		args = append(args, plugin)
	}
	query += ` ORDER BY started_at DESC,id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PluginRunRecord
	for rows.Next() {
		var rec PluginRunRecord
		var started, completed string
		var exit sql.NullInt64
		if err := rows.Scan(&rec.ID, &rec.Plugin, &rec.TriggerType, &rec.TriggerTopic, &started, &completed, &exit, &rec.Status, &rec.StdoutDigest, &rec.Stderr, &rec.Error); err != nil {
			return nil, err
		}
		rec.StartedAt = parseStateTime(started)
		rec.CompletedAt = parseStateTime(completed)
		if exit.Valid {
			rec.ExitCode = int(exit.Int64)
			rec.HasExitCode = true
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func formatStateTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func nullableStateTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return formatStateTime(t)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableExitCode(run PluginRunRecord) any {
	if !run.HasExitCode {
		return nil
	}
	return run.ExitCode
}

func parseStateTime(value string) time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed
	}
	return time.Time{}
}

func (s *SQLiteStore) BeginGeneration(configHash string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, nil
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`INSERT INTO generations(started_at,warnings,config_hash) VALUES(?,'[]',?)`, now, configHash)
	if err != nil {
		return 0, err
	}
	generation, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	s.generation = generation
	return generation, nil
}

func (s *SQLiteStore) RecordGenerationConfig(generation int64, configYAML string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if generation == 0 {
		generation = s.generation
	}
	if generation == 0 {
		return nil
	}
	_, err := s.db.Exec(`UPDATE generations SET config_yaml = ? WHERE generation = ?`, configYAML, generation)
	return err
}

func (s *SQLiteStore) FinishGeneration(generation int64, phase string, warnings []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if generation == 0 {
		generation = s.generation
	}
	data, _ := json.Marshal(warnings)
	_, err := s.db.Exec(`UPDATE generations SET finished_at = ?, phase = ?, warnings = ? WHERE generation = ?`, s.now().UTC().Format(time.RFC3339Nano), phase, string(data), generation)
	return err
}

func (s *SQLiteStore) CurrentGeneration() int64 { return s.generation }

func (s *SQLiteStore) LatestGeneration() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0
	}
	var generation sql.NullInt64
	err := s.db.QueryRow(`SELECT max(generation) FROM generations`).Scan(&generation)
	if err != nil || !generation.Valid {
		return 0
	}
	return generation.Int64
}

func (s *SQLiteStore) ListGenerations(limit int) ([]GenerationRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []GenerationRecord{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT generation,started_at,coalesce(finished_at,''),coalesce(phase,''),coalesce(config_hash,''),config_yaml IS NOT NULL FROM generations ORDER BY generation DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenerationRecord
	for rows.Next() {
		var rec GenerationRecord
		var started, finished string
		if err := rows.Scan(&rec.Generation, &started, &finished, &rec.Phase, &rec.ConfigHash, &rec.HasYAML); err != nil {
			return nil, err
		}
		if parsed, err := time.Parse(time.RFC3339Nano, started); err == nil {
			rec.StartedAt = parsed
		}
		if finished != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, finished); err == nil {
				rec.FinishedAt = parsed
			}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GenerationConfig(generation int64) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", false, nil
	}
	var value sql.NullString
	err := s.db.QueryRow(`SELECT config_yaml FROM generations WHERE generation = ?`, generation).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !value.Valid {
		return "", false, nil
	}
	return value.String, true, nil
}

func (s *SQLiteStore) ObjectGeneration(apiVersion, kind, name string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0
	}
	var generation sql.NullInt64
	err := s.db.QueryRow(`SELECT observed_generation FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name).Scan(&generation)
	if err != nil || !generation.Valid {
		return 0
	}
	return generation.Int64
}

func (s *SQLiteStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.saveStatus(objectRef{APIVersion: apiVersion, Kind: kind, Name: name}, objectStatus(status))
}

func (s *SQLiteStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return map[string]any{}
	}
	status, _, err := s.loadStatus(apiVersion, kind, name)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range status {
		out[key] = value
	}
	return out
}

func (s *SQLiteStore) ListObjectStatuses() ([]ObjectStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []ObjectStatus{}, nil
	}
	rows, err := s.db.Query(`SELECT api_version,kind,name,coalesce(status,'{}') FROM objects ORDER BY api_version,kind,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ObjectStatus
	for rows.Next() {
		var item ObjectStatus
		var raw string
		if err := rows.Scan(&item.APIVersion, &item.Kind, &item.Name, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Status); err != nil {
			item.Status = map[string]any{"error": err.Error()}
		}
		item.Owner = statusString(item.Status, "owner")
		item.ManagedBy = statusString(item.Status, "managedBy")
		item.Management = statusString(item.Status, "management")
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func statusString(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	value, ok := status[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (s *SQLiteStore) DeleteObject(apiVersion, kind, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name)
	return err
}

func (s *SQLiteStore) Backup(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	escaped := strings.ReplaceAll(path, `'`, `''`)
	_, err := s.db.Exec(`VACUUM INTO '` + escaped + `'`)
	return err
}

func (s *SQLiteStore) SaveObjectApplySource(apiVersion, kind, name, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	uid := apiVersion + "/" + kind + "/" + name
	_, err := s.db.Exec(`INSERT INTO objects(api_version,kind,name,uid,resource_version,last_applied_path,status,created_at,modified_at)
VALUES(?,?,?,?,1,?,'{}',?,?)
ON CONFLICT(api_version,kind,name) DO UPDATE SET last_applied_path=excluded.last_applied_path,modified_at=excluded.modified_at`,
		apiVersion, kind, name, uid, path, now, now)
	return err
}

func (s *SQLiteStore) ObjectApplySource(apiVersion, kind, name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return ""
	}
	var path sql.NullString
	err := s.db.QueryRow(`SELECT last_applied_path FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name).Scan(&path)
	if err != nil || !path.Valid {
		return ""
	}
	return path.String
}

func (s *SQLiteStore) IntegrityCheck() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return "", nil
	}
	var result string
	if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return "", err
	}
	return result, nil
}

func (s *SQLiteStore) Vacuum() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	_, err := s.db.Exec(`VACUUM`)
	return err
}

func (s *SQLiteStore) BackupTo(dest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup destination %s already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err := s.db.Exec(`VACUUM INTO ?`, dest)
	return err
}

func (s *SQLiteStore) CountEventsOlderThan(cutoff time.Time) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, nil
	}
	var count int64
	err := s.db.QueryRow(`SELECT count(*) FROM events WHERE created_at < ?`, cutoff.UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count, err
}

func (s *SQLiteStore) PruneEventsOlderThan(cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, nil
	}
	result, err := s.db.Exec(`DELETE FROM events WHERE created_at < ?`, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLiteStore) RecordEvent(apiVersion, kind, name, eventType, reason, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO events(api_version,kind,name,type,reason,message,generation,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		apiVersion, kind, name, eventType, reason, message, nullGeneration(s.generation), s.now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) RecordBusEvent(_ context.Context, event daemonapi.DaemonEvent) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", nil
	}
	apiVersion := event.APIVersion
	kind := event.Kind
	name := event.Daemon.Name
	resourceAPI := ""
	resourceKind := ""
	resourceName := ""
	if event.Resource != nil {
		apiVersion = event.Resource.APIVersion
		kind = event.Resource.Kind
		name = event.Resource.Name
		resourceAPI = event.Resource.APIVersion
		resourceKind = event.Resource.Kind
		resourceName = event.Resource.Name
	}
	attrs, _ := json.Marshal(event.Attributes)
	result, err := s.db.Exec(`INSERT INTO events(api_version,kind,name,type,reason,message,generation,created_at,topic,source_kind,source_instance,resource_api_version,resource_kind,resource_name,severity,attributes)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		apiVersion, kind, name, event.Type, event.Reason, event.Message, nullGeneration(s.generation), s.now().UTC().Format(time.RFC3339Nano),
		event.Type, event.Daemon.Kind, event.Daemon.Instance, resourceAPI, resourceKind, resourceName, event.Severity, string(attrs))
	if err != nil {
		return "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}

func (s *SQLiteStore) Events(apiVersion, kind, name string, limit int) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []Event{}
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id,api_version,kind,name,type,reason,message,coalesce(generation,0),created_at FROM events WHERE api_version = ? AND kind = ? AND name = ? ORDER BY id DESC LIMIT ?`, apiVersion, kind, name, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		var created string
		if err := rows.Scan(&event.ID, &event.APIVersion, &event.Kind, &event.Name, &event.Type, &event.Reason, &event.Message, &event.Generation, &created); err != nil {
			continue
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		events = append(events, event)
	}
	return events
}

func (s *SQLiteStore) ListEvents(query EventQuery) ([]StoredEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []StoredEvent{}, nil
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	var clauses []string
	var args []any
	if query.SinceID > 0 {
		clauses = append(clauses, "id > ?")
		args = append(args, query.SinceID)
	}
	if query.Topic != "" {
		clauses = append(clauses, "coalesce(topic,type) = ?")
		args = append(args, query.Topic)
	}
	if query.Kind != "" {
		clauses = append(clauses, "kind = ?")
		args = append(args, query.Kind)
	}
	if query.Name != "" {
		clauses = append(clauses, "name = ?")
		args = append(args, query.Name)
	}
	if query.Resource != "" {
		kind, name, ok := strings.Cut(query.Resource, "/")
		if !ok || kind == "" || name == "" {
			return nil, fmt.Errorf("resource must be <kind>/<name>")
		}
		clauses = append(clauses, "resource_kind = ? AND resource_name = ?")
		args = append(args, kind, name)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT id,api_version,kind,name,type,reason,message,coalesce(generation,0),created_at,
coalesce(topic,''),coalesce(source_kind,''),coalesce(source_instance,''),coalesce(resource_api_version,''),coalesce(resource_kind,''),coalesce(resource_name,''),coalesce(severity,''),coalesce(attributes,'')
FROM events`+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []StoredEvent
	for rows.Next() {
		var event StoredEvent
		var created string
		var attributes string
		if err := rows.Scan(&event.ID, &event.APIVersion, &event.Kind, &event.Name, &event.Type, &event.Reason, &event.Message, &event.Generation, &created, &event.Topic, &event.SourceKind, &event.SourceInstance, &event.ResourceAPIVersion, &event.ResourceKind, &event.ResourceName, &event.Severity, &attributes); err != nil {
			return nil, err
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if attributes != "" && attributes != "null" {
			var decoded map[string]any
			if err := json.Unmarshal([]byte(attributes), &decoded); err == nil && len(decoded) > 0 {
				event.Attributes = decoded
			}
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

func JSONValue(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func objectRefForKey(key string) objectRef {
	if rest, ok := strings.CutPrefix(key, "ipv6PrefixDelegation."); ok {
		name, field, ok := strings.Cut(rest, ".")
		if ok && name != "" && field != "" {
			return objectRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv6PrefixDelegation", Name: name, Field: field}
		}
	}
	return objectRef{APIVersion: stateAPIVersion, Kind: stateKind, Name: key, Field: "value"}
}

func valueFromStatus(status objectStatus, field string) (Value, bool) {
	if field == "lease" {
		vars, _ := status["_variables"].(map[string]any)
		if raw, ok := vars["lease"]; ok {
			return decodeValue(raw)
		}
		lease := objectStatus{}
		for key, value := range status {
			if key != "_variables" {
				lease[key] = value
			}
		}
		if len(lease) == 0 {
			return Value{}, false
		}
		data, _ := json.Marshal(lease)
		now := time.Now().UTC()
		return Value{Status: StatusSet, Value: string(data), Since: now, UpdatedAt: now}, true
	}
	if field == "value" {
		return decodeValue(status["value"])
	}
	vars, _ := status["_variables"].(map[string]any)
	return decodeValue(vars[field])
}

func setStatusField(status objectStatus, field string, value Value) {
	if field == "lease" {
		var lease map[string]any
		if strings.TrimSpace(value.Value) != "" {
			_ = json.Unmarshal([]byte(value.Value), &lease)
		}
		vars := variablesMap(status)
		vars["lease"] = encodeValueMap(value)
		for key := range status {
			if key != "_variables" {
				delete(status, key)
			}
		}
		for key, item := range lease {
			status[key] = item
		}
		if len(vars) > 0 {
			status["_variables"] = vars
		}
		return
	}
	if field == "value" {
		status["value"] = encodeValueMap(value)
		return
	}
	vars := variablesMap(status)
	vars[field] = encodeValueMap(value)
	status["_variables"] = vars
}

func deleteStatusField(status objectStatus, field string) {
	if field == "lease" {
		vars := variablesMap(status)
		delete(vars, "lease")
		for key := range status {
			if key != "_variables" {
				delete(status, key)
			}
		}
		if len(vars) == 0 {
			delete(status, "_variables")
		} else {
			status["_variables"] = vars
		}
		return
	}
	if field == "value" {
		delete(status, "value")
		return
	}
	vars := variablesMap(status)
	delete(vars, field)
	if len(vars) == 0 {
		delete(status, "_variables")
	} else {
		status["_variables"] = vars
	}
}

func variablesMap(status objectStatus) map[string]any {
	if vars, ok := status["_variables"].(map[string]any); ok {
		return vars
	}
	vars := map[string]any{}
	if raw, ok := status["_variables"]; ok {
		data, _ := json.Marshal(raw)
		_ = json.Unmarshal(data, &vars)
	}
	return vars
}

func encodeValueMap(value Value) map[string]any {
	return map[string]any{
		"status":    value.Status,
		"value":     value.Value,
		"reason":    value.Reason,
		"since":     value.Since.Format(time.RFC3339Nano),
		"updatedAt": value.UpdatedAt.Format(time.RFC3339Nano),
	}
}

func decodeValue(raw any) (Value, bool) {
	if raw == nil {
		return Value{}, false
	}
	data, _ := json.Marshal(raw)
	var decoded struct {
		Status    string `json:"status"`
		Value     string `json:"value"`
		Reason    string `json:"reason"`
		Since     string `json:"since"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Status == "" {
		return Value{}, false
	}
	value := Value{Status: decoded.Status, Value: decoded.Value, Reason: decoded.Reason}
	value.Since, _ = time.Parse(time.RFC3339Nano, decoded.Since)
	value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, decoded.UpdatedAt)
	return value, true
}

func variablesFromObject(apiVersion, kind, name string, status objectStatus) map[string]Value {
	out := map[string]Value{}
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "DHCPv6PrefixDelegation" {
		if value, ok := valueFromStatus(status, "lease"); ok {
			out["ipv6PrefixDelegation."+name+".lease"] = value
		}
		vars := variablesMap(status)
		for field, raw := range vars {
			if field == "lease" {
				continue
			}
			if value, ok := decodeValue(raw); ok {
				out["ipv6PrefixDelegation."+name+"."+field] = value
			}
		}
		return out
	}
	if apiVersion == stateAPIVersion && kind == stateKind {
		if value, ok := valueFromStatus(status, "value"); ok {
			out[name] = value
		}
	}
	return out
}

func splitOwner(owner string) (string, string, string) {
	parts := strings.Split(owner, "/")
	if len(parts) < 3 {
		return "", "", ""
	}
	apiVersion := strings.Join(parts[:len(parts)-2], "/")
	return apiVersion, parts[len(parts)-2], parts[len(parts)-1]
}

func DebugObjects(db *sql.DB) string {
	rows, err := db.Query(`SELECT api_version,kind,name,status FROM objects ORDER BY api_version,kind,name`)
	if err != nil {
		return err.Error()
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var apiVersion, kind, name, status string
		_ = rows.Scan(&apiVersion, &kind, &name, &status)
		lines = append(lines, fmt.Sprintf("%s %s %s %s", apiVersion, kind, name, status))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
