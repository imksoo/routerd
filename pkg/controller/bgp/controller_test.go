// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpapiutil "github.com/osrg/gobgp/v4/pkg/apiutil"
	gobgp "github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgpserver "github.com/osrg/gobgp/v4/pkg/server"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
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

type mobilityFIBStore struct {
	mapStore
	records []routerstate.DynamicConfigPartRecord
}

func (s mobilityFIBStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	return append([]routerstate.DynamicConfigPartRecord(nil), s.records...), nil
}

func mobilityOwnerStore(rows ...map[string]any) mobilityFIBStore {
	return mobilityFIBStore{mapStore: mapStore{}, records: mobilityFIBRecords(rows...)}
}

func mobilityFIBRecords(rows ...map[string]any) []routerstate.DynamicConfigPartRecord {
	scope := dynamicconfig.FIBPoolScope{
		Prefix:                  "10.77.60.0/24",
		RemoteReturnCommunities: []string{bgpstate.MobilityNodeIdentityCommunity("aws-router-a")},
	}
	for _, row := range rows {
		if preferredSource := stringValue(row["preferredSource"]); preferredSource != "" {
			scope.PreferredSource = preferredSource
			break
		}
	}
	verdicts := make([]dynamicconfig.FIBVerdict, 0, len(rows)+1)
	verdicts = append(verdicts, dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Scope: &scope})
	for _, row := range rows {
		verdicts = append(verdicts, dynamicconfig.FIBVerdict{
			PoolRef:   "cloudedge",
			Address:   stringValue(row["address"]),
			Action:    stringValue(row["action"]),
			Class:     stringValue(row["class"]),
			OwnerNode: stringValue(row["ownerNode"]),
			Reason:    stringValue(row["reason"]),
		})
	}
	raw, err := json.Marshal(verdicts)
	if err != nil {
		panic(err)
	}
	now := time.Now().UTC()
	return []routerstate.DynamicConfigPartRecord{{Source: "MobilityPool/cloudedge/node/test", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:fib-test", FIBVerdictsJSON: string(raw), Status: "active"}}
}

func TestMobilityFIBVerdictsIgnoreNonMobilityPlanSource(t *testing.T) {
	raw, err := json.Marshal([]dynamicconfig.FIBVerdict{{
		PoolRef: "untrusted", Address: "10.255.0.1/32", Action: "deliver-remote",
	}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	controller := &Controller{Store: mobilityFIBStore{mapStore: mapStore{}, records: []routerstate.DynamicConfigPartRecord{{
		Source: "plugin/untrusted", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:untrusted", FIBVerdictsJSON: string(raw), Status: "active",
	}}}}
	if verdicts := controller.mobilityFIBVerdicts(); len(verdicts) != 0 {
		t.Fatalf("non-MobilityPool FIB verdicts entered mobility FIB policy: %#v", verdicts)
	}
}

func TestMobilityFIBVerdictsRejectPoolRefOutsideSourcePool(t *testing.T) {
	raw, err := json.Marshal([]dynamicconfig.FIBVerdict{{
		PoolRef: "other", Address: "10.255.0.1/32", Action: "deliver-remote",
	}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	records := append(mobilityFIBRecords(map[string]any{"address": "10.77.60.10/32", "action": "deliver-remote"}), routerstate.DynamicConfigPartRecord{
		Source: "MobilityPool/cloudedge/node/corrupt", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:corrupt-pool", FIBVerdictsJSON: string(raw), Status: "active",
	})
	controller := &Controller{Store: mobilityFIBStore{mapStore: mapStore{}, records: records}}
	if verdicts := controller.mobilityFIBVerdicts(); len(verdicts) != 0 {
		t.Fatalf("cross-pool FIB verdict left a policy for its source pool: %#v", verdicts)
	}
}

func TestMobilityFIBVerdictsRejectMalformedSourcePayload(t *testing.T) {
	now := time.Now().UTC()
	records := append(mobilityFIBRecords(map[string]any{"address": "10.77.60.10/32", "action": "deliver-remote"}), routerstate.DynamicConfigPartRecord{
		Source: "MobilityPool/cloudedge/node/corrupt", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:corrupt-json", FIBVerdictsJSON: "{", Status: "active",
	})
	controller := &Controller{Store: mobilityFIBStore{mapStore: mapStore{}, records: records}}
	if verdicts := controller.mobilityFIBVerdicts(); len(verdicts) != 0 {
		t.Fatalf("malformed FIB verdict payload left a policy for its source pool: %#v", verdicts)
	}
}

func TestMobilityFIBVerdictsRejectSemanticCorruptionForWholePool(t *testing.T) {
	raw, err := json.Marshal([]dynamicconfig.FIBVerdict{
		{PoolRef: "cloudedge", Scope: &dynamicconfig.FIBPoolScope{Prefix: "10.77.60.0/24"}},
		{PoolRef: "cloudedge", Address: "10.77.60.10", Action: "deliver-remote"},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	records := append(mobilityFIBRecords(map[string]any{"address": "10.77.60.10/32", "action": "deliver-remote"}), routerstate.DynamicConfigPartRecord{
		Source: "MobilityPool/cloudedge/node/corrupt", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:semantic-corrupt", FIBVerdictsJSON: string(raw), Status: "active",
	})
	controller := &Controller{Store: mobilityFIBStore{mapStore: mapStore{}, records: records}}
	if verdicts := controller.mobilityFIBVerdicts(); len(verdicts) != 0 {
		t.Fatalf("semantic FIB corruption left a partial policy for its pool: %#v", verdicts)
	}
}

func TestMobilityFIBVerdictsRejectDuplicateJSONFieldForWholePool(t *testing.T) {
	// encoding/json normally accepts duplicate object keys with the last value
	// winning. The typed FIB boundary must instead invalidate the entire Pool.
	raw := `[{"poolRef":"cloudedge","scope":{"prefix":"10.77.60.0/24"}},{"poolRef":"cloudedge","poolRef":"other","address":"10.77.60.10/32","action":"deliver-remote"}]`
	now := time.Now().UTC()
	records := append(mobilityFIBRecords(map[string]any{"address": "10.77.60.10/32", "action": "deliver-remote"}), routerstate.DynamicConfigPartRecord{
		Source: "MobilityPool/cloudedge/node/corrupt", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:duplicate-json-field", FIBVerdictsJSON: raw, Status: "active",
	})
	controller := &Controller{Store: mobilityFIBStore{mapStore: mapStore{}, records: records}}
	if verdicts := controller.mobilityFIBVerdicts(); len(verdicts) != 0 {
		t.Fatalf("duplicate JSON field left a partial policy for its pool: %#v", verdicts)
	}
}

func TestMobilityFIBVerdictsRejectDataplaneScopeMismatch(t *testing.T) {
	fibRaw, err := json.Marshal([]dynamicconfig.FIBVerdict{
		{PoolRef: "cloudedge", Scope: &dynamicconfig.FIBPoolScope{Prefix: "10.77.60.0/24"}},
		{PoolRef: "cloudedge", Address: "10.77.60.10/32", Action: "deliver-remote"},
	})
	if err != nil {
		t.Fatal(err)
	}
	dataplaneRaw, err := json.Marshal(dynamicconfig.MobilityDataplanePlan{
		PoolPrefix: "10.77.61.0/24",
		Routes: []dynamicconfig.MobilityIPv4RouteIntent{{
			ID: "mobility-cloudedge-local-10-77-61-10", PoolRef: "cloudedge", Destination: "10.77.61.10/32",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	controller := &Controller{Store: mobilityFIBStore{mapStore: mapStore{}, records: []routerstate.DynamicConfigPartRecord{{
		Source: "MobilityPool/cloudedge/node/test", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), Digest: "sha256:scope-mismatch", FIBVerdictsJSON: string(fibRaw), MobilityDataplaneJSON: string(dataplaneRaw), Status: "active",
	}}}}
	if verdicts := controller.mobilityFIBVerdicts(); len(verdicts) != 0 {
		t.Fatalf("mismatched dataplane/FIB scope left a policy: %#v", verdicts)
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return value.(string)
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
	callbacks := gobgpserver.WatchEventMessageCallbacks{
		OnBestPath: func(paths []*gobgpapiutil.Path, _ time.Time) {
			if callbackErr == nil && len(paths) > 0 {
				callbackErr = fn(&gobgpapi.WatchEventResponse{Event: &gobgpapi.WatchEventResponse_Table{
					Table: &gobgpapi.WatchEventResponse_TableEvent{Paths: []*gobgpapi.Path{{}}},
				}})
			}
		},
		OnPeerUpdate: func(_ *gobgpapiutil.WatchEventMessage_PeerEvent, _ time.Time) {
			if callbackErr == nil {
				callbackErr = fn(&gobgpapi.WatchEventResponse{Event: &gobgpapi.WatchEventResponse_Peer{
					Peer: &gobgpapi.WatchEventResponse_PeerEvent{Type: gobgpapi.WatchEventResponse_PeerEvent_TYPE_STATE},
				}})
			}
		},
	}
	var options []gobgpserver.WatchOption
	if req.GetTable() != nil {
		options = append(options, gobgpserver.WatchBestPath(false))
	}
	if req.GetPeer() != nil {
		options = append(options, gobgpserver.WatchPeer())
	}
	err := s.BgpServer.WatchEvent(ctx, callbacks, options...)
	if err != nil {
		return err
	}
	return callbackErr
}

func (s *testGoBGPServer) AddPath(_ context.Context, req *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error) {
	path, err := testNativePath(req.GetPath())
	if err != nil {
		return nil, err
	}
	results, err := s.BgpServer.AddPath(gobgpapiutil.AddPathRequest{VRFID: req.GetVrfId(), Paths: []*gobgpapiutil.Path{path}})
	if err != nil {
		return nil, err
	}
	id, err := results[0].UUID.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &gobgpapi.AddPathResponse{Uuid: id}, results[0].Error
}

func (s *testGoBGPServer) DeletePath(_ context.Context, req *gobgpapi.DeletePathRequest) error {
	request := gobgpapiutil.DeletePathRequest{VRFID: req.GetVrfId()}
	switch {
	case len(req.GetUuid()) > 0:
		id, err := uuid.FromBytes(req.GetUuid())
		if err != nil {
			return err
		}
		request.UUIDs = []uuid.UUID{id}
	case req.GetPath() != nil:
		path, err := testNativePath(req.GetPath())
		if err != nil {
			return err
		}
		request.Paths = []*gobgpapiutil.Path{path}
	default:
		request.DeleteAll = true
	}
	return s.BgpServer.DeletePath(request)
}

func (s *testGoBGPServer) ListPath(ctx context.Context, req *gobgpapi.ListPathRequest, fn func(*gobgpapi.Destination)) error {
	var family gobgp.Family
	if requested := req.GetFamily(); requested != nil {
		family = gobgp.NewFamily(uint16(requested.GetAfi()), uint8(requested.GetSafi()))
	}
	return s.BgpServer.ListPath(gobgpapiutil.ListPathRequest{
		TableType: req.GetTableType(),
		Name:      req.GetName(),
		Family:    family,
		SortType:  req.GetSortType(),
	}, func(prefix gobgp.NLRI, paths []*gobgpapiutil.Path) {
		if ctx.Err() != nil {
			return
		}
		destination := &gobgpapi.Destination{Prefix: prefix.String()}
		for _, path := range paths {
			nlri, _ := gobgpapiutil.MarshalNLRI(path.Nlri)
			attrs, _ := gobgpapiutil.MarshalPathAttributes(path.Attrs)
			destination.Paths = append(destination.Paths, &gobgpapi.Path{
				Family:     &gobgpapi.Family{Afi: gobgpapi.Family_Afi(path.Family.Afi()), Safi: gobgpapi.Family_Safi(path.Family.Safi())},
				Nlri:       nlri,
				Pattrs:     attrs,
				Best:       path.Best,
				IsWithdraw: path.Withdrawal,
			})
		}
		fn(destination)
	})
}

func testNativePath(path *gobgpapi.Path) (*gobgpapiutil.Path, error) {
	nlri, err := gobgpapiutil.GetNativeNlri(path)
	if err != nil {
		return nil, err
	}
	attrs, err := gobgpapiutil.GetNativePathAttributes(path)
	if err != nil {
		return nil, err
	}
	family := path.GetFamily()
	return &gobgpapiutil.Path{
		Family:     gobgp.NewFamily(uint16(family.GetAfi()), uint8(family.GetSafi())),
		Nlri:       nlri,
		Attrs:      attrs,
		Withdrawal: path.GetIsWithdraw(),
	}, nil
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
	deletePathErrors []error
	resetRequests    []*gobgpapi.ResetPeerRequest
	resetErrors      []error
	addErrors        []error
	updateErrors     []error
	deleteErrors     []error
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
	if group.GetApplyPolicy().GetImportPolicy() != nil || group.GetApplyPolicy().GetExportPolicy() == nil {
		t.Fatalf("dynamic peer group policy assignments = %#v, want export only", group.GetApplyPolicy())
	}
	if !policyPlanHasDefinedSet(server.policyRequest, gobgpapi.DefinedType_DEFINED_TYPE_NEIGHBOR, "routerd-lan-import-effective-dynamic-routerd-dynamic-cloudedge-leaves-neighbors", "10.255.0.0/20") {
		t.Fatalf("global import policy defined sets = %#v, want dynamic source neighbor set", server.policyRequest.GetDefinedSets())
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
			State: &gobgpapi.PeerState{NeighborAddress: "10.255.0.21", PeerAsn: 64512, SessionState: gobgpapi.PeerState_SESSION_STATE_ESTABLISHED},
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

func TestApplyRouterBGPDynamicDefaultsInheritsExactRouterImportPolicy(t *testing.T) {
	routerSpec := api.BGPRouterSpec{
		ConvergenceProfile: "fast",
		ImportPolicy: api.BGPImportPolicySpec{
			AllowedPrefixes:        []string{"10.77.60.0/24"},
			AllowedPrefixLengthMin: 32,
			AllowedPrefixLengthMax: 32,
		},
	}
	peers := applyRouterBGPDynamicDefaults("lan", routerSpec, map[string]desiredDynamicPeer{
		"routerd-dynamic-leaves": {
			PeerGroupName: "routerd-dynamic-leaves",
		},
	}, nil, nil)
	peer := peers["routerd-dynamic-leaves"]
	if !reflect.DeepEqual(peer.ImportPolicy.AllowedPrefixes, []string{"10.77.60.0/24"}) ||
		peer.ImportPolicy.AllowedPrefixLengthMin != 32 ||
		peer.ImportPolicy.AllowedPrefixLengthMax != 32 {
		t.Fatalf("inherited import policy = %#v", peer.ImportPolicy)
	}
}

func TestApplyRouterBGPDynamicDefaultsUsesOwnExactImportPolicyWithNextHopRewrite(t *testing.T) {
	routerSpec := api.BGPRouterSpec{
		ImportPolicy: api.BGPImportPolicySpec{
			AllowedPrefixes:        []string{"10.250.0.0/24"},
			AllowedPrefixLengthMin: 32,
			AllowedPrefixLengthMax: 32,
		},
	}
	peers := applyRouterBGPDynamicDefaults("lan", routerSpec, map[string]desiredDynamicPeer{
		"routerd-dynamic-leaves": {
			PeerGroupName: "routerd-dynamic-leaves",
			ImportPolicy: api.BGPImportPolicySpec{
				AllowedPrefixes:        []string{"10.77.60.0/24"},
				AllowedPrefixLengthMin: 32,
				AllowedPrefixLengthMax: 32,
				NextHopRewrite:         "peer-address",
			},
		},
	}, nil, nil)
	peer := peers["routerd-dynamic-leaves"]
	if !reflect.DeepEqual(peer.ImportPolicy.AllowedPrefixes, []string{"10.77.60.0/24"}) ||
		peer.ImportPolicy.AllowedPrefixLengthMin != 32 ||
		peer.ImportPolicy.AllowedPrefixLengthMax != 32 ||
		importNextHopRewrite(peer.ImportPolicy) != "peer-address" {
		t.Fatalf("own import policy = %#v", peer.ImportPolicy)
	}
}

func TestApplyRouterBGPDefaultsKeepsDirectTransportAllowlistNarrow(t *testing.T) {
	peers := applyRouterBGPDefaults("lan", api.BGPRouterSpec{}, map[string]desiredPeer{
		"10.255.0.2": {
			Address:                "10.255.0.2",
			PreserveImportPrefixes: true,
			ImportPolicy: api.BGPImportPolicySpec{
				AllowedPrefixes:        []string{"10.77.60.0/24"},
				AllowedPrefixLengthMin: 32,
				AllowedPrefixLengthMax: 32,
			},
		},
		"10.255.0.3": {
			Address: "10.255.0.3",
			ImportPolicy: api.BGPImportPolicySpec{
				AllowedPrefixes:        []string{"10.77.60.0/24"},
				AllowedPrefixLengthMin: 32,
				AllowedPrefixLengthMax: 32,
			},
		},
	}, nil, []string{"10.88.60.22/32"})
	if got, want := peers["10.255.0.2"].ImportPolicy.AllowedPrefixes, []string{"10.77.60.0/24"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("direct transport import allowlist = %#v, want %#v", got, want)
	}
	if got, want := peers["10.255.0.3"].ImportPolicy.AllowedPrefixes, []string{"10.77.60.0/24", "10.88.60.22/32"}; !sameStringSet(got, want) {
		t.Fatalf("ordinary peer import allowlist = %#v, want %#v", got, want)
	}
}

func TestDesiredPeersMarksDirectTransportImportBoundary(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
		Metadata: api.ObjectMeta{Name: "direct", Annotations: map[string]string{
			mobilityconfig.SAMTransportDirectPeerAnnotation:             "true",
			mobilityconfig.SAMTransportDirectPeerRejectRoutesAnnotation: "true",
		}},
		Spec: api.BGPPeerSpec{
			RouterRef: "BGPRouter/lan",
			PeerASN:   64512,
			Peers:     []string{"10.255.0.2"},
		},
	}}}}
	peers, err := (&Controller{Router: router}).desiredPeers("lan", 64512)
	if err != nil {
		t.Fatalf("desiredPeers: %v", err)
	}
	if !peers["10.255.0.2"].PreserveImportPrefixes || !peers["10.255.0.2"].RejectImportAll {
		t.Fatalf("direct transport peer = %#v, want explicit empty-ownership import boundary", peers["10.255.0.2"])
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

func TestSAMDynamicClaimAdmissionUsesDirectMobilityPrefixesWithoutPool(t *testing.T) {
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
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"},
			Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
			Spec: api.SAMEnrollmentPolicySpec{
				TransportProfileRef:   "SAMTransportProfile/cloudedge-transport",
				TunnelAddressPrefixes: []string{"10.255.0.0/24"},
				MobilityPrefixes:      []string{"10.77.60.0/24"},
			},
		},
		samEnrollmentClaimResourceForTest("leaf-a", "10.255.0.31/32", "10.77.60.31/32"),
		samEnrollmentClaimResourceForTest("leaf-b", "10.255.0.32/32", "10.88.60.32/32"),
	)
	admission := (&Controller{Router: router}).samDynamicClaimAdmission()

	if ok, reason := admission.Admit("10.255.0.31", netip.MustParsePrefix("10.77.60.31/32")); !ok {
		t.Fatalf("leaf-a own /32 rejected: %s", reason)
	}
	if ok, reason := admission.Admit("10.255.0.32", netip.MustParsePrefix("10.88.60.32/32")); ok || reason != "prefix-outside-authorized-pools" {
		t.Fatalf("outside route admission = (%t,%q), want prefix-outside-authorized-pools", ok, reason)
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

func TestDynamicClaimAdmissionUsesSAMTransportNeighborAlias(t *testing.T) {
	router := bgpRouterWithImportPrefixes("10.77.60.0/24")
	router.Spec.Resources = append(router.Spec.Resources,
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
				TunnelAddressPrefixes: []string{"10.255.0.0/24"},
				MobilityPoolRefs:      []string{"MobilityPool/clients"},
			},
		},
		samEnrollmentClaimResourceForTest("leaf-a", "10.255.0.99/32", "10.77.60.31/32"),
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{
				Name: "sam-rr-a-leaf-a",
				Annotations: map[string]string{
					"mobility.routerd.net/self-node": "rr-a",
					"mobility.routerd.net/peer-node": "leaf-a",
				},
			},
			Spec: api.TunnelInterfaceSpec{
				Mode:    "fou",
				Local:   "10.30.0.10",
				Remote:  "10.30.0.31",
				Address: "10.255.0.30/31",
			},
		},
	)
	admission := (&Controller{Router: router}).samDynamicClaimAdmission()
	if ok, reason := admission.Admit("10.255.0.31", netip.MustParsePrefix("10.77.60.31/32")); !ok {
		t.Fatalf("SAM transport neighbor alias rejected: %s", reason)
	}
	if ok, reason := admission.Admit("10.255.0.31", netip.MustParsePrefix("10.77.60.32/32")); ok || reason != "prefix-not-owned-by-claim" {
		t.Fatalf("SAM transport neighbor alias wrong-prefix admission = (%t,%q)", ok, reason)
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
					SessionState:    gobgpapi.PeerState_SESSION_STATE_ESTABLISHED,
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
	if statusvalue.Text(peer["enrollmentClaimRef"]) != "SAMEnrollmentClaim/leaf-a" || statusInt(peer["acceptedRoutes"]) != 1 || statusInt(peer["rejectedRoutes"]) != 1 {
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
	if len(s.addErrors) > 0 {
		err := s.addErrors[0]
		s.addErrors = s.addErrors[1:]
		if err != nil {
			return err
		}
	}
	if s.peers == nil {
		s.peers = map[string]*gobgpapi.Peer{}
	}
	peer := req.GetPeer()
	address := peer.GetConf().GetNeighborAddress()
	peer.State = &gobgpapi.PeerState{
		NeighborAddress: address,
		PeerAsn:         peer.GetConf().GetPeerAsn(),
		SessionState:    gobgpapi.PeerState_SESSION_STATE_ESTABLISHED,
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
	if len(s.updateErrors) > 0 {
		err := s.updateErrors[0]
		s.updateErrors = s.updateErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if s.peers == nil {
		s.peers = map[string]*gobgpapi.Peer{}
	}
	peer.State = &gobgpapi.PeerState{
		NeighborAddress: address,
		PeerAsn:         peer.GetConf().GetPeerAsn(),
		SessionState:    gobgpapi.PeerState_SESSION_STATE_ESTABLISHED,
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
		case gobgpapi.ResetPeerRequest_DIRECTION_IN:
			s.resets++
		case gobgpapi.ResetPeerRequest_DIRECTION_OUT:
			s.outResets++
		}
	} else {
		s.hardResets++
		if peer := s.peers[req.GetAddress()]; peer != nil {
			if peer.State == nil {
				peer.State = &gobgpapi.PeerState{NeighborAddress: req.GetAddress()}
			}
			peer.State.SessionState = gobgpapi.PeerState_SESSION_STATE_IDLE
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
	if len(s.deleteErrors) > 0 {
		err := s.deleteErrors[0]
		s.deleteErrors = s.deleteErrors[1:]
		if err != nil {
			return err
		}
	}
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
		if set.GetDefinedType() != gobgpapi.DefinedType_DEFINED_TYPE_PREFIX || set.GetName() != name {
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

func policyPlanHasDefinedSet(req *gobgpapi.SetPoliciesRequest, typ gobgpapi.DefinedType, name, value string) bool {
	for _, set := range req.GetDefinedSets() {
		if set.GetDefinedType() != typ || set.GetName() != name {
			continue
		}
		for _, item := range set.GetList() {
			if item == value {
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
	if len(s.deletePathErrors) > 0 {
		err := s.deletePathErrors[0]
		s.deletePathErrors = s.deletePathErrors[1:]
		if err != nil {
			return err
		}
	}
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
	if s.policyAssignment.GetName() == "global" && s.policyAssignment.GetDirection() == gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT {
		for _, policy := range s.policyAssignment.GetPolicies() {
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
	if peer.GetApplyPolicy().GetImportPolicy() != nil {
		t.Fatalf("peer import policy = %#v, want global-RIB policy only", peer.GetApplyPolicy().GetImportPolicy())
	}
	if server.policyAssignment.GetName() != "global" || server.policyAssignment.GetDirection() != gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT ||
		server.policyAssignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT || len(server.policyAssignment.GetPolicies()) != 1 ||
		server.policyAssignment.GetPolicies()[0].GetName() != "routerd-lan-import-effective" {
		t.Fatalf("global import policy assignment = %#v, want effective neighbor-scoped policy", server.policyAssignment)
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

func TestGoBGPPeerTransportPreservesDefaultActiveCompatibility(t *testing.T) {
	active := goBGPPeer(desiredPeer{Address: "192.0.2.2", ASN: 64513})
	if active.Transport != nil {
		t.Fatalf("default active peer transport = %#v, want nil for pre-passiveMode compatibility", active.Transport)
	}

	passive := goBGPPeer(desiredPeer{Address: "192.0.2.2", ASN: 64513, PassiveMode: true})
	if passive.Transport == nil || !passive.Transport.PassiveMode {
		t.Fatalf("passive peer transport = %#v, want passiveMode=true", passive.Transport)
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
	if peer.GetConf().GetType() != gobgpapi.PeerType_PEER_TYPE_INTERNAL {
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
	if assignment.GetDirection() != gobgpapi.PolicyDirection_POLICY_DIRECTION_EXPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT ||
		len(assignment.GetPolicies()) != 1 ||
		assignment.GetPolicies()[0].GetName() != "routerd-lan-export-10-99-0-2" {
		t.Fatalf("peer export policy = %#v, want default reject with named export policy", assignment)
	}
}

func TestGoBGPPeerDoesNotAttachIneffectiveImportPolicy(t *testing.T) {
	peer := goBGPPeer(desiredPeer{
		Address:      "10.99.0.2",
		ASN:          64577,
		ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"192.168.123.0/24"}},
	})
	if peer.GetApplyPolicy().GetImportPolicy() != nil {
		t.Fatalf("peer import policy = %#v, want no ineffective peer assignment", peer.GetApplyPolicy().GetImportPolicy())
	}
}

func TestBuildBGPPolicyPlanImportPolicyWithCommunities(t *testing.T) {
	peer := desiredPeer{
		Address: "10.99.0.2",
		ASN:     64512,
		ImportPolicy: api.BGPImportPolicySpec{
			AllowedPrefixes:      []string{"10.77.60.0/24"},
			RequiredCommunities:  []string{"64512:301"},
			ForbiddenCommunities: []string{"64512:302"},
			LocalPreference:      200,
		},
	}
	plan := buildBGPPolicyPlan("mobility", api.BGPImportPolicySpec{}, map[string]desiredPeer{"10.99.0.2": peer}, nil)
	scopeName := "routerd-mobility-import-effective-peer-10-99-0-2"
	if plan.GlobalImportAssignment.GetPolicies()[0].GetName() != "routerd-mobility-import-effective" {
		t.Fatalf("global import assignment = %#v, want effective policy", plan.GlobalImportAssignment)
	}
	if !policyPlanHasDefinedSet(plan.SetPolicies, gobgpapi.DefinedType_DEFINED_TYPE_NEIGHBOR, scopeName+"-neighbors", "10.99.0.2/32") {
		t.Fatalf("defined sets = %#v, want peer neighbor set", plan.SetPolicies.GetDefinedSets())
	}
	if !policyPlanHasDefinedSet(plan.SetPolicies, gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY, scopeName+"-required-communities", "64512:301") {
		t.Fatalf("defined sets = %#v, want required community set", plan.SetPolicies.GetDefinedSets())
	}
	if !policyPlanHasDefinedSet(plan.SetPolicies, gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY, scopeName+"-forbidden-communities", "64512:302") {
		t.Fatalf("defined sets = %#v, want forbidden community set", plan.SetPolicies.GetDefinedSets())
	}
	if len(plan.SetPolicies.GetPolicies()) != 1 || len(plan.SetPolicies.GetPolicies()[0].GetStatements()) != 3 {
		t.Fatalf("policies = %#v, want reject, allow, and terminal reject statements", plan.SetPolicies.GetPolicies())
	}
	allow := plan.SetPolicies.GetPolicies()[0].GetStatements()[1]
	if allow.GetConditions().GetNeighborSet().GetName() != scopeName+"-neighbors" ||
		allow.GetConditions().GetPrefixSet().GetName() == "" ||
		allow.GetConditions().GetCommunitySet().GetType() != gobgpapi.MatchSet_TYPE_ALL {
		t.Fatalf("allow statement = %#v, want neighbor, prefix, and required-community conditions", allow)
	}
	if got := allow.GetActions().GetLocalPref().GetValue(); got != 200 {
		t.Fatalf("allow statement local preference = %d, want 200", got)
	}
	terminal := plan.SetPolicies.GetPolicies()[0].GetStatements()[2]
	if terminal.GetConditions().GetNeighborSet().GetName() != scopeName+"-neighbors" || terminal.GetActions().GetRouteAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT {
		t.Fatalf("terminal statement = %#v, want neighbor-scoped reject", terminal)
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
	if assignment.GetDirection() != gobgpapi.PolicyDirection_POLICY_DIRECTION_EXPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT ||
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

func TestReconcileAppliesNeighborScopedGlobalImportPolicy(t *testing.T) {
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
	if server.peers["10.0.0.21"].GetApplyPolicy().GetImportPolicy() != nil {
		t.Fatalf("peer import assignment = %#v, want no ineffective peer assignment", server.peers["10.0.0.21"].GetApplyPolicy().GetImportPolicy())
	}
	policyName := "routerd-lan-import-effective"
	if !policyRequestHasPolicy(server.policyRequest, policyName) ||
		!policyRequestHasPrefixSet(server.policyRequest, "routerd-lan-import-effective-peer-10-0-0-21-prefixes", "192.168.123.0/24") ||
		!policyPlanHasDefinedSet(server.policyRequest, gobgpapi.DefinedType_DEFINED_TYPE_NEIGHBOR, "routerd-lan-import-effective-peer-10-0-0-21-neighbors", "10.0.0.21/32") {
		t.Fatalf("SetPolicies request = %#v, want neighbor-scoped global import policy", server.policyRequest)
	}
	assignment := server.policyAssignment
	if assignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT || len(assignment.GetPolicies()) != 1 || assignment.GetPolicies()[0].GetName() != policyName {
		t.Fatalf("global import assignment = %#v, want effective policy", assignment)
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
	if !req.GetSoft() || req.GetDirection() != gobgpapi.ResetPeerRequest_DIRECTION_OUT || req.GetAddress() != "10.0.0.21" {
		t.Fatalf("ResetPeer request = %#v, want soft OUT for 10.0.0.21", req)
	}
}

func TestReconcileSoftResetsChangedPeerImportAllowlist(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		LocalPreference:        200,
	}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if server.resets != 0 {
		t.Fatalf("initial soft inbound resets = %d, want none before the peer exists", server.resets)
	}

	peerResource = router.Spec.Resources[1]
	peerSpec = peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy.AllowedPrefixes = []string{"10.77.61.0/24"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server.resets = 0
	server.outResets = 0
	server.resetRequests = nil

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.resets != 1 || server.outResets != 0 {
		t.Fatalf("soft resets in/out = %d/%d, want one inbound reset after narrowed direct allowlist", server.resets, server.outResets)
	}
	if len(server.resetRequests) != 1 {
		t.Fatalf("ResetPeer requests = %d, want 1", len(server.resetRequests))
	}
	req := server.resetRequests[0]
	if !req.GetSoft() || req.GetDirection() != gobgpapi.ResetPeerRequest_DIRECTION_IN || req.GetAddress() != "10.0.0.21" {
		t.Fatalf("ResetPeer request = %#v, want soft IN for 10.0.0.21", req)
	}
}

func TestReconcileReplaysPendingDirectImportResetAfterControllerRestart(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		LocalPreference:        200,
	}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	first := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := first.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}

	peerResource = router.Spec.Resources[1]
	peerSpec = peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy.AllowedPrefixes = []string{"10.77.61.0/24"}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server.resetErrors = []error{errors.New("simulated controller crash before inbound reset")}
	if err := first.Reconcile(context.Background()); err == nil {
		t.Fatal("changed direct import policy reset failure should leave reconciliation pending")
	}
	if !server.applied.PendingImportPolicyReset {
		t.Fatalf("applied state = %#v, want durable pending import-reset fence", server.applied)
	}
	if server.resets != 0 {
		t.Fatalf("failed inbound reset count = %d, want 0", server.resets)
	}

	// The live policy already changed, just as it would after a process crash
	// between SetPolicies and ResetPeer. The new controller must honor the
	// durable fence instead of adopting the live policy and trusting the old RIB.
	second := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := second.Reconcile(context.Background()); err != nil {
		t.Fatalf("restart reconcile: %v", err)
	}
	if server.resets != 1 {
		t.Fatalf("post-restart inbound resets = %d, want one replay", server.resets)
	}
	if server.applied.PendingImportPolicyReset {
		t.Fatalf("applied state = %#v, want cleared import-reset fence", server.applied)
	}
}

func TestReconcileJournalsDirectPeerBeforeLiveAdd(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.22/32"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		LocalPreference:        200,
	}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{addErrors: []error{errors.New("simulated direct AddPeer interruption")}}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("direct AddPeer interruption should leave reconciliation pending")
	}
	journaled, found := server.applied.Peers["10.0.0.21"]
	if !found || !journaled.PreserveImportPrefixes {
		t.Fatalf("applied state did not retain journaled direct peer: %#v", server.applied.Peers)
	}
	if !sameStringSet(server.applied.PendingDirectPeerAdditions, []string{"10.0.0.21"}) {
		t.Fatalf("pending direct additions = %#v, want journaled peer", server.applied.PendingDirectPeerAdditions)
	}
	if server.peers["10.0.0.21"] != nil {
		t.Fatalf("direct peer unexpectedly exists after failed AddPeer: %#v", server.peers)
	}

	// A replacement controller can identify the incomplete direct transition
	// and remove it before policy reconciliation when the topology disappears.
	router.Spec.Resources = router.Spec.Resources[:1]
	recovered := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := recovered.Reconcile(context.Background()); err != nil {
		t.Fatalf("recover from journaled direct addition: %v", err)
	}
	if _, found := server.applied.Peers["10.0.0.21"]; found {
		t.Fatalf("applied state retained withdrawn journaled direct peer: %#v", server.applied.Peers)
	}
	if len(server.applied.PendingDirectPeerAdditions) != 0 || len(server.applied.PendingDirectPeerRemovals) != 0 {
		t.Fatalf("applied state retained completed direct transition: additions=%#v removals=%#v", server.applied.PendingDirectPeerAdditions, server.applied.PendingDirectPeerRemovals)
	}
}

func TestReconcileWaitsForObsoleteDirectPeerWithdrawalBeforeRemovingPolicy(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.22/32"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		LocalPreference:        200,
	}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	policyCalls := server.policies
	router.Spec.Resources = router.Spec.Resources[:1]
	server.deleteErrors = []error{errors.New("temporary delete failure")}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("obsolete direct peer deletion failure should keep reconciliation pending")
	}
	if server.policies != policyCalls {
		t.Fatalf("policy calls = %d, want %d; policy must remain until direct peer is withdrawn", server.policies, policyCalls)
	}
	if server.peers["10.0.0.21"] == nil {
		t.Fatalf("direct peer disappeared after failed deletion: %#v", server.peers)
	}
	if _, found := server.applied.Peers["10.0.0.21"]; !found {
		t.Fatalf("applied state lost direct peer policy during failed deletion: %#v", server.applied.Peers)
	}
	if !sameStringSet(server.applied.PendingDirectPeerRemovals, []string{"10.0.0.21"}) {
		t.Fatalf("pending direct peer removals = %#v, want obsolete direct peer", server.applied.PendingDirectPeerRemovals)
	}

	// Model routerd restarting after the failed live delete. The durable
	// retirement marker must tell the replacement controller to remove the
	// still-live direct peer even though applied.json no longer restores it.
	recovered := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := recovered.Reconcile(context.Background()); err != nil {
		t.Fatalf("restart retry reconcile: %v", err)
	}
	if server.peers["10.0.0.21"] != nil {
		t.Fatalf("obsolete direct peer remained after successful retry: %#v", server.peers)
	}
	if _, found := server.applied.Peers["10.0.0.21"]; found {
		t.Fatalf("applied state retained withdrawn direct peer: %#v", server.applied.Peers)
	}
	if len(server.applied.PendingDirectPeerRemovals) != 0 {
		t.Fatalf("applied state retained completed direct-peer retirement: %#v", server.applied.PendingDirectPeerRemovals)
	}
}

func TestReconcileTreatsAlreadyWithdrawnObsoleteDirectPeerAsRemoved(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerResource.Spec = peerResource.Spec.(api.BGPPeerSpec)
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	// Model a successful GoBGP delete followed by a crash before applied.json
	// could be rewritten. The next reconcile must converge rather than retry a
	// stale delete forever.
	delete(server.peers, "10.0.0.21")
	router.Spec.Resources = router.Spec.Resources[:1]
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile after already-withdrawn direct peer: %v", err)
	}
	if _, found := server.applied.Peers["10.0.0.21"]; found {
		t.Fatalf("applied state retained already-withdrawn direct peer: %#v", server.applied.Peers)
	}
}

func TestReconcileWithdrawsDirectPeerBeforeSameAddressBecomesFallbackPeer(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.22/32"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		LocalPreference:        200,
	}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial direct reconcile: %v", err)
	}

	// Keep the neighbor address but turn it into an ordinary fallback peer.
	// The old direct import filter must not be removed until the live direct
	// session has been withdrawn.
	peerResource = router.Spec.Resources[1]
	peerResource.Metadata.Annotations = nil
	peerSpec = peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy.LocalPreference = 100
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server.callLog = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("direct-to-fallback reconcile: %v", err)
	}
	deleteAt := indexStringPrefix(server.callLog, "DeletePeer:10.0.0.21")
	policyAt := indexString(server.callLog, "SetPolicies")
	addAt := indexStringPrefix(server.callLog, "AddPeer:10.0.0.21")
	if deleteAt < 0 || policyAt < 0 || addAt < 0 || !(deleteAt < policyAt && policyAt < addAt) {
		t.Fatalf("direct-to-fallback call order = %#v, want DeletePeer before policy replacement before AddPeer", server.callLog)
	}
	if len(server.applied.PendingDirectPeerAdditions) != 0 || len(server.applied.PendingDirectPeerRemovals) != 0 {
		t.Fatalf("applied state retained completed direct-to-fallback transition: additions=%#v removals=%#v", server.applied.PendingDirectPeerAdditions, server.applied.PendingDirectPeerRemovals)
	}
}

func TestReconcileFencesFallbackToDirectBeforePolicyReplacement(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial fallback reconcile: %v", err)
	}

	// Upgrade the existing neighbor in place. Interrupt UpdatePeer after its
	// direct policy has been installed, which is the crash window where a stale
	// fallback record would otherwise make later policy removal unsafe.
	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.22/32"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		LocalPreference:        200,
	}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server.updateErrors = []error{errors.New("simulated direct UpdatePeer interruption")}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("interrupted fallback-to-direct update should leave reconciliation pending")
	}
	journaled, found := server.applied.Peers["10.0.0.21"]
	if !found || !journaled.PreserveImportPrefixes {
		t.Fatalf("pre-policy journal did not classify peer as direct: %#v", server.applied.Peers)
	}
	if !sameStringSet(server.applied.PendingDirectPeerAdditions, []string{"10.0.0.21"}) {
		t.Fatalf("pending direct additions = %#v, want direct upgrade fence", server.applied.PendingDirectPeerAdditions)
	}
	if server.peers["10.0.0.21"] == nil {
		t.Fatalf("interrupted update unexpectedly removed the still-live fallback peer: %#v", server.peers)
	}

	// Simulate a configuration change (or an enrollment expiry) before routerd
	// can retry the update. A replacement controller must delete the live peer
	// before it is allowed to remove the narrow direct policy.
	router.Spec.Resources = router.Spec.Resources[:1]
	routerResource := router.Spec.Resources[0]
	routerSpec := routerResource.Spec.(api.BGPRouterSpec)
	routerSpec.ImportPolicy = api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.99.0/24"}}
	routerResource.Spec = routerSpec
	router.Spec.Resources[0] = routerResource
	server.callLog = nil
	recovered := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := recovered.Reconcile(context.Background()); err != nil {
		t.Fatalf("recover interrupted fallback-to-direct update: %v", err)
	}
	deleteAt := indexStringPrefix(server.callLog, "DeletePeer:10.0.0.21")
	policyAt := indexString(server.callLog, "SetPolicies")
	if deleteAt < 0 || policyAt < 0 || deleteAt >= policyAt {
		t.Fatalf("recovery call order = %#v, want direct/fallback peer withdrawal before policy replacement", server.callLog)
	}
	if server.peers["10.0.0.21"] != nil {
		t.Fatalf("recovery retained peer after its direct upgrade was abandoned: %#v", server.peers)
	}
}

func TestReconcileUpdatesDirectPeerWhenPendingRetirementIsCancelled(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.22/32"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		LocalPreference:        200,
	}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource

	server := &fakeServer{}
	first := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := first.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial direct reconcile: %v", err)
	}

	// Model D→F after DeletePeer and fallback AddPeer, followed by a crash
	// before the normal final state write: applied.json still says D is being
	// removed while the live address may already carry fallback settings.
	server.applied.PendingDirectPeerRemovals = []string{"10.0.0.21"}
	server.applied = bgpdaemon.Normalize(server.applied)
	server.callLog = nil
	recovered := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := recovered.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile after cancelled direct retirement: %v", err)
	}
	if updateAt := indexStringPrefix(server.callLog, "UpdatePeer:10.0.0.21"); updateAt < 0 {
		t.Fatalf("cancelled direct retirement did not force a live peer update: %#v", server.callLog)
	}
	if len(server.applied.PendingDirectPeerAdditions) != 0 || len(server.applied.PendingDirectPeerRemovals) != 0 {
		t.Fatalf("applied state retained completed direct retirement cancellation: additions=%#v removals=%#v", server.applied.PendingDirectPeerAdditions, server.applied.PendingDirectPeerRemovals)
	}
}

func TestReconcileFencesUnrecordedLiveFallbackBeforeDirectPolicyReplacement(t *testing.T) {
	router := bgpRouterWithImportPrefixes()
	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial fallback reconcile: %v", err)
	}

	// Model a power/process failure after the fallback reached GoBGP but before
	// its applied-state write survived. The static live peer is intentionally
	// left behind with no controller cache or persisted peer record.
	server.applied = bgpdaemon.AppliedConfig{}
	controller.appliedConfig = bgpdaemon.AppliedConfig{}
	controller.appliedPeerKeys = nil
	controller.desiredPeerKeys = nil
	controller.pendingDirectPeerAdditions = nil

	peerResource := router.Spec.Resources[1]
	peerResource.Metadata.Annotations = map[string]string{"mobility.routerd.net/direct-peer": "true"}
	peerSpec := peerResource.Spec.(api.BGPPeerSpec)
	peerSpec.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.22/32"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		LocalPreference:        200,
	}
	peerResource.Spec = peerSpec
	router.Spec.Resources[1] = peerResource
	server.updateErrors = []error{errors.New("simulated direct UpdatePeer interruption")}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("interrupted unrecorded fallback-to-direct update should leave reconciliation pending")
	}
	journaled, found := server.applied.Peers["10.0.0.21"]
	if !found || !journaled.PreserveImportPrefixes {
		t.Fatalf("live-fallback promotion was not journaled before policy replacement: %#v", server.applied.Peers)
	}
	if !sameStringSet(server.applied.PendingDirectPeerAdditions, []string{"10.0.0.21"}) {
		t.Fatalf("pending direct additions = %#v, want live-fallback promotion fence", server.applied.PendingDirectPeerAdditions)
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

func TestReconcileReportsReconvergingDuringGracefulRestartWindow(t *testing.T) {
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
		t.Fatalf("initial reconcile: %v", err)
	}
	server.peers["192.168.1.53"].State.SessionState = gobgpapi.PeerState_SESSION_STATE_IDLE

	controller.startedAt = time.Time{}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile during graceful restart window: %v", err)
	}
	routerStatus := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	if routerStatus["phase"] != "Reconverging" || routerStatus["reason"] != "GoBGPReconverging" {
		t.Fatalf("router status = %#v, want reconverging", routerStatus)
	}
	peerStatus := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPPeer", "k8s")
	if peerStatus["phase"] != "Reconverging" || peerStatus["pendingReason"] != "GoBGPReconverging" {
		t.Fatalf("peer status = %#v, want reconverging", peerStatus)
	}
	if peerStatus["reconvergingUntil"] == "" || peerStatus["reconvergingSince"] == "" {
		t.Fatalf("peer status missing reconverging timestamps: %#v", peerStatus)
	}

	oldErrorAt := time.Now().Add(-7 * time.Minute).UTC().Format(time.RFC3339Nano)
	if err := controller.Store.SaveObjectStatus(api.NetAPIVersion, "BGPPeer", "k8s", map[string]any{
		"phase": "Reconverging",
		"peers": []bgpstate.Peer{{
			Address:           "192.168.1.53",
			ASN:               64513,
			State:             gobgpapi.PeerState_SESSION_STATE_IDLE.String(),
			Established:       false,
			LastEstablishedAt: statusvalue.Text(peerStatus["reconvergingSince"]),
			LastErrorAt:       oldErrorAt,
			LastErrorReason:   gobgpapi.PeerState_SESSION_STATE_IDLE.String(),
		}},
	}); err != nil {
		t.Fatalf("seed old peer error status: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile after graceful restart window: %v", err)
	}
	peerStatus = controller.Store.ObjectStatus(api.NetAPIVersion, "BGPPeer", "k8s")
	if peerStatus["phase"] != "Degraded" {
		t.Fatalf("peer status = %#v, want degraded after grace window", peerStatus)
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

func TestReconcileRefreshesPoliciesBeforeGlobalAssignmentAfterRestart(t *testing.T) {
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
	delete(server.policiesByName, "routerd-lan-import-effective")
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
	firstAssignment := indexString(server.callLog, "SetPolicyAssignment")
	if firstPolicy < 0 {
		t.Fatalf("call order = %#v, want SetPolicies", server.callLog)
	}
	if firstAssignment < 0 || firstPolicy > firstAssignment {
		t.Fatalf("call order = %#v, policy refresh must precede global assignment", server.callLog)
	}
	if server.policies == 0 || server.assigns == 0 || server.updates != 0 {
		t.Fatalf("policies/assigns/updates = %d/%d/%d, want global policy refresh without peer update", server.policies, server.assigns, server.updates)
	}
}

func TestReconcileReaddsDeletedPeerWithoutReapplyingConvergedGlobalPolicy(t *testing.T) {
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
	firstAdd := indexStringPrefix(server.callLog, "AddPeer:")
	if firstAdd < 0 {
		t.Fatalf("call order = %#v, want AddPeer", server.callLog)
	}
	if server.policies != 0 || server.adds == 0 {
		t.Fatalf("policies/adds = %d/%d, want only peer re-add after global policy adoption", server.policies, server.adds)
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
	delete(server.policiesByName, "routerd-lan-import-effective")
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
	delete(server.definedSets, definedSetKey(gobgpapi.DefinedType_DEFINED_TYPE_NEIGHBOR, "routerd-lan-import-effective-peer-10-0-0-21-neighbors"))
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

func TestReconcileRefreshesGlobalImportAssignmentDrift(t *testing.T) {
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
	server.policyAssignment.Policies = nil
	server.policies = 0
	server.assigns = 0
	server.updates = 0
	server.resets = 0

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if server.policies != 1 {
		t.Fatalf("SetPolicies calls = %d, want policy reapplied after global assignment drift", server.policies)
	}
	if server.assigns != 1 || server.updates != 0 {
		t.Fatalf("SetPolicyAssignment/UpdatePeer calls = %d/%d, want global assignment refresh only", server.assigns, server.updates)
	}
	if server.resets != 2 {
		t.Fatalf("soft inbound resets = %d, want one per peer", server.resets)
	}
	server.policies = 0
	server.assigns = 0
	server.resets = 0
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("third reconcile: %v", err)
	}
	if server.policies != 0 || server.assigns != 0 || server.updates != 0 || server.resets != 0 {
		t.Fatalf("post-refresh policies/assigns/updates/resets = %d/%d/%d/%d, want converged no-op", server.policies, server.assigns, server.updates, server.resets)
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
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestination("10.77.60.11/32", "10.99.0.2"),
	}}
	fib := &fakeFIB{guardPreferredSource: true, localPreferredSources: map[string]bool{"10.77.60.10": true}}
	controller := Controller{Router: router, Store: mobilityOwnerStore(
		map[string]any{"address": "10.77.60.11/32", "action": "deliver-remote", "ownerNode": "aws-router", "preferredSource": "10.77.60.10/32"},
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
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestination("10.77.60.11/32", "10.99.0.2"),
	}}
	fib := &fakeFIB{guardPreferredSource: true, localPreferredSources: map[string]bool{}}
	controller := Controller{Router: router, Store: mobilityOwnerStore(
		map[string]any{"address": "10.77.60.11/32", "action": "deliver-remote", "ownerNode": "aws-router", "preferredSource": "10.77.60.10/32"},
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
	controller := Controller{Router: router, Store: mobilityOwnerStore(), Server: server, FIB: fib}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "10.77.60.4/32", NextHops: []string{"10.99.0.2"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("fib routes = %#v, want return-route installed without owner retain %#v", fib.routes, want)
	}
}

func TestReconcileInstallsTransportScopedMobilityRouteOnRRWithoutPool(t *testing.T) {
	const (
		mobilityPrefix = "192.168.123.0/24"
		leafTunnel     = "10.255.1.72"
	)
	leafIdentity := bgpstate.MobilityNodeIdentityCommunity("pve-rt-06")
	otherIdentity := bgpstate.MobilityNodeIdentityCommunity("pve-rt-07")
	router := bgpRouterWithImportPrefixes(mobilityPrefix)
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
			Metadata: api.ObjectMeta{Name: "svnet1-rr"},
			Spec: api.SAMTransportProfileSpec{
				SelfNodeRef: "rr-a",
				Mode:        "ipip",
				InnerPrefix: "10.255.1.0/24",
				BGP: api.SAMTransportBGPProfileSpec{
					RouterRef:            "BGPRouter/lan",
					RouteReflectorClient: true,
					ImportPolicy: api.BGPImportPolicySpec{
						AllowedPrefixes:        []string{mobilityPrefix},
						AllowedPrefixLengthMin: 32,
						AllowedPrefixLengthMax: 32,
					},
				},
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{
				Name:      "sam-transport-svnet1-rr-pve-rt-06",
				OwnerRefs: []api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile", Name: "svnet1-rr"}},
				Annotations: map[string]string{
					"mobility.routerd.net/transport-profile": "svnet1-rr",
					"mobility.routerd.net/self-node":         "rr-a",
					"mobility.routerd.net/peer-node":         "pve-rt-06",
				},
			},
			Spec: api.BGPPeerSpec{
				RouterRef:            "BGPRouter/lan",
				PeerASN:              64512,
				Peers:                []string{leafTunnel},
				RouteReflectorClient: true,
				ImportPolicy: api.BGPImportPolicySpec{
					AllowedPrefixes:        []string{mobilityPrefix},
					AllowedPrefixLengthMin: 32,
					AllowedPrefixLengthMax: 32,
					RequiredCommunities:    []string{leafIdentity},
					ForbiddenCommunities:   []string{otherIdentity},
				},
			},
		},
	)
	valid := testDestinationWithCommunities("192.168.123.111/32", leafTunnel, bgpstate.MobilityCommunityOwner, leafIdentity)
	valid.Paths[0].NeighborIp = leafTunnel
	wrongNeighbor := testDestinationWithCommunities("192.168.123.112/32", "10.255.1.73", bgpstate.MobilityCommunityOwner, leafIdentity)
	wrongNeighbor.Paths[0].NeighborIp = "10.255.1.73"
	wrongIdentity := testDestinationWithCommunities("192.168.123.113/32", leafTunnel, bgpstate.MobilityCommunityOwner, otherIdentity)
	wrongIdentity.Paths[0].NeighborIp = leafTunnel
	server := &fakeServer{routes: []*gobgpapi.Destination{valid, wrongNeighbor, wrongIdentity}}
	fib := &fakeFIB{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: fib}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "192.168.123.111/32", NextHops: []string{leafTunnel}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("RR FIB routes = %#v, want only typed transport route %#v", fib.routes, want)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	prefixes, ok := status["prefixes"].([]bgpstate.Prefix)
	if !ok {
		t.Fatalf("RR RIB status prefixes = %#v, want typed prefixes", status["prefixes"])
	}
	seen := map[string]bool{}
	for _, prefix := range prefixes {
		seen[prefix.Prefix] = true
	}
	for _, prefix := range []string{"192.168.123.111/32", "192.168.123.112/32", "192.168.123.113/32"} {
		if !seen[prefix] {
			t.Fatalf("RR RIB status prefixes = %#v, missing observed route %s", prefixes, prefix)
		}
	}
}

func TestReconcileDoesNotInstallSAMTransportInnerRoutesInFIB(t *testing.T) {
	const (
		transportAggregate = "10.255.0.0/16"
		innerPrefix        = "10.255.1.0/24"
		mobilityPrefix     = "192.168.123.0/24"
	)
	router := bgpRouterWithImportPrefixes(transportAggregate, mobilityPrefix)
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
		Metadata: api.ObjectMeta{Name: "svnet1-core"},
		Spec: api.SAMTransportProfileSpec{
			SelfNodeRef: "pve-rt-07",
			Mode:        "ipip",
			InnerPrefix: innerPrefix,
			BGP: api.SAMTransportBGPProfileSpec{
				RouterRef: "BGPRouter/lan",
			},
		},
	})
	server := &fakeServer{routes: []*gobgpapi.Destination{
		testDestination(transportAggregate, "10.255.1.35", "10.255.1.73"),
		testDestination(innerPrefix, "10.255.1.35", "10.255.1.73"),
		testDestination("10.255.1.34/31", "10.255.1.35"),
		testDestination("192.168.123.111/32", "10.255.1.35"),
	}}
	fib := &fakeFIB{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: fib}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	want := []FIBRoute{{Prefix: "192.168.123.111/32", NextHops: []string{"10.255.1.35"}}}
	if !reflect.DeepEqual(fib.routes, want) {
		t.Fatalf("FIB routes = %#v, want only non-transport route %#v", fib.routes, want)
	}
	status := controller.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	prefixes, ok := status["prefixes"].([]bgpstate.Prefix)
	if !ok {
		t.Fatalf("RIB status prefixes = %#v, want observed prefixes", status["prefixes"])
	}
	seen := map[string]bool{}
	for _, prefix := range prefixes {
		seen[prefix.Prefix] = true
	}
	for _, prefix := range []string{transportAggregate, innerPrefix, "10.255.1.34/31", "192.168.123.111/32"} {
		if !seen[prefix] {
			t.Fatalf("RIB status prefixes = %#v, missing %s", prefixes, prefix)
		}
	}
}

func TestExactIPv4TransitPrefixesNormalizesHostBits(t *testing.T) {
	prefixes, ok := exactIPv4TransitPrefixes(api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"192.168.123.111/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
	})
	if !ok {
		t.Fatal("exactIPv4TransitPrefixes rejected a valid IPv4 prefix")
	}
	want := []netip.Prefix{netip.MustParsePrefix("192.168.123.0/24")}
	if !reflect.DeepEqual(prefixes, want) {
		t.Fatalf("transit prefixes = %#v, want %#v", prefixes, want)
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
	firstObserved := statusvalue.Text(status["observedAt"])
	if firstObserved == "" {
		t.Fatal("missing observedAt after initial reconcile")
	}
	server.watchSessions <- watchSession{events: []*gobgpapi.WatchEventResponse{
		watchPeerStateEvent("10.99.0.11", gobgpapi.PeerState_SESSION_STATE_ESTABLISHED),
	}}
	if err := controller.watchBestPathEvents(context.Background()); err != nil {
		t.Fatalf("watch events: %v", err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "lan")
	secondObserved := statusvalue.Text(status["observedAt"])
	if secondObserved == "" || secondObserved == firstObserved {
		t.Fatalf("peer state change did not trigger re-observation: observedAt before=%q after=%q", firstObserved, secondObserved)
	}
}

func TestGeneratedNeighborScopedImportPolicyIsAcceptedByGoBGP(t *testing.T) {
	server := gobgpserver.NewBgpServer()
	go server.Serve()
	defer server.Stop()
	spec := api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}, LocalPreference: 200}
	prefixes := importPolicyPrefixes(spec)
	if !prefixSetAllows(prefixes, "10.250.0.0/24") || !prefixSetAllows(prefixes, "10.250.0.42/32") {
		t.Fatalf("import prefixes = %#v, want /24 and contained /32 allowed", prefixes)
	}
	if prefixSetAllows(prefixes, "10.88.0.1/32") {
		t.Fatalf("import prefixes = %#v, want unrelated /32 rejected", prefixes)
	}
	plan := buildBGPPolicyPlan("test", api.BGPImportPolicySpec{}, map[string]desiredPeer{
		"10.0.0.2": {Address: "10.0.0.2", ImportPolicy: spec},
	}, nil)
	if got := plan.SetPolicies.GetPolicies()[0].GetStatements()[0].GetActions().GetLocalPref().GetValue(); got != 200 {
		t.Fatalf("generated import policy local preference = %d, want 200", got)
	}
	if err := server.SetPolicies(context.Background(), plan.SetPolicies); err != nil {
		t.Fatalf("SetPolicies rejected generated import policy: %v", err)
	}
}

func TestGeneratedPrefixlessImportPolicyWithDirectPreferenceIsAcceptedByGoBGP(t *testing.T) {
	server := gobgpserver.NewBgpServer()
	go server.Serve()
	defer server.Stop()
	plan := buildBGPPolicyPlan("direct", api.BGPImportPolicySpec{}, map[string]desiredPeer{
		"10.0.0.2": {Address: "10.0.0.2", ImportPolicy: api.BGPImportPolicySpec{
			NextHopRewrite:  "peer-address",
			LocalPreference: 200,
		}},
	}, nil)
	if len(plan.SetPolicies.GetDefinedSets()) != 1 || len(plan.SetPolicies.GetPolicies()) != 1 {
		t.Fatalf("prefixless direct import policy = %#v, want one policy and only its neighbor set", plan.SetPolicies)
	}
	allow := plan.SetPolicies.GetPolicies()[0].GetStatements()[0]
	if allow.GetConditions().GetPrefixSet() != nil || allow.GetActions().GetLocalPref().GetValue() != 200 {
		t.Fatalf("prefixless direct allow statement = %#v, want unrestricted local-preference action", allow)
	}
	if allow.GetConditions().GetNeighborSet().GetName() == "" {
		t.Fatalf("prefixless direct allow statement = %#v, want neighbor condition", allow)
	}
	if err := server.SetPolicies(context.Background(), plan.SetPolicies); err != nil {
		t.Fatalf("SetPolicies rejected prefixless direct import policy: %v", err)
	}
}

// TestGlobalImportPolicyPrefersDirectPeerInRealGoBGP exercises the policy on
// the same normal-peer/global-RIB path used by routerd.  A peer-local import
// assignment is deliberately not involved: GoBGP only evaluates that form for
// route-server clients, whereas routerd's BGP and RR peers use the global RIB.
// All sockets stay inside 127/8; this test does not alter host networking.
func TestGlobalImportPolicyPrefersDirectPeerInRealGoBGP(t *testing.T) {
	ctx := context.Background()
	const (
		asn       = 64512
		centralIP = "127.0.0.1"
		directIP  = "127.0.0.2"
		rrIP      = "127.0.0.3"
		emptyIP   = "127.0.0.4"
		prefix    = "192.0.2.10/32"
	)
	port := loopbackTCPPort(t)

	central := &testGoBGPServer{BgpServer: gobgpserver.NewBgpServer()}
	go central.Serve()
	defer central.Stop()
	if err := central.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: &gobgpapi.Global{
		Asn:              asn,
		RouterId:         "10.255.0.1",
		ListenPort:       int32(port),
		ListenAddresses:  []string{centralIP},
		Families:         []uint32{0},
		UseMultiplePaths: true,
	}}); err != nil {
		t.Fatalf("start central GoBGP: %v", err)
	}

	direct := desiredPeer{
		Address:     directIP,
		ASN:         asn,
		LocalASN:    asn,
		PassiveMode: true,
		ImportPolicy: api.BGPImportPolicySpec{
			AllowedPrefixes: []string{prefix},
			NextHopRewrite:  "peer-address",
			LocalPreference: 200,
		},
	}
	rr := desiredPeer{
		Address:     rrIP,
		ASN:         asn,
		LocalASN:    asn,
		PassiveMode: true,
		ImportPolicy: api.BGPImportPolicySpec{
			AllowedPrefixes: []string{prefix},
			NextHopRewrite:  "peer-address",
			LocalPreference: 100,
		},
	}
	// A signed direct leaf can be connected before it owns a mobility /32.
	// Its session must come up, but even an erroneous route from that neighbor
	// must not be admitted through a prefixless direct policy.
	empty := desiredPeer{
		Address:         emptyIP,
		ASN:             asn,
		LocalASN:        asn,
		PassiveMode:     true,
		RejectImportAll: true,
		ImportPolicy: api.BGPImportPolicySpec{
			NextHopRewrite:  "peer-address",
			LocalPreference: 200,
		},
	}
	desired := map[string]desiredPeer{directIP: direct, rrIP: rr, emptyIP: empty}
	plan := buildBGPPolicyPlan("mesh", api.BGPImportPolicySpec{}, desired, nil)
	if err := central.SetPolicies(ctx, plan.SetPolicies); err != nil {
		t.Fatalf("set global import policies: %v", err)
	}
	if err := central.SetPolicyAssignment(ctx, &gobgpapi.SetPolicyAssignmentRequest{Assignment: plan.GlobalImportAssignment}); err != nil {
		t.Fatalf("set global import assignment: %v", err)
	}
	for _, peer := range []desiredPeer{direct, rr, empty} {
		if err := central.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: goBGPPeer(peer)}); err != nil {
			t.Fatalf("add central peer %s: %v", peer.Address, err)
		}
	}

	startRemote := func(routerID, localAddress string) *testGoBGPServer {
		t.Helper()
		remote := &testGoBGPServer{BgpServer: gobgpserver.NewBgpServer()}
		go remote.Serve()
		t.Cleanup(remote.Stop)
		if err := remote.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: &gobgpapi.Global{
			Asn:        asn,
			RouterId:   routerID,
			ListenPort: -1,
			Families:   []uint32{0},
		}}); err != nil {
			t.Fatalf("start remote %s: %v", localAddress, err)
		}
		if err := remote.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: &gobgpapi.Peer{
			Conf: &gobgpapi.PeerConf{
				NeighborAddress: centralIP,
				PeerAsn:         asn,
				Type:            gobgpapi.PeerType_PEER_TYPE_INTERNAL,
			},
			Transport: &gobgpapi.Transport{
				LocalAddress: localAddress,
				RemotePort:   uint32(port),
			},
			AfiSafis: []*gobgpapi.AfiSafi{goBGPAFISAFI(ipv4Family())},
			Timers: &gobgpapi.Timers{Config: &gobgpapi.TimersConfig{
				ConnectRetry:           1,
				IdleHoldTimeAfterReset: 1,
			}},
		}}); err != nil {
			t.Fatalf("add remote peer %s: %v", localAddress, err)
		}
		return remote
	}
	directRemote := startRemote("10.255.0.2", directIP)
	rrRemote := startRemote("10.255.0.3", rrIP)
	emptyRemote := startRemote("10.255.0.4", emptyIP)

	established := func(address string) bool {
		ready := false
		err := central.BgpServer.ListPeer(ctx, &gobgpapi.ListPeerRequest{Address: address}, func(peer *gobgpapi.Peer) {
			ready = peer.GetState().GetSessionState() == gobgpapi.PeerState_SESSION_STATE_ESTABLISHED
		})
		return err == nil && ready
	}
	waitForCondition(t, 5*time.Second, func() bool { return established(directIP) && established(rrIP) && established(emptyIP) })

	advertise := func(server *testGoBGPServer, nextHop string) {
		t.Helper()
		if _, err := server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: &gobgpapi.Path{
			Family: ipv4Family(),
			Nlri:   ipAddressNLRI(netip.MustParsePrefix(prefix)),
			Pattrs: []*gobgpapi.Attribute{originAttribute(), nextHopAttribute(nextHop)},
		}}); err != nil {
			t.Fatalf("advertise %s through %s: %v", prefix, nextHop, err)
		}
	}
	advertise(directRemote, directIP)
	advertise(rrRemote, rrIP)
	advertise(emptyRemote, emptyIP)

	var got *gobgpapi.Destination
	readDestination := func() *gobgpapi.Destination {
		var destination *gobgpapi.Destination
		err := central.ListPath(ctx, &gobgpapi.ListPathRequest{
			TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL,
			Family:    ipv4Family(),
		}, func(candidate *gobgpapi.Destination) {
			if normalizeRoutePrefix(candidate.GetPrefix()) == prefix {
				destination = candidate
			}
		})
		if err != nil {
			return nil
		}
		return destination
	}
	waitForCondition(t, 5*time.Second, func() bool {
		got = readDestination()
		return got != nil && len(got.GetPaths()) == 2
	})

	var directPath, rrPath *gobgpapi.Path
	for _, path := range got.GetPaths() {
		switch pathNextHop(path) {
		case directIP:
			directPath = path
		case rrIP:
			rrPath = path
		case emptyIP:
			t.Fatalf("empty-ownership direct peer route was imported: %#v", path)
		}
	}
	if directPath == nil || rrPath == nil {
		t.Fatalf("imported paths = %#v, want direct and RR next hops", got.GetPaths())
	}
	if !directPath.GetBest() || pathRank(directPath).LocalPref != 200 {
		t.Fatalf("direct path = %#v, want best local-preference 200", directPath)
	}
	if rrPath.GetBest() || pathRank(rrPath).LocalPref != 100 {
		t.Fatalf("RR path = %#v, want non-best local-preference 100", rrPath)
	}
	routes := fibRoutesFromDestination(got, allowedImportPrefixesForTest(direct.ImportPolicy), peerAddressFIBRewritePeers(desired), nil)
	wantRoutes := []FIBRoute{{Prefix: prefix, NextHops: []string{directIP}}}
	if !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatalf("FIB routes = %#v, want %#v", routes, wantRoutes)
	}
}

func loopbackTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback TCP port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
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
	spec := api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}, LocalPreference: 200}
	peers := map[string]desiredPeer{
		"10.0.0.21": {
			Address:      "10.0.0.21",
			ASN:          64513,
			LocalASN:     64512,
			ImportPolicy: spec,
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

func TestImportPolicyDriftDetectsLocalPreference(t *testing.T) {
	ctx := context.Background()
	spec := api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}, LocalPreference: 200}
	peers := map[string]desiredPeer{"10.0.0.21": {Address: "10.0.0.21", ImportPolicy: spec}}
	server := &fakeServer{}
	controller := Controller{Server: server}
	if err := controller.applyBGPPolicies(ctx, "lan", spec, peers, nil); err != nil {
		t.Fatalf("applyBGPPolicies: %v", err)
	}
	policy := server.policiesByName[effectiveImportPolicyName("lan")]
	if policy == nil || len(policy.GetStatements()) != 2 {
		t.Fatalf("import policy = %#v, want allow and terminal reject statements", policy)
	}
	policy.GetStatements()[0].GetActions().LocalPref.Value = 201
	drift, err := controller.importPolicyDrift(ctx, "lan", spec, peers, nil)
	if err != nil {
		t.Fatalf("importPolicyDrift: %v", err)
	}
	if !drift.PolicyState {
		t.Fatalf("importPolicyDrift = %#v, want local-preference policy drift", drift)
	}
}

