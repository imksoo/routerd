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
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/wireguard"
)

func TestPeerGroupSyncServerReturnsPublishedGroups(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	writePeerGroupPart(t, store, TransportPeerGroupDynamicSource("rr"), "svnet1-rrs", []api.SAMTransportPeerSpec{{
		NodeRef:        "rr-rt01",
		RemoteEndpoint: "10.252.0.1",
	}}, now)

	req := httptest.NewRequest(http.MethodGet, peerGroupSyncPath, nil)
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
}

func TestPeerGroupSyncServerReturnsPublishedMemberSets(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 0, 30, 0, time.UTC)
	store := testStore(t, now)
	writeMemberSetPart(t, store, MobilityMemberSetDynamicSource("svnet1"), "svnet1", []api.MobilityMemberSetMember{{
		NodeRef: "pve-rt01",
		Site:    "pve01",
		Role:    "onprem",
	}}, now)

	req := httptest.NewRequest(http.MethodGet, memberSetSyncPath, nil)
	rr := httptest.NewRecorder()
	server := &PeerGroupSyncServer{Store: store, Now: func() time.Time { return now }}
	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", memberSetSyncPath, rr.Code, rr.Body.String())
	}
	var payload MemberSetSyncResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.MemberSets) != 1 || payload.MemberSets[0].Metadata.Name != "svnet1" {
		t.Fatalf("member sets = %#v, want svnet1", payload.MemberSets)
	}
}

func TestPeerGroupSyncClientFetchesAndStoresGroup(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 1, 0, 0, time.UTC)
	store := testStore(t, now)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		Discover: func(context.Context, *api.Router, string) ([]SAMSyncEndpoint, error) {
			return []SAMSyncEndpoint{{Addr: addr, Source: "sam-route-reflector", Resource: "SAMNodeSet/fabric", NodeRef: "rr-rt01"}}, nil
		},
	}
	group, ok, err := client.SyncPeerGroup(context.Background(), nil, "wg-svnet1", "svnet1-rrs")
	if err != nil {
		t.Fatalf("SyncPeerGroup: %v", err)
	}
	if !ok || len(group.Peers) != 1 || group.Peers[0].NodeRef != "rr-rt01" {
		t.Fatalf("synced group = %#v ok=%v, want rr peer", group, ok)
	}
	part := latestPart(t, store, PeerGroupSyncDynamicSource("svnet1-rrs"))
	resources := decodeResources(t, part.ResourcesJSON)
	if len(resources) != 1 || resources[0].Kind != "SAMPeerGroup" || resources[0].Metadata.Name != "svnet1-rrs" {
		t.Fatalf("stored resources = %#v, want SAMPeerGroup/svnet1-rrs", resources)
	}
}

func TestPeerGroupSyncClientReportsReachableEndpointWithoutGroup(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 1, 15, 0, time.UTC)
	store := testStore(t, now)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != peerGroupSyncPath {
			t.Fatalf("request path = %s, want %s", r.URL.Path, peerGroupSyncPath)
		}
		_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{})
	}))
	defer srv.Close()
	addr, port := serverAddr(t, srv)

	client := &PeerGroupSyncClient{
		Store:      store,
		HTTPClient: srv.Client(),
		Port:       port,
		Now:        func() time.Time { return now },
		Discover: func(context.Context, *api.Router, string) ([]SAMSyncEndpoint, error) {
			return []SAMSyncEndpoint{{Addr: addr, Source: "sam-route-reflector", Resource: "SAMNodeSet/fabric", NodeRef: "rr-rt01"}}, nil
		},
	}
	_, ok, err := client.SyncPeerGroup(context.Background(), nil, "wg-svnet1", "svnet1-rrs")
	if err == nil || !strings.Contains(err.Error(), "SAMPeerGroup/svnet1-rrs was not published") || !strings.Contains(err.Error(), "source=sam-route-reflector") || !strings.Contains(err.Error(), "publishPeerGroup: true") {
		t.Fatalf("SyncPeerGroup error = %v, want not-published publisher hint", err)
	}
	if ok {
		t.Fatalf("SyncPeerGroup ok = true, want false")
	}
}

