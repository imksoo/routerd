// SPDX-License-Identifier: BSD-3-Clause

package eventd_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestMetricsDeliveredIncrements(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	rec := &eventd.TestRecorder{}
	metrics := eventd.NewTestMetrics(rec)

	senderStore := openStore(t, "metrics-delivered.db")
	receiverStore := openStore(t, "metrics-delivered-recv.db")

	receiver := eventd.NewReceiver(receiverStore, testSecret, "cloudedge", "cloud", 5*time.Minute, clock)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: "cloud-a", Endpoint: srv.URL}}
	pusher := eventd.NewPusher(senderStore, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, func(time.Duration) {})
	pusher.SetMetrics(metrics)

	seedEventFrom(t, senderStore, "evt-1", "cloudedge", "onprem", "10.88.60.9/32", now)
	outbox := eventd.NewOutbox(senderStore, senderStore, pusher, "cloudedge", "onprem", time.Second, clock)
	outbox.SetMetrics(metrics)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := rec.Count("routerd_eventd_outbox_delivery_total", "status", "delivered"); got != 1 {
		t.Fatalf("delivery_total{status=delivered} = %d, want 1", got)
	}
	if got := rec.Count("routerd_eventd_outbox_delivery_total", "status", "failed"); got != 0 {
		t.Fatalf("delivery_total{status=failed} = %d, want 0", got)
	}
	if got := rec.Count("routerd_eventd_outbox_delivery_attempts_total", "status", "delivered"); got < 1 {
		t.Fatalf("attempts_total{status=delivered} = %d, want >= 1", got)
	}

	lags := rec.Values("routerd_eventd_outbox_delivery_lag_seconds")
	if len(lags) != 1 {
		t.Fatalf("delivery_lag_seconds records = %d, want 1", len(lags))
	}
	if lags[0] < 0 {
		t.Fatalf("delivery_lag_seconds = %f, want >= 0", lags[0])
	}

	if label, found := rec.HasForbiddenLabel(); found {
		t.Fatalf("forbidden label %q found in metrics", label)
	}
}

func TestMetricsFailedIncrements(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	rec := &eventd.TestRecorder{}
	metrics := eventd.NewTestMetrics(rec)

	store := openStore(t, "metrics-failed.db")

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	peers := []eventd.PeerConfig{{NodeName: "bad-peer", Endpoint: failSrv.URL}}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		failSrv.Client(), clock, func(time.Duration) {})
	pusher.SetMetrics(metrics)

	seedEventFrom(t, store, "evt-fail", "cloudedge", "onprem", "10.88.60.10/32", now)
	outbox := eventd.NewOutbox(store, store, pusher, "cloudedge", "onprem", time.Second, clock)
	outbox.SetMetrics(metrics)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := rec.Count("routerd_eventd_outbox_delivery_total", "status", "failed"); got != 1 {
		t.Fatalf("delivery_total{status=failed} = %d, want 1", got)
	}
	if got := rec.Count("routerd_eventd_outbox_delivery_attempts_total", "status", "failed"); got != 3 {
		t.Fatalf("attempts_total{status=failed} = %d, want 3", got)
	}

	if label, found := rec.HasForbiddenLabel(); found {
		t.Fatalf("forbidden label %q found in metrics", label)
	}
}

