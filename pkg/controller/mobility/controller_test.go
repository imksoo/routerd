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
	if _, ok := maybePathBySourcePrefix(bgp, source, "10.88.60.12/32"); !ok {
		t.Fatalf("paths = %#v, want unexpired provider-discovery owner retained when inventory saw another address", bgp.paths)
	}
}

func TestControllerBGPModeSuppressesFailedProviderActionAddress(t *testing.T) {
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
	if _, ok := maybePathBySourcePrefix(bgp, source, "10.88.60.11/32"); ok {
		t.Fatalf("paths = %#v, want failed provider action address suppressed", bgp.paths)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["providerActionFailedAddresses"]) != "[10.88.60.11/32]" {
		t.Fatalf("status = %#v, want failed /32 address reported", status)
	}
}

func TestControllerBGPModeFreshHomeOwnerSuppressesRemoteProviderCapture(t *testing.T) {
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
			name:         "aws home owner suppresses oci capture",
			address:      "10.88.60.11/32",
			homeNode:     "aws-router-a",
			homeProvider: "aws",
			homeRef:      "aws-provider",
			homeNIC:      "aws-client-nic",
		},
		{
			name:         "azure home owner suppresses oci capture",
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
			ociController := Controller{Router: routerWithBGPRouter(planningRouterForNode("oci-router", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
			if err := ociController.Reconcile(context.Background()); err != nil {
				t.Fatalf("oci Reconcile: %v", err)
			}
			if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "oci-router"), tc.address); ok {
				t.Fatalf("paths = %#v, want OCI captured path suppressed while fresh %s home owner exists", bgp.paths, tc.homeRef)
			}

			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
				"discoveryOwnedAddresses": []string{tc.address},
				"discoverySelfPrivateIPs": []string{"10.88.60.250"},
				"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
			}); err != nil {
				t.Fatalf("SaveObjectStatus(home): %v", err)
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
	if status["selfCaptureResolved"] != false || !strings.Contains(fmt.Sprint(status["selfCaptureReason"]), "self NIC is unresolved") {
		t.Fatalf("status = %#v, want explicit self capture blocker", status)
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

func TestControllerBGPModeProviderCaptureSuccessAdvertisesPlannedDrainTakeover(t *testing.T) {
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
	path := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.11/32")
	if path.Attrs.LocalPref != bgpMobilityLocalPrefBase+1 {
		t.Fatalf("captured path localPref = %d, want active provider-captured path", path.Attrs.LocalPref)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["generatedProviderCapturedBGPPaths"]) != "1" || fmt.Sprint(status["generatedSeizedBGPPaths"]) != "0" {
		t.Fatalf("status = %#v, want provider-captured=1 seized=0", status)
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

func TestControllerBGPModeSeizeSuccessAdvertisesTrapImmediately(t *testing.T) {
	now := time.Date(2026, 6, 3, 11, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	saveBGPStatus(t, store, map[string][]string{
		"10.88.60.12/32": {"10.99.0.3"},
	}, []map[string]any{}, map[string]string{bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32"})
	seedSucceededBGPCaptureAction(t, store, "aws-provider", "eni-b", "aws-router-b", "10.88.60.12/32", "assign-secondary-ip", 1, now.Add(-time.Second))

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: routerWithBGPRouter(planningRouterForNode("aws-router-b", spec)), Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	path := pathBySourcePrefix(t, bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.12/32")
	if path.Attrs.LocalPref != bgpMobilityLocalPrefBase+1 || !stringSliceContains(path.Attrs.Communities, bgpMobilityCommunityRoleCloud) {
		t.Fatalf("path attrs = %#v, want active cloud seized owner", path.Attrs)
	}
	if _, ok := maybePathBySourcePrefix(bgp, DynamicSource("cloudedge", "aws-router-b"), "10.88.60.10/32"); ok {
		t.Fatalf("paths = %#v, want no BGP path for trap without successful provider capture", bgp.paths)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if fmt.Sprint(status["generatedSeizedBGPPaths"]) != "1" {
		t.Fatalf("status = %#v, want generatedSeizedBGPPaths=1", status)
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
