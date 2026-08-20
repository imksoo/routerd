// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
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

func TestControllerBGPModeAdvertisesSelfOwnedHostRouteAndEmitsTypedLocalInventoryRoute(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.Members[1].Capture.Interface = "lan"
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
	router := planningRouterForNode("azure-router", spec)
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: "lan"},
		Spec:     api.InterfaceSpec{IfName: "ens3"},
	})
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
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
	if resources := decodeResources(t, part.ResourcesJSON); len(resources) != 0 {
		t.Fatalf("BGP mode must not emit generic dynamic resources: %#v", resources)
	}
	dataplane := decodeMobilityDataplanePlan(t, part.MobilityDataplaneJSON)
	if len(dataplane.Routes) != 1 {
		t.Fatalf("BGP mode local dataplane routes = %#v, want one local-inventory route", dataplane.Routes)
	}
	route := dataplane.Routes[0]
	if route.ID != "mobility-cloudedge-local-10-88-60-11" || route.Purpose != dynamicconfig.MobilityIPv4RoutePurposeLocalInventory || route.Destination != "10.88.60.11/32" || route.Device != "ens3" || route.Metric != 1 {
		t.Fatalf("local inventory route intent = %#v", route)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Ready" || fmt.Sprint(status["generatedBGPPaths"]) != "1" {
		t.Fatalf("BGP status = %#v", status)
	}
}

func TestControllerBGPModeOnPremL2WaitsForLocalOwnershipObservation(t *testing.T) {
	now := time.Date(2026, 6, 24, 16, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
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
	if status["phase"] != "Pending" {
		t.Fatalf("status = %#v, want Pending until onprem-l2 observes a local owner", status)
	}
	if !strings.Contains(fmt.Sprint(status["reason"]), "onprem-l2 ownership discovery") {
		t.Fatalf("reason = %#v, want onprem-l2 discovery pending reason", status["reason"])
	}
}

func TestControllerBGPModeOnPremL2DiscoveryWarmupKeepsPoolPending(t *testing.T) {
	now := time.Date(2026, 6, 25, 3, 5, 0, 0, time.UTC)
	spec := plannedPoolSpec()
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
	store := testStore(t, now)
	router := planningRouterForNode("onprem-router", spec)
	bootstrapOnPremDiscovery(t, store, router, now.Add(-5*time.Second))
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "onprem-router", "10.88.60.15/32", observation, now.Add(-5*time.Second), 2*time.Minute))
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" {
		t.Fatalf("status = %#v, want Pending during onprem-l2 discovery warmup", status)
	}
	if !strings.Contains(fmt.Sprint(status["reason"]), "warming up") {
		t.Fatalf("reason = %#v, want warmup reason", status["reason"])
	}

	store = testStore(t, now)
	router = planningRouterForNode("onprem-router", spec)
	bootstrapOnPremDiscovery(t, store, router, now.Add(-onPremL2DiscoveryWarmup-time.Second))
	recordEvent(t, store, onPremDiscoveryObservedEvent("cloudedge", "cloudedge", "onprem-router", "10.88.60.15/32", observation, now.Add(-5*time.Second), 2*time.Minute))
	bgp = &fakeBGPPaths{}
	controller = Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile after warmup: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Ready" {
		t.Fatalf("status = %#v, want Ready after onprem-l2 discovery warmup", status)
	}
}

func TestControllerBGPModeOnPremL2AllowsFreshEmptyCompleteDiscovery(t *testing.T) {
	now := time.Date(2026, 6, 26, 4, 10, 0, 0, time.UTC)
	spec := plannedPoolSpec()
	spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
		Mode:            "onprem-l2",
		AllowEmptyAfter: "5s",
		LeaseTTL:        "10s",
		Sources: []api.MobilityOwnershipDiscoverySource{
			{Type: OnPremSourceARPObserver, Interface: "lan"},
		},
	}
	store := testStore(t, now)
	router := planningRouterForNode("onprem-router", spec)
	bootstrapOnPremDiscovery(t, store, router, now.Add(-6*time.Second))
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Ready" {
		t.Fatalf("status = %#v, want Ready for fresh empty complete discovery", status)
	}

	store = testStore(t, now)
	router = planningRouterForNode("onprem-router", spec)
	bootstrapOnPremDiscovery(t, store, router, now.Add(-11*time.Second))
	bgp = &fakeBGPPaths{}
	controller = Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile stale: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" || !strings.Contains(fmt.Sprint(status["reason"]), "onprem-l2 ownership discovery") {
		t.Fatalf("status = %#v, want Pending for stale empty complete discovery", status)
	}
}

