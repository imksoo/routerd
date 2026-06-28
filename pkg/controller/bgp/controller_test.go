// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gobgpapi "github.com/osrg/gobgp/v3/api"
	gobgpserver "github.com/osrg/gobgp/v3/pkg/server"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func mobilityOwnerStore(rows ...map[string]any) mapStore {
	values := make([]any, 0, len(rows))
	for _, row := range rows {
		values = append(values, row)
	}
	return mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
			"ownershipResolverFIBVerdicts": values,
		},
	}
}

type testGoBGPServer struct {
	*gobgpserver.BgpServer
	applied bgpdaemon.AppliedConfig
}

func (s *testGoBGPServer) AppliedConfig(context.Context) (bgpdaemon.AppliedConfig, error) {
	return s.applied, nil
}

func (s *testGoBGPServer) SaveAppliedConfig(_ context.Context, config bgpdaemon.AppliedConfig) error {
	s.applied = bgpdaemon.Normalize(config)
	return nil
}

func (s *testGoBGPServer) WatchEvent(ctx context.Context, req *gobgpapi.WatchEventRequest, fn func(*gobgpapi.WatchEventResponse) error) error {
	var callbackErr error
	err := s.BgpServer.WatchEvent(ctx, req, func(resp *gobgpapi.WatchEventResponse) {
		if callbackErr != nil {
			return
		}
		callbackErr = fn(resp)
	})
	if err != nil {
		return err
	}
	return callbackErr
}

type fakeServer struct {
	starts     int
	stops      int
	adds       int
	updates    int
	deletes    int
	paths      int
	policies   int
	assigns    int
	resets     int
	outResets  int
	hardResets int

	global           *gobgpapi.Global
	peers            map[string]*gobgpapi.Peer
	peerGroups       map[string]*gobgpapi.PeerGroup
	dynamicNeighbors map[string]*gobgpapi.DynamicNeighbor
	routes           []*gobgpapi.Destination
	applied          bgpdaemon.AppliedConfig
	deletedPathUUIDs [][]byte
	resetRequests    []*gobgpapi.ResetPeerRequest
	resetErrors      []error
	callLog          []string

	policyRequest     *gobgpapi.SetPoliciesRequest
	policyAssignment  *gobgpapi.PolicyAssignment
	definedSets       map[string]*gobgpapi.DefinedSet
	policiesByName    map[string]*gobgpapi.Policy
	assignments       map[string]*gobgpapi.PolicyAssignment
	thirdPartyNextHop string
	watchSessions     chan watchSession
	watchRequests     []*gobgpapi.WatchEventRequest
}

type watchSession struct {
	events []*gobgpapi.WatchEventResponse
	err    error
}

func (s *fakeServer) Serve() {}
func (s *fakeServer) Stop()  { s.stops++ }

func TestReconcileStopsServerWhenBGPRemoved(t *testing.T) {
	server := &fakeServer{}
	controller := Controller{
		Router:  &api.Router{},
		Store:   mapStore{},
		Server:  server,
		started: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if server.stops != 1 {
		t.Fatalf("stops = %d, want 1", server.stops)
	}
	if controller.Server != nil || controller.started {
		t.Fatalf("controller did not clear server state: server=%#v started=%t", controller.Server, controller.started)
	}
}

func TestReconcileAppliesBGPDynamicPeer(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources[:1], api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"},
		Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
		Spec: api.BGPDynamicPeerSpec{
			RouterRef:               "BGPRouter/lan",
			PeerASN:                 64512,
			Listen:                  api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"10.255.0.0/20"}},
			RouteReflectorClient:    true,
			RouteReflectorClusterID: "10.99.0.254",
			ImportPolicy:            api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}, NextHopRewrite: "peer-address"},
			ExportPolicy:            api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}},
			Timers:                  api.BGPTimersSpec{Profile: "fast"},
		},
	})
	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	group := server.peerGroups["routerd-dynamic-cloudedge-leaves"]
	if group == nil {
		t.Fatalf("dynamic peer group not added: %#v", server.peerGroups)
	}
	if !group.GetRouteReflector().GetRouteReflectorClient() || group.GetRouteReflector().GetRouteReflectorClusterId() != "10.99.0.254" {
		t.Fatalf("route reflector = %#v", group.GetRouteReflector())
	}
	if got := timersProfile(group.GetTimers().GetConfig()); got != "fast" {
		t.Fatalf("timers profile = %q, want fast", got)
	}
	if group.GetApplyPolicy().GetImportPolicy() == nil || group.GetApplyPolicy().GetExportPolicy() == nil {
		t.Fatalf("dynamic peer group policy assignments missing: %#v", group.GetApplyPolicy())
	}
	neighbor := server.dynamicNeighbors["routerd-dynamic-cloudedge-leaves|10.255.0.0/20"]
	if neighbor == nil {
		t.Fatalf("dynamic neighbor not added: %#v", server.dynamicNeighbors)
	}
}

func TestReconcileDoesNotDeleteLiveDynamicPeerFromStaticReconcile(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"},
		Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
		Spec: api.BGPDynamicPeerSpec{
			RouterRef:    "BGPRouter/lan",
			PeerASN:      64512,
			Listen:       api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"10.255.0.0/20"}},
			ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}},
		},
	})
	server := &fakeServer{peers: map[string]*gobgpapi.Peer{
		"10.255.0.21": {
			Conf:  &gobgpapi.PeerConf{NeighborAddress: "10.255.0.21", PeerAsn: 64512, PeerGroup: "routerd-dynamic-cloudedge-leaves"},
			State: &gobgpapi.PeerState{NeighborAddress: "10.255.0.21", PeerAsn: 64512, SessionState: gobgpapi.PeerState_ESTABLISHED},
		},
	}}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, call := range server.callLog {
		if call == "DeletePeer:10.255.0.21" {
			t.Fatalf("static reconcile deleted dynamic peer; call log=%#v", server.callLog)
		}
	}
	if server.peers["10.255.0.21"] == nil {
		t.Fatalf("dynamic peer removed from fake server; call log=%#v", server.callLog)
	}
}

func TestReconcileBGPPeerConsumesSAMRRSet(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Metadata.Name = "leaf-pve"
	router.Spec.Resources = append(router.Spec.Resources[:1],
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "cloudedge-rrs"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64512,
				PeersFrom: []api.BGPPeersSourceSpec{{Resource: "SAMRRSet/cloudedge-rrs"}},
				ImportPolicy: api.BGPImportPolicySpec{
					AllowedPrefixes: []string{"10.77.60.0/24"},
				},
				ExportPolicy: api.BGPExportPolicySpec{
					AllowedPrefixes: []string{"10.77.60.21/32"},
				},
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
			Metadata: api.ObjectMeta{Name: "cloudedge-rrs"},
			Spec: api.SAMRRSetSpec{
				EnrollmentPolicyRef: "SAMEnrollmentPolicy/cloudedge-leaves",
				Members: []api.SAMRRSetMember{
					{NodeRef: "aws-rr-a", Endpoint: "203.0.113.10", TunnelAddress: "10.99.0.2/32"},
					{NodeRef: "aws-rr-b", Endpoint: "203.0.113.11", TunnelAddress: "10.99.0.3/32"},
				},
			},
		},
	)
	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, address := range []string{"10.99.0.2", "10.99.0.3"} {
		if server.peers[address] == nil {
			t.Fatalf("missing BGP peer %s from SAMRRSet; peers=%v", address, server.peers)
		}
	}
}

func TestImportAllowedPrefixesIncludesDynamicPeers(t *testing.T) {
	applied := bgpdaemon.AppliedConfig{
		Global: bgpdaemon.AppliedGlobal{ImportPolicy: bgpdaemon.AppliedImportPolicy{AllowedPrefixes: []string{"10.250.0.0/24"}}},
	}
	dynamic := map[string]desiredDynamicPeer{
		"routerd-dynamic-leaves": {
			ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}},
		},
	}
	got := importAllowedPrefixesFromAppliedAndDynamic(applied, dynamic)
	var values []string
	for _, prefix := range got {
		values = append(values, prefix.Prefix.String())
	}
	if !sameStringSet(values, []string{"10.250.0.0/24", "10.77.60.0/24"}) {
		t.Fatalf("allowed prefixes = %v", values)
	}
}

func TestDynamicImportAllowedPrefixesRejectRouteLeaks(t *testing.T) {
	allowed := allowedImportPrefixesForTest(api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
	})
	for _, dst := range []*gobgpapi.Destination{
		testDestination("0.0.0.0/0", "10.99.0.11"),
		testDestination("10.20.0.0/24", "10.99.0.11"),
		testDestination("10.77.60.0/24", "10.99.0.11"),
		testDestination("10.77.60.0/25", "10.99.0.11"),
		testDestination("10.77.61.11/32", "10.99.0.11"),
	} {
		if got := fibRoutesFromDestination(dst, allowed, nil, nil); len(got) != 0 {
			t.Fatalf("route %s produced FIB routes %#v, want rejected", dst.GetPrefix(), got)
		}
	}
	got := fibRoutesFromDestination(testDestination("10.77.60.11/32", "10.99.0.11"), allowed, nil, nil)
	want := []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.11"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed dynamic route = %#v, want %#v", got, want)
	}
}

func TestSAMDynamicClaimAdmissionBindsOwnedHostRoutesToTunnelAddress(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
			Metadata: api.ObjectMeta{Name: "cloudedge-transport"},
			Spec: api.SAMTransportProfileSpec{
				SelfNodeRef: "SAMNode/rr-a",
				Mode:        "ipip",
				Encryption:  "none",
				InnerPrefix: "10.255.0.1/32",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "clients"},
			Spec: api.MobilityPoolSpec{
				Prefix:   "10.77.60.0/24",
				GroupRef: "EventGroup/cloudedge",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"},
			Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
			Spec: api.SAMEnrollmentPolicySpec{
				TransportProfileRef:   "SAMTransportProfile/cloudedge-transport",
				TunnelAddressPrefixes: []string{"10.255.0.0/24"},
				MobilityPoolRefs:      []string{"MobilityPool/clients"},
			},
		},
		samEnrollmentClaimResourceForTest("leaf-a", "10.255.0.31/32", "10.77.60.31/32"),
		samEnrollmentClaimResourceForTest("leaf-b", "10.255.0.32/32", "10.77.60.32/32"),
	)
	admission := (&Controller{Router: router}).samDynamicClaimAdmission()

	if ok, reason := admission.Admit("10.255.0.31", netip.MustParsePrefix("10.77.60.31/32")); !ok {
		t.Fatalf("leaf-a own /32 rejected: %s", reason)
	}
	for _, tt := range []struct {
		prefix string
		reason string
	}{
		{prefix: "10.77.60.32/32", reason: "prefix-not-owned-by-claim"},
		{prefix: "10.77.60.40/32", reason: "prefix-not-owned-by-claim"},
		{prefix: "10.77.60.0/24", reason: "not-exact-host-prefix"},
	} {
		if ok, reason := admission.Admit("10.255.0.31", netip.MustParsePrefix(tt.prefix)); ok || reason != tt.reason {
			t.Fatalf("leaf-a route %s admission = (%t,%q), want rejected %q", tt.prefix, ok, reason, tt.reason)
		}
	}
	if ok, reason := admission.Admit("10.255.0.99", netip.MustParsePrefix("10.77.60.31/32")); ok || reason != "no-accepted-claim-for-next-hop" {
		t.Fatalf("unknown next-hop admission = (%t,%q), want no accepted claim", ok, reason)
	}
}

