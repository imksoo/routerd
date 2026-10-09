// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func gatewayHealth(resources []routerstate.ObjectStatus) GatewayHealth {
	health := GatewayHealth{Overall: "ok"}
	for _, resource := range resources {
		if !gatewayHealthKind(resource.Kind) {
			continue
		}
		component := GatewayHealthComponent{
			Kind:               resource.Kind,
			Name:               resource.Name,
			Status:             gatewayComponentStatus(resource.Kind, resource.Status),
			Phase:              statusText(resource.Status, "phase"),
			Reason:             stringutil.FirstNonBlank(statusText(resource.Status, "reason"), statusText(resource.Status, "message")),
			Detail:             gatewayComponentDetail(resource.Kind, resource.Status),
			SelectedCandidate:  statusText(resource.Status, "selectedCandidate"),
			PreferredCandidate: gatewayPreferredCandidate(resource.Status),
			FailedProbes:       gatewayFailedProbes(resource.Kind, resource.Status),
			LastTransition:     gatewayLastTransition(resource.Status),
			Waiting:            gatewayWaiting(resource.Status["waiting"]),
		}
		component.SelectedPath, component.PreferredPath = gatewayEvidencePaths(resource.Kind, resource.Status)
		if component.SelectedPath != "" && component.PreferredPath != "" && component.SelectedPath != component.PreferredPath {
			component.FallbackReason = component.Reason
		}
		health.Components = append(health.Components, component)
	}
	if len(health.Components) == 0 {
		health.Overall = "unknown"
		return health
	}
	for _, component := range health.Components {
		if gatewayHealthRank(component.Status) > gatewayHealthRank(health.Overall) {
			health.Overall = component.Status
		}
	}
	return health
}

func gatewayHealthKind(kind string) bool {
	switch kind {
	case "DNSResolver", "DSLiteTunnel", "DHCPv6PrefixDelegation", "EgressRoutePolicy", "NAT44Rule", "HealthCheck":
		return true
	default:
		return false
	}
}

func gatewayComponentStatus(kind string, status map[string]any) string {
	if len(status) == 0 {
		return "unknown"
	}
	// A resource whose condition evaluated false is intentionally inactive.
	// It is not a partially applied gateway component, so it must not affect
	// the gateway-health aggregate. Do this before kind-specific handling:
	// NAT44Rule and HealthCheck both otherwise map every Pending phase to
	// degraded, which made an inactive standby path look like an outage.
	if gatewayStatusSuppressedByReason(status) {
		return "skip"
	}
	switch kind {
	case "EgressRoutePolicy":
		return gatewayEgressRoutePolicyStatus(status)
	case "NAT44Rule":
		return gatewayNAT44RuleStatus(status)
	case "HealthCheck":
		return gatewayHealthCheckStatus(status)
	}
	return gatewayGenericComponentStatus(status)
}

func gatewayStatusSuppressedByReason(status map[string]any) bool {
	switch statusText(status, "reason") {
	case "WhenFalse", "DependsOnFalse":
		return true
	default:
		return false
	}
}

func gatewayGenericComponentStatus(status map[string]any) string {
	phase := strings.ToLower(statusText(status, "phase"))
	health := strings.ToLower(statusText(status, "health"))
	switch {
	case gatewayDownStatus(phase), gatewayDownStatus(health):
		return "down"
	case len(gatewayWaiting(status["waiting"])) > 0:
		return "degraded"
	case gatewayDegradedStatus(phase), gatewayDegradedStatus(health):
		return "degraded"
	case gatewayOKStatus(health), gatewayOKStatus(phase):
		return "ok"
	default:
		return "unknown"
	}
}

func gatewayEgressRoutePolicyStatus(status map[string]any) string {
	base := gatewayGenericComponentStatus(status)
	if base != "ok" {
		return base
	}
	selected := statusText(status, "selectedCandidate")
	preferred := gatewayPreferredCandidate(status)
	if selected != "" && preferred != "" && selected != preferred {
		return "warn"
	}
	return "pass"
}

func gatewayNAT44RuleStatus(status map[string]any) string {
	switch strings.ToLower(statusText(status, "phase")) {
	case "applied", "active":
		return "pass"
	case "error", "failed":
		return "down"
	case "pending":
		return "degraded"
	default:
		return gatewayGenericComponentStatus(status)
	}
}

func gatewayHealthCheckStatus(status map[string]any) string {
	phase := strings.ToLower(statusText(status, "phase"))
	health := strings.ToLower(statusText(status, "health"))
	switch {
	case phase == "disabled" || health == "disabled":
		return "skip"
	case phase == "healthy" || health == "healthy" || health == "healthok":
		return "pass"
	case phase == "unhealthy" || phase == "failed" || health == "unhealthy" || health == "failed" || health == "healthfailed":
		return "down"
	case phase == "pending" || health == "pending":
		return "degraded"
	default:
		return gatewayGenericComponentStatus(status)
	}
}

func gatewayDownStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "error", "failed", "fail", "healthfailed", "healthfail", "down", "unhealthy", "lost", "expired":
		return true
	default:
		return false
	}
}

func gatewayDegradedStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "degraded", "healthdegraded", "warning", "warn", "pending", "blocked", "starting", "acquiring", "idle":
		return true
	default:
		return false
	}
}

func gatewayOKStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "ok", "healthok", "healthy", "pass", "passing", "applied", "active", "ready", "running", "up", "bound", "installed", "observed":
		return true
	default:
		return false
	}
}

