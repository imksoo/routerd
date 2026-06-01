// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

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
