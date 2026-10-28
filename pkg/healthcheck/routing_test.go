// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"github.com/imksoo/routerd/pkg/api"
	"testing"
	"time"
)

func TestRoutingObservationExpiryAndFailureGrace(t *testing.T) {
	now := time.Now().UTC()
	spec := api.HealthCheckSpec{UnhealthyThreshold: 3, Interval: "1s", Timeout: "1s"}
	for _, tc := range []struct {
		name, phase string
		failures    int
		checked     time.Time
		want        bool
	}{
		{"healthy", PhaseHealthy, 0, now, true},
		{"failure grace", PhaseFailing, 2, now, true},
		{"threshold", PhaseFailing, 3, now, false},
		{"unhealthy", PhaseUnhealthy, 3, now, false},
		{"expired", PhaseHealthy, 0, now.Add(-2*time.Minute - time.Nanosecond), false},
		{"future", PhaseHealthy, 0, now.Add(time.Second), false},
		{"missing", PhaseHealthy, 0, time.Time{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := RoutingObservation{tc.phase, tc.checked, tc.failures}
			if got := o.Usable(now, spec); got != tc.want {
				t.Fatalf("usable=%v want %v", got, tc.want)
			}
		})
	}
}

func TestRoutingObservationNarrowsDaemonAndStoredStatus(t *testing.T) {
	now := time.Now().UTC()
	for _, count := range []any{2, "2", float64(2)} {
		status := map[string]any{"phase": PhaseFailing, "lastCheckedAt": now.Format(time.RFC3339Nano), "consecutiveFailed": count}
		for _, boundary := range []map[string]any{status, {"observed": status}} {
			if !RoutingObservationFromStatus(boundary).Usable(now, api.HealthCheckSpec{}) {
				t.Fatalf("valid current boundary not usable: %#v", boundary)
			}
		}
	}
}
