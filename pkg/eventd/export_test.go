// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

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
