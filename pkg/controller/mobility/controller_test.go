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
	"github.com/imksoo/routerd/pkg/providerinventory"
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

func nonLivenessUpserts(paths []bgpdaemon.AppliedPath) []bgpdaemon.AppliedPath {
	var out []bgpdaemon.AppliedPath
	for _, path := range paths {
		if stringSliceContains(path.Attrs.Communities, bgpstate.MobilityCommunityNodeLiveness) {
			continue
		}
		out = append(out, path)
	}
	return out
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
	if status["phase"] != "Ready" || status["plannerPhase"] != "BGPPlanned" || status["deliveryMode"] != "bgp" || fmt.Sprint(status["generatedBGPPaths"]) != "1" {
		t.Fatalf("BGP status = %#v", status)
	}
}

func TestControllerBGPModeOnPremL2WaitsForLocalOwnershipObservation(t *testing.T) {
	now := time.Date(2026, 6, 24, 16, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
		Mode: "onprem-l2",
		Sources: []api.MobilityOwnershipDiscoverySource{
			{Type: OnPremSourceARPObserver, Interface: "lan"},
		},
	}
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: planningRouterForNode("onprem-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" || status["plannerPhase"] != "Pending" || status["ownershipResolverPhase"] != "Pending" {
		t.Fatalf("status = %#v, want Pending until onprem-l2 observes a local owner", status)
	}
	if !strings.Contains(fmt.Sprint(status["plannerReason"]), "onprem-l2 ownership discovery") {
		t.Fatalf("plannerReason = %#v, want onprem-l2 discovery pending reason", status["plannerReason"])
	}
}

func TestControllerBGPModeOnPremL2DiscoveryWarmupKeepsPoolPending(t *testing.T) {
	now := time.Date(2026, 6, 25, 3, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
		Mode: "onprem-l2",
		Sources: []api.MobilityOwnershipDiscoverySource{
			{Type: OnPremSourceARPObserver, Interface: "lan"},
		},
	}
	observation := onPremObservation{
		Action:     "observed",
		Address:    "10.88.60.15",
		MAC:        "02:00:00:00:00:15",
		Interface:  "lan",
		SourceType: OnPremSourceARPObserver,
		ObservedAt: now.Add(-5 * time.Second),
	}
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "onprem-router", "10.88.60.15/32", observation, now.Add(-5*time.Second), 2*time.Minute))
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryPhase":    "Observed",
		"discoveryMode":     "onprem-l2",
		"discoveryObserved": 1,
		"discoveryArmedAt":  now.Add(-5 * time.Second).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: planningRouterForNode("onprem-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" || status["plannerPhase"] != "Pending" || status["ownershipResolverPhase"] != "Pending" {
		t.Fatalf("status = %#v, want Pending during onprem-l2 discovery warmup", status)
	}
	if !strings.Contains(fmt.Sprint(status["plannerReason"]), "warming up") {
		t.Fatalf("plannerReason = %#v, want warmup reason", status["plannerReason"])
	}

	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryPhase":    "Observed",
		"discoveryMode":     "onprem-l2",
		"discoveryObserved": 1,
		"discoveryArmedAt":  now.Add(-onPremL2DiscoveryWarmup - time.Second).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	bgp = &fakeBGPPaths{}
	controller = Controller{Router: planningRouterForNode("onprem-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile after warmup: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Ready" || status["plannerPhase"] != "BGPPlanned" {
		t.Fatalf("status = %#v, want Ready after onprem-l2 discovery warmup", status)
	}
}

func TestControllerBGPModeOnPremL2AllowsFreshEmptyCompleteDiscovery(t *testing.T) {
	now := time.Date(2026, 6, 26, 4, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
		Mode:            "onprem-l2",
		AllowEmptyAfter: "5s",
		Sources: []api.MobilityOwnershipDiscoverySource{
			{Type: OnPremSourceARPObserver, Interface: "lan"},
		},
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryPhase":           "Complete",
		"discoveryMode":            "onprem-l2",
		"discoveryObserved":        0,
		"discoveryResultCount":     0,
		"discoveryAuthoritative":   false,
		"discoveryAllowEmptyAfter": "5s",
		"discoveryArmedAt":         now.Add(-6 * time.Second).Format(time.RFC3339Nano),
		"discoveryCompletedAt":     now.Add(-time.Second).Format(time.RFC3339Nano),
		"discoveryFreshUntil":      now.Add(time.Minute).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: planningRouterForNode("onprem-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Ready" || status["plannerPhase"] != "BGPPlanned" {
		t.Fatalf("status = %#v, want Ready for fresh empty complete discovery", status)
	}

	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryPhase":           "Complete",
		"discoveryMode":            "onprem-l2",
		"discoveryObserved":        0,
		"discoveryResultCount":     0,
		"discoveryAuthoritative":   false,
		"discoveryAllowEmptyAfter": "5s",
		"discoveryArmedAt":         now.Add(-time.Minute).Format(time.RFC3339Nano),
		"discoveryCompletedAt":     now.Add(-time.Minute).Format(time.RFC3339Nano),
		"discoveryFreshUntil":      now.Add(-time.Second).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus stale: %v", err)
	}
	bgp = &fakeBGPPaths{}
	controller = Controller{Router: planningRouterForNode("onprem-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile stale: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" || !strings.Contains(fmt.Sprint(status["plannerReason"]), "empty ownership discovery is not fresh") {
		t.Fatalf("status = %#v, want Pending for stale empty complete discovery", status)
	}
}

func TestControllerBGPModeProfileSpecMatchesInlineSpec(t *testing.T) {
	now := time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC)
	inlineSpec := awsFailoverPoolSpec()
	inlineSpec.DeliveryPolicy.Mode = "bgp"
	profileSpec := profileAWSFailoverPoolSpecForNode("aws-router-b")

	inlinePaths, inlinePlans := reconcileBGPProfileEquivalence(t, "aws-router-b", inlineSpec, now)
	profilePaths, profilePlans := reconcileBGPProfileEquivalence(t, "aws-router-b", profileSpec, now)

	if got, want := canonicalJSON(t, profilePaths), canonicalJSON(t, inlinePaths); got != want {
		t.Fatalf("profile BGP paths differ from inline\nprofile=%s\ninline=%s", got, want)
	}
	if got, want := canonicalJSON(t, profilePlans), canonicalJSON(t, inlinePlans); got != want {
		t.Fatalf("profile action plans differ from inline\nprofile=%s\ninline=%s", got, want)
	}
	if _, ok := pathBySourcePrefixOptional(profilePaths, DynamicSource("cloudedge", "aws-router-b"), "10.99.0.6/32"); !ok {
		t.Fatalf("profile paths = %#v, want liveness marker", profilePaths)
	}
	for _, address := range []string{"10.88.60.10/32", "10.88.60.12/32", "10.88.60.13/32"} {
		assign := findActionPlanByAddress(profilePlans, "assign-secondary-ip", address)
		if assign == nil {
			t.Fatalf("profile plans = %#v, want trap assign for %s", profilePlans, address)
		}
		if assign.Parameters["allowReassignment"] != "true" {
			t.Fatalf("assign %s parameters = %#v, want D5 standby seize allowReassignment", address, assign.Parameters)
		}
	}
	if assign := findActionPlanByAddress(profilePlans, "assign-secondary-ip", "10.88.60.11/32"); assign != nil {
		t.Fatalf("profile plans = %#v, self/site-owned .11 must not be trapped", profilePlans)
	}
}

func TestControllerBGPModeProviderDiscoveryAdvertisesUnexpiredOwnerEvents(t *testing.T) {
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
	if _, ok := maybePathBySourcePrefix(bgp, source, "10.88.60.11/32"); !ok {
		t.Fatalf("paths = %#v, want unexpired provider-discovery self-origin advertised before fresh inventory status", bgp.paths)
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
		t.Fatalf("paths = %#v, stale provider-discovery owner absent from fresh inventory must be withdrawn", bgp.paths)
	}
}

func TestControllerBGPModeFailedProviderActionDoesNotSuppressHomeOwnerPath(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	source := DynamicSource("cloudedge", "azure-router")
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-failed-address",
		Group:      "cloudedge",
		SourceNode: "azure-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
		Payload: map[string]string{
			"address": "10.88.60.11/32",
			"pool":    "cloudedge",
			"source":  providerDiscoverySource,
			"nicRef":  "client-nic",
		},
	})
	if _, err := store.ImportAction(routerstate.ActionExecutionRecord{
		Source:         source,
		IdempotencyKey: "failed-assign",
		Provider:       "azure",
		ProviderRef:    "azure-provider",
		Action:         "assign-secondary-ip",
		TargetJSON:     `{"address":"10.88.60.11/32","nicRef":"client-nic","providerRef":"azure-provider"}`,
		Status:         routerstate.ActionFailed,
		Error:          "provider API unavailable",
		CreatedAt:      now.Add(-2 * time.Minute),
		UpdatedAt:      now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("ImportAction: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: planningRouterForNode("azure-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, source, "10.88.60.11/32"); !ok {
		t.Fatalf("paths = %#v, want provider-discovery home path retained despite failed capture action", bgp.paths)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["providerActionFailedAddresses"]) != "[10.88.60.11/32]" {
		t.Fatalf("status = %#v, want failed /32 address reported", status)
	}
}

func TestControllerBGPModeFreshHomeOwnerKeepsRemoteProviderDeliveryCapture(t *testing.T) {
	now := time.Date(2026, 6, 9, 17, 30, 0, 0, time.UTC)
	cases := []struct {
		name         string
		address      string
		homeNode     string
		homeProvider string
		homeRef      string
		homeNIC      string
	}{
		{
			name:         "aws home owner keeps oci delivery capture",
			address:      "10.88.60.11/32",
			homeNode:     "aws-router-a",
			homeProvider: "aws",
			homeRef:      "aws-provider",
			homeNIC:      "aws-client-nic",
		},
		{
			name:         "azure home owner keeps oci delivery capture",
			address:      "10.88.60.12/32",
			homeNode:     "azure-router",
			homeProvider: "azure",
			homeRef:      "azure-provider",
			homeNIC:      "azure-client-nic",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := testStore(t, now)
			spec := awsFailoverPoolSpec()
			spec.DeliveryPolicy.Mode = "bgp"
			recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", tc.homeNode, tc.address, tc.homeProvider, tc.homeRef, providerinventory.PrivateIPRecord{
				Address:   tc.address,
				NICRef:    tc.homeNIC,
				SubnetRef: tc.homeRef + "-subnet",
			}, now.Add(-time.Second), time.Hour))
			seedSucceededBGPCaptureAction(t, store, "oci-provider", "oci-vnic", "oci-router", tc.address, "assign-secondary-ip", 1, now.Add(-time.Second))
			saveBGPInstalledNextHops(t, store, map[string][]string{tc.address: {"10.99.0.200"}})
			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
				"discoverySelfPrivateIPs": []string{tc.address},
				"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
			}); err != nil {
				t.Fatalf("SaveObjectStatus(oci): %v", err)
			}

			bgp := &fakeBGPPaths{}
			ociController := Controller{Router: routerWithOCIProvider(routerWithBGPRouter(planningRouterForNode("oci-router", spec))), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
			if err := ociController.Reconcile(context.Background()); err != nil {
				t.Fatalf("oci Reconcile: %v", err)
			}
			if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "oci-router"), tc.address); ok {
				t.Fatalf("paths = %#v, want OCI captured path suppressed while fresh %s home owner exists", bgp.paths, tc.homeRef)
			}
			ociPlans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "oci-router")).ActionPlansJSON)
			if findActionPlanByAddress(ociPlans, "assign-secondary-ip", tc.address) == nil {
				t.Fatalf("oci plans = %#v, want remote delivery capture retained for %s", ociPlans, tc.address)
			}

			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
				"discoveryOwnedAddresses": []string{},
				"discoverySelfPrivateIPs": []string{"10.88.60.250"},
				"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
			}); err != nil {
				t.Fatalf("SaveObjectStatus(home): %v", err)
			}
			if _, err := store.ImportAction(routerstate.ActionExecutionRecord{
				Source:         DynamicSource("cloudedge", tc.homeNode),
				IdempotencyKey: "failed-home-capture-" + safeName(tc.address),
				Provider:       tc.homeProvider,
				ProviderRef:    tc.homeRef,
				Action:         "assign-secondary-ip",
				TargetJSON:     fmt.Sprintf(`{"address":%q,"nicRef":%q,"providerRef":%q}`, tc.address, tc.homeNIC, tc.homeRef),
				Status:         routerstate.ActionFailed,
				Error:          "stale capture failure",
				CreatedAt:      now.Add(-10 * time.Minute),
				UpdatedAt:      now.Add(-9 * time.Minute),
			}); err != nil {
				t.Fatalf("ImportAction(failed home capture): %v", err)
			}
			homeController := Controller{Router: routerWithBGPRouter(planningRouterForNode(tc.homeNode, spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
			if err := homeController.Reconcile(context.Background()); err != nil {
				t.Fatalf("home Reconcile: %v", err)
			}
			if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", tc.homeNode), tc.address); !ok {
				t.Fatalf("paths = %#v, want fresh home owner %s to advertise %s", bgp.paths, tc.homeNode, tc.address)
			}
		})
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