func TestMetricsRetryAttemptsCounted(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	rec := &eventd.TestRecorder{}
	metrics := eventd.NewTestMetrics(rec)

	store := openStore(t, "metrics-retry.db")

	var attempt int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt++
		if attempt < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: "retrying-peer", Endpoint: srv.URL}}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, func(time.Duration) {})
	pusher.SetMetrics(metrics)

	seedEventFrom(t, store, "evt-retry", "cloudedge", "onprem", "10.88.60.11/32", now)
	outbox := eventd.NewOutbox(store, store, pusher, "cloudedge", "onprem", time.Second, clock)
	outbox.SetMetrics(metrics)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := rec.Count("routerd_eventd_outbox_delivery_total", "status", "delivered"); got != 1 {
		t.Fatalf("delivery_total{status=delivered} = %d, want 1", got)
	}
	if got := rec.Count("routerd_eventd_outbox_delivery_attempts_total", "status", "delivered"); got != 3 {
		t.Fatalf("attempts_total{status=delivered} = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestMetricsStaleTTLAndRepush(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	rec := &eventd.TestRecorder{}
	metrics := eventd.NewTestMetrics(rec)

	store := openStore(t, "metrics-stale.db")

	receiver := eventd.NewReceiver(store, testSecret, "cloudedge", "leaf", 5*time.Minute, clock)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: "leaf-a", Endpoint: srv.URL}}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, func(time.Duration) {})
	pusher.SetMetrics(metrics)

	expires1 := now.Add(10 * time.Minute)
	seedEventExpiring(t, store, "shard-1", "cloudedge", "rr-node", "10.77.60.0/25", now, expires1)

	outbox := eventd.NewOutbox(store, store, pusher, "cloudedge", "rr-node", time.Second, clock)
	outbox.SetMetrics(metrics)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #1: %v", err)
	}

	if got := rec.Count("routerd_eventd_outbox_stale_ttl_delivery_total"); got != 0 {
		t.Fatalf("stale_ttl after run #1 = %d, want 0", got)
	}

	// Refresh TTL
	expires2 := now.Add(20 * time.Minute)
	if err := store.RecordFederationEvent(routerstate.EventRecord{
		ID: "shard-1", Group: "cloudedge", SourceNode: "rr-node",
		Type: "routerd.client.ipv4.observed", Subject: "10.77.60.0/25",
		DedupeKey: "10.77.60.0/25", Payload: map[string]string{"mac": "aa:bb:cc:dd:ee:ff"},
		ObservedAt: now, ExpiresAt: expires2,
	}); err != nil {
		t.Fatalf("re-record: %v", err)
	}

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #2: %v", err)
	}

	if got := rec.Count("routerd_eventd_outbox_stale_ttl_delivery_total"); got != 1 {
		t.Fatalf("stale_ttl after TTL refresh = %d, want 1", got)
	}
	if got := rec.Count("routerd_eventd_outbox_repush_total"); got != 1 {
		t.Fatalf("repush after TTL refresh = %d, want 1", got)
	}

	if label, found := rec.HasForbiddenLabel(); found {
		t.Fatalf("forbidden label %q found in metrics", label)
	}
}

func TestMetricsReceiverReject(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	rec := &eventd.TestRecorder{}
	metrics := eventd.NewTestMetrics(rec)

	store := openStore(t, "metrics-receiver.db")
	receiver := eventd.NewReceiver(store, testSecret, "cloudedge", "cloud", 5*time.Minute, clock)
	receiver.SetMetrics(metrics)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	// Bad timestamp — use a request with non-numeric header
	{
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/events", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(eventd.HeaderTimestamp, "not-a-number")
		req.Header.Set(eventd.HeaderSignature, "ignored")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("bad timestamp request: %v", err)
		}
		resp.Body.Close()
	}
	if got := rec.Count("routerd_eventd_receiver_reject_total", "reason", "bad_timestamp"); got != 1 {
		t.Fatalf("reject{bad_timestamp} = %d, want 1", got)
	}

	// Bad signature
	postRaw(t, srv.URL+"/v1/events", now.Unix(), "deadbeef", []byte("{}"))
	if got := rec.Count("routerd_eventd_receiver_reject_total", "reason", "bad_signature"); got != 1 {
		t.Fatalf("reject{bad_signature} = %d, want 1", got)
	}

	// Stale timestamp
	staleTS := now.Add(-1 * time.Hour).Unix()
	staleSig := signEvent(staleTS, []byte("{}"))
	postRaw(t, srv.URL+"/v1/events", staleTS, staleSig, []byte("{}"))
	if got := rec.Count("routerd_eventd_receiver_reject_total", "reason", "stale_timestamp"); got != 1 {
		t.Fatalf("reject{stale_timestamp} = %d, want 1", got)
	}

	// Bad body (valid signature, invalid JSON event - missing required fields)
	body := []byte(`{"id":"","group":"","type":""}`)
	ts := now.Unix()
	sig := signEvent(ts, body)
	postRaw(t, srv.URL+"/v1/events", ts, sig, body)
	if got := rec.Count("routerd_eventd_receiver_reject_total", "reason", "validation"); got != 1 {
		t.Fatalf("reject{validation} = %d, want 1", got)
	}

	if label, found := rec.HasForbiddenLabel(); found {
		t.Fatalf("forbidden label %q found in metrics", label)
	}
}