func TestControllerBGPModeProfileSpecMatchesInlineSpec(t *testing.T) {
	now := time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC)
	inlineSpec := awsFailoverPoolSpec()
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
	source := DynamicSource("cloudedge", "azure-router")
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.21"}},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.11/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic"}, now.Add(-time.Minute), time.Hour),
			providerDiscoveryAddressFactForTest("10.88.60.12/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic"}, now.Add(-time.Minute), time.Hour),
		},
	}, now.Add(-time.Minute))

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: planningRouterForNode("azure-router", spec), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile without fresh discovery status: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(bgp, source, "10.88.60.11/32"); !ok {
		t.Fatalf("paths = %#v, want unexpired provider-discovery self-origin advertised before fresh inventory status", bgp.paths)
	}
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.21"}},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.11/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic"}, now, DefaultLeaseTTL),
		},
	}, now)

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
	source := DynamicSource("cloudedge", "azure-router")
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.21"}},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.11/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic"}, now.Add(-time.Minute), time.Hour),
		},
	}, now.Add(-time.Minute))
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
	if status["providerActionPhase"] != "OK" || status["providerActionFailedAddresses"] != nil {
		t.Fatalf("status = %#v, want current typed home ownership to supersede stale provider failure", status)
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
			for i := range spec.Members {
				if spec.Members[i].NodeRef == "oci-router" {
					spec.Members[i].Capture = api.MobilityMemberCapture{
						Type:        "provider-secondary-ip",
						ProviderRef: "oci-provider",
						NICRef:      "oci-vnic",
					}
				}
			}
			recordEvent(t, store, providerDiscoveryRuntimeEventForTest(t, tc.homeNode, providerDiscoveryRuntimeFact{
				Addresses: []providerDiscoveryAddressFact{
					providerDiscoveryAddressFactForTest(tc.address, tc.homeProvider, tc.homeRef, providerinventory.PrivateIPRecord{NICRef: tc.homeNIC, SubnetRef: tc.homeRef + "-subnet"}, now.Add(-time.Second), time.Hour),
				},
			}, now.Add(-time.Second), time.Hour))
			seedSucceededBGPCaptureAction(t, store, "oci-provider", "oci-vnic", "oci-router", tc.address, "assign-secondary-ip", 1, now.Add(-time.Second))
			saveBGPInstalledNextHops(t, store, map[string][]string{tc.address: {"10.99.0.200"}})
			recordProviderDiscoveryRuntime(t, store, "oci-router", providerDiscoveryRuntimeFact{
				Self: discoverySelfInventory{PrivateIPs: []string{tc.address}},
			}, now)

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

			recordProviderDiscoveryRuntime(t, store, tc.homeNode, providerDiscoveryRuntimeFact{
				Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.250"}},
				Addresses: []providerDiscoveryAddressFact{
					providerDiscoveryAddressFactForTest(tc.address, tc.homeProvider, tc.homeRef, providerinventory.PrivateIPRecord{NICRef: tc.homeNIC, SubnetRef: tc.homeRef + "-subnet"}, now, DefaultLeaseTTL),
				},
			}, now)
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
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.12"}},
	}, now)

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
	}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	spec := plannedPoolSpec()
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
	if status["providerActionPhase"] != "OK" || status["providerActionError"] != "" {
		t.Fatalf("provider action failure status was not cleared: %#v", status)
	}
	if status["providerActionFailedAddresses"] != nil {
		t.Fatalf("provider action failure addresses were not cleared: %#v", status)
	}
	for _, key := range []string{"providerActionFailedTargets", "providerActionFailedDetails", "providerActionFailedAt"} {
		if _, found := status[key]; found {
			t.Fatalf("obsolete provider action status %q was serialized: %#v", key, status)
		}
	}
}

func TestControllerBGPModeWaitsForProviderObservationAfterAssignSuccess(t *testing.T) {
	now := time.Date(2026, 6, 25, 5, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	source := DynamicSource("cloudedge", "azure-router")
	address := "10.88.60.10/32"
	// Remote ownership reaches this node through the owner path, not through a
	// remote member's local static-address overlay.
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.1"}}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.11/32"},
			ForwardingEnabled: boolPtr(true),
		},
	}, now)

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
	if status["phase"] != "Pending" {
		t.Fatalf("status = %#v, want provider observation pending", status)
	}
	if fmt.Sprint(status["providerObservationPendingAddresses"]) != "["+address+"]" {
		t.Fatalf("status = %#v, want pending observation address", status)
	}

	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.11/32"},
			CapturedAddresses: []string{address},
			ForwardingEnabled: boolPtr(true),
		},
	}, now)
	controller.Now = func() time.Time { return now.Add(2500 * time.Millisecond) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("stale observation Reconcile: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" || fmt.Sprint(status["providerObservationPendingAddresses"]) != "["+address+"]" {
		t.Fatalf("status = %#v, want stale provider observation to remain pending", status)
	}

	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.11/32"},
			CapturedAddresses: []string{address},
			ForwardingEnabled: boolPtr(true),
		},
	}, now.Add(3*time.Second))
	controller.Now = func() time.Time { return now.Add(4 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("confirmed observation Reconcile: %v", err)
	}
	status = store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["providerObservationPendingAddresses"] != nil {
		t.Fatalf("status = %#v, want confirmed observation", status)
	}
	for _, key := range []string{"providerObservationConfirmedAddresses", "providerObservationPendingTargets", "providerObservationConfirmedTargets", "providerObservationDetails", "providerObservationConfirmedCount"} {
		if _, found := status[key]; found {
			t.Fatalf("obsolete provider observation status %q was serialized: %#v", key, status)
		}
	}
}

