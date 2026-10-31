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
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/internal/storage"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

const (
	stateAPIVersion = "state.routerd.net/v1alpha1"
	stateKind       = "StateVariable"
)

const (
	stateWALAutoCheckpointPages = 4096
	stateJournalSizeLimitBytes  = 64 * 1024 * 1024
)

type SQLiteStore struct {
	path                     string
	db                       *sql.DB
	now                      func() time.Time
	generation               int64
	eventJournalRows         int64
	eventJournalPayloadBytes int64
	eventJournalMaxRows      int64
	eventJournalMaxBytes     int64
	eventJournalLastAgePrune time.Time
	eventJournalDataVersion  int64
	storageAlert             *StorageAlert
	storageAlertHandler      func(StorageAlert)
	statusWriteCount         uint64
	statusSkipCount          uint64
	statusKindCounter        map[string]StatusKindWriteStats
	closed                   bool
	mu                       sync.RWMutex
}

type StatusKindWriteStats struct {
	Writes uint64 `json:"writes" yaml:"writes"`
	Skips  uint64 `json:"skips" yaml:"skips"`
}

type objectRef struct {
	APIVersion string
	Kind       string
	Name       string
}

type objectStatus map[string]any

func OpenSQLite(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	// Read-modify-write transactions acquire the writer before reading their
	// premise. A deferred WAL snapshot cannot upgrade after another connection
	// commits, even with busy_timeout. The driver keeps ReadOnly snapshots
	// deferred, so topology and checkpoint GETs still require no writer lock.
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
PRAGMA busy_timeout = 5000;
PRAGMA auto_vacuum = INCREMENTAL;
PRAGMA journal_mode = WAL;
PRAGMA wal_autocheckpoint = 4096;
PRAGMA journal_size_limit = 67108864;
PRAGMA wal_checkpoint(TRUNCATE);
`); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &SQLiteStore{
		path: path, db: db, now: time.Now,
		eventJournalMaxRows:  defaultEventJournalMaxRows,
		eventJournalMaxBytes: defaultEventJournalMaxPayloadBytes,
	}
	if err := storage.Initialize(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.loadEventJournalUsage(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func OpenSQLiteReadOnly(path string) (*SQLiteStore, error) {
	return openSQLiteReadOnly(path, false)
}

func OpenSQLiteReadOnlyImmutable(path string) (*SQLiteStore, error) {
	return openSQLiteReadOnly(path, true)
}

func openSQLiteReadOnly(path string, immutable bool) (*SQLiteStore, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	if immutable {
		q.Set("immutable", "1")
	}
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
	if err := storage.Check(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteStore{path: path, db: db, now: time.Now}, nil
}

func (s *SQLiteStore) Get(name string) Value {
	value, err := s.ReadVariable(name)
	if err != nil && !errors.Is(err, sql.ErrConnDone) {
		now := s.now().UTC()
		return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
	}
	return value
}

// ReadVariable distinguishes an absent observation from a failed database read.
// Side-effect decisions must use this boundary instead of the display getter.
func (s *SQLiteStore) ReadVariable(name string) (Value, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return Value{}, sql.ErrConnDone
	}
	ref := objectRefForKey(name)
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if errors.Is(err, sql.ErrNoRows) {
		now := s.now().UTC()
		return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}, nil
	}
	if err != nil {
		return Value{}, err
	}
	if value, ok, err := readStoredVariable(status); err != nil {
		return Value{}, err
	} else if ok {
		return value, nil
	}
	now := s.now().UTC()
	return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}, nil
}

func (s *SQLiteStore) Set(name, value, reason string) (Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Value{}, sql.ErrConnDone
	}
	if value == "" {
		return s.setLocked(name, StatusUnset, "", reason)
	}
	return s.setLocked(name, StatusSet, value, reason)
}

func (s *SQLiteStore) Unset(name, reason string) (Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Value{}, sql.ErrConnDone
	}
	return s.setLocked(name, StatusUnset, "", reason)
}

func (s *SQLiteStore) Forget(name, reason string) (Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Value{}, sql.ErrConnDone
	}
	return s.setLocked(name, StatusUnknown, "", reason)
}

func (s *SQLiteStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	ref := objectRefForKey(name)
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	delete(status, "value")
	if len(status) == 0 {
		_, err := s.db.Exec(`DELETE FROM objects WHERE api_version=? AND kind=? AND name=?`, ref.APIVersion, ref.Kind, ref.Name)
		return err
	}
	return s.saveStatus(ref, status)
}

func (s *SQLiteStore) setLocked(name, status, value, reason string) (Value, error) {
	now := s.now().UTC()
	current, err := s.getLocked(name)
	if err != nil {
		return Value{}, err
	}
	since := current.Since
	if current.Status != status || current.Value != value || since.IsZero() {
		since = now
	}
	next := Value{Status: status, Value: value, Reason: reason, Since: since, UpdatedAt: now}
	if err := s.put(name, next); err != nil {
		return Value{}, err
	}
	return next, nil
}

func (s *SQLiteStore) getLocked(name string) (Value, error) {
	ref := objectRefForKey(name)
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Value{}, err
	}
	if value, ok, err := readStoredVariable(status); err != nil {
		return Value{}, err
	} else if ok {
		return value, nil
	}
	now := s.now().UTC()
	return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}, nil
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
	if errors.Is(err, sql.ErrNoRows) {
		status = objectStatus{}
	} else if err != nil {
		return err
	}
	status["value"] = encodeValueMap(value)
	return s.saveStatus(ref, status)
}

func (s *SQLiteStore) loadStatus(apiVersion, kind, name string) (objectStatus, int64, error) {
	status, generation, _, err := s.loadStatusDocument(apiVersion, kind, name)
	return status, generation, err
}

func (s *SQLiteStore) loadStatusDocument(apiVersion, kind, name string) (objectStatus, int64, string, error) {
	var raw string
	var generation sql.NullInt64
	err := s.db.QueryRow(`SELECT coalesce(status,'{}'), observed_generation FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name).Scan(&raw, &generation)
	if err != nil {
		return nil, 0, "", err
	}
	status := objectStatus{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &status); err != nil {
			return nil, 0, "", fmt.Errorf("decode %s/%s status: %w", kind, name, err)
		}
	}
	if status == nil {
		return nil, 0, "", fmt.Errorf("decode %s/%s status: expected an object", kind, name)
	}
	return status, generation.Int64, raw, nil
}

