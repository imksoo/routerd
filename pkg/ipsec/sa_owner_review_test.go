// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestReviewChangedVICIEndpointsCannotGrantKernelDeletion(t *testing.T) {
	directory := t.TempDir()
	endpoint, session := newVICIEndpoint(t, directory)
	native := &recordedNativeSAs{}
	controller := Controller{Session: session, Directory: directory, nativeSAs: native}
	if err := controller.Reconcile(t.Context(), ipsecTestRouter("203.0.113.1", "fixture-shared-key")); err != nil {
		t.Fatal(err)
	}
	before, err := readReceipt(directory)
	if err != nil || before.Pending || len(before.Connections) != 1 || len(before.Connections[0].SAs) != 0 {
		t.Fatalf("real initial acquisition: %+v %v", before, err)
	}
	foreignIn := saReceipt{ID: saID{Source: "203.0.113.254", Destination: "198.51.100.254", Protocol: "ESP", SPI: 0x10203040}, Scope: "fixture-kernel", ReqID: 1, AddTime: 456, Fingerprint: strings.Repeat("4", 64)}
	foreignOut := foreignIn
	foreignOut.ID.Source, foreignOut.ID.Destination = foreignIn.ID.Destination, foreignIn.ID.Source
	foreignOut.ID.SPI = 0x50607080
	native.states = []saReceipt{foreignIn, foreignOut}
	// A separate VICI writer rebound the existing display name to another pair.
	// No new native SA has been acknowledged or recorded by this controller.
	endpoint.mu.Lock()
	endpoint.connections["routerd-site"] = []byte("operator configuration at different endpoints")
	endpoint.saEvents = map[string][]byte{"routerd-site": viciSAEvent(foreignOut.ID.Source, foreignOut.ID.Destination, foreignIn.ID.SPI, foreignOut.ID.SPI)}
	endpoint.mu.Unlock()
	err = controller.Reconcile(t.Context(), &api.Router{})
	if err == nil || len(native.deleted) != 0 || len(native.states) != 2 {
		t.Fatalf("changed endpoint pair acquired kernel deletion authority: err=%v deletes=%d remaining=%d", err, len(native.deleted), len(native.states))
	}
	after, readErr := readReceipt(directory)
	if readErr != nil || len(after.Connections) != 1 || after.Connections[0].Local != before.Connections[0].Local || after.Connections[0].Remote != before.Connections[0].Remote || len(after.Connections[0].SAs) != 0 {
		t.Fatalf("original receipt was forgotten or foreign SAs adopted: %+v %v", after, readErr)
	}
}
