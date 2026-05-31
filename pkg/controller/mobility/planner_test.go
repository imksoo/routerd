// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPlanDynamicConfigCloudSelfGeneratesClaimAndActionPlans(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: plannedPoolSpec(),
		SelfNode: "azure-router",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{{
			Pool:          "cloudedge",
			Address:       "10.88.60.9/32",
			Status:        routerstate.AddressLeaseStatusActive,
			OwnerNode:     "onprem-router",
			OwnerSite:     "onprem",
			OwnerRole:     "onprem",
			Epoch:         3,
			SourceEventID: "evt-9",
			ExpiresAt:     now.Add(2 * time.Minute),
		}},
		ProviderProfiles: map[string]api.CloudProviderProfileSpec{
			"azure-provider": {
				Provider:       "azure",
				SubscriptionID: "sub-1",
				ResourceGroup:  "rg-router",
				Capabilities:   []string{"nic-secondary-ip", "ip-forwarding"},
				Auth:           api.ProviderAuth{Mode: "external-command", Command: "az"},
			},
		},
	})
	if err != nil {
		t.Fatalf("PlanDynamicConfig: %v", err)
	}
	if out.Part.Spec.Source != "MobilityPool/cloudedge/node/azure-router" || out.Part.Spec.Generation != 1 {
		t.Fatalf("unexpected part identity: source=%q generation=%d", out.Part.Spec.Source, out.Part.Spec.Generation)
	}
	if len(out.Part.Spec.Resources) != 2 {
		t.Fatalf("resources = %d, want domain + claim", len(out.Part.Spec.Resources))
	}
	claim := out.Part.Spec.Resources[1]
	if claim.Kind != "RemoteAddressClaim" {
		t.Fatalf("resource[1].kind = %s", claim.Kind)
	}
	spec, err := claim.RemoteAddressClaimSpec()
	if err != nil {
		t.Fatalf("RemoteAddressClaimSpec: %v", err)
	}
	if spec.DomainRef != "mobility-cloudedge" || spec.Address != "10.88.60.9/32" || spec.OwnerSide != "onprem" {
		t.Fatalf("unexpected claim spec: %+v", spec)
	}
	if spec.Capture.Type != "provider-secondary-ip" || spec.Capture.ProviderRef != "azure-provider" || spec.Delivery.PeerRef != "onprem" {
		t.Fatalf("unexpected claim capture/delivery: %+v", spec)
	}
	if got := claim.Metadata.Annotations["mobility.routerd.net/lease-epoch"]; got != "3" {
		t.Fatalf("lease epoch annotation = %q", got)
	}
	if len(out.ActionPlans) != 2 {
		t.Fatalf("actionPlans = %d, want 2", len(out.ActionPlans))
	}
	for _, plan := range out.ActionPlans {
		if err := routerplugin.ValidateActionPlan(plan); err != nil {
			t.Fatalf("ValidateActionPlan(%s): %v", plan.Name, err)
		}
		if plan.Mode != "dry-run" {
			t.Fatalf("action mode = %q, want dry-run", plan.Mode)
		}
	}
	assign := findActionPlan(out.ActionPlans, "assign-secondary-ip")
	if assign == nil {
		t.Fatal("missing assign-secondary-ip action plan")
	}
	if assign.Target["resourceGroup"] != "rg-router" || assign.Target["nicName"] != "router-nic" || assign.Target["ipConfigName"] == "" || assign.Target["region"] != "japaneast" {
		t.Fatalf("assign target missing azure fields: %+v", assign.Target)
	}
	forwarding := findActionPlan(out.ActionPlans, "ensure-forwarding-enabled")
	if forwarding == nil || forwarding.Parameters["ipForwarding"] != "true" {
		t.Fatalf("unexpected forwarding action: %+v", forwarding)
	}
	if forwarding.Target["address"] != "10.88.60.9/32" {
		t.Fatalf("forwarding target address = %q, want representative captured address", forwarding.Target["address"])
	}
	if forwarding.Undo == nil || forwarding.Undo.Parameters["address"] != "10.88.60.9/32" {
		t.Fatalf("forwarding undo must carry representative address, got %+v", forwarding.Undo)
	}
}

func TestPlanDynamicConfigResolvesDeliveryToBeforeFallback(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	spec := plannedPoolSpec()
	spec.Members = append(spec.Members,
		api.MobilityPoolMember{NodeRef: "aws-router", Site: "aws", Role: "cloud"},
		api.MobilityPoolMember{NodeRef: "oci-router", Site: "oci", Role: "cloud"},
	)
	spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "fallback-cloud", Mode: "route", TunnelInterface: "wg-fallback"}
	spec.Members[0].DeliveryTo = []api.MobilityMemberDeliveryTarget{
		{NodeRef: "aws-router", PeerRef: "aws-main", Mode: "route", TunnelInterface: "wg-aws"},
		{Site: "azure", PeerRef: "azure-main", Mode: "route", TunnelInterface: "wg-azure"},
		{Role: "cloud", PeerRef: "generic-cloud", Mode: "route", TunnelInterface: "wg-cloud"},
	}
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: spec,
		SelfNode: "onprem-router",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{
			{Pool: "cloudedge", Address: "10.88.60.11/32", Status: routerstate.AddressLeaseStatusActive, OwnerNode: "aws-router", OwnerSite: "aws", OwnerRole: "cloud", Epoch: 1, ExpiresAt: now.Add(time.Minute)},
			{Pool: "cloudedge", Address: "10.88.60.12/32", Status: routerstate.AddressLeaseStatusActive, OwnerNode: "azure-router", OwnerSite: "azure", OwnerRole: "cloud", Epoch: 1, ExpiresAt: now.Add(time.Minute)},
			{Pool: "cloudedge", Address: "10.88.60.13/32", Status: routerstate.AddressLeaseStatusActive, OwnerNode: "oci-router", OwnerSite: "oci", OwnerRole: "cloud", Epoch: 1, ExpiresAt: now.Add(time.Minute)},
		},
	})
	if err != nil {
		t.Fatalf("PlanDynamicConfig: %v", err)
	}
	wantPeers := map[string]string{
		"10.88.60.11/32": "aws-main",
		"10.88.60.12/32": "azure-main",
		"10.88.60.13/32": "generic-cloud",
	}
	for _, claim := range out.Claims {
		spec, err := claim.RemoteAddressClaimSpec()
		if err != nil {
			t.Fatalf("RemoteAddressClaimSpec: %v", err)
		}
		if spec.Delivery.PeerRef != wantPeers[spec.Address] {
			t.Fatalf("claim %s peerRef=%q want %q", spec.Address, spec.Delivery.PeerRef, wantPeers[spec.Address])
		}
	}
}

