// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

type recordedNativeSAs struct {
	states  []saReceipt
	deleted []saReceipt
}

func (n *recordedNativeSAs) Scope(context.Context) (string, error) { return "fixture-kernel", nil }

func (n *recordedNativeSAs) ForEndpoints(_ context.Context, local, remote string) ([]saID, error) {
	var result []saID
	for _, state := range n.states {
		id := state.ID
		if (id.Source == local && id.Destination == remote) || (id.Source == remote && id.Destination == local) {
			result = append(result, id)
		}
	}
	return result, nil
}

func (n *recordedNativeSAs) Observe(_ context.Context, id saID) (saReceipt, bool, error) {
	for _, state := range n.states {
		if state.ID == id {
			return state, true, nil
		}
	}
	return saReceipt{}, false, nil
}
func (n *recordedNativeSAs) Delete(_ context.Context, expected saReceipt) error {
	for i, state := range n.states {
		if state.ID == expected.ID {
			if state != expected {
				return fmt.Errorf("native SA was replaced")
			}
			n.deleted = append(n.deleted, state)
			n.states = slices.Delete(n.states, i, i+1)
			return nil
		}
	}
	return nil
}

func TestScopedVICIWithdrawalRemovesRecordedKernelSAAfterProviderLoss(t *testing.T) {
	directory := t.TempDir()
	_, session := newVICIEndpoint(t, directory)
	owned := saReceipt{ID: saID{Source: "198.51.100.1", Destination: "203.0.113.1", Protocol: "ESP", SPI: 0xc77c3da9}, Scope: "fixture-kernel", ReqID: 1, AddTime: 123, Fingerprint: strings.Repeat("1", 64)}
	foreign := owned
	foreign.ID.SPI = 0xcb967070
	foreign.ID.Destination = "203.0.113.254"
	foreign.Fingerprint = strings.Repeat("2", 64)
	native := &recordedNativeSAs{states: []saReceipt{owned, foreign}}
	if err := writeReceipt(directory, receipt{Version: 1, Connections: []connectionReceipt{{Name: "routerd-site", Hash: "previous", Local: owned.ID.Source, Remote: owned.ID.Destination, Scope: owned.Scope, SAs: []saReceipt{owned}}}}); err != nil {
		t.Fatal(err)
	}
	controller := Controller{Session: session, Directory: directory, nativeSAs: native}
	if err := controller.Reconcile(t.Context(), &api.Router{}); err != nil {
		t.Fatal(err)
	}
	if len(native.deleted) != 1 || native.deleted[0] != owned {
		t.Fatalf("VICI has no SA but recorded kernel SA remains: deleted=%+v states=%+v", native.deleted, native.states)
	}
	if len(native.states) != 1 || native.states[0] != foreign {
		t.Fatalf("foreign SA changed: %+v", native.states)
	}
	state, err := readReceipt(directory)
	if err != nil || state.Pending || len(state.Connections) != 0 {
		t.Fatalf("withdrawal receipt=%+v err=%v", state, err)
	}
}

func viciSAEvent(local, remote string, in, out uint32) []byte {
	section := func(name string, body []byte) []byte {
		packet := append([]byte{1, byte(len(name))}, []byte(name)...)
		return append(append(packet, body...), 2)
	}
	child := viciValue("protocol", "ESP")
	child = append(child, viciValue("reqid", "1")...)
	child = append(child, viciValue("spi-in", fmt.Sprintf("%08x", in))...)
	child = append(child, viciValue("spi-out", fmt.Sprintf("%08x", out))...)
	ike := append(viciValue("local-host", local), viciValue("remote-host", remote)...)
	ike = append(ike, section("child-sas", section("net-1", child))...)
	return section("routerd-site", ike)
}

func TestScopedVICIRecordsObservedSAsAcrossReopenAndProviderGeneration(t *testing.T) {
	directory := t.TempDir()
	endpoint, session := newVICIEndpoint(t, directory)
	native := &recordedNativeSAs{}
	router := ipsecTestRouter("203.0.113.1", "first-secret")
	controller := Controller{Session: session, Directory: directory, nativeSAs: native}
	if err := controller.Reconcile(t.Context(), router); err != nil {
		t.Fatal(err)
	}
	oldIn := saReceipt{ID: saID{Source: "203.0.113.1", Destination: "198.51.100.1", Protocol: "ESP", SPI: 0xc77c3da9}, Scope: "fixture-kernel", ReqID: 1, AddTime: 123, Fingerprint: strings.Repeat("1", 64)}
	oldOut := oldIn
	oldOut.ID.Source, oldOut.ID.Destination = "198.51.100.1", "203.0.113.1"
	oldOut.ID.SPI = 0xcb967070
	native.states = []saReceipt{oldIn, oldOut}
	endpoint.mu.Lock()
	endpoint.saEvents = map[string][]byte{"routerd-site": viciSAEvent("198.51.100.1", "203.0.113.1", oldIn.ID.SPI, oldOut.ID.SPI)}
	endpoint.mu.Unlock()
	// Same desired config must still update the observed native recovery receipt.
	if err := controller.Reconcile(t.Context(), router); err != nil {
		t.Fatal(err)
	}
	state, err := readReceipt(directory)
	if err != nil || len(state.Connections) != 1 || len(state.Connections[0].SAs) != 2 {
		t.Fatalf("initial native receipt=%+v err=%v", state, err)
	}
	endpoint.mu.Lock()
	delete(endpoint.connections, "routerd-site")
	delete(endpoint.keys, "routerd-site")
	delete(endpoint.saEvents, "routerd-site")
	endpoint.mu.Unlock()
	reopened := Controller{Session: session, Directory: directory, nativeSAs: native}
	if err := reopened.Reconcile(t.Context(), router); err != nil {
		t.Fatal(err)
	}
	state, err = readReceipt(directory)
	if err != nil || len(state.Connections[0].SAs) != 2 {
		t.Fatalf("provider restart forgot old kernel SAs: %+v %v", state, err)
	}
	nextIn, nextOut := oldIn, oldOut
	nextIn.ID.SPI, nextOut.ID.SPI = 0xc2e2705c, 0xcf14a1ba
	nextIn.AddTime, nextOut.AddTime = 124, 124
	nextIn.Fingerprint, nextOut.Fingerprint = strings.Repeat("2", 64), strings.Repeat("2", 64)
	native.states = append(native.states, nextIn, nextOut)
	endpoint.mu.Lock()
	endpoint.saEvents["routerd-site"] = viciSAEvent("198.51.100.1", "203.0.113.1", nextIn.ID.SPI, nextOut.ID.SPI)
	endpoint.mu.Unlock()
	if err := reopened.Reconcile(t.Context(), router); err != nil {
		t.Fatal(err)
	}
	state, err = readReceipt(directory)
	if err != nil || len(state.Connections[0].SAs) != 4 {
		t.Fatalf("new provider replaced old native authority: %+v %v", state, err)
	}
	if err := reopened.Reconcile(t.Context(), &api.Router{}); err != nil {
		t.Fatal(err)
	}
	if len(native.states) != 0 || len(native.deleted) != 4 {
		t.Fatalf("known old/new generations remain: states=%+v deletes=%+v", native.states, native.deleted)
	}
}

