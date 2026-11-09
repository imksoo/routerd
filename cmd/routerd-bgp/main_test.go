// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net/netip"
	"testing"

	gobgpapi "github.com/osrg/gobgp/v4/api"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

func TestAppliedPoliciesRestoreNeighborScopedGlobalImportPolicy(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address: "192.168.1.38",
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes: []string{"10.250.0.0/24"},
			NextHopRewrite:  "peer-address",
		},
	}
	req, assignment := appliedPolicies(bgpdaemon.AppliedConfig{
		Peers: map[string]bgpdaemon.AppliedPeer{
			"192.168.1.38": peer,
		},
	})
	if len(req.GetPolicies()) != 1 || len(req.GetDefinedSets()) != 2 {
		t.Fatalf("restore policies = policies:%d definedSets:%d, want one global policy with neighbor and prefix sets", len(req.GetPolicies()), len(req.GetDefinedSets()))
	}
	policy := req.GetPolicies()[0]
	if policy.GetName() != "routerd-restore-import-effective" {
		t.Fatalf("policy name = %q, want effective global import policy", policy.GetName())
	}
	action := policy.GetStatements()[0].GetActions().GetNexthop()
	if !action.GetPeerAddress() {
		t.Fatalf("next-hop action = %#v, want peer-address rewrite", action)
	}
	if assignment.GetName() != "global" || assignment.GetDirection() != gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT || len(assignment.GetPolicies()) != 1 ||
		assignment.GetPolicies()[0].GetName() != "routerd-restore-import-effective" {
		t.Fatalf("global import policy assignment = %#v, want effective policy", assignment)
	}
	restoredPeer := appliedPeer(peer, bgpdaemon.AppliedGlobal{ASN: 64512})
	if restoredPeer.GetApplyPolicy().GetImportPolicy() != nil {
		t.Fatalf("restored peer import policy = %#v, want no ineffective peer assignment", restoredPeer.GetApplyPolicy().GetImportPolicy())
	}
}

func TestAppliedPeerRestoresPassiveMode(t *testing.T) {
	peer := appliedPeer(
		bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64512, PassiveMode: true},
		bgpdaemon.AppliedGlobal{ASN: 64512},
	)
	if !peer.GetTransport().GetPassiveMode() {
		t.Fatal("restored peer passive mode = false, want true")
	}
}

func TestAppliedPeerRestoresDefaultActiveCompatibility(t *testing.T) {
	peer := appliedPeer(
		bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64512},
		bgpdaemon.AppliedGlobal{ASN: 64512},
	)
	if peer.Transport != nil {
		t.Fatalf("restored default active peer transport = %#v, want nil for pre-passiveMode compatibility", peer.Transport)
	}
}

func TestAppliedPoliciesRestorePeerImportPolicyWithCommunities(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address: "10.99.0.2",
		ASN:     64512,
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes:      []string{"10.77.60.0/24"},
			RequiredCommunities:  []string{"64512:301"},
			ForbiddenCommunities: []string{"64512:302"},
		},
	}
	req, _ := appliedPolicies(bgpdaemon.AppliedConfig{
		Peers: map[string]bgpdaemon.AppliedPeer{"10.99.0.2": peer},
	})
	scopeName := "routerd-restore-import-effective-peer-10-99-0-2"
	if !appliedPolicyRequestHasDefinedSet(req, gobgpapi.DefinedType_DEFINED_TYPE_NEIGHBOR, scopeName+"-neighbors", "10.99.0.2/32") {
		t.Fatalf("defined sets = %#v, want neighbor set", req.GetDefinedSets())
	}
	if !appliedPolicyRequestHasDefinedSet(req, gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY, scopeName+"-required-communities", "64512:301") {
		t.Fatalf("defined sets = %#v, want required community set", req.GetDefinedSets())
	}
	if !appliedPolicyRequestHasDefinedSet(req, gobgpapi.DefinedType_DEFINED_TYPE_COMMUNITY, scopeName+"-forbidden-communities", "64512:302") {
		t.Fatalf("defined sets = %#v, want forbidden community set", req.GetDefinedSets())
	}
	if len(req.GetPolicies()) != 1 || len(req.GetPolicies()[0].GetStatements()) != 3 {
		t.Fatalf("policies = %#v, want reject-forbidden, allow-import, then terminal reject", req.GetPolicies())
	}
	reject := req.GetPolicies()[0].GetStatements()[0]
	if reject.GetActions().GetRouteAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT ||
		reject.GetConditions().GetCommunitySet().GetType() != gobgpapi.MatchSet_TYPE_ANY {
		t.Fatalf("reject statement = %#v, want forbidden community reject", reject)
	}
	allow := req.GetPolicies()[0].GetStatements()[1]
	if allow.GetActions().GetRouteAction() != gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT ||
		allow.GetConditions().GetNeighborSet().GetName() != scopeName+"-neighbors" ||
		allow.GetConditions().GetPrefixSet().GetName() == "" ||
		allow.GetConditions().GetCommunitySet().GetType() != gobgpapi.MatchSet_TYPE_ALL {
		t.Fatalf("allow statement = %#v, want prefix and required-community accept", allow)
	}
}

