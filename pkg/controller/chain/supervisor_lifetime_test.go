// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSupervisorRetainsOwnershipUntilCompletion(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	old := supervisedDaemonProcesses
	t.Cleanup(func() { supervisedDaemonProcesses = old })
	inspected := make(chan struct{})
	var once sync.Once
	supervisedDaemonProcesses = func(supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
		once.Do(func() { close(inspected) })
		return nil, nil
	}
	spec := supervisedDaemonSpec{ResourceName: "gone", Binary: "routerd-pppoe-client", Args: []string{"daemon", "--resource", "gone"}, OwnerToken: "old"}
	canceled, completion := make(chan struct{}), make(chan struct{})
	state := supervisedDaemonState{Spec: spec, Cancel: func() { close(canceled) }, Done: completion}
	key := supervisedDaemonKey(spec.Binary, spec.ResourceName)
	r := &Runner{clientDaemonStates: map[string]supervisedDaemonState{key: state}}
	finished := make(chan struct{})
	go func() { r.reconcileSupervisedDaemonSpecs(t.Context(), nil, nil); close(finished) }()
	<-canceled
	<-inspected
	r.supervisedMu.Lock()
	_, retained := r.clientDaemonStates[key]
	r.supervisedMu.Unlock()
	if !retained {
		t.Fatal("ownership deleted before supervisor acknowledged completion")
	}
	close(completion)
	<-finished
	if len(r.clientDaemonStates) != 0 {
		t.Fatalf("completed owner retained: %+v", r.clientDaemonStates)
	}
}

func TestSupervisorReadFailureRetainsMarkerAndSuppressesReplacement(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	oldProcesses, oldReady := supervisedDaemonProcesses, supervisedDaemonSocketReady
	t.Cleanup(func() { supervisedDaemonProcesses, supervisedDaemonSocketReady = oldProcesses, oldReady })
	supervisedDaemonProcesses = func(supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
		return nil, errors.New("process observation unavailable")
	}
	supervisedDaemonSocketReady = func(string) bool { return false }
	prior := supervisedDaemonSpec{ResourceName: "wan", Binary: "routerd-pppoe-client", Args: []string{"daemon", "--resource", "wan", "--interface", "old"}, OwnerToken: "prior"}
	marker := supervisedDaemonMarker{Version: 1, Binary: prior.Binary, Resource: prior.ResourceName, SpecHash: supervisedDaemonSpecHash(prior), OwnerToken: prior.OwnerToken, PID: 536870911}
	if err := writeSupervisedDaemonMarker(marker); err != nil {
		t.Fatal(err)
	}
	desired := prior
	desired.Args = []string{"daemon", "--resource", "wan", "--interface", "new"}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := &Runner{}
	t.Cleanup(func() {
		cancel()
		r.supervisedMu.Lock()
		var states []supervisedDaemonState
		for _, state := range r.clientDaemonStates {
			states = append(states, state)
		}
		r.supervisedMu.Unlock()
		for _, state := range states {
			select {
			case <-state.Done:
			case <-time.After(3 * time.Second):
				t.Error("fixture supervisor did not exit")
			}
		}
	})
	r.reconcileSupervisedDaemonSpecs(ctx, nil, []supervisedDaemonSpec{desired})
	if len(r.clientDaemonStates) != 0 {
		t.Fatal("replacement started after failed ownership read")
	}
	markers, err := readSupervisedDaemonMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if got := markers[supervisedDaemonKey(marker.Binary, marker.Resource)]; got != marker {
		t.Fatalf("failed cleanup discarded ownership: %+v", got)
	}
}

