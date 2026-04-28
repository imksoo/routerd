package resource

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

type Ledger interface {
	Remember([]Artifact)
	Forget([]Artifact)
	Owns(Artifact) bool
	Save(string) error
	All() []Artifact
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

func (l *JSONLedger) Remember(artifacts []Artifact) {
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
}

func (l *JSONLedger) Forget(artifacts []Artifact) {
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
}

func (l *JSONLedger) Owns(artifact Artifact) bool {
	for _, known := range l.Artifacts {
		if known.Identity() == artifact.Identity() {
			return true
		}
	}
	return false
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

func (l *JSONLedger) All() []Artifact {
	out := append([]Artifact(nil), l.Artifacts...)
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

type SQLiteLedger struct {
	path string
	db   *sql.DB
}

func OpenSQLiteLedger(path string) (*SQLiteLedger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
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
CREATE TABLE IF NOT EXISTS state (
  key TEXT PRIMARY KEY,
  value TEXT,
  status TEXT NOT NULL,
  reason TEXT,
  since TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  name TEXT NOT NULL,
  owner TEXT,
  attributes TEXT,
  source TEXT,
  generation INTEGER,
  observed_at TEXT
);
`)
	return err
}

func (l *SQLiteLedger) migrateLegacyJSON() error {
	legacy := filepath.Join(filepath.Dir(l.path), "artifacts.json")
	if _, err := os.Stat(legacy); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var count int
	if err := l.db.QueryRow(`SELECT count(*) FROM artifacts`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	jsonLedger, err := loadJSONLedgerFile(legacy)
	if err != nil {
		return err
	}
	l.Remember(jsonLedger.Artifacts)
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

func (l *SQLiteLedger) Remember(artifacts []Artifact) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, artifact := range artifacts {
		if artifact.Owner == "" {
			continue
		}
		attrs, _ := json.Marshal(artifact.Attributes)
		_, _ = l.db.Exec(`INSERT INTO artifacts(id,kind,name,owner,attributes,source,generation,observed_at)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,name=excluded.name,owner=excluded.owner,attributes=excluded.attributes,source=excluded.source,generation=excluded.generation,observed_at=excluded.observed_at`,
			artifact.Identity(), artifact.Kind, artifact.Name, artifact.Owner, string(attrs), "routerd", 1, now)
	}
}

func (l *SQLiteLedger) Forget(artifacts []Artifact) {
	for _, artifact := range artifacts {
		_, _ = l.db.Exec(`DELETE FROM artifacts WHERE id = ?`, artifact.Identity())
	}
}

func (l *SQLiteLedger) Owns(artifact Artifact) bool {
	var id string
	err := l.db.QueryRow(`SELECT id FROM artifacts WHERE id = ?`, artifact.Identity()).Scan(&id)
	return err == nil
}

func (l *SQLiteLedger) Save(path string) error { return nil }

func (l *SQLiteLedger) All() []Artifact {
	rows, err := l.db.Query(`SELECT kind,name,coalesce(owner,''),coalesce(attributes,'{}') FROM artifacts ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var artifact Artifact
		var attrs string
		if err := rows.Scan(&artifact.Kind, &artifact.Name, &artifact.Owner, &attrs); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(attrs), &artifact.Attributes)
		if artifact.Attributes == nil {
			artifact.Attributes = map[string]string{}
		}
		out = append(out, artifact)
	}
	return out
}