func TestImportPolicyDriftDetectsCommunityFilter(t *testing.T) {
	ctx := context.Background()
	spec := api.BGPImportPolicySpec{
		AllowedPrefixes:      []string{"10.250.0.0/24"},
		RequiredCommunities:  []string{"64512:301"},
		ForbiddenCommunities: []string{"64512:302"},
	}
	peers := map[string]desiredPeer{"10.0.0.21": {Address: "10.0.0.21", ImportPolicy: spec}}
	server := &fakeServer{}
	controller := Controller{Server: server}
	if err := controller.applyBGPPolicies(ctx, "lan", spec, peers, nil); err != nil {
		t.Fatalf("applyBGPPolicies: %v", err)
	}
	requiredName := "routerd-lan-import-effective-peer-10-0-0-21-required-communities"
	required := server.definedSets[definedSetKey(gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY, requiredName)]
	if required == nil || len(required.GetList()) != 1 {
		t.Fatalf("required community set = %#v", required)
	}
	required.List[0] = "64512:999"
	drift, err := controller.importPolicyDrift(ctx, "lan", spec, peers, nil)
	if err != nil {
		t.Fatalf("importPolicyDrift: %v", err)
	}
	if !drift.PolicyState {
		t.Fatalf("importPolicyDrift = %#v, want community filter drift", drift)
	}
}

