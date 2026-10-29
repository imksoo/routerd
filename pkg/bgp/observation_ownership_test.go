// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"reflect"
	"testing"
)

func TestDiffDoesNotRewritePublishedObservationSlices(t *testing.T) {
	current := State{Peers: []Peer{{Address: "a", State: "Idle"}, {Address: "a", State: "Established"}, {Address: "z", State: "Idle"}}}
	original := append([]Peer(nil), current.Peers...)
	_ = Diff(State{}, current)
	if !reflect.DeepEqual(current.Peers, original) {
		t.Fatalf("diff rewrote its caller's observation: %#v", current.Peers)
	}
}