func TestPlanDynamicConfigCopiesCaptureActiveWhen(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	spec := plannedPoolSpec()
	spec.Members[0].Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: spec,
		SelfNode: "onprem-router",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{{
			Pool:      "cloudedge",
			Address:   "10.88.60.10/32",
			Status:    routerstate.AddressLeaseStatusActive,
			OwnerNode: "azure-router",
			OwnerSite: "azure",
			OwnerRole: "cloud",
			Epoch:     2,
			ExpiresAt: now.Add(2 * time.Minute),
		}},
	})
	if err != nil {
		t.Fatalf("PlanDynamicConfig: %v", err)
	}
	claim := firstKind(out.Part.Spec.Resources, "RemoteAddressClaim")
	if claim.Kind == "" {
		t.Fatalf("missing RemoteAddressClaim in %+v", out.Part.Spec.Resources)
	}
	claimSpec, err := claim.RemoteAddressClaimSpec()
	if err != nil {
		t.Fatalf("RemoteAddressClaimSpec: %v", err)
	}
	if claimSpec.Capture.ActiveWhen.Type != "vrrp-master" || claimSpec.Capture.ActiveWhen.VirtualAddressRef != "onprem-vip" {
		t.Fatalf("activeWhen not copied: %+v", claimSpec.Capture.ActiveWhen)
	}
}

func TestPlanDynamicConfigSkipsOwnSiteHoldingAndExpiredLeases(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: plannedPoolSpec(),
		SelfNode: "azure-router",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{
			{Pool: "cloudedge", Address: "10.88.60.9/32", Status: routerstate.AddressLeaseStatusActive, OwnerNode: "azure-router", OwnerSite: "azure", OwnerRole: "cloud", Epoch: 1, ExpiresAt: now.Add(time.Minute)},
			{Pool: "cloudedge", Address: "10.88.60.10/32", Status: routerstate.AddressLeaseStatusHolding, OwnerNode: "onprem-router", OwnerSite: "onprem", OwnerRole: "onprem", Epoch: 1, ExpiresAt: now.Add(time.Minute)},
			{Pool: "cloudedge", Address: "10.88.60.11/32", Status: routerstate.AddressLeaseStatusActive, OwnerNode: "onprem-router", OwnerSite: "onprem", OwnerRole: "onprem", Epoch: 1, ExpiresAt: now.Add(-time.Second)},
		},
		ProviderProfiles: map[string]api.CloudProviderProfileSpec{
			"azure-provider": {Provider: "azure"},
		},
	})
	if err != nil {
		t.Fatalf("PlanDynamicConfig: %v", err)
	}
	if len(out.Claims) != 0 || len(out.ActionPlans) != 0 {
		t.Fatalf("claims/actionPlans = %d/%d, want none", len(out.Claims), len(out.ActionPlans))
	}
	if len(out.Part.Spec.Resources) != 1 || out.Part.Spec.Resources[0].Kind != "AddressMobilityDomain" {
		t.Fatalf("resources = %+v, want domain-only part", out.Part.Spec.Resources)
	}
}

func TestPlacementDecision(t *testing.T) {
	base := placementPoolSpec()
	tests := []struct {
		name        string
		mut         func(*api.MobilityPoolSpec)
		self        string
		active      bool
		activeNode  string
		noCandidate bool
	}{
		{
			name:       "primary active",
			self:       "azure-router-a",
			active:     true,
			activeNode: "azure-router-a",
		},
		{
			name:       "standby inactive",
			self:       "azure-router-b",
			active:     false,
			activeNode: "azure-router-a",
		},
		{
			name: "drain primary promotes next",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Maintenance.Drain = true
			},
			self:       "azure-router-b",
			active:     true,
			activeNode: "azure-router-b",
		},
		{
			name: "nodeRef tiebreak",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Placement.Priority = 10
				spec.Members[2].Placement.Priority = 10
			},
			self:       "azure-router-a",
			active:     true,
			activeNode: "azure-router-a",
		},
		{
			name: "all drained",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Maintenance.Drain = true
				spec.Members[2].Maintenance.Drain = true
			},
			self:        "azure-router-a",
			active:      false,
			noCandidate: true,
		},
		{
			name:       "placement unspecified unchanged",
			self:       "onprem-router",
			active:     true,
			activeNode: "onprem-router",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := base
			spec.Members = append([]api.MobilityPoolMember(nil), base.Members...)
			if tt.mut != nil {
				tt.mut(&spec)
			}
			members := plannerMembers(spec.Members)
			decision := evaluatePlacement(members[tt.self], members)
			if decision.Active != tt.active || decision.ActiveNode != tt.activeNode || decision.NoCandidate() != tt.noCandidate {
				t.Fatalf("decision = %+v, want active=%t activeNode=%q noCandidate=%t", decision, tt.active, tt.activeNode, tt.noCandidate)
			}
		})
	}
}