func TestDynamicClaimAdmissionUsesBGPNeighborAddressInsteadOfFIBNextHop(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
			Metadata: api.ObjectMeta{Name: "cloudedge-transport"},
			Spec: api.SAMTransportProfileSpec{
				SelfNodeRef: "SAMNode/rr-a",
				Mode:        "ipip",
				Encryption:  "none",
				InnerPrefix: "10.255.0.1/32",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "clients"},
			Spec: api.MobilityPoolSpec{
				Prefix:   "10.77.60.0/24",
				GroupRef: "EventGroup/cloudedge",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"},
			Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
			Spec: api.SAMEnrollmentPolicySpec{
				TransportProfileRef:   "SAMTransportProfile/cloudedge-transport",
				TunnelAddressPrefixes: []string{"10.255.0.0/24"},
				MobilityPoolRefs:      []string{"MobilityPool/clients"},
			},
		},
		samEnrollmentClaimResourceForTest("leaf-a", "10.255.0.31/32", "10.77.60.31/32"),
		samEnrollmentClaimResourceForTest("leaf-b", "10.255.0.32/32", "10.77.60.32/32"),
	)
	admission := (&Controller{Router: router}).samDynamicClaimAdmission()
	allowed := allowedImportPrefixesForTest(api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
	})
	admit := func(prefix netip.Prefix, identityAddress, _ string, _ []string) bool {
		ok, _ := admission.Admit(identityAddress, prefix)
		return ok
	}

	got := fibRoutesFromDestination(testDestinationWithNeighbor("10.77.60.31/32", "10.255.0.32", "10.255.0.31"), allowed, nil, admit)
	want := []FIBRoute{{Prefix: "10.77.60.31/32", NextHops: []string{"10.255.0.32"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("neighbor-authorized route = %#v, want %#v", got, want)
	}
	if got := fibRoutesFromDestination(testDestinationWithNeighbor("10.77.60.32/32", "10.255.0.32", "10.255.0.31"), allowed, nil, admit); len(got) != 0 {
		t.Fatalf("leaf-a neighbor authorized leaf-b route via manipulated next-hop: %#v", got)
	}
}

func TestBGPDynamicPeerStatusReportsDiscoveredPeersAndAdmissionCounters(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"},
			Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
			Spec: api.BGPDynamicPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64512,
				Listen:    api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"10.255.0.0/24"}},
				ImportPolicy: api.BGPImportPolicySpec{
					AllowedPrefixes:        []string{"10.77.60.0/24"},
					AllowedPrefixLengthMin: 32,
					AllowedPrefixLengthMax: 32,
					NextHopRewrite:         "peer-address",
				},
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
			Metadata: api.ObjectMeta{Name: "cloudedge-transport"},
			Spec: api.SAMTransportProfileSpec{
				SelfNodeRef: "SAMNode/rr-a",
				Mode:        "ipip",
				Encryption:  "none",
				InnerPrefix: "10.255.0.1/32",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "clients"},
			Spec:     api.MobilityPoolSpec{Prefix: "10.77.60.0/24", GroupRef: "EventGroup/cloudedge"},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"},
			Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
			Spec: api.SAMEnrollmentPolicySpec{
				TransportProfileRef:   "SAMTransportProfile/cloudedge-transport",
				TunnelAddressPrefixes: []string{"10.255.0.0/24"},
				MobilityPoolRefs:      []string{"MobilityPool/clients"},
			},
		},
		samEnrollmentClaimResourceForTest("leaf-a", "10.255.0.31/32", "10.77.60.31/32"),
		samEnrollmentClaimResourceForTest("leaf-b", "10.255.0.32/32", "10.77.60.32/32"),
	)
	store := mapStore{}
	server := &fakeServer{
		peers: map[string]*gobgpapi.Peer{
			"10.255.0.31": {
				Conf: &gobgpapi.PeerConf{NeighborAddress: "10.255.0.31", PeerAsn: 64512, PeerGroup: "routerd-dynamic-cloudedge-leaves"},
				State: &gobgpapi.PeerState{
					NeighborAddress: "10.255.0.31",
					PeerAsn:         64512,
					SessionState:    gobgpapi.PeerState_ESTABLISHED,
				},
				AfiSafis: []*gobgpapi.AfiSafi{{State: &gobgpapi.AfiSafiState{Accepted: 1, Received: 2}}},
			},
		},
		routes: []*gobgpapi.Destination{
			testDestination("10.77.60.31/32", "10.255.0.31"),
			testDestination("10.77.60.32/32", "10.255.0.31"),
		},
	}
	controller := Controller{Router: router, Store: store, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "BGPDynamicPeer", "cloudedge-leaves")
	if statusInt(status["discoveredPeerCount"]) != 1 || statusInt(status["acceptedRouteCount"]) != 1 || statusInt(status["rejectedRouteCount"]) != 1 {
		t.Fatalf("dynamic peer status = %#v", status)
	}
	peers, ok := status["discoveredPeers"].([]map[string]any)
	if !ok || len(peers) != 1 {
		t.Fatalf("discoveredPeers = %#v", status["discoveredPeers"])
	}
	peer := peers[0]
	if statusString(peer["enrollmentClaimRef"]) != "SAMEnrollmentClaim/leaf-a" || statusInt(peer["acceptedRoutes"]) != 1 || statusInt(peer["rejectedRoutes"]) != 1 {
		t.Fatalf("discovered peer status = %#v", peer)
	}
	reasons, ok := status["rejectedRouteSummary"].(map[string]int)
	if !ok || reasons["prefix-not-owned-by-claim"] != 1 {
		t.Fatalf("rejectedRouteSummary = %#v", status["rejectedRouteSummary"])
	}
}

func (s *fakeServer) StopBgp(context.Context, *gobgpapi.StopBgpRequest) error { return nil }

func (s *fakeServer) GetBgp(context.Context, *gobgpapi.GetBgpRequest) (*gobgpapi.GetBgpResponse, error) {
	return &gobgpapi.GetBgpResponse{Global: s.global}, nil
}

func (s *fakeServer) StartBgp(_ context.Context, req *gobgpapi.StartBgpRequest) error {
	s.starts++
	s.global = req.GetGlobal()
	if s.peers == nil {
		s.peers = map[string]*gobgpapi.Peer{}
	}
	return nil
}

func (s *fakeServer) AddPeer(_ context.Context, req *gobgpapi.AddPeerRequest) error {
	s.adds++
	s.callLog = append(s.callLog, "AddPeer:"+req.GetPeer().GetConf().GetNeighborAddress())
	if s.peers == nil {
		s.peers = map[string]*gobgpapi.Peer{}
	}
	peer := req.GetPeer()
	address := peer.GetConf().GetNeighborAddress()
	peer.State = &gobgpapi.PeerState{
		NeighborAddress: address,
		PeerAsn:         peer.GetConf().GetPeerAsn(),
		SessionState:    gobgpapi.PeerState_ESTABLISHED,
		Messages:        &gobgpapi.Messages{Received: &gobgpapi.Message{Total: 2}, Sent: &gobgpapi.Message{Total: 3}},
	}
	for _, af := range peer.AfiSafis {
		af.State = &gobgpapi.AfiSafiState{Accepted: 1}
	}
	s.peers[address] = peer
	return nil
}

func (s *fakeServer) UpdatePeer(_ context.Context, req *gobgpapi.UpdatePeerRequest) (*gobgpapi.UpdatePeerResponse, error) {
	s.updates++
	peer := req.GetPeer()
	address := peer.GetConf().GetNeighborAddress()
	s.callLog = append(s.callLog, "UpdatePeer:"+address)
	if s.peers == nil {
		s.peers = map[string]*gobgpapi.Peer{}
	}
	peer.State = &gobgpapi.PeerState{
		NeighborAddress: address,
		PeerAsn:         peer.GetConf().GetPeerAsn(),
		SessionState:    gobgpapi.PeerState_ESTABLISHED,
		Messages:        &gobgpapi.Messages{Received: &gobgpapi.Message{Total: 2}, Sent: &gobgpapi.Message{Total: 3}},
	}
	for _, af := range peer.AfiSafis {
		af.State = &gobgpapi.AfiSafiState{Accepted: 1}
	}
	s.peers[address] = peer
	return &gobgpapi.UpdatePeerResponse{}, nil
}

func (s *fakeServer) ResetPeer(_ context.Context, req *gobgpapi.ResetPeerRequest) error {
	s.resetRequests = append(s.resetRequests, req)
	if len(s.resetErrors) > 0 {
		err := s.resetErrors[0]
		s.resetErrors = s.resetErrors[1:]
		if err != nil {
			return err
		}
	}
	if req.GetSoft() {
		switch req.GetDirection() {
		case gobgpapi.ResetPeerRequest_IN:
			s.resets++
		case gobgpapi.ResetPeerRequest_OUT:
			s.outResets++
		}
	} else {
		s.hardResets++
		if peer := s.peers[req.GetAddress()]; peer != nil {
			if peer.State == nil {
				peer.State = &gobgpapi.PeerState{NeighborAddress: req.GetAddress()}
			}
			peer.State.SessionState = gobgpapi.PeerState_IDLE
		}
	}
	return nil
}

func (s *fakeServer) AppliedConfig(context.Context) (bgpdaemon.AppliedConfig, error) {
	return s.applied, nil
}

func (s *fakeServer) SaveAppliedConfig(_ context.Context, config bgpdaemon.AppliedConfig) error {
	s.applied = bgpdaemon.Normalize(config)
	return nil
}

func (s *fakeServer) DeletePeer(_ context.Context, req *gobgpapi.DeletePeerRequest) error {
	s.deletes++
	s.callLog = append(s.callLog, "DeletePeer:"+req.GetAddress())
	delete(s.peers, req.GetAddress())
	return nil
}

func (s *fakeServer) ListPeer(_ context.Context, _ *gobgpapi.ListPeerRequest, fn func(*gobgpapi.Peer)) error {
	var keys []string
	for key := range s.peers {
		keys = append(keys, key)
	}
	for _, key := range keys {
		fn(s.peers[key])
	}
	return nil
}

func (s *fakeServer) AddPeerGroup(_ context.Context, req *gobgpapi.AddPeerGroupRequest) error {
	if s.peerGroups == nil {
		s.peerGroups = map[string]*gobgpapi.PeerGroup{}
	}
	group := req.GetPeerGroup()
	s.peerGroups[group.GetConf().GetPeerGroupName()] = group
	s.callLog = append(s.callLog, "AddPeerGroup:"+group.GetConf().GetPeerGroupName())
	return nil
}

func (s *fakeServer) DeletePeerGroup(_ context.Context, req *gobgpapi.DeletePeerGroupRequest) error {
	if s.peerGroups != nil {
		delete(s.peerGroups, req.GetName())
	}
	s.callLog = append(s.callLog, "DeletePeerGroup:"+req.GetName())
	return nil
}

func (s *fakeServer) ListPeerGroup(_ context.Context, req *gobgpapi.ListPeerGroupRequest, fn func(*gobgpapi.PeerGroup)) error {
	if s.peerGroups == nil {
		return nil
	}
	if req.GetPeerGroupName() != "" {
		if group := s.peerGroups[req.GetPeerGroupName()]; group != nil {
			fn(group)
		}
		return nil
	}
	var names []string
	for name := range s.peerGroups {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fn(s.peerGroups[name])
	}
	return nil
}

func (s *fakeServer) AddDynamicNeighbor(_ context.Context, req *gobgpapi.AddDynamicNeighborRequest) error {
	if s.dynamicNeighbors == nil {
		s.dynamicNeighbors = map[string]*gobgpapi.DynamicNeighbor{}
	}
	neighbor := req.GetDynamicNeighbor()
	key := neighbor.GetPeerGroup() + "|" + neighbor.GetPrefix()
	s.dynamicNeighbors[key] = neighbor
	s.callLog = append(s.callLog, "AddDynamicNeighbor:"+key)
	return nil
}

func (s *fakeServer) DeleteDynamicNeighbor(_ context.Context, req *gobgpapi.DeleteDynamicNeighborRequest) error {
	key := req.GetPeerGroup() + "|" + req.GetPrefix()
	if s.dynamicNeighbors != nil {
		delete(s.dynamicNeighbors, key)
	}
	s.callLog = append(s.callLog, "DeleteDynamicNeighbor:"+key)
	return nil
}

func (s *fakeServer) ListDynamicNeighbor(_ context.Context, req *gobgpapi.ListDynamicNeighborRequest, fn func(*gobgpapi.DynamicNeighbor)) error {
	if s.dynamicNeighbors == nil {
		return nil
	}
	var keys []string
	for key, neighbor := range s.dynamicNeighbors {
		if req.GetPeerGroup() != "" && neighbor.GetPeerGroup() != req.GetPeerGroup() {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fn(s.dynamicNeighbors[key])
	}
	return nil
}

func (s *fakeServer) SetPolicies(_ context.Context, req *gobgpapi.SetPoliciesRequest) error {
	s.policies++
	s.callLog = append(s.callLog, "SetPolicies")
	s.policyRequest = req
	if s.definedSets == nil {
		s.definedSets = map[string]*gobgpapi.DefinedSet{}
	}
	if s.policiesByName == nil {
		s.policiesByName = map[string]*gobgpapi.Policy{}
	}
	for _, set := range req.GetDefinedSets() {
		s.definedSets[definedSetKey(set.GetDefinedType(), set.GetName())] = set
	}
	for _, policy := range req.GetPolicies() {
		s.policiesByName[policy.GetName()] = policy
	}
	return nil
}

func (s *fakeServer) SetPolicyAssignment(_ context.Context, req *gobgpapi.SetPolicyAssignmentRequest) error {
	s.assigns++
	s.callLog = append(s.callLog, "SetPolicyAssignment")
	s.policyAssignment = req.GetAssignment()
	if s.assignments == nil {
		s.assignments = map[string]*gobgpapi.PolicyAssignment{}
	}
	s.assignments[policyAssignmentKey(req.GetAssignment().GetName(), req.GetAssignment().GetDirection())] = req.GetAssignment()
	return nil
}

func (s *fakeServer) ListDefinedSet(_ context.Context, req *gobgpapi.ListDefinedSetRequest, fn func(*gobgpapi.DefinedSet)) error {
	if s.definedSets == nil {
		return nil
	}
	if req.GetName() != "" {
		if set := s.definedSets[definedSetKey(req.GetDefinedType(), req.GetName())]; set != nil {
			fn(set)
		}
		return nil
	}
	for _, set := range s.definedSets {
		if req.GetDefinedType() == 0 || set.GetDefinedType() == req.GetDefinedType() {
			fn(set)
		}
	}
	return nil
}

func (s *fakeServer) ListPolicy(_ context.Context, req *gobgpapi.ListPolicyRequest, fn func(*gobgpapi.Policy)) error {
	if s.policiesByName == nil {
		return nil
	}
	if req.GetName() != "" {
		if policy := s.policiesByName[req.GetName()]; policy != nil {
			fn(policy)
		}
		return nil
	}
	for _, policy := range s.policiesByName {
		fn(policy)
	}
	return nil
}

func (s *fakeServer) ListPolicyAssignment(_ context.Context, req *gobgpapi.ListPolicyAssignmentRequest, fn func(*gobgpapi.PolicyAssignment)) error {
	if s.assignments == nil {
		return nil
	}
	if req.GetName() != "" || req.GetDirection() != 0 {
		if assignment := s.assignments[policyAssignmentKey(req.GetName(), req.GetDirection())]; assignment != nil {
			fn(assignment)
		}
		return nil
	}
	for _, assignment := range s.assignments {
		fn(assignment)
	}
	return nil
}

func definedSetKey(typ gobgpapi.DefinedType, name string) string {
	return strconv.Itoa(int(typ)) + "/" + strings.TrimSpace(name)
}

func policyAssignmentKey(name string, direction gobgpapi.PolicyDirection) string {
	return strconv.Itoa(int(direction)) + "/" + strings.TrimSpace(name)
}

func policyRequestHasPrefixSet(req *gobgpapi.SetPoliciesRequest, name, prefix string) bool {
	for _, set := range req.GetDefinedSets() {
		if set.GetDefinedType() != gobgpapi.DefinedType_PREFIX || set.GetName() != name {
			continue
		}
		for _, item := range set.GetPrefixes() {
			if item.GetIpPrefix() == prefix {
				return true
			}
		}
	}
	return false
}

func allowedImportPrefixesForTest(spec api.BGPImportPolicySpec) []allowedImportPrefix {
	return importAllowedPrefixesFromPolicy(spec)
}

func policyRequestHasPolicy(req *gobgpapi.SetPoliciesRequest, name string) bool {
	for _, policy := range req.GetPolicies() {
		if policy.GetName() == name {
			return true
		}
	}
	return false
}

func indexString(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}

func indexStringPrefix(values []string, prefix string) int {
	for i, value := range values {
		if strings.HasPrefix(value, prefix) {
			return i
		}
	}
	return -1
}

func policyRequestHasStatement(req *gobgpapi.SetPoliciesRequest, policyName, statementName string) bool {
	for _, policy := range req.GetPolicies() {
		if policy.GetName() != policyName {
			continue
		}
		for _, statement := range policy.GetStatements() {
			if statement.GetName() == statementName {
				return true
			}
		}
	}
	return false
}

func assertUniqueStatementNames(t *testing.T, req *gobgpapi.SetPoliciesRequest) {
	t.Helper()
	seen := map[string]string{}
	for _, policy := range req.GetPolicies() {
		for _, statement := range policy.GetStatements() {
			name := statement.GetName()
			if previous := seen[name]; previous != "" {
				t.Fatalf("statement name %q reused by policies %q and %q", name, previous, policy.GetName())
			}
			seen[name] = policy.GetName()
		}
	}
}

func (s *fakeServer) AddPath(_ context.Context, req *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error) {
	s.paths++
	uuid := []byte{byte(s.paths)}
	req.GetPath().Uuid = uuid
	s.routes = append(s.routes, &gobgpapi.Destination{Prefix: pathPrefix(req.GetPath()), Paths: []*gobgpapi.Path{req.GetPath()}})
	return &gobgpapi.AddPathResponse{Uuid: uuid}, nil
}

func (s *fakeServer) DeletePath(_ context.Context, req *gobgpapi.DeletePathRequest) error {
	s.deletedPathUUIDs = append(s.deletedPathUUIDs, append([]byte(nil), req.GetUuid()...))
	return nil
}

func (s *fakeServer) ListPath(_ context.Context, _ *gobgpapi.ListPathRequest, fn func(*gobgpapi.Destination)) error {
	for _, dst := range s.routes {
		for _, path := range dst.Paths {
			path.Best = true
		}
		fn(dst)
	}
	if s.thirdPartyNextHop != "" {
		if s.importPolicyRewritesPeerAddress() {
			fn(testDestination("10.250.0.0/24", "192.168.1.53", "192.168.1.38"))
		} else {
			fn(testDestination("10.250.0.0/24", s.thirdPartyNextHop))
		}
		return nil
	}
	fn(testDestination("10.250.0.0/24", "192.168.1.53", "192.168.1.38"))
	return nil
}

func (s *fakeServer) WatchEvent(ctx context.Context, req *gobgpapi.WatchEventRequest, fn func(*gobgpapi.WatchEventResponse) error) error {
	s.watchRequests = append(s.watchRequests, req)
	if s.watchSessions == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case session := <-s.watchSessions:
		for _, event := range session.events {
			if err := fn(event); err != nil {
				return err
			}
		}
		if session.err != nil {
			return session.err
		}
		return nil
	}
}

