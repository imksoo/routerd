// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pppoeclient"
)

func TestPPPoELogReaderIsBoundToManagedGeneration(t *testing.T) {
	dir := t.TempDir()
	current, stale := &exec.Cmd{}, &exec.Cmd{}
	d := newDaemon(options{resource: "wan", ifname: "eth0", stateFile: filepath.Join(dir, "state"), eventFile: filepath.Join(dir, "events")}, nil)
	d.cmd = current
	d.snapshot.Phase = pppoeclient.PhaseConnecting
	d.scanLog(stale, strings.NewReader("local  IP address 198.51.100.8"))
	if d.snapshot.Phase != pppoeclient.PhaseConnecting || d.snapshot.CurrentAddress != "" {
		t.Fatalf("stale reader changed session: %+v", d.snapshot)
	}
	d.snapshot.Phase = pppoeclient.PhaseDisconnecting
	d.scanLog(current, strings.NewReader("local  IP address 198.51.100.8"))
	if d.snapshot.Phase != pppoeclient.PhaseDisconnecting {
		t.Fatal("reader overrode disconnect")
	}
}

func TestPPPoERestoreDoesNotReviveConnectionOrForeignIdentity(t *testing.T) {
	dir := t.TempDir()
	d := newDaemon(options{resource: "wan", ifname: "eth0", stateFile: filepath.Join(dir, "state")}, nil)
	prior := d.snapshot
	prior.Phase = pppoeclient.PhaseConnected
	prior.CurrentAddress = "198.51.100.8"
	prior.LastError = "previous diagnostic"
	b, _ := json.Marshal(prior)
	if err := os.WriteFile(d.opts.stateFile, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.restoreState(); err != nil {
		t.Fatal(err)
	}
	if d.snapshot.Phase != pppoeclient.PhaseIdle || d.snapshot.CurrentAddress != "" || d.snapshot.LastError != prior.LastError {
		t.Fatalf("restored active authority: %+v", d.snapshot)
	}
	prior.IfName = "administrator-interface"
	b, _ = json.Marshal(prior)
	if err := os.WriteFile(d.opts.stateFile, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.restoreState(); err == nil {
		t.Fatal("foreign identity accepted")
	}
}

func TestPPPoEFreeBSDReadFailureWithdrawsConnectedObservationAndRecovers(t *testing.T) {
	dir := t.TempDir()
	cmd := &exec.Cmd{}
	d := newDaemon(options{resource: "wan", ifname: "eth0", stateFile: filepath.Join(dir, "state"), eventFile: filepath.Join(dir, "events")}, nil)
	d.cmd = cmd
	observation := freeBSDPPPoEObservation{CurrentAddress: "198.51.100.8", PeerAddress: "198.51.100.1"}
	d.recordFreeBSDPPPoEObservation(cmd, observation)
	d.recordFreeBSDPPPoEObservationError(cmd, errors.New("IPCP interface no longer available"))
	if d.snapshot.Phase == pppoeclient.PhaseConnected || d.snapshot.CurrentAddress != "" || d.snapshot.PeerAddress != "" {
		t.Fatalf("failed observation retained authority: %+v", d.snapshot)
	}
	d.recordFreeBSDPPPoEObservation(cmd, observation)
	if d.snapshot.Phase != pppoeclient.PhaseConnected || d.snapshot.LastError != "" {
		t.Fatalf("did not recover: %+v", d.snapshot)
	}
}

func TestPPPoEPersistenceFailureIsVisibleWithoutBlockingSession(t *testing.T) {
	d := newDaemon(options{resource: "wan", ifname: "eth0", stateFile: filepath.Join(t.TempDir(), "missing", "state")}, nil)
	d.snapshot.Phase = pppoeclient.PhaseConnected
	d.mu.Lock()
	err := d.saveStateLocked()
	d.mu.Unlock()
	if err == nil {
		t.Fatal("expected save failure")
	}
	response := httptest.NewRecorder()
	d.handleStatus(response, httptest.NewRequest("GET", "/v1/status", nil))
	var status daemonapi.DaemonStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Health != daemonapi.HealthFailed || len(status.Conditions) == 0 || status.Resources[0].Phase != pppoeclient.PhaseConnected {
		t.Fatalf("persistence/forwarding state conflated: %+v", status)
	}
}

func TestPPPoEInterfaceEnumerationDoesNotTreatCommandFailureAsAbsence(t *testing.T) {
	oldLinux, oldBSD := runLinuxPPPoEInterfaceProbe, runFreeBSDPPPoEObserverCommand
	t.Cleanup(func() { runLinuxPPPoEInterfaceProbe = oldLinux; runFreeBSDPPPoEObserverCommand = oldBSD })
	failure := errors.New("permission denied")
	runLinuxPPPoEInterfaceProbe = func(context.Context, string, ...string) ([]byte, error) { return nil, failure }
	runFreeBSDPPPoEObserverCommand = runLinuxPPPoEInterfaceProbe
	if _, err := linuxPPPoEInterfaceExists(t.Context(), "ppp-wan"); err == nil {
		t.Fatal("Linux read failure became absence")
	}
	if _, err := freeBSDPPPoEInterfaceExists(t.Context(), "ppp-wan"); err == nil {
		t.Fatal("FreeBSD read failure became absence")
	}
	runLinuxPPPoEInterfaceProbe = func(context.Context, string, ...string) ([]byte, error) { return []byte(`[{"ifname":"lo"}]`), nil }
	if exists, err := linuxPPPoEInterfaceExists(t.Context(), "ppp-wan"); err != nil || exists {
		t.Fatalf("absent interface: %t %v", exists, err)
	}
}

func TestPPPoEMissingCursorReplaysBoundedRetainedEvents(t *testing.T) {
	dir := t.TempDir()
	d := newDaemon(options{resource: "wan", eventFile: filepath.Join(dir, "events")}, nil)
	d.publish("session", "Info", "Connecting", "first", nil)
	cursor := d.events[0].Cursor
	d.publish("session", "Info", "Connected", "second", nil)
	events, _ := d.eventsSinceLocked(cursor, "")
	if len(events) != 1 || events[0].Message != "second" {
		t.Fatalf("exact cursor: %+v", events)
	}
	events, _ = d.eventsSinceLocked("cursor-from-before-restart", "")
	if len(events) != 2 {
		t.Fatalf("unknown cursor suppressed events: %+v", events)
	}
}

func TestPPPoEUnexpectedExitWithdrawsActiveObservationAndReportsFailure(t *testing.T) {
	dir := t.TempDir()
	cmd := &exec.Cmd{}
	done := make(chan struct{})
	d := newDaemon(options{resource: "wan", stateFile: filepath.Join(dir, "state"), eventFile: filepath.Join(dir, "events")}, nil)
	d.cmd, d.cmdDone = cmd, done
	d.snapshot.Phase = pppoeclient.PhaseConnected
	d.snapshot.CurrentAddress = "192.0.2.8"
	d.snapshot.PeerAddress = "192.0.2.1"
	d.snapshot.DNSServers = []string{"192.0.2.53"}
	d.snapshot.ConnectedAt = time.Now()
	d.finishSession(cmd, done, errors.New("link lost"), nil)
	if d.snapshot.Phase != pppoeclient.PhaseFailed || d.snapshot.CurrentAddress != "" || d.snapshot.PeerAddress != "" || len(d.snapshot.DNSServers) != 0 || !d.snapshot.ConnectedAt.IsZero() {
		t.Fatalf("exited child retained active authority: %+v", d.snapshot)
	}
}

func TestPPPoEStatusBindsPublicIdentityWithoutCredentialMaterial(t *testing.T) {
	d := newDaemon(options{resource: "wan", ifname: "eth0", username: "user", password: "never-emit-secret", authMethod: "chap", serviceName: "service", acName: "concentrator"}, nil)
	response := httptest.NewRecorder()
	d.handleStatus(response, httptest.NewRequest("GET", "/v1/status", nil))
	var status daemonapi.DaemonStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	identity := status.Resources[0].PPPoEIdentity
	if identity == nil || identity.Username != "user" || identity.AuthMethod != "chap" || identity.ServiceName != "service" || identity.ACName != "concentrator" || identity.MTU != 1454 || identity.LCPEchoInterval != 30 {
		t.Fatalf("effective public identity missing: %+v", identity)
	}
	if strings.Contains(response.Body.String(), "never-emit-secret") {
		t.Fatal("credential material leaked through identity")
	}
}
