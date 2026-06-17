// SPDX-License-Identifier: BSD-3-Clause

package eventd_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var all []metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		all = append(all, sm.Metrics...)
	}
	return all
}

func findMetric(metrics []metricdata.Metrics, name string) *metricdata.Metrics {
	for i := range metrics {
		if metrics[i].Name == name {
			return &metrics[i]
		}
	}
	return nil
}

func sumCounterValue(m *metricdata.Metrics, filters map[string]string) int64 {
	if m == nil {
		return 0
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	if !ok {
		return 0
	}
	var total int64
	for _, dp := range sum.DataPoints {
		if matchAttrs(dp.Attributes, filters) {
			total += dp.Value
		}
	}
	return total
}

func histogramCount(m *metricdata.Metrics, filters map[string]string) uint64 {
	if m == nil {
		return 0
	}
	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		return 0
	}
	var total uint64
	for _, dp := range hist.DataPoints {
		if matchAttrs(dp.Attributes, filters) {
			total += dp.Count
		}
	}
	return total
}

func matchAttrs(set attribute.Set, filters map[string]string) bool {
	for k, v := range filters {
		val, ok := set.Value(attribute.Key(k))
		if !ok || val.AsString() != v {
			return false
		}
	}
	return true
}

func hasAttrKey(metrics []metricdata.Metrics, key string) bool {
	for _, m := range metrics {
		switch d := m.Data.(type) {
		case metricdata.Sum[int64]:
			for _, dp := range d.DataPoints {
				if _, ok := dp.Attributes.Value(attribute.Key(key)); ok {
					return true
				}
			}
		case metricdata.Histogram[float64]:
			for _, dp := range d.DataPoints {
				if _, ok := dp.Attributes.Value(attribute.Key(key)); ok {
					return true
				}
			}
		}
	}
	return false
}

// TestOTelMetricsDeliveryEndToEnd exercises the full OTel SDK pipeline:
// ManualReader → MeterProvider → NewMetrics → Pusher/Outbox → collect.
func TestOTelMetricsDeliveryEndToEnd(t *testing.T) {
	now := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)
	clock := fixedClock(now)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer provider.Shutdown(context.Background())
	meter := provider.Meter("routerd-eventd-test")
	metrics := eventd.NewMetrics(meter)

	senderStore := openStore(t, "otel-e2e-sender.db")
	receiverStore := openStore(t, "otel-e2e-receiver.db")

	receiver := eventd.NewReceiver(receiverStore, testSecret, "cloudedge", "cloud", 5*time.Minute, clock)
	receiver.SetMetrics(metrics)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: "cloud-a", Endpoint: srv.URL}}
	pusher := eventd.NewPusher(senderStore, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, func(time.Duration) {})
	pusher.SetMetrics(metrics)

	seedEventFrom(t, senderStore, "otel-evt-1", "cloudedge", "onprem", "10.88.60.9/32", now)

	outbox := eventd.NewOutbox(senderStore, senderStore, pusher, "cloudedge", "onprem", time.Second, clock)
	outbox.SetMetrics(metrics)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	collected := collectMetrics(t, reader)

	// delivery_total{status=delivered} = 1
	m := findMetric(collected, "routerd_eventd_outbox_delivery_total")
	if m == nil {
		t.Fatal("metric routerd_eventd_outbox_delivery_total not found")
	}
	if got := sumCounterValue(m, map[string]string{"status": "delivered", "group": "cloudedge", "peer": "cloud-a"}); got != 1 {
		t.Fatalf("delivery_total{status=delivered} = %d, want 1", got)
	}

	// attempts_total >= 1
	m = findMetric(collected, "routerd_eventd_outbox_delivery_attempts_total")
	if m == nil {
		t.Fatal("metric routerd_eventd_outbox_delivery_attempts_total not found")
	}
	if got := sumCounterValue(m, map[string]string{"status": "delivered"}); got < 1 {
		t.Fatalf("attempts_total{status=delivered} = %d, want >= 1", got)
	}

	// delivery_lag_seconds recorded at least once
	m = findMetric(collected, "routerd_eventd_outbox_delivery_lag_seconds")
	if m == nil {
		t.Fatal("metric routerd_eventd_outbox_delivery_lag_seconds not found")
	}
	if got := histogramCount(m, map[string]string{"group": "cloudedge"}); got != 1 {
		t.Fatalf("delivery_lag_seconds count = %d, want 1", got)
	}

	// receiver_accepted_total = 1
	m = findMetric(collected, "routerd_eventd_receiver_accepted_total")
	if m == nil {
		t.Fatal("metric routerd_eventd_receiver_accepted_total not found")
	}
	if got := sumCounterValue(m, map[string]string{"group": "cloudedge"}); got != 1 {
		t.Fatalf("receiver_accepted_total = %d, want 1", got)
	}

	// No forbidden labels
	forbidden := []string{"event_id", "subject", "address", "dedupe_key", "endpoint", "error"}
	for _, key := range forbidden {
		if hasAttrKey(collected, key) {
			t.Fatalf("forbidden attribute %q found in OTel metrics", key)
		}
	}
}