func TestPlanDynamicConfigPlacementActiveOnly(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	lease := routerstate.AddressLeaseRecord{
		Pool:       "cloudedge",
		Address:    "10.88.60.9/32",
		Status:     routerstate.AddressLeaseStatusActive,
		OwnerNode:  "onprem-router",
		OwnerSite:  "onprem",
		OwnerRole:  "onprem",
		Epoch:      1,
		ObservedAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}
	spec := placementPoolSpec()
	profiles := plannedProviderProfiles()
	active, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         spec,
		SelfNode:         "azure-router-a",
		Now:              now,
		Leases:           []routerstate.AddressLeaseRecord{lease},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("active PlanDynamicConfig: %v", err)
	}
	if len(active.Claims) != 1 || findActionPlan(active.ActionPlans, "assign-secondary-ip") == nil {
		t.Fatalf("active claims/actionPlans = %d/%+v, want claim + assign", len(active.Claims), active.ActionPlans)
	}

	inactive, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         spec,
		SelfNode:         "azure-router-b",
		Now:              now,
		Leases:           []routerstate.AddressLeaseRecord{lease},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("inactive PlanDynamicConfig: %v", err)
	}
	if len(inactive.Claims) != 0 || len(inactive.ActionPlans) != 0 || inactive.Placement.ActiveNode != "azure-router-a" {
		t.Fatalf("inactive output = claims %d actions %+v placement %+v, want no claim/action and active a", len(inactive.Claims), inactive.ActionPlans, inactive.Placement)
	}
}

func TestPlanDynamicConfigPlacementDrainMovesCapture(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	spec := placementPoolSpec()
	profiles := plannedProviderProfiles()
	lease := routerstate.AddressLeaseRecord{
		Pool:       "cloudedge",
		Address:    "10.88.60.9/32",
		Status:     routerstate.AddressLeaseStatusActive,
		OwnerNode:  "onprem-router",
		OwnerSite:  "onprem",
		OwnerRole:  "onprem",
		Epoch:      1,
		ObservedAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}
	primary, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         spec,
		SelfNode:         "azure-router-a",
		Now:              now,
		Leases:           []routerstate.AddressLeaseRecord{lease},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("primary PlanDynamicConfig: %v", err)
	}
	if len(primary.Claims) != 1 {
		t.Fatalf("primary claims = %d, want one", len(primary.Claims))
	}

	drained := spec
	drained.Members = append([]api.MobilityPoolMember(nil), spec.Members...)
	drained.Members[1].Maintenance.Drain = true
	drainedOld, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         drained,
		SelfNode:         "azure-router-a",
		Now:              now.Add(time.Second),
		Leases:           []routerstate.AddressLeaseRecord{lease},
		PreviousClaims:   primary.Claims,
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("drained old PlanDynamicConfig: %v", err)
	}
	if len(drainedOld.Claims) != 0 || findActionPlan(drainedOld.ActionPlans, "unassign-secondary-ip") == nil {
		t.Fatalf("drained old claims/actions = %d/%+v, want no claim + unassign", len(drainedOld.Claims), drainedOld.ActionPlans)
	}
	if findActionPlan(drainedOld.ActionPlans, "ensure-forwarding-disabled") == nil {
		t.Fatalf("drained old actions = %+v, want forwarding disable NIC guard", drainedOld.ActionPlans)
	}
	drainedNew, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         drained,
		SelfNode:         "azure-router-b",
		Now:              now.Add(time.Second),
		Leases:           []routerstate.AddressLeaseRecord{lease},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("drained new PlanDynamicConfig: %v", err)
	}
	if len(drainedNew.Claims) != 1 || findActionPlan(drainedNew.ActionPlans, "assign-secondary-ip") == nil {
		t.Fatalf("drained new claims/actions = %d/%+v, want claim + assign", len(drainedNew.Claims), drainedNew.ActionPlans)
	}

	returnedOld, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         spec,
		SelfNode:         "azure-router-a",
		Now:              now.Add(2 * time.Minute),
		Leases:           []routerstate.AddressLeaseRecord{lease},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("returned old PlanDynamicConfig: %v", err)
	}
	if len(returnedOld.Claims) != 1 || findActionPlan(returnedOld.ActionPlans, "assign-secondary-ip") == nil {
		t.Fatalf("returned old claims/actions = %d/%+v, want primary claim + assign", len(returnedOld.Claims), returnedOld.ActionPlans)
	}
	returnedNew, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         spec,
		SelfNode:         "azure-router-b",
		Now:              now.Add(2 * time.Minute),
		Leases:           []routerstate.AddressLeaseRecord{lease},
		PreviousClaims:   drainedNew.Claims,
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("returned new PlanDynamicConfig: %v", err)
	}
	if len(returnedNew.Claims) != 0 || findActionPlan(returnedNew.ActionPlans, "unassign-secondary-ip") == nil {
		t.Fatalf("returned new claims/actions = %d/%+v, want standby unassign", len(returnedNew.Claims), returnedNew.ActionPlans)
	}
}

func TestPlanDynamicConfigPlacementDrainActiveLeaseBypassesDeprovisionHold(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	spec := placementPoolSpec()
	spec.LeasePolicy.HoldDuration = "30s"
	profiles := plannedProviderProfiles()
	lease := routerstate.AddressLeaseRecord{
		Pool:       "cloudedge",
		Address:    "10.88.60.9/32",
		Status:     routerstate.AddressLeaseStatusActive,
		OwnerNode:  "onprem-router",
		OwnerSite:  "onprem",
		OwnerRole:  "onprem",
		Epoch:      1,
		ObservedAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}
	primary, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         spec,
		SelfNode:         "azure-router-a",
		Now:              now,
		Leases:           []routerstate.AddressLeaseRecord{lease},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("primary PlanDynamicConfig: %v", err)
	}
	drained := spec
	drained.Members = append([]api.MobilityPoolMember(nil), spec.Members...)
	drained.Members[1].Maintenance.Drain = true
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         drained,
		SelfNode:         "azure-router-a",
		Now:              now.Add(time.Second),
		Leases:           []routerstate.AddressLeaseRecord{lease},
		PreviousClaims:   primary.Claims,
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("drained PlanDynamicConfig: %v", err)
	}
	if len(out.Claims) != 0 {
		t.Fatalf("drained claims = %d, want none", len(out.Claims))
	}
	if findActionPlan(out.ActionPlans, "unassign-secondary-ip") == nil || findActionPlan(out.ActionPlans, "ensure-forwarding-disabled") == nil {
		t.Fatalf("drained actionPlans = %+v, want immediate unassign + forwarding-disable", out.ActionPlans)
	}
}

