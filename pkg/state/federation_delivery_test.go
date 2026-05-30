// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"testing"
	"time"
)

func TestDeliveryRecordUpdateListRoundTrip(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	// Enqueue is idempotent: second RecordDelivery is a no-op.
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-a"); err != nil {
		t.Fatalf("record delivery duplicate: %v", err)
	}
	if err := store.RecordDelivery("evt-1", "peer-b"); err != nil {
		t.Fatalf("record delivery peer-b: %v", err)
	}

	got, err := store.ListDeliveries("evt-1", "")
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 delivery rows, got %d", len(got))
	}
	for _, d := range got {
		if d.Status != DeliveryPending {
			t.Fatalf("initial status = %q, want pending", d.Status)
		}
		if d.Attempts != 0 {
			t.Fatalf("initial attempts = %d, want 0", d.Attempts)
		}
		if !d.LastAttemptAt.IsZero() || !d.DeliveredAt.IsZero() {
			t.Fatalf("initial times should be zero: %+v", d)
		}
	}

	delivered := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	if err := store.UpdateDeliveryStatus("evt-1", "peer-a", DeliveryDelivered, 1, "", delivered); err != nil {
		t.Fatalf("update delivered: %v", err)
	}
	if err := store.UpdateDeliveryStatus("evt-1", "peer-b", DeliveryFailed, 3, "connection refused", time.Time{}); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	a, err := store.ListDeliveries("evt-1", "peer-a")
	if err != nil {
		t.Fatalf("list peer-a: %v", err)
	}
	if len(a) != 1 || a[0].Status != DeliveryDelivered || a[0].Attempts != 1 {
		t.Fatalf("peer-a round-trip wrong: %+v", a)
	}
	if !a[0].DeliveredAt.Equal(delivered) {
		t.Fatalf("peer-a deliveredAt = %v, want %v", a[0].DeliveredAt, delivered)
	}
	if a[0].LastAttemptAt.IsZero() {
		t.Fatalf("peer-a lastAttemptAt should be stamped from store clock")
	}

	b, err := store.ListDeliveries("evt-1", "peer-b")
	if err != nil {
		t.Fatalf("list peer-b: %v", err)
	}
	if len(b) != 1 || b[0].Status != DeliveryFailed || b[0].Attempts != 3 {
		t.Fatalf("peer-b round-trip wrong: %+v", b)
	}
	if b[0].LastError != "connection refused" {
		t.Fatalf("peer-b lastError = %q", b[0].LastError)
	}
	if !b[0].DeliveredAt.IsZero() {
		t.Fatalf("peer-b deliveredAt should be zero (not delivered): %v", b[0].DeliveredAt)
	}

	// Wildcard peer filter on a single peer returns deliveries across events.
	all, err := store.ListDeliveries("", "peer-a")
	if err != nil {
		t.Fatalf("list all peer-a: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("want 1 row for peer-a wildcard event, got %d", len(all))
	}
}

func TestPruneFederationEventsByAge(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	events := []EventRecord{
		{ID: "old", Group: "g1", Type: "t", ObservedAt: now.Add(-2 * time.Hour)},
		{ID: "mid", Group: "g1", Type: "t", ObservedAt: now.Add(-30 * time.Minute)},
		{ID: "new", Group: "g1", Type: "t", ObservedAt: now.Add(-5 * time.Minute)},
		{ID: "other", Group: "g2", Type: "t", ObservedAt: now.Add(-2 * time.Hour)},
	}
	for _, ev := range events {
		if err := store.RecordFederationEvent(ev); err != nil {
			t.Fatalf("record %s: %v", ev.ID, err)
		}
	}

	// Prune g1 older than 1h -> only "old" deleted.
	deleted, err := store.PruneFederationEvents("g1", time.Hour, 0, now)
	if err != nil {
		t.Fatalf("prune by age: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	got, err := store.ListFederationEvents("g1", true, now.Unix())
	if err != nil {
		t.Fatalf("list g1: %v", err)
	}
	if ids := idsOf(got); !equalIDs(ids, []string{"mid", "new"}) {
		t.Fatalf("g1 after age prune = %v, want [mid new]", ids)
	}
	// g2 untouched.
	g2, err := store.ListFederationEvents("g2", true, now.Unix())
	if err != nil {
		t.Fatalf("list g2: %v", err)
	}
	if len(g2) != 1 {
		t.Fatalf("g2 should be untouched, got %d", len(g2))
	}
}

func TestPruneFederationEventsByCountKeepsNewest(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	events := []EventRecord{
		{ID: "e1", Group: "g1", Type: "t", ObservedAt: now.Add(-5 * time.Minute)},
		{ID: "e2", Group: "g1", Type: "t", ObservedAt: now.Add(-4 * time.Minute)},
		{ID: "e3", Group: "g1", Type: "t", ObservedAt: now.Add(-3 * time.Minute)},
		{ID: "e4", Group: "g1", Type: "t", ObservedAt: now.Add(-2 * time.Minute)},
		{ID: "e5", Group: "g1", Type: "t", ObservedAt: now.Add(-1 * time.Minute)},
	}
	for _, ev := range events {
		if err := store.RecordFederationEvent(ev); err != nil {
			t.Fatalf("record %s: %v", ev.ID, err)
		}
	}

	// Keep newest 2 -> e4, e5 survive; e1,e2,e3 deleted.
	deleted, err := store.PruneFederationEvents("g1", 0, 2, now)
	if err != nil {
		t.Fatalf("prune by count: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}
	got, err := store.ListFederationEvents("g1", true, now.Unix())
	if err != nil {
		t.Fatalf("list g1: %v", err)
	}
	if ids := idsOf(got); !equalIDs(ids, []string{"e4", "e5"}) {
		t.Fatalf("g1 after count prune = %v, want [e4 e5]", ids)
	}

	// Empty group skips the count cap (no cross-group surprises).
	if err := store.RecordFederationEvent(EventRecord{ID: "x1", Group: "g2", Type: "t", ObservedAt: now}); err != nil {
		t.Fatalf("record x1: %v", err)
	}
	deleted, err = store.PruneFederationEvents("", 0, 1, now)
	if err != nil {
		t.Fatalf("prune empty group by count: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("empty-group count prune deleted = %d, want 0 (skipped)", deleted)
	}
}