func (s *fakeServer) importPolicyRewritesPeerAddress() bool {
	assigned := map[string]bool{}
	if s.policyAssignment.GetName() == "global" && s.policyAssignment.GetDirection() == gobgpapi.PolicyDirection_IMPORT {
		for _, policy := range s.policyAssignment.GetPolicies() {
			assigned[policy.GetName()] = true
		}
	}
	for _, peer := range s.peers {
		assignment := peer.GetApplyPolicy().GetImportPolicy()
		if assignment.GetDirection() != gobgpapi.PolicyDirection_IMPORT {
			continue
		}
		for _, policy := range assignment.GetPolicies() {
			assigned[policy.GetName()] = true
		}
	}
	for _, policy := range s.policyRequest.GetPolicies() {
		if !assigned[policy.GetName()] {
			continue
		}
		for _, statement := range policy.GetStatements() {
			if statement.GetActions().GetNexthop().GetPeerAddress() {
				return true
			}
		}
	}
	return false
}

type fakeFIB struct {
	mu                    sync.Mutex
	routes                []FIBRoute
	history               [][]FIBRoute
	unsupported           map[string]string
	err                   error
	guardPreferredSource  bool
	localPreferredSources map[string]bool
}

func (f *fakeFIB) SyncBGP(_ context.Context, routes []FIBRoute) (FIBSyncResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := FIBSyncResult{
		Installed:                    map[string]bool{},
		Unsupported:                  map[string]string{},
		PreferredSource:              map[string]string{},
		PreferredSourceSkipped:       map[string]bool{},
		PreferredSourceSkippedReason: map[string]string{},
	}
	normalized := make([]FIBRoute, 0, len(routes))
	for _, route := range routes {
		prefix := normalizeRoutePrefix(route.Prefix)
		if prefix != "" {
			if f.guardPreferredSource && strings.TrimSpace(route.PreferredSource) != "" && !f.localPreferredSources[strings.TrimSpace(route.PreferredSource)] {
				result.PreferredSourceSkipped[prefix] = true
				result.PreferredSourceSkippedReason[prefix] = "LocalAddressMissing"
				route.PreferredSource = ""
			}
			result.Installed[prefix] = true
			if source := strings.TrimSpace(route.PreferredSource); source != "" {
				result.PreferredSource[prefix] = source
			}
			route.Prefix = prefix
			route.NextHops = normalizeRouteNextHops(route.NextHops)
			route.PreferredSource = strings.TrimSpace(route.PreferredSource)
			normalized = append(normalized, route)
		}
	}
	f.routes = append([]FIBRoute(nil), normalized...)
	f.history = append(f.history, append([]FIBRoute(nil), normalized...))
	for prefix, reason := range f.unsupported {
		delete(result.Installed, prefix)
		result.Unsupported[prefix] = reason
	}
	return result, f.err
}

func (f *fakeFIB) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.history)
}

func (f *fakeFIB) lastRoutes() []FIBRoute {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FIBRoute(nil), f.routes...)
}

func TestReconcileStartsGoBGPAndDoesNotReaddUnchangedPeer(t *testing.T) {
	server := &fakeServer{}
	fib := &fakeFIB{}
	controller := Controller{
		Router: bgpRouter(),
		Store:  mapStore{},
		Server: server,
		FIB:    fib,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.starts != 1 {
		t.Fatalf("starts = %d, want 1", server.starts)
	}
	if !reflect.DeepEqual(server.global.GetFamilies(), []uint32{0}) {
		t.Fatalf("global families = %#v, want ipv4-unicast OpenConfig index 0", server.global.GetFamilies())
	}
	if !server.global.GetUseMultiplePaths() {
		t.Fatal("global multipath disabled, want enabled")
	}
	if server.adds != 1 {
		t.Fatalf("peer adds = %d, want 1", server.adds)
	}
	if server.policies == 0 {
		t.Fatal("SetPolicies was not called")
	}
	peer := server.peers["10.0.0.21"]
	if got := peer.GetAfiSafis()[0].GetUseMultiplePaths().GetEbgp().GetConfig().GetMaximumPaths(); got < 4 {
		t.Fatalf("peer eBGP maximum paths = %d, want >= 4", got)
	}
	importAssignment := peer.GetApplyPolicy().GetImportPolicy()
	if importAssignment.GetDirection() != gobgpapi.PolicyDirection_IMPORT ||
		importAssignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT ||
		len(importAssignment.GetPolicies()) != 1 ||
		importAssignment.GetPolicies()[0].GetName() != "routerd-lan-import" {
		t.Fatalf("peer import policy = %#v, want default import policy assigned to peer", importAssignment)
	}
	if server.policyAssignment.GetName() != "global" || server.policyAssignment.GetDirection() != gobgpapi.PolicyDirection_IMPORT ||
		server.policyAssignment.GetDefaultAction() != gobgpapi.RouteAction_ACCEPT || len(server.policyAssignment.GetPolicies()) != 0 {
		t.Fatalf("global import policy assignment = %#v, want default accept without routerd policy", server.policyAssignment)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	if status["backend"] != "gobgp" || status["phase"] != "Established" {
		t.Fatalf("router status = %#v", status)
	}
	if !reflect.DeepEqual(fib.routes, []FIBRoute{{Prefix: "10.250.0.0/24", NextHops: []string{"192.168.1.38", "192.168.1.53"}}}) {
		t.Fatalf("fib routes = %#v", fib.routes)
	}
	if status["nextHopRewrite"] != "peer-address" {
		t.Fatalf("nextHopRewrite status = %#v, want peer-address", status["nextHopRewrite"])
	}
}

func TestGoBGPPeerEbgpMultihop(t *testing.T) {
	direct := goBGPPeer(desiredPeer{Address: "192.0.2.2", ASN: 64513})
	if direct.GetEbgpMultihop() != nil {
		t.Fatalf("direct peer eBGP multihop = %#v, want nil", direct.GetEbgpMultihop())
	}
	ttlOne := goBGPPeer(desiredPeer{Address: "192.0.2.2", ASN: 64513, EbgpMultihop: 1})
	if ttlOne.GetEbgpMultihop() != nil {
		t.Fatalf("ttl=1 eBGP multihop = %#v, want nil direct-peer behavior", ttlOne.GetEbgpMultihop())
	}
	multihop := goBGPPeer(desiredPeer{Address: "192.0.2.2", ASN: 64513, EbgpMultihop: 8})
	if got := multihop.GetEbgpMultihop(); !got.GetEnabled() || got.GetMultihopTtl() != 8 {
		t.Fatalf("eBGP multihop = %#v, want enabled ttl=8", got)
	}
}

func TestGoBGPPeerInternalRouteReflectorClient(t *testing.T) {
	peer := goBGPPeer(desiredPeer{
		Address:                 "10.99.0.2",
		ASN:                     64577,
		LocalASN:                64577,
		RouteReflectorClient:    true,
		RouteReflectorClusterID: "10.99.0.1",
	})
	if peer.GetConf().GetType() != gobgpapi.PeerType_INTERNAL {
		t.Fatalf("peer type = %v, want internal", peer.GetConf().GetType())
	}
	rr := peer.GetRouteReflector()
	if !rr.GetRouteReflectorClient() || rr.GetRouteReflectorClusterId() != "10.99.0.1" {
		t.Fatalf("route reflector = %#v, want client cluster 10.99.0.1", rr)
	}
}

func TestGoBGPPeerExportPolicy(t *testing.T) {
	peer := goBGPPeer(desiredPeer{
		Address:          "10.99.0.2",
		ASN:              64577,
		ExportPolicyName: "routerd-lan-export-10-99-0-2",
		ExportPolicy:     api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}},
	})
	assignment := peer.GetApplyPolicy().GetExportPolicy()
	if assignment.GetDirection() != gobgpapi.PolicyDirection_EXPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT ||
		len(assignment.GetPolicies()) != 1 ||
		assignment.GetPolicies()[0].GetName() != "routerd-lan-export-10-99-0-2" {
		t.Fatalf("peer export policy = %#v, want default reject with named export policy", assignment)
	}
}

func TestGoBGPPeerImportPolicy(t *testing.T) {
	peer := goBGPPeer(desiredPeer{
		Address:          "10.99.0.2",
		ASN:              64577,
		ImportPolicyName: "routerd-lan-import-10-99-0-2",
		ImportPolicy:     api.BGPImportPolicySpec{AllowedPrefixes: []string{"192.168.123.0/24"}},
	})
	assignment := peer.GetApplyPolicy().GetImportPolicy()
	if assignment.GetDirection() != gobgpapi.PolicyDirection_IMPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT ||
		len(assignment.GetPolicies()) != 1 ||
		assignment.GetPolicies()[0].GetName() != "routerd-lan-import-10-99-0-2" {
		t.Fatalf("peer import policy = %#v, want default reject with named import policy", assignment)
	}
}

func TestReconcileAppliesPeerExportPolicy(t *testing.T) {
	router := bgpRouter()
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ExportPolicy = api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	peer := server.peers["10.0.0.21"]
	assignment := peer.GetApplyPolicy().GetExportPolicy()
	if assignment.GetDirection() != gobgpapi.PolicyDirection_EXPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT ||
		len(assignment.GetPolicies()) != 1 {
		t.Fatalf("peer export assignment = %#v, want default reject with one policy", assignment)
	}
	policyName := assignment.GetPolicies()[0].GetName()
	if policyName != "routerd-lan-export-10-0-0-21" {
		t.Fatalf("peer export policy name = %q", policyName)
	}
	if !policyRequestHasPrefixSet(server.policyRequest, policyName+"-prefixes", "10.250.0.0/24") {
		t.Fatalf("SetPolicies request = %#v, want export prefix set for 10.250.0.0/24", server.policyRequest)
	}
	if !policyRequestHasPolicy(server.policyRequest, policyName) {
		t.Fatalf("SetPolicies request = %#v, want export policy %q", server.policyRequest, policyName)
	}
}

func TestReconcileAppliesPeerImportPolicy(t *testing.T) {
	router := bgpRouter()
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{AllowedPrefixes: []string{"192.168.123.0/24"}}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	peer := server.peers["10.0.0.21"]
	assignment := peer.GetApplyPolicy().GetImportPolicy()
	if assignment.GetDirection() != gobgpapi.PolicyDirection_IMPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT ||
		len(assignment.GetPolicies()) != 1 {
		t.Fatalf("peer import assignment = %#v, want default reject with one policy", assignment)
	}
	policyName := assignment.GetPolicies()[0].GetName()
	if policyName != "routerd-lan-import-10-0-0-21" {
		t.Fatalf("peer import policy name = %q", policyName)
	}
	if !policyRequestHasPrefixSet(server.policyRequest, policyName+"-prefixes", "192.168.123.0/24") {
		t.Fatalf("SetPolicies request = %#v, want peer import prefix set for 192.168.123.0/24", server.policyRequest)
	}
	if !policyRequestHasPrefixSet(server.policyRequest, "routerd-lan-import-prefixes", "10.250.0.0/24") {
		t.Fatalf("SetPolicies request = %#v, want global import prefix set for 10.250.0.0/24", server.policyRequest)
	}
}