func TestPlanDynamicConfigActivePlacementMissingLeaseDoesNotDeprovision(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	spec := placementPoolSpec()
	profiles := plannedProviderProfiles()
	initial, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: spec,
		SelfNode: "azure-router-a",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{{
			Pool:       "cloudedge",
			Address:    "10.88.60.9/32",
			Status:     routerstate.AddressLeaseStatusActive,
			OwnerNode:  "onprem-router",
			OwnerSite:  "onprem",
			OwnerRole:  "onprem",
			Epoch:      1,
			ObservedAt: now,
			ExpiresAt:  now.Add(time.Hour),
		}},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("initial PlanDynamicConfig: %v", err)
	}
	if len(initial.Claims) != 1 {
		t.Fatalf("initial claims = %d, want one", len(initial.Claims))
	}

	out, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         spec,
		SelfNode:         "azure-router-a",
		Now:              now.Add(time.Second),
		Leases:           nil,
		PreviousClaims:   initial.Claims,
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("missing-lease PlanDynamicConfig: %v", err)
	}
	if !out.Placement.Active {
		t.Fatalf("placement = %+v, want active self", out.Placement)
	}
	if len(out.Claims) != 1 {
		t.Fatalf("active placement missing lease claims = %d, want prior provider claim carried forward", len(out.Claims))
	}
	if findActionPlan(out.ActionPlans, "unassign-secondary-ip") != nil || findActionPlan(out.ActionPlans, "ensure-forwarding-disabled") != nil {
		t.Fatalf("active placement missing lease actionPlans = %+v, want no de-provision", out.ActionPlans)
	}
}

func TestPlanDynamicConfigPlacementAllDrainedNoCandidate(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	spec := placementPoolSpec()
	spec.Members[1].Maintenance.Drain = true
	spec.Members[2].Maintenance.Drain = true
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: spec,
		SelfNode: "azure-router-a",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{{
			Pool:      "cloudedge",
			Address:   "10.88.60.9/32",
			Status:    routerstate.AddressLeaseStatusActive,
			OwnerNode: "onprem-router",
			OwnerSite: "onprem",
			OwnerRole: "onprem",
			Epoch:     1,
			ExpiresAt: now.Add(time.Hour),
		}},
		ProviderProfiles: plannedProviderProfiles(),
	})
	if err != nil {
		t.Fatalf("PlanDynamicConfig: %v", err)
	}
	if !out.Placement.NoCandidate() || len(out.Claims) != 0 || len(out.ActionPlans) != 0 {
		t.Fatalf("output = placement %+v claims %d actions %+v, want no candidate with no claim/action", out.Placement, len(out.Claims), out.ActionPlans)
	}
}

func TestPlanDynamicConfigDeprovisionsStaleProviderClaimAfterHold(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	profiles := plannedProviderProfiles()
	initial, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: plannedPoolSpec(),
		SelfNode: "azure-router",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{{
			Pool:       "cloudedge",
			Address:    "10.88.60.9/32",
			Status:     routerstate.AddressLeaseStatusActive,
			OwnerNode:  "onprem-router",
			OwnerSite:  "onprem",
			OwnerRole:  "onprem",
			Epoch:      1,
			ObservedAt: now,
			ExpiresAt:  now.Add(time.Minute),
		}},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("initial PlanDynamicConfig: %v", err)
	}
	if len(initial.Claims) != 1 {
		t.Fatalf("initial claims = %d, want one", len(initial.Claims))
	}

	expired := routerstate.AddressLeaseRecord{
		Pool:       "cloudedge",
		Address:    "10.88.60.9/32",
		Status:     routerstate.AddressLeaseStatusExpired,
		OwnerNode:  "onprem-router",
		OwnerSite:  "onprem",
		OwnerRole:  "onprem",
		Epoch:      1,
		ObservedAt: now,
		ExpiresAt:  now.Add(5 * time.Second),
		UpdatedAt:  now.Add(5 * time.Second),
	}
	beforeHold, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         plannedPoolSpec(),
		SelfNode:         "azure-router",
		Now:              now.Add(20 * time.Second),
		Leases:           []routerstate.AddressLeaseRecord{expired},
		PreviousClaims:   initial.Claims,
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("before-hold PlanDynamicConfig: %v", err)
	}
	if len(beforeHold.Claims) != 0 || len(beforeHold.ActionPlans) != 0 {
		t.Fatalf("before hold claims/actionPlans = %d/%d, want none", len(beforeHold.Claims), len(beforeHold.ActionPlans))
	}

	afterHold, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         plannedPoolSpec(),
		SelfNode:         "azure-router",
		Now:              now.Add(40 * time.Second),
		Leases:           []routerstate.AddressLeaseRecord{expired},
		PreviousClaims:   initial.Claims,
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("after-hold PlanDynamicConfig: %v", err)
	}
	if len(afterHold.Claims) != 0 {
		t.Fatalf("after hold claims = %d, want none", len(afterHold.Claims))
	}
	if len(afterHold.ActionPlans) != 2 {
		t.Fatalf("after hold actionPlans = %d, want unassign + forwarding-disable: %+v", len(afterHold.ActionPlans), afterHold.ActionPlans)
	}
	for _, plan := range afterHold.ActionPlans {
		if err := routerplugin.ValidateActionPlan(plan); err != nil {
			t.Fatalf("ValidateActionPlan(%s): %v", plan.Name, err)
		}
	}
	unassign := findActionPlan(afterHold.ActionPlans, "unassign-secondary-ip")
	if unassign == nil {
		t.Fatalf("missing unassign plan: %+v", afterHold.ActionPlans)
	}
	if unassign.Target["address"] != "10.88.60.9/32" || unassign.Target["nicRef"] == "" || unassign.Parameters["deprovisionSince"] != expired.ExpiresAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected unassign plan: %+v", unassign)
	}
	if unassign.Undo == nil || unassign.Undo.Action != "assign-secondary-ip" || unassign.Undo.Parameters["address"] != "10.88.60.9/32" {
		t.Fatalf("unexpected unassign undo: %+v", unassign.Undo)
	}
	disable := findActionPlan(afterHold.ActionPlans, "ensure-forwarding-disabled")
	if disable == nil {
		t.Fatalf("missing forwarding-disable plan: %+v", afterHold.ActionPlans)
	}
	if disable.Target["address"] != "10.88.60.9/32" || disable.Parameters["priorIpForwarding"] != "false" {
		t.Fatalf("unexpected forwarding-disable plan: %+v", disable)
	}

	converged, err := PlanDynamicConfig(PlannerInput{
		PoolName:         "cloudedge",
		PoolSpec:         plannedPoolSpec(),
		SelfNode:         "azure-router",
		Now:              now.Add(time.Minute),
		Leases:           []routerstate.AddressLeaseRecord{expired},
		PreviousClaims:   nil,
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("converged PlanDynamicConfig: %v", err)
	}
	if len(converged.ActionPlans) != 0 {
		t.Fatalf("converged actionPlans = %+v, want none", converged.ActionPlans)
	}
}