func gatewayHealthRank(status string) int {
	switch status {
	case "down":
		return 3
	case "degraded", "warn":
		return 2
	case "unknown":
		return 1
	default:
		return 0
	}
}

func gatewayComponentDetail(kind string, status map[string]any) string {
	var keys []string
	switch kind {
	case "DNSResolver":
		keys = []string{"listeners", "sources", "listenAddresses", "health"}
	case "DHCPv6PrefixDelegation":
		keys = []string{"currentPrefix", "serverDUID", "health"}
	case "DSLiteTunnel":
		keys = []string{"aftrName", "aftrIPv6", "interface", "device", "localIPv6", "health"}
	case "EgressRoutePolicy":
		keys = []string{"selectedCandidate", "preferredCandidate", "selectedDevice", "selectedGateway", "selectedInterface", "selectedRouteTable", "selectedMetric", "health"}
	case "NAT44Rule":
		keys = []string{"egressInterface", "activeEgressInterface", "snatAddress", "health"}
	case "HealthCheck":
		keys = []string{"target", "sourceAddress", "sourceInterface", "protocol", "consecutiveFailed", "health"}
	default:
		keys = []string{"health"}
	}
	var parts []string
	if kind == "EgressRoutePolicy" {
		selected := statusText(status, "selectedCandidate")
		preferred := gatewayPreferredCandidate(status)
		if selected != "" || preferred != "" {
			parts = append(parts, fmt.Sprintf("selected=%s,preferred=%s", selected, preferred))
		}
	}
	for _, key := range keys {
		text := statusAnyText(status[key])
		if key == "preferredCandidate" && text == "" {
			text = gatewayPreferredCandidate(status)
		}
		if text != "" {
			parts = append(parts, key+"="+text)
		}
	}
	return strings.Join(parts, " ")
}

func gatewayPreferredCandidate(status map[string]any) string {
	if preferred := statusText(status, "preferredCandidate"); preferred != "" {
		return preferred
	}
	if preferred := statusText(status, "desiredCandidate"); preferred != "" {
		return preferred
	}
	candidates := statusList(status["candidates"])
	if len(candidates) == 0 {
		return ""
	}
	type preferredCandidate struct {
		name     string
		weight   int
		priority int
		set      bool
	}
	var best preferredCandidate
	for _, candidate := range candidates {
		disabled, ok := statusBoolValue(candidate["disabled"])
		if ok && disabled {
			continue
		}
		name := statusAnyText(candidate["name"])
		if name == "" {
			continue
		}
		item := preferredCandidate{name: name, weight: statusIntValue(candidate["weight"]), priority: statusIntValue(candidate["priority"]), set: true}
		if !best.set || item.weight > best.weight || (item.weight == best.weight && item.priority < best.priority) {
			best = item
		}
	}
	return best.name
}

func gatewayEvidencePaths(kind string, status map[string]any) (string, string) {
	if kind != "EgressRoutePolicy" {
		return "", ""
	}
	return statusText(status, "selectedCandidate"), gatewayPreferredCandidate(status)
}

func gatewayFailedProbes(kind string, status map[string]any) []string {
	if kind != "HealthCheck" {
		return nil
	}
	var probes []string
	for _, key := range []string{"failedProbes", "recentFailedProbes", "failedProbeNames"} {
		probes = appendGatewayProbeNames(probes, status[key])
	}
	for _, probe := range statusList(status["probes"]) {
		result := strings.ToLower(stringutil.FirstNonBlank(statusAnyText(probe["result"]), statusAnyText(probe["status"]), statusAnyText(probe["phase"]), statusAnyText(probe["health"])))
		if !gatewayDownStatus(result) {
			continue
		}
		probes = appendGatewayProbeNames(probes, stringutil.FirstNonBlank(statusAnyText(probe["name"]), statusAnyText(probe["target"]), statusAnyText(probe["address"])))
	}
	if len(probes) > 5 {
		probes = probes[:5]
	}
	return probes
}

func appendGatewayProbeNames(out []string, value any) []string {
	switch typed := value.(type) {
	case []string:
		for _, item := range typed {
			out = appendUnique(out, item)
		}
	case []any:
		for _, item := range typed {
			out = appendGatewayProbeNames(out, item)
		}
	case string:
		for _, item := range strings.Split(typed, ",") {
			out = appendUnique(out, item)
		}
	default:
		if text := statusAnyText(value); text != "" {
			out = appendUnique(out, text)
		}
	}
	return out
}

func gatewayLastTransition(status map[string]any) string {
	for _, key := range []string{"lastTransitionAt", "updatedAt"} {
		if text := statusTimestampText(status[key]); text != "" {
			return text
		}
	}
	return ""
}

func statusTimestampText(value any) string {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return ""
		}
		return typed.UTC().Format(time.RFC3339Nano)
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return ""
		}
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			return ""
		}
		return parsed.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
}

func gatewayWaiting(value any) []map[string]string {
	switch typed := value.(type) {
	case []map[string]string:
		return append([]map[string]string(nil), typed...)
	case []map[string]any:
		out := make([]map[string]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, gatewayWaitingMap(item))
		}
		return out
	case []any:
		out := make([]map[string]string, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				out = append(out, gatewayWaitingMap(object))
				continue
			}
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, map[string]string{"value": text})
			}
		}
		return out
	default:
		return nil
	}
}

func gatewayWaitingMap(values map[string]any) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			out[key] = text
		}
	}
	return out
}