func TestControllerBGPModeReportsFailedForwardingProviderAction(t *testing.T) {
	now := time.Date(2026, 6, 25, 4, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.3"},
	}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			NICRef:            "eni-a",
			SubnetRef:         "subnet-a",
			PrivateIPs:        []string{"10.88.60.11"},
			ForwardingEnabled: boolPtr(false),
		},
	}, now)
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
	if status["providerActionError"] != "source/dest check update denied" {
		t.Fatalf("status = %#v, want forwarding failure count/error", status)
	}
	if status["providerActionFailedAddresses"] != nil {
		t.Fatalf("status = %#v, forwarding failure must not invent an address", status)
	}
	for _, key := range []string{"providerActionFailedTargets", "providerActionFailedDetails", "providerActionFailedAt"} {
		if _, found := status[key]; found {
			t.Fatalf("obsolete provider action status %q was serialized: %#v", key, status)
		}
	}
}

func TestControllerBGPModeObservedSelfCaptureSupersedesProviderActionFailureStatus(t *testing.T) {
	now := time.Date(2026, 6, 14, 20, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.10/32"
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
	recordProviderDiscoveryRuntime(t, store, "oci-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{address},
			CapturedAddresses: []string{address},
		},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest(address, "oci", "oci-provider", providerinventory.PrivateIPRecord{NICRef: "oci-client-vnic", SubnetRef: "oci-subnet"}, now, DefaultLeaseTTL),
		},
	}, now)

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithOCIProvider(routerWithBGPRouter(planningRouterForNode("oci-router", spec))), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["providerActionPhase"] != "OK" || status["providerActionError"] != "" || status["providerActionFailedAddresses"] != nil {
		t.Fatalf("provider failure was not superseded in status: %#v", status)
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

func TestControllerBGPModeUsesDiscoveredSelfNICForProviderActions(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
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
	if status["phase"] != "Degraded" || !strings.Contains(fmt.Sprint(status["reason"]), "self NIC is unresolved") {
		t.Fatalf("status = %#v, want unresolved self NIC degraded", status)
	}
	if ownerUpserts := nonLivenessUpserts(bgp.upserts); len(ownerUpserts) != 0 {
		t.Fatalf("bgp upserts = %#v, want no self-owned path for remote owner", bgp.upserts)
	}

	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			NICRef:    "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/resolved-router-nic",
			SubnetRef: "/subnets/demo",
		},
	}, now)
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
	recordProviderDiscoveryRuntime(t, store, "azure-router-b", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			NICRef:            "router-b-nic",
			PrivateIPs:        []string{"10.88.60.22"},
			ForwardingEnabled: boolPtr(false),
		},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.10/32", "azure", "azure-provider", providerinventory.PrivateIPRecord{NICRef: "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b", ResourceType: "router-nic"}, now.Add(time.Second), DefaultLeaseTTL),
		},
	}, now.Add(time.Second))

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
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.4/32"}},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.11/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic-a"}, now, DefaultLeaseTTL),
		},
	}, now)

	drained := awsFailoverPoolSpec()
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
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "aws-router-b", spec.Members, map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	}, now.Add(time.Second))
	recordProviderDiscoveryRuntime(t, store, "aws-router-b", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.4/32"}},
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
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.4/32"}},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.11/32", "aws", "aws-provider", providerinventory.PrivateIPRecord{NICRef: "client-nic-a"}, now, DefaultLeaseTTL),
		},
	}, now)
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(routerWithEventGroupListen(planningRouterForNode("aws-router-a", spec), "10.99.0.2")), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}

	drained := awsFailoverPoolSpec()
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
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.4", "10.88.60.11/32"}},
	}, now)
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
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.12/32": {"10.99.0.3"},
	}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			NICRef:            "eni-a",
			SubnetRef:         "subnet-a",
			PrivateIPs:        []string{"10.88.60.11"},
			ForwardingEnabled: boolPtr(false),
		},
	}, now)
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
	if !strings.Contains(forwarding.IdempotencyKey, ":forwarding-drift:") {
		t.Fatalf("forwarding plan = %#v, want provider-observed drift fence", forwarding)
	}
}