func TestPlanDynamicConfigKeepsForwardingEnabledWhenNICStillHasDesiredCapture(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	profiles := plannedProviderProfiles()
	initial, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: plannedPoolSpec(),
		SelfNode: "azure-router",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{
			{Pool: "cloudedge", Address: "10.88.60.9/32", Status: routerstate.AddressLeaseStatusActive, OwnerNode: "onprem-router", OwnerSite: "onprem", OwnerRole: "onprem", Epoch: 1, ObservedAt: now, ExpiresAt: now.Add(time.Minute)},
			{Pool: "cloudedge", Address: "10.88.60.10/32", Status: routerstate.AddressLeaseStatusActive, OwnerNode: "onprem-router", OwnerSite: "onprem", OwnerRole: "onprem", Epoch: 1, ObservedAt: now, ExpiresAt: now.Add(time.Minute)},
		},
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("initial PlanDynamicConfig: %v", err)
	}
	if len(initial.Claims) != 2 {
		t.Fatalf("initial claims = %d, want two", len(initial.Claims))
	}
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: plannedPoolSpec(),
		SelfNode: "azure-router",
		Now:      now.Add(time.Minute),
		Leases: []routerstate.AddressLeaseRecord{
			{Pool: "cloudedge", Address: "10.88.60.9/32", Status: routerstate.AddressLeaseStatusExpired, OwnerNode: "onprem-router", OwnerSite: "onprem", OwnerRole: "onprem", Epoch: 1, ObservedAt: now, ExpiresAt: now.Add(10 * time.Second), UpdatedAt: now.Add(10 * time.Second)},
			{Pool: "cloudedge", Address: "10.88.60.10/32", Status: routerstate.AddressLeaseStatusActive, OwnerNode: "onprem-router", OwnerSite: "onprem", OwnerRole: "onprem", Epoch: 1, ObservedAt: now, ExpiresAt: now.Add(time.Hour)},
		},
		PreviousClaims:   initial.Claims,
		ProviderProfiles: profiles,
	})
	if err != nil {
		t.Fatalf("PlanDynamicConfig: %v", err)
	}
	if findActionPlan(out.ActionPlans, "unassign-secondary-ip") == nil {
		t.Fatalf("missing stale unassign in %+v", out.ActionPlans)
	}
	if findActionPlan(out.ActionPlans, "ensure-forwarding-disabled") != nil {
		t.Fatalf("forwarding-disable should not be generated while another capture remains desired: %+v", out.ActionPlans)
	}
}