func TestReconcileAppliesPeerExportPoliciesWithUniqueStatements(t *testing.T) {
	router := bgpRouter()
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ExportPolicy = api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	extraResource := peerResource
	extraResource.Metadata.Name = "k8s-extra"
	extraSpec := extraResource.Spec.(api.BGPPeerSpec)
	extraSpec.Peers = []string{"10.0.0.22"}
	extraSpec.ExportPolicy = api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}
	extraResource.Spec = extraSpec
	router.Spec.Resources = append(router.Spec.Resources, extraResource)

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	assertUniqueStatementNames(t, server.policyRequest)
	if !policyRequestHasStatement(server.policyRequest, "routerd-lan-export-10-0-0-21", "routerd-lan-export-10-0-0-21-allow-export") {
		t.Fatalf("SetPolicies request = %#v, want peer-specific export statement for 10.0.0.21", server.policyRequest)
	}
	if !policyRequestHasStatement(server.policyRequest, "routerd-lan-export-10-0-0-22", "routerd-lan-export-10-0-0-22-allow-export") {
		t.Fatalf("SetPolicies request = %#v, want peer-specific export statement for 10.0.0.22", server.policyRequest)
	}
}

func TestApplyRouterBGPDefaultsExportsImportAllowedPrefixesToRouteReflectorClients(t *testing.T) {
	peers := map[string]desiredPeer{
		"10.255.70.4": {
			Address:              "10.255.70.4",
			RouteReflectorClient: true,
		},
		"10.255.70.5": {
			Address: "10.255.70.5",
		},
	}
	routerSpec := api.BGPRouterSpec{
		ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}},
	}

	got := applyRouterBGPDefaults("mobility-bgp", routerSpec, peers, []string{"10.99.70.1/32"}, []string{"10.77.60.10/32"})

	if prefixes := got["10.255.70.4"].ExportPolicy.AllowedPrefixes; !sameStringSet(prefixes, []string{"10.77.60.0/24", "10.77.60.10/32", "10.99.70.1/32"}) {
		t.Fatalf("route reflector client export prefixes = %#v, want reflected import allowance plus local exports", prefixes)
	}
	if prefixes := got["10.255.70.5"].ExportPolicy.AllowedPrefixes; !sameStringSet(prefixes, []string{"10.77.60.10/32", "10.99.70.1/32"}) {
		t.Fatalf("regular peer export prefixes = %#v, want only local exports", prefixes)
	}
}

func TestReconcileSoftResetsChangedPeerExportPolicy(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ExportPolicy = api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.200.0.0/24"}}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	unchangedResource := peerResource
	unchangedResource.Metadata.Name = "k8s-unchanged"
	unchangedSpec := unchangedResource.Spec.(api.BGPPeerSpec)
	unchangedSpec.Peers = []string{"10.0.0.22"}
	unchangedResource.Spec = unchangedSpec
	router.Spec.Resources = append(router.Spec.Resources, unchangedResource)

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if server.outResets != 0 {
		t.Fatalf("soft outbound resets = %d, want no reset for newly added peers", server.outResets)
	}

	peerResource = router.Spec.Resources[1]
	peerSpec = peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ExportPolicy = api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server.resets = 0
	server.outResets = 0
	server.resetRequests = nil

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.resets != 0 {
		t.Fatalf("soft inbound resets = %d, want export policy refresh to avoid inbound reset", server.resets)
	}
	if server.outResets != 1 {
		t.Fatalf("soft outbound resets = %d, want one reset for changed export policy", server.outResets)
	}
	if len(server.resetRequests) != 1 {
		t.Fatalf("ResetPeer requests = %d, want 1", len(server.resetRequests))
	}
	req := server.resetRequests[0]
	if !req.GetSoft() || req.GetDirection() != gobgpapi.ResetPeerRequest_OUT || req.GetAddress() != "10.0.0.21" {
		t.Fatalf("ResetPeer request = %#v, want soft OUT for 10.0.0.21", req)
	}
}

func TestReconcileDoesNotSoftResetUnchangedExportPolicy(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ExportPolicy = api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.policies != 1 {
		t.Fatalf("SetPolicies calls = %d, want unchanged export policy no-op after first reconcile", server.policies)
	}
	if server.outResets != 0 {
		t.Fatalf("soft outbound resets = %d, want no reset for unchanged export policy", server.outResets)
	}
}

func TestReconcileDoesNotRefreshUnchangedImportPolicy(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.Peers = []string{"192.168.1.38", "192.168.1.53"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server := &fakeServer{}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.policies != 1 {
		t.Fatalf("SetPolicies calls = %d, want unchanged-policy no-op after first reconcile", server.policies)
	}
	if server.resets != 0 {
		t.Fatalf("soft inbound resets = %d, want no reset for unchanged applied policy", server.resets)
	}
}

func TestReconcileAdoptsRestoredPoliciesAfterControllerRestart(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.Peers = []string{"192.168.1.38", "192.168.1.53"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server := &fakeServer{}
	first := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := first.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	server.policies = 0
	server.assigns = 0
	server.callLog = nil
	second := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := second.Reconcile(context.Background()); err != nil {
		t.Fatalf("post-restart reconcile: %v", err)
	}
	if server.assigns != 0 {
		t.Fatalf("SetPolicyAssignment calls = %d, want restored policy no-op after restart", server.assigns)
	}
	if server.policies != 0 {
		t.Fatalf("SetPolicies calls = %d, want restored policy no-op after restart", server.policies)
	}
	if second.importPolicyKey == "" {
		t.Fatal("importPolicyKey was not set after restored policy adoption")
	}
	if len(server.callLog) != 0 {
		t.Fatalf("post-restart call order = %#v, want no live policy/peer churn", server.callLog)
	}
}

func TestReconcileRefreshesPoliciesBeforePeerAssignmentAfterRestart(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.Peers = []string{"192.168.1.38"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server := &fakeServer{}
	first := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := first.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	delete(server.policiesByName, "routerd-lan-import")
	server.peers["192.168.1.38"].ApplyPolicy.ImportPolicy = nil
	server.policies = 0
	server.assigns = 0
	server.updates = 0
	server.callLog = nil

	second := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := second.Reconcile(context.Background()); err != nil {
		t.Fatalf("post-restart reconcile: %v", err)
	}
	firstPolicy := indexString(server.callLog, "SetPolicies")
	firstUpdate := indexStringPrefix(server.callLog, "UpdatePeer:")
	if firstPolicy < 0 {
		t.Fatalf("call order = %#v, want SetPolicies", server.callLog)
	}
	if firstUpdate < 0 || firstPolicy > firstUpdate {
		t.Fatalf("call order = %#v, policy refresh must precede peer assignment update", server.callLog)
	}
	if server.policies == 0 || server.updates == 0 {
		t.Fatalf("policies/updates = %d/%d, want policy refresh and peer assignment refresh", server.policies, server.updates)
	}
}

func TestReconcileRefreshesPoliciesBeforeReaddingDeletedPeerAfterRestart(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.Peers = []string{"192.168.1.38"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server := &fakeServer{}
	first := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := first.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	delete(server.peers, "192.168.1.38")
	server.policies = 0
	server.assigns = 0
	server.adds = 0
	server.callLog = nil

	second := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := second.Reconcile(context.Background()); err != nil {
		t.Fatalf("post-restart reconcile: %v", err)
	}
	firstPolicy := indexString(server.callLog, "SetPolicies")
	firstAdd := indexStringPrefix(server.callLog, "AddPeer:")
	if firstPolicy < 0 || firstAdd < 0 {
		t.Fatalf("call order = %#v, want SetPolicies and AddPeer", server.callLog)
	}
	if firstPolicy > firstAdd {
		t.Fatalf("call order = %#v, policy refresh must precede peer add", server.callLog)
	}
	if server.policies == 0 || server.adds == 0 {
		t.Fatalf("policies/adds = %d/%d, want policy refresh and peer re-add", server.policies, server.adds)
	}
}

func TestReconcileInstallsPeerAddressECMPForThirdPartyNextHop(t *testing.T) {
	server := &fakeServer{thirdPartyNextHop: "192.168.1.57"}
	fib := &fakeFIB{}
	controller := Controller{
		Router: bgpRouterWithImportPrefixes("10.250.0.0/24"),
		Store:  mapStore{},
		Server: server,
		FIB:    fib,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.250.0.0/24", NextHops: []string{"192.168.1.38", "192.168.1.53"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want peer-address ECMP %#v", fib.routes, want)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	got, ok := status["installedNextHops"].(map[string][]string)
	if !ok || !reflect.DeepEqual(got["10.250.0.0/24"], []string{"192.168.1.38", "192.168.1.53"}) {
		t.Fatalf("installedNextHops = %#v", status["installedNextHops"])
	}
	server.policies = 0
	server.resets = 0
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.policies != 0 {
		t.Fatalf("SetPolicies calls = %d, want valid third-party next-hop not to refresh policy", server.policies)
	}
	if server.resets != 0 {
		t.Fatalf("soft inbound resets = %d, want valid third-party next-hop not to reset peers", server.resets)
	}
}

func TestReconcileRefreshesMissingActualImportPolicy(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.Peers = []string{"192.168.1.38", "192.168.1.53"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server := &fakeServer{}
	fib := &fakeFIB{}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    fib,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	delete(server.policiesByName, "routerd-lan-import")
	server.policies = 0
	server.resets = 0

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.policies != 1 {
		t.Fatalf("SetPolicies calls = %d, want policy reapplied after actual policy drift", server.policies)
	}
	if server.resets != 2 {
		t.Fatalf("soft inbound resets = %d, want one per peer", server.resets)
	}
	server.policies = 0
	server.resets = 0
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if server.policies != 0 || server.resets != 0 {
		t.Fatalf("post-refresh policies/resets = %d/%d, want converged no-op", server.policies, server.resets)
	}
}

func TestReconcileRefreshesMissingActualImportDefinedSet(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	delete(server.definedSets, definedSetKey(gobgpapi.DefinedType_PREFIX, "routerd-lan-import-prefixes"))
	server.policies = 0
	server.resets = 0

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.policies != 1 {
		t.Fatalf("SetPolicies calls = %d, want policy reapplied after actual defined-set drift", server.policies)
	}
	if server.resets != 1 {
		t.Fatalf("soft inbound resets = %d, want one peer reset", server.resets)
	}
}

func TestReconcileRefreshesPeerImportAssignmentDrift(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.Peers = []string{"192.168.1.38", "192.168.1.53"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	server.peers["192.168.1.38"].ApplyPolicy.ImportPolicy = nil
	server.policies = 0
	server.updates = 0
	server.resets = 0

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.policies != 1 {
		t.Fatalf("SetPolicies calls = %d, want policy reapplied after peer import assignment drift", server.policies)
	}
	if server.updates != 1 {
		t.Fatalf("UpdatePeer calls = %d, want one peer assignment refresh", server.updates)
	}
	if server.resets != 2 {
		t.Fatalf("soft inbound resets = %d, want one per peer", server.resets)
	}
	server.policies = 0
	server.updates = 0
	server.resets = 0
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if server.policies != 0 || server.updates != 0 || server.resets != 0 {
		t.Fatalf("post-refresh policies/updates/resets = %d/%d/%d, want converged no-op", server.policies, server.updates, server.resets)
	}
}

func TestReconcileCanLeaveImportNextHopUnchanged(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	spec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	spec.ImportPolicy.NextHopRewrite = "unchanged"
	router.Spec.Resources[0].Spec = spec
	server := &fakeServer{thirdPartyNextHop: "192.168.1.57"}
	fib := &fakeFIB{}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    fib,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.250.0.0/24", NextHops: []string{"192.168.1.57"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want unchanged third-party next-hop %#v", fib.routes, want)
	}
}

func TestReconcileImportsFourSiteMobilityHostRoutes(t *testing.T) {
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestination("10.77.60.10/32", "10.99.0.10"),
		testDestination("10.77.60.11/32", "10.99.0.11"),
		testDestination("10.77.60.12/32", "10.99.0.12"),
		testDestination("10.77.60.13/32", "10.99.0.13"),
	}}
	fib := &fakeFIB{}
	controller := Controller{
		Router: bgpRouterWithImportPrefixes("10.77.60.0/24"),
		Store:  mapStore{},
		Server: server,
		FIB:    fib,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{
		{Prefix: "10.77.60.10/32", NextHops: []string{"10.99.0.10"}},
		{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.11"}},
		{Prefix: "10.77.60.12/32", NextHops: []string{"10.99.0.12"}},
		{Prefix: "10.77.60.13/32", NextHops: []string{"10.99.0.13"}},
	}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("FIB routes = %#v, want 4-site mobility /32 routes %#v", fib.routes, want)
	}
}

func TestReconcileAddsMobilityPreferredSourceForLocalStaticOwnedAddress(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources, bgpMobilityPreferredSourceResources("onprem-router")...)
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestination("10.77.60.11/32", "10.99.0.2"),
	}}
	fib := &fakeFIB{guardPreferredSource: true, localPreferredSources: map[string]bool{"10.77.60.10": true}}
	controller := Controller{Router: router, Store: mobilityOwnerStore(
		map[string]any{"address": "10.77.60.11/32", "action": "deliver-remote", "ownerNode": "aws-router"},
	), Server: server, FIB: fib}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.2"}, PreferredSource: "10.77.60.10"}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want preferred source %#v", fib.routes, want)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	if got := status["preferredSources"].(map[string]string)["10.77.60.11/32"]; got != "10.77.60.10" {
		t.Fatalf("preferredSources = %#v, want 10.77.60.10 for 10.77.60.11/32", status["preferredSources"])
	}
}

