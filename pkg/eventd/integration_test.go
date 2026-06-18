// SPDX-License-Identifier: BSD-3-Clause

package eventd_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const testSecretHex = "0123456789abcdef0123456789abcdef"

var testSecret = []byte(testSecretHex)

func openStore(t *testing.T, name string) *routerstate.SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatalf("open store %s: %v", name, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func seedEvent(t *testing.T, store *routerstate.SQLiteStore, id, group, typ, subject string, observed time.Time) federation.Event {
	t.Helper()
	ev := federation.Event{
		ID:         id,
		Group:      group,
		SourceNode: "onprem",
		Type:       typ,
		Subject:    subject,
		Payload:    map[string]string{"mac": "aa:bb:cc:dd:ee:ff"},
		ObservedAt: observed,
	}
	if err := ev.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	rec := routerstate.EventRecord{
		ID: ev.ID, Group: ev.Group, SourceNode: ev.SourceNode, Type: ev.Type,
		Subject: ev.Subject, DedupeKey: ev.DedupeKey, Payload: ev.Payload, ObservedAt: ev.ObservedAt,
	}
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("record event: %v", err)
	}
	return ev
}

// TestPushDeliveryRoundTrip exercises the Phase 2 acceptance: sign+push from a
// sender to a receiver httptest endpoint, idempotency, tamper rejection,
// failure recording, and retention prune — all in-process, no real sockets or
// real sleeps.
func TestPushDeliveryRoundTrip(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)

	senderStore := openStore(t, "sender.db")
	receiverStore := openStore(t, "receiver.db")

	const group = "cloudedge"
	const peerName = "cloud-a"

	// Receiver mounted on an httptest.Server.
	receiver := eventd.NewReceiver(receiverStore, testSecret, group, "cloud", 5*time.Minute, clock)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: srv.URL}}
	noSleep := func(time.Duration) {}
	pusher := eventd.NewPusher(senderStore, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, noSleep)

	// 1-3: seed a local event on the sender and push it.
	ev := seedEvent(t, senderStore, "evt-1", group, "routerd.client.ipv4.observed", "10.88.60.9/32", now)
	if err := pusher.PushEvent(context.Background(), ev); err != nil {
		t.Fatalf("push: %v", err)
	}

	// 4a: receiver store has the event with payload/subject intact.
	got, err := receiverStore.ListFederationEvents(group, true, now.Unix())
	if err != nil {
		t.Fatalf("list receiver: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("receiver event count = %d, want 1", len(got))
	}
	if got[0].Subject != "10.88.60.9/32" {
		t.Errorf("subject = %q, want 10.88.60.9/32", got[0].Subject)
	}
	if got[0].Payload["mac"] != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("payload mac = %q, want aa:bb:cc:dd:ee:ff", got[0].Payload["mac"])
	}

	// 4b: sender delivery row is delivered with attempts>=1.
	deliveries, err := senderStore.ListDeliveries("evt-1", peerName)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("delivery count = %d, want 1", len(deliveries))
	}
	if deliveries[0].Status != routerstate.DeliveryDelivered {
		t.Errorf("status = %q, want delivered", deliveries[0].Status)
	}
	if deliveries[0].Attempts < 1 {
		t.Errorf("attempts = %d, want >=1", deliveries[0].Attempts)
	}

	// 5: re-push same event id → receiver still has exactly 1 (idempotent).
	if err := pusher.PushEvent(context.Background(), ev); err != nil {
		t.Fatalf("re-push: %v", err)
	}
	got, _ = receiverStore.ListFederationEvents(group, true, now.Unix())
	if len(got) != 1 {
		t.Fatalf("after re-push receiver event count = %d, want 1", len(got))
	}

	// 6a: tamper bad signature → 401, receiver store unchanged.
	body := mustMarshal(t, ev)
	badResp := postRaw(t, srv.URL+"/v1/events", now.Unix(), "deadbeef", body)
	if badResp != http.StatusUnauthorized {
		t.Errorf("bad signature status = %d, want 401", badResp)
	}

	// 6b: stale timestamp (well outside 5m window) → 403.
	staleTS := now.Add(-1 * time.Hour).Unix()
	staleSig := federation.Sign(testSecret, staleTS, body)
	staleResp := postRaw(t, srv.URL+"/v1/events", staleTS, staleSig, body)
	if staleResp != http.StatusForbidden {
		t.Errorf("stale timestamp status = %d, want 403", staleResp)
	}
	// Store still exactly 1.
	got, _ = receiverStore.ListFederationEvents(group, true, now.Unix())
	if len(got) != 1 {
		t.Fatalf("after tamper receiver event count = %d, want 1", len(got))
	}

	// 7: peer that always 500s → after MaxAttempts, status=failed, attempts==MaxAttempts.
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()
	failPeers := []eventd.PeerConfig{{NodeName: "bad-peer", Endpoint: failSrv.URL}}
	failPusher := eventd.NewPusher(senderStore, testSecret, failPeers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		failSrv.Client(), clock, noSleep)
	ev2 := seedEvent(t, senderStore, "evt-2", group, "routerd.client.ipv4.observed", "10.88.60.10/32", now)
	if err := failPusher.PushEvent(context.Background(), ev2); err != nil {
		t.Fatalf("fail push returned error (should record, not error): %v", err)
	}
	failDeliveries, _ := senderStore.ListDeliveries("evt-2", "bad-peer")
	if len(failDeliveries) != 1 {
		t.Fatalf("fail delivery count = %d, want 1", len(failDeliveries))
	}
	if failDeliveries[0].Status != routerstate.DeliveryFailed {
		t.Errorf("fail status = %q, want failed", failDeliveries[0].Status)
	}
	if failDeliveries[0].Attempts != 3 {
		t.Errorf("fail attempts = %d, want 3", failDeliveries[0].Attempts)
	}
	if failDeliveries[0].LastError == "" {
		t.Errorf("fail lastError is empty, want non-empty")
	}

	// 8: prune. Insert events with old observed_at; enforce MaxAge + MaxEvents.
	pruneStore := openStore(t, "prune.db")
	const pruneGroup = "prune-grp"
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-1 * time.Minute)
	seedEvent(t, pruneStore, "old-1", pruneGroup, "t", "s1", old)
	seedEvent(t, pruneStore, "old-2", pruneGroup, "t", "s2", old)
	seedEvent(t, pruneStore, "recent-1", pruneGroup, "t", "s3", recent)
	seedEvent(t, pruneStore, "recent-2", pruneGroup, "t", "s4", recent.Add(time.Second))
	seedEvent(t, pruneStore, "recent-3", pruneGroup, "t", "s5", recent.Add(2*time.Second))

	pruner := eventd.NewPruner(pruneStore, pruneGroup,
		eventd.Retention{MaxAge: 24 * time.Hour, MaxEvents: 2}, time.Minute, clock)
	if _, err := pruner.PruneOnce(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	remaining, _ := pruneStore.ListFederationEvents(pruneGroup, true, now.Unix())
	if len(remaining) != 2 {
		t.Fatalf("after prune count = %d, want 2 (newest kept)", len(remaining))
	}
	for _, r := range remaining {
		if r.ID == "old-1" || r.ID == "old-2" {
			t.Errorf("old event %q survived prune", r.ID)
		}
	}
}

func mustMarshal(t *testing.T, ev federation.Event) []byte {
	t.Helper()
	body, err := jsonMarshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}