func TestControllerPlannerUsesEventGroupNodeNameAndOverwritesGenerationOne(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	if err := store.UpsertAddressLease(routerstate.AddressLeaseRecord{
		Pool:      "cloudedge",
		Address:   "10.88.60.9/32",
		Status:    routerstate.AddressLeaseStatusActive,
		OwnerNode: "onprem-router",
		OwnerSite: "onprem",
		OwnerRole: "onprem",
		Epoch:     1,
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("UpsertAddressLease: %v", err)
	}
	controller := Controller{Router: planningRouter(), Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	source := DynamicSource("cloudedge", "azure-router")
	parts, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(parts) != 1 || parts[0].Generation != 1 {
		t.Fatalf("parts = %+v, want one generation-1 part", parts)
	}
	if resources := decodeResources(t, parts[0].ResourcesJSON); countKind(resources, "RemoteAddressClaim") != 1 {
		t.Fatalf("initial resources = %+v, want one claim", resources)
	}

	if err := store.UpsertAddressLease(routerstate.AddressLeaseRecord{
		Pool:      "cloudedge",
		Address:   "10.88.60.9/32",
		Status:    routerstate.AddressLeaseStatusExpired,
		OwnerNode: "onprem-router",
		OwnerSite: "onprem",
		OwnerRole: "onprem",
		Epoch:     1,
		ExpiresAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	controller.Now = func() time.Time { return now.Add(time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	parts, err = store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource after overwrite: %v", err)
	}
	if len(parts) != 1 || parts[0].Generation != 1 {
		t.Fatalf("parts after overwrite = %+v, want one generation-1 part", parts)
	}
	if resources := decodeResources(t, parts[0].ResourcesJSON); countKind(resources, "RemoteAddressClaim") != 0 || countKind(resources, "AddressMobilityDomain") != 1 {
		t.Fatalf("overwritten resources = %+v, want domain only", resources)
	}
	if actions := decodeActionPlans(t, parts[0].ActionPlansJSON); findActionPlan(actions, "unassign-secondary-ip") == nil || findActionPlan(actions, "ensure-forwarding-disabled") == nil {
		t.Fatalf("overwritten action plans = %+v, want de-provision plans", actions)
	}
}

func TestControllerPlannerPlacementDrainMovesDynamicPartBetweenNodes(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	if err := store.UpsertAddressLease(routerstate.AddressLeaseRecord{
		Pool:       "cloudedge",
		Address:    "10.88.60.9/32",
		Status:     routerstate.AddressLeaseStatusActive,
		OwnerNode:  "onprem-router",
		OwnerSite:  "onprem",
		OwnerRole:  "onprem",
		Epoch:      1,
		ObservedAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("UpsertAddressLease: %v", err)
	}
	spec := placementPoolSpec()
	controllerA := Controller{Router: planningRouterForNode("azure-router-a", spec), Store: store, Now: func() time.Time { return now }}
	controllerB := Controller{Router: planningRouterForNode("azure-router-b", spec), Store: store, Now: func() time.Time { return now }}
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile A: %v", err)
	}
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile B: %v", err)
	}
	partA := latestPart(t, store, DynamicSource("cloudedge", "azure-router-a"))
	partB := latestPart(t, store, DynamicSource("cloudedge", "azure-router-b"))
	if countKind(decodeResources(t, partA.ResourcesJSON), "RemoteAddressClaim") != 1 || findActionPlan(decodeActionPlans(t, partA.ActionPlansJSON), "assign-secondary-ip") == nil {
		t.Fatalf("initial A part resources/actions = %s / %s, want active claim+assign", partA.ResourcesJSON, partA.ActionPlansJSON)
	}
	if countKind(decodeResources(t, partB.ResourcesJSON), "RemoteAddressClaim") != 0 || len(decodeActionPlans(t, partB.ActionPlansJSON)) != 0 {
		t.Fatalf("initial B part resources/actions = %s / %s, want inactive", partB.ResourcesJSON, partB.ActionPlansJSON)
	}

	drained := spec
	drained.Members = append([]api.MobilityPoolMember(nil), spec.Members...)
	drained.Members[1].Maintenance.Drain = true
	controllerA.Router = planningRouterForNode("azure-router-a", drained)
	controllerB.Router = planningRouterForNode("azure-router-b", drained)
	controllerA.Now = func() time.Time { return now.Add(time.Minute) }
	controllerB.Now = func() time.Time { return now.Add(time.Minute) }
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("drain reconcile A: %v", err)
	}
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("drain reconcile B: %v", err)
	}
	partA = latestPart(t, store, DynamicSource("cloudedge", "azure-router-a"))
	partB = latestPart(t, store, DynamicSource("cloudedge", "azure-router-b"))
	if countKind(decodeResources(t, partA.ResourcesJSON), "RemoteAddressClaim") != 0 || findActionPlan(decodeActionPlans(t, partA.ActionPlansJSON), "unassign-secondary-ip") == nil {
		t.Fatalf("drained A part resources/actions = %s / %s, want no claim + unassign", partA.ResourcesJSON, partA.ActionPlansJSON)
	}
	if countKind(decodeResources(t, partB.ResourcesJSON), "RemoteAddressClaim") != 1 || findActionPlan(decodeActionPlans(t, partB.ActionPlansJSON), "assign-secondary-ip") == nil {
		t.Fatalf("drained B part resources/actions = %s / %s, want claim + assign", partB.ResourcesJSON, partB.ActionPlansJSON)
	}

	controllerA.Router = planningRouterForNode("azure-router-a", spec)
	controllerB.Router = planningRouterForNode("azure-router-b", spec)
	controllerA.Now = func() time.Time { return now.Add(2 * time.Minute) }
	controllerB.Now = func() time.Time { return now.Add(2 * time.Minute) }
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("return reconcile A: %v", err)
	}
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("return reconcile B: %v", err)
	}
	partA = latestPart(t, store, DynamicSource("cloudedge", "azure-router-a"))
	partB = latestPart(t, store, DynamicSource("cloudedge", "azure-router-b"))
	if countKind(decodeResources(t, partA.ResourcesJSON), "RemoteAddressClaim") != 1 || findActionPlan(decodeActionPlans(t, partA.ActionPlansJSON), "assign-secondary-ip") == nil {
		t.Fatalf("returned A part resources/actions = %s / %s, want claim + assign", partA.ResourcesJSON, partA.ActionPlansJSON)
	}
	if countKind(decodeResources(t, partB.ResourcesJSON), "RemoteAddressClaim") != 0 || findActionPlan(decodeActionPlans(t, partB.ActionPlansJSON), "unassign-secondary-ip") == nil {
		t.Fatalf("returned B part resources/actions = %s / %s, want no claim + unassign", partB.ResourcesJSON, partB.ActionPlansJSON)
	}
}

