// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func deliveryPoolForTest(poolName string, spec testMobilityPoolSpec, self memberPlanInfo, members map[string]memberPlanInfo) NormalizedMobilityPool {
	prefix, err := netip.ParsePrefix(spec.Prefix)
	if err != nil {
		panic(err)
	}
	return NormalizedMobilityPool{
		Name:                 poolName,
		Source:               DynamicSource(poolName, self.NodeRef),
		Spec:                 spec.MobilityPoolSpec,
		Prefix:               prefix.Masked(),
		SelfNode:             self.NodeRef,
		SelfCaptureInterface: self.Capture.Interface,
		Self:                 self,
		Members:              members,
	}
}

func TestProviderObservationRequiresFreshUnassignSnapshot(t *testing.T) {
	now := time.Date(2026, 6, 26, 3, 10, 0, 0, time.UTC)
	address := "10.88.60.11/32"
	target := map[string]string{"address": address, "providerRef": "aws-provider", "nicRef": "eni-a"}
	targetJSON, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	previous := []dynamicconfig.ActionPlan{{
		Provider:       "aws",
		ProviderRef:    "aws-provider",
		Action:         actionUnassignSecondaryIP,
		Target:         target,
		IdempotencyKey: "unassign-key",
	}}
	journal := []routerstate.ActionExecutionRecord{{
		ID:             1,
		IdempotencyKey: "unassign-key",
		Provider:       "aws",
		ProviderRef:    "aws-provider",
		Action:         actionUnassignSecondaryIP,
		TargetJSON:     string(targetJSON),
		Status:         routerstate.ActionSucceeded,
		ExecutedAt:     now.Add(time.Second),
		UpdatedAt:      now.Add(time.Second),
	}}
	history := newProviderActionHistoryWithRevision(previous, journal, "")
	_, stale := projectProviderPlanStatus(nil, history, map[string]bool{address: false}, true, now, false, false, time.Time{})
	if stale.PendingCount != 1 || fmt.Sprint(stale.PendingAddresses) != "["+address+"]" {
		t.Fatalf("stale = %#v, want unassign observation pending", stale)
	}
	_, fresh := projectProviderPlanStatus(nil, history, map[string]bool{address: false}, true, now.Add(2*time.Second), false, false, time.Time{})
	if fresh.PendingCount != 0 || len(fresh.PendingAddresses) != 0 {
		t.Fatalf("fresh = %#v, want unassign observation confirmed", fresh)
	}
}

func TestProviderObservationRequiresFreshForwardingSnapshot(t *testing.T) {
	now := time.Date(2026, 6, 26, 3, 15, 0, 0, time.UTC)
	plan := dynamicconfig.ActionPlan{
		Provider:       "aws",
		ProviderRef:    "aws-provider",
		Action:         "ensure-forwarding-enabled",
		Target:         map[string]string{"providerRef": "aws-provider", "nicRef": "eni-a"},
		IdempotencyKey: "forwarding-key",
	}
	targetJSON, err := json.Marshal(plan.Target)
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	journal := []routerstate.ActionExecutionRecord{{
		ID:             1,
		IdempotencyKey: plan.IdempotencyKey,
		Provider:       plan.Provider,
		ProviderRef:    plan.ProviderRef,
		Action:         plan.Action,
		TargetJSON:     string(targetJSON),
		Status:         routerstate.ActionSucceeded,
		ExecutedAt:     now.Add(time.Second),
		UpdatedAt:      now.Add(time.Second),
	}}
	history := newProviderActionHistoryWithRevision(nil, journal, "")
	_, stale := projectProviderPlanStatus([]dynamicconfig.ActionPlan{plan}, history, nil, true, now, true, true, now)
	if stale.PendingCount != 1 || len(stale.PendingAddresses) != 0 {
		t.Fatalf("stale = %#v, want forwarding observation pending", stale)
	}
	_, fresh := projectProviderPlanStatus([]dynamicconfig.ActionPlan{plan}, history, nil, true, now, true, true, now.Add(2*time.Second))
	if fresh.PendingCount != 0 || len(fresh.PendingAddresses) != 0 {
		t.Fatalf("fresh = %#v, want forwarding observation confirmed", fresh)
	}
}

func TestPlanBGPMobilityDeliverySuppressesSameSiteSecondaryIPCapture(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-a"]
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{
			{
				Address:           "10.88.60.11/32",
				Class:             ownershipClassRemoteHomeOwned,
				HomeOwnerNode:     "aws-router-b",
				Source:            providerDiscoverySource,
				SuppressionReason: "remote-home-owner",
			},
			{
				Address:           "10.88.60.12/32",
				Class:             ownershipClassRemoteHomeOwned,
				HomeOwnerNode:     "azure-router",
				Source:            providerDiscoverySource,
				SuppressionReason: "remote-home-owner",
			},
		},
		Placement: PlacementDecision{
			Group:      "aws-edge",
			Active:     true,
			ActiveNode: self.NodeRef,
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{
					"10.88.60.11/32": {"10.99.0.5"},
					"10.88.60.12/32": {"10.99.0.3"},
				},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{
				"aws-provider": {Provider: "aws"},
			}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", "10.88.60.11/32"); assign != nil {
		t.Fatalf("action plans = %#v, same-site AWS home address must not be assigned as router secondary IP", delivery.ProviderActions)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", "10.88.60.12/32"); assign == nil {
		t.Fatalf("action plans = %#v, remote site home address should remain capturable", delivery.ProviderActions)
	}
}

func TestPlanBGPMobilityDeliveryDoesNotReviveIneligibleCaptureAfterFailedAction(t *testing.T) {
	now := time.Date(2026, 6, 26, 14, 20, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.11/32"

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:       address,
			Class:         ownershipClassLocalHomeOwned,
			HomeOwnerNode: self.NodeRef,
			Source:        providerDiscoverySource,
		}},
		Placement: PlacementDecision{
			Group:      "aws-edge",
			Active:     true,
			ActiveNode: self.NodeRef,
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{
				Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{{
					Action:      "assign-secondary-ip",
					Provider:    "aws",
					ProviderRef: "aws-provider",
					TargetJSON:  `{"address":"10.88.60.11/32","nicRef":"eni-b","providerRef":"aws-provider"}`,
					Status:      routerstate.ActionFailed,
					Error:       "provider conflict",
					UpdatedAt:   now.Add(-time.Second),
					ExecutedAt:  now.Add(-time.Second),
				}}, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, failed action must not revive local-home capture", delivery.ProviderActions)
	}
}

func TestPlanBGPMobilityDeliverySuppressesSameSiteFreshHomeStaleCapture(t *testing.T) {
	now := time.Date(2026, 6, 26, 14, 25, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.11/32"

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:           address,
			Class:             ownershipClassStaleCapture,
			HomeOwnerNode:     "aws-router-a",
			SuppressionReason: "fresh-home-owner",
			Source:            providerDiscoverySource,
		}},
		Placement: PlacementDecision{
			Group:      "aws-edge",
			Active:     true,
			ActiveNode: self.NodeRef,
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, same-site fresh-home stale capture must not be assigned", delivery.ProviderActions)
	}
}