func TestControllerBGPModeKeepsOnPremOwnerWhenOneDiscoverySourceExpires(t *testing.T) {
	now := time.Date(2026, 6, 5, 13, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].StaticOwnedAddresses = nil
	address := "10.88.60.21/32"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-arp-observed",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ObservedEventType,
		Subject:    address,
		ObservedAt: now.Add(-2 * time.Minute),
		ExpiresAt:  now.Add(5 * time.Minute),
		Payload: map[string]string{
			"address":    address,
			"pool":       "cloudedge",
			"source":     onPremDiscoverySource,
			"sourceType": OnPremSourceARPObserver,
		},
	})
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-dhcp-expired",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ExpiredEventType,
		Subject:    address,
		ObservedAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(5 * time.Minute),
		Payload: map[string]string{
			"address":    address,
			"pool":       "cloudedge",
			"source":     onPremDiscoverySource,
			"sourceType": OnPremSourceDHCPv4Lease,
		},
	})

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "onprem-router"), address); !ok {
		t.Fatalf("paths = %#v, want ARP-observed owner retained despite DHCP expiry", bgp.paths)
	}
}

func TestControllerBGPModeDrainKeepsLocalPathAtStandbyPreference(t *testing.T) {
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
		bgpdaemon.AppliedPathKey(bgpdaemon.AppliedPath{Source: aSource, Prefix: "10.99.0.2/32"}): bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: aSource,
			Prefix: "10.99.0.2/32",
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
			Attrs:  bgpdaemon.AppliedPathAttrs{LocalPref: 50, Communities: []string{bgpstate.MobilityCommunityNodeLiveness, bgpstate.MobilityNodeIdentityCommunity("azure-router-a")}},
		}),
	}}

	controllerA := Controller{Router: routerWithEventGroupListen(planningRouterForNode("azure-router-a", spec), "10.99.0.2"), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("old owner Reconcile: %v", err)
	}
	path := pathBySourcePrefix(t, bgp, aSource, "10.88.60.12/32")
	if path.Attrs.LocalPref != bgpMobilityLocalPrefBase {
		t.Fatalf("drained path localPref = %d, want standby preference", path.Attrs.LocalPref)
	}
	if len(bgp.deletes) != 1 || bgp.deletes[0].Source != aSource || bgp.deletes[0].Prefix != "10.99.0.2/32" {
		t.Fatalf("old owner deletes = %#v, want liveness marker withdrawn only", bgp.deletes)
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
	saveBGPStatus(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}}, nil, nil)
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
	ownerUpserts := nonLivenessUpserts(bgp.upserts)
	if len(ownerUpserts) != 1 || ownerUpserts[0].Prefix != "10.88.60.11/32" {
		t.Fatalf("BGP upserts after failed provider action = %#v, want route retained", bgp.upserts)
	}
	part = latestPart(t, store, source)
	if findActionPlanByAddress(decodeActionPlans(t, part.ActionPlansJSON), "assign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatalf("actionPlans after failure = %s, want desired provider assign retained", part.ActionPlansJSON)
	}
}

