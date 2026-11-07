// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"context"
	"strings"
	"testing"
)

func TestRouteWithdrawalAndSeizurePreserveSameNameDifferentPrefix(t *testing.T) {
	for _, action := range []string{actionUnassignRouteTableRoute, actionAssignRouteTableRoute} {
		f := &routeFakeAz{showOut: cannedRouteShow("10.88.60.0/24", "10.88.60.254")}
		spec := routeReqSpec(action, modeExecute)
		spec.Parameters = map[string]string{"allowReassignment": "true"}
		res := dispatchWith(spec, f.run)
		if len(f.calls) != 1 || !strings.Contains(strings.Join(f.calls[0], " "), " route show ") {
			t.Fatalf("foreign prefix mutated: action=%s result=%+v calls=%v", action, res.Status, f.calls)
		}
	}
}

func TestForwardingWriteFailureKeepsPriorFact(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Join(leadingTokens(args), " ") == "network nic show" {
			return cannedNICShow(false), nil
		}
		return nil, context.DeadlineExceeded
	}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeExecute), runner)
	if res.Status.Status != statusOutcomeUnknown || res.Status.Observed["priorIpForwarding"] != "false" {
		t.Fatalf("prior forwarding fact lost: %+v", res.Status)
	}
}

func TestMissingNICObservationCannotAuthorizeForwardingWrite(t *testing.T) {
	writes := 0
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		if strings.Join(leadingTokens(args), " ") == "network nic show" {
			return []byte(`{}`), nil
		}
		writes++
		return nil, nil
	}
	res := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeExecute), runner)
	if res.Status.Status != statusFailed || writes != 0 {
		t.Fatalf("unknown NIC became a forwarding prior: %+v writes=%d", res.Status, writes)
	}
}

func TestUnknownListObservationNeverProvesDisplacedRelease(t *testing.T) {
	withNoRetrySleep(t)
	f := newSeizeFakeAz()
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		if !f.oldHolds && strings.Join(leadingTokens(args), " ") == "network nic ip-config list" {
			return []byte(`null`), nil
		}
		return f.run(ctx, args...)
	}
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, runner)
	if res.Status.Status != statusOutcomeUnknown || res.Status.Observed["displacedIPConfigDeleted"] != "true" {
		t.Fatalf("null release observation acknowledged: %+v", res.Status)
	}
}

func TestListObservationDistinguishesUnknownFromEmpty(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `[{"name":"old","ipConfigurations":null}]`} {
		runner := func(context.Context, ...string) ([]byte, error) { return []byte(body), nil }
		_, nicErr := listNICs(t.Context(), runner, "rg1", "s1")
		if (nicErr != nil) != (body != `[]`) {
			t.Fatalf("NIC list %s: %v", body, nicErr)
		}
		if body == `null` || body == `[]` {
			_, cfgErr := listIPConfigs(t.Context(), runner, "rg1", "nic1", "s1")
			if (cfgErr != nil) != (body == `null`) {
				t.Fatalf("IP config list %s: %v", body, cfgErr)
			}
		}
	}
}

func TestIPConfigTargetCannotReadOneNICAndWriteAnother(t *testing.T) {
	for _, field := range []string{"nicName", "resourceGroup", "subscriptionId"} {
		spec := reqSpec(actionAssignSecondaryIP, modeExecute)
		spec.Parameters = map[string]string{"allowReassignment": "true"}
		spec.Target[field] = "different"
		calls := 0
		runner := func(context.Context, ...string) ([]byte, error) { calls++; return nil, nil }
		res := dispatchWith(spec, runner)
		if res.Status.Status != statusFailed || calls != 0 {
			t.Fatalf("contradictory %s reached provider: %+v calls=%d", field, res.Status, calls)
		}
	}
}

func TestNamedNICOperationsUseSubscriptionFromResourceID(t *testing.T) {
	f := newSeizeFakeAz()
	spec := reqSpec(actionAssignSecondaryIP, modeExecute)
	delete(spec.Target, "subscriptionId")
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	res := dispatchWith(spec, f.run)
	if res.Status.Status != statusSucceeded {
		t.Fatalf("seize: %+v", res.Status)
	}
	for _, call := range f.calls {
		text := strings.Join(call, " ")
		if strings.Contains(text, "--resource-group") && !strings.Contains(text, "--subscription s1") {
			t.Fatalf("named operation escaped NIC subscription: %v", call)
		}
	}
}
