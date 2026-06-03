// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type fakeBGPPaths struct {
	paths   map[string]bgpdaemon.AppliedPath
	upserts []bgpdaemon.AppliedPath
	deletes []bgpdaemon.AppliedPath
}

func (f *fakeBGPPaths) ListPaths(_ context.Context, source string) ([]bgpdaemon.AppliedPath, error) {
	var out []bgpdaemon.AppliedPath
	for _, path := range f.paths {
		if path.Source == source {
			out = append(out, path)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out, nil
}

func (f *fakeBGPPaths) UpsertPath(_ context.Context, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedPath, error) {
	if f.paths == nil {
		f.paths = map[string]bgpdaemon.AppliedPath{}
	}
	path = bgpdaemon.NormalizeAppliedPath(path)
	key := bgpdaemon.AppliedPathKey(path)
	f.paths[key] = path
	f.upserts = append(f.upserts, path)
	return path, nil
}

func (f *fakeBGPPaths) DeletePath(_ context.Context, path bgpdaemon.AppliedPath) error {
	path = bgpdaemon.NormalizeAppliedPath(path)
	delete(f.paths, bgpdaemon.AppliedPathKey(path))
	f.deletes = append(f.deletes, path)
	return nil
}

func pathBySourcePrefix(t *testing.T, bgp *fakeBGPPaths, source, prefix string) bgpdaemon.AppliedPath {
	t.Helper()
	path, ok := maybePathBySourcePrefix(bgp, source, prefix)
	if !ok {
		t.Fatalf("BGP path %s %s not found; paths=%#v", source, prefix, bgp.paths)
	}
	return path
}

func maybePathBySourcePrefix(bgp *fakeBGPPaths, source, prefix string) (bgpdaemon.AppliedPath, bool) {
	if bgp == nil {
		return bgpdaemon.AppliedPath{}, false
	}
	key := bgpdaemon.AppliedPathKey(bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
		Source: source,
		Prefix: prefix,
		Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
	}))
	path, ok := bgp.paths[key]
	return path, ok
}

