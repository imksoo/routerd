package resource

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLiteLedgerPersistsArtifacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	ledger, err := OpenSQLiteLedger(path)
	if err != nil {
		t.Fatalf("open sqlite ledger: %v", err)
	}
	artifact := Artifact{
		Kind:       "nft.table",
		Name:       "routerd_nat",
		Owner:      "net.routerd.net/v1alpha1/IPv4SourceNAT/lan",
		Attributes: map[string]string{"family": "ip", "name": "routerd_nat"},
	}
	ledger.Remember([]Artifact{artifact})
	if !ledger.Owns(artifact) {
		t.Fatal("sqlite ledger does not own remembered artifact")
	}

	reloaded, err := OpenSQLiteLedger(path)
	if err != nil {
		t.Fatalf("reopen sqlite ledger: %v", err)
	}
	if !reloaded.Owns(artifact) {
		t.Fatal("reloaded sqlite ledger does not own artifact")
	}
}

func TestSQLiteLedgerMigratesTwoTableSQLiteArtifacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE artifacts(id TEXT PRIMARY KEY, kind TEXT, name TEXT, owner TEXT, attributes TEXT, source TEXT, generation INTEGER, observed_at TEXT);
INSERT INTO artifacts(id,kind,name,owner,attributes,source,generation,observed_at) VALUES('nft.table/routerd_nat','nft.table','routerd_nat','net.routerd.net/v1alpha1/IPv4SourceNAT/lan','{"family":"ip","name":"routerd_nat"}','routerd',1,'2026-04-28T00:00:00Z');`)
	if err != nil {
		t.Fatalf("seed fixture db: %v", err)
	}
	_ = db.Close()

	ledger, err := OpenSQLiteLedger(path)
	if err != nil {
		t.Fatalf("open sqlite ledger: %v", err)
	}
	artifact := Artifact{Kind: "nft.table", Name: "routerd_nat", Owner: "net.routerd.net/v1alpha1/IPv4SourceNAT/lan"}
	if !ledger.Owns(artifact) {
		t.Fatal("migrated ledger does not own artifact")
	}
}

func TestSQLiteLedgerMigratesLegacyJSONAndRenames(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "artifacts.json")
	if err := os.WriteFile(legacy, []byte(`{
  "version": 1,
  "artifacts": [
    {
      "kind": "nft.table",
      "name": "routerd_nat",
      "owner": "net.routerd.net/v1alpha1/IPv4SourceNAT/lan",
      "attributes": {"family": "ip", "name": "routerd_nat"}
    }
  ]
}
`), 0644); err != nil {
		t.Fatalf("write legacy ledger: %v", err)
	}

	ledger, err := OpenSQLiteLedger(filepath.Join(dir, "routerd.db"))
	if err != nil {
		t.Fatalf("open sqlite ledger: %v", err)
	}
	artifact := Artifact{Kind: "nft.table", Name: "routerd_nat", Owner: "net.routerd.net/v1alpha1/IPv4SourceNAT/lan"}
	if !ledger.Owns(artifact) {
		t.Fatal("migrated ledger does not own artifact")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy ledger still present: %v", err)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Fatalf("migrated ledger missing: %v", err)
	}
}
