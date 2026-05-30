// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"path/filepath"
	"testing"
	"time"
)

func mustOpenStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	return store
}

func TestRecordEventIdempotent(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	rec := EventRecord{
		ID:         "evt-1",
		Group:      "cloudedge",
		SourceNode: "onprem",
		Type:       "routerd.client.ipv4.observed",
		Subject:    "10.88.60.9/32",
		Payload:    map[string]string{"mac": "aa:bb:cc:dd:ee:ff"},
		ObservedAt: time.Now().UTC(),
	}
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("record: %v", err)
	}
	// Duplicate id must be a no-op, not an error.
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("record duplicate: %v", err)
	}
	got, err := store.ListFederationEvents("", false, time.Now().Unix())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event after duplicate insert, got %d", len(got))
	}
	if got[0].Payload["mac"] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("payload round-trip failed: %v", got[0].Payload)
	}
	if got[0].DedupeKey != "evt-1" {
		t.Fatalf("dedupeKey default = %q, want evt-1", got[0].DedupeKey)
	}
}

func TestListEventsGroupAndExpiredFilter(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	events := []EventRecord{
		{ID: "a", Group: "g1", Type: "t", ObservedAt: now.Add(-3 * time.Minute)},
		{ID: "b", Group: "g1", Type: "t", ObservedAt: now.Add(-2 * time.Minute), ExpiresAt: now.Add(-time.Minute)},
		{ID: "c", Group: "g2", Type: "t", ObservedAt: now.Add(-time.Minute)},
		{ID: "d", Group: "g1", Type: "t", ObservedAt: now, ExpiresAt: now.Add(time.Hour)},
	}
	for _, ev := range events {
		if err := store.RecordFederationEvent(ev); err != nil {
			t.Fatalf("record %s: %v", ev.ID, err)
		}
	}

	// g1, exclude expired -> a, d (b expired).
	got, err := store.ListFederationEvents("g1", false, now.Unix())
	if err != nil {
		t.Fatalf("list g1: %v", err)
	}
	if ids := idsOf(got); !equalIDs(ids, []string{"a", "d"}) {
		t.Fatalf("g1 non-expired ids = %v, want [a d]", ids)
	}

	// g1, include expired -> a, b, d (ordered by observed_at).
	got, err = store.ListFederationEvents("g1", true, now.Unix())
	if err != nil {
		t.Fatalf("list g1 include expired: %v", err)
	}
	if ids := idsOf(got); !equalIDs(ids, []string{"a", "b", "d"}) {
		t.Fatalf("g1 all ids = %v, want [a b d]", ids)
	}

	// no group filter, exclude expired -> a, c, d.
	got, err = store.ListFederationEvents("", false, now.Unix())
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if ids := idsOf(got); !equalIDs(ids, []string{"a", "c", "d"}) {
		t.Fatalf("all non-expired ids = %v, want [a c d]", ids)
	}
}

func idsOf(recs []EventRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
