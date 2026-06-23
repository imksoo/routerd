// SPDX-License-Identifier: BSD-3-Clause

package captureprovider

import (
	"testing"
	"time"
)

func TestActionPlanFacadeClaimAddressPlansProviderAction(t *testing.T) {
	plan, err := NewActionPlanFacade().ClaimAddress(AddressClaimRequest{
		Provider:          "aws",
		ProviderRef:       "aws-provider",
		Pool:              "cloudedge",
		Address:           "10.88.60.10/32",
		NICRef:            "eni-a",
		Strategy:          StrategySecondaryIP,
		Target:            map[string]string{"region": "ap-northeast-1"},
		AllowReassignment: true,
	})
	if err != nil {
		t.Fatalf("ClaimAddress: %v", err)
	}
	if plan.Action != ActionAssignSecondaryIP || plan.Mode != "dry-run" {
		t.Fatalf("plan action/mode = %q/%q, want provider-action dry-run assign", plan.Action, plan.Mode)
	}
	if plan.IdempotencyKey != "mobility:cloudedge:aws:eni-a:assign-secondary-ip:10.88.60.10/32" {
		t.Fatalf("idempotency key = %q", plan.IdempotencyKey)
	}
	if plan.Target["address"] != "10.88.60.10/32" || plan.Target["nicRef"] != "eni-a" || plan.Target["region"] != "ap-northeast-1" {
		t.Fatalf("target = %#v", plan.Target)
	}
	if plan.Parameters["allowReassignment"] != "true" {
		t.Fatalf("parameters = %#v, want allowReassignment", plan.Parameters)
	}
	if plan.Undo == nil || plan.Undo.Action != ActionUnassignSecondaryIP || plan.Undo.Parameters["address"] != "10.88.60.10/32" {
		t.Fatalf("undo = %#v", plan.Undo)
	}
}

func TestActionPlanFacadeRouteClearDoesNotRequireNextHop(t *testing.T) {
	plan, err := NewActionPlanFacade().ClearNextHop(RouteClearRequest{
		Provider:      "azure",
		ProviderRef:   "azure-provider",
		Pool:          "cloudedge",
		Address:       "10.88.60.11/32",
		NICRef:        "nic-a",
		RouteTableRef: "rt-cloudedge",
		StaleSince:    time.Date(2026, 6, 23, 1, 2, 3, 4, time.UTC),
	})
	if err != nil {
		t.Fatalf("ClearNextHop: %v", err)
	}
	if plan.Action != ActionUnassignSecondaryIP {
		t.Fatalf("action = %q, want %q", plan.Action, ActionUnassignSecondaryIP)
	}
	if plan.Target["captureStrategy"] != StrategyRouteTable || plan.Target["routeTableRef"] != "rt-cloudedge" {
		t.Fatalf("target = %#v", plan.Target)
	}
	if plan.Parameters["deprovisionSince"] != "2026-06-23T01:02:03.000000004Z" {
		t.Fatalf("parameters = %#v", plan.Parameters)
	}
}

func TestActionPlanFacadeRouteClaimRequiresProviderNextHopWhenNeeded(t *testing.T) {
	_, err := NewActionPlanFacade().SetNextHop(RouteSteeringRequest{
		Provider:      "azure",
		ProviderRef:   "azure-provider",
		Pool:          "cloudedge",
		Address:       "10.88.60.11/32",
		NICRef:        "nic-a",
		RouteTableRef: "rt-cloudedge",
	})
	if err == nil {
		t.Fatalf("SetNextHop succeeded, want nextHopIPAddress validation error")
	}
}

func TestCaptureOutcomeString(t *testing.T) {
	if RateLimited.String() != "RateLimited" || CaptureOutcome(99).String() != "CaptureOutcome(99)" {
		t.Fatalf("unexpected outcome strings")
	}
}