func TestControllerBGPModeAdvertisesSelfOwnedHostRouteAndSuppressesSAMPart(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	bgp := &fakeBGPPaths{paths: map[string]bgpdaemon.AppliedPath{
		bgpdaemon.AppliedPathKey(bgpdaemon.AppliedPath{Source: DynamicSource("cloudedge", "azure-router"), Prefix: "10.88.60.99/32"}): bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: DynamicSource("cloudedge", "azure-router"),
			Prefix: "10.88.60.99/32",
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
		}),
	}}
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-azure",
		Group:      "cloudedge",
		SourceNode: "azure-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	controller := Controller{Router: planningRouterForNode("azure-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(bgp.upserts) != 1 || bgp.upserts[0].Source != DynamicSource("cloudedge", "azure-router") || bgp.upserts[0].Prefix != "10.88.60.11/32" {
		t.Fatalf("upserts = %#v, want self-owned /32", bgp.upserts)
	}
	if bgp.upserts[0].Attrs.LocalPref != bgpMobilityLocalPrefBase+1 || !stringSliceContains(bgp.upserts[0].Attrs.Communities, bgpMobilityCommunityRoleCloud) || !stringSliceContains(bgp.upserts[0].Attrs.Communities, bgpMobilityCommunitySourceObserved) {
		t.Fatalf("upsert attrs = %#v, want cloud observed owner attributes", bgp.upserts[0].Attrs)
	}
	if len(bgp.deletes) != 1 || bgp.deletes[0].Prefix != "10.88.60.99/32" {
		t.Fatalf("deletes = %#v, want stale source path deleted", bgp.deletes)
	}
	part := latestPart(t, store, DynamicSource("cloudedge", "azure-router"))
	resources := decodeResources(t, part.ResourcesJSON)
	if len(resources) != 0 {
		t.Fatalf("BGP mode dynamic resources = %#v, want empty SAM part", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "BGPPlanned" || status["deliveryMode"] != "bgp" || fmt.Sprint(status["generatedBGPPaths"]) != "1" {
		t.Fatalf("BGP status = %#v", status)
	}
}

func TestControllerBGPModeProviderDiscoveryAdvertisesOnlyFreshInventoryOwnedAddresses(t *testing.T) {
	now := time.Date(2026, 6, 3, 16, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	source := DynamicSource("cloudedge", "azure-router")
	for _, address := range []string{"10.88.60.11/32", "10.88.60.12/32"} {
		recordEvent(t, store, routerstate.EventRecord{
			ID:         "evt-" + strings.ReplaceAll(address, "/", "-"),
			Group:      "cloudedge",
			SourceNode: "azure-router",
			Type:       ObservedEventType,
			Subject:    address,
			ObservedAt: now.Add(-time.Minute),
			ExpiresAt:  now.Add(time.Hour),
			Payload: map[string]string{
				"address": address,
				"pool":    "cloudedge",
				"source":  providerDiscoverySource,
				"nicRef":  "client-nic",
			},
		})
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: planningRouterForNode("azure-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile without fresh discovery status: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, source, "10.88.60.11/32"); ok {
		t.Fatalf("paths = %#v, want provider-discovery self-origin held until fresh inventory status", bgp.paths)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryOwnedAddresses": []string{"10.88.60.11/32"},
		"discoverySelfPrivateIPs": []string{"10.88.60.21"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	controller.Now = func() time.Time { return now.Add(time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile with fresh discovery status: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, source, "10.88.60.11/32"); !ok {
		t.Fatalf("paths = %#v, want fresh inventory-backed provider-discovery owner advertised", bgp.paths)
	}
	if _, ok := maybePathBySourcePrefix(bgp, source, "10.88.60.12/32"); ok {
		t.Fatalf("paths = %#v, want stale provider-discovery owner not advertised", bgp.paths)
	}
}

func TestControllerBGPModeProviderDiscoveryDoesNotAdvertiseRouterNICTrapAsOwner(t *testing.T) {
	now := time.Date(2026, 6, 3, 16, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-router-nic-trap",
		Group:      "cloudedge",
		SourceNode: "azure-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.12/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
		Payload: map[string]string{
			"address": "10.88.60.12/32",
			"pool":    "cloudedge",
			"source":  providerDiscoverySource,
			"nicRef":  "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
		},
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryOwnedAddresses": []string{"10.88.60.12/32"},
		"discoverySelfPrivateIPs": []string{"10.88.60.12"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: planningRouterForNode("azure-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "azure-router"), "10.88.60.12/32"); ok {
		t.Fatalf("paths = %#v, want router-NIC trap excluded from self-origin ownership", bgp.paths)
	}
}

func TestControllerBGPModeDrainWithdrawsLocalPathWithoutOwnershipEpoch(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := centralizedOwnershipPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[1].Maintenance.Drain = true
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-a",
		Group:      "cloudedge",
		SourceNode: "azure-router-a",
		Type:       ObservedEventType,
		Subject:    "10.88.60.12/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	aSource := DynamicSource("cloudedge", "azure-router-a")
	bgp := &fakeBGPPaths{paths: map[string]bgpdaemon.AppliedPath{
		bgpdaemon.AppliedPathKey(bgpdaemon.AppliedPath{Source: aSource, Prefix: "10.88.60.12/32"}): bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: aSource,
			Prefix: "10.88.60.12/32",
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
			Attrs:  bgpdaemon.AppliedPathAttrs{LocalPref: bgpMobilityLocalPrefBase + 1},
		}),
	}}

	controllerA := Controller{Router: planningRouterForNode("azure-router-a", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("old owner Reconcile: %v", err)
	}
	if len(bgp.deletes) != 1 || bgp.deletes[0].Source != aSource || bgp.deletes[0].Prefix != "10.88.60.12/32" {
		t.Fatalf("old owner deletes = %#v, want withdraw", bgp.deletes)
	}
}

func TestControllerBGPModeDoesNotUseHeartbeatLivenessForOwnership(t *testing.T) {
	base := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{
		Type:         "centralized",
		AutoFailover: true,
	}
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "hb-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       "routerd.mobility.ignored-liveness",
		Subject:    "mobility/cloudedge/aws-router-a",
		Payload:    map[string]string{"pool": "cloudedge", "node": "aws-router-a"},
		ObservedAt: base,
		ExpiresAt:  base.Add(time.Hour),
	})

	now := base.Add(2 * time.Minute)
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: planningRouterForNode("aws-router-b", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(bgp.upserts) != 0 {
		t.Fatalf("upserts = %#v, want no heartbeat-driven standby advertise in BGP clean mode", bgp.upserts)
	}
}

func TestControllerBGPModeProviderActionFailureDoesNotRemoveBGPPath(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-azure",
		Group:      "cloudedge",
		SourceNode: "azure-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-onprem",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.10/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	saveBGPInstalledNextHops(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}})
	bgp := &fakeBGPPaths{}
	source := DynamicSource("cloudedge", "azure-router")
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	part := latestPart(t, store, source)
	resources := decodeResources(t, part.ResourcesJSON)
	if len(resources) != 0 {
		t.Fatalf("BGP mode resources = %#v, want no SAM resources", resources)
	}
	plans := decodeActionPlans(t, part.ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("actionPlans = %#v, want remote-owned background provider trap", plans)
	}
	if findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("actionPlans = %#v, want no self-owned provider assign", plans)
	}
	if assign.Parameters[bgpPathSigParam] == "" {
		t.Fatalf("assign parameters = %#v, want BGP path fence without epoch fences", assign.Parameters)
	}
	if _, err := importApprovedAction(t, assign, source, store, now); err != nil {
		t.Fatalf("import action: %v", err)
	}
	rows, err := store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		t.Fatalf("ListActions: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("imported action not found")
	}
	if err := store.MarkActionResult(rows[0].ID, routerstate.ActionFailed, "failed", "provider API unavailable", nil, now.Add(time.Second)); err != nil {
		t.Fatalf("MarkActionResult failed: %v", err)
	}

	bgp.upserts = nil
	controller.Now = func() time.Time { return now.Add(2 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if len(bgp.upserts) != 1 || bgp.upserts[0].Prefix != "10.88.60.11/32" {
		t.Fatalf("BGP upserts after failed provider action = %#v, want route retained", bgp.upserts)
	}
	part = latestPart(t, store, source)
	if findActionPlanByAddress(decodeActionPlans(t, part.ActionPlansJSON), "assign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatalf("actionPlans after failure = %s, want desired provider assign retained", part.ActionPlansJSON)
	}
}

func TestControllerBGPModeUsesDiscoveredSelfNICForProviderActions(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[1].Capture.NICRef = ""
	spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", ProviderRef: "azure-provider", SubnetRef: "/subnets/demo"}
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-onprem",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.10/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	saveBGPInstalledNextHops(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}})
	bgp := &fakeBGPPaths{}
	source := DynamicSource("cloudedge", "azure-router")
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("unresolved Reconcile: %v", err)
	}
	part := latestPart(t, store, source)
	if plans := decodeActionPlans(t, part.ActionPlansJSON); len(plans) != 0 {
		t.Fatalf("unresolved plans = %#v, want no provider actions", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "Degraded" || !strings.Contains(fmt.Sprint(status["plannerReason"]), "self NIC is unresolved") {
		t.Fatalf("status = %#v, want unresolved self NIC degraded", status)
	}
	if len(bgp.upserts) != 0 {
		t.Fatalf("bgp upserts = %#v, want no self-owned path for remote owner", bgp.upserts)
	}

	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfNICRef":    "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/resolved-router-nic",
		"discoverySelfSubnetRef": "/subnets/demo",
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	controller.Now = func() time.Time { return now.Add(time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("resolved Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("resolved plans = %#v status=%#v, want provider assign", plans, store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge"))
	}
	if assign.Target["nicRef"] != "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/resolved-router-nic" {
		t.Fatalf("assign target = %#v, want discovered nicRef", assign.Target)
	}
}

func TestControllerBGPModeProviderStateFollowsBestPathOwnerChange(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := centralizedOwnershipPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	saveBGPInstalledNextHops(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}})
	bgp := &fakeBGPPaths{}
	sourceA := DynamicSource("cloudedge", "azure-router-a")
	sourceB := DynamicSource("cloudedge", "azure-router-b")
	controllerA := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial router-a Reconcile: %v", err)
	}
	initialPlans := decodeActionPlans(t, latestPart(t, store, sourceA).ActionPlansJSON)
	if findActionPlanByAddress(initialPlans, "assign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatalf("initial plans = %#v, want router-a assign", initialPlans)
	}

	spec.Members[1].Maintenance.Drain = true
	controllerA.Router = routerWithBGPRouter(planningRouterForNode("azure-router-a", spec))
	controllerA.Now = func() time.Time { return now.Add(time.Second) }
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("drained router-a Reconcile: %v", err)
	}
	drainedA := decodeActionPlans(t, latestPart(t, store, sourceA).ActionPlansJSON)
	if findActionPlanByAddress(drainedA, "unassign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatalf("drained router-a plans = %#v, want background unassign", drainedA)
	}
	if findActionPlanByAddress(drainedA, "assign-secondary-ip", "10.88.60.10/32") != nil {
		t.Fatalf("drained router-a plans = %#v, want no assign", drainedA)
	}

	controllerB := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router-b", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(2 * time.Second) }}
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("router-b Reconcile: %v", err)
	}
	standbyPlans := decodeActionPlans(t, latestPart(t, store, sourceB).ActionPlansJSON)
	assignB := findActionPlanByAddress(standbyPlans, "assign-secondary-ip", "10.88.60.10/32")
	if assignB == nil {
		t.Fatalf("router-b plans = %#v, want background assign", standbyPlans)
	}
	if assignB.Parameters[bgpPathSigParam] == "" || assignB.Parameters[captureParamHolder] != "azure-router-b" {
		t.Fatalf("router-b assign parameters = %#v, want path-fenced trap to active placement holder", assignB.Parameters)
	}
}

func TestControllerBGPModeProviderTrapUsesRemoteInstalledNextHops(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-aws-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-aws-a-stale-oci",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       ObservedEventType,
		Subject:    "10.88.60.13/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
		Payload:    map[string]string{"source": providerDiscoverySource, "pool": "cloudedge"},
	})
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.2"},
		"10.88.60.12/32": {"10.99.0.3"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, nil, map[string]string{bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.0.2/32"})
	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(planningRouterForNode("aws-router-a", spec))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	for _, address := range []string{"10.88.60.10/32", "10.88.60.12/32", "10.88.60.13/32"} {
		assign := findActionPlanByAddress(plans, "assign-secondary-ip", address)
		if assign == nil {
			t.Fatalf("plans = %#v, want remote trap assign for %s", plans, address)
		}
		if assign.Parameters[captureParamHolder] != "aws-router-a" {
			t.Fatalf("assign %s parameters = %#v, want trap holder aws-router-a", address, assign.Parameters)
		}
		if assign.Parameters[bgpPathSigParam] == "" {
			t.Fatalf("assign %s parameters = %#v, want BGP path fence without capture epoch", address, assign.Parameters)
		}
	}
	if findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, want no same-site/self-owned trap assign", plans)
	}
}

func TestControllerBGPModeProviderTrapExcludesFreshOwnedAddressAndDeprovisionsStickySelfTrap(t *testing.T) {
	now := time.Date(2026, 6, 3, 16, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	source := DynamicSource("cloudedge", "azure-router")
	previousPlans, err := json.Marshal([]dynamicconfig.ActionPlan{{
		Name:        "mobility-cloudedge-assign-10-88-60-11-32",
		Provider:    "azure",
		ProviderRef: "azure-provider",
		Action:      "assign-secondary-ip",
		Target: map[string]string{
			"address":     "10.88.60.11/32",
			"nicRef":      "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
			"provider":    "azure",
			"providerRef": "azure-provider",
			"region":      "japaneast",
		},
		Parameters: map[string]string{
			bgpPathSigParam:        "prefix=10.88.60.11/32;nextHops=10.99.0.2",
			bgpTrapLastSeenAtParam: now.Add(-time.Minute).Format(time.RFC3339Nano),
		},
	}})
	if err != nil {
		t.Fatalf("marshal previous plans: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:          source,
		Generation:      dynamicGeneration,
		ObservedAt:      now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		ActionPlansJSON: string(previousPlans),
		Status:          "active",
	}); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryOwnedAddresses": []string{"10.88.60.11/32"},
		"discoverySelfPrivateIPs": []string{"10.88.60.4"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus(MobilityPool/cloudedge): %v", err)
	}
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.2"},
	}, nil, map[string]string{bgpstate.MobilityNodeIdentityCommunity("azure-router"): "10.99.0.3/32"})

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	if findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, want no trap assign for fresh-owned self address despite transient remote next-hop", plans)
	}
	if findActionPlanByAddress(plans, "unassign-secondary-ip", "10.88.60.11/32") == nil {
		t.Fatalf("plans = %#v, want sticky self-trap deprovisioned", plans)
	}
}

