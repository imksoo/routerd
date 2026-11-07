// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// consoleMetricInstruments holds the console observation gauges.
// Built once per process via getConsoleMetrics(); reusing the same instrument
// objects on every /api/v1/summary call keeps the OTel SDK from accumulating
// duplicate-instrument metadata on each request (which we saw as steady heap
// growth tied to summary polling).
type consoleMetricInstruments struct {
	mu                          sync.Mutex
	phases                      map[string]int64
	dryRunGauge                 metric.Int64Gauge
	controllerErrorGauge        metric.Int64Gauge
	controllerLastDurationGauge metric.Float64Gauge
	phaseGauge                  metric.Int64Gauge
	leaseGauge                  metric.Int64Gauge
	stickyGauge                 metric.Int64Gauge
	clientGauge                 metric.Int64Gauge
}

var (
	consoleMetricsOnce sync.Once
	consoleMetrics     *consoleMetricInstruments
)

func newConsoleMetrics(meter metric.Meter) *consoleMetricInstruments {
	m := &consoleMetricInstruments{}
	m.dryRunGauge, _ = meter.Int64Gauge("routerd.controller.dry_run.count")
	m.controllerErrorGauge, _ = meter.Int64Gauge("routerd.controller.reconcile.errors")
	m.controllerLastDurationGauge, _ = meter.Float64Gauge("routerd.controller.reconcile.last_duration_ms")
	m.phaseGauge, _ = meter.Int64Gauge("routerd.resource.phase.count")
	m.leaseGauge, _ = meter.Int64Gauge("routerd.dhcp.lease.active")
	m.stickyGauge, _ = meter.Int64Gauge("routerd.dhcp.sticky.held")
	m.clientGauge, _ = meter.Int64Gauge("routerd.client.active.count")
	return m
}

func getConsoleMetrics() *consoleMetricInstruments {
	consoleMetricsOnce.Do(func() { consoleMetrics = newConsoleMetrics(otel.Meter("routerd")) })
	return consoleMetrics
}

func (m *consoleMetricInstruments) record(ctx context.Context, snapshot Snapshot, sticky []logstore.DHCPStickyLease, now time.Time) {
	// Serialize the previous-label update with records from concurrent requests.
	m.mu.Lock()
	defer m.mu.Unlock()
	var dryRun int64
	for _, controller := range snapshot.Controllers {
		if strings.EqualFold(strings.TrimSpace(controller.Mode), "dry-run") {
			dryRun++
		}
		attrs := metric.WithAttributes(attribute.String("routerd.controller.name", controller.Name))
		m.controllerErrorGauge.Record(ctx, controller.ReconcileErrorCount, attrs)
		if controller.LastDurationMillis > 0 {
			m.controllerLastDurationGauge.Record(ctx, controller.LastDurationMillis, attrs)
		}
	}
	m.dryRunGauge.Record(ctx, dryRun)
	if snapshot.collected(SectionResources) {
		phaseCounts := map[string]int64{}
		for _, resource := range snapshot.Resources {
			phase := "Unknown"
			if resource.Status != nil {
				if value := strings.TrimSpace(fmt.Sprint(resource.Status["phase"])); value != "" && value != "<nil>" {
					phase = value
				}
			}
			phaseCounts[phase]++
		}
		// Cumulative OTel gauges retain the last value of an absent label.
		for phase := range m.phases {
			if _, present := phaseCounts[phase]; !present {
				m.phaseGauge.Record(ctx, 0, metric.WithAttributes(attribute.String("routerd.resource.phase", phase)))
			}
		}
		for phase, count := range phaseCounts {
			m.phaseGauge.Record(ctx, count, metric.WithAttributes(attribute.String("routerd.resource.phase", phase)))
		}
		m.phases = phaseCounts
	}
	if snapshot.collected(SectionDHCPLeases) {
		activeLeases := map[string]int64{"ipv4": 0, "ipv6": 0}
		for _, lease := range snapshot.DHCPLeases {
			if lease.Source != "sticky-history" {
				activeLeases[metricAddressFamily(lease.Family, lease.IP)]++
			}
		}
		for family, count := range activeLeases {
			m.leaseGauge.Record(ctx, count, metric.WithAttributes(attribute.String("network.address.family", family)))
		}
		stickyHeld := map[string]int64{"ipv4": 0, "ipv6": 0}
		for _, lease := range sticky {
			if !lease.StickyUntil.IsZero() && lease.StickyUntil.After(now) {
				stickyHeld[metricAddressFamily(lease.Family, lease.IP)]++
			}
		}
		for family, count := range stickyHeld {
			m.stickyGauge.Record(ctx, count, metric.WithAttributes(attribute.String("network.address.family", family)))
		}
	}
	if snapshot.collected(SectionClients) {
		m.clientGauge.Record(ctx, int64(len(snapshot.Clients)))
	}
}

func metricAddressFamily(family, ip string) string {
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "ipv4":
		return "ipv4"
	case "ipv6":
		return "ipv6"
	default:
		if strings.Contains(ip, ":") {
			return "ipv6"
		}
		return "ipv4"
	}
}
