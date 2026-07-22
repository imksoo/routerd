// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestSelftestRedactsSharedRuntimeConfig(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"selftest", "--interface", "vtnet0", "--username", "user", "--password", "secret value", "--runtime-dir", t.TempDir()}, &output); err != nil {
		t.Fatalf("selftest: %v", err)
	}
	if strings.Contains(output.String(), "secret value") {
		t.Fatalf("selftest leaked password: %s", output.String())
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("decode selftest: %v", err)
	}
	if result["peer"] == nil || result["freebsd"] == nil {
		t.Fatalf("selftest missing platform configs: %#v", result)
	}
}

func TestPPPoEDiagnosticRedactsPassword(t *testing.T) {
	d := &daemon{opts: options{password: "secret value", stateFile: filepath.Join(t.TempDir(), "state.json")}}
	d.scanLog(strings.NewReader("mpd: authentication failed for secret value"))
	d.mu.Lock()
	defer d.mu.Unlock()
	if got := d.exitDiagnosticLocked(os.ErrInvalid); strings.Contains(got, "secret value") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("diagnostic = %q", got)
	}
}