func TestAppliedPoliciesPreservePrefixlessPeerImportPreference(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address: "10.99.0.2",
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			NextHopRewrite:  "peer-address",
			LocalPreference: 200,
		},
	}
	req, _ := appliedPolicies(bgpdaemon.AppliedConfig{
		Global: bgpdaemon.AppliedGlobal{ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes: []string{"192.0.2.0/24"},
			LocalPreference: 100,
		}},
		Peers: map[string]bgpdaemon.AppliedPeer{"10.99.0.2": peer},
	})
	if len(req.GetPolicies()) != 1 || len(req.GetPolicies()[0].GetStatements()) != 2 {
		t.Fatalf("restore policies = %#v, want prefixless peer policy with allow and terminal reject", req)
	}
	allow := req.GetPolicies()[0].GetStatements()[0]
	if allow.GetConditions().GetPrefixSet() != nil ||
		allow.GetActions().GetLocalPref().GetValue() != 200 ||
		!allow.GetActions().GetNexthop().GetPeerAddress() {
		t.Fatalf("prefixless peer rule = %#v, want direct peer preference 200", allow)
	}
}

func TestAppliedPoliciesRestoreRejectsRoutesFromEmptyOwnershipDirectPeer(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address:                "10.99.0.3",
		PreserveImportPrefixes: true,
		RejectImportAll:        true,
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			NextHopRewrite:  "peer-address",
			LocalPreference: 200,
		},
	}
	req, assignment := appliedPolicies(bgpdaemon.AppliedConfig{
		Global: bgpdaemon.AppliedGlobal{ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes: []string{"10.77.60.0/24"},
		}},
		Peers: map[string]bgpdaemon.AppliedPeer{peer.Address: peer},
	})
	if len(req.GetPolicies()) != 1 || len(req.GetDefinedSets()) != 1 || len(req.GetPolicies()[0].GetStatements()) != 1 {
		t.Fatalf("restored empty-ownership direct policy = %#v, want one neighbor-scoped reject statement", req)
	}
	statement := req.GetPolicies()[0].GetStatements()[0]
	if statement.GetName() != "routerd-restore-import-effective-peer-10-99-0-3-reject-all-import" ||
		statement.GetActions().GetRouteAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT ||
		statement.GetConditions().GetNeighborSet().GetName() != "routerd-restore-import-effective-peer-10-99-0-3-neighbors" {
		t.Fatalf("restored empty-ownership direct statement = %#v", statement)
	}
	if assignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_ACCEPT {
		t.Fatalf("global assignment = %#v, want default accept for local paths", assignment)
	}
}

