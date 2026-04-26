package resource

import (
	"path/filepath"
	"testing"
)

func TestLedgerRememberForgetAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifacts.json")
	ledger, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("load missing ledger: %v", err)
	}
	artifact := Artifact{Kind: "nft.table", Name: "routerd_nat", Owner: "net.routerd.net/v1alpha1/IPv4SourceNAT/lan"}
	ledger.Remember([]Artifact{artifact})
	if !ledger.Owns(artifact) {
		t.Fatal("ledger does not own remembered artifact")
	}
	if err := ledger.Save(path); err != nil {
		t.Fatalf("save ledger: %v", err)
	}
	loaded, err := LoadLedger(path)
	if err != nil {
		t.Fatalf("reload ledger: %v", err)
	}
	if !loaded.Owns(artifact) {
		t.Fatal("reloaded ledger does not own artifact")
	}
	loaded.Forget([]Artifact{artifact})
	if loaded.Owns(artifact) {
		t.Fatal("ledger still owns forgotten artifact")
	}
}