func TestControllerBGPModeAdvertisesSelfLivenessMarker(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
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

func TestControllerBGPModeRejectsEventGroupSiteAlias(t *testing.T) {
	now := time.Date(2026, 6, 3, 10, 43, 4, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	router := planningRouterForNode("aws-router-a", spec)
	for index := range router.Spec.Resources {
		resource := &router.Spec.Resources[index]
		if resource.APIVersion != api.FederationAPIVersion || resource.Kind != "EventGroup" {
			continue
		}
		group, err := resource.EventGroupSpec()
		if err != nil {
			t.Fatal(err)
		}
		group.NodeName = "aws-router"
		resource.Spec = group
	}
	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Degraded" || !strings.Contains(fmt.Sprint(status["reason"]), `self node "aws-router" is not a member`) {
		t.Fatalf("status = %#v, want fail-closed exact SAMNodeSet member requirement", status)
	}
	if len(bgp.upserts) != 0 {
		t.Fatalf("BGP upserts = %#v, want none for invalid EventGroup identity", bgp.upserts)
	}
}

func TestControllerBGPModeStandbyDefersTrapWhenActiveLivenessMarkerPresent(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
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
	if status["bgpSeizeHoldDownActive"] != true || status["bgpSeizeHoldDownUntil"] == "" {
		t.Fatalf("initial status = %#v, want active seize hold-down status", status)
	}
	if status["phase"] != "Pending" || status["reason"] != "BGP capture seize hold-down is active" {
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
	if status["bgpSeizeHoldDownActive"] != false || status["bgpSeizeHoldDownKey"] == "" || status["bgpSeizeHoldDownUntil"] == "" {
		t.Fatalf("status = %#v, want elapsed seize hold-down record retained for restart safety", status)
	}
}

func TestControllerBGPModeCaptureRejoinDoesNotImportTransitionAndCanonicalAssignVariants(t *testing.T) {
	now := time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	selfNode := "aws-router-b"
	address := "10.88.60.12/32"
	livenessMarkers := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity(selfNode): "10.99.0.5/32",
	}
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		address:          {"10.99.0.3"},
		"10.88.60.13/32": {"10.99.0.4"},
	}, []map[string]any{}, livenessMarkers)
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", selfNode, spec.Members, livenessMarkers, now)
	members := plannerMembers(spec.Members)
	self := members[selfNode]
	seedSucceededActionRecordForPlannerTest(t, store, providerCaptureActionRecordForPlannerTest(t, 91, actionUnassignSecondaryIP, address, self.Capture.ProviderRef, providerCaptureRefFromCapture(self.Capture), self.NodeRef, now.Add(-10*time.Second), map[string]string{
		bgpPathSigParam:    "deprovision:" + address + ":observed-self-stale:since=" + now.Add(-time.Minute).Format(time.RFC3339Nano),
		"deprovisionSince": now.Add(-time.Minute).Format(time.RFC3339Nano),
	}))

	current := now
	controller := Controller{
		Router:   routerWithBGPRouter(planningRouterForNode(selfNode, spec)),
		Store:    store,
		BGPPaths: &fakeBGPPaths{},
		Now:      func() time.Time { return current },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	source := DynamicSource("cloudedge", selfNode)
	plans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	assign := findActionPlanByAddress(plans, actionAssignSecondaryIP, address)
	if assign == nil {
		t.Fatalf("initial plans = %#v, want assign for %s", plans, address)
	}
	if strings.Contains(assign.IdempotencyKey, ":transition:") {
		t.Fatalf("initial assign key = %q, want canonical key before transition retry", assign.IdempotencyKey)
	}
	inserted := importActionPlanRecord(t, store, source, *assign, current)
	if !inserted {
		t.Fatalf("initial assign %q was not inserted", assign.IdempotencyKey)
	}
	markActionSucceededByKey(t, store, assign.IdempotencyKey, current.Add(time.Second))

	current = current.Add(2 * time.Second)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	secondPlans := decodeActionPlans(t, latestPart(t, store, source).ActionPlansJSON)
	secondAssign := findActionPlanByAddress(secondPlans, actionAssignSecondaryIP, address)
	if secondAssign == nil {
		t.Fatalf("second plans = %#v, want retained assign for %s", secondPlans, address)
	}
	if secondAssign.IdempotencyKey != assign.IdempotencyKey {
		t.Fatalf("second assign key = %q, want same canonical key %q", secondAssign.IdempotencyKey, assign.IdempotencyKey)
	}
	inserted = importActionPlanRecord(t, store, source, *secondAssign, current)
	if inserted {
		t.Fatalf("second assign %q inserted a duplicate journal row", secondAssign.IdempotencyKey)
	}
	if got := countActionRowsByAddress(t, store, actionAssignSecondaryIP, address); got != 1 {
		t.Fatalf("assign journal rows for %s = %d, want exactly 1", address, got)
	}
}

