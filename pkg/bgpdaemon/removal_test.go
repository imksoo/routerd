// SPDX-License-Identifier: BSD-3-Clause

package bgpdaemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovalIntentSurvivesFailedCheckpointDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	if err := BeginRemoval(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(path, "blocked")
	if err := os.WriteFile(child, []byte("old state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CompleteRemoval(path); err == nil {
		t.Fatal("failed checkpoint deletion was swallowed")
	}
	pending, err := RemovalPending(path)
	if err != nil || !pending {
		t.Fatal("failed cleanup lost removal intent")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := CompleteRemoval(path); err != nil {
		t.Fatal(err)
	}
	pending, err = RemovalPending(path)
	if err != nil || pending {
		t.Fatal("completed cleanup retained removal intent")
	}
}