func TestControllerBGPModeReappliesForwardingWhenProviderObservedDisabled(t *testing.T) {
	now := time.Date(2026, 6, 3, 14, 20, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.3"},
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfNICRef":            "eni-a",
		"discoverySelfSubnetRef":         "subnet-a",
		"discoverySelfPrivateIPs":        []string{"10.88.60.11"},
		"discoverySelfForwardingEnabled": false,
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	var forwarding *dynamicconfig.ActionPlan
	for i := range plans {
		if plans[i].Action == "ensure-forwarding-enabled" {
			forwarding = &plans[i]
			break
		}
	}
	if forwarding == nil {
		t.Fatalf("plans = %#v, want ensure-forwarding-enabled", plans)
	}
	if !strings.Contains(forwarding.IdempotencyKey, ":forwarding-drift:") || forwarding.Parameters["mobilityForwardingDrift"] == "" {
		t.Fatalf("forwarding plan = %#v, want provider-observed drift fence", forwarding)
	}
}

func TestControllerBGPModeAdvertisesSelfLivenessMarker(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(routerWithEventGroupListen(planningRouterForNode("aws-router-a", spec), "10.99.0.2"))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	marker := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-a"), "10.99.0.2/32")
	if !stringSliceContains(marker.Attrs.Communities, bgpstate.MobilityCommunityNodeLiveness) ||
		!stringSliceContains(marker.Attrs.Communities, bgpstate.MobilityNodeIdentityCommunity("aws-router-a")) {
		t.Fatalf("marker attrs = %#v, want liveness + node identity communities", marker.Attrs)
	}
}

func TestControllerBGPModeAdvertisesCanonicalSelfLivenessMarker(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(routerWithEventGroupListen(planningRouterForNode("Node/aws-router-a", spec), "10.99.0.2"))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	marker := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-a"), "10.99.0.2/32")
	if !stringSliceContains(marker.Attrs.Communities, bgpstate.MobilityNodeIdentityCommunity("aws-router-a")) {
		t.Fatalf("marker attrs = %#v, want canonical node identity community", marker.Attrs)
	}
	if stringSliceContains(marker.Attrs.Communities, bgpstate.MobilityNodeIdentityCommunity("Node/aws-router-a")) {
		t.Fatalf("marker attrs = %#v, want no raw non-canonical node identity community", marker.Attrs)
	}
}

func TestControllerBGPModeAdvertisesMemberNodeIdentityWhenEventGroupUsesSiteAlias(t *testing.T) {
	now := time.Date(2026, 6, 3, 10, 43, 4, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	for i := range spec.Members {
		switch spec.Members[i].NodeRef {
		case "azure-router":
			spec.Members[i].NodeRef = "azure-router-a"
		}
	}
	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(routerWithEventGroupListen(planningRouterForNode("azure-router", spec), "10.99.0.3"))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	marker := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "azure-router-a"), "10.99.0.3/32")
	if !stringSliceContains(marker.Attrs.Communities, bgpstate.MobilityNodeIdentityCommunity("azure-router-a")) {
		t.Fatalf("marker attrs = %#v, want member nodeRef identity community", marker.Attrs)
	}
	if stringSliceContains(marker.Attrs.Communities, bgpstate.MobilityNodeIdentityCommunity("azure-router")) {
		t.Fatalf("marker attrs = %#v, want no EventGroup alias identity community", marker.Attrs)
	}
}