func TestPlanBGPMobilityDeliverySuppressesSameProviderFreshHomeStaleCaptureAcrossPlacementGroups(t *testing.T) {
	now := time.Date(2026, 6, 29, 23, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	for i := range spec.Members {
		if spec.Members[i].NodeRef == "aws-router-a" {
			spec.Members[i].Placement.Group = "aws-edge-a"
		}
		if spec.Members[i].NodeRef == "aws-router-b" {
			spec.Members[i].Placement.Group = "aws-edge-b"
		}
	}
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.11/32"

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:           address,
			Class:             ownershipClassStaleCapture,
			HomeOwnerNode:     "aws-router-a",
			HomeProviderRef:   "aws-provider",
			SuppressionReason: "fresh-home-owner",
			Source:            providerDiscoverySource,
		}},
		Placement: PlacementDecision{
			Group:      "aws-edge-b",
			Active:     true,
			ActiveNode: self.NodeRef,
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, same-provider fresh-home stale capture must not be assigned across placement groups", delivery.ProviderActions)
	}
}

func TestPlanBGPMobilityDeliverySuppressesSameSiteRemoteHomeDuringSeize(t *testing.T) {
	now := time.Date(2026, 6, 26, 15, 40, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.11/32"

	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, address, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	for i := range previous {
		stampBGPPathFenceActionPlans(previous[i:i+1], address, "prefix=10.88.60.11/32;nextHops=10.99.0.3", self.NodeRef, time.Time{})
	}

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:           address,
			Class:             ownershipClassRemoteHomeOwned,
			HomeOwnerNode:     "aws-router-a",
			HomeProviderRef:   "aws-provider",
			Source:            providerDiscoverySource,
			SuppressionReason: "remote-home-owner",
		}},
		Placement: PlacementDecision{
			Group:                 "aws-edge",
			Active:                true,
			ActiveNode:            self.NodeRef,
			ActiveIdentityNodeRef: self.NodeRef,
			Seize:                 true,
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(previous, nil, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, same-site remote-home primary must not be assigned during seize", delivery.ProviderActions)
	}
}

func TestPlanBGPMobilityDeliveryDistributedPartialLivenessDoesNotDuplicateAssign(t *testing.T) {
	now := time.Date(2026, 6, 27, 5, 25, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	for i := range spec.Members {
		if spec.Members[i].Placement.Group == "aws-edge" {
			spec.Members[i].MaxSecondaryIPs = 128
		}
	}
	members := plannerMembers(spec.Members)
	address := "10.88.60.12/32"
	decisions := []ownershipDecision{{
		Address:           address,
		Class:             ownershipClassRemoteHomeOwned,
		HomeOwnerNode:     "azure-router",
		Source:            providerDiscoverySource,
		SuppressionReason: "remote-home-owner",
	}}
	planFor := func(self memberPlanInfo, markers map[string]string) PoolPlan {
		t.Helper()
		delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
			Decisions: decisions,
			Placement: PlacementDecision{
				Group:      "aws-edge",
				Active:     true,
				ActiveNode: self.NodeRef,
			},
			PoolRuntimeSnapshot: PoolRuntimeSnapshot{
				Pool: deliveryPoolForTest("cloudedge", spec, self, members),
				BGP: BGPSnapshot{
					InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
					LivenessMarkers:   markers,
					InstalledObserved: true,
				},
				Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}}},
				Now:      now,
			},
		})
		if err != nil {
			t.Fatalf("planBGPMobilityDelivery(%s): %v", self.NodeRef, err)
		}
		return delivery
	}

	selfA := members["aws-router-a"]
	selfB := members["aws-router-b"]
	deliveryA := planFor(selfA, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.0.2/32",
	})
	deliveryB := planFor(selfB, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	})
	assignA := findActionPlanByAddress(deliveryA.ProviderActions, "assign-secondary-ip", address)
	assignB := findActionPlanByAddress(deliveryB.ProviderActions, "assign-secondary-ip", address)
	if assignA != nil && assignB != nil {
		t.Fatalf("partial liveness generated duplicate same-site assigns: A=%#v B=%#v", deliveryA.ProviderActions, deliveryB.ProviderActions)
	}
	if assignA == nil && assignB == nil {
		t.Fatalf("partial liveness generated no assign for either selected holder: A=%#v B=%#v", deliveryA.ProviderActions, deliveryB.ProviderActions)
	}
}

