// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForceDeleteDiscoveryDoesNotCreateMissingState(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state.db")
	if _, err := forceDeleteTargetFromArg("UnknownKind/stale", path, ""); err == nil {
		t.Fatal("missing state produced invented target")
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 0 {
		t.Fatalf("force-delete discovery created files: %+v %v", files, err)
	}
}
