// SPDX-License-Identifier: BSD-3-Clause

package resource

import (
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
		Owner:      "net.routerd.net/v1alpha1/NAT44Rule/lan",
		Attributes: map[string]string{"family": "ip", "name": "routerd_nat"},
	}
	if err := ledger.Remember([]Artifact{artifact}); err != nil {
		t.Fatal(err)
	}
	if owns, err := ledger.Owns(artifact); err != nil || !owns {
		t.Fatal("sqlite ledger does not own remembered artifact")
	}
	if err := ledger.Close(); err != nil {
		t.Fatalf("close sqlite ledger: %v", err)
	}

	reloaded, err := OpenSQLiteLedger(path)
	if err != nil {
		t.Fatalf("reopen sqlite ledger: %v", err)
	}
	defer func() { _ = reloaded.Close() }()
	if owns, err := reloaded.Owns(artifact); err != nil || !owns {
		t.Fatal("reloaded sqlite ledger does not own artifact")
	}
}

func TestReadOnlyLedgerObservesWALAndRejectsMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd.db")
	writer, err := OpenSQLiteLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	artifact := Artifact{Kind: "ip.route", Name: "test", Owner: "routerd/test"}
	if err := writer.Remember([]Artifact{artifact}); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenSQLiteLedgerReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if owns, err := reader.Owns(artifact); err != nil || !owns {
		t.Fatalf("committed ownership missing: %t %v", owns, err)
	}
	if err := reader.Forget([]Artifact{artifact}); err == nil {
		t.Fatal("read-only ledger accepted deletion")
	}
	if owns, err := writer.Owns(artifact); err != nil || !owns {
		t.Fatalf("plan changed ownership: %t %v", owns, err)
	}
}
