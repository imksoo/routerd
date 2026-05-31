// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
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

func planningRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     api.EventGroupSpec{NodeName: "azure-router"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     plannedPoolSpec(),
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