func TestMetricsNoForbiddenLabels(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	rec := &eventd.TestRecorder{}
	metrics := eventd.NewTestMetrics(rec)

	senderStore := openStore(t, "metrics-labels.db")
	receiverStore := openStore(t, "metrics-labels-recv.db")

	receiver := eventd.NewReceiver(receiverStore, testSecret, "cloudedge", "cloud", 5*time.Minute, clock)
	receiver.SetMetrics(metrics)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: "cloud-a", Endpoint: srv.URL}}
	pusher := eventd.NewPusher(senderStore, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, func(time.Duration) {})
	pusher.SetMetrics(metrics)

	seedEventFrom(t, senderStore, "evt-labels", "cloudedge", "onprem", "10.88.60.100/32", now)
	outbox := eventd.NewOutbox(senderStore, senderStore, pusher, "cloudedge", "onprem", time.Second, clock)
	outbox.SetMetrics(metrics)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// Also trigger receiver-side rejection
	postRaw(t, srv.URL+"/v1/events", now.Unix(), "badbadbad", []byte("{}"))

	if label, found := rec.HasForbiddenLabel(); found {
		t.Fatalf("forbidden label %q found in metrics records", label)
	}

	// Verify only allowed labels present
	allowed := map[string]bool{"group": true, "peer": true, "event_type": true, "status": true, "reason": true}
	for _, record := range rec.Records {
		for key := range record.Attrs {
			if !allowed[key] {
				t.Errorf("unexpected label %q in metric %q", key, record.Name)
			}
		}
	}
}

func TestMetricsOutboxTickRecorded(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	rec := &eventd.TestRecorder{}
	metrics := eventd.NewTestMetrics(rec)

	store := openStore(t, "metrics-outbox-tick.db")
	peers := []eventd.PeerConfig{}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 1, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		nil, clock, func(time.Duration) {})
	pusher.SetMetrics(metrics)

	outbox := eventd.NewOutbox(store, store, pusher, "cloudedge", "onprem", time.Second, clock)
	outbox.SetMetrics(metrics)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := rec.Count("routerd_eventd_outbox_tick_total"); got != 1 {
		t.Fatalf("outbox_tick_total = %d, want 1", got)
	}
	durations := rec.Values("routerd_eventd_outbox_tick_duration_seconds")
	if len(durations) != 1 {
		t.Fatalf("outbox_tick_duration records = %d, want 1", len(durations))
	}
	if durations[0] < 0 {
		t.Fatalf("outbox_tick_duration = %f, want >= 0", durations[0])
	}
	if got := rec.Count("routerd_eventd_outbox_tick_errors_total"); got != 0 {
		t.Fatalf("outbox_tick_errors = %d, want 0", got)
	}
}

func TestMetricsPrunerTickRecorded(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	clock := fixedClock(now)
	rec := &eventd.TestRecorder{}
	metrics := eventd.NewTestMetrics(rec)

	store := openStore(t, "metrics-pruner-tick.db")
	seedEventFrom(t, store, "old-1", "cloudedge", "onprem", "10.88.60.1/32",
		now.Add(-48*time.Hour))

	pruner := eventd.NewPruner(store, "cloudedge",
		eventd.Retention{MaxAge: 24 * time.Hour, MaxEvents: 100}, time.Minute, clock)
	pruner.SetMetrics(metrics)

	pruned, err := pruner.PruneOnce(context.Background())
	if err != nil {
		t.Fatalf("PruneOnce: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}

	if got := rec.Count("routerd_eventd_pruner_tick_total"); got != 1 {
		t.Fatalf("pruner_tick_total = %d, want 1", got)
	}
	if got := rec.Count("routerd_eventd_pruner_pruned_total"); got != 1 {
		t.Fatalf("pruner_pruned_total = %d, want 1", got)
	}
	if got := rec.Count("routerd_eventd_pruner_tick_errors_total"); got != 0 {
		t.Fatalf("pruner_tick_errors = %d, want 0", got)
	}
}

func signEvent(ts int64, body []byte) string {
	return signWithSecret(testSecret, ts, body)
}
