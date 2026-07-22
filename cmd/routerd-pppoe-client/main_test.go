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
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/pppoeclient"
)

func TestStartSessionFailsClosedWhenFreeBSDPPPoEModuleLoadFails(t *testing.T) {
	previous := ensureFreeBSDPPPoEModule
	t.Cleanup(func() { ensureFreeBSDPPPoEModule = previous })
	want := errors.New("ng_iface unavailable")
	ensureFreeBSDPPPoEModule = func(context.Context) error { return want }
	dir := t.TempDir()
	d := newDaemon(options{resource: "wan", ifname: "vtnet0", username: "user", password: "secret", runtimeDir: dir, stateFile: filepath.Join(dir, "state.json")}, nil)
	err := d.startSession(context.Background())
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "ng_iface") {
		t.Fatalf("startSession error = %v, want module-load failure", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cmd != nil || d.snapshot.Phase != pppoeclient.PhaseFailed {
		t.Fatalf("module failure did not fail closed: cmd=%v phase=%q", d.cmd, d.snapshot.Phase)
	}
}

func TestStartSessionRefusesPreexistingFreeBSDPPPoEInterface(t *testing.T) {
	previousOS := currentPPPoEOS
	previousModules := ensureFreeBSDPPPoEModule
	previousExists := freeBSDPPPoEInterfaceExists
	t.Cleanup(func() {
		currentPPPoEOS = previousOS
		ensureFreeBSDPPPoEModule = previousModules
		freeBSDPPPoEInterfaceExists = previousExists
	})
	currentPPPoEOS = func() platform.OS { return platform.OSFreeBSD }
	ensureFreeBSDPPPoEModule = func(context.Context) error { return nil }
	freeBSDPPPoEInterfaceExists = func(_ context.Context, ifname string) (bool, error) {
		if ifname != pppoeclient.DefaultIfName("wan") {
			t.Fatalf("checked interface = %q", ifname)
		}
		return true, nil
	}
	dir := t.TempDir()
	d := newDaemon(options{resource: "wan", ifname: "vtnet0", username: "user", password: "secret", runtimeDir: dir, stateFile: filepath.Join(dir, "state.json")}, nil)
	err := d.startSession(context.Background())
	if err == nil || !strings.Contains(err.Error(), "already exists") || !strings.Contains(err.Error(), pppoeclient.DefaultIfName("wan")) {
		t.Fatalf("startSession error = %v, want foreign-interface refusal", err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cmd != nil || d.snapshot.Phase != pppoeclient.PhaseFailed {
		t.Fatalf("foreign interface did not fail closed: cmd=%v phase=%q", d.cmd, d.snapshot.Phase)
	}
}

func TestEnsureFreeBSDPPPoEModulesLoadsIPCPInterfaceModulesAndLeavesLinuxUntouched(t *testing.T) {
	previous := loadFreeBSDPPPoEModule
	t.Cleanup(func() { loadFreeBSDPPPoEModule = previous })
	var loaded []string
	loadFreeBSDPPPoEModule = func(_ context.Context, module string) error {
		loaded = append(loaded, module)
		return nil
	}
	if err := ensureFreeBSDPPPoEModules(t.Context(), platform.OSFreeBSD); err != nil {
		t.Fatalf("ensure FreeBSD modules: %v", err)
	}
	if got, want := strings.Join(loaded, ","), "ng_pppoe,ng_iface"; got != want {
		t.Fatalf("loaded modules = %q, want %q", got, want)
	}
	loaded = nil
	if err := ensureFreeBSDPPPoEModules(t.Context(), platform.OSLinux); err != nil {
		t.Fatalf("ensure Linux modules: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("Linux loaded FreeBSD modules: %#v", loaded)
	}
}

func TestEnsureFreeBSDPPPoEModulesFailsClosedWhenIPCPInterfaceLoadFails(t *testing.T) {
	previous := loadFreeBSDPPPoEModule
	t.Cleanup(func() { loadFreeBSDPPPoEModule = previous })
	want := errors.New("ng_iface unavailable")
	loadFreeBSDPPPoEModule = func(_ context.Context, module string) error {
		if module == "ng_iface" {
			return want
		}
		return nil
	}
	err := ensureFreeBSDPPPoEModules(t.Context(), platform.OSFreeBSD)
	if !errors.Is(err, want) {
		t.Fatalf("ensure FreeBSD modules error = %v, want ng_iface failure", err)
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

func TestParseFreeBSDPPPoEInterfaceObservation(t *testing.T) {
	addresses, err := parseFreeBSDPPPoEAddresses(`ppp39362e66: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> metric 0 mtu 1454
		inet 198.18.10.2 --> 198.18.10.1 netmask 0xffffffff
`)
	if err != nil {
		t.Fatalf("parse FreeBSD PPPoE addresses: %v", err)
	}
	if addresses.CurrentAddress != "198.18.10.2" || addresses.PeerAddress != "198.18.10.1" {
		t.Fatalf("addresses = %#v", addresses)
	}
	bytesIn, bytesOut, err := parseFreeBSDPPPoECounters(`Name    Mtu Network       Address              Ipkts Ierrs Idrop Ibytes    Opkts Oerrs  Obytes Coll
ppp39362e66 1454 <Link#9>    00:00:00:00:00:00       7     0     0   4567       9     0    6789    0
`, "ppp39362e66")
	if err != nil {
		t.Fatalf("parse FreeBSD PPPoE counters: %v", err)
	}
	if bytesIn != 4567 || bytesOut != 6789 {
		t.Fatalf("counters = (%d, %d), want (4567, 6789)", bytesIn, bytesOut)
	}
}

func TestObserveFreeBSDPPPoEInterfaceUsesInjectedBaseCommands(t *testing.T) {
	previous := runFreeBSDPPPoEObserverCommand
	t.Cleanup(func() { runFreeBSDPPPoEObserverCommand = previous })
	var commands []string
	runFreeBSDPPPoEObserverCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		switch name {
		case "ifconfig":
			return []byte("pppwan: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> metric 0 mtu 1454\n\tinet 198.18.10.2 --> 198.18.10.1 netmask 0xffffffff\n"), nil
		case "netstat":
			return []byte("Name    Mtu Network       Address              Ipkts Ierrs Idrop Ibytes    Opkts Oerrs  Obytes Coll\npppwan 1454 <Link#9>    00:00:00:00:00:00       7     0     0   4567       9     0    6789    0\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	got, err := observeFreeBSDPPPoEInterface(t.Context(), "pppwan")
	if err != nil {
		t.Fatalf("observe interface: %v", err)
	}
	if got.CurrentAddress != "198.18.10.2" || got.PeerAddress != "198.18.10.1" || got.BytesIn != 4567 || got.BytesOut != 6789 {
		t.Fatalf("observation = %#v", got)
	}
	if strings.Join(commands, ",") != "ifconfig pppwan,netstat -I pppwan -b" {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestFreeBSDPPPoEInterfaceObservationPersistsKernelState(t *testing.T) {
	dir := t.TempDir()
	cmd := &exec.Cmd{}
	d := &daemon{
		opts:     options{resource: "wan", stateFile: filepath.Join(dir, "state.json"), eventFile: filepath.Join(dir, "events.jsonl")},
		snapshot: pppoeclient.Snapshot{Resource: "wan", IfName: "pppwan", Phase: pppoeclient.PhaseConnecting},
		cmd:      cmd,
	}
	d.recordFreeBSDPPPoEObservation(cmd, freeBSDPPPoEObservation{
		CurrentAddress: "198.18.10.2",
		PeerAddress:    "198.18.10.1",
		BytesIn:        12,
		BytesOut:       34,
	})
	d.mu.Lock()
	snapshot := d.snapshot
	d.mu.Unlock()
	if snapshot.Phase != pppoeclient.PhaseConnected || snapshot.CurrentAddress != "198.18.10.2" || snapshot.PeerAddress != "198.18.10.1" || snapshot.BytesIn != 12 || snapshot.BytesOut != 34 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	state, err := os.ReadFile(d.opts.stateFile)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	events, err := os.ReadFile(d.opts.eventFile)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if !strings.Contains(string(state), "198.18.10.2") || !strings.Contains(string(events), "FreeBSD PPPoE interface has assigned") {
		t.Fatalf("kernel observation was not persisted: state=%s events=%s", state, events)
	}
}
