// SPDX-License-Identifier: BSD-3-Clause

package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadersAlwaysObserveCompleteStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	value := map[string]any{"phase": "Running", "body": strings.Repeat("x", 128*1024)}
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	writeErrors := make(chan error, 1)
	go func() {
		defer close(done)
		for range 100 {
			if err := Write(path, value); err != nil {
				writeErrors <- err
				return
			}
		}
	}()
	for {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(data) {
			t.Fatalf("reader observed incomplete status (%d bytes)", len(data))
		}
		select {
		case <-done:
			select {
			case err := <-writeErrors:
				t.Fatal(err)
			default:
				return
			}
		default:
		}
	}
}

func TestEncodingFailurePreservesPublishedStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	if err := Write(path, map[string]string{"phase": "Running"}); err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(path, make(chan struct{})); err == nil {
		t.Fatal("unsupported status value accepted")
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != string(previous) {
		t.Fatalf("failed publication changed status: %q err=%v", current, err)
	}
}
