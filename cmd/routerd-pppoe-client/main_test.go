// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/pppoeclient"
)

func TestStartSessionFailsClosedWhenFreeBSDPPPoEModuleLoadFails(t *testing.T) {
	previous := ensureFreeBSDPPPoEModule
	t.Cleanup(func() { ensureFreeBSDPPPoEModule = previous })
	want := errors.New("ng_pppoe unavailable")
	ensureFreeBSDPPPoEModule = func(context.Context) error { return want }
	dir := t.TempDir()
	d := newDaemon(options{resource: "wan", ifname: "vtnet0", username: "user", password: "secret", runtimeDir: dir, stateFile: filepath.Join(dir, "state.json")}, nil)
	err := d.startSession(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("startSession error = %v, want module-load failure", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cmd != nil || d.snapshot.Phase != pppoeclient.PhaseFailed {
		t.Fatalf("module failure did not fail closed: cmd=%v phase=%q", d.cmd, d.snapshot.Phase)
	}
}

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
	for name, line := range map[string]string{
		"linux-pppd":   "pppd: authentication failed for secret value",
		"freebsd-mpd5": "mpd: authentication failed for secret value",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			stateFile := filepath.Join(dir, "state.json")
			eventFile := filepath.Join(dir, "events.jsonl")
			d := &daemon{opts: options{resource: "wan", password: "secret value", stateFile: stateFile, eventFile: eventFile}}
			d.scanLog(strings.NewReader(line))
			d.mu.Lock()
			gotDiagnostic := d.exitDiagnosticLocked(os.ErrInvalid)
			d.mu.Unlock()
			if strings.Contains(gotDiagnostic, "secret value") || !strings.Contains(gotDiagnostic, "[REDACTED]") {
				t.Fatalf("diagnostic = %q", gotDiagnostic)
			}
			for _, path := range []string{stateFile, eventFile} {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read %s: %v", path, err)
				}
				got := string(data)
				if strings.Contains(got, "secret value") || !strings.Contains(got, "[REDACTED]") {
					t.Fatalf("persisted diagnostic %s = %q", path, got)
				}
			}
		})
	}
}

func TestDiscoveryTimeoutPersistsActionableFailureForBothBackends(t *testing.T) {
	for osName, wantCommand := range map[string]string{"linux": "pppd", "freebsd": "mpd5"} {
		t.Run(osName, func(t *testing.T) {
			cfg := pppoeclient.Config{Resource: "wan", Interface: "vtnet0", Spec: api.PPPoESessionSpec{Username: "user"}}
			gotCommand, _ := pppoeclient.CommandForOS(osName, cfg)
			if gotCommand != wantCommand {
				t.Fatalf("command = %q, want %q", gotCommand, wantCommand)
			}

			dir := t.TempDir()
			stateFile := filepath.Join(dir, "state.json")
			eventFile := filepath.Join(dir, "events.jsonl")
			cmd := &exec.Cmd{}
			d := &daemon{
				opts:     options{resource: "wan", password: "secret value", stateFile: stateFile, eventFile: eventFile, discoveryTimeout: time.Millisecond},
				snapshot: pppoeclient.Snapshot{Resource: "wan", Phase: pppoeclient.PhaseConnecting},
				cmd:      cmd,
			}
			d.watchDiscoveryTimeout(cmd)

			state, err := os.ReadFile(stateFile)
			if err != nil {
				t.Fatalf("read state: %v", err)
			}
			if strings.Contains(string(state), "secret value") || !strings.Contains(string(state), pppoeclient.PhaseFailed) || !strings.Contains(string(state), "discovery timed out") {
				t.Fatalf("state = %s", state)
			}
			events, err := os.ReadFile(eventFile)
			if err != nil {
				t.Fatalf("read events: %v", err)
			}
			if strings.Contains(string(events), "secret value") || !strings.Contains(string(events), "DiscoveryTimeout") {
				t.Fatalf("events = %s", events)
			}

			d.scanLog(strings.NewReader("local  IP address 198.51.100.2"))
			d.mu.Lock()
			phase := d.snapshot.Phase
			d.mu.Unlock()
			if phase != pppoeclient.PhaseConnected {
				t.Fatalf("late address did not recover session: phase=%q", phase)
			}
		})
	}
}
