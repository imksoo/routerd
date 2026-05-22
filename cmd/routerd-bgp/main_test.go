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
	req := appliedPolicies(bgpdaemon.AppliedConfig{
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
	restoredPeer := appliedPeer(peer, bgpdaemon.AppliedImportPolicy{})
	assignment := restoredPeer.GetApplyPolicy().GetImportPolicy()
	if assignment.GetDefaultAction() != gobgpapi.RouteAction_REJECT || len(assignment.GetPolicies()) != 1 || assignment.GetPolicies()[0].GetName() != "routerd-lan-import" {
		t.Fatalf("restored peer import policy = %#v, want default reject and peer policy assignment", assignment)
	}
}
