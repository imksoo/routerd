// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/wireguard"
)

func TestSelectSyncCandidateUsesRevisionNotArrivalOrder(t *testing.T) {
	older := syncCandidate{
		resource: samPeerGroupResource("svnet1-rrs", []api.SAMTransportPeerSpec{{NodeRef: "old"}}),
		meta:     syncMetadata{PublisherID: "rr-a", Revision: 10, Digest: "sha256:old"},
	}
	newer := syncCandidate{
		resource: samPeerGroupResource("svnet1-rrs", []api.SAMTransportPeerSpec{{NodeRef: "new"}}),
		meta:     syncMetadata{PublisherID: "rr-b", Revision: 11, Digest: "sha256:new"},
	}
	for _, candidates := range [][]syncCandidate{{older, newer}, {newer, older}} {
		selected, err := selectSyncCandidate(candidates, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		spec, err := selected.resource.SAMPeerGroupSpec()
		if err != nil {
			t.Fatal(err)
		}
		if spec.Nodes[0].NodeRef != "new" {
			t.Fatalf("selected node = %q", spec.Nodes[0].NodeRef)
		}
	}
}

func TestSelectSyncCandidateRejectsSameRevisionConflict(t *testing.T) {
	resource := samPeerGroupResource("svnet1-rrs", []api.SAMTransportPeerSpec{{NodeRef: "rr"}})
	_, err := selectSyncCandidate([]syncCandidate{
		{resource: resource, meta: syncMetadata{PublisherID: "rr-a", Revision: 10, Digest: "sha256:a"}},
		{resource: resource, meta: syncMetadata{PublisherID: "rr-b", Revision: 10, Digest: "sha256:b"}},
	}, time.Now())
	if err == nil {
		t.Fatal("same-revision digest conflict was accepted")
	}
}

func TestSAMTransportProfilePeersFromUsesExpiredLastKnownGoodGroup(t *testing.T) {
	observedAt := time.Date(2026, 6, 8, 10, 3, 0, 0, time.UTC)
	now := observedAt.Add(DefaultLeaseTTL + time.Second)
	store := testStore(t, observedAt)
	writePeerGroupPart(t, store, PeerGroupSyncDynamicSource("svnet1-rrs"), "svnet1-rrs", []api.SAMTransportPeerSpec{{
		NodeRef:        "rr-rt01",
		RemoteEndpoint: "10.252.0.1",
	}}, observedAt)

	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	router.Spec.Resources[0].Spec = spec

	controller := TransportController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "10.252.0.1" {
		t.Fatalf("last-known-good tunnel remote = %q, want 10.252.0.1", tunnel.Remote)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Derived" {
		t.Fatalf("status phase = %#v, want Derived status=%#v", status["phase"], status)
	}
	peersFrom, ok := status["peersFrom"].([]any)
	if !ok || len(peersFrom) != 1 {
		t.Fatalf("peersFrom status = %#v, want one source", status["peersFrom"])
	}
	source, ok := peersFrom[0].(map[string]any)
	if !ok || source["phase"] != "Stale" {
		t.Fatalf("peersFrom[0] = %#v, want phase Stale", peersFrom[0])
	}
	if source["warning"] == "" {
		t.Fatalf("peersFrom[0] = %#v, want stale warning", peersFrom[0])
	}
}

func TestFirstAllowedIPAddrsPrefersIPv4ThenIPv6(t *testing.T) {
	addrs := firstAllowedIPAddrs([]wireguard.PeerStatus{
		{AllowedIPs: []string{"fd00::2/128"}},
		{AllowedIPs: []string{"10.0.0.2/32", "fd00::3/128"}},
	})
	if got := addrStrings(addrs); len(got) != 2 || got[0] != "10.0.0.2" || got[1] != "fd00::2" {
		t.Fatalf("first allowed addrs = %v, want IPv4 then IPv6 peer addresses", got)
	}
}

func writePeerGroupPart(t *testing.T, store peerGroupSyncStore, source, name string, peers []api.SAMTransportPeerSpec, now time.Time) {
	t.Helper()
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     source,
			Generation: dynamicGeneration,
			ObservedAt: now,
			ExpiresAt:  now.Add(DefaultLeaseTTL),
			Resources:  []api.Resource{samPeerGroupResource(name, peers)},
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatalf("encode dynamic part: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
}

func addrStrings(addrs []netip.Addr) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}
