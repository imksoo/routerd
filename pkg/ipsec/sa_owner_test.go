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