func TestPeerGroupSyncClientFetchesAndStoresMemberSet(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 1, 30, 0, time.UTC)
	store := testStore(t, now)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != memberSetSyncPath {
			t.Fatalf("request path = %s, want %s", r.URL.Path, memberSetSyncPath)
		}
		_ = json.NewEncoder(w).Encode(MemberSetSyncResponse{MemberSets: []api.Resource{mobilityMemberSetResource("svnet1", []api.MobilityMemberSetMember{{
			NodeRef: "pve-rt01",
			Site:    "pve01",
			Role:    "onprem",
		}})}})
	}))
	defer srv.Close()
	addr, port := serverAddr(t, srv)

	client := &PeerGroupSyncClient{
		Store:      store,
		HTTPClient: srv.Client(),
		Port:       port,
		Now:        func() time.Time { return now },
		Discover: func(context.Context, *api.Router, string) ([]SAMSyncEndpoint, error) {
			return []SAMSyncEndpoint{{Addr: addr, Source: "sam-route-reflector", Resource: "SAMNodeSet/fabric", NodeRef: "pve-rt01"}}, nil
		},
	}
	set, ok, err := client.SyncMemberSet(context.Background(), nil, "svnet1")
	if err != nil {
		t.Fatalf("SyncMemberSet: %v", err)
	}
	if !ok || len(set.Members) != 1 || set.Members[0].NodeRef != "pve-rt01" {
		t.Fatalf("synced member set = %#v ok=%v, want pve member", set, ok)
	}
	part := latestPart(t, store, MemberSetSyncDynamicSource("svnet1"))
	resources := decodeResources(t, part.ResourcesJSON)
	if len(resources) != 1 || resources[0].Kind != "MobilityMemberSet" || resources[0].Metadata.Name != "svnet1" {
		t.Fatalf("stored resources = %#v, want MobilityMemberSet/svnet1", resources)
	}
}

func TestPeerGroupSyncClientReportsReachableEndpointWithoutMemberSet(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 1, 45, 0, time.UTC)
	store := testStore(t, now)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != memberSetSyncPath {
			t.Fatalf("request path = %s, want %s", r.URL.Path, memberSetSyncPath)
		}
		_ = json.NewEncoder(w).Encode(MemberSetSyncResponse{})
	}))
	defer srv.Close()
	addr, port := serverAddr(t, srv)

	client := &PeerGroupSyncClient{
		Store:      store,
		HTTPClient: srv.Client(),
		Port:       port,
		Now:        func() time.Time { return now },
		Discover: func(context.Context, *api.Router, string) ([]SAMSyncEndpoint, error) {
			return []SAMSyncEndpoint{{Addr: addr, Source: "sam-route-reflector", Resource: "SAMNodeSet/fabric", NodeRef: "rr-rt01"}}, nil
		},
	}
	_, ok, err := client.SyncMemberSet(context.Background(), nil, "cloudedge")
	if err == nil || !strings.Contains(err.Error(), "MobilityMemberSet/cloudedge was not published") || !strings.Contains(err.Error(), "source=sam-route-reflector") {
		t.Fatalf("SyncMemberSet error = %v, want not-published publisher hint", err)
	}
	if ok {
		t.Fatalf("SyncMemberSet ok = true, want false")
	}
}

