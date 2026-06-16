// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"testing"
)

// reqRouteSpec builds a route-table capture request: a captured /32 steered to
// the router's next-hop private IP (10.88.60.1, which the canned private-ip list
// resolves to ocid1.privateip.oc1..primary) via a VCN route table.
func reqRouteSpec(action, mode string) executeActionRequestSpec {
	s := reqSpec(action, mode)
	s.Target["routeTableRef"] = "ocid1.routetable.oc1..rt1"
	s.Target["captureStrategy"] = captureStrategyRouteTable
	s.Target["nextHopIPAddress"] = "10.88.60.1"
	return s
}

// updateRouteRulesArg finds the `route-table update --route-rules <json>` call
// and decodes the rule set written back, or reports that no update was issued.
func updateRouteRulesArg(t *testing.T, calls [][]string) ([]ociRouteRule, bool) {
	t.Helper()
	for _, c := range calls {
		isRT, isUpdate := false, false
		for _, tk := range leadingTokens(c) {
			if tk == "route-table" {
				isRT = true
			}
			if tk == "update" {
				isUpdate = true
			}
		}
		if !isRT || !isUpdate {
			continue
		}
		for i, a := range c {
			if a == "--route-rules" && i+1 < len(c) {
				var rules []ociRouteRule
				if err := json.Unmarshal([]byte(c[i+1]), &rules); err != nil {
					t.Fatalf("parse --route-rules payload %q: %v", c[i+1], err)
				}
				return rules, true
			}
		}
	}
	return nil, false
}

func hasRuleDest(rules []ociRouteRule, dest string) bool {
	for _, r := range rules {
		if r.Destination == dest {
			return true
		}
	}
	return false
}

func hasRule(rules []ociRouteRule, dest, entity string) bool {
	for _, r := range rules {
		if r.Destination == dest && r.NetworkEntityID == entity {
			return true
		}
	}
	return false
}