func TestControllerBGPModeStandbyDefersTrapWhenActiveLivenessMarkerPresent(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-aws-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.2", "10.99.0.5"},
		"10.88.60.12/32": {"10.99.0.3"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, []map[string]any{}, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.0.2/32",
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	})
	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(planningRouterForNode("aws-router-b", spec))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-b")).ActionPlansJSON)
	if findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.10/32") != nil ||
		findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.12/32") != nil ||
		findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.13/32") != nil {
		t.Fatalf("standby plans = %#v, want no provider traps while active liveness marker is present", plans)
	}
}

func TestControllerBGPModeStandbySeizesTrapWhenActiveLivenessMarkerWithdrawn(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-aws-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.5"},
		"10.88.60.12/32": {"10.99.0.3"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, []map[string]any{}, map[string]string{bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32"})
	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(planningRouterForNode("aws-router-b", spec))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-b")).ActionPlansJSON)
	for _, address := range []string{"10.88.60.10/32", "10.88.60.12/32", "10.88.60.13/32"} {
		assign := findActionPlanByAddress(plans, "assign-secondary-ip", address)
		if assign == nil {
			t.Fatalf("plans = %#v, want standby seize assign for %s", plans, address)
		}
		if assign.Parameters["allowReassignment"] != "true" {
			t.Fatalf("assign %s parameters = %#v, want allowReassignment for liveness-driven seize", address, assign.Parameters)
		}
		if assign.Parameters[captureParamHolder] != "aws-router-b" || assign.Parameters[bgpPathSigParam] == "" {
			t.Fatalf("assign %s parameters = %#v, want path-fenced holder aws-router-b", address, assign.Parameters)
		}
	}
	if findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, want no same-site self-owned trap despite standby .11 path", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	election, ok := status["bgpCaptureElection"].(map[string]any)
	if !ok {
		t.Fatalf("bgpCaptureElection = %#v, want map status", status["bgpCaptureElection"])
	}
	if election["seize"] != true || election["selfMarkerPresent"] != true || election["activeMarkerPresent"] != false {
		t.Fatalf("bgpCaptureElection = %#v, want self marker present, active marker absent, seize", election)
	}
	if election["selfCommunity"] != bgpstate.MobilityNodeIdentityCommunity("aws-router-b") ||
		election["activeCommunity"] != bgpstate.MobilityNodeIdentityCommunity("aws-router-a") {
		t.Fatalf("bgpCaptureElection communities = %#v, want aws-b self and aws-a active", election)
	}
}

func TestControllerBGPModeBG24RuntimeSeizesWhenAWSActiveMarkerAbsent(t *testing.T) {
	now := time.Date(2026, 6, 3, 10, 43, 4, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-aws-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
	})
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.5"},
		"10.88.60.12/32": {"10.99.0.3"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, []map[string]any{}, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("onprem-router"):  "10.99.0.1/32",
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"):   "10.99.0.5/32",
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
		bgpstate.MobilityNodeIdentityCommunity("azure-router"):   "10.99.0.3/32",
		bgpstate.MobilityNodeIdentityCommunity("oci-router"):     "10.99.0.4/32",
	})
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-b", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-b")).ActionPlansJSON)
	for _, address := range []string{"10.88.60.10/32", "10.88.60.12/32", "10.88.60.13/32"} {
		assign := findActionPlanByAddress(plans, "assign-secondary-ip", address)
		if assign == nil {
			t.Fatalf("plans = %#v, want BG24 standby seize assign for %s", plans, address)
		}
		if assign.Parameters["allowReassignment"] != "true" {
			t.Fatalf("assign %s parameters = %#v, want allowReassignment", address, assign.Parameters)
		}
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	election, ok := status["bgpCaptureElection"].(map[string]any)
	if !ok || election["seize"] != true || election["selfCommunity"] != bgpstate.MobilityNodeIdentityCommunity("aws-router-b") ||
		election["activeCommunity"] != bgpstate.MobilityNodeIdentityCommunity("aws-router-a") || election["activeMarkerPresent"] != false {
		t.Fatalf("bgpCaptureElection = %#v, want BG24 aws-b seize with aws-a marker absent", status["bgpCaptureElection"])
	}
}