func (s *SQLiteStore) saveStatus(ref objectRef, status objectStatus) error {
	if status == nil {
		status = objectStatus{}
	}
	if ref.APIVersion == "net.routerd.net/v1alpha1" && ref.Kind == "DHCPv6PrefixDelegation" {
		clean := cleanPDStatus(status)
		previous, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if observation, ok := previous["observation"]; ok {
			clean["observation"] = observation
		}
		if observed, ok := previous["observed"]; ok {
			clean["observed"] = observed
		} else {
			delete(clean, "observed")
		}
		status = clean
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encode %s/%s status: %w", ref.Kind, ref.Name, err)
	}
	if same, err := s.objectStatusUnchanged(ref, data); err != nil {
		return err
	} else if same {
		s.statusSkipCount++
		s.incrementStatusKindSkipLocked(ref.Kind)
		return nil
	}
	uid := ref.APIVersion + "/" + ref.Kind + "/" + ref.Name
	_, err = s.db.Exec(`INSERT INTO objects(api_version,kind,name,uid,resource_version,observed_generation,status,created_at,modified_at)
VALUES(?,?,?,?,1,?,?,?,?)
ON CONFLICT(api_version,kind,name) DO UPDATE SET resource_version=resource_version+1,observed_generation=excluded.observed_generation,status=excluded.status,modified_at=excluded.modified_at`,
		ref.APIVersion, ref.Kind, ref.Name, uid, nullGeneration(s.generation), string(data), now, now)
	if err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	s.statusWriteCount++
	s.incrementStatusKindWriteLocked(ref.Kind)
	return nil
}

func (s *SQLiteStore) StatusWriteStats() (writes, skips uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return 0, 0
	}
	return s.statusWriteCount, s.statusSkipCount
}

func (s *SQLiteStore) StatusKindWriteStats() map[string]StatusKindWriteStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || len(s.statusKindCounter) == 0 {
		return nil
	}
	out := make(map[string]StatusKindWriteStats, len(s.statusKindCounter))
	for kind, stats := range s.statusKindCounter {
		out[kind] = stats
	}
	return out
}

