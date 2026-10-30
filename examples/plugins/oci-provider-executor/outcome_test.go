// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"context"
	"strings"
	"testing"
)

func TestRouteWithdrawalPreservesBroaderForeignPrefix(t *testing.T) {
	f := &fakeOCI{routeTableGetOut: []byte(`{"etag":"etag-1","data":{"id":"ocid1.routetable.oc1..rt1","route-rules":[{"destination":"10.88.60.0/24","destination-type":"CIDR_BLOCK","network-entity-id":"ocid1.privateip.oc1..primary"}]}}`)}
	spec := reqRouteSpec(actionUnassignRouteTableRoute, modeExecute)
	spec.Target["address"] = "10.88.60.0/32"
	res := dispatchWith(spec, f.run)
	if _, changed := updateRouteRulesArg(t, f.calls); changed {
		t.Fatalf("broader /24 deleted for requested /32: %+v", res.Status)
	}
}

func TestRouteTableCASPreservesUnrelatedRuleFields(t *testing.T) {
	f := &fakeOCI{routeTableGetOut: []byte(`{"etag":"observed-etag","data":{"id":"ocid1.routetable.oc1..rt1","route-rules":[{"destination":"all-services","destination-type":"SERVICE_CIDR_BLOCK","network-entity-id":"service-gateway","route-type":"STATIC","description":"foreign service route"}]}}`)}
	res := dispatchWith(reqRouteSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("assign: %+v", res.Status)
	}
	rules, ok := updateRouteRulesArg(t, f.calls)
	if !ok || len(rules) != 2 || rules[0].DestinationType != "SERVICE_CIDR_BLOCK" || rules[0].RouteType != "STATIC" || rules[0].Description != "foreign service route" {
		t.Fatalf("full-set update lost unrelated fields: %+v", rules)
	}
	for _, call := range f.calls {
		if strings.Join(leadingTokens(call), " ") == "network route-table update" {
			if !strings.Contains(strings.Join(call, " "), "--if-match observed-etag") {
				t.Fatalf("write not bound to GET: %v", call)
			}
			return
		}
	}
	t.Fatal("missing full-set update")
}

func TestRouteTableMissingETagOrRulesCannotWrite(t *testing.T) {
	for _, body := range []string{`{"data":{"id":"ocid1.routetable.oc1..rt1","route-rules":[]}}`, `{"etag":"etag","data":{"id":"ocid1.routetable.oc1..rt1"}}`} {
		f := &fakeOCI{routeTableGetOut: []byte(body)}
		res := dispatchWith(reqRouteSpec(actionAssignRouteTableRoute, modeExecute), f.run)
		if _, changed := updateRouteRulesArg(t, f.calls); changed || res.Status.Status != statusFailed {
			t.Fatalf("incomplete GET authorized update: %+v calls=%v", res.Status, f.calls)
		}
	}
}

func TestRouteTableWriteConflictIsNotRetried(t *testing.T) {
	f := &fakeOCI{}
	writes := 0
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Join(leadingTokens(args), " ") == "network route-table update" {
			writes++
			return nil, context.DeadlineExceeded
		}
		return f.run(ctx, args...)
	}
	res := dispatchWith(reqRouteSpec(actionAssignRouteTableRoute, modeExecute), runner)
	if writes != 1 || res.Status.Status != statusOutcomeUnknown || res.Status.Observed["routeTableETag"] != "fixture-etag" {
		t.Fatalf("uncertain CAS repeated/lost facts: writes=%d result=%+v", writes, res.Status)
	}
}

func TestForwardingWriteFailureKeepsPriorFact(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Join(leadingTokens(args), " ") == "network vnic get" {
			return cannedVNICGet(false), nil
		}
		return nil, context.DeadlineExceeded
	}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeExecute), runner)
	if res.Status.Status != statusOutcomeUnknown || res.Status.Observed["priorSkipSourceDestCheck"] != "false" {
		t.Fatalf("prior forwarding fact lost: %+v", res.Status)
	}
}

func TestMissingNextHopOCIDCannotAuthorizeRouteTableWrite(t *testing.T) {
	f := &fakeOCI{listOut: []byte(`{"data":[{"ip-address":"10.88.60.1","id":""}]}`)}
	res := dispatchWith(reqRouteSpec(actionAssignRouteTableRoute, modeExecute), f.run)
	if _, changed := updateRouteRulesArg(t, f.calls); res.Status.Status != statusFailed || changed {
		t.Fatalf("unknown next-hop ID became a route: %+v calls=%v", res.Status, f.calls)
	}
}
