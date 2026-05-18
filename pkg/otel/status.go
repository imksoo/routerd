// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"context"
	"fmt"
	"strings"

	"routerd/pkg/controlapi"
	"routerd/pkg/logstore"
	routerstate "routerd/pkg/state"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func RecordStatusMetrics(ctx context.Context, resources []routerstate.ObjectStatus, controllers []controlapi.ControllerStatus, leases []logstore.DHCPStickyLease) {
	meter := otel.Meter("routerd")
	dryRunGauge, _ := meter.Int64Gauge("routerd.controller.dry_run.count")
	controllerErrorGauge, _ := meter.Int64Gauge("routerd.controller.reconcile.errors")
	controllerLastDurationGauge, _ := meter.Float64Gauge("routerd.controller.reconcile.last_duration_ms")
	phaseGauge, _ := meter.Int64Gauge("routerd.resource.phase.count")
	dhcpActiveGauge, _ := meter.Int64Gauge("routerd.dhcp.lease.active")
	dhcpStickyGauge, _ := meter.Int64Gauge("routerd.dhcp.sticky.held")
	clientActiveGauge, _ := meter.Int64Gauge("routerd.client.active.count")
	bgpPeerGauge, _ := meter.Int64Gauge("routerd.bgp.peer.established")
	bgpPrefixGauge, _ := meter.Int64Gauge("routerd.bgp.prefix.accepted")
	vipActiveGauge, _ := meter.Int64Gauge("routerd.vip.active")
	ingressActiveGauge, _ := meter.Int64Gauge("routerd.ingress.service.active")
	ingressHealthyBackendGauge, _ := meter.Int64Gauge("routerd.ingress.backend.healthy")

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

	phases := map[string]int64{}
	clientAddresses := map[string]bool{}
	for _, resource := range resources {
		phase := "Unknown"
		if resource.Status != nil {
			if value := strings.TrimSpace(toString(resource.Status["phase"])); value != "" {
				phase = value
			}
			if resource.Kind == "DHCPv4Server" || resource.Kind == "DHCPv6Server" {
				if leasesValue, ok := resource.Status["activeLeases"].(int); ok {
					family := "ipv4"
					if resource.Kind == "DHCPv6Server" {
						family = "ipv6"
					}
					dhcpActiveGauge.Record(ctx, int64(leasesValue), metric.WithAttributes(attribute.String("network.address.family", family)))
				}
			}
			if resource.Kind == "BGPRouter" {
				if value, ok := statusInt64(resource.Status["establishedPeers"]); ok {
					bgpPeerGauge.Record(ctx, value, metric.WithAttributes(attribute.String("routerd.resource.name", resource.Name)))
				}
				if value, ok := statusInt64(resource.Status["acceptedPrefixes"]); ok {
					bgpPrefixGauge.Record(ctx, value, metric.WithAttributes(attribute.String("routerd.resource.name", resource.Name)))
				}
			}
			if resource.Kind == "VirtualIPv4Address" {
				active := int64(0)
				if strings.EqualFold(phase, "Applied") || strings.EqualFold(phase, "Active") || strings.EqualFold(phase, "Master") {
					active = 1
				}
				vipActiveGauge.Record(ctx, active, metric.WithAttributes(
					attribute.String("routerd.resource.name", resource.Name),
					attribute.String("network.local.address", toString(resource.Status["address"])),
				))
			}
			if resource.Kind == "IngressService" {
				active := int64(0)
				if strings.EqualFold(phase, "Active") || strings.EqualFold(phase, "Degraded") {
					active = 1
				}
				ingressActiveGauge.Record(ctx, active, metric.WithAttributes(attribute.String("routerd.resource.name", resource.Name)))
				if value, ok := statusInt64(resource.Status["healthyBackends"]); ok {
					ingressHealthyBackendGauge.Record(ctx, value, metric.WithAttributes(attribute.String("routerd.resource.name", resource.Name)))
				}
			}
		}
		phases[phase]++
	}
	for phase, count := range phases {
		phaseGauge.Record(ctx, count, metric.WithAttributes(attribute.String("routerd.resource.phase", phase)))
	}

	stickyByFamily := map[string]int64{}
	for _, lease := range leases {
		family := strings.ToLower(strings.TrimSpace(lease.Family))
		if family == "" {
			if strings.Contains(lease.IP, ":") {
				family = "ipv6"
			} else {
				family = "ipv4"
			}
		}
		stickyByFamily[family]++
		if lease.IP != "" {
			clientAddresses[lease.IP] = true
		}
	}
	for family, count := range stickyByFamily {
		dhcpStickyGauge.Record(ctx, count, metric.WithAttributes(attribute.String("network.address.family", family)))
	}
	clientActiveGauge.Record(ctx, int64(len(clientAddresses)))
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func statusInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	default:
		return 0, false
	}
}