func TestRouteTableAssignExecuteAppendsRule(t *testing.T) {
	f := &fakeOCI{}
	res := dispatchWith(reqRouteSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	rules, ok := updateRouteRulesArg(t, f.calls)
	if !ok {
		t.Fatalf("execute assign must issue a route-table update; calls=%v", f.calls)
	}
	if !hasRule(rules, "10.88.60.9/32", "ocid1.privateip.oc1..primary") {
		t.Fatalf("mobility rule (dest -> our next-hop OCID) missing: %+v", rules)
	}
	if !hasRuleDest(rules, "0.0.0.0/0") {
		t.Fatalf("read-modify-write must preserve the unrelated default gateway rule: %+v", rules)
	}
}

func TestRouteTableViaSecondaryIPAction(t *testing.T) {
	f := &fakeOCI{}
	// The planner emits the abstract action assign-secondary-ip + captureStrategy
	// route-table; the executor must dispatch it to the route-table path.
	res := dispatchWith(reqRouteSpec(actionAssignSecondaryIP, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if _, ok := updateRouteRulesArg(t, f.calls); !ok {
		t.Fatalf("assign-secondary-ip with route-table strategy must update the route table; calls=%v", f.calls)
	}
}

func TestRouteTableAssignDryRunReadOnly(t *testing.T) {
	f := &fakeOCI{}
	res := dispatchWith(reqRouteSpec(actionAssignRouteTableRoute, modeDryRun), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	for _, c := range f.calls {
		if !isReadOnlyVerb(c) {
			t.Fatalf("dry-run issued a non-read-only command (must NOT mutate); call=%v", c)
		}
	}
	if _, ok := updateRouteRulesArg(t, f.calls); ok {
		t.Fatalf("dry-run must never issue route-table update")
	}
}

func TestRouteTableAssignSeizeReplacesForeignRule(t *testing.T) {
	f := &fakeOCI{routeTableGetOut: []byte(`{"data":{"route-rules":[{"destination":"10.88.60.9/32","destination-type":"CIDR_BLOCK","network-entity-id":"ocid1.privateip.oc1..OTHER"}]}}`)}
	spec := reqRouteSpec(actionAssignRouteTableRoute, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	rules, ok := updateRouteRulesArg(t, f.calls)
	if !ok {
		t.Fatalf("seize must update the route table; calls=%v", f.calls)
	}
	if !hasRule(rules, "10.88.60.9/32", "ocid1.privateip.oc1..primary") {
		t.Fatalf("seize must repoint the rule to our next hop: %+v", rules)
	}
}

func TestRouteTableAssignNoSeizeFailsOnForeignRule(t *testing.T) {
	f := &fakeOCI{routeTableGetOut: []byte(`{"data":{"route-rules":[{"destination":"10.88.60.9/32","destination-type":"CIDR_BLOCK","network-entity-id":"ocid1.privateip.oc1..OTHER"}]}}`)}
	res := dispatchWith(reqRouteSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("assign without allowReassignment must fail on a foreign rule, got %q", res.Status.Status)
	}
	if _, ok := updateRouteRulesArg(t, f.calls); ok {
		t.Fatalf("must not clobber a foreign rule without seize")
	}
}

func TestRouteTableAssignIdempotentWhenAlreadyOurs(t *testing.T) {
	f := &fakeOCI{routeTableGetOut: []byte(`{"data":{"route-rules":[{"destination":"10.88.60.9/32","destination-type":"CIDR_BLOCK","network-entity-id":"ocid1.privateip.oc1..primary"}]}}`)}
	res := dispatchWith(reqRouteSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	if _, ok := updateRouteRulesArg(t, f.calls); ok {
		t.Fatalf("idempotent re-assign of our own route must not update")
	}
}

func TestRouteTableUnassignDeletesOurRule(t *testing.T) {
	f := &fakeOCI{routeTableGetOut: []byte(`{"data":{"route-rules":[{"destination":"0.0.0.0/0","destination-type":"CIDR_BLOCK","network-entity-id":"ocid1.internetgateway.oc1..igw"},{"destination":"10.88.60.9/32","destination-type":"CIDR_BLOCK","network-entity-id":"ocid1.privateip.oc1..primary"}]}}`)}
	res := dispatchWith(reqRouteSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("want succeeded, got %q err=%q", res.Status.Status, res.Status.Error)
	}
	rules, ok := updateRouteRulesArg(t, f.calls)
	if !ok {
		t.Fatalf("unassign must update the route table; calls=%v", f.calls)
	}
	if hasRuleDest(rules, "10.88.60.9/32") {
		t.Fatalf("our /32 rule must be removed: %+v", rules)
	}
	if !hasRuleDest(rules, "0.0.0.0/0") {
		t.Fatalf("unassign must preserve the unrelated default gateway rule: %+v", rules)
	}
}

func TestRouteTableUnassignSkipsForeignHolder(t *testing.T) {
	f := &fakeOCI{routeTableGetOut: []byte(`{"data":{"route-rules":[{"destination":"10.88.60.9/32","destination-type":"CIDR_BLOCK","network-entity-id":"ocid1.privateip.oc1..OTHER"}]}}`)}
	res := dispatchWith(reqRouteSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("unassign of a foreign-held route must be skipped, got %q", res.Status.Status)
	}
	if _, ok := updateRouteRulesArg(t, f.calls); ok {
		t.Fatalf("must not update when the route belongs to another holder")
	}
}

func TestRouteTableUnassignMissingSkips(t *testing.T) {
	f := &fakeOCI{}
	res := dispatchWith(reqRouteSpec(actionUnassignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSkipped {
		t.Fatalf("unassign of a missing route must be skipped (idempotent), got %q", res.Status.Status)
	}
	if _, ok := updateRouteRulesArg(t, f.calls); ok {
		t.Fatalf("must not update when there is nothing to remove")
	}
}

func TestRouteTableRequiresNextHop(t *testing.T) {
	f := &fakeOCI{}
	spec := reqRouteSpec(actionAssignRouteTableRoute, modeExecute)
	delete(spec.Target, "nextHopIPAddress")
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusFailed {
		t.Fatalf("route-table assign must fail without nextHopIPAddress, got %q", res.Status.Status)
	}
}
