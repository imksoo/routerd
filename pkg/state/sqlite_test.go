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
		ValidLifetime:  "14400",
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
	err = db.QueryRow(`SELECT json_extract(value, '$.lastPrefix') FROM state WHERE key = ?`, "ipv6PrefixDelegation.wan-pd.lease").Scan(&prefix)
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
    "ipv6PrefixDelegation.wan-pd.lastPrefix": {
      "status": "set",
      "value": "2001:db8:1200:1210::/60",
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
