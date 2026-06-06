// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
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

type fakeServer struct {
	starts    int
	stops     int
	adds      int
	updates   int
	deletes   int
	paths     int
	policies  int
	assigns   int
	resets    int
	outResets int

	global           *gobgpapi.Global
	peers            map[string]*gobgpapi.Peer
	routes           []*gobgpapi.Destination
	applied          bgpdaemon.AppliedConfig
	deletedPathUUIDs [][]byte
	resetRequests    []*gobgpapi.ResetPeerRequest

	policyRequest     *gobgpapi.SetPoliciesRequest
	policyAssignment  *gobgpapi.PolicyAssignment
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
	if req.GetSoft() {
		s.resetRequests = append(s.resetRequests, req)
		switch req.GetDirection() {
		case gobgpapi.ResetPeerRequest_IN:
			s.resets++
		case gobgpapi.ResetPeerRequest_OUT:
			s.outResets++
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

func (s *fakeServer) SetPolicies(_ context.Context, req *gobgpapi.SetPoliciesRequest) error {
	s.policies++
	s.policyRequest = req
	return nil
}

func (s *fakeServer) ListPolicy(_ context.Context, req *gobgpapi.ListPolicyRequest, fn func(*gobgpapi.Policy)) error {
	for _, policy := range s.policyRequest.GetPolicies() {
		if strings.TrimSpace(req.GetName()) != "" && strings.TrimSpace(policy.GetName()) != strings.TrimSpace(req.GetName()) {
			continue
		}
		fn(policy)
	}
	return nil
}

func (s *fakeServer) ListDefinedSet(_ context.Context, req *gobgpapi.ListDefinedSetRequest, fn func(*gobgpapi.DefinedSet)) error {
	for _, set := range s.policyRequest.GetDefinedSets() {
		if set.GetDefinedType() != req.GetDefinedType() {
			continue
		}
		if strings.TrimSpace(req.GetName()) != "" && strings.TrimSpace(set.GetName()) != strings.TrimSpace(req.GetName()) {
			continue
		}
		fn(set)
	}
	return nil
}

func (s *fakeServer) ListPolicyAssignment(_ context.Context, req *gobgpapi.ListPolicyAssignmentRequest, fn func(*gobgpapi.PolicyAssignment)) error {
	assignment := s.policyAssignment
	if assignment == nil {
		return nil
	}
	if strings.TrimSpace(req.GetName()) != "" && strings.TrimSpace(assignment.GetName()) != strings.TrimSpace(req.GetName()) {
		return nil
	}
	if req.GetDirection() != gobgpapi.PolicyDirection_UNKNOWN && assignment.GetDirection() != req.GetDirection() {
		return nil
	}
	fn(assignment)
	return nil
}

func (s *fakeServer) SetPolicyAssignment(_ context.Context, req *gobgpapi.SetPolicyAssignmentRequest) error {
	s.assigns++
	s.policyAssignment = req.GetAssignment()
	return nil
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

func policyRequestHasPolicy(req *gobgpapi.SetPoliciesRequest, name string) bool {
	for _, policy := range req.GetPolicies() {
		if policy.GetName() == name {
			return true
		}
	}
	return false
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

func TestReconcileHydratesAppliedImportPolicyAfterRestart(t *testing.T) {
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
		t.Fatalf("SetPolicyAssignment calls = %d, want post-restart same-intent no-op", server.assigns)
	}
	if server.policies != 0 {
		t.Fatalf("SetPolicies calls = %d, want post-restart same-intent no-op", server.policies)
	}
	if second.importPolicyKey == "" {
		t.Fatal("importPolicyKey was not hydrated from applied state")
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
}

func TestReconcileRefreshesImportPolicyAfterReconnectDrift(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.250.0.0/24")
	peerResource := router.Spec.Resources[1]
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.Peers = []string{"192.168.1.38", "192.168.1.53"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	routerSpec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	staleDesired := applyRouterBGPDefaults("lan", routerSpec, map[string]desiredPeer{
		"192.168.1.38": {Address: "192.168.1.38", ASN: 64513, LocalASN: 64512},
		"192.168.1.53": {Address: "192.168.1.53", ASN: 64513, LocalASN: 64512},
	}, mapKeys(advertisedPrefixes(routerSpec)), nil)
	server := &fakeServer{thirdPartyNextHop: "192.168.1.57"}
	fib := &fakeFIB{}
	controller := Controller{
		Router:          router,
		Store:           mapStore{},
		Server:          server,
		FIB:             fib,
		importPolicyKey: bgpPoliciesKey(routerSpec.ImportPolicy, staleDesired),
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if server.policies != 1 {
		t.Fatalf("SetPolicies calls = %d, want policy reapplied after next-hop drift", server.policies)
	}
	if server.resets != 2 {
		t.Fatalf("soft inbound resets = %d, want one per peer", server.resets)
	}
	want := []FIBRoute{{Prefix: "10.250.0.0/24", NextHops: []string{"192.168.1.38", "192.168.1.53"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want peer-address ECMP after refresh %#v", fib.routes, want)
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
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: fib}
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
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: fib}
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
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: fib}
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

func TestWatchEventSkipsDuplicateFIBApplyAndPollFallbackStillWorks(t *testing.T) {
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
	if fib.calls() != 1 {
		t.Fatalf("FIB calls after duplicate watch event = %d, want unchanged", fib.calls())
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("poll reconcile duplicate: %v", err)
	}
	if fib.calls() != 1 {
		t.Fatalf("FIB calls after duplicate poll = %d, want unchanged", fib.calls())
	}
	server.routes = []*gobgpapi.Destination{testDestination("10.77.60.11/32", "10.99.0.14")}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("poll fallback reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"10.99.0.14"}}}
	if !reflect.DeepEqual(fib.lastRoutes(), want) {
		t.Fatalf("FIB routes after poll fallback = %#v, want %#v", fib.lastRoutes(), want)
	}
	if fib.calls() != 2 {
		t.Fatalf("FIB calls after poll fallback = %d, want 2", fib.calls())
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
	if _, ok := server.peers["10.0.0.21"]; !ok {
		t.Fatalf("peer missing after transient BFD Up->Down before sustained gate: %#v", server.peers)
	}
	controller.bfdPeerDownSince[bfdPeerGateKey("BFD/k8s", "10.0.0.21")] = time.Now().Add(-time.Minute)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("sustained down reconcile: %v", err)
	}
	if server.deletes != 0 {
		t.Fatalf("deletes after sustained Up->Down = %d, want 0", server.deletes)
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
	server := &fakeServer{
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
	if len(server.deletedPathUUIDs) != 2 ||
		!reflect.DeepEqual(server.deletedPathUUIDs[0], []byte{9}) ||
		!reflect.DeepEqual(server.deletedPathUUIDs[1], []byte{7}) {
		t.Fatalf("deleted path UUIDs = %#v, want old static and refreshed dynamic", server.deletedPathUUIDs)
	}
	pathsByKey := map[string]bgpdaemon.AppliedPath{}
	for _, path := range server.applied.Paths {
		pathsByKey[bgpdaemon.AppliedPathKey(path)] = path
	}
	mobilityKey := bgpdaemon.AppliedPathKey(bgpdaemon.AppliedPath{Source: "MobilityPool/demo/node/aws-router-a", Prefix: "10.77.60.11/32"})
	if pathsByKey[mobilityKey].UUID == "" || pathsByKey[mobilityKey].UUID == bgpdaemon.EncodeUUID([]byte{7}) {
		t.Fatalf("mobility path was not refreshed: %#v", server.applied.Paths)
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
	server := &fakeServer{
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
	if len(server.deletedPathUUIDs) != 1 || !reflect.DeepEqual(server.deletedPathUUIDs[0], []byte{7}) {
		t.Fatalf("deleted paths = %#v, want dynamic refresh only", server.deletedPathUUIDs)
	}
	if server.paths != 1 {
		t.Fatalf("AddPath calls = %d, want dynamic refresh only", server.paths)
	}
}

func TestReconcileRefreshesMissingDynamicAdvertisementFromAppliedState(t *testing.T) {
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
	if len(server.deletedPathUUIDs) != 1 || !reflect.DeepEqual(server.deletedPathUUIDs[0], []byte{7}) {
		t.Fatalf("deleted paths = %#v, want missing dynamic UUID refresh", server.deletedPathUUIDs)
	}
	if server.paths != 1 {
		t.Fatalf("AddPath calls = %d, want missing dynamic path re-added", server.paths)
	}
	pathsByKey := map[string]bgpdaemon.AppliedPath{}
	for _, path := range server.applied.Paths {
		pathsByKey[bgpdaemon.AppliedPathKey(path)] = path
	}
	key := bgpdaemon.AppliedPathKey(bgpdaemon.AppliedPath{Source: "MobilityPool/demo/node/aws-router-a", Prefix: "10.77.60.11/32"})
	if pathsByKey[key].UUID == "" || pathsByKey[key].UUID == bgpdaemon.EncodeUUID([]byte{7}) {
		t.Fatalf("dynamic path UUID was not refreshed: %#v", server.applied.Paths)
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

func TestBestFIBRoutesBuildsECMPAndSkipsLocalAdvertisements(t *testing.T) {
	routes := bestFIBRoutes([]bgpstate.Prefix{
		{Prefix: "10.250.0.0/24", NextHop: "192.168.1.53", Best: true, Valid: true},
		{Prefix: "10.250.0.0/24", NextHop: "192.168.1.38", Best: true, Valid: true},
		{Prefix: "10.0.0.0/16", NextHop: "0.0.0.0", Best: true, Valid: true},
		{Prefix: "10.96.0.0/12", NextHop: "192.168.1.57", Best: true, Valid: true},
	}, []netip.Prefix{netip.MustParsePrefix("10.250.0.0/16")})
	want := []FIBRoute{{Prefix: "10.250.0.0/24", NextHops: []string{"192.168.1.38", "192.168.1.53"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationChoosesHigherLocalPref(t *testing.T) {
	routes := fibRoutesFromDestination(testRankedDestination("10.77.60.12/32",
		rankedPath{nextHop: "10.99.0.11", localPref: 201, med: 20},
		rankedPath{nextHop: "10.99.0.12", localPref: 202, med: 10},
	), []netip.Prefix{netip.MustParsePrefix("10.77.60.0/24")}, nil)
	want := []FIBRoute{{Prefix: "10.77.60.12/32", NextHops: []string{"10.99.0.12"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationUsesPeerAddressRewriteFromNeighbor(t *testing.T) {
	dst := testDestinationWithNeighbor("192.168.123.112/32", "10.252.0.17", "10.252.0.1")
	routes := fibRoutesFromDestination(
		dst,
		[]netip.Prefix{netip.MustParsePrefix("192.168.123.0/24")},
		map[string]bool{"10.252.0.1": true},
	)
	want := []FIBRoute{{Prefix: "192.168.123.112/32", NextHops: []string{"10.252.0.1"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want peer-address next-hop %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationCanLeaveReflectedNextHopUnchanged(t *testing.T) {
	dst := testDestinationWithNeighbor("192.168.123.112/32", "10.252.0.17", "10.252.0.1")
	routes := fibRoutesFromDestination(
		dst,
		[]netip.Prefix{netip.MustParsePrefix("192.168.123.0/24")},
		nil,
	)
	want := []FIBRoute{{Prefix: "192.168.123.112/32", NextHops: []string{"10.252.0.17"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want unchanged reflected next-hop %#v", routes, want)
	}
}

func TestPrefixAllowedRequiresSameFamilyAndCoveredLength(t *testing.T) {
	allowed := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("2001:db8::/32")}
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