func TestAppliedPoliciesRestorePeerExportPolicy(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address: "10.252.0.18",
		ASN:     64512,
		ImportPolicy: bgpdaemon.AppliedImportPolicy{
			AllowedPrefixes: []string{"10.252.0.0/24"},
		},
		ExportPolicyName: "routerd-lan-export-10-252-0-2",
		ExportPolicy: bgpdaemon.AppliedExportPolicy{
			AllowedPrefixes: []string{"192.168.123.129/32"},
		},
	}
	req, assignment := appliedPolicies(bgpdaemon.AppliedConfig{
		Peers: map[string]bgpdaemon.AppliedPeer{
			"10.252.0.18": peer,
		},
		Paths: []bgpdaemon.AppliedPath{{
			Source: "MobilityPool/svnet1/node/pve-rt08",
			Prefix: "192.168.123.132/32",
		}},
	})
	if !appliedPolicyRequestHasStatement(req, "routerd-restore-import-effective", "routerd-restore-import-effective-peer-10-252-0-18-allow-import") {
		t.Fatalf("restore policies = %#v, want import policy", req)
	}
	if !appliedPolicyRequestHasStatement(req, "routerd-lan-export-10-252-0-2", "routerd-lan-export-10-252-0-2-allow-export") {
		t.Fatalf("restore policies = %#v, want peer export policy", req)
	}
	if len(assignment.GetPolicies()) != 1 || assignment.GetPolicies()[0].GetName() != "routerd-restore-import-effective" {
		t.Fatalf("global import assignment = %#v, want effective import policy", assignment)
	}
	if !appliedPolicyRequestHasPrefix(req, "routerd-restore-import-effective-peer-10-252-0-18-prefixes", "192.168.123.132/32") {
		t.Fatalf("restore policies = %#v, want dynamic mobility prefix in import policy", req)
	}
	if !appliedPolicyRequestHasPrefix(req, "routerd-lan-export-10-252-0-2-prefixes", "192.168.123.132/32") {
		t.Fatalf("restore policies = %#v, want dynamic mobility prefix in export policy", req)
	}
	restoredPeer := appliedPeer(peer, bgpdaemon.AppliedGlobal{ASN: 64512})
	if restoredPeer.GetApplyPolicy().GetImportPolicy() != nil {
		t.Fatalf("restored peer import policy = %#v, want no ineffective import assignment", restoredPeer.GetApplyPolicy().GetImportPolicy())
	}
	exportAssignment := restoredPeer.GetApplyPolicy().GetExportPolicy()
	if exportAssignment.GetDirection() != gobgpapi.PolicyDirection_POLICY_DIRECTION_EXPORT ||
		exportAssignment.GetDefaultAction() != gobgpapi.RouteAction_ROUTE_ACTION_REJECT ||
		len(exportAssignment.GetPolicies()) != 1 ||
		exportAssignment.GetPolicies()[0].GetName() != "routerd-lan-export-10-252-0-2" {
		t.Fatalf("restored peer export policy = %#v, want export assignment", exportAssignment)
	}
}

func appliedPolicyRequestHasPrefix(req *gobgpapi.SetPoliciesRequest, setName, prefix string) bool {
	for _, set := range req.GetDefinedSets() {
		if set.GetName() != setName {
			continue
		}
		for _, got := range set.GetPrefixes() {
			if got.GetIpPrefix() == prefix {
				return true
			}
		}
	}
	return false
}

func appliedPolicyRequestHasDefinedSet(req *gobgpapi.SetPoliciesRequest, typ gobgpapi.DefinedType, setName, value string) bool {
	for _, set := range req.GetDefinedSets() {
		if set.GetDefinedType() != typ || set.GetName() != setName {
			continue
		}
		for _, got := range set.GetList() {
			if got == value {
				return true
			}
		}
	}
	return false
}

