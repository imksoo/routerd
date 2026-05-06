package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreStateIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	daemon := &daemon{opts: options{stateFile: path}}
	if err := daemon.restoreState(); err != nil {
		t.Fatalf("restore empty state: %v", err)
	}
}