func TestAppliedImportPolicyLocalPreferenceRoundTrip(t *testing.T) {
	global := appliedGlobalFromSpec(api.BGPRouterSpec{
		ASN:          64512,
		RouterID:     "10.0.0.1",
		ImportPolicy: api.BGPImportPolicySpec{LocalPreference: 200},
	}, nil)
	if global.ImportPolicy.LocalPreference != 200 {
		t.Fatalf("applied global local preference = %d, want 200", global.ImportPolicy.LocalPreference)
	}
	applied := appliedPeer(desiredPeer{
		Address:                "10.0.0.2",
		ASN:                    64513,
		PreserveImportPrefixes: true,
		RejectImportAll:        true,
		ImportPolicy: api.BGPImportPolicySpec{
			AllowedPrefixes:        []string{"10.77.60.22/32"},
			AllowedPrefixLengthMin: 32,
			AllowedPrefixLengthMax: 32,
			RequiredCommunities:    []string{"64512:301"},
			ForbiddenCommunities:   []string{"64512:302"},
			NextHopRewrite:         "peer-address",
			LocalPreference:        201,
		},
	})
	if applied.ImportPolicy.LocalPreference != 201 {
		t.Fatalf("applied peer local preference = %d, want 201", applied.ImportPolicy.LocalPreference)
	}
	restored := desiredPeersFromApplied(64512, map[string]bgpdaemon.AppliedPeer{"10.0.0.2": applied})["10.0.0.2"]
	if restored.ImportPolicy.LocalPreference != 201 || restored.ImportPolicy.AllowedPrefixLengthMin != 32 || restored.ImportPolicy.AllowedPrefixLengthMax != 32 ||
		!sameStringSet(restored.ImportPolicy.AllowedPrefixes, []string{"10.77.60.22/32"}) ||
		!sameStringSet(restored.ImportPolicy.RequiredCommunities, []string{"64512:301"}) ||
		!sameStringSet(restored.ImportPolicy.ForbiddenCommunities, []string{"64512:302"}) ||
		!restored.PreserveImportPrefixes || !restored.RejectImportAll {
		t.Fatalf("restored direct peer import boundary = %#v", restored)
	}
}

