// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
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

func TestControllerProjectsObservedEventToAddressLease(t *testing.T) {
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-1",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.9/32",
		DedupeKey:  "client-9",
		ObservedAt: now.Add(-time.Minute),
	})

	controller := Controller{Router: testRouter(), Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	lease, found, err := store.GetAddressLease("cloudedge", "10.88.60.9/32")
	if err != nil {
		t.Fatalf("GetAddressLease: %v", err)
	}
	if !found {
		t.Fatal("lease not found")
	}
	if lease.OwnerNode != "onprem-router" || lease.OwnerSite != "onprem" || lease.OwnerRole != "onprem" || lease.Status != routerstate.AddressLeaseStatusActive || lease.Epoch != 1 {
		t.Fatalf("unexpected lease: %+v", lease)
	}
	if lease.ExpiresAt != now.Add(-time.Minute).Add(DefaultLeaseTTL) {
		t.Fatalf("expiresAt = %s", lease.ExpiresAt)
	}
}

func TestControllerHoldsThenAdoptsOwnerChange(t *testing.T) {
	base := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-1",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.9/32",
		ObservedAt: base.Add(-time.Minute),
		ExpiresAt:  base.Add(time.Hour),
	})
	controller := Controller{Router: testRouter(), Store: store, Now: func() time.Time { return base }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}

	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-2",
		Group:      "cloudedge",
		SourceNode: "azure-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.9/32",
		ObservedAt: base.Add(10 * time.Second),
		ExpiresAt:  base.Add(time.Hour),
	})
	controller.Now = func() time.Time { return base.Add(20 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("holding Reconcile: %v", err)
	}
	lease, _, err := store.GetAddressLease("cloudedge", "10.88.60.9/32")
	if err != nil {
		t.Fatalf("GetAddressLease holding: %v", err)
	}
	if lease.OwnerNode != "onprem-router" || lease.CandidateOwnerNode != "azure-router" || lease.Status != routerstate.AddressLeaseStatusHolding || lease.Epoch != 1 {
		t.Fatalf("unexpected held lease: %+v", lease)
	}

	controller.Now = func() time.Time { return base.Add(45 * time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("adopt Reconcile: %v", err)
	}
	lease, _, err = store.GetAddressLease("cloudedge", "10.88.60.9/32")
	if err != nil {
		t.Fatalf("GetAddressLease adopted: %v", err)
	}
	if lease.OwnerNode != "azure-router" || lease.CandidateOwnerNode != "" || lease.Status != routerstate.AddressLeaseStatusActive || lease.Epoch != 2 {
		t.Fatalf("unexpected adopted lease: %+v", lease)
	}
}

func TestControllerTieBreaksDeterministically(t *testing.T) {
	now := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	observedAt := now.Add(-time.Minute)
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-1",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.9/32",
		ObservedAt: observedAt,
		ExpiresAt:  now.Add(time.Hour),
	})
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-2",
		Group:      "cloudedge",
		SourceNode: "azure-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.9/32",
		ObservedAt: observedAt,
		ExpiresAt:  now.Add(time.Hour),
	})
	controller := Controller{Router: testRouter(), Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	lease, _, err := store.GetAddressLease("cloudedge", "10.88.60.9/32")
	if err != nil {
		t.Fatalf("GetAddressLease: %v", err)
	}
	if lease.OwnerNode != "azure-router" || lease.SourceEventID != "evt-2" {
		t.Fatalf("unexpected deterministic winner: %+v", lease)
	}
}

func TestControllerProjectsExpiredEvent(t *testing.T) {
	base := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-1",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ObservedEventType,
		Subject:    "10.88.60.9/32",
		ObservedAt: base.Add(-time.Minute),
		ExpiresAt:  base.Add(time.Hour),
	})
	controller := Controller{Router: testRouter(), Store: store, Now: func() time.Time { return base }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "evt-2",
		Group:      "cloudedge",
		SourceNode: "onprem-router",
		Type:       ExpiredEventType,
		Subject:    "10.88.60.9/32",
		ObservedAt: base.Add(time.Minute),
		ExpiresAt:  base.Add(time.Hour),
	})
	controller.Now = func() time.Time { return base.Add(2 * time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("expired Reconcile: %v", err)
	}
	lease, _, err := store.GetAddressLease("cloudedge", "10.88.60.9/32")
	if err != nil {
		t.Fatalf("GetAddressLease: %v", err)
	}
	if lease.Status != routerstate.AddressLeaseStatusExpired || lease.SourceEventID != "evt-2" || lease.Epoch != 1 {
		t.Fatalf("unexpected expired lease: %+v", lease)
	}
}

