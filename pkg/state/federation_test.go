// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"fmt"
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

func TestRecordFederationMobilityHeartbeatsCompactToLatestPerGroupNode(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for minute := 0; minute < 24*60; minute += 5 {
		now := base.Add(time.Duration(minute) * time.Minute)
		for _, node := range []string{"aws-router", "azure-router"} {
			if err := store.RecordFederationEvent(EventRecord{
				ID:         fmt.Sprintf("hb-%s-%04d", node, minute),
				Group:      "cloudedge",
				SourceNode: node,
				Type:       mobilityHeartbeatEventType,
				Subject:    "cloudedge/" + node,
				DedupeKey:  heartbeatDedupeKeyForTest(node),
				Payload: map[string]string{
					"pool": "cloudedge",
					"node": node,
					"seq":  fmt.Sprint(minute),
				},
				ObservedAt: now,
				RecordedAt: now,
			}); err != nil {
				t.Fatalf("record heartbeat %s minute %d: %v", node, minute, err)
			}
		}
	}
	if err := store.RecordFederationEvent(EventRecord{
		ID:         "observed-client",
		Group:      "cloudedge",
		SourceNode: "onprem",
		Type:       "routerd.client.ipv4.observed",
		Subject:    "10.77.60.10/32",
		ObservedAt: base.Add(24*time.Hour + time.Minute),
	}); err != nil {
		t.Fatalf("record non-heartbeat event: %v", err)
	}

	events, err := store.ListFederationEvents("cloudedge", false, base.Add(25*time.Hour).Unix())
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if got, want := len(events), 3; got != want {
		t.Fatalf("event rows after 24h heartbeats = %d, want %d: ids=%v", got, want, idsOf(events))
	}
	wantLatest := map[string]string{
		"mobility-heartbeat:cloudedge:aws-router":   "1435",
		"mobility-heartbeat:cloudedge:azure-router": "1435",
	}
	for _, ev := range events {
		if wantSeq, ok := wantLatest[ev.DedupeKey]; ok {
			if ev.Payload["seq"] != wantSeq {
				t.Fatalf("latest heartbeat %s seq = %q, want %q", ev.DedupeKey, ev.Payload["seq"], wantSeq)
			}
			delete(wantLatest, ev.DedupeKey)
		}
	}
	if len(wantLatest) != 0 {
		t.Fatalf("missing latest heartbeats for %v", wantLatest)
	}
	stats, err := store.FederationHeartbeatCompactionStats("cloudedge")
	if err != nil {
		t.Fatalf("heartbeat compaction stats: %v", err)
	}
	if stats.DuplicateRows != 0 || len(stats.Keys) != 0 {
		t.Fatalf("heartbeat duplicates after write compaction = %+v", stats)
	}
}

func TestRecordFederationProviderDiscoveryObservedCompactsToLatest(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	base := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		now := base.Add(time.Duration(i) * time.Minute)
		if err := store.RecordFederationEvent(EventRecord{
			ID:         fmt.Sprintf("provider-discovery-%d", i),
			Group:      "cloudedge",
			SourceNode: "azure-router-a",
			Type:       "routerd.client.ipv4.observed",
			Subject:    "10.88.60.11/32",
			DedupeKey:  "mobility:provider-discovery:cloudedge:azure-router-a:10.88.60.11_32",
			Payload:    map[string]string{"source": "provider-discovery", "address": "10.88.60.11/32"},
			ObservedAt: now,
			ExpiresAt:  now.Add(5 * time.Minute),
		}); err != nil {
			t.Fatalf("record provider discovery %d: %v", i, err)
		}
	}
	if err := store.RecordFederationEvent(EventRecord{
		ID:         "manual-observed",
		Group:      "cloudedge",
		SourceNode: "azure-router-a",
		Type:       "routerd.client.ipv4.observed",
		Subject:    "10.88.60.12/32",
		DedupeKey:  "manual-observed",
		ObservedAt: base,
	}); err != nil {
		t.Fatalf("record manual observed: %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", true, base.Add(10*time.Minute).Unix())
	if err != nil {
		t.Fatalf("ListFederationEvents: %v", err)
	}
	if ids := idsOf(events); !equalIDs(ids, []string{"manual-observed", "provider-discovery-2"}) {
		t.Fatalf("ids = %v, want manual plus latest provider discovery", ids)
	}
}

func heartbeatDedupeKeyForTest(node string) string {
	if node == "azure-router" {
		return ""
	}
	return "mobility-heartbeat:cloudedge:" + node
}

func TestCompactFederationHeartbeatsPrunesLegacyDuplicatesPerGroup(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	insertLegacyHeartbeat := func(id, group, key string, observed time.Time) {
		t.Helper()
		if _, err := store.db.Exec(`
			INSERT INTO federation_events (id, group_name, source_node, type, subject, dedupe_key, payload, observed_at, expires_at, recorded_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)
		`, id, group, "aws-router", mobilityHeartbeatEventType, "cloudedge/aws-router", key, `{"pool":"cloudedge","node":"aws-router"}`, observed.Unix(), observed.Unix()); err != nil {
			t.Fatalf("insert legacy heartbeat %s: %v", id, err)
		}
	}
	insertLegacyHeartbeat("g1-old", "g1", "mobility-heartbeat:cloudedge:aws-router", base)
	insertLegacyHeartbeat("g1-new", "g1", "mobility-heartbeat:cloudedge:aws-router", base.Add(time.Minute))
	insertLegacyHeartbeat("g2-old", "g2", "mobility-heartbeat:cloudedge:aws-router", base)
	insertLegacyHeartbeat("g2-new", "g2", "mobility-heartbeat:cloudedge:aws-router", base.Add(2*time.Minute))

	stats, err := store.FederationHeartbeatCompactionStats("")
	if err != nil {
		t.Fatalf("heartbeat compaction stats before: %v", err)
	}
	if stats.DuplicateRows != 2 {
		t.Fatalf("duplicates before compaction = %+v, want 2 duplicate rows", stats)
	}
	removed, err := store.CompactFederationHeartbeats("")
	if err != nil {
		t.Fatalf("compact heartbeats: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	for _, group := range []string{"g1", "g2"} {
		events, err := store.ListFederationEvents(group, false, base.Add(time.Hour).Unix())
		if err != nil {
			t.Fatalf("list %s: %v", group, err)
		}
		if len(events) != 1 {
			t.Fatalf("%s events after compaction = %v, want 1", group, idsOf(events))
		}
		if events[0].ID != group+"-new" {
			t.Fatalf("%s retained id = %q, want %s-new", group, events[0].ID, group)
		}
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