func TestImportPolicyKeysIncludeLocalPreference(t *testing.T) {
	base := api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}}
	if action := importLocalPreferenceAction(base); action != nil {
		t.Fatalf("zero local preference action = %#v, want nil", action)
	}
	preferred := base
	preferred.LocalPreference = 200
	if importPolicyKey(base) == importPolicyKey(preferred) {
		t.Fatal("importPolicyKey ignored local preference")
	}
	if bgpPoliciesKey(base, nil, nil) == bgpPoliciesKey(preferred, nil, nil) {
		t.Fatal("bgpPoliciesKey ignored global local preference")
	}
	peer := desiredPeer{
		Address:      "10.0.0.2",
		ImportPolicy: base,
	}
	preferredPeer := peer
	preferredPeer.ImportPolicy.LocalPreference = 201
	if bgpPoliciesKey(base, map[string]desiredPeer{peer.Address: peer}, nil) == bgpPoliciesKey(base, map[string]desiredPeer{preferredPeer.Address: preferredPeer}, nil) {
		t.Fatal("bgpPoliciesKey ignored peer local preference")
	}
	prefixlessPeer := desiredPeer{Address: "10.0.0.3", ImportPolicy: api.BGPImportPolicySpec{LocalPreference: 200}}
	prefixlessPreferredPeer := prefixlessPeer
	prefixlessPreferredPeer.ImportPolicy.LocalPreference = 201
	if bgpImportPoliciesKey(base, map[string]desiredPeer{prefixlessPeer.Address: prefixlessPeer}, nil) == bgpImportPoliciesKey(base, map[string]desiredPeer{prefixlessPreferredPeer.Address: prefixlessPreferredPeer}, nil) {
		t.Fatal("bgpImportPoliciesKey ignored prefixless peer local preference")
	}
	rejectAllPeer := prefixlessPeer
	rejectAllPeer.RejectImportAll = true
	if bgpImportPoliciesKey(base, map[string]desiredPeer{prefixlessPeer.Address: prefixlessPeer}, nil) == bgpImportPoliciesKey(base, map[string]desiredPeer{rejectAllPeer.Address: rejectAllPeer}, nil) {
		t.Fatal("bgpImportPoliciesKey ignored empty-ownership reject-all boundary")
	}
	dynamic := desiredDynamicPeer{
		PeerGroupName: "routerd-dynamic-leaves",
		ImportPolicy:  base,
	}
	preferredDynamic := dynamic
	preferredDynamic.ImportPolicy.LocalPreference = 202
	if bgpPoliciesKey(base, nil, map[string]desiredDynamicPeer{dynamic.PeerGroupName: dynamic}) == bgpPoliciesKey(base, nil, map[string]desiredDynamicPeer{preferredDynamic.PeerGroupName: preferredDynamic}) {
		t.Fatal("bgpPoliciesKey ignored dynamic peer local preference")
	}
	communityConstrained := base
	communityConstrained.RequiredCommunities = []string{"64512:301"}
	communityConstrained.ForbiddenCommunities = []string{"64512:302"}
	if importPolicyKey(base) == importPolicyKey(communityConstrained) {
		t.Fatal("importPolicyKey ignored community filters")
	}
	if bgpPoliciesKey(base, nil, nil) == bgpPoliciesKey(communityConstrained, nil, nil) {
		t.Fatal("bgpPoliciesKey ignored global community filters")
	}
	communityPeer := peer
	communityPeer.ImportPolicy = communityConstrained
	if bgpPoliciesKey(base, map[string]desiredPeer{peer.Address: peer}, nil) == bgpPoliciesKey(base, map[string]desiredPeer{communityPeer.Address: communityPeer}, nil) {
		t.Fatal("bgpPoliciesKey ignored peer community filters")
	}
	communityDynamic := dynamic
	communityDynamic.ImportPolicy = communityConstrained
	if bgpPoliciesKey(base, nil, map[string]desiredDynamicPeer{dynamic.PeerGroupName: dynamic}) == bgpPoliciesKey(base, nil, map[string]desiredDynamicPeer{communityDynamic.PeerGroupName: communityDynamic}) {
		t.Fatal("bgpPoliciesKey ignored dynamic peer community filters")
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
	store := mobilityFIBStore{mapStore: mapStore{}, records: mobilityFIBRecords(
		map[string]any{"address": "10.77.60.4/32", "action": "local-route", "ownerNode": "aws-router"},
		map[string]any{"address": "10.77.60.11/32", "action": "deliver-remote", "ownerNode": "aws-router-b"},
	)}
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
	store := mobilityFIBStore{mapStore: mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {},
	}, records: mobilityFIBRecords(
		map[string]any{"address": "10.77.60.11/32", "action": "local-route", "ownerNode": "aws-router-b", "reason": "remote-home-owner-overlaps-local-ownership-event"},
		map[string]any{"address": "10.77.60.12/32", "action": "deliver-remote", "ownerNode": "azure-router"},
	)}
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
			State: &gobgpapi.PeerState{NeighborAddress: "10.0.0.21", PeerAsn: 64513, SessionState: gobgpapi.PeerState_SESSION_STATE_ESTABLISHED},
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