func TestControllerBGPCaptureCandidateNextHopsExcludeReturnRoutes(t *testing.T) {
	now := time.Date(2026, 6, 13, 22, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	prefixes := []map[string]any{
		{
			"prefix":      "10.88.60.4/32",
			"nextHop":     "10.99.0.2",
			"best":        true,
			"valid":       true,
			"communities": []string{bgpstate.MobilityCommunityReturnRoute, bgpstate.MobilityNodeIdentityCommunity("aws-router-a")},
		},
		bgpOwnerPrefix("10.88.60.13/32", "10.99.0.4", "oci-router"),
	}
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.4/32":  {"10.99.0.2"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, prefixes, nil)

	router := routerWithBGPRouter(planningRouterForNode("aws-router-a", spec))
	poolSpec, _ := localizeMobilityPoolSpecForNode(spec, "aws-router-a")
	normalized, err := resolveNormalizedMobilityPool(router, poolSpec)
	if err != nil {
		t.Fatalf("normalize pool: %v", err)
	}
	bgp := collectBGPSnapshot(router, store, normalized.Pool)
	got := bgp.CaptureNextHops
	if !bgp.CaptureRIBObserved {
		t.Fatal("collectBGPSnapshot capture RIB observed=false, want prefixes to be authoritative")
	}
	if _, ok := got["10.88.60.4/32"]; ok {
		t.Fatalf("capture candidate next hops = %#v, router return-route must not be captured", got)
	}
	if hops := got["10.88.60.13/32"]; len(hops) != 1 || hops[0] != "10.99.0.4" {
		t.Fatalf("capture candidate next hops = %#v, want owner path for .13", got)
	}
}

func TestControllerBGPModeStandbyKeepsConfirmedCaptureWhileActiveMarkerAbsent(t *testing.T) {
	now := time.Date(2026, 6, 13, 22, 4, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.12/32"
	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, address, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	stampBGPPathFenceActionPlans(previous, address, "prefix="+address+";nextHops=10.99.0.3", self.NodeRef, now.Add(-time.Minute))
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:            address,
			Class:              ownershipClassConfirmedCapture,
			CaptureHolderNode:  self.NodeRef,
			AdvertiseOwnerNode: self.NodeRef,
			CaptureState:       captureStateConfirmed,
		}},
		Placement: PlacementDecision{
			Group:               "aws-edge",
			Active:              false,
			ActiveNode:          "aws-router-a",
			ActiveMarkerPresent: false,
			Reason:              "configured active marker absent",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP:  BGPSnapshot{InstalledObserved: true},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(previous, nil, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{address: true},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if intent, ok := localCaptureIntentForTest(delivery.LocalDataplane.Captures, address); !ok || intent.Disposition != dynamicconfig.CaptureProtectExisting {
		t.Fatalf("local capture intents = %#v, standby holder must stay protected while active liveness is absent", delivery.LocalDataplane.Captures)
	}
	if unassign := findActionPlanByAddress(delivery.ProviderActions, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("action plans = %#v, standby holder must not release before active liveness returns", delivery.ProviderActions)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, protect-only capture must not reassign", delivery.ProviderActions)
	}
}

func TestPlanBGPMobilityDeliverySuppressesDistributedCaptureDuringSeizeHoldDown(t *testing.T) {
	now := time.Date(2026, 6, 24, 14, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	for i := range spec.Members {
		if spec.Members[i].Placement.Group == "aws-edge" {
			spec.Members[i].MaxSecondaryIPs = 128
		}
	}
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{
			{
				Address:       "10.88.60.10/32",
				Class:         ownershipClassRemoteHomeOwned,
				HomeOwnerNode: "azure-router",
			},
			{
				Address:            "10.88.60.12/32",
				Class:              ownershipClassConfirmedCapture,
				CaptureHolderNode:  self.NodeRef,
				AdvertiseOwnerNode: self.NodeRef,
				CaptureState:       captureStateConfirmed,
			},
		},
		Placement: PlacementDecision{
			Group:              "aws-edge",
			Active:             false,
			ActiveNode:         "aws-router-a",
			Seize:              false,
			SeizeHoldDown:      true,
			SeizeHoldDownKey:   "aws-router-a|aws-router-b",
			SeizeHoldDownSince: now,
			SeizeHoldDownUntil: now.Add(bgpSeizeLivenessMissingHold),
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{"10.88.60.10/32": {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{
				"aws-provider": {Provider: "aws"},
			}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{"10.88.60.12/32": true},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if _, ok := localCaptureIntentForTest(delivery.LocalDataplane.Captures, "10.88.60.10/32"); ok {
		t.Fatalf("local capture intents = %#v, hold-down must suppress new distributed captures", delivery.LocalDataplane.Captures)
	}
	if intent, ok := localCaptureIntentForTest(delivery.LocalDataplane.Captures, "10.88.60.12/32"); !ok || intent.Disposition != dynamicconfig.CaptureProtectExisting {
		t.Fatalf("local capture intents = %#v, hold-down must retain only protect-only self captures", delivery.LocalDataplane.Captures)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", "10.88.60.10/32"); assign != nil {
		t.Fatalf("action plans = %#v, hold-down must not assign new distributed captures", delivery.ProviderActions)
	}
}

func TestPlanBGPMobilityDeliveryStampsProviderActionAssignmentFence(t *testing.T) {
	now := time.Date(2026, 6, 24, 15, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:       address,
			Class:         ownershipClassRemoteHomeOwned,
			HomeOwnerNode: "azure-router",
		}},
		Placement: PlacementDecision{
			Group:                 "aws-edge",
			Active:                true,
			ActiveNode:            self.NodeRef,
			Seize:                 true,
			ActiveIdentityNodeRef: "aws-router-a",
			Reason:                "active BGP liveness marker is absent",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{
				"aws-provider": {Provider: "aws"},
			}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want assign", delivery.ProviderActions)
	}
	if assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign parameters = %#v, want failover reassignment", assign.Parameters)
	}
	if assign.Parameters[captureAssignmentGenerationParam] == "" {
		t.Fatalf("assign parameters = %#v, want assignment generation", assign.Parameters)
	}
	if !strings.Contains(assign.IdempotencyKey, ":assigngen:"+safeName(assign.Parameters[captureAssignmentGenerationParam])) {
		t.Fatalf("assign idempotencyKey = %q, parameters = %#v, want assignment generation fence", assign.IdempotencyKey, assign.Parameters)
	}
	if assign.Parameters[captureAssignmentDesiredHolderParam] != self.NodeRef {
		t.Fatalf("assign parameters = %#v, want assignment desired holder %s", assign.Parameters, self.NodeRef)
	}
	if assign.Parameters[captureAssignmentPreviousHolderParam] != "aws-router-a" {
		t.Fatalf("assign parameters = %#v, want assignment previous holder", assign.Parameters)
	}
	if assign.Parameters[bgpPathSigParam] == "" || assign.Parameters[captureParamHolder] != self.NodeRef {
		t.Fatalf("assign parameters = %#v, want path and holder fences", assign.Parameters)
	}
	if _, err := time.Parse(time.RFC3339Nano, assign.Parameters[captureAssignmentLeaseUntilParam]); err != nil {
		t.Fatalf("assign parameters = %#v, want RFC3339 assignment leaseUntil: %v", assign.Parameters, err)
	}
}

func TestPlanBGPMobilityDeliveryAssignsPerAddressGenerations(t *testing.T) {
	now := time.Date(2026, 6, 25, 23, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	addresses := []string{"10.88.60.10/32", "10.88.60.12/32"}
	decisions := []ownershipDecision{
		{Address: addresses[0], Class: ownershipClassRemoteHomeOwned, HomeOwnerNode: "azure-router"},
		{Address: addresses[1], Class: ownershipClassRemoteHomeOwned, HomeOwnerNode: "oci-router"},
	}
	installed := map[string][]string{
		addresses[0]: {"10.99.0.3"},
		addresses[1]: {"10.99.0.4"},
	}
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: decisions,
		Placement: PlacementDecision{
			Group:                 "aws-edge",
			Active:                true,
			ActiveNode:            self.NodeRef,
			Seize:                 true,
			ActiveIdentityNodeRef: "aws-router-a",
			Reason:                "hard-failure",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: installed,
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	generations := map[string]bool{}
	for _, address := range addresses {
		assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address)
		if assign == nil {
			t.Fatalf("action plans = %#v, want assign for %s", delivery.ProviderActions, address)
		}
		generation := assign.Parameters[captureAssignmentGenerationParam]
		if generation == "" {
			t.Fatalf("assign %s parameters = %#v, want assignment generation", address, assign.Parameters)
		}
		if generations[generation] {
			t.Fatalf("assignment generation %q reused across addresses; plans=%#v", generation, delivery.ProviderActions)
		}
		generations[generation] = true
		if !strings.Contains(assign.IdempotencyKey, ":assigngen:"+safeName(generation)) {
			t.Fatalf("assign %s key = %q, want assignment generation fence", address, assign.IdempotencyKey)
		}
		if got := captureAssignmentsFromActionPlans(delivery.ProviderActions)[address].Generation; got != generation {
			t.Fatalf("published assignment %s generation = %q, want %q", address, got, generation)
		}
	}
}

func TestPlanBGPMobilityDeliveryStampsAssignmentGenerationForStandbyCapture(t *testing.T) {
	now := time.Date(2026, 6, 26, 1, 35, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:       address,
			Class:         ownershipClassRemoteHomeOwned,
			HomeOwnerNode: "azure-router",
		}},
		Placement: PlacementDecision{
			Group:      "aws-edge",
			Active:     true,
			ActiveNode: "aws-router-a",
			Reason:     "peer-active",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want standby capture assign", delivery.ProviderActions)
	}
	generation := assign.Parameters[captureAssignmentGenerationParam]
	if generation == "" {
		t.Fatalf("assign parameters = %#v, want assignment generation", assign.Parameters)
	}
	if !strings.Contains(assign.IdempotencyKey, ":assigngen:"+safeName(generation)) {
		t.Fatalf("assign idempotencyKey = %q, parameters = %#v, want assignment generation fence", assign.IdempotencyKey, assign.Parameters)
	}
	if assign.Parameters[captureAssignmentDesiredHolderParam] != self.NodeRef {
		t.Fatalf("assign parameters = %#v, want assignment desired holder %s", assign.Parameters, self.NodeRef)
	}
	if _, err := time.Parse(time.RFC3339Nano, assign.Parameters[captureAssignmentLeaseUntilParam]); err != nil {
		t.Fatalf("assign parameters = %#v, want assignment leaseUntil: %v", assign.Parameters, err)
	}
	if got := captureAssignmentsFromActionPlans(delivery.ProviderActions)[address].Generation; got != generation {
		t.Fatalf("published assignment generation = %q, want %q", got, generation)
	}
}

func TestControllerBGPModeStandbyReleasesConfirmedCaptureWhenActiveMarkerReturns(t *testing.T) {
	now := time.Date(2026, 6, 13, 22, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.12/32"
	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, address, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	stampBGPPathFenceActionPlans(previous, address, "prefix="+address+";nextHops=10.99.0.3", self.NodeRef, now.Add(-time.Minute))
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:            address,
			Class:              ownershipClassConfirmedCapture,
			CaptureHolderNode:  self.NodeRef,
			AdvertiseOwnerNode: self.NodeRef,
			CaptureState:       captureStateConfirmed,
		}},
		Placement: PlacementDecision{
			Group:               "aws-edge",
			Active:              false,
			ActiveNode:          "aws-router-a",
			ActiveMarkerPresent: true,
			Reason:              "configured active has returned",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP:  BGPSnapshot{InstalledObserved: true},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(previous, nil, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{address: true},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if intent, ok := localCaptureIntentForTest(delivery.LocalDataplane.Captures, address); ok && intent.Disposition == dynamicconfig.CaptureProtectExisting {
		t.Fatalf("local capture intents = %#v, standby holder must release after configured active liveness returns", delivery.LocalDataplane.Captures)
	}
	if unassign := findActionPlanByAddress(delivery.ProviderActions, "unassign-secondary-ip", address); unassign == nil {
		t.Fatalf("action plans = %#v, standby confirmed holder must release after configured active liveness returns", delivery.ProviderActions)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, protect-only capture must not reassign", delivery.ProviderActions)
	}
	if len(delivery.LocalDataplane.Captures) != 1 || delivery.LocalDataplane.Captures[0].Address != address || delivery.LocalDataplane.Captures[0].Disposition != dynamicconfig.CaptureRelease {
		t.Fatalf("local capture intents = %#v, standby release must be explicit", delivery.LocalDataplane.Captures)
	}
}

func TestControllerBGPModeStandbyReleasesObservedSelfCaptureWithoutPriorAction(t *testing.T) {
	now := time.Date(2026, 6, 14, 21, 40, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:            address,
			Class:              ownershipClassRemoteHomeOwned,
			HomeOwnerNode:      "onprem-router",
			CaptureHolderNode:  self.NodeRef,
			CaptureProviderRef: "aws-provider",
			CaptureTargetRef:   "eni-b",
			CaptureState:       captureStateConfirmed,
			CaptureStrategy:    captureStrategySecondaryIP,
			CaptureSucceeded:   true,
			Source:             "bgp-owner",
			SuppressionReason:  "bgp-owner",
		}},
		Placement: PlacementDecision{
			Group:               "aws-edge",
			Active:              false,
			ActiveNode:          "aws-router-a",
			ActiveMarkerPresent: true,
			Reason:              "configured active has returned",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool:     deliveryPoolForTest("cloudedge", spec, self, members),
			BGP:      BGPSnapshot{InstalledObserved: true},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{address: true},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if unassign := findActionPlanByAddress(delivery.ProviderActions, "unassign-secondary-ip", address); unassign == nil {
		t.Fatalf("action plans = %#v, standby observed self-capture must release once active liveness is present", delivery.ProviderActions)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, standby observed self-capture must not reassign", delivery.ProviderActions)
	}
}

func TestPlanBGPMobilityDeliveryReleasesProviderConflictLoserCapture(t *testing.T) {
	now := time.Date(2026, 6, 14, 21, 42, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:            address,
			Class:              ownershipClassStaleCapture,
			HomeOwnerNode:      "aws-router-a",
			CaptureHolderNode:  self.NodeRef,
			CaptureProviderRef: "aws-provider",
			CaptureTargetRef:   "eni-b",
			CaptureState:       captureStateConfirmed,
			CaptureStrategy:    captureStrategySecondaryIP,
			CaptureSucceeded:   true,
			Source:             providerDiscoverySource,
			SuppressionReason:  "provider-split-brain-loser",
			ConflictReason:     "duplicate-provider-home-owners",
			ConflictWinnerNode: "aws-router-a",
			ConflictResolution: "loser-release-local-capture",
		}},
		Placement: PlacementDecision{
			Group:      "aws-edge",
			Active:     false,
			ActiveNode: "aws-router-a",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool:     deliveryPoolForTest("cloudedge", spec, self, members),
			BGP:      BGPSnapshot{InstalledObserved: true},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{address: true},
				SelfInventoryKnown: true,
			},
			Previous: PreviousPoolState{ObservedStaleSince: map[string]time.Time{address: now.Add(-3 * time.Minute)}},
			Now:      now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if unassign := findActionPlanByAddress(delivery.ProviderActions, "unassign-secondary-ip", address); unassign == nil {
		t.Fatalf("action plans = %#v, conflict loser self-capture must be released after hold-down", delivery.ProviderActions)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, conflict loser must not reassign", delivery.ProviderActions)
	}
}

func TestControllerBGPModeStandbyKeepsObservedSelfCaptureWhileActiveMarkerAbsent(t *testing.T) {
	now := time.Date(2026, 6, 14, 21, 45, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:            address,
			Class:              ownershipClassRemoteHomeOwned,
			HomeOwnerNode:      "onprem-router",
			CaptureHolderNode:  self.NodeRef,
			CaptureProviderRef: "aws-provider",
			CaptureTargetRef:   "eni-b",
			CaptureState:       captureStateConfirmed,
			CaptureStrategy:    captureStrategySecondaryIP,
			CaptureSucceeded:   true,
			Source:             "bgp-owner",
			SuppressionReason:  "bgp-owner",
		}},
		Placement: PlacementDecision{
			Group:               "aws-edge",
			Active:              false,
			ActiveNode:          "aws-router-a",
			ActiveMarkerPresent: false,
			Reason:              "configured active marker absent",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool:     deliveryPoolForTest("cloudedge", spec, self, members),
			BGP:      BGPSnapshot{InstalledObserved: true},
			Provider: ProviderSnapshot{Profiles: map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}}},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{address: true},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if intent, ok := localCaptureIntentForTest(delivery.LocalDataplane.Captures, address); !ok || intent.Disposition != dynamicconfig.CaptureProtectExisting {
		t.Fatalf("local capture intents = %#v, standby holder must stay protected while active liveness is absent", delivery.LocalDataplane.Captures)
	}
	if unassign := findActionPlanByAddress(delivery.ProviderActions, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("action plans = %#v, standby observed self-capture must not release before active liveness returns", delivery.ProviderActions)
	}
}

func TestControllerBGPModeStandbyReleaseSkipsAbsentObservedSelfCapture(t *testing.T) {
	now := time.Date(2026, 6, 14, 17, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.12/32"
	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, address, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	stampBGPPathFenceActionPlans(previous, address, "prefix="+address+";nextHops=10.99.0.3", self.NodeRef, now.Add(-time.Minute))
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:            address,
			Class:              ownershipClassRemoteHomeOwned,
			HomeOwnerNode:      "azure-router",
			Source:             "bgp-owner",
			SuppressionReason:  "bgp-owner",
			CaptureState:       captureStateConfirmed,
			CaptureHolderNode:  self.NodeRef,
			AdvertiseOwnerNode: "azure-router",
		}},
		Placement: PlacementDecision{
			Group:               "aws-edge",
			Active:              false,
			ActiveNode:          "aws-router-a",
			ActiveMarkerPresent: true,
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP:  BGPSnapshot{InstalledObserved: true},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(previous, nil, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if unassign := findActionPlanByAddress(delivery.ProviderActions, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("action plans = %#v, absent fresh self inventory must not generate redundant unassign", delivery.ProviderActions)
	}
}

func TestControllerBGPModeProtectsObservedRemoteHomeCaptureFromUnassign(t *testing.T) {
	now := time.Date(2026, 6, 13, 22, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-a"]
	address := "10.88.60.10/32"
	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, address, map[string]bool{}, false)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	stampBGPPathFenceActionPlans(previous, address, "prefix="+address+";nextHops=10.99.0.1", self.NodeRef, now.Add(-time.Minute))
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:            address,
			Class:              ownershipClassRemoteHomeOwned,
			HomeOwnerNode:      "onprem-router",
			Source:             "bgp-owner",
			SuppressionReason:  "bgp-owner",
			CaptureState:       captureStateStale,
			CaptureHolderNode:  self.NodeRef,
			CaptureProviderRef: "aws-provider",
			CaptureTargetRef:   "eni-a",
			CaptureStrategy:    captureStrategySecondaryIP,
		}},
		Placement: PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: self.NodeRef},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP:  BGPSnapshot{InstalledObserved: true},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(previous, nil, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:    map[string]bool{address: true},
				SelfInventoryKnown: true,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if intent, ok := localCaptureIntentForTest(delivery.LocalDataplane.Captures, address); !ok || intent.Disposition != dynamicconfig.CaptureProtectExisting {
		t.Fatalf("local capture intents = %#v, want observed remote-home capture protected", delivery.LocalDataplane.Captures)
	}
	if unassign := findActionPlanByAddress(delivery.ProviderActions, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("action plans = %#v, observed desired remote-home capture must not unassign", delivery.ProviderActions)
	}
	if assign := findActionPlanByAddress(delivery.ProviderActions, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, protect-only capture must not reassign", delivery.ProviderActions)
	}
}

func TestBGPProviderDeprovisionUnassignDoesNotRecapture(t *testing.T) {
	now := time.Date(2026, 6, 24, 16, 40, 0, 0, time.UTC)
	self := memberPlanInfo{
		NodeRef: "aws-router-a",
		Capture: api.MobilityMemberCapture{
			ProviderRef: "aws-provider",
			NICRef:      "eni-a",
		},
	}
	address := "10.88.60.12/32"
	targetJSON, err := json.Marshal(map[string]string{
		"address":     address,
		"nicRef":      "eni-a",
		"providerRef": "aws-provider",
	})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	paramsJSON, err := json.Marshal(map[string]string{
		bgpPathSigParam:    "deprovision:" + address + ":observed-self-stale:since=" + now.Add(-time.Minute).Format(time.RFC3339Nano),
		"deprovisionSince": now.Add(-time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	journal := []routerstate.ActionExecutionRecord{{
		ID:             42,
		ProviderRef:    "aws-provider",
		Action:         "unassign-secondary-ip",
		TargetJSON:     string(targetJSON),
		ParametersJSON: string(paramsJSON),
		Status:         routerstate.ActionSucceeded,
		ExecutedAt:     now,
	}}
	history := newProviderActionHistoryWithRevision(nil, journal, "")
	if shouldAllowBGPTrapReassignment(self, address, history, map[string]bool{address: true}, true, now) {
		t.Fatal("deprovision unassign must not allow transition recapture")
	}
	plans := []dynamicconfig.ActionPlan{{
		Action:         "assign-secondary-ip",
		IdempotencyKey: "assign",
		Target: map[string]string{
			"address":     address,
			"nicRef":      "eni-a",
			"providerRef": "aws-provider",
		},
	}}
	stampBGPProviderTransitionFences(plans, self, address, history, map[string]bool{address: true}, true, now)
	if strings.Contains(plans[0].IdempotencyKey, ":transition:") {
		t.Fatalf("plan = %#v, deprovision unassign must not stamp transition recapture", plans[0])
	}
}

func TestPlanBGPMobilityDeliveryUsesCanonicalAssignKeyBeforeTransitionRetry(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	targetRef := providerCaptureRefFromCapture(self.Capture)
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForPlannerTest(t, 41, actionUnassignSecondaryIP, address, self.Capture.ProviderRef, targetRef, self.NodeRef, now.Add(-10*time.Second), map[string]string{
			bgpPathSigParam:    "deprovision:" + address + ":observed-self-stale:since=" + now.Add(-time.Minute).Format(time.RFC3339Nano),
			"deprovisionSince": now.Add(-time.Minute).Format(time.RFC3339Nano),
		}),
	}

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:       address,
			Class:         ownershipClassRemoteHomeOwned,
			HomeOwnerNode: "azure-router",
		}},
		Placement: PlacementDecision{
			Group:                 "aws-edge",
			Active:                true,
			ActiveNode:            self.NodeRef,
			Seize:                 true,
			ActiveIdentityNodeRef: "aws-router-a",
			Reason:                "leaf-rejoin",
		},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(nil, journal, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:     map[string]bool{},
				SelfInventoryKnown:  true,
				DiscoveryLastScanAt: now,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ProviderActions, actionAssignSecondaryIP, address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want assign", delivery.ProviderActions)
	}
	if strings.Contains(assign.IdempotencyKey, ":transition:") {
		t.Fatalf("assign key/parameters = %q %#v, fresh assignment must use canonical key before transition retry", assign.IdempotencyKey, assign.Parameters)
	}
}

func TestPlanBGPMobilityDeliverySuppressesProviderMissingRetryBeforeHold(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-a"]
	address := "10.88.60.10/32"
	targetRef := providerCaptureRefFromCapture(self.Capture)
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForPlannerTest(t, 51, actionAssignSecondaryIP, address, self.Capture.ProviderRef, targetRef, self.NodeRef, now.Add(-5*time.Second), nil),
	}

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:       address,
			Class:         ownershipClassRemoteHomeOwned,
			HomeOwnerNode: "azure-router",
		}},
		Placement: PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: self.NodeRef},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(nil, journal, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:     map[string]bool{},
				SelfInventoryKnown:  true,
				DiscoveryLastScanAt: now,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ProviderActions, actionAssignSecondaryIP, address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want retained canonical assign plan", delivery.ProviderActions)
	}
	if strings.Contains(assign.IdempotencyKey, ":transition:provider-missing-") {
		t.Fatalf("assign key/parameters = %q %#v, provider-missing retry must wait for hold", assign.IdempotencyKey, assign.Parameters)
	}
}

