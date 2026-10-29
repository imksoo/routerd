// SPDX-License-Identifier: BSD-3-Clause

package eventfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendJSONLineRotatesAtLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendJSONLineWithLimit(path, map[string]string{"new": "event"}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated file: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "old\n" {
		t.Fatalf("new event was not written after rotation")
	}
}

func TestAppendJSONLineReportsFailedRotationWithoutGrowingCurrentFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	original := []byte("old\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".1", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path+".1", "retain"), []byte("occupied"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := AppendJSONLineWithLimit(path, map[string]string{"new": "event"}, 1); err == nil {
		t.Fatal("failed rotation reported success")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("current log grew after rotation failed: %q err=%v", data, err)
	}
}