func TestReconcileTagsAndReplacesStaticAdvertisementCommunities(t *testing.T) {
	router := bgpRouter()
	bgpResource := router.Spec.Resources[0]
	bgpSpec := bgpResource.Spec.(api.BGPRouterSpec)
	identity := bgpstate.MobilityNodeIdentityCommunity("leaf-a")
	bgpSpec.Communities = api.BGPCommunitiesSpec{Set: api.BGPCommunitySetSpec{Out: []string{identity}}}
	bgpResource.Spec = bgpSpec
	router.Spec.Resources[0] = bgpResource

	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if server.paths != 1 || len(server.routes) == 0 {
		t.Fatalf("static advertisements = paths:%d routes:%#v", server.paths, server.routes)
	}
	if got := pathCommunities(server.routes[0].GetPaths()[0]); !sameStringSet(got, []string{identity}) {
		t.Fatalf("static GoBGP path communities = %#v, want %#v", got, []string{identity})
	}
	static := staticAppliedPaths(server.applied.Paths)["10.0.0.0/16"]
	if !sameStringSet(static.Attrs.Communities, []string{identity}) {
		t.Fatalf("persisted static path attrs = %#v, want identity", static.Attrs)
	}

	bgpResource = router.Spec.Resources[0]
	bgpSpec = bgpResource.Spec.(api.BGPRouterSpec)
	bgpSpec.Communities.Set.Out = []string{"64512:999"}
	bgpResource.Spec = bgpSpec
	router.Spec.Resources[0] = bgpResource
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("community replacement reconcile: %v", err)
	}
	if server.paths != 2 || len(server.deletedPathUUIDs) != 1 {
		t.Fatalf("static community replacement add/delete = %d/%d, want 2/1", server.paths, len(server.deletedPathUUIDs))
	}
	static = staticAppliedPaths(server.applied.Paths)["10.0.0.0/16"]
	if !sameStringSet(static.Attrs.Communities, []string{"64512:999"}) {
		t.Fatalf("replaced persisted static path attrs = %#v", static.Attrs)
	}
}