func TestAppliedPoliciesRestoreMultipleImportPoliciesWithUniqueStatements(t *testing.T) {
	req, _ := appliedPolicies(bgpdaemon.AppliedConfig{
		Peers: map[string]bgpdaemon.AppliedPeer{
			"192.168.1.38": {
				Address: "192.168.1.38",
				ImportPolicy: bgpdaemon.AppliedImportPolicy{
					AllowedPrefixes: []string{"10.250.0.0/24"},
				},
			},
			"192.168.1.53": {
				Address: "192.168.1.53",
				ImportPolicy: bgpdaemon.AppliedImportPolicy{
					AllowedPrefixes: []string{"10.250.0.0/24"},
				},
			},
		},
	})
	assertAppliedPolicyStatementNamesUnique(t, req)
	if !appliedPolicyRequestHasStatement(req, "routerd-restore-import-effective", "routerd-restore-import-effective-peer-192-168-1-38-allow-import") {
		t.Fatalf("restore policies = %#v, want statement scoped to 192.168.1.38", req)
	}
	if !appliedPolicyRequestHasStatement(req, "routerd-restore-import-effective", "routerd-restore-import-effective-peer-192-168-1-53-allow-import") {
		t.Fatalf("restore policies = %#v, want statement scoped to 192.168.1.53", req)
	}
}

func TestAppliedPeerEbgpMultihop(t *testing.T) {
	direct := appliedPeer(bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64513}, bgpdaemon.AppliedGlobal{ASN: 64512})
	if direct.GetEbgpMultihop() != nil {
		t.Fatalf("direct peer eBGP multihop = %#v, want nil", direct.GetEbgpMultihop())
	}
	multihop := appliedPeer(bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64513, EbgpMultihop: 16}, bgpdaemon.AppliedGlobal{ASN: 64512})
	if got := multihop.GetEbgpMultihop(); !got.GetEnabled() || got.GetMultihopTtl() != 16 {
		t.Fatalf("restored eBGP multihop = %#v, want enabled ttl=16", got)
	}
}

func TestAppliedPolicyPrefixesAllowMoreSpecifics(t *testing.T) {
	prefixes := appliedPolicyPrefixes(bgpdaemon.AppliedImportPolicy{AllowedPrefixes: []string{"10.77.60.0/24", "2001:db8:77::/64"}})
	if !appliedPrefixSetAllows(prefixes, "10.77.60.0/24") || !appliedPrefixSetAllows(prefixes, "10.77.60.11/32") {
		t.Fatalf("applied prefixes = %#v, want IPv4 prefix and more-specific accepted", prefixes)
	}
	if appliedPrefixSetAllows(prefixes, "10.77.0.0/16") || appliedPrefixSetAllows(prefixes, "10.88.0.1/32") {
		t.Fatalf("applied prefixes = %#v, want less-specific and unrelated IPv4 rejected", prefixes)
	}
	if !appliedPrefixSetAllows(prefixes, "2001:db8:77::/64") || !appliedPrefixSetAllows(prefixes, "2001:db8:77::11/128") {
		t.Fatalf("applied prefixes = %#v, want IPv6 prefix and /128 accepted", prefixes)
	}
	if appliedPrefixSetAllows(prefixes, "2001:db8:88::1/128") {
		t.Fatalf("applied prefixes = %#v, want unrelated IPv6 rejected", prefixes)
	}
}

func appliedPrefixSetAllows(prefixes []*gobgpapi.Prefix, candidate string) bool {
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

func appliedPolicyRequestHasStatement(req *gobgpapi.SetPoliciesRequest, policyName, statementName string) bool {
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

func assertAppliedPolicyStatementNamesUnique(t *testing.T, req *gobgpapi.SetPoliciesRequest) {
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

func TestAppliedPeerRestoresInternalRouteReflectorClient(t *testing.T) {
	peer := appliedPeer(bgpdaemon.AppliedPeer{
		Address:                 "10.99.0.2",
		ASN:                     64577,
		RouteReflectorClient:    true,
		RouteReflectorClusterID: "10.99.0.1",
	}, bgpdaemon.AppliedGlobal{ASN: 64577})
	if peer.GetConf().GetType() != gobgpapi.PeerType_PEER_TYPE_INTERNAL {
		t.Fatalf("peer type = %v, want internal", peer.GetConf().GetType())
	}
	rr := peer.GetRouteReflector()
	if !rr.GetRouteReflectorClient() || rr.GetRouteReflectorClusterId() != "10.99.0.1" {
		t.Fatalf("route reflector = %#v, want client cluster 10.99.0.1", rr)
	}
}