func (s *SQLiteStore) incrementStatusKindWriteLocked(kind string) {
	if s.statusKindCounter == nil {
		s.statusKindCounter = map[string]StatusKindWriteStats{}
	}
	stats := s.statusKindCounter[kind]
	stats.Writes++
	s.statusKindCounter[kind] = stats
}

func (s *SQLiteStore) incrementStatusKindSkipLocked(kind string) {
	if s.statusKindCounter == nil {
		s.statusKindCounter = map[string]StatusKindWriteStats{}
	}
	stats := s.statusKindCounter[kind]
	stats.Skips++
	s.statusKindCounter[kind] = stats
}

func (s *SQLiteStore) objectStatusUnchanged(ref objectRef, next []byte) (bool, error) {
	var raw string
	var generation sql.NullInt64
	err := s.db.QueryRow(`SELECT coalesce(status,'{}'), observed_generation FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, ref.APIVersion, ref.Kind, ref.Name).Scan(&raw, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	current := objectStatus{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &current); err != nil {
			return false, fmt.Errorf("decode %s/%s status: %w", ref.Kind, ref.Name, err)
		}
	}
	if current == nil {
		return false, fmt.Errorf("decode %s/%s status: expected an object", ref.Kind, ref.Name)
	}
	currentData, _ := json.Marshal(current)
	if string(currentData) != string(next) {
		return false, nil
	}
	if s.generation == 0 {
		return !generation.Valid, nil
	}
	return generation.Valid && generation.Int64 == s.generation, nil
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
	// ActionPlansJSON holds JSON-encoded provider ActionPlans. They are inert
	// in this store; the separate provider-action engine may consider them only
	// after its policy, approval, fencing, and typed-source checks pass.
	ActionPlansJSON string `json:"actionPlansJson,omitempty" yaml:"actionPlansJson,omitempty"`
	// MobilityDataplaneJSON holds the JSON-encoded typed local dataplane plan.
	// It is not a user-authored resource or status projection.
	MobilityDataplaneJSON string `json:"mobilityDataplaneJson,omitempty" yaml:"mobilityDataplaneJson,omitempty"`
	// ARPObserverIntentsJSON holds the JSON-encoded typed local observation
	// bootstrap intents. It is not a user-authored resource or status projection.
	ARPObserverIntentsJSON string `json:"arpObserverIntentsJson,omitempty" yaml:"arpObserverIntentsJson,omitempty"`
	FIBVerdictsJSON        string `json:"fibVerdictsJson,omitempty" yaml:"fibVerdictsJson,omitempty"`
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
		return sql.ErrConnDone
	}
	_, err := s.db.Exec(upsertDynamicConfigPartSQL, dynamicConfigPartArguments(part, s.now().UTC())...)
	return err
}

const upsertDynamicConfigPartSQL = `INSERT INTO dynamic_config_parts(source,generation,observed_at,expires_at,digest,resources_json,directives_json,actionplans_json,mobility_dataplane_json,arp_observer_intents_json,fib_verdicts_json,status,error,created_at,updated_at)
	VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(source,generation) DO UPDATE SET observed_at=excluded.observed_at,expires_at=excluded.expires_at,digest=excluded.digest,resources_json=excluded.resources_json,directives_json=excluded.directives_json,actionplans_json=excluded.actionplans_json,mobility_dataplane_json=excluded.mobility_dataplane_json,arp_observer_intents_json=excluded.arp_observer_intents_json,fib_verdicts_json=excluded.fib_verdicts_json,status=excluded.status,error=excluded.error,updated_at=excluded.updated_at`

func dynamicConfigPartArguments(part DynamicConfigPartRecord, now time.Time) []any {
	if part.ObservedAt.IsZero() {
		part.ObservedAt = now
	}
	if part.CreatedAt.IsZero() {
		part.CreatedAt = now
	}
	if part.UpdatedAt.IsZero() {
		part.UpdatedAt = now
	}
	return []any{
		part.Source, part.Generation, formatStateTime(part.ObservedAt), formatStateTime(part.ExpiresAt), part.Digest, nullableString(part.ResourcesJSON), nullableString(part.DirectivesJSON), nullableString(part.ActionPlansJSON), nullableString(part.MobilityDataplaneJSON), nullableString(part.ARPObserverIntentsJSON), nullableString(part.FIBVerdictsJSON), part.Status, nullableString(part.Error), formatStateTime(part.CreatedAt), formatStateTime(part.UpdatedAt),
	}
}

func (s *SQLiteStore) ListDynamicConfigParts() ([]DynamicConfigPartRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []DynamicConfigPartRecord{}, sql.ErrConnDone
	}
	rows, err := s.db.Query(`SELECT id,source,generation,observed_at,expires_at,digest,coalesce(resources_json,''),coalesce(directives_json,''),coalesce(actionplans_json,''),coalesce(mobility_dataplane_json,''),coalesce(arp_observer_intents_json,''),coalesce(fib_verdicts_json,''),status,coalesce(error,''),created_at,updated_at FROM dynamic_config_parts ORDER BY observed_at DESC,generation DESC,id DESC`)
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
		return []DynamicConfigPartRecord{}, sql.ErrConnDone
	}
	rows, err := s.db.Query(`SELECT id,source,generation,observed_at,expires_at,digest,coalesce(resources_json,''),coalesce(directives_json,''),coalesce(actionplans_json,''),coalesce(mobility_dataplane_json,''),coalesce(arp_observer_intents_json,''),coalesce(fib_verdicts_json,''),status,coalesce(error,''),created_at,updated_at FROM dynamic_config_parts WHERE source = ? ORDER BY generation DESC,observed_at DESC,id DESC`, source)
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
		if err := rows.Scan(&rec.ID, &rec.Source, &rec.Generation, &observed, &expires, &rec.Digest, &rec.ResourcesJSON, &rec.DirectivesJSON, &rec.ActionPlansJSON, &rec.MobilityDataplaneJSON, &rec.ARPObserverIntentsJSON, &rec.FIBVerdictsJSON, &rec.Status, &rec.Error, &created, &updated); err != nil {
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
		return 0, sql.ErrConnDone
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
		return sql.ErrConnDone
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
		return []PluginRunRecord{}, sql.ErrConnDone
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
		return 0, sql.ErrConnDone
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

// BeginGenerationIfChanged atomically records configYAML only when its
// normalized configuration hash differs from the most recently successful
// configuration snapshot. Reconciles without a configuration change still run
// and may update observed state against that existing generation, but do not
// create another config_yaml snapshot. An errored attempt never suppresses a
// later retry of the same configuration.
func (s *SQLiteStore) BeginGenerationIfChanged(configHash, configYAML string) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, false, sql.ErrConnDone
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`
INSERT INTO generations(started_at,warnings,config_hash,config_yaml)
SELECT ?,'[]',?,?
WHERE NOT EXISTS (
  SELECT 1
  FROM (
    SELECT config_hash
    FROM generations
    WHERE config_yaml IS NOT NULL
      AND phase IN ('Healthy', 'Applied', 'Committed')
    ORDER BY generation DESC
    LIMIT 1
  ) AS latest
  WHERE latest.config_hash = ?
)`, now, configHash, configYAML, configHash)
	if err != nil {
		return 0, false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if inserted == 0 {
		var generation int64
		err := s.db.QueryRow(`
SELECT generation
FROM generations
WHERE config_yaml IS NOT NULL
  AND phase IN ('Healthy', 'Applied', 'Committed')
  AND config_hash = ?
ORDER BY generation DESC
LIMIT 1`, configHash).Scan(&generation)
		if err != nil {
			return 0, false, err
		}
		s.generation = generation
		return generation, false, nil
	}
	generation, err := result.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	s.generation = generation
	return generation, true, nil
}

func (s *SQLiteStore) RecordGenerationConfig(generation int64, configYAML string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
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
		return sql.ErrConnDone
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
		return []GenerationRecord{}, sql.ErrConnDone
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
		return "", false, sql.ErrConnDone
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
		return sql.ErrConnDone
	}
	return s.saveStatus(objectRef{APIVersion: apiVersion, Kind: kind, Name: name}, objectStatus(status))
}

func (s *SQLiteStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	status, _, err := s.loadStatus(apiVersion, kind, name)
	if errors.Is(err, sql.ErrNoRows) {
		status = objectStatus{}
	} else if err != nil {
		return err
	}
	for key, value := range updates {
		status[key] = value
	}
	return s.saveStatus(objectRef{APIVersion: apiVersion, Kind: kind, Name: name}, status)
}

func (s *SQLiteStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	status, _ := s.ReadObjectStatus(apiVersion, kind, name)
	return status
}

func (s *SQLiteStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	status, _, raw, err := s.loadStatusDocument(apiVersion, kind, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "HealthCheck" {
		if err := projectHealthStatus(status, raw, s.now().UTC()); err != nil {
			return nil, err
		}
	}
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "DHCPv6PrefixDelegation" {
		if err := projectPDStatus(status, raw, s.now().UTC()); err != nil {
			return nil, err
		}
	}
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "DHCPv6Address" {
		if err := projectDHCPv6AddressStatus(status, raw, s.now().UTC()); err != nil {
			return nil, err
		}
	}
	out := map[string]any{}
	for key, value := range status {
		out[key] = value
	}
	return out, nil
}

func (s *SQLiteStore) ListObjectStatuses() ([]ObjectStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []ObjectStatus{}, sql.ErrConnDone
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
			return nil, fmt.Errorf("decode %s/%s status: %w", item.Kind, item.Name, err)
		}
		if item.Status == nil {
			return nil, fmt.Errorf("decode %s/%s status: expected an object", item.Kind, item.Name)
		}
		if item.APIVersion == "net.routerd.net/v1alpha1" && item.Kind == "HealthCheck" {
			if err := projectHealthStatus(item.Status, raw, s.now().UTC()); err != nil {
				return nil, err
			}
		}
		if item.APIVersion == "net.routerd.net/v1alpha1" && item.Kind == "DHCPv6PrefixDelegation" {
			if err := projectPDStatus(item.Status, raw, s.now().UTC()); err != nil {
				return nil, err
			}
		}
		if item.APIVersion == "net.routerd.net/v1alpha1" && item.Kind == "DHCPv6Address" {
			if err := projectDHCPv6AddressStatus(item.Status, raw, s.now().UTC()); err != nil {
				return nil, err
			}
		}
		item.Owner = statusvalue.Field(item.Status, "owner")
		item.ManagedBy = statusvalue.Field(item.Status, "managedBy")
		item.Management = statusvalue.Field(item.Status, "management")
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteStore) DeleteObject(apiVersion, kind, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	_, err := s.db.Exec(`DELETE FROM objects WHERE api_version = ? AND kind = ? AND name = ?`, apiVersion, kind, name)
	return err
}

func (s *SQLiteStore) Backup(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
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
		return sql.ErrConnDone
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
		return "", sql.ErrConnDone
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
		return sql.ErrConnDone
	}
	_, err := s.db.Exec(`VACUUM`)
	return err
}

func (s *SQLiteStore) BackupTo(dest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
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
		return 0, sql.ErrConnDone
	}
	var count int64
	err := s.db.QueryRow(`SELECT count(*) FROM events WHERE created_at < ?`, cutoff.UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count, err
}

func (s *SQLiteStore) PruneEventsOlderThan(cutoff time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, sql.ErrConnDone
	}
	formattedCutoff := cutoff.UTC().Format(time.RFC3339Nano)
	var deleted int64
	for {
		result, err := s.db.Exec(`DELETE FROM events WHERE id IN (
SELECT id FROM events WHERE created_at < ? ORDER BY id LIMIT ?)`, formattedCutoff, eventJournalPruneBatchRows)
		if err != nil {
			return deleted, s.noteStorageWriteErrorLocked(err)
		}
		batch, err := result.RowsAffected()
		if err != nil {
			return deleted, err
		}
		deleted += batch
		if batch > 0 {
			_, _ = s.db.Exec(`PRAGMA incremental_vacuum(4096)`)
			_, _ = s.db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`)
		}
		if batch < eventJournalPruneBatchRows {
			break
		}
	}
	if err := s.loadEventJournalUsage(); err != nil {
		return deleted, err
	}
	return deleted, nil
}

