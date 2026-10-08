// SPDX-License-Identifier: BSD-3-Clause

package resource

import (
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
)

// Ledger records ownership. Read failures must not be treated as an empty
// ledger or an unowned artifact. SQLite mutations commit before returning;
// JSON mutations require Save to persist.
type Ledger interface {
	Remember([]Artifact) error
	Forget([]Artifact) error
	Owns(Artifact) (bool, error)
	Save(string) error
	All() ([]Artifact, error)
	Close() error
}

type JSONLedger struct {
	Version   int        `json:"version"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Artifacts []Artifact `json:"artifacts"`
}

func NewLedger() Ledger {
	return &JSONLedger{Version: 1}
}

func LoadLedger(path string) (Ledger, error) {
	if path == "" {
		return NewLedger(), nil
	}
	if filepath.Ext(path) != ".json" {
		return OpenSQLiteLedger(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &JSONLedger{Version: 1}, nil
		}
		return nil, err
	}
	var ledger JSONLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, err
	}
	if ledger.Version == 0 {
		ledger.Version = 1
	}
	return &ledger, nil
}

func (l *JSONLedger) Remember(artifacts []Artifact) error {
	byID := map[string]Artifact{}
	for _, artifact := range l.Artifacts {
		byID[artifact.Identity()] = artifact
	}
	for _, artifact := range artifacts {
		if artifact.Owner == "" {
			continue
		}
		byID[artifact.Identity()] = artifact
	}
	l.Artifacts = l.Artifacts[:0]
	for _, artifact := range byID {
		l.Artifacts = append(l.Artifacts, artifact)
	}
	sort.Slice(l.Artifacts, func(i, j int) bool {
		return l.Artifacts[i].Identity() < l.Artifacts[j].Identity()
	})
	l.Version = 1
	l.UpdatedAt = time.Now().UTC()
	return nil
}

func (l *JSONLedger) Forget(artifacts []Artifact) error {
	remove := map[string]bool{}
	for _, artifact := range artifacts {
		remove[artifact.Identity()] = true
	}
	var kept []Artifact
	for _, artifact := range l.Artifacts {
		if !remove[artifact.Identity()] {
			kept = append(kept, artifact)
		}
	}
	l.Artifacts = kept
	l.UpdatedAt = time.Now().UTC()
	return nil
}

func (l *JSONLedger) Owns(artifact Artifact) (bool, error) {
	for _, known := range l.Artifacts {
		if known.Identity() == artifact.Identity() {
			return true, nil
		}
	}
	return false, nil
}

func (l *JSONLedger) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func (l *JSONLedger) All() ([]Artifact, error) {
	out := append([]Artifact(nil), l.Artifacts...)
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out, nil
}

// Close is a no-op for JSONLedger; it exists to satisfy the Ledger interface
// so callers can uniformly defer ledger.Close() regardless of the backing store.
func (l *JSONLedger) Close() error {
	return nil
}

type SQLiteLedger struct {
	path       string
	db         *sql.DB
	generation int64
}

func OpenSQLiteLedger(path string) (*SQLiteLedger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Match pkg/state/sqlite.go: bound the pool to a single connection so
	// repeated OpenSQLiteLedger calls (e.g. routerd serve's 30s reconcile
	// loop) cannot accumulate idle file descriptors against routerd.db.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	ledger := &SQLiteLedger{path: path, db: db}
	if err := ledger.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ledger.migrateLegacyJSON(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return ledger, nil
}

func (l *SQLiteLedger) init() error {
	_, err := l.db.Exec(`
CREATE TABLE IF NOT EXISTS generations (
  generation INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  phase TEXT,
  warnings TEXT,
  config_hash TEXT
);
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
CREATE TABLE IF NOT EXISTS ledger_migrations (
  name TEXT PRIMARY KEY
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
`)
	if err != nil {
		return err
	}
	hasLastAppliedPath, err := l.tableHasColumn("objects", "last_applied_path")
	if err != nil {
		return err
	}
	if !hasLastAppliedPath {
		if _, err := l.db.Exec(`ALTER TABLE objects ADD COLUMN last_applied_path TEXT`); err != nil {
			return err
		}
	}
	return l.migrateLegacyArtifactsTable()
}

func (l *SQLiteLedger) migrateLegacyArtifactsTable() error {
	hasNew, err := l.tableHasColumn("artifacts", "artifact_id")
	if err != nil || hasNew {
		return err
	}
	hasOld, err := l.tableHasColumn("artifacts", "id")
	if err != nil || !hasOld {
		return err
	}
	if _, err := l.db.Exec(`ALTER TABLE artifacts RENAME TO artifacts_legacy`); err != nil {
		return err
	}
	if _, err := l.db.Exec(`CREATE TABLE artifacts (
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
)`); err != nil {
		return err
	}
	rows, err := l.db.Query(`SELECT id,kind,name,coalesce(owner,''),coalesce(attributes,'{}'),coalesce(source,''),generation,coalesce(observed_at,'') FROM artifacts_legacy`)
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
		if _, err := l.db.Exec(`INSERT INTO artifacts(artifact_id,kind,name,owner_api_version,owner_kind,owner_name,attributes,source,generation,observed_at)
VALUES(?,?,?,?,?,?,?,?,?,?)`, item.id, item.kind, item.name, ownerAPI, ownerKind, ownerName, item.attrs, item.source, item.generation, item.observed); err != nil {
			return err
		}
	}
	_, err = l.db.Exec(`DROP TABLE artifacts_legacy`)
	return err
}

func (l *SQLiteLedger) tableExists(name string) (bool, error) {
	var found string
	err := l.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (l *SQLiteLedger) tableHasColumn(table, column string) (bool, error) {
	exists, err := l.tableExists(table)
	if err != nil || !exists {
		return false, err
	}
	rows, err := l.db.Query(`PRAGMA table_info(` + table + `)`)
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

func (l *SQLiteLedger) migrateLegacyJSON() error {
	legacy := filepath.Join(filepath.Dir(l.path), "artifacts.json")
	if _, err := os.Stat(legacy); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	tx, err := l.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var migrated int
	if err := tx.QueryRow(`SELECT count(*) FROM ledger_migrations WHERE name = 'legacy-artifacts-json'`).Scan(&migrated); err != nil {
		return err
	}
	if migrated != 0 {
		// The import committed before an interrupted or failed rename. Never
		// import again, even if all of that ownership has since been forgotten.
		if err := tx.Commit(); err != nil {
			return err
		}
		return os.Rename(legacy, legacy+".migrated")
	}
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM artifacts`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	jsonLedger, err := loadJSONLedgerFile(legacy)
	if err != nil {
		return err
	}
	if err := l.rememberArtifacts(tx, jsonLedger.Artifacts); err != nil {
		return fmt.Errorf("migrate legacy ownership ledger: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO ledger_migrations(name) VALUES ('legacy-artifacts-json')`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return os.Rename(legacy, legacy+".migrated")
}

func loadJSONLedgerFile(path string) (*JSONLedger, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ledger JSONLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, err
	}
	if ledger.Version == 0 {
		ledger.Version = 1
	}
	return &ledger, nil
}

func (l *SQLiteLedger) Remember(artifacts []Artifact) error {
	tx, err := l.db.Begin()
	if err != nil {
		return fmt.Errorf("remember artifacts: %w", err)
	}
	defer tx.Rollback()
	if err := l.rememberArtifacts(tx, artifacts); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ownership ledger: %w", err)
	}
	return nil
}

func (l *SQLiteLedger) rememberArtifacts(tx *sql.Tx, artifacts []Artifact) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, artifact := range artifacts {
		if artifact.Owner == "" {
			continue
		}
		attrs, err := json.Marshal(artifact.Attributes)
		if err != nil {
			return fmt.Errorf("encode artifact %s: %w", artifact.Identity(), err)
		}
		ownerAPI, ownerKind, ownerName := splitOwner(artifact.Owner)
		_, err = tx.Exec(`INSERT INTO artifacts(artifact_id,kind,name,owner_api_version,owner_kind,owner_name,attributes,source,generation,observed_at)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(artifact_id) DO UPDATE SET kind=excluded.kind,name=excluded.name,owner_api_version=excluded.owner_api_version,owner_kind=excluded.owner_kind,owner_name=excluded.owner_name,attributes=excluded.attributes,source=excluded.source,generation=excluded.generation,observed_at=excluded.observed_at`,
			artifact.Identity(), artifact.Kind, artifact.Name, ownerAPI, ownerKind, ownerName, string(attrs), "routerd", effectiveGeneration(l.generation), now)
		if err != nil {
			return fmt.Errorf("remember artifact %s: %w", artifact.Identity(), err)
		}
	}
	return nil
}