func TestReconcileSkipsMobilityPreferredSourceWhenLocalAddressMissing(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources, bgpMobilityPreferredSourceResources("onprem-router")...)
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestination("10.77.60.11/32", "10.99.0.2"),
	}}
	fib := &fakeFIB{guardPreferredSource: true, localPreferredSources: map[string]bool{}}
	controller := Controller{Router: router, Store: mobilityOwnerStore(
		map[string]any{"address": "10.77.60.11/32", "action": "deliver-remote", "ownerNode": "aws-router"},
	), Server: server, FIB: fib}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.2"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want source skipped %#v", fib.routes, want)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	skipped, ok := status["preferredSourceSkipped"].(map[string]bool)
	if !ok || !skipped["10.77.60.11/32"] {
		t.Fatalf("preferredSourceSkipped = %#v, want 10.77.60.11/32 skipped", status["preferredSourceSkipped"])
	}
	reasons, ok := status["preferredSourceSkippedReason"].(map[string]string)
	if !ok || reasons["10.77.60.11/32"] != "LocalAddressMissing" {
		t.Fatalf("preferredSourceSkippedReason = %#v, want LocalAddressMissing", status["preferredSourceSkippedReason"])
	}
}

func TestReconcileDoesNotAddMobilityPreferredSourceForCloudNonOwner(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources, bgpMobilityPreferredSourceResources("aws-router")...)
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestination("10.77.60.10/32", "10.99.0.1"),
	}}
	fib := &fakeFIB{guardPreferredSource: true, localPreferredSources: map[string]bool{"10.77.60.11": true}}
	controller := Controller{Router: router, Store: mobilityOwnerStore(
		map[string]any{"address": "10.77.60.10/32", "action": "deliver-remote", "ownerNode": "onprem-router"},
	), Server: server, FIB: fib}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.10/32", NextHops: []string{"10.99.0.1"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want no preferred source for cloud non-owner %#v", fib.routes, want)
	}
}

func TestReconcileExposesMobilityLivenessMarkerWithoutInstallingFIBRoute(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24", "10.99.0.0/24")
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestination("10.77.60.10/32", "10.99.0.1"),
		testDestinationWithCommunities("10.99.0.2/32", "10.99.0.1", bgpstate.MobilityCommunityNodeLiveness, bgpstate.MobilityNodeIdentityCommunity("aws-router-a")),
	}}
	fib := &fakeFIB{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: fib}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.10/32", NextHops: []string{"10.99.0.1"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want marker excluded %#v", fib.routes, want)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	markers, ok := status["livenessMarkers"].(map[string]string)
	if !ok {
		t.Fatalf("livenessMarkers = %#v, want map[string]string", status["livenessMarkers"])
	}
	community := bgpstate.MobilityNodeIdentityCommunity("aws-router-a")
	if got := markers[community]; got != "10.99.0.2/32" {
		t.Fatalf("livenessMarkers[%s] = %q, want 10.99.0.2/32", community, got)
	}
	prefixes, ok := status["prefixes"].([]bgpstate.Prefix)
	if !ok {
		t.Fatalf("prefixes = %#v, want []bgpstate.Prefix", status["prefixes"])
	}
	for _, prefix := range prefixes {
		if prefix.Prefix == "10.99.0.2/32" || bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityNodeLiveness) {
			t.Fatalf("status prefixes include liveness marker: %#v", prefixes)
		}
	}
	installed, ok := status["installedNextHops"].(map[string][]string)
	if !ok {
		t.Fatalf("installedNextHops = %#v, want map[string][]string", status["installedNextHops"])
	}
	if _, ok := installed["10.99.0.2/32"]; ok {
		t.Fatalf("installedNextHops include liveness marker: %#v", installed)
	}
}

func TestReconcileInstallsMobilityReturnRouteWithoutOwnerRetain(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestinationWithCommunities("10.77.60.4/32", "10.99.0.2", bgpstate.MobilityCommunityReturnRoute, bgpstate.MobilityNodeIdentityCommunity("aws-router-a")),
	}}
	fib := &fakeFIB{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: fib}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.4/32", NextHops: []string{"10.99.0.2"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want return-route installed without owner retain %#v", fib.routes, want)
	}
}

func TestWatchEventTriggersImmediateFIBSync(t *testing.T) {
	server := &fakeServer{
		routes:        []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.11")},
		watchSessions: make(chan watchSession, 1),
	}
	fib := &fakeFIB{}
	controller := Controller{
		Router:              bgpRouterWithImportPrefixes("10.77.60.0/24"),
		Store:               mapStore{},
		Server:              server,
		FIB:                 fib,
		WatchReconnectDelay: time.Millisecond,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if fib.calls() != 1 {
		t.Fatalf("initial FIB calls = %d, want 1", fib.calls())
	}
	server.routes = []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.12")}
	server.watchSessions <- watchSession{events: []*gobgpapi.WatchEventResponse{watchTableEvent("10.77.60.11/32", "10.99.0.12")}}
	if err := controller.watchBestPathEvents(context.Background()); err != nil {
		t.Fatalf("watch events: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.12"}}}
	if !reflect.DeepEqual(fib.lastRoutes(), want) {
		t.Fatalf("FIB routes = %#v, want event-updated routes %#v", fib.lastRoutes(), want)
	}
	if fib.calls() != 2 {
		t.Fatalf("FIB calls = %d, want event-triggered second sync", fib.calls())
	}
}

func TestWatchEventIncludesDynamicPeerImportAllowlist(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"},
		Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
		Spec: api.BGPDynamicPeerSpec{
			RouterRef:    "BGPRouter/lan",
			PeerASN:      64512,
			Listen:       api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"10.255.0.0/20"}},
			ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}},
		},
	})
	server := &fakeServer{
		routes:        []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.11")},
		watchSessions: make(chan watchSession, 1),
	}
	fib := &fakeFIB{}
	controller := Controller{
		Router:              router,
		Store:               mapStore{},
		Server:              server,
		FIB:                 fib,
		WatchReconnectDelay: time.Millisecond,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if got := fib.lastRoutes(); !reflect.DeepEqual(got, []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.11"}}}) {
		t.Fatalf("initial FIB routes = %#v, want dynamic import route", got)
	}
	server.routes = []*gobgpapi.Destination{testDestination("10.77.60.12/32", "10.99.0.12")}
	server.watchSessions <- watchSession{events: []*gobgpapi.WatchEventResponse{watchTableEvent("10.77.60.12/32", "10.99.0.12")}}
	if err := controller.watchBestPathEvents(context.Background()); err != nil {
		t.Fatalf("watch events: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.12/32", NextHops: []string{"10.99.0.12"}}}
	if !reflect.DeepEqual(fib.lastRoutes(), want) {
		t.Fatalf("FIB routes after watch = %#v, want dynamic import route %#v", fib.lastRoutes(), want)
	}
}

func TestWatchEventReconnectsAfterStreamError(t *testing.T) {
	server := &fakeServer{
		routes:        []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.11")},
		watchSessions: make(chan watchSession, 2),
	}
	fib := &fakeFIB{}
	controller := Controller{
		Router:              bgpRouterWithImportPrefixes("10.77.60.0/24"),
		Store:               mapStore{},
		Server:              server,
		FIB:                 fib,
		WatchReconnectDelay: time.Millisecond,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	server.watchSessions <- watchSession{err: errors.New("stream reset")}
	server.routes = []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.13")}
	server.watchSessions <- watchSession{events: []*gobgpapi.WatchEventResponse{watchTableEvent("10.77.60.11/32", "10.99.0.13")}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller.Start(ctx)
	waitForCondition(t, 500*time.Millisecond, func() bool {
		return fib.calls() >= 2
	})
	want := []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.13"}}}
	if !reflect.DeepEqual(fib.lastRoutes(), want) {
		t.Fatalf("FIB routes after reconnect = %#v, want %#v", fib.lastRoutes(), want)
	}
	if len(server.watchRequests) < 2 {
		t.Fatalf("watch requests = %d, want reconnect after stream error", len(server.watchRequests))
	}
}

func TestWatchEventReappliesFIBSoKernelDriftCanRecover(t *testing.T) {
	server := &fakeServer{
		routes:        []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.11")},
		watchSessions: make(chan watchSession, 1),
	}
	fib := &fakeFIB{}
	controller := Controller{
		Router: bgpRouterWithImportPrefixes("10.77.60.0/24"),
		Store:  mapStore{},
		Server: server,
		FIB:    fib,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	server.watchSessions <- watchSession{events: []*gobgpapi.WatchEventResponse{watchTableEvent("10.77.60.11/32", "10.99.0.11")}}
	if err := controller.watchBestPathEvents(context.Background()); err != nil {
		t.Fatalf("watch event: %v", err)
	}
	if fib.calls() != 2 {
		t.Fatalf("FIB calls after duplicate watch event = %d, want reapply", fib.calls())
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("poll reconcile duplicate: %v", err)
	}
	if fib.calls() != 3 {
		t.Fatalf("FIB calls after duplicate poll = %d, want reapply", fib.calls())
	}
	server.routes = []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.14")}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("poll fallback reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.14"}}}
	if !reflect.DeepEqual(fib.lastRoutes(), want) {
		t.Fatalf("FIB routes after poll fallback = %#v, want %#v", fib.lastRoutes(), want)
	}
	if fib.calls() != 4 {
		t.Fatalf("FIB calls after poll fallback = %d, want 4", fib.calls())
	}
}

func TestWatchPeerStateChangeTriggersReObservation(t *testing.T) {
	server := &fakeServer{
		routes:        []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.11")},
		watchSessions: make(chan watchSession, 1),
	}
	fib := &fakeFIB{}
	store := mapStore{}
	controller := Controller{
		Router:              bgpRouterWithImportPrefixes("10.77.60.0/24"),
		Store:               store,
		Server:              server,
		FIB:                 fib,
		WatchReconnectDelay: time.Millisecond,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	firstObserved := statusString(status["observedAt"])
	if firstObserved == "" {
		t.Fatal("missing observedAt after initial reconcile")
	}
	server.watchSessions <- watchSession{events: []*gobgpapi.WatchEventResponse{
		watchPeerStateEvent("10.99.0.11", gobgpapi.PeerState_ESTABLISHED),
	}}
	if err := controller.watchBestPathEvents(context.Background()); err != nil {
		t.Fatalf("watch events: %v", err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	secondObserved := statusString(status["observedAt"])
	if secondObserved == "" || secondObserved == firstObserved {
		t.Fatalf("peer state change did not trigger re-observation: observedAt before=%q after=%q", firstObserved, secondObserved)
	}
}

func TestGeneratedImportPolicyIsAcceptedByGoBGP(t *testing.T) {
	server := gobgpserver.NewBgpServer()
	go server.Serve()
	defer server.Stop()
	spec := api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}
	prefixes := importPolicyPrefixes(spec)
	if !prefixSetAllows(prefixes, "10.250.0.0/24") || !prefixSetAllows(prefixes, "10.250.0.42/32") {
		t.Fatalf("import prefixes = %#v, want /24 and contained /32 allowed", prefixes)
	}
	if prefixSetAllows(prefixes, "10.88.0.1/32") {
		t.Fatalf("import prefixes = %#v, want unrelated /32 rejected", prefixes)
	}
	req := &gobgpapi.SetPoliciesRequest{
		DefinedSets: []*gobgpapi.DefinedSet{{
			DefinedType: gobgpapi.DefinedType_PREFIX,
			Name:        "routerd-test-import-prefixes",
			Prefixes:    prefixes,
		}},
		Policies: []*gobgpapi.Policy{{
			Name: "routerd-test-import",
			Statements: []*gobgpapi.Statement{{
				Name: "allow-import",
				Conditions: &gobgpapi.Conditions{PrefixSet: &gobgpapi.MatchSet{
					Type: gobgpapi.MatchSet_ANY,
					Name: "routerd-test-import-prefixes",
				}},
				Actions: &gobgpapi.Actions{
					RouteAction: gobgpapi.RouteAction_ACCEPT,
					Nexthop:     nextHopRewriteAction(spec),
				},
			}},
		}},
	}
	if err := server.SetPolicies(context.Background(), req); err != nil {
		t.Fatalf("SetPolicies rejected generated import policy: %v", err)
	}
}

func TestAppliedImportPolicyConvergesWithGoBGP(t *testing.T) {
	ctx := context.Background()
	server := &testGoBGPServer{BgpServer: gobgpserver.NewBgpServer()}
	go server.Serve()
	defer server.Stop()
	if err := server.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: &gobgpapi.Global{
		Asn:              64512,
		RouterId:         "10.0.0.1",
		ListenPort:       -1,
		Families:         []uint32{0},
		UseMultiplePaths: true,
	}}); err != nil {
		t.Fatalf("StartBgp: %v", err)
	}
	spec := api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}
	peers := map[string]desiredPeer{
		"10.0.0.21": {
			Address:          "10.0.0.21",
			ASN:              64513,
			LocalASN:         64512,
			ImportPolicy:     spec,
			ImportPolicyName: bgpPolicyName("lan", "import"),
		},
	}
	controller := Controller{Server: server}
	if err := controller.applyBGPPolicies(ctx, "lan", spec, peers, nil); err != nil {
		t.Fatalf("applyBGPPolicies: %v", err)
	}
	if err := server.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: goBGPPeer(peers["10.0.0.21"])}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	drift, err := controller.importPolicyDrift(ctx, "lan", spec, peers, nil)
	if err != nil {
		t.Fatalf("importPolicyDrift: %v", err)
	}
	if drift.RefreshNeeded() {
		t.Fatalf("importPolicyDrift after apply = %#v, want no drift", drift)
	}
}

func TestImportPolicyPrefixesAllowMoreSpecifics(t *testing.T) {
	prefixes := importPolicyPrefixes(api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24", "2001:db8:77::/64"}})
	if !prefixSetAllows(prefixes, "10.77.60.0/24") || !prefixSetAllows(prefixes, "10.77.60.11/32") {
		t.Fatalf("import prefixes = %#v, want IPv4 prefix and more-specific accepted", prefixes)
	}
	if prefixSetAllows(prefixes, "10.77.0.0/16") || prefixSetAllows(prefixes, "10.88.0.1/32") {
		t.Fatalf("import prefixes = %#v, want less-specific and unrelated IPv4 rejected", prefixes)
	}
	if !prefixSetAllows(prefixes, "2001:db8:77::/64") || !prefixSetAllows(prefixes, "2001:db8:77::11/128") {
		t.Fatalf("import prefixes = %#v, want IPv6 prefix and /128 accepted", prefixes)
	}
	if prefixSetAllows(prefixes, "2001:db8:88::1/128") {
		t.Fatalf("import prefixes = %#v, want unrelated IPv6 rejected", prefixes)
	}
}

func TestImportPolicyPrefixesCanRequireExactHostRoutes(t *testing.T) {
	prefixes := importPolicyPrefixes(api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
	})
	if len(prefixes) != 1 || prefixes[0].GetMaskLengthMin() != 32 || prefixes[0].GetMaskLengthMax() != 32 {
		t.Fatalf("import prefixes = %#v, want exact /32 mask bounds", prefixes)
	}
	for _, rejected := range []string{"10.77.60.0/24", "10.77.60.0/25", "0.0.0.0/0", "10.20.0.0/24", "10.77.61.11/32"} {
		if prefixSetAllows(prefixes, rejected) {
			t.Fatalf("import prefixes = %#v allowed %s, want rejected", prefixes, rejected)
		}
	}
	if !prefixSetAllows(prefixes, "10.77.60.11/32") {
		t.Fatalf("import prefixes = %#v rejected authorized host route", prefixes)
	}
}

func prefixSetAllows(prefixes []*gobgpapi.Prefix, candidate string) bool {
	parsed, err := netip.ParsePrefix(candidate)
	if err != nil {
		return false
	}
	parsed = parsed.Masked()
	for _, allowed := range prefixes {
		parent, err := netip.ParsePrefix(allowed.GetIpPrefix())
		if err != nil {
			continue
		}
		parent = parent.Masked()
		if parent.Addr().Is4() != parsed.Addr().Is4() {
			continue
		}
		if parent.Contains(parsed.Addr()) && uint32(parsed.Bits()) >= allowed.GetMaskLengthMin() && uint32(parsed.Bits()) <= allowed.GetMaskLengthMax() {
			return true
		}
	}
	return false
}

func TestReconcileDegradesWhenSomePrefixesCannotInstall(t *testing.T) {
	server := &fakeServer{routes: []*gobgpapi.Destination{testDestination("2001:db8:250::/64", "2001:db8::53")}}
	controller := Controller{
		Router: bgpRouterWithImportPrefixes("10.250.0.0/24", "2001:db8:250::/64"),
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{unsupported: map[string]string{"2001:db8:250::/64": "GoBGPIPv6FIBUnsupported"}},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	if status["phase"] != "Degraded" || status["pendingReason"] != "GoBGPFIBPartial" {
		t.Fatalf("router status = %#v, want degraded partial FIB", status)
	}
	prefixes, ok := status["prefixes"].([]bgpstate.Prefix)
	if !ok {
		t.Fatalf("prefixes = %#v", status["prefixes"])
	}
	byPrefix := map[string]bgpstate.Prefix{}
	for _, prefix := range prefixes {
		byPrefix[prefix.Prefix] = prefix
	}
	if got := byPrefix["10.250.0.0/24"]; !got.Installed || got.SelectionState != "installed" {
		t.Fatalf("v4 prefix = %#v, want installed", got)
	}
	if got := byPrefix["2001:db8:250::/64"]; got.Installed || got.SelectionReason != "GoBGPIPv6FIBUnsupported" {
		t.Fatalf("v6 prefix = %#v, want unsupported", got)
	}
}

func TestReconcileReportsFIBSyncFailure(t *testing.T) {
	controller := Controller{
		Router: bgpRouter(),
		Store:  mapStore{},
		Server: &fakeServer{},
		FIB:    &fakeFIB{err: errors.New("netlink denied")},
	}
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "GoBGPFIBSyncFailed") {
		t.Fatalf("reconcile error = %v, want GoBGPFIBSyncFailed", err)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	if status["phase"] != "Pending" || status["pendingReason"] != "GoBGPFIBSyncFailed" {
		t.Fatalf("pending status = %#v", status)
	}
}

func TestReconcileSuppressesLocalMobilityPrivateIPFromFIB(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources, bgpMobilityPreferredSourceResources("aws-router")...)
	store := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
			"discoverySelfPrivateIPs": []any{"10.77.60.4/32"},
			"ownershipResolverFIBVerdicts": []any{
				map[string]any{
					"address":   "10.77.60.4/32",
					"action":    "local-route",
					"ownerNode": "aws-router",
				},
				map[string]any{
					"address":   "10.77.60.11/32",
					"action":    "deliver-remote",
					"ownerNode": "aws-router-b",
				},
			},
		},
	}
	fib := &fakeFIB{}
	controller := Controller{
		Router: router,
		Store:  store,
		Server: &fakeServer{routes: []*gobgpapi.Destination{
			testDestination("10.77.60.4/32", "10.255.0.41"),
			testDestination("10.77.60.11/32", "10.255.0.41"),
		}},
		FIB: fib,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	routes := fib.lastRoutes()
	if len(routes) != 1 || routes[0].Prefix != "10.77.60.11/32" {
		t.Fatalf("FIB routes = %#v, want only remote 10.77.60.11/32", routes)
	}
}

