// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	gobgpapi "github.com/osrg/gobgp/v3/api"

	"routerd/pkg/bgpdaemon"
)

func TestAppliedPoliciesRestorePeerImportPolicyWithoutGlobalPolicy(t *testing.T) {
	peer := bgpdaemon.AppliedPeer{
		Address:          "192.168.1.38",
		ImportPolicyName: "routerd-lan-import",
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
	if len(req.GetPolicies()) != 1 || len(req.GetDefinedSets()) != 1 {
		t.Fatalf("restore policies = policies:%d definedSets:%d, want one peer policy and one prefix set", len(req.GetPolicies()), len(req.GetDefinedSets()))
	}
	policy := req.GetPolicies()[0]
	if policy.GetName() != "routerd-lan-import" {
		t.Fatalf("policy name = %q, want peer import policy name", policy.GetName())
	}
	action := policy.GetStatements()[0].GetActions().GetNexthop()
	if !action.GetPeerAddress() {
		t.Fatalf("next-hop action = %#v, want peer-address rewrite", action)
	}
	if assignment.GetName() != "global" || assignment.GetDirection() != gobgpapi.PolicyDirection_IMPORT ||
		assignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT || len(assignment.GetPolicies()) != 1 ||
		assignment.GetPolicies()[0].GetName() != "routerd-lan-import" {
		t.Fatalf("global import policy assignment = %#v, want restored policy assigned to global import", assignment)
	}
	restoredPeer := appliedPeer(peer, bgpdaemon.AppliedImportPolicy{})
	if applyPolicy := restoredPeer.GetApplyPolicy(); applyPolicy != nil && applyPolicy.GetImportPolicy() != nil {
		t.Fatalf("restored peer import policy = %#v, want no per-neighbor import policy for normal eBGP", applyPolicy.GetImportPolicy())
	}
}

func TestAppliedPeerEbgpMultihop(t *testing.T) {
	direct := appliedPeer(bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64513}, bgpdaemon.AppliedImportPolicy{})
	if direct.GetEbgpMultihop() != nil {
		t.Fatalf("direct peer eBGP multihop = %#v, want nil", direct.GetEbgpMultihop())
	}
	multihop := appliedPeer(bgpdaemon.AppliedPeer{Address: "192.0.2.2", ASN: 64513, EbgpMultihop: 16}, bgpdaemon.AppliedImportPolicy{})
	if got := multihop.GetEbgpMultihop(); !got.GetEnabled() || got.GetMultihopTtl() != 16 {
		t.Fatalf("restored eBGP multihop = %#v, want enabled ttl=16", got)
	}
}