func TestControllerPlannerPlacementDrainRestartReadsPreviousClaimForImmediateDeprovision(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	path := t.TempDir() + "/routerd.db"
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	lease := routerstate.AddressLeaseRecord{
		Pool:       "cloudedge",
		Address:    "10.88.60.9/32",
		Status:     routerstate.AddressLeaseStatusActive,
		OwnerNode:  "onprem-router",
		OwnerSite:  "onprem",
		OwnerRole:  "onprem",
		Epoch:      1,
		ObservedAt: now,
		ExpiresAt:  now.Add(time.Hour),
	}
	if err := store.UpsertAddressLease(lease); err != nil {
		t.Fatalf("UpsertAddressLease: %v", err)
	}
	spec := placementPoolSpec()
	controller := Controller{Router: planningRouterForNode("azure-router-a", spec), Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial reconcile: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store before restart: %v", err)
	}

	reopened, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer reopened.Close()
	drained := spec
	drained.Members = append([]api.MobilityPoolMember(nil), spec.Members...)
	drained.Members[1].Maintenance.Drain = true
	restarted := Controller{Router: planningRouterForNode("azure-router-a", drained), Store: reopened, Now: func() time.Time { return now.Add(time.Second) }}
	if err := restarted.Reconcile(context.Background()); err != nil {
		t.Fatalf("drain reconcile after restart: %v", err)
	}
	part := latestPart(t, reopened, DynamicSource("cloudedge", "azure-router-a"))
	if countKind(decodeResources(t, part.ResourcesJSON), "RemoteAddressClaim") != 0 {
		t.Fatalf("restarted drained resources = %s, want no claim", part.ResourcesJSON)
	}
	actions := decodeActionPlans(t, part.ActionPlansJSON)
	if findActionPlan(actions, "unassign-secondary-ip") == nil || findActionPlan(actions, "ensure-forwarding-disabled") == nil {
		t.Fatalf("restarted drained actionPlans = %+v, want immediate unassign + forwarding-disable", actions)
	}
}

func TestControllerPlannerPlacementDrainRestartWithoutLeaseStillDeprovisions(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	path := t.TempDir() + "/routerd.db"
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	spec := placementPoolSpec()
	prior, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: spec,
		SelfNode: "azure-router-a",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{{
			Pool:       "cloudedge",
			Address:    "10.88.60.9/32",
			Status:     routerstate.AddressLeaseStatusActive,
			OwnerNode:  "onprem-router",
			OwnerSite:  "onprem",
			OwnerRole:  "onprem",
			Epoch:      1,
			ObservedAt: now,
			ExpiresAt:  now.Add(time.Hour),
		}},
		ProviderProfiles: plannedProviderProfiles(),
	})
	if err != nil {
		t.Fatalf("prior PlanDynamicConfig: %v", err)
	}
	record, err := dynamicPartRecord(prior.Part)
	if err != nil {
		t.Fatalf("dynamicPartRecord: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store before restart: %v", err)
	}

	reopened, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	defer reopened.Close()
	leases, err := reopened.ListAddressLeases("cloudedge", true, now.Add(time.Second))
	if err != nil {
		t.Fatalf("ListAddressLeases: %v", err)
	}
	if len(leases) != 0 {
		t.Fatalf("seeded leases = %+v, want none to cover startup before lease reprojection", leases)
	}
	drained := spec
	drained.Members = append([]api.MobilityPoolMember(nil), spec.Members...)
	drained.Members[1].Maintenance.Drain = true
	restarted := Controller{Router: planningRouterForNode("azure-router-a", drained), Store: reopened, Now: func() time.Time { return now.Add(time.Second) }}
	if err := restarted.Reconcile(context.Background()); err != nil {
		t.Fatalf("drain reconcile after restart without leases: %v", err)
	}
	part := latestPart(t, reopened, DynamicSource("cloudedge", "azure-router-a"))
	if countKind(decodeResources(t, part.ResourcesJSON), "RemoteAddressClaim") != 0 {
		t.Fatalf("restarted drained resources = %s, want no claim", part.ResourcesJSON)
	}
	actions := decodeActionPlans(t, part.ActionPlansJSON)
	unassign := findActionPlan(actions, "unassign-secondary-ip")
	if unassign == nil {
		t.Fatalf("restarted drained actionPlans = %+v, want unassign", actions)
	}
	if unassign.Target["address"] != "10.88.60.9/32" || unassign.Parameters["deprovisionSince"] != now.Add(time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("unexpected unassign plan: %+v", unassign)
	}
	if findActionPlan(actions, "ensure-forwarding-disabled") == nil {
		t.Fatalf("restarted drained actionPlans = %+v, want forwarding-disable", actions)
	}
}

func TestControllerPlannerActiveStartupMissingLeaseRetainsMemoryThenDrainDeprovisions(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	path := t.TempDir() + "/routerd.db"
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	spec := placementPoolSpec()
	prior, err := PlanDynamicConfig(PlannerInput{
		PoolName: "cloudedge",
		PoolSpec: spec,
		SelfNode: "azure-router-a",
		Now:      now,
		Leases: []routerstate.AddressLeaseRecord{{
			Pool:       "cloudedge",
			Address:    "10.88.60.9/32",
			Status:     routerstate.AddressLeaseStatusActive,
			OwnerNode:  "onprem-router",
			OwnerSite:  "onprem",
			OwnerRole:  "onprem",
			Epoch:      1,
			ObservedAt: now,
			ExpiresAt:  now.Add(time.Hour),
		}},
		ProviderProfiles: plannedProviderProfiles(),
	})
	if err != nil {
		t.Fatalf("prior PlanDynamicConfig: %v", err)
	}
	record, err := dynamicPartRecord(prior.Part)
	if err != nil {
		t.Fatalf("dynamicPartRecord: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
	part := latestPart(t, store, DynamicSource("cloudedge", "azure-router-a"))
	if countKind(decodeResources(t, part.ResourcesJSON), "RemoteAddressClaim") != 1 {
		t.Fatalf("initial resources = %s, want claim", part.ResourcesJSON)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store after active reconcile: %v", err)
	}

	reopened, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite: %v", err)
	}
	leases, err := reopened.ListAddressLeases("cloudedge", true, now.Add(time.Second))
	if err != nil {
		t.Fatalf("ListAddressLeases: %v", err)
	}
	if len(leases) != 0 {
		t.Fatalf("seeded leases = %+v, want none", leases)
	}
	activeRestart := Controller{Router: planningRouterForNode("azure-router-a", spec), Store: reopened, Now: func() time.Time { return now.Add(time.Second) }}
	if err := activeRestart.Reconcile(context.Background()); err != nil {
		t.Fatalf("active restart reconcile with missing lease: %v", err)
	}
	part = latestPart(t, reopened, DynamicSource("cloudedge", "azure-router-a"))
	if got := countKind(decodeResources(t, part.ResourcesJSON), "RemoteAddressClaim"); got != 1 {
		t.Fatalf("active restart missing lease resources = %s, want previous claim memory retained", part.ResourcesJSON)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close store after active restart: %v", err)
	}

	reopened, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen sqlite for drain: %v", err)
	}
	defer reopened.Close()
	drained := spec
	drained.Members = append([]api.MobilityPoolMember(nil), spec.Members...)
	drained.Members[1].Maintenance.Drain = true
	drainRestart := Controller{Router: planningRouterForNode("azure-router-a", drained), Store: reopened, Now: func() time.Time { return now.Add(2 * time.Second) }}
	if err := drainRestart.Reconcile(context.Background()); err != nil {
		t.Fatalf("drain restart reconcile: %v", err)
	}
	actions := decodeActionPlans(t, latestPart(t, reopened, DynamicSource("cloudedge", "azure-router-a")).ActionPlansJSON)
	if findActionPlan(actions, "unassign-secondary-ip") == nil {
		t.Fatalf("drain restart actionPlans = %+v, want unassign after retained memory", actions)
	}
}

