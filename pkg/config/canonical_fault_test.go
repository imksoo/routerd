// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFaultAtomicWriteReplacementAndDurability(t *testing.T) {
	fault := errors.New("injected file operation failure")
	for _, stage := range []string{"success", "rename", "chmod", "sync-directory"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "canonical.yaml")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			ops := atomicFileOps{rename: os.Rename, chmod: os.Chmod, syncDir: syncDir}
			switch stage {
			case "rename":
				ops.rename = func(string, string) error { return fault }
			case "chmod":
				ops.chmod = func(string, os.FileMode) error { return fault }
			case "sync-directory":
				ops.syncDir = func(string) error { return fault }
			}
			outcome, err := atomicWriteFile(path, []byte("new"), ops)
			if stage == "success" && err != nil || stage != "success" && !errors.Is(err, fault) {
				t.Errorf("error = %v", err)
			}
			if outcome.Replaced != (stage != "rename") || outcome.DurabilityConfirmed != (stage == "success") {
				t.Errorf("outcome = %+v", outcome)
			}
			data, err := os.ReadFile(path)
			want := "new"
			if stage == "rename" {
				want = "old"
			}
			if err != nil || string(data) != want {
				t.Errorf("visible canonical = %q, %v", data, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Errorf("mode not preserved: %v, %v", info, err)
			}
			files, err := os.ReadDir(dir)
			if err != nil || len(files) != 1 {
				t.Errorf("temporary files remain: %v, %v", files, err)
			}
		})
	}
}

func TestFaultAtomicWritePreflightAndNewFile(t *testing.T) {
	result, err := AtomicWriteFileWithOutcome("", []byte("new"))
	if err == nil || result.Replaced || result.DurabilityConfirmed {
		t.Fatalf("empty path = %+v, %v", result, err)
	}
	path := filepath.Join(t.TempDir(), "nested", "router.yaml")
	result, err = AtomicWriteFileWithOutcome(path, []byte("new"))
	if err != nil || !result.Replaced || !result.DurabilityConfirmed {
		t.Fatalf("new file = %+v, %v", result, err)
	}
}