func TestControllerBGPModeSeizeSuccessDoesNotAdvertiseTrapAsOwner(t *testing.T) {
	now := time.Date(2026, 6, 3, 11, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.12/32": {"10.99.0.3"},
	}, []map[string]any{}, map[string]string{bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32"})
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "aws-router-b", spec.Members, map[string]string{
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

func TestControllerBGPModeProviderCaptureCompletionEventsUseProductionObservations(t *testing.T) {
	now := time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	selfNode := "aws-router-b"
	seized := "10.88.60.17/32"
	confirmed := "10.88.60.34/32"
	livenessMarkers := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity(selfNode): "10.99.0.5/32",
	}
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-aws-a-seized",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       ObservedEventType,
		Subject:    seized,
		ObservedAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
		Payload: map[string]string{
			"address":      seized,
			"pool":         "cloudedge",
			"source":       providerDiscoverySource,
			"nicRef":       "eni-a",
			"resourceType": "router-nic",
		},
	})
	recordProviderDiscoveryRuntime(t, store, selfNode, providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{confirmed},
		},
	}, now)
	saveBGPStatus(t, store, map[string][]string{
		seized:    {"10.99.0.2"},
		confirmed: {"10.99.0.3"},
	}, []map[string]any{
		bgpOwnerPrefix(seized, "10.99.0.2", "aws-router-a"),
		bgpOwnerPrefix(confirmed, "10.99.0.3", "azure-router"),
	}, livenessMarkers)
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", selfNode, spec.Members, livenessMarkers, now)
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-b", selfNode, seized, "assign-secondary-ip", 2, now.Add(-5*time.Second))
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-b", selfNode, confirmed, "assign-secondary-ip", 1, now.Add(-4*time.Second))

	controller := Controller{
		Router:   routerWithBGPRouter(planningRouterForNode(selfNode, spec)),
		Store:    store,
		BGPPaths: &fakeBGPPaths{},
		Now:      func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	events := listMobilityTransitionEvents(t, store)
	seizeEvents := transitionEventsByKindAddress(events, "seize-complete")
	confirmEvents := transitionEventsByKindAddress(events, "capture-confirmed")
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	plans := decodeActionPlans(t, latestPart(t, store, DynamicSource("cloudedge", selfNode)).ActionPlansJSON)
	seizedPlan := findActionPlanByAddress(plans, actionAssignSecondaryIP, seized)
	if seizedPlan == nil || seizedPlan.Parameters[captureAssignmentGenerationParam] == "" {
		t.Fatalf("provider plans = %#v, want fenced active assignment for %s", plans, seized)
	}
	for _, key := range []string{"bgpCaptureClaim", "bgpCaptureAssignments", "bgpCaptureAssignmentSeq"} {
		if _, found := status[key]; found {
			t.Fatalf("status unexpectedly retains planner control state %q: %#v", key, status)
		}
	}
	_, hasSeizeComplete := seizeEvents[seized]
	_, hasCaptureConfirmed := confirmEvents[confirmed]
	if !hasSeizeComplete || !hasCaptureConfirmed {
		t.Fatalf("completion events: seize-complete=%d capture-confirmed=%d, want one each (seize=%#v confirm=%#v)", len(seizeEvents), len(confirmEvents), seizeEvents, confirmEvents)
	}
	durations := extractTransitionDurationsByAddress(t, events)
	if got, ok := durations["seize-complete"][seized]; !ok {
		t.Fatalf("missing extractable seize duration for %s (durations=%#v)", seized, durations)
	} else if got < 0 {
		t.Fatalf("extractable seize duration for %s = %s, want >= 0", seized, got)
	}
	if got, ok := durations["capture-confirmed"][confirmed]; !ok {
		t.Fatalf("missing extractable capture duration for %s (durations=%#v)", confirmed, durations)
	} else if got < 0 {
		t.Fatalf("extractable capture duration for %s = %s, want >= 0", confirmed, got)
	}
}

func TestControllerBGPModeBG24RuntimeSeizesWhenAWSActiveMarkerAbsent(t *testing.T) {
	now := time.Date(2026, 6, 3, 10, 43, 4, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
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
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "aws-router-b", spec.Members, map[string]string{
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
}

func TestControllerBGPModeRestoreKeepsOwnerPreferredOverStandby(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
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

func TestControllerBGPModeProviderTrapRecapturesAfterSuccessfulRelease(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
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
		if !strings.Contains(assign.IdempotencyKey, ":transition:after-unassign-") {
			t.Fatalf("assign %s key/parameters = %q %#v, want transition-fenced recapture after unassign", address, assign.IdempotencyKey, assign.Parameters)
		}
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

func TestControllerBGPModeProviderTrapRecapturesWhenObservedProviderStateLost(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", "10.88.60.10/32", "assign-secondary-ip", 1, now.Add(-3*time.Minute))
	// The on-prem owner is represented by its observed BGP owner path.
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
	}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.11"}},
	}, now.Add(-time.Second))

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
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", "10.88.60.10/32", "assign-secondary-ip", 1, now.Add(-5*time.Second))
	// The on-prem owner is represented by its observed BGP owner path.
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
	}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.11"},
			CapturedAddresses: []string{},
		},
	}, now)

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
	if strings.Contains(assign.IdempotencyKey, ":transition:provider-missing-") {
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
	decisions := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] == ownershipClassConfirmedCapture || decision["captureState"] == captureStateConfirmed {
		t.Fatalf("decision = %#v, action journal without provider observation must not confirm capture", decision)
	}
}

