// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"testing"
	"time"
)

func TestRoutingObservationExpiryAndFailureGrace(t *testing.T) {
	now := time.Now().UTC()
	spec := HealthCheckSpec{Target: "1.1.1.1", UnhealthyThreshold: 3, Interval: "1s", Timeout: "1s"}
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
			o := HealthObservation{Probe: spec.Probe(), Phase: tc.phase, CheckedAt: tc.checked, ConsecutiveFailed: tc.failures}
			if got := o.Usable(now, spec); got != tc.want {
				t.Fatalf("usable=%v want %v", got, tc.want)
			}
		})
	}
}

func TestHealthObservationRequiresCurrentConfiguredProbe(t *testing.T) {
	now := time.Now().UTC()
	spec := HealthCheckSpec{Target: "192.0.2.1", Protocol: "tcp", SourceInterface: "wan0", SourceAddress: "192.0.2.2", FwMark: 17}
	sample := HealthObservation{Probe: spec.Probe(), Phase: "Healthy", CheckedAt: now}
	if !sample.Usable(now, spec) {
		t.Fatal("fresh configured probe was not usable")
	}
	changed := spec
	changed.Target = "192.0.2.3"
	if sample.Usable(now, changed) {
		t.Fatal("previous target authorized current route")
	}
	changed = spec
	changed.SourceInterface = "wan1"
	if sample.Usable(now, changed) {
		t.Fatal("previous interface authorized current route")
	}
	changed = spec
	changed.SourceAddress = "192.0.2.4"
	if sample.Usable(now, changed) {
		t.Fatal("previous source authorized current route")
	}
	changed = spec
	changed.FwMark = 0
	if sample.Usable(now, changed) {
		t.Fatal("previous mark authorized current route")
	}
	changed = spec
	changed.UnhealthyThreshold = 5
	if sample.Usable(now, changed) {
		t.Fatal("previous thresholds authorized current route")
	}
	sample.Probe = HealthProbe{}
	if sample.Current(now) || sample.Usable(now, spec) {
		t.Fatal("identityless retained history authorized routing")
	}
}

func TestHealthProbeNormalizationAndFreshnessOverflow(t *testing.T) {
	implicit := HealthCheckSpec{Target: " ::ffff:192.0.2.1 ", Protocol: "tcp"}.Probe()
	explicit := HealthCheckSpec{Target: "192.0.2.1", Protocol: "tcp", AddressFamily: "ipv4", Port: 443, Interval: "30s", Timeout: "3s", HealthyThreshold: 1, UnhealthyThreshold: 3}.Probe()
	if implicit != explicit {
		t.Fatalf("different effective probes: %+v %+v", implicit, explicit)
	}
	if got := (HealthProbe{Interval: time.Duration(1<<63 - 1), Timeout: time.Second}).Freshness(); got != time.Duration(1<<63-1) {
		t.Fatalf("freshness overflowed: %v", got)
	}
}