func TestPlanBGPMobilityDeliveryRetriesProviderMissingAfterCanonicalSucceededAndHoldElapsed(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-a"]
	address := "10.88.60.10/32"
	targetRef := providerCaptureRefFromCapture(self.Capture)
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForPlannerTest(t, 61, actionAssignSecondaryIP, address, self.Capture.ProviderRef, targetRef, self.NodeRef, now.Add(-bgpProviderMissingRetryHold-time.Second), nil),
	}

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:       address,
			Class:         ownershipClassRemoteHomeOwned,
			HomeOwnerNode: "azure-router",
		}},
		Placement: PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: self.NodeRef},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(nil, journal, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:     map[string]bool{},
				SelfInventoryKnown:  true,
				DiscoveryLastScanAt: now,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ProviderActions, actionAssignSecondaryIP, address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want provider-missing retry assign", delivery.ProviderActions)
	}
	if !strings.Contains(assign.IdempotencyKey, ":transition:provider-missing-61") {
		t.Fatalf("assign key/parameters = %q %#v, want provider-missing retry after hold", assign.IdempotencyKey, assign.Parameters)
	}
}

func TestPlanBGPMobilityDeliveryAllowsAfterUnassignRecaptureWhenCanonicalSucceeded(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 15, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	self := members["aws-router-a"]
	address := "10.88.60.10/32"
	targetRef := providerCaptureRefFromCapture(self.Capture)
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForPlannerTest(t, 71, actionAssignSecondaryIP, address, self.Capture.ProviderRef, targetRef, self.NodeRef, now.Add(-time.Minute), nil),
		providerCaptureActionRecordForPlannerTest(t, 72, actionUnassignSecondaryIP, address, self.Capture.ProviderRef, targetRef, self.NodeRef, now.Add(-time.Second), nil),
	}

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		Decisions: []ownershipDecision{{
			Address:       address,
			Class:         ownershipClassRemoteHomeOwned,
			HomeOwnerNode: "azure-router",
		}},
		Placement: PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: self.NodeRef},
		PoolRuntimeSnapshot: PoolRuntimeSnapshot{
			Pool: deliveryPoolForTest("cloudedge", spec, self, members),
			BGP: BGPSnapshot{
				InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
				InstalledObserved: true,
			},
			Provider: ProviderSnapshot{
				Profiles:      map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
				ActionHistory: newProviderActionHistoryWithRevision(nil, journal, ""),
			},
			Ownership: OwnershipFacts{
				SelfCapturedIPs:     map[string]bool{},
				SelfInventoryKnown:  true,
				DiscoveryLastScanAt: now,
			},
			Now: now,
		},
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ProviderActions, actionAssignSecondaryIP, address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want after-unassign recapture assign", delivery.ProviderActions)
	}
	if !strings.Contains(assign.IdempotencyKey, ":transition:after-unassign-72") {
		t.Fatalf("assign key/parameters = %q %#v, want after-unassign recapture despite canonical succeeded", assign.IdempotencyKey, assign.Parameters)
	}
}

