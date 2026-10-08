// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/logstore"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// consoleMetricInstruments holds the gauges recordConsoleMetrics writes to.
// Built once per process via getConsoleMetrics(); reusing the same instrument
// objects on every /api/v1/summary call keeps the OTel SDK from accumulating
// duplicate-instrument metadata on each request (which we saw as steady heap
// growth tied to summary polling).
type consoleMetricInstruments struct {
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
	consoleMetrics     consoleMetricInstruments
)

func getConsoleMetrics() consoleMetricInstruments {
	consoleMetricsOnce.Do(func() {
		meter := otel.Meter("routerd")
		consoleMetrics.dryRunGauge, _ = meter.Int64Gauge("routerd.controller.dry_run.count")
		consoleMetrics.controllerErrorGauge, _ = meter.Int64Gauge("routerd.controller.reconcile.errors")
		consoleMetrics.controllerLastDurationGauge, _ = meter.Float64Gauge("routerd.controller.reconcile.last_duration_ms")
		consoleMetrics.phaseGauge, _ = meter.Int64Gauge("routerd.resource.phase.count")
		consoleMetrics.leaseGauge, _ = meter.Int64Gauge("routerd.dhcp.lease.active")
		consoleMetrics.stickyGauge, _ = meter.Int64Gauge("routerd.dhcp.sticky.held")
		consoleMetrics.clientGauge, _ = meter.Int64Gauge("routerd.client.active.count")
	})
	return consoleMetrics
}

func recordConsoleMetrics(ctx context.Context, resources []routerstate.ObjectStatus, controllers []controlapi.ControllerStatus, leases []DHCPLease, clients []ClientEntry, sticky []logstore.DHCPStickyLease, now time.Time) {
	m := getConsoleMetrics()
	dryRunGauge := m.dryRunGauge
	controllerErrorGauge := m.controllerErrorGauge
	controllerLastDurationGauge := m.controllerLastDurationGauge
	phaseGauge := m.phaseGauge
	leaseGauge := m.leaseGauge
	stickyGauge := m.stickyGauge
	clientGauge := m.clientGauge
	var dryRun int64
	for _, controller := range controllers {
		if strings.EqualFold(strings.TrimSpace(controller.Mode), "dry-run") {
			dryRun++
		}
		attrs := metric.WithAttributes(attribute.String("routerd.controller.name", controller.Name))
		controllerErrorGauge.Record(ctx, controller.ReconcileErrorCount, attrs)
		if controller.LastDurationMillis > 0 {
			controllerLastDurationGauge.Record(ctx, controller.LastDurationMillis, attrs)
		}
	}
	dryRunGauge.Record(ctx, dryRun)
	phaseCounts := map[string]int64{}
	for _, resource := range resources {
		phase := "Unknown"
		if resource.Status != nil {
			if value := strings.TrimSpace(fmt.Sprint(resource.Status["phase"])); value != "" && value != "<nil>" {
				phase = value
			}
		}
		phaseCounts[phase]++
	}
	for phase, count := range phaseCounts {
		phaseGauge.Record(ctx, count, metric.WithAttributes(attribute.String("routerd.resource.phase", phase)))
	}
	activeLeases := map[string]int64{}
	for _, lease := range leases {
		if lease.Source == "sticky-history" {
			continue
		}
		family := strings.ToLower(strings.TrimSpace(lease.Family))
		if family == "" {
			family = "ipv4"
			if strings.Contains(lease.IP, ":") {
				family = "ipv6"
			}
		}
		activeLeases[family]++
	}
	for family, count := range activeLeases {
		leaseGauge.Record(ctx, count, metric.WithAttributes(attribute.String("network.address.family", family)))
	}
	stickyHeld := map[string]int64{}
	for _, lease := range sticky {
		if lease.StickyUntil.IsZero() || !lease.StickyUntil.After(now) {
			continue
		}
		family := strings.ToLower(strings.TrimSpace(lease.Family))
		if family == "" {
			family = "ipv4"
			if strings.Contains(lease.IP, ":") {
				family = "ipv6"
			}
		}
		stickyHeld[family]++
	}
	for family, count := range stickyHeld {
		stickyGauge.Record(ctx, count, metric.WithAttributes(attribute.String("network.address.family", family)))
	}
	if clients != nil {
		clientGauge.Record(ctx, int64(len(clients)))
	}
}
