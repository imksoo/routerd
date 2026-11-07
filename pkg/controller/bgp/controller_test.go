// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpapiutil "github.com/osrg/gobgp/v4/pkg/apiutil"
	gobgp "github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgpserver "github.com/osrg/gobgp/v4/pkg/server"

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

func TestApplyRouterBGPDefaultsPreservesExplicitPeerConvergenceProfile(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "sam-inherited"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/mobility",
				PeerASN:   64512,
				Peers:     []string{"10.255.0.2"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "sam-explicit"},
			Spec: api.BGPPeerSpec{
				RouterRef:          "BGPRouter/mobility",
				PeerASN:            64512,
				Peers:              []string{"10.255.0.3"},
				ConvergenceProfile: "stable",
			},
		},
	}}}
	desired, err := (&Controller{Router: router}).desiredPeers("mobility", 64512)
	if err != nil {
		t.Fatalf("desiredPeers: %v", err)
	}
	peers := applyRouterBGPDefaults("mobility", api.BGPRouterSpec{
		ConvergenceProfile: "fast",
	}, desired, nil, nil)
	if got := peers["10.255.0.2"]; got.ConvergenceProfile != "fast" || got.GracefulRestart.Enabled == nil || *got.GracefulRestart.Enabled {
		t.Fatalf("inherited peer convergence = %#v, want fast with graceful restart disabled", got)
	}
	if got := peers["10.255.0.3"]; got.ConvergenceProfile != "stable" || got.GracefulRestart.Enabled == nil || !*got.GracefulRestart.Enabled {
		t.Fatalf("explicit peer convergence = %#v, want stable with graceful restart enabled", got)
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
