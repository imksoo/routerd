// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAmbiguousRouteReplaceNeverFallsBackToCreate(t *testing.T) {
	writes := []string{}
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[1] == "describe-route-tables" {
			return cannedDescribeRouteTable("rtb-cloudedge", "10.88.60.9/32", "eni-old"), nil
		}
		writes = append(writes, args[1])
		return nil, context.DeadlineExceeded
	}
	spec := routeReqSpec(actionAssignRouteTableRoute, modeExecute)
	spec.Parameters = map[string]string{"allowReassignment": "true"}
	result := dispatchWith(spec, runner)
	if result.Status.Status != statusOutcomeUnknown || strings.Join(writes, ",") != "replace-route" || result.Status.Observed["priorNextHopNICRef"] != "eni-old" || result.Status.Observed["assignedRoute"] != "" {
		t.Fatalf("unconfirmed replacement replayed/acknowledged: %+v writes=%v", result.Status, writes)
	}
}

func TestForwardingWriteFailureKeepsPriorFact(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[1] == "describe-network-interfaces" {
			return cannedDescribe(true, "10.0.0.2"), nil
		}
		return nil, context.DeadlineExceeded
	}
	result := dispatchWith(reqSpec(actionEnsureFwdEnabled, modeExecute), runner)
	if result.Status.Status != statusOutcomeUnknown || result.Status.Observed["priorSourceDestCheck"] != "true" {
		t.Fatalf("prior forwarding fact lost: %+v", result.Status)
	}
}

func TestPrelaunchFailureIsDefinitiveWithoutProviderReplay(t *testing.T) {
	result := mutationFailure(newResult(), "launch", &commandNotStartedError{errors.New("executable unavailable")})
	if result.Status.Status != statusFailed {
		t.Fatalf("no process ran: %+v", result.Status)
	}
}