func TestReconcileSuppressesConflictLocalProviderEvidenceFromFIB(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources, bgpMobilityPreferredSourceResources("aws-router-a")...)
	store := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
			"ownershipResolverFIBVerdicts": []any{
				map[string]any{
					"address":        "10.77.60.11/32",
					"action":         "local-route",
					"conflictReason": "remote-home-owner-overlaps-local-ownership-event",
					"ownerNode":      "aws-router-b",
					"localNode":      "aws-router-a",
				},
				map[string]any{
					"address":   "10.77.60.12/32",
					"action":    "deliver-remote",
					"ownerNode": "azure-router",
				},
			},
		},
	}
	fib := &fakeFIB{}
	controller := Controller{
		Router: router,
		Store:  store,
		Server: &fakeServer{routes: []*gobgpapi.Destination{
			testDestination("10.77.60.11/32", "10.255.0.11"),
			testDestination("10.77.60.12/32", "10.255.0.11"),
		}},
		FIB: fib,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	routes := fib.lastRoutes()
	if len(routes) != 1 || routes[0].Prefix != "10.77.60.12/32" {
		t.Fatalf("FIB routes = %#v, want only remote 10.77.60.12/32", routes)
	}
}

func TestReconcileBFDObservationNeverDeconfiguresPeer(t *testing.T) {
	router := bgpRouter()
	peer := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	peer.BFD = "BFD/k8s"
	router.Spec.Resources[1].Spec = peer
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"},
		Metadata: api.ObjectMeta{Name: "k8s"},
		Spec:     api.BFDSpec{Peer: "BGPPeer/k8s"},
	})
	controller := Controller{
		Router: router,
		Store: mapStore{
			api.NetAPIVersion + "/BFD/k8s": {
				"phase":      "Down",
				"peerStates": map[string]any{"10.0.0.21": "Down"},
			},
		},
		Server: &fakeServer{},
		FIB:    &fakeFIB{},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	server := controller.Server.(*fakeServer)
	if server.adds != 1 || server.deletes != 0 {
		t.Fatalf("bootstrap with never-up BFD Down counts adds=%d deletes=%d, want 1/0", server.adds, server.deletes)
	}
	if _, ok := server.peers["10.0.0.21"]; !ok {
		t.Fatalf("bootstrap peer missing while BFD has never been Up: %#v", server.peers)
	}
	controller.Store.SaveObjectStatus(api.NetAPIVersion, "BFD", "k8s", map[string]any{
		"phase":      "Up",
		"peerStates": map[string]any{"10.0.0.21": "Up"},
	})
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.adds != 1 || server.deletes != 0 {
		t.Fatalf("after BFD Up counts adds=%d deletes=%d, want no peer churn", server.adds, server.deletes)
	}
	server.resetRequests = nil
	controller.Store.SaveObjectStatus(api.NetAPIVersion, "BFD", "k8s", map[string]any{
		"phase":      "Down",
		"peerStates": map[string]any{"10.0.0.21": "Down"},
	})
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if server.deletes != 0 {
		t.Fatalf("deletes after transient Up->Down = %d, want 0", server.deletes)
	}
	if server.hardResets != 1 {
		t.Fatalf("hard resets after BFD Up->Down = %d, want 1", server.hardResets)
	}
	if len(server.resetRequests) != 1 || server.resetRequests[0].GetSoft() || server.resetRequests[0].GetAddress() != "10.0.0.21" {
		t.Fatalf("reset requests after BFD Up->Down = %#v, want one hard reset for 10.0.0.21", server.resetRequests)
	}
	if _, ok := server.peers["10.0.0.21"]; !ok {
		t.Fatalf("peer missing after BFD Up->Down: %#v", server.peers)
	}
	controller.bfdPeerDownSince[bfdPeerGateKey("BFD/k8s", "10.0.0.21")] = time.Now().Add(-time.Minute)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("sustained down reconcile: %v", err)
	}
	if server.deletes != 0 {
		t.Fatalf("deletes after sustained Up->Down = %d, want 0", server.deletes)
	}
	if server.hardResets != 1 {
		t.Fatalf("hard resets after sustained Down = %d, want no repeated reset", server.hardResets)
	}
	if _, ok := server.peers["10.0.0.21"]; !ok {
		t.Fatalf("peer missing after sustained BFD Up->Down: %#v", server.peers)
	}
	controller.Store.SaveObjectStatus(api.NetAPIVersion, "BFD", "k8s", map[string]any{
		"phase":      "Up",
		"peerStates": map[string]any{"10.0.0.21": "Up"},
	})
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("fourth reconcile: %v", err)
	}
	if server.adds != 1 {
		t.Fatalf("adds after BFD re-Up = %d, want 1", server.adds)
	}
	if _, ok := server.peers["10.0.0.21"]; !ok {
		t.Fatalf("peer was not restored after BFD Up: %#v", server.peers)
	}
}

func TestReconcileBFDDownHardResetRetriesAfterFailure(t *testing.T) {
	router := bgpRouter()
	peer := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	peer.BFD = "BFD/k8s"
	router.Spec.Resources[1].Spec = peer
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"},
		Metadata: api.ObjectMeta{Name: "k8s"},
		Spec:     api.BFDSpec{Peer: "BGPPeer/k8s"},
	})
	server := &fakeServer{}
	controller := Controller{
		Router: router,
		Store: mapStore{
			api.NetAPIVersion + "/BFD/k8s": {
				"phase":      "Up",
				"peerStates": map[string]any{"10.0.0.21": "Up"},
			},
		},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial up reconcile: %v", err)
	}
	server.resetErrors = []error{errors.New("temporary reset failure")}
	controller.Store.SaveObjectStatus(api.NetAPIVersion, "BFD", "k8s", map[string]any{
		"phase":      "Down",
		"peerStates": map[string]any{"10.0.0.21": "Down"},
	})
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("BFD Down reset failure should keep BGPRouter pending")
	}
	if len(server.resetRequests) != 1 || server.hardResets != 0 {
		t.Fatalf("after failed reset requests/hardResets = %d/%d, want 1/0", len(server.resetRequests), server.hardResets)
	}
	controller.bfdPeerLastResetAt[bfdPeerGateKey("BFD/k8s", "10.0.0.21")] = time.Now().Add(-time.Minute)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("retry reconcile: %v", err)
	}
	if len(server.resetRequests) != 2 || server.hardResets != 1 {
		t.Fatalf("after retry requests/hardResets = %d/%d, want 2/1", len(server.resetRequests), server.hardResets)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("sustained down after successful reset: %v", err)
	}
	if len(server.resetRequests) != 2 || server.hardResets != 1 {
		t.Fatalf("after successful reset sustained down requests/hardResets = %d/%d, want no repeat", len(server.resetRequests), server.hardResets)
	}
}

func TestObserveBFDDownRearmsWhenBGPSessionReestablishes(t *testing.T) {
	controller := Controller{
		Store: mapStore{
			api.NetAPIVersion + "/BFD/k8s": {
				"phase":      "Down",
				"peerStates": map[string]any{"10.0.0.21": "Down"},
			},
		},
		bfdPeerSeenUp:        map[string]bool{bfdPeerGateKey("BFD/k8s", "10.0.0.21"): true},
		bfdPeerDownSince:     map[string]time.Time{bfdPeerGateKey("BFD/k8s", "10.0.0.21"): time.Now().Add(-time.Minute)},
		bfdPeerResetPending:  map[string]bool{},
		bfdPeerResetAttempts: map[string]int{bfdPeerGateKey("BFD/k8s", "10.0.0.21"): 1},
	}
	targets := controller.observeBFDPeerStates(map[string]desiredPeer{
		"10.0.0.21": {Address: "10.0.0.21", BFD: "BFD/k8s"},
	}, map[string]bool{"10.0.0.21": true})
	if len(targets) != 1 || targets[0].Address != "10.0.0.21" {
		t.Fatalf("targets = %#v, want rearmed reset for re-established BGP session", targets)
	}
	if !controller.bfdPeerResetPending[bfdPeerGateKey("BFD/k8s", "10.0.0.21")] {
		t.Fatal("BFD Down with live Established BGP should rearm reset pending")
	}
}

func TestReconcileBFDDownHardResetAfterControllerRestart(t *testing.T) {
	router := bgpRouter()
	peer := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	peer.BFD = "BFD/k8s"
	router.Spec.Resources[1].Spec = peer
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"},
		Metadata: api.ObjectMeta{Name: "k8s"},
		Spec:     api.BFDSpec{Peer: "BGPPeer/k8s"},
	})
	server := &fakeServer{peers: map[string]*gobgpapi.Peer{
		"10.0.0.21": {
			Conf:  &gobgpapi.PeerConf{NeighborAddress: "10.0.0.21", PeerAsn: 64513},
			State: &gobgpapi.PeerState{NeighborAddress: "10.0.0.21", PeerAsn: 64513, SessionState: gobgpapi.PeerState_ESTABLISHED},
		},
	}}
	controller := Controller{
		Router: router,
		Store: mapStore{
			api.NetAPIVersion + "/BFD/k8s": {
				"phase":      "Down",
				"peerStates": map[string]any{"10.0.0.21": "Down"},
			},
		},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile after controller restart while BFD Down: %v", err)
	}
	if len(server.resetRequests) != 1 || server.hardResets != 1 {
		t.Fatalf("reset requests/hardResets = %d/%d, want one hard reset from live established peer", len(server.resetRequests), server.hardResets)
	}
	if controller.bfdPeerSeenUp[bfdPeerGateKey("BFD/k8s", "10.0.0.21")] {
		t.Fatal("restart-safe reset should not require synthesizing seen-up state")
	}
}

