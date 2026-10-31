// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

type recordedNativeSAs struct {
	states  []saReceipt
	deleted []saReceipt
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
	owned := saReceipt{ID: saID{Source: "198.51.100.1", Destination: "203.0.113.1", Protocol: "ESP", SPI: 0xc77c3da9}, Scope: "fixture-kernel", ReqID: 1, AddTime: 123, Fingerprint: "owned-generation"}
	foreign := owned
	foreign.ID.SPI = 0xcb967070
	foreign.Fingerprint = "foreign-generation"
	native := &recordedNativeSAs{states: []saReceipt{owned, foreign}}
	if err := writeReceipt(directory, receipt{Version: 1, Connections: []connectionReceipt{{Name: "routerd-site", Hash: "previous", SAs: []saReceipt{owned}}}}); err != nil {
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
