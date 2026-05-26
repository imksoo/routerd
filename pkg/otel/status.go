// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/logstore"
	routerstate "github.com/imksoo/routerd/pkg/state"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var statusMetricMemory = struct {
	sync.Mutex
	seen map[string]struct{}
	last map[string]int64
}{
	seen: map[string]struct{}{},
	last: map[string]int64{},
}

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
	bgpPeerTransitionCounter, _ := meter.Int64Counter("routerd.bgp.peer.state.transitions")
	bgpMessageReceivedCounter, _ := meter.Int64Counter("routerd.bgp.message.received_total")
	bgpMessageSentCounter, _ := meter.Int64Counter("routerd.bgp.message.sent_total")
	vipActiveGauge, _ := meter.Int64Gauge("routerd.vip.active")
	vrrpRoleTransitionCounter, _ := meter.Int64Counter("routerd.vrrp.role.transitions")
	vrrpMasterGauge, _ := meter.Int64Gauge("routerd.vrrp.master")
	ingressActiveGauge, _ := meter.Int64Gauge("routerd.ingress.service.active")
	ingressHealthyBackendGauge, _ := meter.Int64Gauge("routerd.ingress.backend.healthy")
	ingressHealthyBackendCountGauge, _ := meter.Int64Gauge("routerd.ingress.backend.healthy.count")
	ingressFailoverCounter, _ := meter.Int64Counter("routerd.ingress.failover.total")
	ingressBackendCheckCounter, _ := meter.Int64Counter("routerd.ingress.backend.health_check_total")

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
				for _, peer := range statusMaps(resource.Status["peers"]) {
					peerAddress := toString(peer["address"])
					state := defaultStatusString(toString(peer["state"]), "unknown")
					if messages, ok := statusInt64(peer["messagesReceived"]); ok {
						recordCounterDelta(ctx, "bgp.rx."+resource.Name+"."+peerAddress, messages, bgpMessageReceivedCounter,
							attribute.String("routerd.resource.name", resource.Name),
							attribute.String("peer", peerAddress),
						)
					}
					if messages, ok := statusInt64(peer["messagesSent"]); ok {
						recordCounterDelta(ctx, "bgp.tx."+resource.Name+"."+peerAddress, messages, bgpMessageSentCounter,
							attribute.String("routerd.resource.name", resource.Name),
							attribute.String("peer", peerAddress),
						)
					}
					if ts := strings.TrimSpace(toString(peer["lastEstablishedAt"])); strings.EqualFold(state, "Established") && ts != "" {
						recordOnce("bgp.peer.up."+resource.Name+"."+peerAddress+"."+ts, func() {
							bgpPeerTransitionCounter.Add(ctx, 1, metric.WithAttributes(
								attribute.String("routerd.resource.name", resource.Name),
								attribute.String("peer", peerAddress),
								attribute.String("from", "non-established"),
								attribute.String("to", "Established"),
							))
						})
					}
					if ts := strings.TrimSpace(toString(peer["lastErrorAt"])); !strings.EqualFold(state, "Established") && ts != "" {
						recordOnce("bgp.peer.down."+resource.Name+"."+peerAddress+"."+ts+"."+state, func() {
							bgpPeerTransitionCounter.Add(ctx, 1, metric.WithAttributes(
								attribute.String("routerd.resource.name", resource.Name),
								attribute.String("peer", peerAddress),
								attribute.String("from", "Established"),
								attribute.String("to", state),
							))
						})
					}
				}
			}
			if resource.Kind == "VirtualAddress" {
				active := int64(0)
				if strings.EqualFold(phase, "Applied") || strings.EqualFold(phase, "Active") || strings.EqualFold(phase, "Master") {
					active = 1
				}
				vipActiveGauge.Record(ctx, active, metric.WithAttributes(
					attribute.String("routerd.resource.name", resource.Name),
					attribute.String("network.local.address", toString(resource.Status["address"])),
				))
				role := defaultStatusString(strings.ToLower(toString(resource.Status["role"])), "unknown")
				master := int64(0)
				if role == "master" {
					master = 1
				}
				vrrpMasterGauge.Record(ctx, master, metric.WithAttributes(attribute.String("routerd.resource.name", resource.Name)))
				if ts := strings.TrimSpace(toString(resource.Status["lastRoleTransitionAt"])); ts != "" {
					recordOnce("vrrp.role."+resource.Name+"."+role+"."+ts, func() {
						vrrpRoleTransitionCounter.Add(ctx, 1, metric.WithAttributes(
							attribute.String("routerd.resource.name", resource.Name),
							attribute.String("role", role),
						))
					})
				}
			}
			if resource.Kind == "IngressService" {
				active := int64(0)
				if strings.EqualFold(phase, "Active") || strings.EqualFold(phase, "Degraded") {
					active = 1
				}
				ingressActiveGauge.Record(ctx, active, metric.WithAttributes(attribute.String("routerd.resource.name", resource.Name)))
				if value, ok := statusInt64(resource.Status["healthyBackends"]); ok {
					ingressHealthyBackendCountGauge.Record(ctx, value, metric.WithAttributes(attribute.String("routerd.resource.name", resource.Name)))
				}
				for _, backend := range statusMaps(resource.Status["backends"]) {
					backendName := toString(backend["name"])
					healthy := int64(0)
					if statusBool(backend["healthy"]) {
						healthy = 1
					}
					ingressHealthyBackendGauge.Record(ctx, healthy, metric.WithAttributes(
						attribute.String("routerd.resource.name", resource.Name),
						attribute.String("backend", backendName),
					))
					if ts := strings.TrimSpace(toString(backend["lastHealthyAt"])); healthy == 1 && ts != "" {
						recordOnce("ingress.check.healthy."+resource.Name+"."+backendName+"."+ts, func() {
							ingressBackendCheckCounter.Add(ctx, 1, metric.WithAttributes(
								attribute.String("routerd.resource.name", resource.Name),
								attribute.String("backend", backendName),
								attribute.String("outcome", "healthy"),
							))
						})
					}
					if ts := strings.TrimSpace(toString(backend["lastUnhealthyAt"])); healthy == 0 && ts != "" {
						recordOnce("ingress.check.unhealthy."+resource.Name+"."+backendName+"."+ts, func() {
							ingressBackendCheckCounter.Add(ctx, 1, metric.WithAttributes(
								attribute.String("routerd.resource.name", resource.Name),
								attribute.String("backend", backendName),
								attribute.String("outcome", "unhealthy"),
							))
						})
					}
				}
				if ts := strings.TrimSpace(toString(resource.Status["lastActiveBackendTransitionAt"])); ts != "" {
					current := statusMap(resource.Status["activeBackend"])
					previous := statusMap(resource.Status["previousActiveBackend"])
					from := defaultStatusString(toString(previous["name"]), "-")
					to := defaultStatusString(toString(current["name"]), "-")
					if from != to {
						recordOnce("ingress.failover."+resource.Name+"."+from+"."+to+"."+ts, func() {
							ingressFailoverCounter.Add(ctx, 1, metric.WithAttributes(
								attribute.String("routerd.resource.name", resource.Name),
								attribute.String("from", from),
								attribute.String("to", to),
							))
						})
					}
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

func recordOnce(key string, fn func()) {
	statusMetricMemory.Lock()
	if _, ok := statusMetricMemory.seen[key]; ok {
		statusMetricMemory.Unlock()
		return
	}
	statusMetricMemory.seen[key] = struct{}{}
	statusMetricMemory.Unlock()
	fn()
}

func recordCounterDelta(ctx context.Context, key string, current int64, counter metric.Int64Counter, attrs ...attribute.KeyValue) {
	statusMetricMemory.Lock()
	previous := statusMetricMemory.last[key]
	statusMetricMemory.last[key] = current
	statusMetricMemory.Unlock()
	if current <= previous {
		return
	}
	counter.Add(ctx, current-previous, metric.WithAttributes(attrs...))
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

func statusMaps(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func statusMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func statusBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func defaultStatusString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
