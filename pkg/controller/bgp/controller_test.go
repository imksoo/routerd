// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"

	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpserver "github.com/osrg/gobgp/v4/pkg/server"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
)

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
