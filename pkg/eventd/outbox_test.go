// SPDX-License-Identifier: BSD-3-Clause

package eventd_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// seedEventFrom records a federation event with an explicit SourceNode so tests
// can distinguish locally-originated events from events received from a peer.
func seedEventFrom(t *testing.T, store *routerstate.SQLiteStore, id, group, source, subject string, observed time.Time) {
	t.Helper()
	ev := federation.Event{
		ID:         id,
		Group:      group,
		SourceNode: source,
		Type:       "routerd.client.ipv4.observed",
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
}

// TestOutboxLoopPrevention verifies the ADR loop-prevention invariant: only
// locally-originated events (SourceNode == nodeName) are pushed; events received
// from a peer (different SourceNode) are never re-pushed.
func TestOutboxLoopPrevention(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	const group = "cloudedge"
	const nodeName = "onprem"
	const peerName = "cloud-a"

	store := openStore(t, "outbox.db")

	// Count POSTs that reach the receiver.
	var posts atomic.Uint64
	receiver := eventd.NewReceiver(store, testSecret, group, "cloud", 5*time.Minute, clock)
	mux := http.NewServeMux()
	receiver.Register(mux)
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/events" {
			posts.Add(1)
		}
		mux.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(wrapped)
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: srv.URL}}
	noSleep := func(time.Duration) {}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, noSleep)

	// Local event (push it) and a received event from a peer (must NOT push).
	seedEventFrom(t, store, "local-1", group, nodeName, "10.88.60.9/32", now)
	seedEventFrom(t, store, "remote-1", group, "some-peer", "10.88.60.50/32", now)

	outbox := eventd.NewOutbox(store, store, pusher, group, nodeName, time.Second, clock)
	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// Exactly one POST (the local event); the received event was not re-pushed.
	if got := posts.Load(); got != 1 {
		t.Fatalf("receiver POST count = %d, want 1 (only local event)", got)
	}

	// local-1 delivered; remote-1 has no delivery row at all.
	local, _ := store.ListDeliveries("local-1", peerName)
	if len(local) != 1 || local[0].Status != routerstate.DeliveryDelivered {
		t.Fatalf("local-1 delivery = %+v, want one delivered row", local)
	}
	remote, _ := store.ListDeliveries("remote-1", "")
	if len(remote) != 0 {
		t.Fatalf("remote-1 delivery rows = %d, want 0 (loop prevention)", len(remote))
	}
}

// TestOutboxIdempotent verifies a second RunOnce does not re-push an
// already-delivered event nor bump its attempt counter.
func TestOutboxIdempotent(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	const group = "cloudedge"
	const nodeName = "onprem"
	const peerName = "cloud-a"

	store := openStore(t, "outbox-idem.db")

	var posts atomic.Uint64
	receiver := eventd.NewReceiver(store, testSecret, group, "cloud", 5*time.Minute, clock)
	mux := http.NewServeMux()
	receiver.Register(mux)
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/events" {
			posts.Add(1)
		}
		mux.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(wrapped)
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: srv.URL}}
	noSleep := func(time.Duration) {}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, noSleep)

	seedEventFrom(t, store, "local-1", group, nodeName, "10.88.60.9/32", now)
	outbox := eventd.NewOutbox(store, store, pusher, group, nodeName, time.Second, clock)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #1: %v", err)
	}
	firstPosts := posts.Load()
	if firstPosts != 1 {
		t.Fatalf("after run #1 POST count = %d, want 1", firstPosts)
	}
	d1, _ := store.ListDeliveries("local-1", peerName)
	if len(d1) != 1 || d1[0].Status != routerstate.DeliveryDelivered {
		t.Fatalf("after run #1 delivery = %+v, want delivered", d1)
	}
	attemptsAfter1 := d1[0].Attempts

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #2: %v", err)
	}
	if got := posts.Load(); got != firstPosts {
		t.Fatalf("after run #2 POST count = %d, want unchanged %d", got, firstPosts)
	}
	d2, _ := store.ListDeliveries("local-1", peerName)
	if len(d2) != 1 || d2[0].Attempts != attemptsAfter1 {
		t.Fatalf("after run #2 attempts = %+v, want unchanged (%d)", d2, attemptsAfter1)
	}
}