func TestControllerBGPModeRestoreKeepsOwnerPreferredOverStandby(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-aws-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.11/32": {"10.99.0.22"},
	})
	bgp := &fakeBGPPaths{}

	controllerA := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial router-a Reconcile: %v", err)
	}
	controllerB := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-b", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial router-b Reconcile: %v", err)
	}
	aPath := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-a"), "10.88.60.11/32")
	bPath := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.11/32")
	if aPath.Attrs.LocalPref <= bPath.Attrs.LocalPref {
		t.Fatalf("initial localPref A=%d B=%d, want active A preferred over standby B", aPath.Attrs.LocalPref, bPath.Attrs.LocalPref)
	}
	if findActionPlanByAddress(decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-b")).ActionPlansJSON), "assign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("standby B generated self-site trap for .11")
	}

	drained := awsFailoverPoolSpec()
	drained.DeliveryPolicy.Mode = "bgp"
	drained.Members[1].Maintenance.Drain = true
	controllerA.Router = routerWithBGPRouter(planningRouterForNode("aws-router-a", drained))
	controllerA.Now = func() time.Time { return now.Add(2 * time.Second) }
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("drained router-a Reconcile: %v", err)
	}
	controllerB.Router = routerWithBGPRouter(planningRouterForNode("aws-router-b", drained))
	controllerB.Now = func() time.Time { return now.Add(3 * time.Second) }
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("takeover router-b Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "aws-router-a"), "10.88.60.11/32"); ok {
		t.Fatalf("drained router-a path still present")
	}
	bTakeover := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.11/32")
	if bTakeover.Attrs.LocalPref != bgpMobilityLocalPrefBase+1 {
		t.Fatalf("takeover B localPref = %d, want active high", bTakeover.Attrs.LocalPref)
	}
	if findActionPlanByAddress(decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-b")).ActionPlansJSON), "assign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("active B generated provider trap for same-site .11")
	}

	restored := awsFailoverPoolSpec()
	restored.DeliveryPolicy.Mode = "bgp"
	controllerA.Router = routerWithBGPRouter(planningRouterForNode("aws-router-a", restored))
	controllerA.Now = func() time.Time { return now.Add(4 * time.Second) }
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("restored router-a Reconcile: %v", err)
	}
	controllerB.Router = routerWithBGPRouter(planningRouterForNode("aws-router-b", restored))
	controllerB.Now = func() time.Time { return now.Add(5 * time.Second) }
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("restored router-b Reconcile: %v", err)
	}
	aRestored := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-a"), "10.88.60.11/32")
	bRestored := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.11/32")
	if aRestored.Attrs.LocalPref != bgpMobilityLocalPrefBase+1 || bRestored.Attrs.LocalPref != bgpMobilityLocalPrefBase || aRestored.Attrs.LocalPref <= bRestored.Attrs.LocalPref {
		t.Fatalf("restored localPref A=%d B=%d, want A high and B standby low", aRestored.Attrs.LocalPref, bRestored.Attrs.LocalPref)
	}
	if bRestored.Attrs.MED != 20 {
		t.Fatalf("restored B MED = %d, want placement priority 20", bRestored.Attrs.MED)
	}
	if findActionPlanByAddress(decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-b")).ActionPlansJSON), "assign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("restored standby B retained provider trap for .11")
	}
}