func TestControllerBGPModeRemoteProviderTrapRecapturesWithoutSelfMarkerMatch(t *testing.T) {
	now := time.Date(2026, 6, 13, 20, 40, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
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
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.4/32"}},
	}, now)

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
	spec.Members[1].Capture.CaptureStrategy = captureStrategyRouteTable
	spec.Members[1].Capture.Target = map[string]string{
		"region":           "japaneast",
		"routeTableRef":    "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/routeTables/rt-cloudedge",
		"nextHopIPAddress": "10.88.60.4",
	}
	localInventory := []providerinventory.PrivateIPRecord{
		{Address: "10.88.60.4/32", NICRef: "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic", SubnetRef: "/subnets/azure", ProviderRef: "azure-provider", ResourceType: "router-nic", Primary: true},
		{Address: "10.88.60.11/32", NICRef: "/subscriptions/sub-1/resourceGroups/rg-app/providers/Microsoft.Network/networkInterfaces/client-nic", SubnetRef: "/subnets/azure", ProviderRef: "azure-provider", ResourceType: "instance-nic"},
	}
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:      []string{"10.88.60.4"},
			PrimaryObserved: true,
		},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.4/32", "azure", "azure-provider", localInventory[0], now, DefaultLeaseTTL),
			providerDiscoveryAddressFactForTest("10.88.60.11/32", "azure", "azure-provider", localInventory[1], now, DefaultLeaseTTL),
		},
	}, now)
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
	if findActionPlanByAddress(plans, actionAssignRouteTableRoute, "10.88.60.4/32") != nil ||
		findActionPlanByAddress(plans, actionAssignRouteTableRoute, "10.88.60.11/32") != nil {
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

func TestControllerBGPModeProviderTrapRejectsUnknownBGPOnlyAddress(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 2, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
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
	decisions := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	decision := ownershipStatusDecisionByAddress(t, decisions, "10.88.60.44/32")
	if decision["class"] != ownershipClassUnknown || decision["source"] != "bgp-rib" {
		t.Fatalf("decision = %#v, want unknown bgp-rib address", decision)
	}
}

func TestControllerBGPModeReturnRouteDoesNotBecomeUnknownClaim(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 3, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
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
	decisions := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	for _, decision := range decisions {
		if decision["address"] == "10.88.60.4/32" {
			t.Fatalf("return-route leaked into ownership resolver decisions: %#v", decision)
		}
	}
}

func TestControllerBGPModeRouteTableWrongLocalUDRIsDeprovisioned(t *testing.T) {
	now := time.Date(2026, 6, 9, 23, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
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
	localInventory := []providerinventory.PrivateIPRecord{
		{Address: "10.88.60.4/32", NICRef: "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic", SubnetRef: "/subnets/azure", ProviderRef: "azure-provider", ResourceType: "router-nic", Primary: true},
		{Address: "10.88.60.11/32", NICRef: "/subscriptions/sub-1/resourceGroups/rg-app/providers/Microsoft.Network/networkInterfaces/client-nic", SubnetRef: "/subnets/azure", ProviderRef: "azure-provider", ResourceType: "instance-nic"},
	}
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.4"}},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest("10.88.60.4/32", "azure", "azure-provider", localInventory[0], now, DefaultLeaseTTL),
			providerDiscoveryAddressFactForTest("10.88.60.11/32", "azure", "azure-provider", localInventory[1], now, DefaultLeaseTTL),
		},
	}, now)
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
	if findActionPlanByAddress(plans, actionAssignRouteTableRoute, "10.88.60.4/32") != nil ||
		findActionPlanByAddress(plans, actionAssignRouteTableRoute, "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, want wrong local UDR assign removed from desired set", plans)
	}
	if findActionPlanByAddress(plans, actionUnassignRouteTableRoute, "10.88.60.4/32") == nil ||
		findActionPlanByAddress(plans, actionUnassignRouteTableRoute, "10.88.60.11/32") == nil {
		t.Fatalf("plans = %#v, want wrong local UDR deprovisioned", plans)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	decisions := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	selfDecision := ownershipStatusDecisionByAddress(t, decisions, "10.88.60.4/32")
	if selfDecision["class"] != ownershipClassStaleCapture || selfDecision["suppressionReason"] != "local-router-self" {
		t.Fatalf("self decision = %#v, want local router self stale capture", selfDecision)
	}
}

