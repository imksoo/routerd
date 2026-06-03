// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"testing"

	"github.com/imksoo/routerd/pkg/federation"
)

func TestPeerMatchesAddressEventStillUsesSubjectPrefixes(t *testing.T) {
	peer := PeerConfig{SubjectPrefixes: []string{"10.77.60."}}
	ev := federation.Event{
		Type:    "routerd.client.ipv4.observed",
		Subject: "10.88.60.10/32",
	}
	if peerMatches(peer, ev) {
		t.Fatalf("address event matched nonmatching subject prefix")
	}
}

func TestPeerMatchesAddressEventMatchesSubjectPrefixes(t *testing.T) {
	peer := PeerConfig{SubjectPrefixes: []string{"10.77.60."}}
	ev := federation.Event{
		Type:    "routerd.client.ipv4.observed",
		Subject: "10.77.60.10/32",
	}
	if !peerMatches(peer, ev) {
		t.Fatalf("address event did not match matching subject prefix")
	}
}
