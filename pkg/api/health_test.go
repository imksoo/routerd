// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"testing"
	"time"
)

func TestRoutingObservationExpiryAndFailureGrace(t *testing.T) {
	now := time.Now().UTC()
	spec := HealthCheckSpec{UnhealthyThreshold: 3, Interval: "1s", Timeout: "1s"}
	for _, tc := range []struct {
		name, phase string
		failures    int
		checked     time.Time
		want        bool
	}{
		{"healthy", "Healthy", 0, now, true},
		{"failure grace", "Failing", 2, now, true},
		{"threshold", "Failing", 3, now, false},
		{"unhealthy", "Unhealthy", 3, now, false},
		{"expired", "Healthy", 0, now.Add(-2*time.Minute - time.Nanosecond), false},
		{"future", "Healthy", 0, now.Add(time.Second), false},
		{"missing", "Healthy", 0, time.Time{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := HealthObservation{tc.phase, tc.checked, tc.failures}
			if got := o.Usable(now, spec); got != tc.want {
				t.Fatalf("usable=%v want %v", got, tc.want)
			}
		})
	}
}
