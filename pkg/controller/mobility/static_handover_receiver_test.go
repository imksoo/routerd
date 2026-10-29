// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/mobilityfib"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func handoverReceiverSnapshot(self string, spec testMobilityPoolSpec, events []routerstate.EventRecord, now time.Time) PoolRuntimeSnapshot {
	pool := ownershipPoolFixture("cloudedge", self, spec)
	pool.Source = DynamicSource(pool.Name, self)
	return PoolRuntimeSnapshot{Pool: pool, Events: events, Now: now}
}

func handoverReleaseFixture(intent api.MobilityStaticHandover, now time.Time) routerstate.EventRecord {
	return routerstate.EventRecord{
		ID: "release-" + intent.ID, Group: "cloudedge", SourceNode: intent.FromNodeRef,
		Type: ExpiredEventType, Subject: intent.Address, ObservedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute),
		Payload: map[string]string{"address": intent.Address, "pool": "cloudedge", "handoverID": intent.ID,
			"fromNodeRef": intent.FromNodeRef, "toNodeRef": intent.ToNodeRef, "sourceType": staticHandoverType},
	}
}

func assertHandoverWithheld(t *testing.T, snapshot PoolRuntimeSnapshot) {
	t.Helper()
	plan, err := ReconcilePool(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.BGPPaths) != 0 {
		t.Fatalf("handover granted advertisement before its release proof: %+v", plan.BGPPaths)
	}
	for _, decision := range plan.Addresses {
		if decision.AdvertiseOwnerNode != "" || decision.HomeOwnerNode != "" || decision.Class != ownershipClassUnknown {
			t.Fatalf("handover invented ownership before its release proof: %+v", decision)
		}
	}
	for _, verdict := range plan.FIBVerdicts {
		if verdict.Address != "" && verdict.Action != mobilityfib.ActionWithhold {
			t.Fatalf("handover granted forwarding before release: %+v", verdict)
		}
	}
}

func TestStaticHandoverReceiverWaitsForExactRelease(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	spec := staticPoolSpec()
	intent := api.MobilityStaticHandover{ID: "move-one", Address: "10.88.60.10/32", FromNodeRef: "onprem-router", ToNodeRef: "azure-router"}
	spec.StaticHandovers = []api.MobilityStaticHandover{intent}
	assertHandoverWithheld(t, handoverReceiverSnapshot("azure-router", spec, nil, now))
	for _, scenario := range []struct {
		name  string
		alter func(*routerstate.EventRecord)
	}{
		{"other pool release", func(event *routerstate.EventRecord) { event.Payload["pool"] = "other-pool" }},
		{"unrelated DHCP expiry", func(event *routerstate.EventRecord) { event.Payload["sourceType"] = OnPremSourceDHCPv4Lease }},
		{"different target", func(event *routerstate.EventRecord) { event.Payload["toNodeRef"] = "different-router" }},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			event := handoverReleaseFixture(intent, now)
			scenario.alter(&event)
			assertHandoverWithheld(t, handoverReceiverSnapshot("azure-router", spec, []routerstate.EventRecord{event}, now))
		})
	}
	proof := handoverReleaseFixture(intent, now)
	plan, err := ReconcilePool(handoverReceiverSnapshot("azure-router", spec, []routerstate.EventRecord{proof}, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.BGPPaths) != 1 || plan.BGPPaths[0].Source != DynamicSource("cloudedge", "azure-router") || plan.BGPPaths[0].Prefix != intent.Address {
		t.Fatalf("exact released handover did not reach the actual BGP plan: %+v", plan.BGPPaths)
	}
	if !stringSliceContains(plan.BGPPaths[0].Attrs.Communities, bgpMobilityCommunitySourceHandover) {
		t.Fatalf("handover source identity lost: %+v", plan.BGPPaths[0])
	}
}

func TestStaticHandoverReceiverProofExpiresWithoutSelfRIBBypass(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	spec := staticPoolSpec()
	intent := api.MobilityStaticHandover{ID: "move-one", Address: "10.88.60.10/32", FromNodeRef: "onprem-router", ToNodeRef: "azure-router"}
	spec.StaticHandovers = []api.MobilityStaticHandover{intent}
	proof := handoverReleaseFixture(intent, now)
	snapshot := handoverReceiverSnapshot("azure-router", spec, []routerstate.EventRecord{proof}, proof.ExpiresAt)
	snapshot.BGP = BGPSnapshot{HomeOwnerNodes: map[string]string{intent.Address: "azure-router"}, InstalledObserved: true, CaptureRIBObserved: true}
	assertHandoverWithheld(t, snapshot)
}

func TestStaticHandoverReceiverSuppressesOldOwnerAndRejectsReplayAfterRollback(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	spec := staticPoolSpec()
	initial, err := ReconcilePool(handoverReceiverSnapshot("onprem-router", spec, nil, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.BGPPaths) != 1 {
		t.Fatal("ordinary static ownership became dependent on federation proof")
	}
	intent := api.MobilityStaticHandover{ID: "move-one", Address: "10.88.60.10/32", FromNodeRef: "onprem-router", ToNodeRef: "azure-router"}
	spec.StaticHandovers = []api.MobilityStaticHandover{intent}
	oldObserved := routerstate.EventRecord{ID: "old-static-observed", Group: "cloudedge", SourceNode: "onprem-router", Type: ObservedEventType, Subject: intent.Address, ObservedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Payload: map[string]string{"address": intent.Address, "pool": "cloudedge", "sourceType": staticOwnedType}}
	assertHandoverWithheld(t, handoverReceiverSnapshot("onprem-router", spec, []routerstate.EventRecord{oldObserved}, now))
	proof := handoverReleaseFixture(intent, now)
	spec.StaticHandovers = nil
	rollback, err := ReconcilePool(handoverReceiverSnapshot("onprem-router", spec, []routerstate.EventRecord{proof}, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(rollback.BGPPaths) != 1 || !stringSliceContains(rollback.BGPPaths[0].Attrs.Communities, bgpMobilityCommunitySourceStatic) {
		t.Fatal("old release proof revoked restored static-owned intent")
	}
	intent.ID = "move-two"
	spec.StaticHandovers = []api.MobilityStaticHandover{intent}
	assertHandoverWithheld(t, handoverReceiverSnapshot("azure-router", spec, []routerstate.EventRecord{proof}, now))
}

func TestStaticHandoverObservedEventCannotBecomeOrdinaryOwnership(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	spec := staticPoolSpec()
	event := routerstate.EventRecord{ID: "handover-observed", Group: "cloudedge", SourceNode: "azure-router", Type: ObservedEventType, Subject: "10.88.60.10/32", ObservedAt: now, ExpiresAt: now.Add(time.Minute), Payload: map[string]string{"address": "10.88.60.10/32", "pool": "cloudedge", "sourceType": staticHandoverType}}
	plan, err := ReconcilePool(handoverReceiverSnapshot("azure-router", spec, []routerstate.EventRecord{event}, now))
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.BGPPaths) != 0 {
		t.Fatalf("generic event path bypassed handover authorization: %+v", plan.BGPPaths)
	}
}
