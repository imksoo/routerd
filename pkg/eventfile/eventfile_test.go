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
