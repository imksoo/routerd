// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

var (
	attrGroup     = attribute.Key("group")
	attrPeer      = attribute.Key("peer")
	attrEventType = attribute.Key("event_type")
	attrStatus    = attribute.Key("status")
	attrReason    = attribute.Key("reason")
)

type Metrics struct {
	deliveryTotal     otelmetric.Int64Counter
	attemptsTotal     otelmetric.Int64Counter
	repushTotal       otelmetric.Int64Counter
	staleTTLTotal     otelmetric.Int64Counter
	deliveryLag       otelmetric.Float64Histogram
	receiverReject    otelmetric.Int64Counter
	receiverAccepted  otelmetric.Int64Counter
	receiverDuplicate otelmetric.Int64Counter

	outboxTickTotal      otelmetric.Int64Counter
	outboxTickErrors     otelmetric.Int64Counter
	outboxTickDuration   otelmetric.Float64Histogram
	prunerTickTotal      otelmetric.Int64Counter
	prunerPrunedTotal    otelmetric.Int64Counter
	prunerTickErrors     otelmetric.Int64Counter
}

func NewMetrics(meter otelmetric.Meter) *Metrics {
	m := &Metrics{}
	m.deliveryTotal, _ = meter.Int64Counter("routerd_eventd_outbox_delivery_total",
		otelmetric.WithDescription("Total outbox delivery results by status"))
	m.attemptsTotal, _ = meter.Int64Counter("routerd_eventd_outbox_delivery_attempts_total",
		otelmetric.WithDescription("Total outbox delivery attempts"))
	m.repushTotal, _ = meter.Int64Counter("routerd_eventd_outbox_repush_total",
		otelmetric.WithDescription("Events re-pushed due to TTL refresh"))
	m.staleTTLTotal, _ = meter.Int64Counter("routerd_eventd_outbox_stale_ttl_delivery_total",
		otelmetric.WithDescription("Deliveries detected with stale TTL before re-push"))
	m.deliveryLag, _ = meter.Float64Histogram("routerd_eventd_outbox_delivery_lag_seconds",
		otelmetric.WithDescription("Time between event observation and delivery"))
	m.receiverReject, _ = meter.Int64Counter("routerd_eventd_receiver_reject_total",
		otelmetric.WithDescription("Events rejected by the receiver"))
	m.receiverAccepted, _ = meter.Int64Counter("routerd_eventd_receiver_accepted_total",
		otelmetric.WithDescription("Events accepted by the receiver"))
	m.receiverDuplicate, _ = meter.Int64Counter("routerd_eventd_receiver_duplicate_total",
		otelmetric.WithDescription("Duplicate events received"))
	m.outboxTickTotal, _ = meter.Int64Counter("routerd_eventd_outbox_tick_total",
		otelmetric.WithDescription("Outbox loop iterations"))
	m.outboxTickErrors, _ = meter.Int64Counter("routerd_eventd_outbox_tick_errors_total",
		otelmetric.WithDescription("Outbox loop iteration errors"))
	m.outboxTickDuration, _ = meter.Float64Histogram("routerd_eventd_outbox_tick_duration_seconds",
		otelmetric.WithDescription("Time per outbox loop iteration"))
	m.prunerTickTotal, _ = meter.Int64Counter("routerd_eventd_pruner_tick_total",
		otelmetric.WithDescription("Pruner loop iterations"))
	m.prunerPrunedTotal, _ = meter.Int64Counter("routerd_eventd_pruner_pruned_total",
		otelmetric.WithDescription("Events pruned by retention"))
	m.prunerTickErrors, _ = meter.Int64Counter("routerd_eventd_pruner_tick_errors_total",
		otelmetric.WithDescription("Pruner loop iteration errors"))
	return m
}

func (m *Metrics) RecordDelivery(ctx context.Context, group, peer, eventType, status string) {
	if m == nil {
		return
	}
	attrs := otelmetric.WithAttributes(
		attrGroup.String(group),
		attrPeer.String(peer),
		attrEventType.String(eventType),
		attrStatus.String(status),
	)
	m.deliveryTotal.Add(ctx, 1, attrs)
}