func TestControllerBGPModeProviderTrapRecapturesAfterSuccessfulRelease(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	for _, address := range []string{"10.88.60.12/32", "10.88.60.13/32"} {
		seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-3*time.Minute))
		seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "unassign-secondary-ip", 1, now.Add(-2*time.Minute))
	}
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.3"},
		"10.88.60.13/32": {"10.99.0.4"},
	})

	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(planningRouterForNode("aws-router-a", spec))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	for _, address := range []string{"10.88.60.12/32", "10.88.60.13/32"} {
		assign := findActionPlanByAddress(plans, "assign-secondary-ip", address)
		if assign == nil {
			t.Fatalf("plans = %#v, want recapture assign for %s", plans, address)
		}
		if assign.Parameters[bgpPathSigParam] == "" {
			t.Fatalf("assign %s parameters = %#v, want BGP path fence after release", address, assign.Parameters)
		}
		if assign.Parameters["allowReassignment"] != "true" {
			t.Fatalf("assign %s parameters = %#v, want reassignment after successful release", address, assign.Parameters)
		}
		if !strings.Contains(assign.IdempotencyKey, ":transition:after-unassign-") || assign.Parameters[bgpTrapTransitionParam] == "" {
			t.Fatalf("assign %s key/parameters = %q %#v, want transition-fenced recapture after unassign", address, assign.IdempotencyKey, assign.Parameters)
		}
	}
}