func TestControllerBGPModeProviderTrapRIBStartupIsConservative(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.StaticHandovers = []api.MobilityStaticHandover{{
		Address:     "10.88.60.10/32",
		FromNodeRef: "azure-router",
		ToNodeRef:   "onprem-router",
	}}
	router := routerWithBGPRouter(planningRouterForNode("azure-router", spec))
	saveBGPStatus(t, store, map[string][]string{"10.88.60.10/32": {"10.99.0.1"}}, []map[string]any{
		bgpOwnerPrefix("10.88.60.10/32", "10.99.0.1", "onprem-router"),
	}, nil)
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
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPInstalledNextHops(t, store, map[string][]string{address: {"10.99.0.200"}})
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.4/32"}},
	}, now)

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
	decisions := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] != ownershipClassStaleCapture || decision["suppressionReason"] != "capture-not-desired" {
		t.Fatalf("decision = %#v, want capture-not-desired stale capture", decision)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureIsProtectedWithoutPriorAction(t *testing.T) {
	now := time.Date(2026, 6, 10, 18, 35, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.10/32"
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{address},
		},
	}, now)

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
	decisions := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] != ownershipClassStaleCapture || decision["suppressionReason"] != "self-captured-secondary" {
		t.Fatalf("decision = %#v, want self-captured-secondary stale capture", decision)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureWaitsForRecentTrapMissingHold(t *testing.T) {
	now := time.Date(2026, 6, 10, 18, 37, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
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
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{address},
		},
	}, now)

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
	address := "10.88.60.12/32"
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.3"}}, []map[string]any{{
		"prefix":      address,
		"nextHop":     "10.99.0.3",
		"best":        true,
		"valid":       true,
		"communities": []string{bgpstate.MobilityCommunityReturnRoute},
	}}, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{address},
		},
	}, now)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"observedSelfStaleCaptures": map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
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
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-5*time.Minute))
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.2"}}, []map[string]any{
		bgpOwnerPrefix(address, "10.99.0.2", "aws-router-a"),
	}, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{address},
		},
	}, now)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"observedSelfStaleCaptures": map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
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
	decisions := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] != ownershipClassStaleCapture || decision["suppressionReason"] != "self-captured-secondary" {
		t.Fatalf("decision = %#v, want succeeded self-captured-secondary stale capture", decision)
	}
}

func TestControllerBGPModeObservedSelfStaleCaptureWithInstalledOwnerPathIsProtected(t *testing.T) {
	now := time.Date(2026, 6, 10, 18, 42, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-3*time.Minute))
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.3"}}, []map[string]any{
		bgpOwnerPrefix(address, "10.99.0.3", "azure-router"),
	}, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{address},
		},
	}, now)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"observedSelfStaleCaptures": map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
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
	spec.Members[3].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider"}
	spec.Members[3].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", ProviderRef: "azure-provider"}
	discoveredNIC := "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic"
	address := "10.88.60.10/32"
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			NICRef:            discoveredNIC,
			PrivateIPs:        []string{"10.88.60.22/32"},
			CapturedAddresses: []string{address},
		},
	}, now)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"observedSelfStaleCaptures": map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
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

func TestControllerBGPModeObservedSelfStaleCaptureUsesCanonicalCaptureNIC(t *testing.T) {
	now := time.Date(2026, 6, 10, 20, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.Members[3].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider"}
	spec.Members[3].Capture.NICRef = "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/target-router-nic"
	address := "10.88.60.10/32"
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "azure-router", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{address},
		},
	}, now)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"observedSelfStaleCaptures": map[string]string{address: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
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
		t.Fatalf("plans = %#v, observed stale provider-secondary capture must be deprovisioned via capture NIC cleanup", plans)
	} else if unassign.Target["nicRef"] != spec.Members[3].Capture.NICRef {
		t.Fatalf("unassign target = %#v, want capture nicRef %q", unassign.Target, spec.Members[3].Capture.NICRef)
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

func TestControllerBGPModeProviderHomeConflictBlocksProviderAction(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 15, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	address := "10.88.60.11/32"
	recordEvent(t, store, providerDiscoveryRuntimeEventForTest(t, "oci-router", providerDiscoveryRuntimeFact{
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest(address, "oci", "oci-provider", providerinventory.PrivateIPRecord{
				NICRef:       "oci-client",
				SubnetRef:    "oci-subnet",
				ResourceRef:  "ocid1.instance.oc1.test.client",
				ResourceType: "instance-nic",
			}, now.Add(-time.Second), time.Hour),
		},
	}, now.Add(-time.Second), time.Hour))
	saveBGPInstalledNextHops(t, store, map[string][]string{address: {"10.99.0.200"}})
	localInventory := []providerinventory.PrivateIPRecord{{
		Address:      address,
		NICRef:       "eni-client",
		SubnetRef:    "subnet-a",
		ProviderRef:  "aws-provider",
		ResourceRef:  "i-aws-client",
		ResourceType: "instance-nic",
	}}
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{PrivateIPs: []string{"10.88.60.4/32"}},
		Addresses: []providerDiscoveryAddressFact{
			providerDiscoveryAddressFactForTest(address, "aws", "aws-provider", localInventory[0], now, DefaultLeaseTTL),
		},
	}, now)

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
	if status["phase"] != "Degraded" || status["providerActionPhase"] != "Blocked" {
		t.Fatalf("status = %#v, want degraded blocked conflict", status)
	}
	controlTable := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	row := ownershipStatusDecisionByAddress(t, controlTable, address)
	if row["state"] != "Conflict" || row["conflictReason"] != "duplicate-provider-home-owners" || row["conflictWinnerNode"] != "aws-router-a" || row["ownerNode"] != "aws-router-a" || row["ownerProviderRef"] != "aws-provider" {
		t.Fatalf("control-plane owner table row = %#v, want deterministic provider-home conflict winner", row)
	}
}