func providerCaptureActionRecordForPlannerTest(t *testing.T, id int64, action, address, providerRef, nicRef, holder string, at time.Time, params map[string]string) routerstate.ActionExecutionRecord {
	t.Helper()
	targetJSON, err := json.Marshal(map[string]string{
		"address":     address,
		"nicRef":      nicRef,
		"providerRef": providerRef,
	})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	if params == nil {
		params = map[string]string{}
	}
	if params[bgpPathSigParam] == "" {
		params[bgpPathSigParam] = "prefix=" + normalizeAddressString(address) + ";nextHops=10.99.0.3"
	}
	if params[captureParamHolder] == "" {
		params[captureParamHolder] = holder
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return routerstate.ActionExecutionRecord{
		ID:             id,
		IdempotencyKey: strings.Join([]string{"test", providerRef, nicRef, action, address, fmt.Sprint(id)}, ":"),
		Provider:       strings.TrimSuffix(providerRef, "-provider"),
		ProviderRef:    providerRef,
		Action:         action,
		TargetJSON:     string(targetJSON),
		ParametersJSON: string(paramsJSON),
		Status:         routerstate.ActionSucceeded,
		ExecutedAt:     at.UTC(),
		UpdatedAt:      at.UTC(),
	}
}

func seedSucceededActionRecordForPlannerTest(t *testing.T, store *routerstate.SQLiteStore, rec routerstate.ActionExecutionRecord) {
	t.Helper()
	inserted := importActionRecordForPlannerTest(t, store, rec)
	if !inserted {
		t.Fatalf("seed action %q was not inserted", rec.IdempotencyKey)
	}
	markActionSucceededByKey(t, store, rec.IdempotencyKey, rec.ExecutedAt)
}

func importActionPlanRecord(t *testing.T, store *routerstate.SQLiteStore, source string, plan dynamicconfig.ActionPlan, now time.Time) bool {
	t.Helper()
	targetJSON, err := json.Marshal(plan.Target)
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	paramsJSON, err := json.Marshal(plan.Parameters)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return importActionRecordForPlannerTest(t, store, routerstate.ActionExecutionRecord{
		IdempotencyKey: plan.IdempotencyKey,
		Source:         source,
		Provider:       plan.Provider,
		ProviderRef:    plan.ProviderRef,
		Action:         plan.Action,
		TargetJSON:     string(targetJSON),
		ParametersJSON: string(paramsJSON),
		RiskLevel:      plan.RiskLevel,
		Status:         routerstate.ActionPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

func importActionRecordForPlannerTest(t *testing.T, store *routerstate.SQLiteStore, rec routerstate.ActionExecutionRecord) bool {
	t.Helper()
	rec.Status = routerstate.ActionPending
	inserted, err := store.ImportAction(rec)
	if err != nil {
		t.Fatalf("ImportAction(%q): %v", rec.IdempotencyKey, err)
	}
	return inserted
}

func markActionSucceededByKey(t *testing.T, store *routerstate.SQLiteStore, key string, at time.Time) {
	t.Helper()
	rec, ok, err := store.GetActionByIdempotencyKey(key)
	if err != nil || !ok {
		t.Fatalf("GetActionByIdempotencyKey(%q): ok=%v err=%v", key, ok, err)
	}
	if err := store.ApproveAction(rec.ID, "test", at.Add(-time.Second)); err != nil {
		t.Fatalf("ApproveAction(%q): %v", key, err)
	}
	claimed, err := store.BeginActionExecution(rec.ID, at.Add(-500*time.Millisecond))
	if err != nil || !claimed {
		t.Fatalf("BeginActionExecution(%q): claimed=%v err=%v", key, claimed, err)
	}
	if err := store.MarkActionResult(rec.ID, routerstate.ActionSucceeded, "ok", "", nil, at); err != nil {
		t.Fatalf("MarkActionResult(%q): %v", key, err)
	}
}

func countActionRowsByAddress(t *testing.T, store *routerstate.SQLiteStore, action, address string) int {
	t.Helper()
	rows, err := store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		t.Fatalf("ListActions: %v", err)
	}
	address = normalizeAddressString(address)
	count := 0
	for _, row := range rows {
		if row.Action != action {
			continue
		}
		target := decodeActionRecordMap(row.TargetJSON)
		if normalizeAddressString(target["address"]) == address {
			count++
		}
	}
	return count
}

func TestBGPPathSigFromObservedSelfStaleIsStable(t *testing.T) {
	staleSince := time.Date(2026, 6, 10, 18, 45, 0, 0, time.UTC)
	first := bgpPathSigFromObservedSelfStale("10.88.60.10/32", staleSince)
	second := bgpPathSigFromObservedSelfStale("10.88.60.10", staleSince)
	if first != second {
		t.Fatalf("path sig mismatch for same address: %q != %q", first, second)
	}
	nextGeneration := bgpPathSigFromObservedSelfStale("10.88.60.10/32", staleSince.Add(time.Minute))
	if first == nextGeneration {
		t.Fatalf("path sig must distinguish repeated stale cleanup generations for the same address: %q", first)
	}
	if !strings.Contains(first, "observed-self-stale") {
		t.Fatalf("path sig %q does not identify observed self-stale cleanup", first)
	}
	if !strings.Contains(first, staleSince.Format(time.RFC3339Nano)) {
		t.Fatalf("path sig %q does not include stale first-seen generation %q", first, staleSince.Format(time.RFC3339Nano))
	}
}

func TestBGPObservedSelfStaleCleanupIdempotencyKeyIncludesGeneration(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "aws"}
	capture := api.MobilityMemberCapture{
		Type:        "provider-secondary-ip",
		ProviderRef: "aws-provider",
		NICRef:      "eni-a",
		Target:      map[string]string{"region": "ap-northeast-1"},
	}
	address := "10.88.60.10/32"
	holder := "aws-router-a"
	firstSeen := time.Date(2026, 6, 10, 18, 45, 0, 0, time.UTC)

	planFor := func(staleSince time.Time) dynamicconfig.ActionPlan {
		t.Helper()
		plan, err := providerCaptureActionPlan("cloudedge", profile, capture, address, false, false, time.Date(2026, 6, 10, 18, 50, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("providerCaptureActionPlan: %v", err)
		}
		plans := []dynamicconfig.ActionPlan{plan}
		stampBGPPathFenceActionPlans(plans, address, bgpPathSigFromObservedSelfStale(address, staleSince), holder, time.Time{})
		return plans[0]
	}

	first := planFor(firstSeen)
	sameGeneration := planFor(firstSeen)
	nextGeneration := planFor(firstSeen.Add(time.Minute))
	if first.IdempotencyKey != sameGeneration.IdempotencyKey {
		t.Fatalf("same stale generation produced different idempotency keys:\n%s\n%s", first.IdempotencyKey, sameGeneration.IdempotencyKey)
	}
	if first.Parameters[bgpPathSigParam] != sameGeneration.Parameters[bgpPathSigParam] {
		t.Fatalf("same stale generation produced different path sigs:\n%s\n%s", first.Parameters[bgpPathSigParam], sameGeneration.Parameters[bgpPathSigParam])
	}
	if first.IdempotencyKey == nextGeneration.IdempotencyKey {
		t.Fatalf("different stale generations must not collide on idempotency key: %s", first.IdempotencyKey)
	}
	if first.Parameters[bgpPathSigParam] == nextGeneration.Parameters[bgpPathSigParam] {
		t.Fatalf("different stale generations must not collide on path sig: %s", first.Parameters[bgpPathSigParam])
	}
	if !strings.Contains(first.Parameters[bgpPathSigParam], firstSeen.Format(time.RFC3339Nano)) {
		t.Fatalf("path sig %q does not include first stale generation", first.Parameters[bgpPathSigParam])
	}
}

func testStore(t *testing.T, now time.Time) *routerstate.SQLiteStore {
	t.Helper()
	_ = now
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func recordEvent(t *testing.T, store *routerstate.SQLiteStore, rec routerstate.EventRecord) {
	t.Helper()
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("RecordFederationEvent: %v", err)
	}
}

// bootstrapOnPremDiscovery creates the same daemon intent and durable arm
// fact that a real discovery reconcile creates. Controller tests must not use
// MobilityPool status as a substitute for either input.
func bootstrapOnPremDiscovery(t *testing.T, store *routerstate.SQLiteStore, router *api.Router, armedAt time.Time) {
	t.Helper()
	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return armedAt }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("bootstrap onprem discovery: %v", err)
	}
}

// recordProviderDiscoveryRuntime feeds controller tests through the same
// durable typed fact that production consumes, rather than through a
// MobilityPool status projection.
func recordProviderDiscoveryRuntime(t *testing.T, store *routerstate.SQLiteStore, nodeRef string, fact providerDiscoveryRuntimeFact, observedAt time.Time) {
	t.Helper()
	recordEvent(t, store, providerDiscoveryRuntimeEventForTest(t, nodeRef, fact, observedAt, DefaultLeaseTTL))
}

// providerDiscoveryRuntimeEventForTest builds the sole provider ownership
// input accepted by production. Tests use it instead of reconstructing the
// removed per-address provider event protocol.
func providerDiscoveryRuntimeEventForTest(t *testing.T, nodeRef string, fact providerDiscoveryRuntimeFact, observedAt time.Time, ttl time.Duration) routerstate.EventRecord {
	t.Helper()
	placement := PlacementDecision{
		LivenessObserved:    fact.Placement.LivenessObserved,
		ActiveNode:          fact.Placement.ActiveNode,
		Active:              fact.Placement.Active,
		Seize:               fact.Placement.Seize,
		SelfMarkerPresent:   fact.Placement.SelfMarkerPresent,
		ActiveMarkerPresent: fact.Placement.ActiveMarkerPresent,
		SelfMarker:          fact.Placement.SelfMarker,
		ActiveMarker:        fact.Placement.ActiveMarker,
	}
	pool := NormalizedMobilityPool{
		Name:     "cloudedge",
		Spec:     api.MobilityPoolSpec{GroupRef: "cloudedge"},
		SelfNode: nodeRef,
	}
	event, err := providerDiscoveryRuntimeEvent(pool, fact.Self, fact.Addresses, placement, observedAt, ttl)
	if err != nil {
		t.Fatalf("provider discovery runtime event: %v", err)
	}
	return event
}

func providerDiscoveryAddressFactForTest(address, provider, providerRef string, record providerinventory.PrivateIPRecord, observedAt time.Time, ttl time.Duration) providerDiscoveryAddressFact {
	return providerDiscoveryAddressFactFromRecord(address, provider, providerRef, record, observedAt, ttl)
}

func idsOfEvents(events []routerstate.EventRecord) []string {
	ids := make([]string, 0, len(events))
	for _, ev := range events {
		ids = append(ids, ev.ID)
	}
	return ids
}

func countEvents(events []routerstate.EventRecord, eventType, sourceNode, subject string) int {
	var count int
	for _, ev := range events {
		if ev.Type == eventType && ev.SourceNode == sourceNode && ev.Subject == subject {
			count++
		}
	}
	return count
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func profileAWSFailoverPoolSpecForNode(selfNode string) testMobilityPoolSpec {
	spec := awsFailoverPoolSpec()
	spec.Values = map[string]string{
		"aws.region": "ap-northeast-1",
		"aws.nic":    map[string]string{"aws-router-a": "eni-a", "aws-router-b": "eni-b"}[selfNode],
	}
	spec.Profiles = api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
		"aws-edge": {
			Capture: api.MobilityMemberCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "aws-provider",
				TargetFrom:  map[string]string{"region": "aws.region"},
				NICRef:      spec.Values["aws.nic"],
			},
		},
	}}
	spec.Members = []api.ResolvedMobilityPoolMember{
		spec.Members[0],
		{NodeRef: "aws-router-a", Site: "aws", Role: "cloud", Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10}},
		{NodeRef: "aws-router-b", Site: "aws", Role: "cloud", ProfileRef: "aws-edge", Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 20}},
		{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
		{NodeRef: "oci-router", Site: "oci", Role: "cloud"},
	}
	return spec
}

func canonicalJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}
	return string(data)
}

func pathBySourcePrefixOptional(paths []bgpdaemon.AppliedPath, source, prefix string) (bgpdaemon.AppliedPath, bool) {
	for _, path := range paths {
		if path.Source == source && path.Prefix == prefix {
			return path, true
		}
	}
	return bgpdaemon.AppliedPath{}, false
}

func staticPoolSpec() testMobilityPoolSpec {
	return testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
		{NodeRef: "onprem-router", Site: "onprem", Role: "onprem", StaticOwnedAddresses: []string{"10.88.60.10/32"}},
		{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
	},
	}
}

func staticRouter(nodeName string, spec testMobilityPoolSpec) *api.Router {
	poolSpec, nodeSet := localizeMobilityPoolSpecForNode(spec, nodeName)
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
				TypeMeta: nodeSet.TypeMeta,
				Metadata: nodeSet.Metadata,
				Spec:     nodeSet.Spec,
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     poolSpec,
			},
		}},
	}
}

func routeTableAssignPlan(poolName, provider, providerRef, nicRef, routeTableRef, address string, at time.Time) dynamicconfig.ActionPlan {
	pathSig := "prefix=" + normalizeAddressString(address) + ";test=wrong-local-udr"
	return dynamicconfig.ActionPlan{
		Name:        safeName("mobility-" + poolName + "-assign-" + address),
		Provider:    provider,
		ProviderRef: providerRef,
		Action:      actionAssignRouteTableRoute,
		Target: map[string]string{
			"address":         address,
			"provider":        provider,
			"providerRef":     providerRef,
			"nicRef":          nicRef,
			"routeTableRef":   routeTableRef,
			"captureStrategy": captureStrategyRouteTable,
		},
		Parameters: map[string]string{
			bgpPathSigParam:        pathSig,
			bgpTrapLastSeenAtParam: at.Format(time.RFC3339Nano),
			captureParamHolder:     "azure-router",
		},
	}
}