func TestReconcileStaticAdvertisementWithdrawalRecoversFromAlreadyMissingPath(t *testing.T) {
	router := bgpRouter()
	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	bgpResource := router.Spec.Resources[0]
	bgpSpec := bgpResource.Spec.(api.BGPRouterSpec)
	bgpSpec.ExportPolicy.AllowedPrefixes = nil
	bgpResource.Spec = bgpSpec
	router.Spec.Resources[0] = bgpResource
	server.deletePathErrors = []error{errors.New("can't find a specified path")}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("withdrawal after already-missing path: %v", err)
	}
	if _, found := staticAppliedPaths(server.applied.Paths)["10.0.0.0/16"]; found {
		t.Fatalf("applied paths retained withdrawn static advertisement: %#v", server.applied.Paths)
	}
}

func TestReconcileFencesStaticWithdrawalBeforeLiveDelete(t *testing.T) {
	router := bgpRouter()
	server := &fakeServer{}
	controller := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	bgpResource := router.Spec.Resources[0]
	bgpSpec := bgpResource.Spec.(api.BGPRouterSpec)
	bgpSpec.ExportPolicy.AllowedPrefixes = nil
	bgpResource.Spec = bgpSpec
	router.Spec.Resources[0] = bgpResource
	server.deletePathErrors = []error{errors.New("simulated static withdrawal interruption")}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("static withdrawal interruption should leave reconciliation pending")
	}
	if _, found := staticAppliedPaths(server.applied.Paths)["10.0.0.0/16"]; found {
		t.Fatalf("restart state retained static path being withdrawn: %#v", server.applied.Paths)
	}
	retiring := staticAppliedPaths(server.applied.PendingStaticPathRemovals)["10.0.0.0/16"]
	if retiring.Prefix != "10.0.0.0/16" || retiring.UUID == "" {
		t.Fatalf("pending static withdrawal = %#v, want persisted UUID", server.applied.PendingStaticPathRemovals)
	}

	// A process restart sees only the tombstone, retries the idempotent UUID
	// withdrawal, and finishes without re-advertising the removed prefix.
	recovered := Controller{Router: router, Store: mapStore{}, Server: server, FIB: &fakeFIB{}}
	if err := recovered.Reconcile(context.Background()); err != nil {
		t.Fatalf("recover static withdrawal: %v", err)
	}
	if len(server.applied.PendingStaticPathRemovals) != 0 {
		t.Fatalf("applied state retained completed static withdrawal: %#v", server.applied.PendingStaticPathRemovals)
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

func TestReconcileUpdatesPeerWhenPassiveModeChangesAcrossRestart(t *testing.T) {
	router := bgpRouter()
	server := &fakeServer{}
	reconcile := func() {
		t.Helper()
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
	}

	reconcile()
	if server.adds != 1 || server.deletes != 0 || server.updates != 0 {
		t.Fatalf("initial counts adds=%d deletes=%d updates=%d, want 1/0/0", server.adds, server.deletes, server.updates)
	}

	peer := router.Spec.Resources[1]
	spec, err := peer.BGPPeerSpec()
	if err != nil {
		t.Fatalf("peer spec: %v", err)
	}
	spec.PassiveMode = true
	peer.Spec = spec
	router.Spec.Resources[1] = peer
	reconcile()
	if server.adds != 1 || server.deletes != 0 || server.updates != 1 {
		t.Fatalf("active->passive counts adds=%d deletes=%d updates=%d, want 1/0/1", server.adds, server.deletes, server.updates)
	}
	if transport := server.peers["10.0.0.21"].Transport; transport == nil || !transport.PassiveMode {
		t.Fatalf("active->passive transport = %#v, want explicit passive", transport)
	}
	if !server.applied.Peers["10.0.0.21"].PassiveMode {
		t.Fatalf("active->passive applied peer = %#v, want passive", server.applied.Peers["10.0.0.21"])
	}

	spec.PassiveMode = false
	peer.Spec = spec
	router.Spec.Resources[1] = peer
	reconcile()
	if server.adds != 1 || server.deletes != 0 || server.updates != 2 {
		t.Fatalf("passive->active counts adds=%d deletes=%d updates=%d, want 1/0/2", server.adds, server.deletes, server.updates)
	}
	if transport := server.peers["10.0.0.21"].Transport; transport != nil {
		t.Fatalf("passive->active transport = %#v, want nil default-active transport", transport)
	}
	if server.applied.Peers["10.0.0.21"].PassiveMode {
		t.Fatalf("passive->active applied peer = %#v, want active", server.applied.Peers["10.0.0.21"])
	}

	reconcile()
	if server.adds != 1 || server.deletes != 0 || server.updates != 2 {
		t.Fatalf("unchanged active counts adds=%d deletes=%d updates=%d, want no churn", server.adds, server.deletes, server.updates)
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
			State:  &gobgpapi.PeerState{NeighborAddress: "10.0.0.21", PeerAsn: 64513, SessionState: gobgpapi.PeerState_SESSION_STATE_ESTABLISHED},
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
			SessionState: gobgpapi.PeerState_SESSION_STATE_ESTABLISHED,
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

func TestFIBRoutesFromDestinationUsesGoBGPSelectedECMPSet(t *testing.T) {
	allowed := allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}})
	destination := testRankedDestination("10.77.60.12/32",
		rankedPath{nextHop: "10.255.0.124", localPref: 100},
		rankedPath{nextHop: "10.255.0.211", localPref: 100},
		rankedPath{nextHop: "10.255.0.238", localPref: 100},
	)
	destination.Paths[1].Best = true

	routes := fibRoutesFromDestination(destination, allowed, nil, nil)
	want := []FIBRoute{{Prefix: "10.77.60.12/32", NextHops: []string{"10.255.0.211"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want selected direct path %#v", routes, want)
	}

	destination.Paths[1].Best = false
	destination.Paths[0].Best = true
	destination.Paths[2].Best = true
	routes = fibRoutesFromDestination(destination, allowed, nil, nil)
	want = []FIBRoute{{Prefix: "10.77.60.12/32", NextHops: []string{"10.255.0.124", "10.255.0.238"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want selected ECMP set %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationDoesNotOverrideLocalBestPath(t *testing.T) {
	dst := testRankedDestination("10.77.60.12/32",
		rankedPath{nextHop: "0.0.0.0", localPref: 201},
		rankedPath{nextHop: "10.255.0.2", localPref: 200},
	)
	dst.Paths[0].Best = true
	routes := fibRoutesFromDestination(dst, allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}}), nil, nil)
	if len(routes) != 0 {
		t.Fatalf("routes = %#v, want no FIB route when GoBGP selected a local path", routes)
	}
}

func TestFIBRoutesFromDestinationPrefersLiveRRPathOverStaleDirectMeshPath(t *testing.T) {
	dst := testRankedDestination("10.77.60.12/32",
		rankedPath{nextHop: "10.255.0.2", localPref: 200},
		rankedPath{nextHop: "10.255.0.1", localPref: 100},
	)
	// GoBGP can keep the stale direct path as best for the graceful-restart
	// window even though the RR path is still live.
	dst.Paths[0].Best = true
	dst.Paths[0].Stale = true
	routes := fibRoutesFromDestination(dst, allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}}), nil, nil)
	want := []FIBRoute{{Prefix: "10.77.60.12/32", NextHops: []string{"10.255.0.1"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want live RR route %#v", routes, want)
	}
}

func TestFIBRoutesFromDestinationRetainsStalePathWithoutLiveAlternative(t *testing.T) {
	dst := testRankedDestination("10.77.60.12/32", rankedPath{nextHop: "10.255.0.2", localPref: 200})
	dst.Paths[0].Best = true
	dst.Paths[0].Stale = true
	routes := fibRoutesFromDestination(dst, allowedImportPrefixesForTest(api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}}), nil, nil)
	want := []FIBRoute{{Prefix: "10.77.60.12/32", NextHops: []string{"10.255.0.2"}}}
	if !reflect.DeepEqual(routes, want) {
		t.Fatalf("routes = %#v, want stale graceful-restart route %#v", routes, want)
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
	nlri := ipAddressNLRI(parsed)
	var paths []*gobgpapi.Path
	for _, path := range ranked {
		paths = append(paths, &gobgpapi.Path{
			Family: ipv4Family(),
			Nlri:   nlri,
			Pattrs: []*gobgpapi.Attribute{
				nextHopAttribute(path.nextHop),
				localPrefAttribute(path.localPref),
				medAttribute(path.med),
			},
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
				Type: gobgpapi.WatchEventResponse_PeerEvent_TYPE_STATE,
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

func testDestination(prefix string, nextHops ...string) *gobgpapi.Destination {
	parsed := netip.MustParsePrefix(prefix)
	nlri := ipAddressNLRI(parsed)
	var paths []*gobgpapi.Path
	for _, nextHop := range nextHops {
		paths = append(paths, &gobgpapi.Path{
			Family: ipv4Family(),
			Nlri:   nlri,
			Pattrs: []*gobgpapi.Attribute{nextHopAttribute(nextHop)},
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
	nlri := ipAddressNLRI(parsed)
	attrs := []*gobgpapi.Attribute{nextHopAttribute(nextHop)}
	if len(communities) > 0 {
		values, err := standardCommunityValuesForTest(communities)
		if err != nil {
			panic(err)
		}
		attrs = append(attrs, communitiesAttribute(values))
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
