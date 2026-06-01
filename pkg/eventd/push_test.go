// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"testing"

	"github.com/imksoo/routerd/pkg/federation"
)

func TestPeerMatchesControlPlaneLivenessBypassesSubjectPrefixes(t *testing.T) {
	peer := PeerConfig{SubjectPrefixes: []string{"10.77.60."}}
	ev := federation.Event{
		Type:    federation.MobilityMemberHeartbeatType,
		Subject: "cloudedge/aws-router-a",
	}
	if !peerMatches(peer, ev) {
		t.Fatalf("heartbeat event did not match peer with data-plane subject prefix")
	}
}

func TestPeerMatchesControlPlaneLivenessRespectsTypes(t *testing.T) {
	peer := PeerConfig{Types: []string{"routerd.client.ipv4.observed"}, SubjectPrefixes: []string{"10.77.60."}}
	ev := federation.Event{
		Type:    federation.MobilityMemberHeartbeatType,
		Subject: "cloudedge/aws-router-a",
	}
	if peerMatches(peer, ev) {
		t.Fatalf("heartbeat event matched peer with excluding type filter")
	}
}

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
