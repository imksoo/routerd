// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"strconv"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

// RoutingObservation is the current information needed to select an egress.
// Diagnostic history and persistence metadata do not participate in this decision.
type RoutingObservation struct {
	Phase             string
	CheckedAt         time.Time
	ConsecutiveFailed int
}

// RoutingObservationFromStatus narrows the current store/daemon boundary. The
// store decodes JSON numbers as float64; daemon observations carry strings.
func RoutingObservationFromStatus(status map[string]any) RoutingObservation {
	if observed, ok := status["observed"].(map[string]any); ok && len(observed) > 0 {
		status = observed
	}
	var out RoutingObservation
	out.Phase, _ = status["phase"].(string)
	checked, _ := status["lastCheckedAt"].(string)
	out.CheckedAt, _ = time.Parse(time.RFC3339Nano, checked)
	switch count := status["consecutiveFailed"].(type) {
	case int:
		out.ConsecutiveFailed = count
	case float64:
		if count >= 0 && count < float64(1<<53) && count == float64(int(count)) {
			out.ConsecutiveFailed = int(count)
		}
	case string:
		out.ConsecutiveFailed, _ = strconv.Atoi(count)
	}
	return out
}

// Usable keeps a previously healthy path during the configured failure grace
// period. Missing, future-dated or expired observations never establish readiness.
func (o RoutingObservation) Usable(now time.Time, spec api.HealthCheckSpec) bool {
	threshold := unhealthyThreshold(spec)
	switch o.Phase {
	case PhaseHealthy:
	case PhaseFailing:
		if o.ConsecutiveFailed <= 0 || o.ConsecutiveFailed >= threshold {
			return false
		}
	default:
		return false
	}
	freshness := 2 * time.Minute
	if candidate := durationOr(spec.Interval, 30*time.Second)*3 + durationOr(spec.Timeout, 3*time.Second); candidate > freshness {
		freshness = candidate
	}
	return !o.CheckedAt.IsZero() && !now.Before(o.CheckedAt) && !now.After(o.CheckedAt.Add(freshness))
}
