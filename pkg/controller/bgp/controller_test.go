// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	gobgpapi "github.com/osrg/gobgp/v3/api"
	"google.golang.org/protobuf/types/known/anypb"

	"routerd/pkg/api"
	bgpstate "routerd/pkg/bgp"
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
	starts  int
	stops   int
	adds    int
	deletes int
	paths   int

	global *gobgpapi.Global
	peers  map[string]*gobgpapi.Peer
	routes []*gobgpapi.Destination
}

func (s *fakeServer) Serve() {}
func (s *fakeServer) Stop()  { s.stops++ }

func (s *fakeServer) StopBgp(context.Context, *gobgpapi.StopBgpRequest) error { return nil }

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

func (s *fakeServer) AddPath(_ context.Context, req *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error) {
	s.paths++
	s.routes = append(s.routes, &gobgpapi.Destination{Prefix: pathPrefix(req.GetPath()), Paths: []*gobgpapi.Path{req.GetPath()}})
	return &gobgpapi.AddPathResponse{Uuid: []byte{byte(s.paths)}}, nil
}

func (s *fakeServer) DeletePath(context.Context, *gobgpapi.DeletePathRequest) error { return nil }

func (s *fakeServer) ListPath(_ context.Context, _ *gobgpapi.ListPathRequest, fn func(*gobgpapi.Destination)) error {
	for _, dst := range s.routes {
		for _, path := range dst.Paths {
			path.Best = true
		}
		fn(dst)
	}
	fn(testDestination("10.250.0.0/24", "192.168.1.53", "192.168.1.38"))
	return nil
}

type fakeFIB struct {
	routes      []FIBRoute
	unsupported map[string]string
	err         error
}

func (f *fakeFIB) SyncBGP(_ context.Context, routes []FIBRoute) (FIBSyncResult, error) {
	f.routes = append([]FIBRoute(nil), routes...)
	result := FIBSyncResult{Installed: map[string]bool{}, Unsupported: map[string]string{}}
	for _, route := range routes {
		prefix := normalizeRoutePrefix(route.Prefix)
		if prefix != "" {
			result.Installed[prefix] = true
		}
	}
	for prefix, reason := range f.unsupported {
		delete(result.Installed, prefix)
		result.Unsupported[prefix] = reason
	}
	return result, f.err
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
	peer := server.peers["10.0.0.21"]
	if got := peer.GetAfiSafis()[0].GetUseMultiplePaths().GetEbgp().GetConfig().GetMaximumPaths(); got < 4 {
		t.Fatalf("peer eBGP maximum paths = %d, want >= 4", got)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	if status["backend"] != "gobgp" || status["phase"] != "Established" {
		t.Fatalf("router status = %#v", status)
	}
	if !reflect.DeepEqual(fib.routes, []FIBRoute{{Prefix: "10.250.0.0/24", NextHops: []string{"192.168.1.38", "192.168.1.53"}}}) {
		t.Fatalf("fib routes = %#v", fib.routes)
	}
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

func TestReconcileReportsBFDUnsupported(t *testing.T) {
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
		Store:  mapStore{},
		Server: &fakeServer{},
		FIB:    &fakeFIB{},
	}
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "GoBGPBFDUnsupported") {
		t.Fatalf("reconcile error = %v, want GoBGPBFDUnsupported", err)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BFD", "k8s")
	if status["phase"] != "Pending" || status["pendingReason"] != "GoBGPBFDUnsupported" {
		t.Fatalf("bfd status = %#v", status)
	}
}

func TestReconcileRecreatesServerWhenGlobalConfigChanges(t *testing.T) {
	router := bgpRouter()
	first := &fakeServer{}
	second := &fakeServer{}
	servers := []*fakeServer{first, second}
	controller := Controller{
		Router: router,
		Store:  mapStore{},
		FIB:    &fakeFIB{},
		NewServer: func() GoBGPServer {
			if len(servers) == 0 {
				t.Fatal("unexpected server allocation")
			}
			next := servers[0]
			servers = servers[1:]
			return next
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	spec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	spec.RouterID = "10.0.0.2"
	router.Spec.Resources[0].Spec = spec
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if first.stops != 1 {
		t.Fatalf("old server stops = %d, want 1", first.stops)
	}
	if second.starts != 1 || second.global.GetRouterId() != "10.0.0.2" {
		t.Fatalf("new server starts=%d routerID=%q, want 1/10.0.0.2", second.starts, second.global.GetRouterId())
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

var _ bgpstate.State
