package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	path string
	db   *sql.DB
	now  func() time.Time
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
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
	MigratePDLeases(store)
	return store, nil
}

func (s *SQLiteStore) init() error {
	_, err := s.db.Exec(`
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

func (s *SQLiteStore) migrateLegacyJSON() error {
	legacy := filepath.Join(filepath.Dir(s.path), "state.json")
	if _, err := os.Stat(legacy); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM state`).Scan(&count); err != nil {
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

func (s *SQLiteStore) Save(path string) error {
	if path == "" || path == s.path {
		return nil
	}
	// Save is retained for callers that still pass the store path. Exporting a
	// SQLite store to JSON is intentionally not supported for new writes.
	return nil
}

func (s *SQLiteStore) Get(name string) Value {
	var value Value
	var since, updated string
	err := s.db.QueryRow(`SELECT status, coalesce(value,''), coalesce(reason,''), since, updated_at FROM state WHERE key = ?`, name).Scan(&value.Status, &value.Value, &value.Reason, &since, &updated)
	if err != nil {
		now := s.now().UTC()
		return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
	}
	value.Since, _ = time.Parse(time.RFC3339Nano, since)
	value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return value
}

func (s *SQLiteStore) Set(name, value, reason string) Value {
	if value == "" {
		return s.Unset(name, reason)
	}
	return s.set(name, StatusSet, value, reason)
}

func (s *SQLiteStore) Unset(name, reason string) Value {
	return s.set(name, StatusUnset, "", reason)
}

func (s *SQLiteStore) Forget(name, reason string) Value {
	return s.set(name, StatusUnknown, "", reason)
}

func (s *SQLiteStore) Delete(name string) {
	_, _ = s.db.Exec(`DELETE FROM state WHERE key = ?`, name)
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
	_, err := s.db.Exec(`INSERT INTO state(key,value,status,reason,since,updated_at)
VALUES(?,?,?,?,?,?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value,status=excluded.status,reason=excluded.reason,since=excluded.since,updated_at=excluded.updated_at`,
		name, value.Value, value.Status, value.Reason, value.Since.Format(time.RFC3339Nano), value.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) Age(name string) time.Duration {
	return s.now().UTC().Sub(s.Get(name).Since)
}

func (s *SQLiteStore) Now() time.Time {
	return s.now().UTC()
}

func (s *SQLiteStore) Variables() map[string]Value {
	rows, err := s.db.Query(`SELECT key,status,coalesce(value,''),coalesce(reason,''),since,updated_at FROM state ORDER BY key`)
	if err != nil {
		return map[string]Value{}
	}
	defer rows.Close()
	out := map[string]Value{}
	for rows.Next() {
		var key, since, updated string
		var value Value
		if err := rows.Scan(&key, &value.Status, &value.Value, &value.Reason, &since, &updated); err != nil {
			continue
		}
		value.Since, _ = time.Parse(time.RFC3339Nano, since)
		value.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out[key] = value
	}
	return out
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func JSONValue(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