// seedEventExpiring records a locally-originated event with an explicit
// ExpiresAt so tests can exercise the non-expired list filter the Outbox relies
// on (ListFederationEvents includeExpired=false).
func seedEventExpiring(t *testing.T, store *routerstate.SQLiteStore, id, group, source, subject string, observed, expires time.Time) {
	t.Helper()
	ev := federation.Event{
		ID:         id,
		Group:      group,
		SourceNode: source,
		Type:       "routerd.client.ipv4.observed",
		Subject:    subject,
		Payload:    map[string]string{"mac": "aa:bb:cc:dd:ee:ff"},
		ObservedAt: observed,
		ExpiresAt:  expires,
	}
	if err := ev.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	rec := routerstate.EventRecord{
		ID: ev.ID, Group: ev.Group, SourceNode: ev.SourceNode, Type: ev.Type,
		Subject: ev.Subject, DedupeKey: ev.DedupeKey, Payload: ev.Payload,
		ObservedAt: ev.ObservedAt, ExpiresAt: ev.ExpiresAt,
	}
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("record event: %v", err)
	}
}

// TestOutboxSkipsExpiredEvent verifies the Outbox never pushes an already-expired
// locally-originated event: RunOnce lists events with includeExpired=false, so an
// event whose ExpiresAt is in the past is filtered out before the push loop. A
// non-expired sibling event is delivered in the same pass, proving the skip is
// due to expiry (not a broken sender/receiver setup).
func TestOutboxSkipsExpiredEvent(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	const group = "cloudedge"
	const nodeName = "onprem"
	const peerName = "cloud-a"

	store := openStore(t, "outbox-expired.db")

	var posts atomic.Uint64
	receiver := eventd.NewReceiver(store, testSecret, group, "cloud", 5*time.Minute, clock)
	mux := http.NewServeMux()
	receiver.Register(mux)
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/events" {
			posts.Add(1)
		}
		mux.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(wrapped)
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: srv.URL}}
	noSleep := func(time.Duration) {}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, noSleep)

	// Expired local event (must NOT push) and a fresh local event (positive control).
	seedEventExpiring(t, store, "expired-1", group, nodeName, "10.88.60.9/32", now.Add(-2*time.Minute), now.Add(-1*time.Minute))
	seedEventExpiring(t, store, "fresh-1", group, nodeName, "10.88.60.10/32", now, now.Add(30*time.Minute))

	outbox := eventd.NewOutbox(store, store, pusher, group, nodeName, time.Second, clock)
	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// Exactly one POST: only the fresh event reached the receiver.
	if got := posts.Load(); got != 1 {
		t.Fatalf("receiver POST count = %d, want 1 (only fresh event)", got)
	}

	// Expired event was never pushed: no delivery row for it.
	expired, _ := store.ListDeliveries("expired-1", "")
	if len(expired) != 0 {
		t.Fatalf("expired-1 delivery rows = %d, want 0 (expired skipped)", len(expired))
	}

	// Positive control: the fresh event WAS delivered.
	fresh, _ := store.ListDeliveries("fresh-1", peerName)
	if len(fresh) != 1 || fresh[0].Status != routerstate.DeliveryDelivered {
		t.Fatalf("fresh-1 delivery = %+v, want one delivered row", fresh)
	}
}