func (m *Metrics) RecordAttempts(ctx context.Context, group, peer, eventType, status string, count int64) {
	if m == nil {
		return
	}
	attrs := otelmetric.WithAttributes(
		attrGroup.String(group),
		attrPeer.String(peer),
		attrEventType.String(eventType),
		attrStatus.String(status),
	)
	m.attemptsTotal.Add(ctx, count, attrs)
}

func (m *Metrics) RecordRepush(ctx context.Context, group, peer, eventType string) {
	if m == nil {
		return
	}
	attrs := otelmetric.WithAttributes(
		attrGroup.String(group),
		attrPeer.String(peer),
		attrEventType.String(eventType),
	)
	m.repushTotal.Add(ctx, 1, attrs)
}

func (m *Metrics) RecordStaleTTL(ctx context.Context, group, peer, eventType string) {
	if m == nil {
		return
	}
	attrs := otelmetric.WithAttributes(
		attrGroup.String(group),
		attrPeer.String(peer),
		attrEventType.String(eventType),
	)
	m.staleTTLTotal.Add(ctx, 1, attrs)
}

func (m *Metrics) RecordDeliveryLag(ctx context.Context, group, peer, eventType string, lagSeconds float64) {
	if m == nil {
		return
	}
	attrs := otelmetric.WithAttributes(
		attrGroup.String(group),
		attrPeer.String(peer),
		attrEventType.String(eventType),
	)
	m.deliveryLag.Record(ctx, lagSeconds, attrs)
}

func (m *Metrics) RecordReceiverReject(ctx context.Context, group, reason string) {
	if m == nil {
		return
	}
	attrs := otelmetric.WithAttributes(
		attrGroup.String(group),
		attrReason.String(reason),
	)
	m.receiverReject.Add(ctx, 1, attrs)
}

func (m *Metrics) RecordReceiverAccepted(ctx context.Context, group string) {
	if m == nil {
		return
	}
	m.receiverAccepted.Add(ctx, 1, otelmetric.WithAttributes(attrGroup.String(group)))
}

func (m *Metrics) RecordReceiverDuplicate(ctx context.Context, group string) {
	if m == nil {
		return
	}
	m.receiverDuplicate.Add(ctx, 1, otelmetric.WithAttributes(attrGroup.String(group)))
}

func (m *Metrics) RecordOutboxTick(ctx context.Context, group string) {
	if m == nil {
		return
	}
	m.outboxTickTotal.Add(ctx, 1, otelmetric.WithAttributes(attrGroup.String(group)))
}

func (m *Metrics) RecordOutboxTickError(ctx context.Context, group string) {
	if m == nil {
		return
	}
	m.outboxTickErrors.Add(ctx, 1, otelmetric.WithAttributes(attrGroup.String(group)))
}

func (m *Metrics) RecordOutboxTickDuration(ctx context.Context, group string, seconds float64) {
	if m == nil {
		return
	}
	m.outboxTickDuration.Record(ctx, seconds, otelmetric.WithAttributes(attrGroup.String(group)))
}

func (m *Metrics) RecordPrunerTick(ctx context.Context, group string) {
	if m == nil {
		return
	}
	m.prunerTickTotal.Add(ctx, 1, otelmetric.WithAttributes(attrGroup.String(group)))
}

func (m *Metrics) RecordPrunerPruned(ctx context.Context, group string, count int64) {
	if m == nil {
		return
	}
	m.prunerPrunedTotal.Add(ctx, count, otelmetric.WithAttributes(attrGroup.String(group)))
}

func (m *Metrics) RecordPrunerTickError(ctx context.Context, group string) {
	if m == nil {
		return
	}
	m.prunerTickErrors.Add(ctx, 1, otelmetric.WithAttributes(attrGroup.String(group)))
}

// TestRecorder captures metric events for testing without an OTel SDK.
type TestRecorder struct {
	mu      sync.Mutex
	Records []TestMetricRecord
}

type TestMetricRecord struct {
	Name  string
	Value float64
	Attrs map[string]string
}

func (r *TestRecorder) record(name string, value float64, attrs map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Records = append(r.Records, TestMetricRecord{Name: name, Value: value, Attrs: attrs})
}

