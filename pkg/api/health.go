// SPDX-License-Identifier: BSD-3-Clause

package api

import "time"

// HealthObservation is the current probe result used by routing. It crosses the
// daemon and persistence boundaries as one typed value; event history is separate.
type HealthObservation struct {
	Phase             string    `json:"phase"`
	CheckedAt         time.Time `json:"lastCheckedAt"`
	ConsecutiveFailed int       `json:"consecutiveFailed"`
}

func (o HealthObservation) Usable(now time.Time, spec HealthCheckSpec) bool {
	if !BoolDefault(spec.Enabled, true) {
		return false
	}
	threshold := spec.UnhealthyThreshold
	if threshold <= 0 {
		threshold = 3
	}
	switch o.Phase {
	case "Healthy":
	case "Failing":
		if o.ConsecutiveFailed <= 0 || o.ConsecutiveFailed >= threshold {
			return false
		}
	default:
		return false
	}
	interval, err := time.ParseDuration(spec.Interval)
	if err != nil || interval <= 0 {
		interval = 30 * time.Second
	}
	timeout, err := time.ParseDuration(spec.Timeout)
	if err != nil || timeout <= 0 {
		timeout = 3 * time.Second
	}
	freshness := max(2*time.Minute, 3*interval+timeout)
	return !o.CheckedAt.IsZero() && !now.Before(o.CheckedAt) && !now.After(o.CheckedAt.Add(freshness))
}