func TestSupervisorDHCPv6IdentityChangeWaitsForOldSocketOwner(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	oldProcesses, oldReady := supervisedDaemonProcesses, supervisedDaemonSocketReady
	t.Cleanup(func() { supervisedDaemonProcesses, supervisedDaemonSocketReady = oldProcesses, oldReady })
	supervisedDaemonProcesses = func(supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
		return nil, errors.New("old owner cannot be observed")
	}
	supervisedDaemonSocketReady = func(string) bool { return false }
	marker := supervisedDaemonMarker{Version: 1, Binary: "routerd-dhcpv6-client", Resource: "pd", SpecHash: "old", OwnerToken: "prior", PID: 536870911}
	if err := writeSupervisedDaemonMarker(marker); err != nil {
		t.Fatal(err)
	}
	// Removing PD leaves the address-only resource on the same interface. Its
	// supervisor key changes, but it still needs the old owner's UDP/546 socket.
	desired := supervisedDaemonSpec{ResourceName: "DHCPv6Address/wan", Binary: marker.Binary, Args: []string{"daemon", "--resource", "DHCPv6Address/wan", "--interface", "wan0"}}
	ctx, cancel := context.WithCancel(t.Context())
	r := &Runner{}
	t.Cleanup(func() {
		cancel()
		for _, state := range r.clientDaemonStates {
			<-state.Done
		}
	})
	if err := r.reconcileSupervisedDaemonSpecs(ctx, nil, []supervisedDaemonSpec{desired}); err == nil {
		t.Fatal("unknown old owner accepted as stopped")
	}
	if len(r.clientDaemonStates) != 0 {
		t.Fatal("new association worker started while old interface owner was unknown")
	}
}

func TestSupervisorWaitsForOwnedInitializingDaemonWithoutReadySocket(t *testing.T) {
	useSupervisedDaemonMarkerTestRoot(t)
	oldProcesses, oldReady := supervisedDaemonProcesses, supervisedDaemonSocketReady
	t.Cleanup(func() { supervisedDaemonProcesses, supervisedDaemonSocketReady = oldProcesses, oldReady })
	spec := supervisedDaemonSpec{ResourceName: "wan", Binary: "routerd-pppoe-client", Args: []string{"daemon", "--resource", "wan"}, OwnerToken: "initializing"}
	marker := supervisedDaemonMarker{Version: 1, Binary: spec.Binary, Resource: spec.ResourceName, SpecHash: supervisedDaemonSpecHash(spec), OwnerToken: spec.OwnerToken, PID: 536870911}
	if err := writeSupervisedDaemonMarker(marker); err != nil {
		t.Fatal(err)
	}
	inspected := make(chan struct{})
	var once sync.Once
	supervisedDaemonProcesses = func(supervisedDaemonMarker) ([]supervisedDaemonProcess, error) {
		once.Do(func() { close(inspected) })
		return []supervisedDaemonProcess{{PID: marker.PID, Command: "/usr/local/sbin/routerd-pppoe-client daemon --resource wan --supervisor-owner initializing"}}, nil
	}
	supervisedDaemonSocketReady = func(string) bool { return false }
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := (&Runner{}).startSupervisedDaemonSpec(ctx, nil, spec)
	select {
	case <-inspected:
	case <-ctx.Done():
		t.Fatal("owned process was not inspected")
	}
	cancel()
	<-done
	current, err := readSupervisedDaemonMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if current[supervisedDaemonKey(marker.Binary, marker.Resource)] != marker {
		t.Fatal("initializing process was replaced")
	}
}

func TestSupervisorCancellationAllowsActualNativeGracefulExit(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	useSupervisedDaemonMarkerTestRoot(t)
	dir := t.TempDir()
	ready, stopped := filepath.Join(dir, "ready"), filepath.Join(dir, "stopped")
	trap := "printf stopped > " + shellQuote(stopped) + "; exit 0"
	script := fmt.Sprintf("#!/bin/sh\ntrap %s TERM\nprintf ready > %s\nwhile :; do sleep 0.05; done\n", shellQuote(trap), shellQuote(ready))
	binary := "routerd-pppoe-client"
	if err := os.WriteFile(filepath.Join(dir, binary), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	spec := supervisedDaemonSpec{ResourceName: "fixture", Binary: binary, Args: []string{"daemon", "--resource", "fixture"}, OwnerToken: "term"}
	if err := writeSupervisedDaemonMarker(supervisedDaemonMarker{Version: 1, Binary: binary, Resource: spec.ResourceName, SpecHash: supervisedDaemonSpecHash(spec), OwnerToken: spec.OwnerToken}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := (&Runner{}).startSupervisedDaemonSpec(ctx, nil, spec)
	t.Cleanup(func() { cancel(); <-done })
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(time.Millisecond) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture did not start")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not acknowledge graceful stop")
	}
	if b, err := os.ReadFile(stopped); err != nil || string(b) != "stopped" {
		t.Fatalf("daemon was killed before graceful cleanup: %q %v", b, err)
	}
}
