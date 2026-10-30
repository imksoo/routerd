// SPDX-License-Identifier: BSD-3-Clause

package resource

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/storage"

	_ "modernc.org/sqlite"
)

// Ledger records ownership. Durable mutations commit before returning; transient
// ledgers are explicitly selected for read-only evaluation. Read failures are errors.
type Ledger interface {
	Remember([]Artifact) error
	Forget([]Artifact) error
	Owns(Artifact) (bool, error)
	All() ([]Artifact, error)
	Close() error
}

type MemoryLedger struct {
	artifacts []Artifact
}

func NewLedger() *MemoryLedger {
	return &MemoryLedger{}
}

func LoadLedger(path string) (Ledger, error) {
	if path == "" {
		return NewLedger(), nil
	}
	return OpenSQLiteLedger(path)
}

func (l *MemoryLedger) Remember(artifacts []Artifact) error {
	byID := map[string]Artifact{}
	for _, artifact := range l.artifacts {
		artifact.Attributes = maps.Clone(artifact.Attributes)
		byID[artifact.Identity()] = artifact
	}
	for _, artifact := range artifacts {
		if artifact.Owner == "" {
			continue
		}
		artifact.Attributes = maps.Clone(artifact.Attributes)
		byID[artifact.Identity()] = artifact
	}
	l.artifacts = l.artifacts[:0]
	for _, artifact := range byID {
		l.artifacts = append(l.artifacts, artifact)
	}
	sort.Slice(l.artifacts, func(i, j int) bool {
		return l.artifacts[i].Identity() < l.artifacts[j].Identity()
	})
	return nil
}

func (l *MemoryLedger) Forget(artifacts []Artifact) error {
	remove := map[string]bool{}
	for _, artifact := range artifacts {
		remove[artifact.Identity()] = true
	}
	var kept []Artifact
	for _, artifact := range l.artifacts {
		if !remove[artifact.Identity()] {
			kept = append(kept, artifact)
		}
	}
	l.artifacts = kept
	return nil
}

func (l *MemoryLedger) Owns(artifact Artifact) (bool, error) {
	for _, known := range l.artifacts {
		if known.Identity() == artifact.Identity() {
			return true, nil
		}
	}
	return false, nil
}

func (l *MemoryLedger) All() ([]Artifact, error) {
	out := append([]Artifact(nil), l.artifacts...)
	for i := range out {
		out[i].Attributes = maps.Clone(out[i].Attributes)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out, nil
}

// Close is a no-op for MemoryLedger; it exists to satisfy the Ledger interface
// so callers can uniformly defer ledger.Close() regardless of the backing store.
func (l *MemoryLedger) Close() error {
	return nil
}

type SQLiteLedger struct {
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
	ledger := &SQLiteLedger{db: db}
	if err := storage.Initialize(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return ledger, nil
}

// OpenSQLiteLedgerReadOnly includes committed WAL state without initializing or
// migrating the database. SQL itself prevents writes through this handle.
func OpenSQLiteLedgerReadOnly(path string) (*SQLiteLedger, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := storage.Check(db); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteLedger{db: db}, nil
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