// TestOTelMetricsReceiverReject exercises receiver rejection paths through the
// real OTel SDK pipeline.
func TestOTelMetricsReceiverReject(t *testing.T) {
	now := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)
	clock := fixedClock(now)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer provider.Shutdown(context.Background())
	meter := provider.Meter("routerd-eventd-test")
	metrics := eventd.NewMetrics(meter)

	store := openStore(t, "otel-reject.db")
	receiver := eventd.NewReceiver(store, testSecret, "cloudedge", "cloud", 5*time.Minute, clock)
	receiver.SetMetrics(metrics)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	// Bad signature
	postRaw(t, srv.URL+"/v1/events", now.Unix(), "deadbeef", []byte("{}"))

	// Stale timestamp
	staleTS := now.Add(-1 * time.Hour).Unix()
	staleSig := federation.Sign(testSecret, staleTS, []byte("{}"))
	postRaw(t, srv.URL+"/v1/events", staleTS, staleSig, []byte("{}"))

	collected := collectMetrics(t, reader)

	m := findMetric(collected, "routerd_eventd_receiver_reject_total")
	if m == nil {
		t.Fatal("metric routerd_eventd_receiver_reject_total not found")
	}

	if got := sumCounterValue(m, map[string]string{"reason": "bad_signature"}); got != 1 {
		t.Fatalf("reject{bad_signature} = %d, want 1", got)
	}
	if got := sumCounterValue(m, map[string]string{"reason": "stale_timestamp"}); got != 1 {
		t.Fatalf("reject{stale_timestamp} = %d, want 1", got)
	}
}

// TestOTelMetricsStaleTTLRepush exercises the stale TTL / repush metrics
// through the full OTel SDK pipeline.
func TestOTelMetricsStaleTTLRepush(t *testing.T) {
	now := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)
	clock := fixedClock(now)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer provider.Shutdown(context.Background())
	meter := provider.Meter("routerd-eventd-test")
	metrics := eventd.NewMetrics(meter)

	store := openStore(t, "otel-stale.db")

	receiver := eventd.NewReceiver(store, testSecret, "cloudedge", "leaf", 5*time.Minute, clock)
	srv := httptest.NewServer(receiver.Handler())
	defer srv.Close()

	peers := []eventd.PeerConfig{{NodeName: "leaf-a", Endpoint: srv.URL}}
	pusher := eventd.NewPusher(store, testSecret, peers,
		eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
		srv.Client(), clock, func(time.Duration) {})
	pusher.SetMetrics(metrics)

	expires1 := now.Add(10 * time.Minute)
	seedEventExpiring(t, store, "stale-1", "cloudedge", "rr-node", "10.77.60.0/25", now, expires1)

	outbox := eventd.NewOutbox(store, store, pusher, "cloudedge", "rr-node", time.Second, clock)
	outbox.SetMetrics(metrics)

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #1: %v", err)
	}

	// Refresh TTL
	expires2 := now.Add(20 * time.Minute)
	if err := store.RecordFederationEvent(routerstate.EventRecord{
		ID: "stale-1", Group: "cloudedge", SourceNode: "rr-node",
		Type: "routerd.client.ipv4.observed", Subject: "10.77.60.0/25",
		DedupeKey: "10.77.60.0/25", Payload: map[string]string{"mac": "aa:bb:cc:dd:ee:ff"},
		ObservedAt: now, ExpiresAt: expires2,
	}); err != nil {
		t.Fatalf("re-record: %v", err)
	}

	if err := outbox.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce #2: %v", err)
	}

	collected := collectMetrics(t, reader)

	m := findMetric(collected, "routerd_eventd_outbox_stale_ttl_delivery_total")
	if m == nil {
		t.Fatal("metric routerd_eventd_outbox_stale_ttl_delivery_total not found")
	}
	if got := sumCounterValue(m, map[string]string{"group": "cloudedge", "peer": "leaf-a"}); got != 1 {
		t.Fatalf("stale_ttl_total = %d, want 1", got)
	}

	m = findMetric(collected, "routerd_eventd_outbox_repush_total")
	if m == nil {
		t.Fatal("metric routerd_eventd_outbox_repush_total not found")
	}
	if got := sumCounterValue(m, map[string]string{"group": "cloudedge", "peer": "leaf-a"}); got != 1 {
		t.Fatalf("repush_total = %d, want 1", got)
	}

	// Both delivery runs should show 2 total deliveries
	m = findMetric(collected, "routerd_eventd_outbox_delivery_total")
	if m == nil {
		t.Fatal("metric routerd_eventd_outbox_delivery_total not found")
	}
	if got := sumCounterValue(m, map[string]string{"status": "delivered"}); got != 2 {
		t.Fatalf("delivery_total{delivered} = %d, want 2 (initial + repush)", got)
	}
}