func (l *SQLiteLedger) Forget(artifacts []Artifact) error {
	tx, err := l.db.Begin()
	if err != nil {
		return fmt.Errorf("forget artifacts: %w", err)
	}
	defer tx.Rollback()
	for _, artifact := range artifacts {
		if _, err := tx.Exec(`DELETE FROM artifacts WHERE artifact_id = ?`, artifact.Identity()); err != nil {
			return fmt.Errorf("forget artifact %s: %w", artifact.Identity(), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ownership ledger: %w", err)
	}
	return nil
}

func (l *SQLiteLedger) Owns(artifact Artifact) (bool, error) {
	var id string
	err := l.db.QueryRow(`SELECT artifact_id FROM artifacts WHERE artifact_id = ?`, artifact.Identity()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check ownership of %s: %w", artifact.Identity(), err)
	}
	return true, nil
}

// SQLite mutations are committed before Remember and Forget return. Save only
// checks that the backing store is still available; it does not defer writes.
func (l *SQLiteLedger) Save(path string) error { return l.db.Ping() }

func (l *SQLiteLedger) All() ([]Artifact, error) {
	rows, err := l.db.Query(`SELECT kind,name,coalesce(owner_api_version,''),coalesce(owner_kind,''),coalesce(owner_name,''),coalesce(attributes,'{}') FROM artifacts ORDER BY artifact_id`)
	if err != nil {
		return nil, fmt.Errorf("list owned artifacts: %w", err)
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var artifact Artifact
		var attrs, ownerAPI, ownerKind, ownerName string
		if err := rows.Scan(&artifact.Kind, &artifact.Name, &ownerAPI, &ownerKind, &ownerName, &attrs); err != nil {
			return nil, fmt.Errorf("scan owned artifact: %w", err)
		}
		artifact.Owner = joinOwner(ownerAPI, ownerKind, ownerName)
		if err := json.Unmarshal([]byte(attrs), &artifact.Attributes); err != nil {
			return nil, fmt.Errorf("decode owned artifact %s: %w", artifact.Identity(), err)
		}
		if artifact.Attributes == nil {
			artifact.Attributes = map[string]string{}
		}
		out = append(out, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read owned artifacts: %w", err)
	}
	return out, nil
}

func (l *SQLiteLedger) SetGeneration(generation int64) {
	l.generation = generation
}

// Close releases the underlying *sql.DB. Callers must defer Close on every
// successful LoadLedger/OpenSQLiteLedger return; otherwise the SQLite file
// descriptor (and its WAL) leaks for the lifetime of the process.
func (l *SQLiteLedger) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func effectiveGeneration(generation int64) any {
	if generation == 0 {
		return nil
	}
	return generation
}

func splitOwner(owner string) (string, string, string) {
	parts := strings.Split(owner, "/")
	if len(parts) < 3 {
		return "", "", ""
	}
	return strings.Join(parts[:len(parts)-2], "/"), parts[len(parts)-2], parts[len(parts)-1]
}

func joinOwner(apiVersion, kind, name string) string {
	if apiVersion == "" || kind == "" || name == "" {
		return ""
	}
	return apiVersion + "/" + kind + "/" + name
}