func TestControllerPlannerPlacementAllDrainedStatus(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	if err := store.UpsertAddressLease(routerstate.AddressLeaseRecord{
		Pool:      "cloudedge",
		Address:   "10.88.60.9/32",
		Status:    routerstate.AddressLeaseStatusActive,
		OwnerNode: "onprem-router",
		OwnerSite: "onprem",
		OwnerRole: "onprem",
		Epoch:     1,
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("UpsertAddressLease: %v", err)
	}
	spec := placementPoolSpec()
	spec.Members[1].Maintenance.Drain = true
	spec.Members[2].Maintenance.Drain = true
	controller := Controller{Router: planningRouterForNode("azure-router-a", spec), Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "NoPlacementCandidate" || !strings.Contains(fmt.Sprint(status["plannerReason"]), "no non-drained members") {
		t.Fatalf("status = %#v, want NoPlacementCandidate with reason", status)
	}
}

func plannedPoolSpec() api.MobilityPoolSpec {
	return api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{
				NodeRef:  "onprem-router",
				Site:     "onprem",
				Role:     "onprem",
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"},
				Delivery: api.MobilityMemberDelivery{PeerRef: "azure", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
					Target:       map[string]string{"region": "japaneast"},
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
		},
		LeasePolicy: api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
	}
}

func placementPoolSpec() api.MobilityPoolSpec {
	spec := plannedPoolSpec()
	spec.Members = []api.MobilityPoolMember{
		spec.Members[0],
		api.MobilityPoolMember{
			NodeRef: "azure-router-a",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
				Target:       map[string]string{"region": "japaneast", "ipConfigName": "capture-a"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
		},
		api.MobilityPoolMember{
			NodeRef: "azure-router-b",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b",
				Target:       map[string]string{"region": "japaneast", "ipConfigName": "capture-b"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
		},
	}
	return spec
}

func plannedProviderProfiles() map[string]api.CloudProviderProfileSpec {
	return map[string]api.CloudProviderProfileSpec{
		"azure-provider": {
			Provider:       "azure",
			SubscriptionID: "sub-1",
			ResourceGroup:  "rg-router",
			Capabilities:   []string{"nic-secondary-ip", "ip-forwarding"},
			Auth:           api.ProviderAuth{Mode: "external-command", Command: "az"},
		},
	}
}

func planningRouter() *api.Router {
	return planningRouterForNode("azure-router", plannedPoolSpec())
}

func planningRouterForNode(nodeName string, spec api.MobilityPoolSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     api.EventGroupSpec{NodeName: nodeName},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     spec,
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
				Metadata: api.ObjectMeta{Name: "azure-provider"},
				Spec: api.CloudProviderProfileSpec{
					Provider:       "azure",
					SubscriptionID: "sub-1",
					ResourceGroup:  "rg-router",
					Capabilities:   []string{"nic-secondary-ip", "ip-forwarding"},
					Auth:           api.ProviderAuth{Mode: "external-command", Command: "az"},
				},
			},
		}},
	}
}

func latestPart(t *testing.T, store interface {
	GetDynamicConfigPartsBySource(string) ([]routerstate.DynamicConfigPartRecord, error)
}, source string) routerstate.DynamicConfigPartRecord {
	t.Helper()
	parts, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource(%s): %v", source, err)
	}
	if len(parts) == 0 {
		t.Fatalf("GetDynamicConfigPartsBySource(%s) returned no parts", source)
	}
	return parts[0]
}

func findActionPlan(plans []dynamicconfig.ActionPlan, action string) *dynamicconfig.ActionPlan {
	for i := range plans {
		if plans[i].Action == action {
			return &plans[i]
		}
	}
	return nil
}

func decodeResources(t *testing.T, raw string) []api.Resource {
	t.Helper()
	var resources []api.Resource
	if err := json.Unmarshal([]byte(raw), &resources); err != nil {
		t.Fatalf("decode resources: %v raw=%s", err, raw)
	}
	return resources
}

func decodeActionPlans(t *testing.T, raw string) []dynamicconfig.ActionPlan {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var plans []dynamicconfig.ActionPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		t.Fatalf("decode action plans: %v raw=%s", err, raw)
	}
	return plans
}

func countKind(resources []api.Resource, kind string) int {
	count := 0
	for _, res := range resources {
		if strings.EqualFold(res.Kind, kind) {
			count++
		}
	}
	return count
}

func firstKind(resources []api.Resource, kind string) api.Resource {
	for _, res := range resources {
		if strings.EqualFold(res.Kind, kind) {
			return res
		}
	}
	return api.Resource{}
}