func TestHardResetBFDDownPeersContinuesAfterPeerFailure(t *testing.T) {
	server := &fakeServer{resetErrors: []error{errors.New("first reset failed"), nil}}
	controller := Controller{
		Server: server,
		bfdPeerResetPending: map[string]bool{
			"BFD/a|10.0.0.21": true,
			"BFD/b|10.0.0.22": true,
		},
	}
	err := controller.hardResetBFDDownPeers(context.Background(), []bfdPeerResetTarget{
		{Key: "BFD/a|10.0.0.21", Address: "10.0.0.21"},
		{Key: "BFD/b|10.0.0.22", Address: "10.0.0.22"},
	})
	if err == nil {
		t.Fatal("want aggregate reset error")
	}
	if len(server.resetRequests) != 2 || server.hardResets != 1 {
		t.Fatalf("requests/hardResets = %d/%d, want both attempted and second succeeded", len(server.resetRequests), server.hardResets)
	}
	if !controller.bfdPeerResetPending["BFD/a|10.0.0.21"] {
		t.Fatal("failed peer reset should remain pending")
	}
	if controller.bfdPeerResetPending["BFD/b|10.0.0.22"] {
		t.Fatal("successful peer reset should clear pending")
	}
	controller.bfdPeerLastResetAt["BFD/a|10.0.0.21"] = time.Now().Add(-time.Minute)
	if err := controller.hardResetBFDDownPeers(context.Background(), []bfdPeerResetTarget{
		{Key: "BFD/a|10.0.0.21", Address: "10.0.0.21"},
		{Key: "BFD/b|10.0.0.22", Address: "10.0.0.22"},
	}); err != nil {
		t.Fatalf("retry pending peer: %v", err)
	}
	if len(server.resetRequests) != 3 || server.hardResets != 2 {
		t.Fatalf("after retry requests/hardResets = %d/%d, want only failed peer retried", len(server.resetRequests), server.hardResets)
	}
}

func TestBFDResetRuntimeStatusExposesPendingAttempts(t *testing.T) {
	now := time.Date(2026, 6, 26, 6, 0, 0, 0, time.UTC)
	key := "BFD/k8s|10.0.0.21"
	controller := Controller{
		bfdPeerDownSince:     map[string]time.Time{key: now.Add(-5 * time.Second)},
		bfdPeerResetPending:  map[string]bool{key: true},
		bfdPeerLastResetAt:   map[string]time.Time{key: now},
		bfdPeerResetError:    map[string]string{key: "temporary reset failure"},
		bfdPeerResetAttempts: map[string]int{key: 2},
	}
	status := controller.bfdResetRuntimeStatus()
	if status["bfdResetPending"] != true || status["bfdResetPendingCount"] != 1 {
		t.Fatalf("status = %#v, want one pending reset", status)
	}
	if peers, ok := status["bfdResetPendingPeers"].([]string); !ok || len(peers) != 1 || peers[0] != key {
		t.Fatalf("pending peers = %#v, want %s", status["bfdResetPendingPeers"], key)
	}
	if attempts := status["bfdResetAttemptCount"].(map[string]int); attempts[key] != 2 {
		t.Fatalf("attempts = %#v, want %s=2", attempts, key)
	}
	if errors := status["bfdResetLastError"].(map[string]string); errors[key] != "temporary reset failure" {
		t.Fatalf("errors = %#v, want last reset error", errors)
	}
}

func TestReconcileDoesNotRestartDaemonWhenGlobalConfigChanges(t *testing.T) {
	router := bgpRouter()
	first := &fakeServer{}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return first
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	spec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	spec.RouterID = "10.0.0.2"
	router.Spec.Resources[0].Spec = spec
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "GoBGPStartFailed") {
		t.Fatalf("second reconcile error = %v, want GoBGPStartFailed", err)
	}
	if first.stops != 0 || first.starts != 1 {
		t.Fatalf("daemon lifecycle changed: stops=%d starts=%d, want 0/1", first.stops, first.starts)
	}
}

func TestReconcileReattachesToLiveDaemonWithoutPeerOrPathChurn(t *testing.T) {
	router := bgpRouter()
	server := &fakeServer{}
	first := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return server
		},
	}
	if err := first.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	for _, peer := range server.peers {
		peer.Timers = nil
		peer.GracefulRestart = nil
	}
	adds, deletes, paths := server.adds, server.deletes, server.paths
	second := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return server
		},
	}
	if err := second.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.adds != adds || server.deletes != deletes || server.paths != paths {
		t.Fatalf("restart reattach churned GoBGP state: adds %d->%d deletes %d->%d paths %d->%d", adds, server.adds, deletes, server.deletes, paths, server.paths)
	}
}

func TestReconcilePreservesMobilityPathsWhenStaticAdvertisementsChange(t *testing.T) {
	router := bgpRouter()
	mobilityPath, err := localPath("10.77.60.11/32")
	if err != nil {
		t.Fatal(err)
	}
	mobilityPath.Uuid = []byte{7}
	server := &fakeServer{
		routes: []*gobgpapi.Destination{{Prefix: "10.77.60.11/32", Paths: []*gobgpapi.Path{mobilityPath}}},
		applied: bgpdaemon.AppliedConfig{
			Version: bgpdaemon.AppliedVersion,
			Global:  bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1"},
			Peers:   map[string]bgpdaemon.AppliedPeer{},
			Paths: []bgpdaemon.AppliedPath{
				bgpdaemon.StaticAppliedPath("10.20.0.0/24", []byte{9}),
				{
					Source: "MobilityPool/demo/node/aws-router-a",
					Prefix: "10.77.60.11/32",
					Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
					UUID:   bgpdaemon.EncodeUUID([]byte{7}),
				},
			},
		},
	}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(server.deletedPathUUIDs) != 1 || !reflect.DeepEqual(server.deletedPathUUIDs[0], []byte{9}) {
		t.Fatalf("deleted path UUIDs = %#v, want old static only", server.deletedPathUUIDs)
	}
	pathsByKey := map[string]bgpdaemon.AppliedPath{}
	for _, path := range server.applied.Paths {
		pathsByKey[bgpdaemon.AppliedPathKey(path)] = path
	}
	mobilityKey := bgpdaemon.AppliedPathKey(bgpdaemon.AppliedPath{Source: "MobilityPool/demo/node/aws-router-a", Prefix: "10.77.60.11/32"})
	if pathsByKey[mobilityKey].UUID != bgpdaemon.EncodeUUID([]byte{7}) {
		t.Fatalf("mobility path UUID changed despite live advertisement: %#v", server.applied.Paths)
	}
	staticKey := bgpdaemon.AppliedPathKey(bgpdaemon.StaticAppliedPath("10.0.0.0/16", nil))
	if pathsByKey[staticKey].Source != bgpdaemon.AppliedPathSourceStatic || pathsByKey[staticKey].UUID == "" {
		t.Fatalf("desired static path missing from applied state: %#v", server.applied.Paths)
	}
	if len(server.applied.Advertisements) != 1 || server.applied.Advertisements[0] != "10.0.0.0/16" {
		t.Fatalf("legacy static advertisements = %#v", server.applied.Advertisements)
	}
	if got := server.applied.Peers["10.0.0.21"].ExportPolicy.AllowedPrefixes; !sameStringSet(got, []string{"10.0.0.0/16", "10.77.60.11/32"}) {
		t.Fatalf("export policy prefixes = %#v, want static and dynamic mobility prefixes", got)
	}
	if got := server.applied.Global.ImportPolicy.AllowedPrefixes; !sameStringSet(got, []string{"10.250.0.0/24", "10.77.60.11/32"}) {
		t.Fatalf("global import policy prefixes = %#v, want configured and dynamic mobility prefixes", got)
	}
	if got := server.applied.Peers["10.0.0.21"].ImportPolicy.AllowedPrefixes; !sameStringSet(got, []string{"10.250.0.0/24", "10.77.60.11/32"}) {
		t.Fatalf("peer import policy prefixes = %#v, want configured and dynamic mobility prefixes", got)
	}
}

func TestReconcileKeepsUnchangedStaticAdvertisementWithoutReadd(t *testing.T) {
	router := bgpRouter()
	mobilityPath, err := localPath("10.77.60.11/32")
	if err != nil {
		t.Fatal(err)
	}
	mobilityPath.Uuid = []byte{7}
	server := &fakeServer{
		routes: []*gobgpapi.Destination{{Prefix: "10.77.60.11/32", Paths: []*gobgpapi.Path{mobilityPath}}},
		applied: bgpdaemon.AppliedConfig{
			Version: bgpdaemon.AppliedVersion,
			Global:  bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "10.0.0.1"},
			Peers:   map[string]bgpdaemon.AppliedPeer{},
			Paths: []bgpdaemon.AppliedPath{
				bgpdaemon.StaticAppliedPath("10.0.0.0/16", []byte{9}),
				{
					Source: "MobilityPool/demo/node/aws-router-a",
					Prefix: "10.77.60.11/32",
					Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
					UUID:   bgpdaemon.EncodeUUID([]byte{7}),
				},
			},
		},
	}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(server.deletedPathUUIDs) != 0 {
		t.Fatalf("deleted paths = %#v, want no churn for live dynamic advertisement", server.deletedPathUUIDs)
	}
	if server.paths != 0 {
		t.Fatalf("AddPath calls = %d, want no churn for live dynamic advertisement", server.paths)
	}
}

func TestReconcileLeavesDynamicAdvertisementOwnershipToControlAPI(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24", "10.77.60.11/32")
	staticPath, err := localPath("10.0.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	staticPath.Uuid = []byte{9}
	server := &fakeServer{
		routes: []*gobgpapi.Destination{{Prefix: "10.0.0.0/16", Paths: []*gobgpapi.Path{staticPath}}},
		applied: bgpdaemon.AppliedConfig{
			Version: bgpdaemon.AppliedVersion,
			Global: bgpdaemon.AppliedGlobal{
				ASN:             64512,
				RouterID:        "10.0.0.1",
				ImportPolicy:    bgpdaemon.AppliedImportPolicy{AllowedPrefixes: []string{"10.250.0.0/24", "10.77.60.11/32"}, NextHopRewrite: "peer-address"},
				ListenPort:      179,
				ListenAddresses: nil,
			},
			Peers: map[string]bgpdaemon.AppliedPeer{
				"10.0.0.21": {
					Address: "10.0.0.21",
					ASN:     64513,
					ImportPolicy: bgpdaemon.AppliedImportPolicy{
						AllowedPrefixes: []string{"10.250.0.0/24", "10.77.60.11/32"},
						NextHopRewrite:  "peer-address",
					},
					ExportPolicy: bgpdaemon.AppliedExportPolicy{AllowedPrefixes: []string{"10.0.0.0/16", "10.77.60.11/32"}},
				},
			},
			Paths: []bgpdaemon.AppliedPath{
				bgpdaemon.StaticAppliedPath("10.0.0.0/16", []byte{9}),
				{
					Source: "MobilityPool/demo/node/aws-router-a",
					Prefix: "10.77.60.11/32",
					Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
					UUID:   bgpdaemon.EncodeUUID([]byte{7}),
				},
			},
		},
	}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		Server: server,
		FIB:    &fakeFIB{},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(server.deletedPathUUIDs) != 0 {
		t.Fatalf("deleted paths = %#v, want BGP controller not to churn control-API dynamic paths", server.deletedPathUUIDs)
	}
	if server.paths != 0 {
		t.Fatalf("AddPath calls = %d, want BGP controller not to re-add control-API dynamic paths", server.paths)
	}
	pathsByKey := map[string]bgpdaemon.AppliedPath{}
	for _, path := range server.applied.Paths {
		pathsByKey[bgpdaemon.AppliedPathKey(path)] = path
	}
	key := bgpdaemon.AppliedPathKey(bgpdaemon.AppliedPath{Source: "MobilityPool/demo/node/aws-router-a", Prefix: "10.77.60.11/32"})
	if pathsByKey[key].UUID != bgpdaemon.EncodeUUID([]byte{7}) {
		t.Fatalf("dynamic path UUID changed outside control API ownership: %#v", server.applied.Paths)
	}
}

func TestReconcileUpdatesPeerWhenLiveConfigDrifts(t *testing.T) {
	router := bgpRouter()
	server := &fakeServer{}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return server
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	peer := router.Spec.Resources[1]
	spec, err := peer.BGPPeerSpec()
	if err != nil {
		t.Fatalf("peer spec: %v", err)
	}
	spec.Timers.Profile = "slow"
	peer.Spec = spec
	router.Spec.Resources[1] = peer
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.updates != 1 || server.deletes != 0 || server.adds != 1 {
		t.Fatalf("peer drift reconcile counts updates=%d deletes=%d adds=%d, want 1/0/1", server.updates, server.deletes, server.adds)
	}
	got := server.peers["10.0.0.21"].GetTimers().GetConfig().GetHoldTime()
	if got != 180 {
		t.Fatalf("hold time = %d, want slow profile 180", got)
	}
}

func TestReconcileUpdatesPeerWhenConfigChangedAcrossRouterdRestart(t *testing.T) {
	router := bgpRouter()
	server := &fakeServer{}
	first := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return server
		},
	}
	if err := first.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	peer := router.Spec.Resources[1]
	spec, err := peer.BGPPeerSpec()
	if err != nil {
		t.Fatalf("peer spec: %v", err)
	}
	spec.Timers.Profile = "slow"
	peer.Spec = spec
	router.Spec.Resources[1] = peer
	second := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return server
		},
	}
	if err := second.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.updates != 1 || server.deletes != 0 || server.adds != 1 {
		t.Fatalf("restart+config change counts updates=%d deletes=%d adds=%d, want 1/0/1", server.updates, server.deletes, server.adds)
	}
	if got := server.peers["10.0.0.21"].GetTimers().GetConfig().GetHoldTime(); got != 180 {
		t.Fatalf("hold time = %d, want slow profile 180", got)
	}
	if got := bgpdaemon.Hash(server.applied); got == "" {
		t.Fatal("applied config hash is empty")
	}
}