func TestControllerBGPModeClearsStaleProviderActionFailureStatus(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"providerActionPhase":           "Failed",
		"providerActionError":           "provider API unavailable",
		"providerActionFailedAddresses": []string{"10.88.60.11/32"},
		"providerActionFailedTargets":   []string{"10.88.60.11/32"},
		"providerActionFailedDetails":   []map[string]string{{"action": "assign-secondary-ip", "address": "10.88.60.11/32"}},
		"providerActionFailedCount":     1,
		"providerActionFailedAt":        now.Add(-time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	controller := Controller{
		Router:   routerWithBGPRouter(planningRouterForNode("azure-router", spec)),
		Store:    store,
		BGPPaths: &fakeBGPPaths{},
		Now:      func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["providerActionPhase"] != "OK" || status["providerActionError"] != "" || fmt.Sprint(status["providerActionFailedCount"]) != "0" {
		t.Fatalf("provider action failure status was not cleared: %#v", status)
	}
	if status["providerActionFailedAddresses"] != nil || status["providerActionFailedTargets"] != nil || status["providerActionFailedDetails"] != nil || status["providerActionFailedAt"] != "" {
		t.Fatalf("provider action failure details were not cleared: %#v", status)
	}
}

func TestControllerBGPModeWaitsForProviderObservationAfterAssignSuccess(t *testing.T) {
	now := time.Date(2026, 6, 25, 5, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
	source := DynamicSource("cloudedge", "azure-router")
	address := "10.88.60.10/32"
	saveBGPInstalledNextHops(t, store, map[string][]string{address: {"10.99.0.1"}})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.11/32"},
		"discoverySelfCapturedAddresses": []string{},
		"discoverySelfForwardingEnabled": true,
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", address)
	if assign == nil {
		t.Fatalf("plans = %#v, want provider assign", plans)
	}
	id, err := importApprovedAction(t, assign, source, store, now)
	if err != nil {
		t.Fatalf("import action: %v", err)
	}
	if err := store.MarkActionResult(id, routerstate.ActionSucceeded, "ok", "", nil, now.Add(time.Second)); err != nil {
		t.Fatalf("MarkActionResult: %v", err)
	}

	controller.Now = func() time.Time { return now.Add(2 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("pending observation Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" || status["plannerPhase"] != "Pending" || status["providerObservationPhase"] != "Pending" {
		t.Fatalf("status = %#v, want provider observation pending", status)
	}
	if fmt.Sprint(status["providerObservationPendingAddresses"]) != "["+address+"]" || fmt.Sprint(status["providerObservationPendingCount"]) != "1" {
		t.Fatalf("status = %#v, want pending observation address", status)
	}
	details := ownershipStatusDecisions(t, status["providerObservationDetails"])
	pending := ownershipStatusDecisionByAddress(t, details, address)
	if pending["phase"] != "Pending" || pending["reason"] == "" {
		t.Fatalf("providerObservationDetails = %#v, want pending reason", details)
	}

	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.11/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoverySelfForwardingEnabled": true,
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus stale observed: %v", err)
	}
	controller.Now = func() time.Time { return now.Add(2500 * time.Millisecond) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("stale observation Reconcile: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["providerObservationPhase"] != "Pending" || fmt.Sprint(status["providerObservationPendingCount"]) != "1" {
		t.Fatalf("status = %#v, want stale provider observation to remain pending", status)
	}
	details = ownershipStatusDecisions(t, status["providerObservationDetails"])
	pending = ownershipStatusDecisionByAddress(t, details, address)
	if pending["reason"] != "provider inventory snapshot predates action completion" || pending["snapshotCompletedAt"] == "" || pending["requiredAfter"] == "" {
		t.Fatalf("providerObservationDetails = %#v, want stale snapshot reason and timestamps", details)
	}

	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.11/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoverySelfForwardingEnabled": true,
		"discoveryLastScanAt":            now.Add(3 * time.Second).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus observed: %v", err)
	}
	controller.Now = func() time.Time { return now.Add(4 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("confirmed observation Reconcile: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["providerObservationPhase"] != "OK" || fmt.Sprint(status["providerObservationPendingCount"]) != "0" {
		t.Fatalf("status = %#v, want confirmed observation", status)
	}
	if fmt.Sprint(status["providerObservationConfirmedAddresses"]) != "["+address+"]" || fmt.Sprint(status["providerObservationConfirmedCount"]) != "1" {
		t.Fatalf("status = %#v, want confirmed observation address", status)
	}
}

func TestControllerBGPModeReportsFailedForwardingProviderAction(t *testing.T) {
	now := time.Date(2026, 6, 25, 4, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.3"},
	}, nil, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfNICRef":            "eni-a",
		"discoverySelfSubnetRef":         "subnet-a",
		"discoverySelfPrivateIPs":        []string{"10.88.60.11"},
		"discoverySelfForwardingEnabled": false,
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	source := DynamicSource("cloudedge", "aws-router-a")
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	forwarding := findActionPlan(plans, "ensure-forwarding-enabled")
	if forwarding == nil {
		t.Fatalf("plans = %#v, want ensure-forwarding-enabled", plans)
	}
	id, err := importApprovedAction(t, forwarding, source, store, now)
	if err != nil {
		t.Fatalf("import action: %v", err)
	}
	failedAt := now.Add(2 * time.Second)
	if err := store.MarkActionResult(id, routerstate.ActionFailed, "failed", "source/dest check update denied", nil, failedAt); err != nil {
		t.Fatalf("MarkActionResult: %v", err)
	}

	controller.Now = func() time.Time { return now.Add(3 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Failed" || status["providerActionPhase"] != "Failed" {
		t.Fatalf("status = %#v, want failed provider action phase", status)
	}
	if fmt.Sprint(status["providerActionFailedCount"]) != "1" || status["providerActionError"] != "source/dest check update denied" {
		t.Fatalf("status = %#v, want forwarding failure count/error", status)
	}
	if status["providerActionFailedAt"] != failedAt.Format(time.RFC3339) {
		t.Fatalf("status = %#v, want failedAt %s", status, failedAt.Format(time.RFC3339))
	}
	details, ok := status["providerActionFailedDetails"].([]interface{})
	if !ok || len(details) != 1 {
		t.Fatalf("providerActionFailedDetails = %#v, want one detail", status["providerActionFailedDetails"])
	}
	detail, ok := details[0].(map[string]interface{})
	if !ok {
		t.Fatalf("providerActionFailedDetails = %#v, want object detail", details)
	}
	if detail["action"] != "ensure-forwarding-enabled" || detail["target"] != "eni-a" || detail["idempotencyKey"] != forwarding.IdempotencyKey {
		t.Fatalf("providerActionFailedDetails = %#v, want forwarding target detail", details)
	}
}

func TestControllerBGPModeObservedSelfCaptureSupersedesProviderActionFailureStatus(t *testing.T) {
	now := time.Date(2026, 6, 14, 20, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.10/32"
	recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:   address,
		NICRef:    "oci-client-vnic",
		SubnetRef: "oci-subnet",
	}, now.Add(-time.Second), time.Hour))
	if _, err := store.ImportAction(routerstate.ActionExecutionRecord{
		Source:         DynamicSource("cloudedge", "aws-router-a"),
		IdempotencyKey: "stale-failed-assign-" + safeName(address),
		Provider:       "aws",
		ProviderRef:    "aws-provider",
		Action:         "assign-secondary-ip",
		TargetJSON:     `{"address":"10.88.60.10/32","nicRef":"eni-a","providerRef":"aws-provider"}`,
		Status:         routerstate.ActionFailed,
		Error:          "stale provider conflict",
		CreatedAt:      now.Add(-time.Minute),
		UpdatedAt:      now.Add(-30 * time.Second),
		ExecutedAt:     now.Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("ImportAction(failed): %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{address},
		"discoverySelfCapturedAddresses": []string{address},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithOCIProvider(routerWithBGPRouter(planningRouterForNode("oci-router", spec))), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["providerActionPhase"] != "OK" || fmt.Sprint(status["providerActionFailedCount"]) != "0" {
		t.Fatalf("provider failure was not superseded in status: %#v", status)
	}
	if fmt.Sprint(status["providerActionSupersededFailureCount"]) != "1" ||
		fmt.Sprint(status["providerActionSupersededFailureAddresses"]) != "[10.88.60.10/32]" ||
		status["providerActionSupersededFailureReason"] != "observed-self-capture" {
		t.Fatalf("superseded failure breadcrumb missing: %#v", status)
	}
}

func TestInterpretProviderCaptureAssignFailures(t *testing.T) {
	now := time.Date(2026, 6, 14, 19, 8, 31, 0, time.UTC)
	actions := []routerstate.ActionExecutionRecord{
		{
			Action:     "assign-secondary-ip",
			Status:     routerstate.ActionFailed,
			TargetJSON: `{"address":"10.88.60.11/32"}`,
			Error:      "provider conflict: address already allocated",
			UpdatedAt:  now,
		},
		{
			Action:     "assign-secondary-ip",
			Status:     routerstate.ActionFailed,
			TargetJSON: `{"address":"10.88.60.12/32"}`,
			Error:      "provider unavailable",
			UpdatedAt:  now,
		},
	}
	interpreted := interpretProviderCaptureAssignFailures(actions, map[string]bool{"10.88.60.11/32": true}, now.Add(time.Second))
	if _, ok := interpreted.Active["10.88.60.11/32"]; ok {
		t.Fatalf("observed self capture failure is still active: %#v", interpreted.Active)
	}
	if _, ok := interpreted.Superseded["10.88.60.11/32"]; !ok {
		t.Fatalf("observed self capture failure was not marked superseded: %#v", interpreted.Superseded)
	}
	if _, ok := interpreted.Active["10.88.60.12/32"]; !ok {
		t.Fatalf("unobserved failure was not active: %#v", interpreted.Active)
	}
	stale := interpretProviderCaptureAssignFailures(actions, map[string]bool{"10.88.60.11/32": true}, now.Add(-time.Second))
	if _, ok := stale.Active["10.88.60.11/32"]; !ok {
		t.Fatalf("stale observation should not supersede failed action: %#v", stale)
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
	stale := providerCaptureObservationStatus(nil, previous, journal, map[string]bool{address: false}, true, now, false, false, time.Time{}, nil)
	if stale.Phase != "Pending" || fmt.Sprint(stale.PendingAddresses) != "["+address+"]" {
		t.Fatalf("stale = %#v, want unassign observation pending", stale)
	}
	fresh := providerCaptureObservationStatus(nil, previous, journal, map[string]bool{address: false}, true, now.Add(2*time.Second), false, false, time.Time{}, nil)
	if fresh.Phase != "OK" || fmt.Sprint(fresh.ConfirmedAddresses) != "["+address+"]" {
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
	stale := providerCaptureObservationStatus([]dynamicconfig.ActionPlan{plan}, nil, journal, nil, true, now, true, true, now, nil)
	if stale.Phase != "Pending" || fmt.Sprint(stale.PendingTargets) != "[eni-a]" {
		t.Fatalf("stale = %#v, want forwarding observation pending", stale)
	}
	fresh := providerCaptureObservationStatus([]dynamicconfig.ActionPlan{plan}, nil, journal, nil, true, now, true, true, now.Add(2*time.Second), nil)
	if fresh.Phase != "OK" || fmt.Sprint(fresh.ConfirmedTargets) != "[eni-a]" {
		t.Fatalf("fresh = %#v, want forwarding observation confirmed", fresh)
	}
}

func TestSAMAddressStatusesExposeProviderObservationBlocker(t *testing.T) {
	now := time.Date(2026, 6, 26, 4, 40, 0, 0, time.UTC)
	address := "10.88.60.11/32"
	plan := dynamicconfig.ActionPlan{
		Provider:       "aws",
		ProviderRef:    "aws-provider",
		Action:         actionAssignSecondaryIP,
		Target:         map[string]string{"address": address, "providerRef": "aws-provider", "nicRef": "eni-a"},
		Parameters:     map[string]string{captureAssignmentGenerationParam: "gen-42"},
		IdempotencyKey: "assign-key",
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
		ExecutedAt:     now,
		UpdatedAt:      now,
	}}
	observations := providerCaptureObservationStatus([]dynamicconfig.ActionPlan{plan}, nil, journal, map[string]bool{}, true, now.Add(time.Second), false, false, time.Time{}, []ownershipDecision{{
		Address:       address,
		Class:         ownershipClassRemoteHomeOwned,
		HomeOwnerNode: "aws-router",
		Source:        "provider-discovery",
	}})
	statuses := samAddressStatuses([]ownershipDecision{{
		Address:       address,
		Class:         ownershipClassRemoteHomeOwned,
		HomeOwnerNode: "aws-router",
		Source:        "provider-discovery",
	}}, []dynamicconfig.ActionPlan{plan}, journal, nil, observations)
	item, ok := statuses[address].(map[string]any)
	if !ok {
		t.Fatalf("address status = %#v, want map", statuses[address])
	}
	if item["phase"] != "Pending" || item["blockingCondition"] != "ProviderObserved" || item["assignmentGeneration"] != "gen-42" {
		t.Fatalf("address status = %#v, want pending provider observation blocker and generation", item)
	}
	conditions := item["conditions"].(map[string]string)
	if conditions["OwnershipResolved"] != "True" || conditions["ProviderActionApplied"] != "True" || conditions["ProviderObserved"] != "False" {
		t.Fatalf("conditions = %#v, want resolved/action true and observed false", conditions)
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
	saveBGPStatus(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}}, nil, nil)
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
	if status["phase"] != "Degraded" || status["plannerPhase"] != "Degraded" || !strings.Contains(fmt.Sprint(status["plannerReason"]), "self NIC is unresolved") {
		t.Fatalf("status = %#v, want unresolved self NIC degraded", status)
	}
	if status["selfCaptureResolved"] != false || !strings.Contains(fmt.Sprint(status["selfCaptureReason"]), "self NIC is unresolved") {
		t.Fatalf("status = %#v, want explicit self capture blocker", status)
	}
	if ownerUpserts := nonLivenessUpserts(bgp.upserts); len(ownerUpserts) != 0 {
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
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["selfCaptureResolved"] != true {
		t.Fatalf("status = %#v, want resolved self capture", status)
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
	saveBGPStatus(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}}, nil, nil)
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
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryOwnedAddresses":        []string{"10.88.60.10/32"},
		"discoverySelfPrivateIPs":        []string{"10.88.60.22"},
		"discoverySelfNICRef":            "router-b-nic",
		"discoverySelfForwardingEnabled": false,
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
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

func TestControllerBGPModeDrainMarkerWithdrawLetsPeerSeizeWithStaleConfig(t *testing.T) {
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
		Payload: map[string]string{
			"address": "10.88.60.11/32",
			"pool":    "cloudedge",
			"source":  providerDiscoverySource,
			"nicRef":  "client-nic-a",
		},
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryOwnedAddresses": []string{"10.88.60.11/32"},
		"discoverySelfPrivateIPs": []string{"10.88.60.4/32"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	drained := awsFailoverPoolSpec()
	drained.DeliveryPolicy.Mode = "bgp"
	drained.Members[1].Maintenance.Drain = true
	bgp := &fakeBGPPaths{}
	controllerA := Controller{Router: routerWithBGPRouter(routerWithEventGroupListen(planningRouterForNode("aws-router-a", drained), "10.99.0.2")), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controllerA.Reconcile(context.Background()); err != nil {
		t.Fatalf("drained router-a Reconcile: %v", err)
	}
	aPath := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-a"), "10.88.60.11/32")
	if aPath.Attrs.LocalPref != bgpMobilityLocalPrefBase {
		t.Fatalf("drained router-a localPref = %d, want low-preference handoff path", aPath.Attrs.LocalPref)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "aws-router-a"), "10.99.0.2/32"); ok {
		t.Fatalf("drained router-a still advertises liveness marker: %#v", bgp.paths)
	}

	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.11/32": {"10.99.0.2"},
	}, []map[string]any{}, map[string]string{bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32"})
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "aws-router-b", spec, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	}, now.Add(time.Second))
	controllerB := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-b", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now.Add(time.Second) }}
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("stale-config router-b Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-b")).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.11/32")
	if assign == nil {
		t.Fatalf("stale-config router-b plans = %#v, want seize assign after drained active marker withdrew", plans)
	}
	if assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign parameters = %#v, want allowReassignment for marker-withdraw seize", assign.Parameters)
	}
}

func TestControllerGracefulStopSuppressesProviderDeprovision(t *testing.T) {
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
		Payload: map[string]string{
			"address": "10.88.60.11/32",
			"pool":    "cloudedge",
			"source":  providerDiscoverySource,
			"nicRef":  "client-nic-a",
		},
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryOwnedAddresses": []string{"10.88.60.11/32"},
		"discoverySelfPrivateIPs": []string{"10.88.60.4/32"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(routerWithEventGroupListen(planningRouterForNode("aws-router-a", spec), "10.99.0.2")), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}

	drained := awsFailoverPoolSpec()
	drained.DeliveryPolicy.Mode = "bgp"
	drained.Members[1].Maintenance.Drain = true
	controller.Router = routerWithBGPRouter(routerWithEventGroupListen(planningRouterForNode("aws-router-a", drained), "10.99.0.2"))
	controller.SuppressProviderDeprovision = true
	controller.Now = func() time.Time { return now.Add(time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("graceful-stop Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if findActionPlanByAddress(plans, "unassign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, want graceful stop prepare to suppress local unassign", plans)
	}
	path := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-a"), "10.88.60.11/32")
	if path.Attrs.LocalPref != bgpMobilityLocalPrefBase {
		t.Fatalf("localPref = %d, want low-preference handoff path", path.Attrs.LocalPref)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "aws-router-a"), "10.99.0.2/32"); ok {
		t.Fatalf("graceful stop still advertises liveness marker: %#v", bgp.paths)
	}
}

func TestControllerBGPModeProviderCaptureSuccessDoesNotAdvertisePlannedDrainTakeover(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[1].Maintenance.Drain = true
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-aws-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
		Payload: map[string]string{
			"address": "10.88.60.11/32",
			"pool":    "cloudedge",
			"source":  providerDiscoverySource,
			"nicRef":  "client-nic-a",
		},
	})
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.11/32": {"10.99.0.2"},
	}, []map[string]any{}, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.0.2/32",
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	})
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-b", "aws-router-b", "10.88.60.11/32", "assign-secondary-ip", 1, now.Add(-time.Second))

	bgp := &fakeBGPPaths{}
	controllerB := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-b", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controllerB.Reconcile(context.Background()); err != nil {
		t.Fatalf("router-b Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.11/32"); ok {
		t.Fatalf("paths = %#v, provider capture must not advertise home ownership", bgp.paths)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if _, ok := status["generatedProviderCapturedBGPPaths"]; ok {
		t.Fatalf("status = %#v, provider-captured BGP status is obsolete because provider capture must not advertise ownership", status)
	}
	if _, ok := status["generatedSeizedBGPPaths"]; ok {
		t.Fatalf("status = %#v, seized BGP path status is obsolete because seize is a provider-capture action, not owner advertisement", status)
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
		"discoverySelfPrivateIPs": []string{"10.88.60.4", "10.88.60.11/32"},
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
	if findActionPlanByAddress(plans, "unassign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, provider-secondary BGP delivery must not deprovision sticky self-trap during convergence", plans)
	}
}

func TestControllerBGPModeReappliesForwardingWhenProviderObservedDisabled(t *testing.T) {
	now := time.Date(2026, 6, 3, 14, 20, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.3"},
	}, nil, nil)
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

func TestControllerBGPModeAdvertisesSelfLivenessMarkerFromBGPRouterID(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(planningRouterForNode("aws-router-a", spec))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	marker := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-a"), "10.99.0.1/32")
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

func TestControllerBGPModeStandbySeizesTrapAfterActiveLivenessHoldDown(t *testing.T) {
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
	current := now
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return current }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	initialPlans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-b")).ActionPlansJSON)
	if findActionPlanByAddress(initialPlans, "assign-secondary-ip", "10.88.60.10/32") != nil {
		t.Fatalf("initial plans = %#v, want hold-down before standby seize", initialPlans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	election, ok := status["bgpCaptureElection"].(map[string]any)
	if !ok {
		t.Fatalf("initial bgpCaptureElection = %#v, want map status", status["bgpCaptureElection"])
	}
	if election["seize"] != false || election["seizeHoldDown"] != true || election["selfMarkerPresent"] != true || election["activeMarkerPresent"] != false {
		t.Fatalf("initial bgpCaptureElection = %#v, want self marker present, active marker absent, hold-down", election)
	}
	if status["bgpCapturePending"] != true || status["bgpCapturePendingReason"] != "seize-hold-down" || status["bgpCapturePendingUntil"] == "" {
		t.Fatalf("initial status = %#v, want pending capture hold-down status", status)
	}
	if status["phase"] != "Pending" || status["plannerPhase"] != "Pending" || status["plannerReason"] != "BGP capture seize hold-down is active" {
		t.Fatalf("initial status = %#v, want pending planner phase during capture hold-down", status)
	}

	current = now.Add(bgpSeizeLivenessMissingHold + time.Second)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("hold-down elapsed Reconcile: %v", err)
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
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	election, ok = status["bgpCaptureElection"].(map[string]any)
	if !ok {
		t.Fatalf("bgpCaptureElection = %#v, want map status", status["bgpCaptureElection"])
	}
	if election["seize"] != true || election["seizeHoldDown"] != false || election["selfMarkerPresent"] != true || election["activeMarkerPresent"] != false {
		t.Fatalf("bgpCaptureElection = %#v, want self marker present, active marker absent, seize", election)
	}
	if status["bgpCapturePending"] != false || status["bgpCapturePendingReason"] != "" || status["bgpCapturePendingUntil"] != "" {
		t.Fatalf("status = %#v, want pending capture status cleared after hold-down", status)
	}
	if election["selfCommunity"] != bgpstate.MobilityNodeIdentityCommunity("aws-router-b") ||
		election["activeCommunity"] != bgpstate.MobilityNodeIdentityCommunity("aws-router-a") {
		t.Fatalf("bgpCaptureElection communities = %#v, want aws-b self and aws-a active", election)
	}
}

func TestControllerBGPModeSeizeSuccessDoesNotAdvertiseTrapAsOwner(t *testing.T) {
	now := time.Date(2026, 6, 3, 11, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.12/32": {"10.99.0.3"},
	}, []map[string]any{}, map[string]string{bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32"})
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "aws-router-b", spec, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	}, now)
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-b", "aws-router-b", "10.88.60.12/32", "assign-secondary-ip", 1, now.Add(-time.Second))

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-b", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.12/32"); ok {
		t.Fatalf("paths = %#v, provider capture must not advertise home ownership", bgp.paths)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.10/32"); ok {
		t.Fatalf("paths = %#v, want no BGP path for trap without successful provider capture", bgp.paths)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if _, ok := status["generatedSeizedBGPPaths"]; ok {
		t.Fatalf("status = %#v, seized BGP path status is obsolete because seize is a provider-capture action, not owner advertisement", status)
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
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "aws-router-b", spec, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("onprem-router"):  "10.99.0.1/32",
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"):   "10.99.0.5/32",
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
		bgpstate.MobilityNodeIdentityCommunity("azure-router"):   "10.99.0.3/32",
		bgpstate.MobilityNodeIdentityCommunity("oci-router"):     "10.99.0.4/32",
	}, now)
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
	aDrained := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-a"), "10.88.60.11/32")
	if aDrained.Attrs.LocalPref != bgpMobilityLocalPrefBase {
		t.Fatalf("drained router-a localPref = %d, want low-preference handoff path", aDrained.Attrs.LocalPref)
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

func TestPlanBGPMobilityDeliverySuppressesSameSiteSecondaryIPCapture(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-a"]
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		InstalledNextHops: map[string][]string{
			"10.88.60.11/32": {"10.99.0.5"},
			"10.88.60.12/32": {"10.99.0.3"},
		},
		Profiles: map[string]api.CloudProviderProfileSpec{
			"aws-provider": {Provider: "aws"},
		},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", "10.88.60.11/32"); assign != nil {
		t.Fatalf("action plans = %#v, same-site AWS home address must not be assigned as router secondary IP", delivery.ActionPlans)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", "10.88.60.12/32"); assign == nil {
		t.Fatalf("action plans = %#v, remote site home address should remain capturable", delivery.ActionPlans)
	}
}

func TestPlanBGPMobilityDeliveryDoesNotReviveIneligibleCaptureAfterFailedAction(t *testing.T) {
	now := time.Date(2026, 6, 26, 14, 20, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.11/32"

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
		Profiles:          map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ActionJournal: []routerstate.ActionExecutionRecord{{
			Action:      "assign-secondary-ip",
			Provider:    "aws",
			ProviderRef: "aws-provider",
			TargetJSON:  `{"address":"10.88.60.11/32","nicRef":"eni-b","providerRef":"aws-provider"}`,
			Status:      routerstate.ActionFailed,
			Error:       "provider conflict",
			UpdatedAt:   now.Add(-time.Second),
			ExecutedAt:  now.Add(-time.Second),
		}},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, failed action must not revive local-home capture", delivery.ActionPlans)
	}
}

func TestPlanBGPMobilityDeliverySuppressesSameSiteFreshHomeStaleCapture(t *testing.T) {
	now := time.Date(2026, 6, 26, 14, 25, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.11/32"

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		InstalledNextHops:    map[string][]string{address: {"10.99.0.3"}},
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, same-site fresh-home stale capture must not be assigned", delivery.ActionPlans)
	}
}

func TestPlanBGPMobilityDeliverySuppressesSameProviderFreshHomeStaleCaptureAcrossPlacementGroups(t *testing.T) {
	now := time.Date(2026, 6, 29, 23, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
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
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		InstalledNextHops:    map[string][]string{address: {"10.99.0.3"}},
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, same-provider fresh-home stale capture must not be assigned across placement groups", delivery.ActionPlans)
	}
}

func TestPlanBGPMobilityDeliverySuppressesSameSiteRemoteHomeDuringSeize(t *testing.T) {
	now := time.Date(2026, 6, 26, 15, 40, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.11/32"

	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, self.CaptureTarget, address, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	for i := range previous {
		previous[i] = stampSingleBGPPathFence(previous[i], address, "prefix=10.88.60.11/32;nextHops=10.99.0.3", self.NodeRef)
	}

	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		PreviousPlans:        previous,
		InstalledNextHops:    map[string][]string{address: {"10.99.0.3"}},
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, same-site remote-home primary must not be assigned during seize", delivery.ActionPlans)
	}
}

func TestPlanBGPMobilityDeliveryDistributedPartialLivenessDoesNotDuplicateAssign(t *testing.T) {
	now := time.Date(2026, 6, 27, 5, 25, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
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
	planFor := func(self memberPlanInfo, markers map[string]string) bgpDeliveryPlannerResult {
		t.Helper()
		delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
			PoolName:  "cloudedge",
			Source:    DynamicSource("cloudedge", self.NodeRef),
			Self:      self,
			Members:   members,
			Spec:      spec,
			Decisions: decisions,
			Placement: PlacementDecision{
				Group:      "aws-edge",
				Active:     true,
				ActiveNode: self.NodeRef,
			},
			InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
			Profiles:          map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
			LivenessMarkers:   markers,
			RIBObserved:       true,
			Now:               now,
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
	if deliveryA.Distribution == nil || deliveryB.Distribution == nil {
		t.Fatalf("distributed capture missing distribution: A=%#v B=%#v", deliveryA.Distribution, deliveryB.Distribution)
	}
	holderA := deliveryA.Distribution.Assignments[address]
	holderB := deliveryB.Distribution.Assignments[address]
	if holderA == "" || holderA != holderB {
		t.Fatalf("partial liveness distributions diverged: A=%q B=%q", holderA, holderB)
	}
	assignA := findActionPlanByAddress(deliveryA.ActionPlans, "assign-secondary-ip", address)
	assignB := findActionPlanByAddress(deliveryB.ActionPlans, "assign-secondary-ip", address)
	if assignA != nil && assignB != nil {
		t.Fatalf("partial liveness generated duplicate same-site assigns: A=%#v B=%#v", deliveryA.ActionPlans, deliveryB.ActionPlans)
	}
	if assignA == nil && assignB == nil {
		t.Fatalf("partial liveness generated no assign for assigned holder %q: A=%#v B=%#v", holderA, deliveryA.ActionPlans, deliveryB.ActionPlans)
	}
}

func TestFilterSupersededSameProviderHomeFailures(t *testing.T) {
	failed := []providerActionPlanFailure{{
		IdempotencyKey: "stale-same-provider",
		Action:         "assign-secondary-ip",
		Address:        "10.88.60.11/32",
		ProviderRef:    "aws-provider",
		Error:          "primary address is already allocated",
	}, {
		IdempotencyKey: "stale-local-home",
		Action:         "assign-secondary-ip",
		Address:        "10.88.60.16/32",
		ProviderRef:    "aws-provider",
		Error:          "assigned, but move is not allowed",
	}, {
		IdempotencyKey: "stale-fresh-home",
		Action:         "assign-secondary-ip",
		Address:        "10.88.60.17/32",
		ProviderRef:    "aws-provider",
		Error:          "private address is already allocated to a provider-discovered home",
	}, {
		IdempotencyKey: "remote-provider",
		Action:         "assign-secondary-ip",
		Address:        "10.88.60.12/32",
		ProviderRef:    "aws-provider",
		Error:          "still desired",
	}}
	decisions := []ownershipDecision{{
		Address:         "10.88.60.11/32",
		Class:           ownershipClassRemoteHomeOwned,
		HomeOwnerNode:   "aws-router-a",
		HomeProviderRef: "aws-provider",
	}, {
		Address:         "10.88.60.12/32",
		Class:           ownershipClassRemoteHomeOwned,
		HomeOwnerNode:   "azure-router",
		HomeProviderRef: "azure-provider",
	}, {
		Address:          "10.88.60.16/32",
		Class:            ownershipClassLocalHomeOwned,
		HomeOwnerNode:    "aws-router-a",
		LocalProviderRef: "aws-provider",
	}, {
		Address:           "10.88.60.17/32",
		Class:             ownershipClassStaleCapture,
		HomeOwnerNode:     "aws-router-b",
		HomeProviderRef:   "aws-provider",
		Source:            providerDiscoverySource,
		SuppressionReason: "fresh-home-owner",
	}}

	got := filterSupersededSameProviderHomeFailures(failed, decisions, "aws-provider")
	if len(got) != 1 || got[0].IdempotencyKey != "remote-provider" {
		t.Fatalf("filtered failures = %#v, want only remote provider failure retained", got)
	}
}

func TestControllerBGPCaptureCandidateNextHopsExcludeProviderCapturePaths(t *testing.T) {
	now := time.Date(2026, 6, 13, 22, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	prefixes := []map[string]any{
		{
			"prefix":      "10.88.60.12/32",
			"nextHop":     "10.99.0.5",
			"best":        true,
			"valid":       true,
			"communities": []string{bgpMobilityCommunitySourceCapture},
		},
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
		"10.88.60.12/32": {"10.99.0.5"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, prefixes, nil)

	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store}
	got, observed := controller.bgpCaptureCandidateNextHops(spec)
	if !observed {
		t.Fatal("bgpCaptureCandidateNextHops observed=false, want prefixes to be authoritative")
	}
	if _, ok := got["10.88.60.12/32"]; ok {
		t.Fatalf("capture candidate next hops = %#v, provider-capture path must not be recaptured", got)
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
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.12/32"
	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, self.CaptureTarget, address, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	stampBGPPathFenceActionPlans(previous, address, "prefix="+address+";nextHops=10.99.0.3", self.NodeRef, now.Add(-time.Minute))
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		PreviousPlans:        previous,
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{address: true},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if len(delivery.CaptureCandidates) != 1 || !delivery.CaptureCandidates[address].ProtectOnly {
		t.Fatalf("capture candidates = %#v, standby holder must stay protected while active liveness is absent", delivery.CaptureCandidates)
	}
	if unassign := findActionPlanByAddress(delivery.ActionPlans, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("action plans = %#v, standby holder must not release before active liveness returns", delivery.ActionPlans)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, protect-only capture must not reassign", delivery.ActionPlans)
	}
}

func TestPlanBGPMobilityDeliverySuppressesDistributedCaptureDuringSeizeHoldDown(t *testing.T) {
	now := time.Date(2026, 6, 24, 14, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	for i := range spec.Members {
		if spec.Members[i].Placement.Group == "aws-edge" {
			spec.Members[i].MaxSecondaryIPs = 128
		}
	}
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		InstalledNextHops: map[string][]string{
			"10.88.60.10/32": {"10.99.0.3"},
		},
		Profiles: map[string]api.CloudProviderProfileSpec{
			"aws-provider": {Provider: "aws"},
		},
		ObservedSelfCaptures: map[string]bool{"10.88.60.12/32": true},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if candidate, ok := delivery.CaptureCandidates["10.88.60.10/32"]; ok && !candidate.ProtectOnly {
		t.Fatalf("capture candidates = %#v, hold-down must suppress new distributed captures", delivery.CaptureCandidates)
	}
	if len(delivery.CaptureCandidates) != 1 || !delivery.CaptureCandidates["10.88.60.12/32"].ProtectOnly {
		t.Fatalf("capture candidates = %#v, hold-down must retain only protect-only self captures", delivery.CaptureCandidates)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", "10.88.60.10/32"); assign != nil {
		t.Fatalf("action plans = %#v, hold-down must not assign new distributed captures", delivery.ActionPlans)
	}
	claim := bgpCaptureClaimForPlacement(self, delivery.Placement, now)
	if claim.Phase != "Pending" || claim.DesiredHolder != self.NodeRef || claim.PreviousHolder != "aws-router-a" {
		t.Fatalf("claim = %#v, want pending claim for self against previous active", claim)
	}
	if claim.Generation == "" || claim.PendingUntil.IsZero() {
		t.Fatalf("claim = %#v, want generation and pendingUntil", claim)
	}
}

func TestPlanBGPMobilityDeliveryStampsActiveClaimGenerationOnAssign(t *testing.T) {
	now := time.Date(2026, 6, 24, 15, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		InstalledNextHops: map[string][]string{
			address: {"10.99.0.3"},
		},
		Profiles: map[string]api.CloudProviderProfileSpec{
			"aws-provider": {Provider: "aws"},
		},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want assign", delivery.ActionPlans)
	}
	if assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign parameters = %#v, want failover reassignment", assign.Parameters)
	}
	if assign.Parameters[captureClaimPhaseParam] != "Active" {
		t.Fatalf("assign parameters = %#v, want active claim phase", assign.Parameters)
	}
	if assign.Parameters[captureClaimGenerationParam] == "" {
		t.Fatalf("assign parameters = %#v, want claim generation", assign.Parameters)
	}
	if assign.Parameters[captureAssignmentGenerationParam] == "" {
		t.Fatalf("assign parameters = %#v, want assignment generation", assign.Parameters)
	}
	if !strings.Contains(assign.IdempotencyKey, ":assigngen:"+safeName(assign.Parameters[captureAssignmentGenerationParam])) {
		t.Fatalf("assign idempotencyKey = %q, parameters = %#v, want assignment generation fence", assign.IdempotencyKey, assign.Parameters)
	}
	if strings.Contains(assign.IdempotencyKey, ":claimgen:") {
		t.Fatalf("assign idempotencyKey = %q, claim generation must not churn per-address assignment key", assign.IdempotencyKey)
	}
	if assign.Parameters[captureClaimDesiredHolderParam] != self.NodeRef {
		t.Fatalf("assign parameters = %#v, want desired holder %s", assign.Parameters, self.NodeRef)
	}
	if assign.Parameters[captureAssignmentDesiredHolderParam] != self.NodeRef {
		t.Fatalf("assign parameters = %#v, want assignment desired holder %s", assign.Parameters, self.NodeRef)
	}
	if assign.Parameters[captureClaimPreviousHolderParam] != "aws-router-a" {
		t.Fatalf("assign parameters = %#v, want previous holder", assign.Parameters)
	}
	if assign.Parameters[captureAssignmentPreviousHolderParam] != "aws-router-a" {
		t.Fatalf("assign parameters = %#v, want assignment previous holder", assign.Parameters)
	}
	if assign.Parameters[captureAssignmentClaimEpochParam] != assign.Parameters[captureClaimGenerationParam] {
		t.Fatalf("assign parameters = %#v, want assignment claim epoch to reference group claim", assign.Parameters)
	}
	if _, err := time.Parse(time.RFC3339Nano, assign.Parameters[captureClaimLeaseUntilParam]); err != nil {
		t.Fatalf("assign parameters = %#v, want RFC3339 leaseUntil: %v", assign.Parameters, err)
	}
	if _, err := time.Parse(time.RFC3339Nano, assign.Parameters[captureAssignmentLeaseUntilParam]); err != nil {
		t.Fatalf("assign parameters = %#v, want RFC3339 assignment leaseUntil: %v", assign.Parameters, err)
	}
}

func TestPlanBGPMobilityDeliveryAssignsPerAddressGenerations(t *testing.T) {
	now := time.Date(2026, 6, 25, 23, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
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
		PoolName:  "cloudedge",
		Source:    DynamicSource("cloudedge", self.NodeRef),
		Self:      self,
		Members:   members,
		Spec:      spec,
		Decisions: decisions,
		Placement: PlacementDecision{
			Group:                 "aws-edge",
			Active:                true,
			ActiveNode:            self.NodeRef,
			Seize:                 true,
			ActiveIdentityNodeRef: "aws-router-a",
			Reason:                "hard-failure",
		},
		InstalledNextHops:    installed,
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	generations := map[string]bool{}
	for _, address := range addresses {
		assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address)
		if assign == nil {
			t.Fatalf("action plans = %#v, want assign for %s", delivery.ActionPlans, address)
		}
		generation := assign.Parameters[captureAssignmentGenerationParam]
		if generation == "" {
			t.Fatalf("assign %s parameters = %#v, want assignment generation", address, assign.Parameters)
		}
		if generations[generation] {
			t.Fatalf("assignment generation %q reused across addresses; plans=%#v", generation, delivery.ActionPlans)
		}
		generations[generation] = true
		if !strings.Contains(assign.IdempotencyKey, ":assigngen:"+safeName(generation)) {
			t.Fatalf("assign %s key = %q, want assignment generation fence", address, assign.IdempotencyKey)
		}
		if got := delivery.CaptureAssignments[address].Generation; got != generation {
			t.Fatalf("delivery assignment %s generation = %q, want %q", address, got, generation)
		}
	}
}

func TestPlanBGPMobilityDeliveryStampsAssignmentGenerationForStandbyCapture(t *testing.T) {
	now := time.Date(2026, 6, 26, 1, 35, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		InstalledNextHops:    map[string][]string{address: {"10.99.0.3"}},
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want standby capture assign", delivery.ActionPlans)
	}
	if assign.Parameters[captureClaimPhaseParam] != "" {
		t.Fatalf("assign parameters = %#v, standby group claim should not stamp claim metadata", assign.Parameters)
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
	if got := delivery.CaptureAssignments[address].Generation; got != generation {
		t.Fatalf("delivery assignment generation = %q, want %q", got, generation)
	}
}

func TestPlanBGPMobilityDeliveryKeepsAssignmentGenerationAcrossGroupClaimChange(t *testing.T) {
	now := time.Date(2026, 6, 25, 23, 20, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	previous := bgpCaptureAssignment{
		Address:        address,
		Phase:          "Active",
		Generation:     "aws-edge/10-88-60-10-32/7",
		Seq:            7,
		ClaimEpoch:     "aws-edge/1",
		DesiredHolder:  self.NodeRef,
		PreviousHolder: "aws-router-a",
		IssuedAt:       now.Add(-time.Minute),
		RenewedAt:      now.Add(-time.Minute),
		LeaseUntil:     now.Add(DefaultLeaseTTL),
	}
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
			Reason:                "hard-failure",
		},
		InstalledNextHops: map[string][]string{address: {"10.99.0.3"}},
		Profiles:          map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		CaptureClaim: bgpCaptureClaim{
			Group:          "aws-edge",
			Phase:          "Active",
			Generation:     "aws-edge/99",
			EpochSeq:       99,
			DesiredHolder:  self.NodeRef,
			PreviousHolder: "aws-router-a",
			Reason:         "hard-failure",
			LeaseUntil:     now.Add(DefaultLeaseTTL),
		},
		CaptureAssignments:   map[string]bgpCaptureAssignment{address: previous},
		CaptureAssignmentSeq: 7,
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address)
	if assign == nil {
		t.Fatalf("action plans = %#v, want assign", delivery.ActionPlans)
	}
	if got := assign.Parameters[captureAssignmentGenerationParam]; got != previous.Generation {
		t.Fatalf("assignment generation = %q, want previous %q despite new group claim", got, previous.Generation)
	}
	if got := assign.Parameters[captureAssignmentClaimEpochParam]; got != "aws-edge/99" {
		t.Fatalf("assignment claim epoch = %q, want current group claim", got)
	}
	if delivery.CaptureAssignmentSeq != 7 {
		t.Fatalf("assignment seq = %d, want unchanged 7", delivery.CaptureAssignmentSeq)
	}
}

func TestPlanBGPMobilityDeliveryPrunesNonDesiredCaptureAssignment(t *testing.T) {
	now := time.Date(2026, 6, 26, 2, 58, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	desired := "10.88.60.10/32"
	stale := "10.88.60.12/32"
	previousDesired := bgpCaptureAssignment{
		Address:       desired,
		Phase:         "Active",
		Generation:    "aws-edge/10-88-60-10-32/3",
		Seq:           3,
		DesiredHolder: self.NodeRef,
		IssuedAt:      now.Add(-time.Minute),
		RenewedAt:     now.Add(-time.Minute),
		LeaseUntil:    now.Add(DefaultLeaseTTL),
	}
	previousStale := bgpCaptureAssignment{
		Address:       stale,
		Phase:         "Active",
		Generation:    "aws-edge/10-88-60-12-32/4",
		Seq:           4,
		DesiredHolder: self.NodeRef,
		IssuedAt:      now.Add(-time.Minute),
		RenewedAt:     now.Add(-time.Minute),
		LeaseUntil:    now.Add(DefaultLeaseTTL),
	}
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
		Decisions: []ownershipDecision{{
			Address:       desired,
			Class:         ownershipClassRemoteHomeOwned,
			HomeOwnerNode: "azure-router",
		}, {
			Address:           stale,
			Class:             ownershipClassStaleCapture,
			HomeOwnerNode:     "azure-router",
			SuppressionReason: "fresh-home-owner",
		}},
		Placement: PlacementDecision{
			Group:      "aws-edge",
			Active:     true,
			ActiveNode: self.NodeRef,
		},
		InstalledNextHops: map[string][]string{desired: {"10.99.0.3"}},
		Profiles:          map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		CaptureAssignments: map[string]bgpCaptureAssignment{
			desired: previousDesired,
			stale:   previousStale,
		},
		CaptureAssignmentSeq: 4,
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if got := delivery.CaptureAssignments[desired].Generation; got != previousDesired.Generation {
		t.Fatalf("desired assignment generation = %q, want previous %q", got, previousDesired.Generation)
	}
	if _, ok := delivery.CaptureAssignments[stale]; ok {
		t.Fatalf("capture assignments = %#v, non-desired assignment %s must be pruned", delivery.CaptureAssignments, stale)
	}
	if delivery.CaptureAssignmentSeq != 4 {
		t.Fatalf("assignment seq = %d, want previous max seq retained", delivery.CaptureAssignmentSeq)
	}
}

func TestBGPCaptureClaimPersistedEpochDoesNotReuseRepeatedTransition(t *testing.T) {
	now := time.Date(2026, 6, 25, 11, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	nodeA := members["aws-router-a"]
	nodeB := members["aws-router-b"]
	status := map[string]any{}

	firstB := bgpCaptureClaimForPlacementWithStatus("cloudedge", nodeB, members, PlacementDecision{
		Group:                 "aws-edge",
		Active:                true,
		ActiveNode:            nodeB.NodeRef,
		Seize:                 true,
		ActiveIdentityNodeRef: nodeA.NodeRef,
	}, status, now)
	if firstB.Generation != "aws-edge/1" || firstB.EpochSeq != 1 {
		t.Fatalf("first B claim = %#v, want aws-edge/1 seq=1", firstB)
	}
	status = map[string]any{
		"bgpCaptureClaim":         bgpCaptureClaimStatus(firstB),
		"bgpCaptureClaimEpochSeq": firstB.EpochSeq,
	}

	activeA := bgpCaptureClaimForPlacementWithStatus("cloudedge", nodeA, members, PlacementDecision{
		Group:                 "aws-edge",
		Active:                true,
		ActiveNode:            nodeA.NodeRef,
		Seize:                 true,
		ActiveIdentityNodeRef: nodeB.NodeRef,
	}, status, now.Add(time.Minute))
	if activeA.Generation != "aws-edge/2" || activeA.EpochSeq != 2 {
		t.Fatalf("active A claim = %#v, want aws-edge/2 seq=2", activeA)
	}
	status = map[string]any{
		"bgpCaptureClaim":         bgpCaptureClaimStatus(activeA),
		"bgpCaptureClaimEpochSeq": activeA.EpochSeq,
	}

	secondB := bgpCaptureClaimForPlacementWithStatus("cloudedge", nodeB, members, PlacementDecision{
		Group:                 "aws-edge",
		Active:                true,
		ActiveNode:            nodeB.NodeRef,
		Seize:                 true,
		ActiveIdentityNodeRef: nodeA.NodeRef,
	}, status, now.Add(2*time.Minute))
	if secondB.Generation != "aws-edge/3" || secondB.EpochSeq != 3 {
		t.Fatalf("second B claim = %#v, want aws-edge/3 seq=3", secondB)
	}
	if secondB.Generation == firstB.Generation {
		t.Fatalf("repeated A->B reused generation %q", secondB.Generation)
	}
}

func TestBGPCaptureClaimRestoresActiveEpochAndRenewsLease(t *testing.T) {
	now := time.Date(2026, 6, 25, 11, 45, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	nodeA := members["aws-router-a"]
	nodeB := members["aws-router-b"]
	placement := PlacementDecision{
		Group:                 "aws-edge",
		Active:                true,
		ActiveNode:            nodeB.NodeRef,
		Seize:                 true,
		ActiveIdentityNodeRef: nodeA.NodeRef,
	}
	issued := bgpCaptureClaimForPlacementWithStatus("cloudedge", nodeB, members, placement, nil, now)
	restarted := bgpCaptureClaimForPlacementWithStatus("cloudedge", nodeB, members, placement, map[string]any{
		"bgpCaptureClaim":         bgpCaptureClaimStatus(issued),
		"bgpCaptureClaimEpochSeq": issued.EpochSeq,
	}, now.Add(time.Minute))

	if restarted.Generation != issued.Generation || restarted.EpochSeq != issued.EpochSeq {
		t.Fatalf("restarted claim = %#v, issued = %#v, want same epoch", restarted, issued)
	}
	if !restarted.IssuedAt.Equal(issued.IssuedAt) {
		t.Fatalf("issuedAt = %s, want %s", restarted.IssuedAt, issued.IssuedAt)
	}
	if !restarted.RenewedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("renewedAt = %s, want restart reconcile time", restarted.RenewedAt)
	}
	if !restarted.LeaseUntil.After(issued.LeaseUntil) {
		t.Fatalf("leaseUntil = %s, want after initial %s", restarted.LeaseUntil, issued.LeaseUntil)
	}
}

func TestBGPCaptureClaimDistinguishesGracefulDrainAndHardFailure(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	members := plannerMembers(spec.Members)
	nodeA := members["aws-router-a"]
	nodeB := members["aws-router-b"]

	hardFailure := bgpCaptureClaimForPlacementWithStatus("cloudedge", nodeB, members, PlacementDecision{
		Group:                 "aws-edge",
		Active:                true,
		ActiveNode:            nodeB.NodeRef,
		Seize:                 true,
		ActiveIdentityNodeRef: nodeA.NodeRef,
	}, nil, now)
	if hardFailure.Reason != "hard-failure" {
		t.Fatalf("hard failure reason = %q, want hard-failure", hardFailure.Reason)
	}

	initialA := bgpCaptureClaimForPlacementWithStatus("cloudedge", nodeA, members, PlacementDecision{
		Group:      "aws-edge",
		Active:     true,
		ActiveNode: nodeA.NodeRef,
	}, nil, now)
	drainedMembers := plannerMembers(spec.Members)
	drainedA := drainedMembers[nodeA.NodeRef]
	drainedA.MaintenanceDrain = true
	drainedMembers[nodeA.NodeRef] = drainedA
	graceful := bgpCaptureClaimForPlacementWithStatus("cloudedge", nodeB, drainedMembers, PlacementDecision{
		Group:      "aws-edge",
		Active:     true,
		ActiveNode: nodeB.NodeRef,
	}, map[string]any{
		"bgpCaptureClaim":         bgpCaptureClaimStatus(initialA),
		"bgpCaptureClaimEpochSeq": initialA.EpochSeq,
	}, now.Add(time.Minute))
	if graceful.Reason != "graceful-drain" {
		t.Fatalf("graceful reason = %q, want graceful-drain", graceful.Reason)
	}
}

func TestControllerBGPModeStandbyReleasesConfirmedCaptureWhenActiveMarkerReturns(t *testing.T) {
	now := time.Date(2026, 6, 13, 22, 5, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.12/32"
	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, self.CaptureTarget, address, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	stampBGPPathFenceActionPlans(previous, address, "prefix="+address+";nextHops=10.99.0.3", self.NodeRef, now.Add(-time.Minute))
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		PreviousPlans:        previous,
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{address: true},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if candidate, ok := delivery.CaptureCandidates[address]; ok && candidate.ProtectOnly {
		t.Fatalf("capture candidates = %#v, standby holder must release after configured active liveness returns", delivery.CaptureCandidates)
	}
	if unassign := findActionPlanByAddress(delivery.ActionPlans, "unassign-secondary-ip", address); unassign == nil {
		t.Fatalf("action plans = %#v, standby confirmed holder must release after configured active liveness returns", delivery.ActionPlans)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, protect-only capture must not reassign", delivery.ActionPlans)
	}
}

func TestControllerBGPModeStandbyReleasesObservedSelfCaptureWithoutPriorAction(t *testing.T) {
	now := time.Date(2026, 6, 14, 21, 40, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
			Source:             staticOwnedType,
			SuppressionReason:  "static-owned-by-remote",
		}},
		Placement: PlacementDecision{
			Group:               "aws-edge",
			Active:              false,
			ActiveNode:          "aws-router-a",
			ActiveMarkerPresent: true,
			Reason:              "configured active has returned",
		},
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{address: true},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if unassign := findActionPlanByAddress(delivery.ActionPlans, "unassign-secondary-ip", address); unassign == nil {
		t.Fatalf("action plans = %#v, standby observed self-capture must release once active liveness is present", delivery.ActionPlans)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, standby observed self-capture must not reassign", delivery.ActionPlans)
	}
}

func TestPlanBGPMobilityDeliveryReleasesProviderConflictLoserCapture(t *testing.T) {
	now := time.Date(2026, 6, 14, 21, 42, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{address: true},
		ObservedSelfIPsOK:    true,
		ObservedStaleSince:   map[string]time.Time{address: now.Add(-3 * time.Minute)},
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if unassign := findActionPlanByAddress(delivery.ActionPlans, "unassign-secondary-ip", address); unassign == nil {
		t.Fatalf("action plans = %#v, conflict loser self-capture must be released after hold-down", delivery.ActionPlans)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, conflict loser must not reassign", delivery.ActionPlans)
	}
}

func TestControllerBGPModeStandbyKeepsObservedSelfCaptureWhileActiveMarkerAbsent(t *testing.T) {
	now := time.Date(2026, 6, 14, 21, 45, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.10/32"
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
			Source:             staticOwnedType,
			SuppressionReason:  "static-owned-by-remote",
		}},
		Placement: PlacementDecision{
			Group:               "aws-edge",
			Active:              false,
			ActiveNode:          "aws-router-a",
			ActiveMarkerPresent: false,
			Reason:              "configured active marker absent",
		},
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{address: true},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if len(delivery.CaptureCandidates) != 1 || !delivery.CaptureCandidates[address].ProtectOnly {
		t.Fatalf("capture candidates = %#v, standby holder must stay protected while active liveness is absent", delivery.CaptureCandidates)
	}
	if unassign := findActionPlanByAddress(delivery.ActionPlans, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("action plans = %#v, standby observed self-capture must not release before active liveness returns", delivery.ActionPlans)
	}
}

func TestControllerBGPModeStandbyReleaseSkipsAbsentObservedSelfCapture(t *testing.T) {
	now := time.Date(2026, 6, 14, 17, 30, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-b"]
	address := "10.88.60.12/32"
	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, self.CaptureTarget, address, map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	stampBGPPathFenceActionPlans(previous, address, "prefix="+address+";nextHops=10.99.0.3", self.NodeRef, now.Add(-time.Minute))
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
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
		PreviousPlans:        previous,
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if unassign := findActionPlanByAddress(delivery.ActionPlans, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("action plans = %#v, absent fresh self inventory must not generate redundant unassign", delivery.ActionPlans)
	}
}

func TestControllerBGPModeProtectsObservedRemoteHomeCaptureFromUnassign(t *testing.T) {
	now := time.Date(2026, 6, 13, 22, 10, 0, 0, time.UTC)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	members := plannerMembers(spec.Members)
	self := members["aws-router-a"]
	address := "10.88.60.10/32"
	previous, err := providerActionPlans("cloudedge", api.CloudProviderProfileSpec{Provider: "aws"}, self.Capture, self.CaptureTarget, address, map[string]bool{}, false)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	stampBGPPathFenceActionPlans(previous, address, "prefix="+address+";nextHops=10.99.0.1", self.NodeRef, now.Add(-time.Minute))
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName: "cloudedge",
		Source:   DynamicSource("cloudedge", self.NodeRef),
		Self:     self,
		Members:  members,
		Spec:     spec,
		Decisions: []ownershipDecision{{
			Address:            address,
			Class:              ownershipClassRemoteHomeOwned,
			HomeOwnerNode:      "onprem-router",
			Source:             staticOwnedType,
			SuppressionReason:  "static-owned-by-remote",
			CaptureState:       captureStateStale,
			CaptureHolderNode:  self.NodeRef,
			CaptureProviderRef: "aws-provider",
			CaptureTargetRef:   "eni-a",
			CaptureStrategy:    captureStrategySecondaryIP,
		}},
		Placement:            PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: self.NodeRef},
		PreviousPlans:        previous,
		Profiles:             map[string]api.CloudProviderProfileSpec{"aws-provider": {Provider: "aws"}},
		ObservedSelfCaptures: map[string]bool{address: true},
		ObservedSelfIPsOK:    true,
		RIBObserved:          true,
		Now:                  now,
	})
	if err != nil {
		t.Fatalf("planBGPMobilityDelivery: %v", err)
	}
	if len(delivery.CaptureCandidates) != 1 || !delivery.CaptureCandidates[address].ProtectOnly {
		t.Fatalf("capture candidates = %#v, want observed remote-home capture protected", delivery.CaptureCandidates)
	}
	if unassign := findActionPlanByAddress(delivery.ActionPlans, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("action plans = %#v, observed desired remote-home capture must not unassign", delivery.ActionPlans)
	}
	if assign := findActionPlanByAddress(delivery.ActionPlans, "assign-secondary-ip", address); assign != nil {
		t.Fatalf("action plans = %#v, protect-only capture must not reassign", delivery.ActionPlans)
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
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.3"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, nil, nil)

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

func TestBGPProviderDeprovisionUnassignDoesNotRecapture(t *testing.T) {
	now := time.Date(2026, 6, 24, 16, 40, 0, 0, time.UTC)
	self := memberPlanInfo{
		NodeRef: "aws-router-a",
		Capture: api.AddressCapture{
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
	if shouldAllowBGPTrapReassignment(self, address, nil, journal, map[string]bool{address: true}, true, now) {
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
	stampBGPProviderTransitionFence(plans, self, address, journal, map[string]bool{address: true}, true, now)
	if plans[0].Parameters[bgpTrapTransitionParam] != "" || strings.Contains(plans[0].IdempotencyKey, ":transition:") {
		t.Fatalf("plan = %#v, deprovision unassign must not stamp transition recapture", plans[0])
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

func TestControllerBGPModeProviderTrapHoldsRecentProviderMissingObservation(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", "10.88.60.10/32", "assign-secondary-ip", 1, now.Add(-5*time.Second))
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.11"},
		"discoverySelfCapturedAddresses": []string{},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
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
		t.Fatalf("plans = %#v, want desired provider assign retained", plans)
	}
	if assign.Parameters[bgpTrapTransitionParam] != "" || strings.Contains(assign.IdempotencyKey, ":transition:provider-missing-") {
		t.Fatalf("assign key/parameters = %q %#v, recent provider-missing observation must not churn a new transition", assign.IdempotencyKey, assign.Parameters)
	}
	if assign.Parameters["allowReassignment"] == "true" {
		t.Fatalf("assign parameters = %#v, recent provider-missing observation must not force reassignment", assign.Parameters)
	}
}

func TestControllerBGPModeUnobservedHistoricalCaptureDoesNotUnassign(t *testing.T) {
	now := time.Date(2026, 6, 14, 21, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPStatus(t, store, map[string][]string{
		address: {"10.99.0.3"},
	}, []map[string]any{
		bgpOwnerPrefix(address, "10.99.0.3", "azure-router"),
	}, nil)

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if unassign := findActionPlanByAddress(plans, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("plans = %#v, historical capture without provider observation must not be destructively unassigned", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	decisions := ownershipStatusDecisions(t, status["ownershipResolverDecisions"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] == ownershipClassConfirmedCapture || decision["captureState"] == captureStateConfirmed {
		t.Fatalf("decision = %#v, action journal without provider observation must not confirm capture", decision)
	}
}

func TestControllerBGPModeRemoteProviderTrapRecapturesWithoutSelfMarkerMatch(t *testing.T) {
	now := time.Date(2026, 6, 13, 20, 40, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.13/32"
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-oci-client",
		Group:      "cloudedge",
		SourceNode: "oci-router",
		Type:       ObservedEventType,
		Subject:    address,
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
		Payload: map[string]string{
			"source": providerDiscoverySource,
			"pool":   "cloudedge",
		},
	})
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-3*time.Minute))
	saveBGPStatus(t, store, map[string][]string{
		address: {"10.99.0.2"},
	}, []map[string]any{
		bgpOwnerPrefix(address, "10.99.0.2", "oci-router"),
	}, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.0.2/32",
	})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs": []string{"10.88.60.4/32"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus(MobilityPool/cloudedge): %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, "assign-secondary-ip", address)
	if assign == nil {
		t.Fatalf("plans = %#v, want recapture assign for remote provider trap with installed BGP path", plans)
	}
	if assign.Parameters[bgpPathSigParam] != "prefix=10.88.60.13/32;nextHops=10.99.0.2" {
		t.Fatalf("assign parameters = %#v, want installed path signature", assign.Parameters)
	}
	if assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign parameters = %#v, want reassignment after provider-observed loss", assign.Parameters)
	}
}

func TestControllerBGPModeRouteTableAdvertisesRouterSelfReturnRouteWithoutCapture(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[1].Capture.ProviderMode = captureStrategyRouteTable
	spec.Members[1].Capture.CaptureStrategy = captureStrategyRouteTable
	spec.Members[1].Capture.Target = map[string]string{
		"region":           "japaneast",
		"routeTableRef":    "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/routeTables/rt-cloudedge",
		"nextHopIPAddress": "10.88.60.4",
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryOwnedAddresses": []string{"10.88.60.11/32"},
		"discoveryLocalInventory": []map[string]any{
			{"address": "10.88.60.4/32", "nicRef": "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic", "subnetRef": "/subnets/azure", "providerRef": "azure-provider", "resourceType": "router-nic", "primary": true},
			{"address": "10.88.60.11/32", "nicRef": "/subscriptions/sub-1/resourceGroups/rg-app/providers/Microsoft.Network/networkInterfaces/client-nic", "subnetRef": "/subnets/azure", "providerRef": "azure-provider", "resourceType": "instance-nic"},
		},
		"discoverySelfPrivateIPs":      []string{"10.88.60.4"},
		"discoverySelfPrimaryObserved": true,
		"discoveryLastScanAt":          now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.4/32":  {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.1"},
	})

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "azure-router")).ActionPlansJSON)
	if findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.4/32") != nil ||
		findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, want no provider capture assign for router self or local same-subnet home", plans)
	}
	selfPath := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "azure-router"), "10.88.60.4/32")
	if !stringSliceContains(selfPath.Attrs.Communities, bgpstate.MobilityCommunityReturnRoute) {
		t.Fatalf("self path attrs = %#v, want return-route community", selfPath.Attrs)
	}
	if stringSliceContains(selfPath.Attrs.Communities, bgpstate.MobilityCommunityOwner) {
		t.Fatalf("self path attrs = %#v, router return-route must not be a mobility owner path", selfPath.Attrs)
	}
	pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "azure-router"), "10.88.60.11/32")
}

func TestBGPMobilityProviderCapturePathCarriesNodeIdentityOnly(t *testing.T) {
	attrs := bgpMobilityPathAttrs(memberPlanInfo{
		NodeRef: "aws-router-a",
		Role:    "cloud",
	}, "provider-capture", true)
	if !stringSliceContains(attrs.Communities, bgpstate.MobilityNodeIdentityCommunity("aws-router-a")) {
		t.Fatalf("communities = %#v, want node identity on provider-capture path", attrs.Communities)
	}
	if !stringSliceContains(attrs.Communities, bgpMobilityCommunitySourceCapture) {
		t.Fatalf("communities = %#v, want provider-capture source community", attrs.Communities)
	}
	if stringSliceContains(attrs.Communities, bgpMobilityCommunityOwner) || stringSliceContains(attrs.Communities, bgpMobilityCommunityActiveHolder) {
		t.Fatalf("communities = %#v, provider-capture path must not advertise ownership or active holdership", attrs.Communities)
	}
}

func TestControllerBGPModeProviderTrapRejectsUnknownBGPOnlyAddress(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 2, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.44/32": {"10.99.0.200"},
	})

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "azure-router")).ActionPlansJSON)
	if findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.44/32") != nil {
		t.Fatalf("plans = %#v, want BGP-only unknown address to stay out of provider capture", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	decisions := ownershipStatusDecisions(t, status["ownershipResolverDecisions"])
	decision := ownershipStatusDecisionByAddress(t, decisions, "10.88.60.44/32")
	if decision["class"] != ownershipClassUnknown || decision["source"] != "bgp-rib" {
		t.Fatalf("decision = %#v, want unknown bgp-rib address", decision)
	}
}