// TestOutboxRestartResend verifies an undelivered (failed) event is resent by a
// fresh Outbox over the same store once the peer endpoint recovers — the
// restart/peer-recovery resend property.
func TestOutboxRestartResend(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	const group = "cloudedge"
	const nodeName = "onprem"
	const peerName = "cloud-a"

	store := openStore(t, "outbox-restart.db")
	noSleep := func(time.Duration) {}
	seedEventFrom(t, store, "local-1", group, nodeName, "10.88.60.9/32", now)

	// First outbox: peer always 500s → delivery fails.
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	failPeers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: failSrv.URL}}
	failPusher := eventd.NewPusher(store, testSecret, failPeers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		failSrv.Client(), clock, noSleep)
	failOutbox := eventd.NewOutbox(store, store, failPusher, group, nodeName, time.Second, clock)
	if err := failOutbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("fail RunOnce: %v", err)
	}
	failSrv.Close()

	d, _ := store.ListDeliveries("local-1", peerName)
	if len(d) != 1 || d[0].Status != routerstate.DeliveryFailed {
		t.Fatalf("after fail run delivery = %+v, want failed", d)
	}

	// "Restart": a NEW Outbox over the SAME store, peer now healthy.
	recvStore := openStore(t, "outbox-restart-recv.db")
	receiver := eventd.NewReceiver(recvStore, testSecret, group, "cloud", 5*time.Minute, clock)
	okSrv := httptest.NewServer(receiver.Handler())
	defer okSrv.Close()
	okPeers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: okSrv.URL}}
	okPusher := eventd.NewPusher(store, testSecret, okPeers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		okSrv.Client(), clock, noSleep)
	okOutbox := eventd.NewOutbox(store, store, okPusher, group, nodeName, time.Second, clock)
	if err := okOutbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("recover RunOnce: %v", err)
	}

	d2, _ := store.ListDeliveries("local-1", peerName)
	if len(d2) != 1 || d2[0].Status != routerstate.DeliveryDelivered {
		t.Fatalf("after recover delivery = %+v, want delivered", d2)
	}
	got, _ := recvStore.ListFederationEvents(group, true, now.Unix())
	if len(got) != 1 {
		t.Fatalf("receiver event count = %d, want 1 (resent after restart)", len(got))
	}
}

// TestOutboxRepushOnTTLRefresh verifies the core fix for #529: when an event's
// ExpiresAt is extended (TTL refreshed by re-emission), the outbox re-pushes
// even though the event was previously delivered.
func TestOutboxRepushOnTTLRefresh(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	const group = "cloudedge"
	const nodeName = "rr-node"
	const peerName = "leaf-a"

	store := openStore(t, "outbox-ttl.db")

	var posts atomic.Uint64
	receiver := eventd.NewReceiver(store, testSecret, group, "leaf", 5*time.Minute, clock)
	mux := http.NewServeMux()
	receiver.Register(mux)
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/events" {
			posts.Add(1)
		}
		mux.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(wrapped)
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: peerName, Endpoint: srv.URL}}
	noSleep := func(time.Duration) {}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, noSleep)

	expires1 := now.Add(10 * time.Minute)
	seedEventExpiring(t, store, "shard-1", group, nodeName, "10.77.60.0/25", now, expires1)

	outbox := eventd.NewOutbox(store, store, pusher, group, nodeName, time.Second, clock)
	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #1: %v", err)
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("after run #1 POST count = %d, want 1", got)
	}
	d1, _ := store.ListDeliveries("shard-1", peerName)
	if len(d1) != 1 || d1[0].Status != routerstate.DeliveryDelivered {
		t.Fatalf("after run #1 delivery = %+v, want delivered", d1)
	}
	if !d1[0].EventExpiresAt.Equal(expires1) {
		t.Fatalf("delivery EventExpiresAt = %v, want %v", d1[0].EventExpiresAt, expires1)
	}

	// Second RunOnce without TTL refresh: should NOT re-push.
	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #2: %v", err)
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("after run #2 POST count = %d, want 1 (no re-push)", got)
	}

	// Simulate TTL refresh: re-record same event with extended ExpiresAt.
	expires2 := now.Add(20 * time.Minute)
	if err := store.RecordFederationEvent(routerstate.EventRecord{
		ID: "shard-1", Group: group, SourceNode: nodeName,
		Type: "routerd.client.ipv4.observed", Subject: "10.77.60.0/25",
		DedupeKey: "10.77.60.0/25", Payload: map[string]string{"mac": "aa:bb:cc:dd:ee:ff"},
		ObservedAt: now, ExpiresAt: expires2,
	}); err != nil {
		t.Fatalf("re-record with extended TTL: %v", err)
	}

	// Third RunOnce: event expires_at moved forward → outbox MUST re-push.
	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #3: %v", err)
	}
	if got := posts.Load(); got != 2 {
		t.Fatalf("after run #3 POST count = %d, want 2 (re-pushed after TTL refresh)", got)
	}
	d3, _ := store.ListDeliveries("shard-1", peerName)
	if len(d3) != 1 || d3[0].Status != routerstate.DeliveryDelivered {
		t.Fatalf("after run #3 delivery = %+v, want delivered", d3)
	}
	if !d3[0].EventExpiresAt.Equal(expires2) {
		t.Fatalf("after run #3 EventExpiresAt = %v, want %v (updated)", d3[0].EventExpiresAt, expires2)
	}
}