func (s *SQLiteStore) RecordEvent(apiVersion, kind, name, eventType, reason, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	message = boundedJournalText(message, maxJournalMessageBytes)
	payloadBytes := journalPayloadBytes(apiVersion, kind, name, eventType, reason, message)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	defer tx.Rollback()
	usage, err := s.prepareEventJournalBatchLocked(tx, 1, payloadBytes)
	if err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	_, err = tx.Exec(`INSERT INTO events(api_version,kind,name,type,reason,message,generation,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		apiVersion, kind, name, eventType, reason, message, nullGeneration(s.generation), s.now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	if err := tx.Commit(); err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	s.applyEventJournalUsageLocked(usage)
	s.eventJournalRows++
	s.eventJournalPayloadBytes += payloadBytes
	return nil
}

func (s *SQLiteStore) RecordBusEvent(ctx context.Context, event daemonapi.DaemonEvent) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", sql.ErrConnDone
	}
	bounded, attrs := boundedJournalEvent(event)
	payloadBytes := busEventJournalPayloadBytes(bounded, attrs)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", s.noteStorageWriteErrorLocked(err)
	}
	defer tx.Rollback()
	usage, err := s.prepareEventJournalBatchLocked(tx, 1, payloadBytes)
	if err != nil {
		return "", s.noteStorageWriteErrorLocked(err)
	}
	cursor, payloadBytes, err := recordBusEvent(tx, s.generation, s.now().UTC(), bounded)
	if err != nil {
		return "", s.noteStorageWriteErrorLocked(err)
	}
	if err := tx.Commit(); err != nil {
		return "", s.noteStorageWriteErrorLocked(err)
	}
	s.applyEventJournalUsageLocked(usage)
	s.eventJournalRows++
	s.eventJournalPayloadBytes += payloadBytes
	return cursor, nil
}

func recordBusEvent(executor interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}, generation int64, now time.Time, event daemonapi.DaemonEvent) (string, int64, error) {
	if err := projectDHCPLeaseObservation(executor, now, event); err != nil {
		return "", 0, err
	}
	event, attrs := boundedJournalEvent(event)
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
	payloadBytes := journalPayloadBytes(apiVersion, kind, name, event.Type, event.Reason, event.Message, event.Type, event.Daemon.Kind, event.Daemon.Instance, resourceAPI, resourceKind, resourceName, event.Severity, attrs)
	result, err := executor.Exec(`INSERT INTO events(api_version,kind,name,type,reason,message,generation,created_at,topic,source_kind,source_instance,resource_api_version,resource_kind,resource_name,severity,attributes)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		apiVersion, kind, name, event.Type, event.Reason, event.Message, nullGeneration(generation), now.UTC().Format(time.RFC3339Nano),
		event.Type, event.Daemon.Kind, event.Daemon.Instance, resourceAPI, resourceKind, resourceName, event.Severity, attrs)
	if err != nil {
		return "", 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%d", id), payloadBytes, nil
}

func busEventJournalPayloadBytes(event daemonapi.DaemonEvent, attrs string) int64 {
	apiVersion, kind, name := event.APIVersion, event.Kind, event.Daemon.Name
	resourceAPI, resourceKind, resourceName := "", "", ""
	if event.Resource != nil {
		apiVersion, kind, name = event.Resource.APIVersion, event.Resource.Kind, event.Resource.Name
		resourceAPI, resourceKind, resourceName = event.Resource.APIVersion, event.Resource.Kind, event.Resource.Name
	}
	return journalPayloadBytes(apiVersion, kind, name, event.Type, event.Reason, event.Message, event.Type, event.Daemon.Kind, event.Daemon.Instance, resourceAPI, resourceKind, resourceName, event.Severity, attrs)
}

func (s *SQLiteStore) ReadEvents(ctx context.Context, apiVersion, kind, name string, limit int) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, sql.ErrConnDone
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,api_version,kind,name,type,reason,message,coalesce(generation,0),created_at FROM events WHERE api_version = ? AND kind = ? AND name = ? ORDER BY id DESC LIMIT ?`, apiVersion, kind, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		var created string
		if err := rows.Scan(&event.ID, &event.APIVersion, &event.Kind, &event.Name, &event.Type, &event.Reason, &event.Message, &event.Generation, &created); err != nil {
			return nil, err
		}
		event.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("decode event %d creation time: %w", event.ID, err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SQLiteStore) ListEvents(query EventQuery) ([]StoredEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return []StoredEvent{}, sql.ErrConnDone
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
	order := "DESC"
	if query.Ascending {
		order = "ASC"
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT id,api_version,kind,name,type,reason,message,coalesce(generation,0),created_at,
coalesce(topic,''),coalesce(source_kind,''),coalesce(source_instance,''),coalesce(resource_api_version,''),coalesce(resource_kind,''),coalesce(resource_name,''),coalesce(severity,''),coalesce(attributes,'')
FROM events`+where+` ORDER BY id `+order+` LIMIT ?`, args...)
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
		event.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("decode event %d creation time: %w", event.ID, err)
		}
		if attributes != "" && attributes != "null" {
			var decoded map[string]any
			if err := json.Unmarshal([]byte(attributes), &decoded); err != nil {
				return nil, fmt.Errorf("decode event %d attributes: %w", event.ID, err)
			}
			event.Attributes = decoded
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *SQLiteStore) LoadOrInitializeEventConsumerCursor(consumer string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, sql.ErrConnDone
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`INSERT INTO event_consumer_cursors(consumer,cursor,updated_at)
SELECT ?,COALESCE(MAX(id),0),? FROM events WHERE true
ON CONFLICT(consumer) DO NOTHING`, consumer, now); err != nil {
		return 0, err
	}
	var cursor int64
	err := s.db.QueryRow(`SELECT cursor FROM event_consumer_cursors WHERE consumer = ?`, consumer).Scan(&cursor)
	return cursor, err
}