func TestControllerBGPModeReturnRouteDoesNotBecomeUnknownClaim(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 3, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	saveBGPStatus(t, store,
		map[string][]string{
			"10.88.60.4/32": {"10.99.0.2"},
		},
		[]map[string]any{
			{
				"prefix":  "10.88.60.4/32",
				"nextHop": "10.99.0.2",
				"best":    true,
				"valid":   true,
				"communities": []string{
					bgpstate.MobilityCommunityReturnRoute,
					bgpstate.MobilityNodeIdentityCommunity("aws-router-a"),
				},
			},
		},
		nil,
	)

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	decisions := ownershipStatusDecisions(t, status["ownershipResolverDecisions"])
	for _, decision := range decisions {
		if decision["address"] == "10.88.60.4/32" {
			t.Fatalf("return-route leaked into ownership resolver decisions: %#v", decision)
		}
	}
	unknown := ownershipStatusDecisions(t, status["ownershipResolverUnknownClaims"])
	for _, claim := range unknown {
		if claim["address"] == "10.88.60.4/32" {
			t.Fatalf("return-route leaked into unknown claims: %#v", claim)
		}
	}
}

func TestControllerBGPModeRouteTableWrongLocalUDRIsDeprovisioned(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[1].Capture.ProviderMode = captureStrategyRouteTable
	spec.Members[1].Capture.CaptureStrategy = captureStrategyRouteTable
	spec.Members[1].Capture.Target = map[string]string{
		"region":           "japaneast",
		"routeTableRef":    "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/routeTables/rt-cloudedge",
		"nextHopIPAddress": "10.88.60.4",
	}
	source := DynamicSource("cloudedge", "azure-router")
	previous := []dynamicconfig.ActionPlan{
		routeTableAssignPlan("cloudedge", "azure", "azure-provider", "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic", "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/routeTables/rt-cloudedge", "10.88.60.4/32", now.Add(-time.Minute)),
		routeTableAssignPlan("cloudedge", "azure", "azure-provider", "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic", "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/routeTables/rt-cloudedge", "10.88.60.11/32", now.Add(-time.Minute)),
	}
	rawPrevious, err := json.Marshal(previous)
	if err != nil {
		t.Fatalf("marshal previous plans: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:          source,
		Generation:      dynamicGeneration,
		ObservedAt:      now.Add(-time.Minute),
		ExpiresAt:       now.Add(time.Hour),
		ActionPlansJSON: string(rawPrevious),
		Status:          "active",
	}); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryLocalInventory": []map[string]any{
			{"address": "10.88.60.4/32", "nicRef": "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic", "subnetRef": "/subnets/azure", "providerRef": "azure-provider", "resourceType": "router-nic", "primary": true},
			{"address": "10.88.60.11/32", "nicRef": "/subscriptions/sub-1/resourceGroups/rg-app/providers/Microsoft.Network/networkInterfaces/client-nic", "subnetRef": "/subnets/azure", "providerRef": "azure-provider", "resourceType": "instance-nic"},
		},
		"discoverySelfPrivateIPs": []string{"10.88.60.4"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.4/32":  {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.1"},
	})

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	if findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.4/32") != nil ||
		findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, want wrong local UDR assign removed from desired set", plans)
	}
	if findActionPlanByAddress(plans, actionUnassignSecondaryIP, "10.88.60.4/32") == nil ||
		findActionPlanByAddress(plans, actionUnassignSecondaryIP, "10.88.60.11/32") == nil {
		t.Fatalf("plans = %#v, want wrong local UDR deprovisioned", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	decisions := ownershipStatusDecisions(t, status["ownershipResolverDecisions"])
	selfDecision := ownershipStatusDecisionByAddress(t, decisions, "10.88.60.4/32")
	if selfDecision["class"] != ownershipClassStaleCapture || selfDecision["suppressionReason"] != "local-router-self" {
		t.Fatalf("self decision = %#v, want local router self stale capture", selfDecision)
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
	if err := controller.reconcileBGPDelivery(context.Background(), res, spec, map[string]any{"phase": "Disabled"}, now); err != nil {
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
	spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
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
	if findActionPlanByAddress(stalePlans, "unassign-secondary-ip", "10.88.60.10/32") != nil {
		t.Fatalf("sustained empty plans = %#v, provider-secondary BGP delivery must not unassign stale trap automatically", stalePlans)
	}
}

func TestControllerBGPModeDeprovisionDoesNotRegenerateFromActionJournal(t *testing.T) {
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
	if unassign := findActionPlanByAddress(plans, "unassign-secondary-ip", "10.88.60.10/32"); unassign != nil {
		t.Fatalf("plans = %#v, provider-secondary BGP delivery must not regenerate unassign from action journal", plans)
	}
}

func TestControllerBGPModeStaleActionOnlyDoesNotRecreateCapture(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPInstalledNextHops(t, store, map[string][]string{address: {"10.99.0.200"}})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs": []string{"10.88.60.4/32"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if findActionPlanByAddress(plans, "assign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, stale provider-action state must not recreate missing capture", plans)
	}
	if findActionPlanByAddress(plans, "unassign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, provider-secondary BGP delivery must not clean stale provider-action state by unassign", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	decisions := ownershipStatusDecisions(t, status["ownershipResolverDecisions"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] != ownershipClassStaleCapture || decision["suppressionReason"] != "capture-not-desired" {
		t.Fatalf("decision = %#v, want capture-not-desired stale capture", decision)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureIsProtectedWithoutPriorAction(t *testing.T) {
	now := time.Date(2026, 6, 10, 18, 35, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.10/32"
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{address},
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
	if findActionPlanByAddress(plans, "assign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, observed stale capture must not recreate assign", plans)
	}
	if unassign := findActionPlanByAddress(plans, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("plans = %#v, first observed stale provider-secondary capture must wait for cleanup hold", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	staleSince := observedSelfStaleCaptureSinceFromStatus(status)
	if staleSince[address].IsZero() {
		t.Fatalf("observedSelfStaleCaptures = %#v, want first-seen marker for %s", status["observedSelfStaleCaptures"], address)
	}
	decisions := ownershipStatusDecisions(t, status["ownershipResolverDecisions"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] != ownershipClassStaleCapture || decision["suppressionReason"] != "self-captured-secondary" {
		t.Fatalf("decision = %#v, want self-captured-secondary stale capture", decision)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureWaitsForRecentTrapMissingHold(t *testing.T) {
	now := time.Date(2026, 6, 10, 18, 37, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	source := DynamicSource("cloudedge", "aws-router-a")
	address := "10.88.60.12/32"
	previousPlans, err := json.Marshal([]dynamicconfig.ActionPlan{{
		Name:        "mobility-cloudedge-assign-10-88-60-12-32",
		Provider:    "aws",
		ProviderRef: "aws-provider",
		Action:      "assign-secondary-ip",
		Target: map[string]string{
			"address":     address,
			"nicRef":      "eni-a",
			"provider":    "aws",
			"providerRef": "aws-provider",
		},
		Parameters: map[string]string{
			bgpPathSigParam:        "prefix=10.88.60.12/32;nextHops=10.99.0.3",
			bgpTrapLastSeenAtParam: now.Add(-time.Minute).Format(time.RFC3339Nano),
			captureParamHolder:     "aws-router-a",
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
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	if findActionPlanByAddress(plans, "unassign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, recent BGP trap must hold observed self stale cleanup during convergence", plans)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureWithInstalledReturnRouteIsCleaned(t *testing.T) {
	now := time.Date(2026, 6, 10, 18, 40, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.12/32"
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.3"}}, []map[string]any{{
		"prefix":      address,
		"nextHop":     "10.99.0.3",
		"best":        true,
		"valid":       true,
		"communities": []string{bgpstate.MobilityCommunityReturnRoute},
	}}, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		"observedSelfStaleCaptures":      map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if findActionPlanByAddress(plans, "unassign-secondary-ip", address) == nil {
		t.Fatalf("plans = %#v, stale self-capture must be cleaned when it is not a capture candidate even if a return route exists", plans)
	}
}

func TestControllerBGPModeSucceededSelfCapturedStaleDoesNotUnassignObservedSecondaryIP(t *testing.T) {
	now := time.Date(2026, 6, 10, 18, 41, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-5*time.Minute))
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.2"}}, []map[string]any{
		bgpOwnerPrefix(address, "10.99.0.2", "aws-router-a"),
	}, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		"observedSelfStaleCaptures":      map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if unassign := findActionPlanByAddress(plans, "unassign-secondary-ip", address); unassign != nil {
		t.Fatalf("plans = %#v, succeeded observed secondary IP must be retained instead of churned", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	decisions := ownershipStatusDecisions(t, status["ownershipResolverDecisions"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] != ownershipClassStaleCapture || decision["suppressionReason"] != "self-captured-secondary" {
		t.Fatalf("decision = %#v, want succeeded self-captured-secondary stale capture", decision)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureWithInstalledOwnerPathIsProtected(t *testing.T) {
	now := time.Date(2026, 6, 10, 18, 42, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-3*time.Minute))
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.3"}}, []map[string]any{
		bgpOwnerPrefix(address, "10.99.0.3", "azure-router"),
	}, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		"observedSelfStaleCaptures":      map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if findActionPlanByAddress(plans, "unassign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, installed valid provider-secondary capture must be protected by capture candidate computation", plans)
	}
	if findActionPlanByAddress(plans, "assign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, observed valid provider-secondary capture must not be reassigned", plans)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureUsesDiscoveredSelfNIC(t *testing.T) {
	now := time.Date(2026, 6, 10, 19, 35, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[3].Capture.NICRef = ""
	spec.Members[3].OwnershipDiscovery.Mode = "provider-private-ip"
	discoveredNIC := "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic"
	address := "10.88.60.10/32"
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfNICRef":            discoveredNIC,
		"discoverySelfPrivateIPs":        []string{"10.88.60.22/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		"observedSelfStaleCaptures":      map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "azure-router")).ActionPlansJSON)
	if unassign := findActionPlanByAddress(plans, "unassign-secondary-ip", address); unassign == nil {
		t.Fatalf("plans = %#v, observed stale provider-secondary capture must be deprovisioned via discovered NIC cleanup", plans)
	} else if unassign.Target["nicRef"] != discoveredNIC {
		t.Fatalf("unassign target = %#v, want discovered nicRef %q", unassign.Target, discoveredNIC)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureUsesCaptureTargetNIC(t *testing.T) {
	now := time.Date(2026, 6, 10, 20, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[3].Capture.NICRef = ""
	if spec.Members[3].Capture.Target == nil {
		spec.Members[3].Capture.Target = map[string]string{}
	}
	spec.Members[3].Capture.Target["nicRef"] = "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/target-router-nic"
	address := "10.88.60.10/32"
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		"observedSelfStaleCaptures":      map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("azure-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "azure-router")).ActionPlansJSON)
	if unassign := findActionPlanByAddress(plans, "unassign-secondary-ip", address); unassign == nil {
		t.Fatalf("plans = %#v, observed stale provider-secondary capture must be deprovisioned via capture-target NIC cleanup", plans)
	} else if unassign.Target["nicRef"] != spec.Members[3].Capture.Target["nicRef"] {
		t.Fatalf("unassign target = %#v, want capture target nicRef %q", unassign.Target, spec.Members[3].Capture.Target["nicRef"])
	}
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
	capture := api.AddressCapture{
		Type:        "provider-secondary-ip",
		ProviderRef: "aws-provider",
		NICRef:      "eni-a",
	}
	captureTarget := map[string]string{"nicRef": "eni-a", "region": "ap-northeast-1"}
	address := "10.88.60.10/32"
	holder := "aws-router-a"
	firstSeen := time.Date(2026, 6, 10, 18, 45, 0, 0, time.UTC)

	planFor := func(staleSince time.Time) dynamicconfig.ActionPlan {
		t.Helper()
		plan, err := providerUnassignActionPlan("cloudedge", profile, capture, captureTarget, address, time.Date(2026, 6, 10, 18, 50, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("providerUnassignActionPlan: %v", err)
		}
		return stampSingleBGPPathFence(plan, address, bgpPathSigFromObservedSelfStale(address, staleSince), holder)
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

func TestControllerBGPModeRemoteHomeLocalInventoryConflictBlocksProviderAction(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 15, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.11/32"
	recordEvent(t, store, providerDiscoveryObservedEvent("cloudedge", "cloudedge", "oci-router", address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
		Address:      "10.88.60.11",
		NICRef:       "oci-client",
		SubnetRef:    "oci-subnet",
		ResourceRef:  "ocid1.instance.oc1.test.client",
		ResourceType: "instance-nic",
	}, now.Add(-time.Second), time.Hour))
	saveBGPInstalledNextHops(t, store, map[string][]string{address: {"10.99.0.200"}})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoveryLocalInventory": []map[string]any{
			{
				"address":      address,
				"nicRef":       "eni-client",
				"subnetRef":    "subnet-a",
				"providerRef":  "aws-provider",
				"resourceRef":  "i-aws-client",
				"resourceType": "instance-nic",
			},
		},
		"discoverySelfPrivateIPs": []string{"10.88.60.4/32"},
		"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if findActionPlanByAddress(plans, "assign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, conflict must not generate provider capture action", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "Degraded" || status["providerActionPhase"] != "Blocked" || status["ownershipResolverPhase"] != "Conflict" {
		t.Fatalf("status = %#v, want degraded blocked conflict", status)
	}
	conflicts := ownershipStatusDecisions(t, status["ownershipResolverConflicts"])
	if len(conflicts) != 1 || conflicts[0]["address"] != address || conflicts[0]["localNodeRef"] != "aws-router-a" {
		t.Fatalf("conflicts = %#v, want local/remote conflict row", conflicts)
	}
	ownerTable := ownershipStatusDecisions(t, status["ownershipResolverOwnerTable"])
	row := ownershipStatusDecisionByAddress(t, ownerTable, address)
	if row["state"] != "Conflict" || row["ownerNode"] != "oci-router" || row["ownerProviderRef"] != "oci-provider" || row["localNode"] != "aws-router-a" || row["localProviderRef"] != "aws-provider" {
		t.Fatalf("owner table row = %#v, want central monitoring row", row)
	}
}

func TestControllerBGPModeSucceededStaleCaptureDoesNotCarryPreviousTrap(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 20, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	source := DynamicSource("cloudedge", "aws-router-a")
	address := "10.88.60.12/32"
	previousPlans, err := json.Marshal([]dynamicconfig.ActionPlan{{
		Name:        "mobility-cloudedge-assign-10-88-60-12-32",
		Provider:    "aws",
		ProviderRef: "aws-provider",
		Action:      "assign-secondary-ip",
		Target: map[string]string{
			"address":     address,
			"nicRef":      "eni-a",
			"provider":    "aws",
			"providerRef": "aws-provider",
		},
		Parameters: map[string]string{
			bgpPathSigParam:        "prefix=10.88.60.12/32;nextHops=10.99.0.200",
			bgpTrapLastSeenAtParam: now.Add(-time.Minute).Format(time.RFC3339Nano),
			captureParamHolder:     "aws-router-a",
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
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-30*time.Second))
	saveBGPInstalledNextHops(t, store, map[string][]string{})
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	if findActionPlanByAddress(plans, "assign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, succeeded stale capture must not carry previous trap after provider cleanup", plans)
	}
}

func TestControllerBGPModeConfirmedCaptureDoesNotDeprovision(t *testing.T) {
	now := time.Date(2026, 6, 10, 14, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.3"}}, nil, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{address},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "aws-router-a"), address); ok {
		t.Fatalf("paths = %#v, provider capture must not advertise home ownership", bgp.paths)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if findActionPlanByAddress(plans, "unassign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, want confirmed capture protected from deprovision", plans)
	}
	if findActionPlanByAddress(plans, "assign-secondary-ip", address) != nil {
		t.Fatalf("plans = %#v, want no new assign plan for already confirmed capture", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	decisions := ownershipStatusDecisions(t, status["ownershipResolverDecisions"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] != ownershipClassConfirmedCapture {
		t.Fatalf("decision = %#v, want ConfirmedCapture", decision)
	}
}

func TestControllerBGPModeProtectOnlyCaptureKeepsForwardingEnabled(t *testing.T) {
	now := time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	confirmed := "10.88.60.12/32"
	stale := "10.88.60.10/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", confirmed, "assign-secondary-ip", 1, now.Add(-2*time.Minute))
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", stale, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPStatus(t, store, map[string][]string{confirmed: {"10.99.0.3"}}, nil, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{confirmed},
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
	if findActionPlanByAddress(plans, "unassign-secondary-ip", stale) != nil {
		t.Fatalf("plans = %#v, stale capture absent from provider inventory must not be unassigned while confirmed capture remains", plans)
	}
	if findActionPlan(plans, "ensure-forwarding-disabled") != nil {
		t.Fatalf("plans = %#v, must not disable forwarding while confirmed capture remains on same provider target", plans)
	}
}

func TestControllerBGPModeStaleCaptureCleanupKeepsForwardingReady(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	stale := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", stale, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"discoverySelfPrivateIPs":        []string{"10.88.60.4/32"},
		"discoverySelfCapturedAddresses": []string{stale},
		"discoveryLastScanAt":            now.Format(time.RFC3339Nano),
		"observedSelfStaleCaptures":      map[string]string{stale: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-a", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", "aws-router-a")).ActionPlansJSON)
	if findActionPlanByAddress(plans, "unassign-secondary-ip", stale) == nil {
		t.Fatalf("plans = %#v, stale provider-secondary capture must be automatically unassigned", plans)
	}
	if findActionPlan(plans, "ensure-forwarding-disabled") != nil {
		t.Fatalf("plans = %#v, BGP SAM router candidates must keep provider forwarding ready after capture cleanup", plans)
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

func reconcileBGPProfileEquivalence(t *testing.T, selfNode string, spec api.MobilityPoolSpec, now time.Time) ([]bgpdaemon.AppliedPath, []dynamicconfig.ActionPlan) {
	t.Helper()
	store := testStore(t, now)
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-" + selfNode,
		Group:      "cloudedge",
		SourceNode: selfNode,
		Type:       ObservedEventType,
		Subject:    "10.88.60.11/32",
		ObservedAt: now.Add(-time.Second),
		ExpiresAt:  now.Add(time.Hour),
	})
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.3"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, nil, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity(selfNode): "10.99.0.6/32",
	})
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", selfNode, spec, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity(selfNode): "10.99.0.6/32",
	}, now)
	bgp := &fakeBGPPaths{}
	router := routerWithBGPRouter(routerWithEventGroupListen(planningRouterForNode(selfNode, spec), "10.99.0.6"))
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile(%s): %v", selfNode, err)
	}
	var paths []bgpdaemon.AppliedPath
	for _, path := range bgp.paths {
		if path.Source == DynamicSource("cloudedge", selfNode) {
			paths = append(paths, path)
		}
	}
	sort.SliceStable(paths, func(i, j int) bool { return paths[i].Prefix < paths[j].Prefix })
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", selfNode)).ActionPlansJSON)
	return paths, plans
}

func profileAWSFailoverPoolSpecForNode(selfNode string) api.MobilityPoolSpec {
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Values = map[string]string{
		"aws.region": "ap-northeast-1",
		"aws.nic":    map[string]string{"aws-router-a": "eni-a", "aws-router-b": "eni-b"}[selfNode],
	}
	spec.Profiles = api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{
		"aws-edge": {
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "aws-provider",
				ProviderMode: "nic-secondary-ip",
				TargetFrom:   map[string]string{"region": "aws.region"},
				NICRef:       spec.Values["aws.nic"],
			},
		},
	}}
	spec.Members = []api.MobilityPoolMember{
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
				t.Fatalf("ownershipResolverDecisions item = %#v, want map[string]any", item)
			}
			out = append(out, m)
		}
		return out
	default:
		t.Fatalf("ownershipResolverDecisions = %#v, want slice", raw)
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
		"plannerPhase": "Planned",
	}); err != nil {
		t.Fatalf("savePlannerStatus: %v", err)
	}
	discovery.saveDiscoveryStatus("cloudedge", map[string]any{
		"discoveryPhase":          "Observed",
		"discoverySelfPrivateIPs": []string{"10.88.60.21"},
	})

	status := store.SQLiteStore.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "Planned" || status["discoveryPhase"] != "Observed" {
		t.Fatalf("status = %#v", status)
	}
	if store.mergeCalls != 2 || store.objectStatusCalls != 0 {
		t.Fatalf("mergeCalls=%d objectStatusCalls=%d, want partial merge without read-modify-write", store.mergeCalls, store.objectStatusCalls)
	}
}