func (r *TestRecorder) Count(name string, filters ...string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int64
	for _, rec := range r.Records {
		if rec.Name != name {
			continue
		}
		if len(filters) > 0 && !matchFilters(rec.Attrs, filters) {
			continue
		}
		total += int64(rec.Value)
	}
	return total
}

func (r *TestRecorder) Values(name string, filters ...string) []float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	var vals []float64
	for _, rec := range r.Records {
		if rec.Name != name {
			continue
		}
		if len(filters) > 0 && !matchFilters(rec.Attrs, filters) {
			continue
		}
		vals = append(vals, rec.Value)
	}
	return vals
}

func matchFilters(attrs map[string]string, filters []string) bool {
	for i := 0; i+1 < len(filters); i += 2 {
		if attrs[filters[i]] != filters[i+1] {
			return false
		}
	}
	return true
}

func (r *TestRecorder) HasForbiddenLabel() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	forbidden := map[string]bool{
		"event_id":   true,
		"subject":    true,
		"address":    true,
		"dedupe_key": true,
		"endpoint":   true,
		"error":      true,
	}
	for _, rec := range r.Records {
		for key := range rec.Attrs {
			if forbidden[key] {
				return key, true
			}
		}
	}
	return "", false
}

// NewTestMetrics builds a Metrics that records to a TestRecorder.
func NewTestMetrics(recorder *TestRecorder) *Metrics {
	m := &Metrics{}
	m.deliveryTotal = &testCounter{name: "routerd_eventd_outbox_delivery_total", rec: recorder}
	m.attemptsTotal = &testCounter{name: "routerd_eventd_outbox_delivery_attempts_total", rec: recorder}
	m.repushTotal = &testCounter{name: "routerd_eventd_outbox_repush_total", rec: recorder}
	m.staleTTLTotal = &testCounter{name: "routerd_eventd_outbox_stale_ttl_delivery_total", rec: recorder}
	m.deliveryLag = &testHistogram{name: "routerd_eventd_outbox_delivery_lag_seconds", rec: recorder}
	m.receiverReject = &testCounter{name: "routerd_eventd_receiver_reject_total", rec: recorder}
	m.receiverAccepted = &testCounter{name: "routerd_eventd_receiver_accepted_total", rec: recorder}
	m.receiverDuplicate = &testCounter{name: "routerd_eventd_receiver_duplicate_total", rec: recorder}
	m.outboxTickTotal = &testCounter{name: "routerd_eventd_outbox_tick_total", rec: recorder}
	m.outboxTickErrors = &testCounter{name: "routerd_eventd_outbox_tick_errors_total", rec: recorder}
	m.outboxTickDuration = &testHistogram{name: "routerd_eventd_outbox_tick_duration_seconds", rec: recorder}
	m.prunerTickTotal = &testCounter{name: "routerd_eventd_pruner_tick_total", rec: recorder}
	m.prunerPrunedTotal = &testCounter{name: "routerd_eventd_pruner_pruned_total", rec: recorder}
	m.prunerTickErrors = &testCounter{name: "routerd_eventd_pruner_tick_errors_total", rec: recorder}
	return m
}

type testCounter struct {
	otelmetric.Int64Counter
	name string
	rec  *TestRecorder
}

func (c *testCounter) Add(_ context.Context, value int64, opts ...otelmetric.AddOption) {
	cfg := otelmetric.NewAddConfig(opts)
	attrs := extractAttrs(cfg.Attributes())
	c.rec.record(c.name, float64(value), attrs)
}

type testHistogram struct {
	otelmetric.Float64Histogram
	name string
	rec  *TestRecorder
}

func (h *testHistogram) Record(_ context.Context, value float64, opts ...otelmetric.RecordOption) {
	cfg := otelmetric.NewRecordConfig(opts)
	attrs := extractAttrs(cfg.Attributes())
	h.rec.record(h.name, value, attrs)
}

func extractAttrs(set attribute.Set) map[string]string {
	m := make(map[string]string, set.Len())
	iter := set.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

// DeliveryEvent carries per-delivery metadata for metrics recording.
// Pusher populates this after each delivery attempt.
type DeliveryEvent struct {
	Group      string
	Peer       string
	EventType  string
	Status     string
	Attempts   int
	ObservedAt time.Time
	DeliveredAt time.Time
}