func TestControllerProjectsStaticOwnedAddressAndEmitsObserved(t *testing.T) {
	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Router: staticRouter("onprem-router", staticPoolSpec()), Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	lease, found, err := store.GetAddressLease("cloudedge", "10.88.60.10/32")
	if err != nil {
		t.Fatalf("GetAddressLease: %v", err)
	}
	if !found {
		t.Fatal("static lease not found")
	}
	if lease.OwnerNode != "onprem-router" || lease.OwnerSite != "onprem" || lease.SourceType != staticOwnedType || lease.Status != routerstate.AddressLeaseStatusActive || lease.Epoch != 1 {
		t.Fatalf("unexpected static lease: %+v", lease)
	}
	if !lease.ExpiresAt.IsZero() {
		t.Fatalf("static lease expiresAt = %s, want zero", lease.ExpiresAt)
	}
	events, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if got := countEvents(events, ObservedEventType, "onprem-router", "10.88.60.10/32"); got != 1 {
		t.Fatalf("observed event count = %d, events=%+v", got, events)
	}
}

func TestControllerStaticOwnedRemovalExpiresLeaseAndEmitsExpired(t *testing.T) {
	base := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	spec := staticPoolSpec()
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: store, Now: func() time.Time { return base }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}

	spec.Members[0].StaticOwnedAddresses = nil
	controller.Router = staticRouter("onprem-router", spec)
	controller.Now = func() time.Time { return base.Add(time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("removal Reconcile: %v", err)
	}
	lease, _, err := store.GetAddressLease("cloudedge", "10.88.60.10/32")
	if err != nil {
		t.Fatalf("GetAddressLease: %v", err)
	}
	if lease.Status != routerstate.AddressLeaseStatusExpired || lease.OwnerNode != "onprem-router" || lease.SourceType != ExpiredEventType {
		t.Fatalf("unexpected expired static lease: %+v", lease)
	}
	events, err := store.ListFederationEvents("cloudedge", true, base.Add(time.Minute).Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if got := countEvents(events, ExpiredEventType, "onprem-router", "10.88.60.10/32"); got != 1 {
		t.Fatalf("expired event count = %d, events=%+v", got, events)
	}
}

func TestControllerStaticHandoverWaitsForFromReleaseEvent(t *testing.T) {
	base := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	spec := staticPoolSpec()
	controller := Controller{Router: staticRouter("onprem-router", spec), Store: store, Now: func() time.Time { return base }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}

	spec.Members[0].StaticOwnedAddresses = nil
	spec.StaticHandovers = []api.MobilityStaticHandover{{Address: "10.88.60.10/32", FromNodeRef: "onprem-router", ToNodeRef: "azure-router"}}
	controller.Router = staticRouter("azure-router", spec)
	controller.Now = func() time.Time { return base.Add(time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("cloud handover before release Reconcile: %v", err)
	}
	lease, _, err := store.GetAddressLease("cloudedge", "10.88.60.10/32")
	if err != nil {
		t.Fatalf("GetAddressLease before release: %v", err)
	}
	if lease.OwnerNode != "onprem-router" {
		t.Fatalf("owner changed before release event: %+v", lease)
	}

	controller.Router = staticRouter("onprem-router", spec)
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("onprem release Reconcile: %v", err)
	}
	controller.Router = staticRouter("azure-router", spec)
	controller.Now = func() time.Time { return base.Add(time.Minute + 31*time.Second) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("cloud handover after release Reconcile: %v", err)
	}
	lease, _, err = store.GetAddressLease("cloudedge", "10.88.60.10/32")
	if err != nil {
		t.Fatalf("GetAddressLease after release: %v", err)
	}
	if lease.OwnerNode != "azure-router" || lease.OwnerSite != "azure" || lease.SourceType != staticHandoverType || lease.Status != routerstate.AddressLeaseStatusActive || lease.Epoch != 2 {
		t.Fatalf("unexpected handed-over lease: %+v", lease)
	}
}

func TestControllerEmitsAutoFailoverHeartbeatForCloudSelf(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := centralizedOwnershipPoolSpec()
	spec.IPOwnershipPolicy.AutoFailover = true
	spec.IPOwnershipPolicy.HeartbeatInterval = "10s"
	spec.IPOwnershipPolicy.HeartbeatTTL = "30s"
	controller := Controller{Router: planningRouterForNode("azure-router-a", spec), Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	var heartbeats []routerstate.EventRecord
	for _, ev := range events {
		if ev.Type == HeartbeatEventType {
			heartbeats = append(heartbeats, ev)
		}
	}
	if len(heartbeats) != 1 {
		t.Fatalf("heartbeats = %+v, want one", heartbeats)
	}
	if heartbeats[0].SourceNode != "azure-router-a" || heartbeats[0].Payload["pool"] != "cloudedge" || heartbeats[0].Payload["node"] != "azure-router-a" {
		t.Fatalf("heartbeat = %+v", heartbeats[0])
	}
}

func TestOwnershipLivenessSurvivesCompactedHeartbeatStream(t *testing.T) {
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := testStore(t, base)
	spec := centralizedOwnershipPoolSpec()
	spec.IPOwnershipPolicy.AutoFailover = true
	spec.IPOwnershipPolicy.HeartbeatInterval = "10s"
	spec.IPOwnershipPolicy.HeartbeatTTL = "30s"
	spec.IPOwnershipPolicy.PromotionHoldDuration = "20s"

	for hour := 0; hour < 24; hour++ {
		observed := base.Add(time.Duration(hour) * time.Hour)
		recordEvent(t, store, routerstate.EventRecord{
			ID:         fmt.Sprintf("hb-a-%02d", hour),
			Group:      "cloudedge",
			SourceNode: "azure-router-a",
			Type:       HeartbeatEventType,
			Subject:    "cloudedge/azure-router-a",
			DedupeKey:  "mobility-heartbeat:cloudedge:azure-router-a",
			Payload:    map[string]string{"pool": "cloudedge", "node": "azure-router-a", "seq": fmt.Sprint(hour)},
			ObservedAt: observed,
			RecordedAt: observed,
		})
	}
	fresh := base.Add(25 * time.Hour)
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "hb-b-fresh",
		Group:      "cloudedge",
		SourceNode: "azure-router-b",
		Type:       HeartbeatEventType,
		Subject:    "cloudedge/azure-router-b",
		DedupeKey:  "mobility-heartbeat:cloudedge:azure-router-b",
		Payload:    map[string]string{"pool": "cloudedge", "node": "azure-router-b"},
		ObservedAt: fresh,
		RecordedAt: fresh,
	})

	events, err := store.ListFederationEvents("cloudedge", true, fresh.Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("compacted heartbeat events = %d, want 2: ids=%v", len(events), idsOfEvents(events))
	}
	controller := Controller{Router: planningRouterForNode("azure-router-b", spec), Store: store, Now: func() time.Time { return fresh }}
	view, err := controller.ownershipLiveness("cloudedge", spec, fresh)
	if err != nil {
		t.Fatalf("ownershipLiveness: %v", err)
	}
	if !view.StaleNodes["azure-router-a"] {
		t.Fatalf("azure-router-a should remain stale after heartbeat compaction: %+v", view)
	}
	if view.StaleNodes["azure-router-b"] {
		t.Fatalf("fresh router marked stale after heartbeat compaction: %+v", view)
	}
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
		Type:                  "centralized",
		AutoFailover:          true,
		HeartbeatInterval:     "10s",
		HeartbeatTTL:          "30s",
		PromotionHoldDuration: "0s",
	}
	recordEvent(t, store, routerstate.EventRecord{
		ID:         "hb-a",
		Group:      "cloudedge",
		SourceNode: "aws-router-a",
		Type:       HeartbeatEventType,
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
	if assign.Parameters[ownershipParamEpoch] != "" || assign.Parameters[captureParamEpoch] != "" || assign.Parameters[bgpPathSigParam] == "" {
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
	if err := store.UpsertAddressLease(routerstate.AddressLeaseRecord{
		Pool:      "cloudedge",
		Address:   "10.88.60.10/32",
		Status:    routerstate.AddressLeaseStatusActive,
		OwnerNode: "onprem-router",
		OwnerSite: "onprem",
		OwnerRole: "onprem",
		Epoch:     1,
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("UpsertAddressLease: %v", err)
	}
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
	if assignB.Parameters[ownershipParamOwner] != "" || assignB.Parameters[captureParamEpoch] != "" || assignB.Parameters[bgpPathSigParam] == "" || assignB.Parameters[captureParamHolder] != "azure-router-b" {
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
	saveBGPInstalledNextHops(t, store, map[string][]string{
		"10.88.60.10/32": {"10.99.0.1"},
		"10.88.60.11/32": {"10.99.0.2"},
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
	for _, address := range []string{"10.88.60.10/32", "10.88.60.12/32", "10.88.60.13/32"} {
		assign := findActionPlanByAddress(plans, "assign-secondary-ip", address)
		if assign == nil {
			t.Fatalf("plans = %#v, want remote trap assign for %s", plans, address)
		}
		if assign.Parameters[captureParamHolder] != "aws-router-a" {
			t.Fatalf("assign %s parameters = %#v, want trap holder aws-router-a", address, assign.Parameters)
		}
		if assign.Parameters[bgpPathSigParam] == "" || assign.Parameters[captureParamEpoch] != "" {
			t.Fatalf("assign %s parameters = %#v, want BGP path fence without capture epoch", address, assign.Parameters)
		}
	}
	if findActionPlanByAddress(plans, "assign-secondary-ip", "10.88.60.11/32") != nil {
		t.Fatalf("plans = %#v, want no same-site/self-owned trap assign", plans)
	}
}

func TestControllerBGPModeProviderTrapRecapturesAfterSuccessfulRelease(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	if _, err := store.ReconcileMobilityCaptureEpochs([]routerstate.MobilityCaptureEpochRecord{
		awsFailoverCaptureEpoch("10.88.60.10/32", "aws-router-a", 1),
		awsFailoverCaptureEpoch("10.88.60.12/32", "aws-router-a", 1),
		awsFailoverCaptureEpoch("10.88.60.13/32", "aws-router-a", 1),
	}); err != nil {
		t.Fatalf("seed capture epochs: %v", err)
	}
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
		if assign.Parameters[bgpPathSigParam] == "" || assign.Parameters[captureParamEpoch] != "" {
			t.Fatalf("assign %s parameters = %#v, want BGP path fence after release", address, assign.Parameters)
		}
		if assign.Parameters["allowReassignment"] != "true" {
			t.Fatalf("assign %s parameters = %#v, want reassignment after successful release", address, assign.Parameters)
		}
	}
}

func TestControllerBGPModeProviderTrapRecapturesWhenObservedProviderStateLost(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := awsFailoverPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
	if _, err := store.ReconcileMobilityCaptureEpochs([]routerstate.MobilityCaptureEpochRecord{
		awsFailoverCaptureEpoch("10.88.60.10/32", "aws-router-a", 1),
	}); err != nil {
		t.Fatalf("seed capture epochs: %v", err)
	}
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
	if assign.Parameters[bgpPathSigParam] == "" || assign.Parameters[captureParamEpoch] != "" {
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
	if secondAssign.Parameters[bgpPathSigParam] == "" || secondAssign.Parameters[captureParamEpoch] != "" {
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
	if assign.Parameters[bgpPathSigParam] == "" || assign.Parameters[captureParamEpoch] != "" || assign.Parameters[captureParamHolder] != "azure-router" {
		t.Fatalf("assign parameters = %#v, want BGP path fence for static-owned trap", assign.Parameters)
	}
	if assign.Parameters[ownershipParamEpoch] != "" {
		t.Fatalf("assign parameters = %#v, want no stale ownership fence when ownership row is missing", assign.Parameters)
	}
}

func TestControllerBGPModeProviderTrapRIBStartupIsConservative(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	if err := store.UpsertAddressLease(routerstate.AddressLeaseRecord{
		Pool:      "cloudedge",
		Address:   "10.88.60.10/32",
		Status:    routerstate.AddressLeaseStatusActive,
		OwnerNode: "onprem-router",
		OwnerSite: "onprem",
		OwnerRole: "onprem",
		Epoch:     1,
		ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("UpsertAddressLease: %v", err)
	}
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
	if findActionPlanByAddress(emptyPlans, "unassign-secondary-ip", "10.88.60.10/32") == nil {
		t.Fatalf("observed empty plans = %#v, want stale trap unassign", emptyPlans)
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
	if unassign.Parameters[bgpPathSigParam] == "" || unassign.Parameters[captureParamEpoch] != "" {
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

func TestControllerRouteModeDeletesBGPPathsAndKeepsLegacySAMPlanner(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
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
	if len(bgp.deletes) != 1 || bgp.deletes[0].Prefix != "10.88.60.10/32" {
		t.Fatalf("route mode BGP deletes = %#v, want rollback cleanup", bgp.deletes)
	}
	part := latestPart(t, store, source)
	resources := decodeResources(t, part.ResourcesJSON)
	if countKind(resources, "RemoteAddressClaim") != 1 {
		t.Fatalf("route mode resources = %#v, want legacy RemoteAddressClaim", resources)
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
				LeasePolicy: api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
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
		LeasePolicy: api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
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