func TestReconcileDoesNotUpdatePeerForDynamicPrefixesOrGracefulRestartFormatting(t *testing.T) {
	router := bgpRouter()
	routerSpec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	routerSpec.GracefulRestart.RestartTime = "2m"
	routerSpec.GracefulRestart.StalePathTime = "6m"
	router.Spec.Resources[0].Spec = routerSpec

	server := &fakeServer{}
	first := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return server
		},
	}
	if err := first.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if got := server.applied.Peers["10.0.0.21"].GracefulRestart; got == nil || got.RestartTime != 120 || got.StaleRoutesTime != 360 {
		t.Fatalf("applied graceful restart = %#v, want 120s/360s", got)
	}
	server.applied.Paths = append(server.applied.Paths, bgpdaemon.AppliedPath{
		Source: "MobilityPool/demo/node/aws-router-a",
		Prefix: "10.77.60.11/32",
		Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
		UUID:   bgpdaemon.EncodeUUID([]byte{7}),
	})
	adds, updates, deletes := server.adds, server.updates, server.deletes
	second := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return server
		},
	}
	if err := second.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.adds != adds || server.updates != updates || server.deletes != deletes {
		t.Fatalf("dynamic prefix/GR formatting churned peers: adds %d->%d updates %d->%d deletes %d->%d", adds, server.adds, updates, server.updates, deletes, server.deletes)
	}
	if server.outResets == 0 {
		t.Fatal("dynamic export prefix change should still trigger an outbound soft reset")
	}
}

func TestReconcileDoesNotSilentlyAdoptLivePeerWithoutAppliedState(t *testing.T) {
	router := bgpRouter()
	server := &fakeServer{peers: map[string]*gobgpapi.Peer{
		"10.0.0.21": {
			Conf:   &gobgpapi.PeerConf{NeighborAddress: "10.0.0.21", PeerAsn: 64513},
			Timers: &gobgpapi.Timers{Config: &gobgpapi.TimersConfig{HoldTime: 90}},
			State:  &gobgpapi.PeerState{NeighborAddress: "10.0.0.21", PeerAsn: 64513, SessionState: gobgpapi.PeerState_ESTABLISHED},
		},
	}}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			return server
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if server.updates != 1 {
		t.Fatalf("updates = %d, want 1 explicit UpdatePeer for unproven live peer", server.updates)
	}
}

func TestPollIntervalUsesBGPRouterWatcher(t *testing.T) {
	router := bgpRouter()
	spec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	spec.Watcher.PollInterval = "4s"
	router.Spec.Resources[0].Spec = spec
	if got := PollInterval(router); got != 4*time.Second {
		t.Fatalf("poll interval = %v", got)
	}
	spec.Watcher.PollInterval = "1s"
	router.Spec.Resources[0].Spec = spec
	if got := PollInterval(router); got != 15*time.Second {
		t.Fatalf("short poll interval = %v", got)
	}
}

func TestStatePeerMapsListPeerFields(t *testing.T) {
	peer := statePeer(&gobgpapi.Peer{
		Conf: &gobgpapi.PeerConf{NeighborAddress: "192.0.2.1", PeerAsn: 64513},
		State: &gobgpapi.PeerState{
			SessionState: gobgpapi.PeerState_ESTABLISHED,
			Messages:     &gobgpapi.Messages{Received: &gobgpapi.Message{Total: 7}, Sent: &gobgpapi.Message{Total: 8}},
		},
		AfiSafis: []*gobgpapi.AfiSafi{{State: &gobgpapi.AfiSafiState{Accepted: 3}}},
	})
	if !peer.Established || peer.ASN != 64513 || peer.PrefixesReceived != 3 || peer.MessagesReceived != 7 || peer.MessagesSent != 8 {
		t.Fatalf("peer = %#v", peer)
	}
}

func TestFIBRoutesFromStatePrefixesBuildsECMPAndSkipsLocalAdvertisements(t *testing.T) {
	routes := fibRoutesFromStatePrefixes([]bgpstate.Prefix{
		{Prefix: "10.250.0.0/24", NextHop: "192.168.1.53", Best: true, Valid: true},
		{Prefix: "10.250.0.0/24", NextHop: "192.168.1.38", Best: true, Valid: true},
		{Prefix: "10.0.0.0/16", NextHop: "0.0.0.0", Best: true, Valid: true},
		{Prefix: "10.96.0.0/12", NextHop: "192.168.1.57", Best: true, Valid: true},
	}, allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/16"}}), nil)
	want := []FIBRoute{{Prefix: "10.250.0.0/24", NextHops: []string{"192.168.1.38", "192.168.1.53"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationChoosesHigherLocalPref(t *testing.T) {
	routes := fibRoutesFromDestination(testRankedDestination("10.77.60.12/32",
		rankedPath{nextHop: "10.99.0.11", localPref: 201, med: 20},
		rankedPath{nextHop: "10.99.0.12", localPref: 202, med: 10},
	), allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}}), nil, nil)
	want := []FIBRoute{{Prefix: "10.77.60.12/32", NextHops: []string{"10.99.0.12"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationUsesPeerAddressRewriteFromNeighbor(t *testing.T) {
	dst := testDestinationWithNeighbor("192.168.123.112/32", "10.252.0.17", "10.252.0.1")
	routes := fibRoutesFromDestination(
		dst,
		allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"192.168.123.0/24"}}),
		map[string]bool{"10.252.0.1": true},
		nil,
	)
	want := []FIBRoute{{Prefix: "192.168.123.112/32", NextHops: []string{"10.252.0.1"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want peer-address next-hop %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationKeepsPeerAddressRewriteMultipath(t *testing.T) {
	dst := testRankedDestination("192.168.123.112/32",
		rankedPath{nextHop: "10.252.0.17", localPref: 100, med: 0},
		rankedPath{nextHop: "10.252.0.18", localPref: 100, med: 0},
	)
	dst.Paths[0].NeighborIp = "10.252.0.1"
	dst.Paths[1].NeighborIp = "10.252.0.2"
	routes := fibRoutesFromDestination(
		dst,
		allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"192.168.123.0/24"}}),
		map[string]bool{"10.252.0.1": true, "10.252.0.2": true},
		nil,
	)
	want := []FIBRoute{{Prefix: "192.168.123.112/32", NextHops: []string{"10.252.0.1", "10.252.0.2"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want peer-address multipath %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationCanLeaveReflectedNextHopUnchanged(t *testing.T) {
	dst := testDestinationWithNeighbor("192.168.123.112/32", "10.252.0.17", "10.252.0.1")
	routes := fibRoutesFromDestination(
		dst,
		allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"192.168.123.0/24"}}),
		nil,
		nil,
	)
	want := []FIBRoute{{Prefix: "192.168.123.112/32", NextHops: []string{"10.252.0.17"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want unchanged reflected next-hop %#v", routes, want)
	}
}

func TestPrefixAllowedRequiresSameFamilyAndCoveredLength(t *testing.T) {
	allowed := allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.0.0.0/8", "2001:db8::/32"}})
	tests := []struct {
		prefix string
		want   bool
	}{
		{"10.250.0.0/24", true},
		{"10.0.0.0/7", false},
		{"192.168.1.0/24", false},
		{"2001:db8:1::/64", true},
		{"2001:db9::/64", false},
	}
	for _, tt := range tests {
		if got := prefixAllowed(netip.MustParsePrefix(tt.prefix), allowed); got != tt.want {
			t.Fatalf("prefixAllowed(%s) = %t, want %t", tt.prefix, got, tt.want)
		}
	}
}

type rankedPath struct {
	nextHop   string
	localPref uint32
	med       uint32
}

func testRankedDestination(prefix string, ranked ...rankedPath) *gobgpapi.Destination {
	parsed := netip.MustParsePrefix(prefix)
	nlri, _ := anypb.New(&gobgpapi.IPAddressPrefix{Prefix: parsed.Addr().String(), PrefixLen: uint32(parsed.Bits())})
	var paths []*gobgpapi.Path
	for _, path := range ranked {
		nh, _ := anypb.New(&gobgpapi.NextHopAttribute{NextHop: path.nextHop})
		localPref, _ := anypb.New(&gobgpapi.LocalPrefAttribute{LocalPref: path.localPref})
		med, _ := anypb.New(&gobgpapi.MultiExitDiscAttribute{Med: path.med})
		paths = append(paths, &gobgpapi.Path{
			Family: ipv4Family(),
			Nlri:   nlri,
			Pattrs: []*anypb.Any{nh, localPref, med},
		})
	}
	return &gobgpapi.Destination{Prefix: prefix, Paths: paths}
}

func watchTableEvent(prefix, nextHop string) *gobgpapi.WatchEventResponse {
	return &gobgpapi.WatchEventResponse{
		Event: &gobgpapi.WatchEventResponse_Table{
			Table: &gobgpapi.WatchEventResponse_TableEvent{
				Paths: testDestination(prefix, nextHop).GetPaths(),
			},
		},
	}
}

func watchPeerStateEvent(address string, state gobgpapi.PeerState_SessionState) *gobgpapi.WatchEventResponse {
	return &gobgpapi.WatchEventResponse{
		Event: &gobgpapi.WatchEventResponse_Peer{
			Peer: &gobgpapi.WatchEventResponse_PeerEvent{
				Type: gobgpapi.WatchEventResponse_PeerEvent_STATE,
				Peer: &gobgpapi.Peer{
					State: &gobgpapi.PeerState{
						NeighborAddress: address,
						SessionState:    state,
					},
				},
			},
		},
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if !fn() {
		t.Fatalf("condition not satisfied within %s", timeout)
	}
}

func bgpRouter() *api.Router {
	return bgpRouterWithImportPrefixes("10.250.0.0/24")
}

func bgpRouterWithImportPrefixes(prefixes ...string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.BGPRouterSpec{
				ASN:          64512,
				RouterID:     "10.0.0.1",
				ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.0.0.0/16"}},
				ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: prefixes},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "k8s"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.0.0.21"},
			},
		},
	}}}
}

func samEnrollmentClaimResourceForTest(name, tunnelAddress, ownedAddress string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.SAMEnrollmentClaimSpec{
			PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
			LeafID:        name,
			TunnelAddress: tunnelAddress,
			Mobility: api.SAMEnrollmentClaimMobilitySpec{
				OwnedAddresses: []string{ownedAddress},
			},
			BGP: api.SAMEnrollmentClaimBGPSpec{
				ASN:      64512,
				RouterID: strings.TrimSuffix(tunnelAddress, "/32"),
			},
		},
	}
}

func bgpMobilityPreferredSourceResources(selfNode string) []api.Resource {
	return []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     api.EventGroupSpec{NodeName: selfNode},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec: api.MobilityPoolSpec{
				Prefix:         "10.77.60.0/24",
				GroupRef:       "cloudedge",
				DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
				Members: []api.MobilityPoolMember{
					{
						NodeRef:              "onprem-router",
						Site:                 "onprem",
						Role:                 "onprem",
						StaticOwnedAddresses: []string{"10.77.60.10/32"},
						Capture:              api.MobilityMemberCapture{Type: "proxy-arp", Interface: "ens21"},
					},
					{
						NodeRef: "aws-router",
						Site:    "aws",
						Role:    "cloud",
						Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", Interface: "ens5", ConfigureOSAddress: false},
					},
				},
			},
		},
	}
}

func testDestination(prefix string, nextHops ...string) *gobgpapi.Destination {
	parsed := netip.MustParsePrefix(prefix)
	nlri, _ := anypb.New(&gobgpapi.IPAddressPrefix{Prefix: parsed.Addr().String(), PrefixLen: uint32(parsed.Bits())})
	var paths []*gobgpapi.Path
	for _, nextHop := range nextHops {
		nh, _ := anypb.New(&gobgpapi.NextHopAttribute{NextHop: nextHop})
		paths = append(paths, &gobgpapi.Path{
			Family: ipv4Family(),
			Nlri:   nlri,
			Pattrs: []*anypb.Any{nh},
			Best:   true,
		})
	}
	return &gobgpapi.Destination{
		Prefix: prefix,
		Paths:  paths,
	}
}

func testDestinationWithNeighbor(prefix, nextHop, neighbor string) *gobgpapi.Destination {
	dst := testDestination(prefix, nextHop)
	for _, path := range dst.Paths {
		path.NeighborIp = neighbor
	}
	return dst
}

func testDestinationWithCommunities(prefix, nextHop string, communities ...string) *gobgpapi.Destination {
	parsed := netip.MustParsePrefix(prefix)
	nlri, _ := anypb.New(&gobgpapi.IPAddressPrefix{Prefix: parsed.Addr().String(), PrefixLen: uint32(parsed.Bits())})
	nh, _ := anypb.New(&gobgpapi.NextHopAttribute{NextHop: nextHop})
	attrs := []*anypb.Any{nh}
	if len(communities) > 0 {
		values, err := standardCommunityValuesForTest(communities)
		if err != nil {
			panic(err)
		}
		attr, _ := anypb.New(&gobgpapi.CommunitiesAttribute{Communities: values})
		attrs = append(attrs, attr)
	}
	return &gobgpapi.Destination{
		Prefix: prefix,
		Paths: []*gobgpapi.Path{{
			Family: ipv4Family(),
			Nlri:   nlri,
			Pattrs: attrs,
			Best:   true,
		}},
	}
}

func standardCommunityValuesForTest(values []string) ([]uint32, error) {
	var out []uint32
	for _, value := range values {
		parts := strings.Split(strings.TrimSpace(value), ":")
		if len(parts) != 2 {
			return nil, errors.New("invalid community")
		}
		left, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil {
			return nil, err
		}
		right, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			return nil, err
		}
		out = append(out, uint32(left)<<16|uint32(right))
	}
	return out, nil
}

var _ bgpstate.State