func TestControllerBGPModeProviderTrapRecapturesWhenObservedProviderStateLost(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", "10.88.60.10/32", "assign-secondary-ip", 1, now.Add(-3*time.Minute))
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs": []string{"10.88.60.11"},
		"discoveryLastScanAt":     now.Add(-time.Second).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus(MobilityPool/cloudedge): %v", err)
	}

	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(planningRouterForNode("aws-router-a", spec))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want recapture assign for provider-observed missing trap", plans)
	}
	if assign.Parameters[bgpPathSigParam] == "" {
		t.Fatalf("assign parameters = %#v, want BGP path fence after provider-observed loss", assign.Parameters)
	}
	if assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign parameters = %#v, want reassignment after provider-observed loss", assign.Parameters)
	}
	if assign.Parameters[captureParamHolder] != "aws-router-a" {
		t.Fatalf("assign parameters = %#v, want aws-router-a holder", assign.Parameters)
	}

	controller.Now = func() time.Time { return now.Add(time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	secondPlans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	secondAssign := findActionPlanByAddress(secondPlans, "assign-secondary-ip", "10.88.60.10/32")
	if secondAssign == nil {
		t.Fatalf("second plans = %#v, want recapture assign retained", secondPlans)
	}
	if secondAssign.Parameters[bgpPathSigParam] == "" {
		t.Fatalf("second assign parameters = %#v, want pending path fence retained", secondAssign.Parameters)
	}
}

func TestControllerBGPModeProviderTrapUsesStaticOwnedOwnerWhenOwnershipMissing(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
	saveBGPInstalledNextHops(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}})

	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(planningRouterForNode("azure-router", spec))
	res := mobilityPoolResource(t, router, "cloudedge")
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.reconcileBGPDelivery(context.Background(), res, spec, now); err != nil {
		t.Fatalf("reconcileBGPDelivery: %v", err)
	}

	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "azure-router")).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want static-owned onprem trap assign without ownership row", plans)
	}
	if assign.Parameters[bgpPathSigParam] == "" || assign.Parameters[captureParamHolder] != "azure-router" {
		t.Fatalf("assign parameters = %#v, want BGP path fence for static-owned trap", assign.Parameters)
	}
}