func (s *SQLiteStore) SaveEventConsumerCursor(consumer string, cursor int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	_, err := s.db.Exec(`INSERT INTO event_consumer_cursors(consumer,cursor,updated_at) VALUES(?,?,?)
ON CONFLICT(consumer) DO UPDATE SET cursor=excluded.cursor,updated_at=excluded.updated_at`,
		consumer, cursor, s.now().UTC().Format(time.RFC3339Nano))
	return err
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

func objectRefForKey(key string) objectRef {
	return objectRef{APIVersion: stateAPIVersion, Kind: stateKind, Name: key}
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

// readStoredVariable decodes the closed variable shape at its SQLite boundary.
// A missing value is unknown; a malformed present value is a read failure.
func readStoredVariable(status objectStatus) (Value, bool, error) {
	raw, found := status["value"]
	if !found {
		return Value{}, false, nil
	}
	fields, ok := raw.(map[string]any)
	if !ok || fields == nil {
		return Value{}, false, fmt.Errorf("invalid stored variable: expected an object")
	}
	var value Value
	var since, updated string
	for _, field := range []struct {
		name     string
		target   *string
		required bool
	}{
		{"status", &value.Status, true}, {"value", &value.Value, false}, {"reason", &value.Reason, false}, {"since", &since, true}, {"updatedAt", &updated, true},
	} {
		raw, exists := fields[field.name]
		if !exists && !field.required {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			return Value{}, false, fmt.Errorf("invalid stored variable %s: expected a string", field.name)
		}
		*field.target = text
	}
	switch value.Status {
	case StatusSet:
		if value.Value == "" {
			return Value{}, false, fmt.Errorf("invalid stored set variable: missing value")
		}
	case StatusUnset, StatusUnknown:
		if value.Value != "" {
			return Value{}, false, fmt.Errorf("invalid stored %s variable: unexpected value", value.Status)
		}
	default:
		return Value{}, false, fmt.Errorf("invalid stored variable status %q", value.Status)
	}
	var err error
	value.Since, err = time.Parse(time.RFC3339Nano, since)
	if err != nil {
		return Value{}, false, fmt.Errorf("invalid stored variable since: %w", err)
	}
	value.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return Value{}, false, fmt.Errorf("invalid stored variable updatedAt: %w", err)
	}
	return value, true, nil
}

func variablesFromObject(apiVersion, kind, name string, status objectStatus) map[string]Value {
	out := map[string]Value{}
	if apiVersion == stateAPIVersion && kind == stateKind {
		if value, ok, err := readStoredVariable(status); err == nil && ok {
			out[name] = value
		}
	}
	return out
}