func localCaptureIntentForTest(intents []dynamicconfig.LocalCaptureIntent, address string) (dynamicconfig.LocalCaptureIntent, bool) {
	for _, intent := range intents {
		if intent.Address == address {
			return intent, true
		}
	}
	return dynamicconfig.LocalCaptureIntent{}, false
}

func decodeMobilityDataplanePlan(t *testing.T, raw string) dynamicconfig.MobilityDataplanePlan {
	t.Helper()
	var plan dynamicconfig.MobilityDataplanePlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatalf("decode mobility dataplane plan: %v raw=%s", err, raw)
	}
	return plan
}

func ownershipStatusDecisionByAddress(t *testing.T, decisions []map[string]any, address string) map[string]any {
	t.Helper()
	for _, decision := range decisions {
		if fmt.Sprint(decision["address"]) == address {
			return decision
		}
	}
	t.Fatalf("ownership decision %s not found in %#v", address, decisions)
	return nil
}

func ownershipStatusDecisions(t *testing.T, raw any) []map[string]any {
	t.Helper()
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			m, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("ownership status table item = %#v, want map[string]any", item)
			}
			out = append(out, m)
		}
		return out
	default:
		t.Fatalf("ownership status table = %#v, want slice", raw)
		return nil
	}
}

type mergeTrackingStore struct {
	*routerstate.SQLiteStore
	objectStatusCalls int
	mergeCalls        int
}