func TestControllerBGPModeSucceededStaleCaptureDoesNotCarryPreviousTrap(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 20, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
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
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{},
		},
	}, now)

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
	address := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", address, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPStatus(t, store, map[string][]string{address: {"10.99.0.3"}}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{address},
		},
	}, now)

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
	decisions := ownershipStatusDecisions(t, status["ownershipResolverControlPlaneOwnerTable"])
	decision := ownershipStatusDecisionByAddress(t, decisions, address)
	if decision["class"] != ownershipClassConfirmedCapture {
		t.Fatalf("decision = %#v, want ConfirmedCapture", decision)
	}
}

func TestControllerBGPModeProtectOnlyCaptureKeepsForwardingEnabled(t *testing.T) {
	now := time.Date(2026, 6, 10, 14, 30, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	confirmed := "10.88.60.12/32"
	stale := "10.88.60.10/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", confirmed, "assign-secondary-ip", 1, now.Add(-2*time.Minute))
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", stale, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPStatus(t, store, map[string][]string{confirmed: {"10.99.0.3"}}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{confirmed},
		},
	}, now)

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
	stale := "10.88.60.12/32"
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-a", "aws-router-a", stale, "assign-secondary-ip", 1, now.Add(-time.Minute))
	saveBGPStatus(t, store, map[string][]string{}, nil, nil)
	recordProviderDiscoveryRuntime(t, store, "aws-router-a", providerDiscoveryRuntimeFact{
		Self: discoverySelfInventory{
			PrivateIPs:        []string{"10.88.60.4/32"},
			CapturedAddresses: []string{stale},
		},
	}, now)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"observedSelfStaleCaptures": map[string]string{stale: now.Add(-3 * time.Minute).Format(time.RFC3339Nano)},
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

func TestControllerBGPModeWithdrawsCARPBackupCaptureAdvertisement(t *testing.T) {
	now := time.Date(2026, 7, 25, 1, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := staticPoolSpec()
	spec.Members[0].Capture = api.MobilityMemberCapture{
		Type:      "proxy-arp",
		Interface: "lan",
		ActiveWhen: api.CaptureActiveWhen{
			Type:              "vrrp-master",
			VirtualAddressRef: "VirtualAddress/onprem-vip",
		},
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "onprem-vip", map[string]any{"role": "master"}); err != nil {
		t.Fatalf("save master status: %v", err)
	}
	paths := &fakeBGPPaths{}
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: store, BGPPaths: paths, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("master reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(paths, DynamicSource("cloudedge", "onprem-router"), "10.88.60.10/32"); !ok {
		t.Fatalf("master did not advertise capture path: %#v", paths.paths)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "onprem-vip", map[string]any{"role": "backup"}); err != nil {
		t.Fatalf("save backup status: %v", err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("backup reconcile: %v", err)
	}
	if _, ok := maybePathBySourcePrefix(paths, DynamicSource("cloudedge", "onprem-router"), "10.88.60.10/32"); ok {
		t.Fatalf("backup retained capture path: %#v", paths.paths)
	}
	if len(paths.deletes) != 1 || paths.deletes[0].Prefix != "10.88.60.10/32" {
		t.Fatalf("backup deletes = %#v, want exact capture withdrawal", paths.deletes)
	}
}

func TestControllerBGPModeStaticHandoverSwitchesAdvertisementSource(t *testing.T) {
	base := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	spec := staticPoolSpec()
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

func reconcileBGPProfileEquivalence(t *testing.T, selfNode string, spec testMobilityPoolSpec, now time.Time) ([]bgpdaemon.AppliedPath, []dynamicconfig.ActionPlan) {
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
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", selfNode, spec.Members, map[string]string{
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