func TestMobilityMembersFromSyncReportsNotPublishedMemberSet(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 1, 50, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.MembersFrom = []api.MobilityMembersSourceSpec{{Resource: "MobilityMemberSet/cloudedge"}}
	router := planningRouterForNode("onprem-router", spec)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(MemberSetSyncResponse{})
	}))
	defer srv.Close()
	addr, port := serverAddr(t, srv)

	resolved, err := (mobilityMemberResolver{
		Router: router,
		Sync: &PeerGroupSyncClient{
			Store:      store,
			HTTPClient: srv.Client(),
			Port:       port,
			Now:        func() time.Time { return now },
			Discover: func(context.Context, *api.Router, string) ([]SAMSyncEndpoint, error) {
				return []SAMSyncEndpoint{{Addr: addr, Source: "sam-route-reflector", Resource: "SAMNodeSet/fabric", NodeRef: "rr-rt01"}}, nil
			},
		},
	}).resolve(context.Background(), spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.MembersFrom) != 1 {
		t.Fatalf("membersFrom = %#v, want one source", resolved.MembersFrom)
	}
	reason := resolved.MembersFrom[0].Reason
	if !strings.Contains(reason, "MobilityMemberSet/cloudedge was not published") || !strings.Contains(reason, "source=sam-route-reflector") {
		t.Fatalf("membersFrom reason = %q, want not-published publisher hint", reason)
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			Discover: func(context.Context, *api.Router, string) ([]SAMSyncEndpoint, error) {
				return []SAMSyncEndpoint{{Addr: addr, Source: "sam-route-reflector", Resource: "SAMNodeSet/fabric", NodeRef: "rr-rt01"}}, nil
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

func TestSAMTransportProfilePeersFromSyncReportsNotPublishedGroup(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 2, 15, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	router.Spec.Resources[0].Spec = spec
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{})
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
			Discover: func(context.Context, *api.Router, string) ([]SAMSyncEndpoint, error) {
				return []SAMSyncEndpoint{{Addr: addr, Source: "sam-route-reflector", Resource: "SAMNodeSet/fabric", NodeRef: "rr-rt01"}}, nil
			},
		},
		Now: func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Pending" {
		t.Fatalf("status phase = %#v, want Pending status=%#v", status["phase"], status)
	}
	peersFrom, ok := status["peersFrom"].([]any)
	if !ok || len(peersFrom) != 1 {
		t.Fatalf("status peersFrom = %#v, want one source", status["peersFrom"])
	}
	source, ok := peersFrom[0].(map[string]any)
	if !ok {
		t.Fatalf("status peersFrom[0] = %#v, want object", peersFrom[0])
	}
	reason, _ := source["reason"].(string)
	if !strings.Contains(reason, "SAMPeerGroup/svnet1-rrs was not published") || !strings.Contains(reason, "source=sam-route-reflector") || !strings.Contains(reason, "publishPeerGroup: true") {
		t.Fatalf("peersFrom reason = %q, want not-published publisher hint", reason)
	}
}

func TestDiscoverWireGuardSAMSyncEndpointsPrefersSAMRouteReflectors(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "azure-a"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
				Metadata: api.ObjectMeta{Name: "wg-hybrid"},
				Spec: api.WireGuardInterfaceSpec{
					SelfNodeRef: "azure-a",
					PeersFrom:   []api.WireGuardPeersSourceSpec{{Resource: "SAMNodeSet/fabric"}},
				},
			},
			samNodeSetResource("fabric", []api.SAMNodeSpec{
				{
					NodeRef:        "onprem",
					RouteReflector: true,
					SAMEndpoint:    "10.99.0.1",
				},
				{
					NodeRef:     "azure-b",
					SAMEndpoint: "10.99.0.3",
				},
				{
					NodeRef:        "azure-a",
					RouteReflector: true,
					SAMEndpoint:    "10.99.0.2",
				},
			}),
		}},
	}

	addrs, err := DiscoverWireGuardSAMSyncEndpoints(context.Background(), router, "wg-hybrid")
	if err != nil {
		t.Fatalf("DiscoverWireGuardSAMSyncEndpoints: %v", err)
	}
	if got := addrStrings(addrs); len(got) != 1 || got[0] != "10.99.0.1" {
		t.Fatalf("sync endpoints = %v, want only route-reflector onprem 10.99.0.1", got)
	}
	endpoints, err := DiscoverWireGuardSAMSyncEndpointDetails(context.Background(), router, "wg-hybrid")
	if err != nil {
		t.Fatalf("DiscoverWireGuardSAMSyncEndpointDetails: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("sync endpoint details = %#v, want one route-reflector endpoint", endpoints)
	}
	if endpoints[0].Source != "sam-route-reflector" || endpoints[0].Resource != "SAMNodeSet/fabric" || endpoints[0].NodeRef != "onprem" {
		t.Fatalf("sync endpoint detail = %#v, want route-reflector source/resource/nodeRef", endpoints[0])
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
	record, err := dynamicPartRecord(part)
	if err != nil {
		t.Fatalf("dynamicPartRecord: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
}

func writeMemberSetPart(t *testing.T, store peerGroupSyncStore, source, name string, members []api.MobilityMemberSetMember, now time.Time) {
	t.Helper()
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     source,
			Generation: dynamicGeneration,
			ObservedAt: now,
			ExpiresAt:  now.Add(DefaultLeaseTTL),
			Resources:  []api.Resource{mobilityMemberSetResource(name, members)},
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := dynamicPartRecord(part)
	if err != nil {
		t.Fatalf("dynamicPartRecord: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
}

func mobilityMemberSetResource(name string, members []api.MobilityMemberSetMember) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityMemberSet"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.MobilityMemberSetSpec{Members: members},
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

func addrStrings(addrs []netip.Addr) []string {
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}
