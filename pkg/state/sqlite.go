package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"routerd/pkg/daemonapi"
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
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
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

func (s *SQLiteStore) init() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS generations (
  generation INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  phase TEXT,
  warnings TEXT,
  config_hash TEXT
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
	if err := s.ensureArtifactsTable(); err != nil {
		return err
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
	if value == "" {
		return s.Unset(name, reason)
	}
	return s.set(name, StatusSet, value, reason)
}

func (s *SQLiteStore) Unset(name, reason string) Value { return s.set(name, StatusUnset, "", reason) }
func (s *SQLiteStore) Forget(name, reason string) Value {
	return s.set(name, StatusUnknown, "", reason)
}

func (s *SQLiteStore) Delete(name string) {
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

func (s *SQLiteStore) set(name, status, value, reason string) Value {
	now := s.now().UTC()
	current := s.Get(name)
	since := current.Since
	if current.Status != status || current.Value != value || since.IsZero() {
		since = now
	}
	next := Value{Status: status, Value: value, Reason: reason, Since: since, UpdatedAt: now}
	_ = s.put(name, next)
	return next
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

func (s *SQLiteStore) BeginGeneration(configHash string) (int64, error) {
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

func (s *SQLiteStore) FinishGeneration(generation int64, phase string, warnings []string) error {
	if generation == 0 {
		generation = s.generation
	}
	data, _ := json.Marshal(warnings)
	_, err := s.db.Exec(`UPDATE generations SET finished_at = ?, phase = ?, warnings = ? WHERE generation = ?`, s.now().UTC().Format(time.RFC3339Nano), phase, string(data), generation)
	return err
}

func (s *SQLiteStore) CurrentGeneration() int64 { return s.generation }

func (s *SQLiteStore) ObjectGeneration(apiVersion, kind, name string) int64 {
	var generation sql.NullInt64
	err := s.db.QueryRow(`SELECT observed_generation FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name).Scan(&generation)
	if err != nil || !generation.Valid {
		return 0
	}
	return generation.Int64
}

func (s *SQLiteStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	return s.saveStatus(objectRef{APIVersion: apiVersion, Kind: kind, Name: name}, objectStatus(status))
}

func (s *SQLiteStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
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

func (s *SQLiteStore) DeleteObject(apiVersion, kind, name string) error {
	_, err := s.db.Exec(`DELETE FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name)
	return err
}

func (s *SQLiteStore) SaveObjectApplySource(apiVersion, kind, name, path string) error {
	now := s.now().UTC().Format(time.RFC3339Nano)
	uid := apiVersion + "/" + kind + "/" + name
	_, err := s.db.Exec(`INSERT INTO objects(api_version,kind,name,uid,resource_version,last_applied_path,status,created_at,modified_at)
VALUES(?,?,?,?,1,?,'{}',?,?)
ON CONFLICT(api_version,kind,name) DO UPDATE SET last_applied_path=excluded.last_applied_path,modified_at=excluded.modified_at`,
		apiVersion, kind, name, uid, path, now, now)
	return err
}

func (s *SQLiteStore) ObjectApplySource(apiVersion, kind, name string) string {
	var path sql.NullString
	err := s.db.QueryRow(`SELECT last_applied_path FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name).Scan(&path)
	if err != nil || !path.Valid {
		return ""
	}
	return path.String
}

func (s *SQLiteStore) RecordEvent(apiVersion, kind, name, eventType, reason, message string) error {
	_, err := s.db.Exec(`INSERT INTO events(api_version,kind,name,type,reason,message,generation,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		apiVersion, kind, name, eventType, reason, message, nullGeneration(s.generation), s.now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) RecordBusEvent(_ context.Context, event daemonapi.DaemonEvent) (string, error) {
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

func (s *SQLiteStore) Close() error { return s.db.Close() }

func JSONValue(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func objectRefForKey(key string) objectRef {
	if rest, ok := strings.CutPrefix(key, "ipv6PrefixDelegation."); ok {
		name, field, ok := strings.Cut(rest, ".")
		if ok && name != "" && field != "" {
			return objectRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "IPv6PrefixDelegation", Name: name, Field: field}
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
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "IPv6PrefixDelegation" {
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