func TestScopedVICIUnknownSameEndpointSARetainsPendingWithoutDeletion(t *testing.T) {
	directory := t.TempDir()
	_, session := newVICIEndpoint(t, directory)
	unknown := saReceipt{ID: saID{Source: "198.51.100.1", Destination: "203.0.113.1", Protocol: "ESP", SPI: 0xc77c3da9}, Scope: "fixture-kernel", ReqID: 1, AddTime: 123, Fingerprint: strings.Repeat("1", 64)}
	native := &recordedNativeSAs{states: []saReceipt{unknown}}
	previous := receipt{Version: 1, Connections: []connectionReceipt{{Name: "routerd-site", Hash: "previous", Local: unknown.ID.Source, Remote: unknown.ID.Destination, Scope: unknown.Scope}}}
	if err := writeReceipt(directory, previous); err != nil {
		t.Fatal(err)
	}
	controller := Controller{Session: session, Directory: directory, nativeSAs: native}
	err := controller.Reconcile(t.Context(), &api.Router{})
	if err == nil || !strings.Contains(err.Error(), "without current ownership proof") {
		t.Fatalf("unknown kernel SA was reported removed: %v", err)
	}
	if len(native.deleted) != 0 || len(native.states) != 1 || native.states[0] != unknown {
		t.Fatalf("endpoint equality granted deletion authority: %+v", native)
	}
	state, readErr := readReceipt(directory)
	if readErr != nil || !state.Pending || len(state.Connections) != 1 || len(state.Connections[0].SAs) != 0 {
		t.Fatalf("unknown SA was forgotten/adopted: %+v %v", state, readErr)
	}
}

func TestScopedVICIWithdrawalPreservesAnotherRecordedConnectionAtSameEndpoints(t *testing.T) {
	directory := t.TempDir()
	_, session := newVICIEndpoint(t, directory)
	owned := saReceipt{ID: saID{Source: "198.51.100.1", Destination: "203.0.113.1", Protocol: "ESP", SPI: 0xc77c3da9}, Scope: "fixture-kernel", ReqID: 1, AddTime: 123, Fingerprint: strings.Repeat("1", 64)}
	other := owned
	other.ID.SPI = 0xcb967070
	other.Fingerprint = strings.Repeat("2", 64)
	native := &recordedNativeSAs{states: []saReceipt{owned, other}}
	desired := ipsecTestRouter("203.0.113.1", "other-secret")
	desired.Spec.Resources[0].Metadata.Name = "other"
	prepared, _, err := prepareConnections(desired, directory)
	if err != nil {
		t.Fatal(err)
	}
	retained := prepared[0].connectionReceipt
	retained.Scope, retained.SAs = "fixture-kernel", []saReceipt{other}
	previous := receipt{Version: 1, Connections: []connectionReceipt{
		{Name: "routerd-site", Hash: "previous", Local: owned.ID.Source, Remote: owned.ID.Destination, Scope: owned.Scope, SAs: []saReceipt{owned}},
		retained,
	}}
	if err := writeReceipt(directory, previous); err != nil {
		t.Fatal(err)
	}
	controller := Controller{Session: session, Directory: directory, nativeSAs: native}
	if err := controller.Reconcile(t.Context(), desired); err != nil {
		t.Fatal(err)
	}
	if len(native.deleted) != 1 || native.deleted[0] != owned || len(native.states) != 1 || native.states[0] != other {
		t.Fatalf("another owned connection changed: %+v", native)
	}
	state, err := readReceipt(directory)
	if err != nil || state.Pending || len(state.Connections) != 1 || state.Connections[0].Name != "routerd-other" || len(state.Connections[0].SAs) != 1 || state.Connections[0].SAs[0] != other {
		t.Fatalf("retained connection receipt=%+v err=%v", state, err)
	}
}