func TestControllerBGPModeProviderTrapRIBStartupIsConservative(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	router := routerWithBGPRouter(planningRouterForNode("azure-router", spec))
	saveBGPInstalledNextHops(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}})
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	source := DynamicSource("cloudedge", "azure-router")
	if findActionPlanByAddress(decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON), "assign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatal("initial remote trap assign not generated")
	}

	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{"phase": "Starting"}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	controller.Now = func() time.Time { return now.Add(time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("unobserved RIB Reconcile: %v", err)
	}
	unobservedPlans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	if findActionPlanByAddress(unobservedPlans, "unassign-secondary-ip", "10.88.60.10/32") != nil {
		t.Fatalf("unobserved plans = %#v, want conservative hold without unassign", unobservedPlans)
	}
	if findActionPlanByAddress(unobservedPlans, "assign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatalf("unobserved plans = %#v, want previous trap carried forward", unobservedPlans)
	}

	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{"installedNextHops": map[string]any{}}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	controller.Now = func() time.Time { return now.Add(2 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("observed empty RIB Reconcile: %v", err)
	}
	emptyPlans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	if findActionPlanByAddress(emptyPlans, "unassign-secondary-ip", "10.88.60.10/32") != nil {
		t.Fatalf("observed empty plans = %#v, want short RIB gap held without unassign", emptyPlans)
	}
	if findActionPlanByAddress(emptyPlans, "assign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatalf("observed empty plans = %#v, want previous trap carried through short RIB gap", emptyPlans)
	}

	controller.Now = func() time.Time { return now.Add(bgpTrapRIBMissingHold + time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("sustained empty RIB Reconcile: %v", err)
	}
	stalePlans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	if findActionPlanByAddress(stalePlans, "unassign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatalf("sustained empty plans = %#v, want stale trap unassign", stalePlans)
	}
}

func TestControllerBGPModeDeprovisionRegeneratesFromActionJournal(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", "10.88.60.10/32", "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPInstalledNextHops(t, store, map[string][]string{})

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	unassign := findActionPlanByAddress(plans, "unassign-secondary-ip", "10.88.60.10/32")
	if unassign == nil {
		t.Fatalf("plans = %#v, want unassign regenerated from succeeded assign journal", plans)
	}
	if unassign.Parameters[bgpPathSigParam] == "" {
		t.Fatalf("unassign parameters = %#v, want path fence without capture epoch", unassign.Parameters)
	}
}

func TestControllerBGPModeStaticOwnedAdvertisesOnPremOwner(t *testing.T) {
	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(bgp.upserts) != 1 || bgp.upserts[0].Prefix != "10.88.60.10/32" || bgp.upserts[0].Source != DynamicSource("cloudedge", "onprem-router") {
		t.Fatalf("upserts = %#v, want static-owned onprem /32", bgp.upserts)
	}
	if !stringSliceContains(bgp.upserts[0].Attrs.Communities, bgpMobilityCommunityRoleOnPrem) || !stringSliceContains(bgp.upserts[0].Attrs.Communities, bgpMobilityCommunitySourceStatic) {
		t.Fatalf("attrs = %#v, want onprem static communities", bgp.upserts[0].Attrs)
	}
}

func TestControllerBGPModeStaticHandoverSwitchesAdvertisementSource(t *testing.T) {
	base := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	spec := staticPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	bgp := &fakeBGPPaths{}
	onpremSource := DynamicSource("cloudedge", "onprem-router")
	azureSource := DynamicSource("cloudedge", "azure-router")

	controller := Controller{Router: staticRouter("onprem-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return base }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	if len(bgp.upserts) != 1 || bgp.upserts[0].Source != onpremSource || bgp.upserts[0].Prefix != "10.88.60.10/32" {
		t.Fatalf("initial upserts = %#v, want onprem advertise", bgp.upserts)
	}

	spec.Members[0].StaticOwnedAddresses = nil
	spec.StaticHandovers = []api.MobilityStaticHandover{{Address: "10.88.60.10/32", FromNodeRef: "onprem-router", ToNodeRef: "azure-router"}}
	controller.Router = staticRouter("onprem-router", spec)
	controller.Now = func() time.Time { return base.Add(time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("release Reconcile: %v", err)
	}
	if len(bgp.deletes) != 1 || bgp.deletes[0].Source != onpremSource || bgp.deletes[0].Prefix != "10.88.60.10/32" {
		t.Fatalf("handover deletes = %#v, want onprem withdraw", bgp.deletes)
	}

	controller.Router = staticRouter("azure-router", spec)
	controller.Now = func() time.Time { return base.Add(time.Minute + 31*time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("cloud handover Reconcile: %v", err)
	}
	if len(bgp.upserts) != 2 || bgp.upserts[1].Source != azureSource || bgp.upserts[1].Prefix != "10.88.60.10/32" {
		t.Fatalf("handover upserts = %#v, want azure advertise after release", bgp.upserts)
	}
	if !stringSliceContains(bgp.upserts[1].Attrs.Communities, bgpMobilityCommunitySourceHandover) {
		t.Fatalf("handover attrs = %#v, want handover source community", bgp.upserts[1].Attrs)
	}
}

func TestControllerRouteModeIsRejectedByMainlinePlanner(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "route"
	source := DynamicSource("cloudedge", "azure-router")
	bgp := &fakeBGPPaths{paths: map[string]bgpdaemon.AppliedPath{
		bgpdaemon.AppliedPathKey(bgpdaemon.AppliedPath{Source: source, Prefix: "10.88.60.10/32"}): bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: source,
			Prefix: "10.88.60.10/32",
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
		}),
	}}
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-onprem",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.10/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	controller := Controller{Router: planningRouterForNode("azure-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(bgp.upserts) != 0 || len(bgp.deletes) != 0 {
		t.Fatalf("route mode BGP mutations = upserts:%#v deletes:%#v, want none", bgp.upserts, bgp.deletes)
	}
	parts, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("route mode generated parts = %+v, want none", parts)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "Degraded" || !strings.Contains(fmt.Sprint(status["plannerReason"]), "deliveryPolicy.mode=route is no longer supported") {
		t.Fatalf("route mode status = %#v", status)
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

func testRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec: api.MobilityPoolSpec{
				Prefix:   "10.88.60.0/24",
				GroupRef: "cloudedge",
				Members: []api.MobilityPoolMember{
					{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
					{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
				},
			},
		}}},
	}
}

func staticPoolSpec() api.MobilityPoolSpec {
	return api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem", StaticOwnedAddresses: []string{"10.88.60.10/32"}},
			{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
		},
	}
}

func staticRouter(nodeName string, spec api.MobilityPoolSpec) *api.Router {
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
		}},
	}
}