func (s *mergeTrackingStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	s.objectStatusCalls++
	return s.SQLiteStore.ObjectStatus(apiVersion, kind, name)
}

func (s *mergeTrackingStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	s.mergeCalls++
	return s.SQLiteStore.MergeObjectStatus(apiVersion, kind, name, updates)
}

func TestMobilityPoolStatusWritersUsePartialMerge(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store := &mergeTrackingStore{SQLiteStore: testStore(t, now)}
	planner := Controller{Store: store}
	discovery := DiscoveryController{Store: store}

	if err := planner.savePlannerStatus("cloudedge", map[string]any{
		"phase": "Planned",
	}); err != nil {
		t.Fatalf("savePlannerStatus: %v", err)
	}
	discovery.saveDiscoveryStatus("cloudedge", map[string]any{
		"discoveryPhase":    "Observed",
		"discoveryObserved": 1,
	})

	status := store.SQLiteStore.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Planned" || status["discoveryPhase"] != "Observed" {
		t.Fatalf("status = %#v", status)
	}
	if store.mergeCalls != 2 || store.objectStatusCalls != 0 {
		t.Fatalf("mergeCalls=%d objectStatusCalls=%d, want partial merge without read-modify-write", store.mergeCalls, store.objectStatusCalls)
	}
}

func TestControllerScopesEphemeralGracefulDrain(t *testing.T) {
	controller := Controller{
		ForceSelfDrainPools: map[string]bool{"handoff": true},
		ReconcilePools:      map[string]bool{"handoff": true},
	}
	if !controller.forceSelfDrainPool("handoff") || controller.forceSelfDrainPool("singleton") {
		t.Fatalf("force drain scope = %#v", controller.ForceSelfDrainPools)
	}
	if !controller.reconcilesPool("handoff") || controller.reconcilesPool("singleton") {
		t.Fatalf("reconcile scope = %#v", controller.ReconcilePools)
	}
}
