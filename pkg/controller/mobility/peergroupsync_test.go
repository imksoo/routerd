// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/wireguard"
)

func TestPeerGroupSyncServerReturnsPublishedGroups(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	writePeerGroupPart(t, store, TransportPeerGroupDynamicSource("rr"), "svnet1-rrs", []api.SAMTransportPeerSpec{{
		NodeRef:        "rr-rt01",
		RemoteEndpoint: "10.252.0.1",
	}}, now)

	req := httptest.NewRequest(http.MethodGet, peerGroupSyncPath+"?name=svnet1-rrs", nil)
	rr := httptest.NewRecorder()
	server := &PeerGroupSyncServer{Store: store, Now: func() time.Time { return now }}
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", peerGroupSyncPath, rr.Code, rr.Body.String())
	}
	var payload PeerGroupSyncResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.PeerGroups) != 1 || payload.PeerGroups[0].Metadata.Name != "svnet1-rrs" {
		t.Fatalf("peer groups = %#v, want svnet1-rrs", payload.PeerGroups)
	}
	metadata := syncMetadataForResource(payload.PeerGroups[0], "rr-a")
	if metadata.Revision == 0 || metadata.ValidUntil.IsZero() {
		t.Fatalf("resource metadata = %#v, want source generation and TTL", metadata)
	}
	firstRevision, firstValidUntil, firstDigest := metadata.Revision, metadata.ValidUntil, resourceDigest(payload.PeerGroups[0])
	server.Now = func() time.Time { return now.Add(time.Minute) }
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, peerGroupSyncPath+"?name=svnet1-rrs", nil))
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	metadata = syncMetadataForResource(payload.PeerGroups[0], "rr-a")
	if metadata.Revision != firstRevision || !metadata.ValidUntil.Equal(firstValidUntil) || resourceDigest(payload.PeerGroups[0]) != firstDigest {
		t.Fatalf("unchanged resource changed sync metadata: %#v digest=%s", metadata, resourceDigest(payload.PeerGroups[0]))
	}
}

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

func TestPeerGroupSyncClientFetchesAndStoresGroup(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 1, 0, 0, time.UTC)
	store := testStore(t, now)
	srv := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != peerGroupSyncPath {
			t.Fatalf("request path = %s, want %s", r.URL.Path, peerGroupSyncPath)
		}
		_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{PeerGroups: []api.Resource{samPeerGroupResource("svnet1-rrs", []api.SAMTransportPeerSpec{{
			NodeRef:        "rr-rt01",
			RemoteEndpoint: "10.252.0.1",
		}})}})
	}))
	defer srv.Close()
	addr, port := serverAddr(t, srv)

	client := &PeerGroupSyncClient{
		Store:      store,
		HTTPClient: srv.Client(),
		Port:       port,
		Now:        func() time.Time { return now },
		Discover: func(context.Context, *api.Router, string) ([]netip.Addr, error) {
			return []netip.Addr{addr}, nil
		},
	}
	group, ok, err := client.SyncPeerGroup(context.Background(), nil, "wg-svnet1", "svnet1-rrs")
	if err != nil {
		t.Fatalf("SyncPeerGroup: %v", err)
	}
	if !ok || len(group.Nodes) != 1 || group.Nodes[0].NodeRef != "rr-rt01" {
		t.Fatalf("synced group = %#v ok=%v, want rr node", group, ok)
	}
	part := latestPart(t, store, PeerGroupSyncDynamicSource("svnet1-rrs"))
	resources := decodeResources(t, part.ResourcesJSON)
	if len(resources) != 1 || resources[0].Kind != "SAMPeerGroup" || resources[0].Metadata.Name != "svnet1-rrs" {
		t.Fatalf("stored resources = %#v, want SAMPeerGroup/svnet1-rrs", resources)
	}
	for _, key := range []string{"routerd.net/sync-publisher", "routerd.net/sync-revision", "routerd.net/sync-digest"} {
		if resources[0].Metadata.Annotations[key] != "" {
			t.Fatalf("stored resource retained write-only %s annotation: %#v", key, resources[0].Metadata.Annotations)
		}
	}
}

func TestSAMTransportProfilePeersFromSyncResolvesMissingGroup(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 2, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	router.Spec.Resources[0].Spec = spec
	srv := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{PeerGroups: []api.Resource{samPeerGroupResource("svnet1-rrs", []api.SAMTransportPeerSpec{{
			NodeRef:        "rr-rt01",
			RemoteEndpoint: "10.252.0.1",
		}})}})
	}))
	defer srv.Close()
	addr, port := serverAddr(t, srv)

	controller := TransportController{
		Router: router,
		Store:  store,
		PeerGroupSync: &PeerGroupSyncClient{
			Store:      store,
			HTTPClient: srv.Client(),
			Port:       port,
			Now:        func() time.Time { return now },
			Discover: func(context.Context, *api.Router, string) ([]netip.Addr, error) {
				return []netip.Addr{addr}, nil
			},
		},
		Now: func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "10.252.0.1" {
		t.Fatalf("synced tunnel remote = %q, want 10.252.0.1", tunnel.Remote)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Derived" {
		t.Fatalf("status phase = %#v, want Derived status=%#v", status["phase"], status)
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

func serverAddr(t *testing.T, srv *httptest.Server) (netip.Addr, int) {
	t.Helper()
	host, portText, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if host == "" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		t.Fatalf("ParseAddr(%q): %v", host, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi(%q): %v", portText, err)
	}
	return addr, port
}

// newIPv4TestServer avoids depending on an IPv6 loopback listener.  The sync
// protocol is IP-family independent, while restricted CI sandboxes commonly
// disallow creating a TCP6 listener.
func newIPv4TestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen IPv4 test server: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func addrStrings(addrs []netip.Addr) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}
