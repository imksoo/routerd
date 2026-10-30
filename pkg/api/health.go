// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"math"
	"net/netip"
	"strings"
	"time"
)

// HealthProbe is the normalized probe configuration whose sample may authorize
// routing. It contains no historical counters or current source lifetime.
type HealthProbe struct {
	Target             string        `json:"target"`
	Protocol           string        `json:"protocol"`
	AddressFamily      string        `json:"addressFamily"`
	Port               int           `json:"port"`
	Via                string        `json:"via,omitempty"`
	FwMark             int           `json:"fwmark,omitempty"`
	SourceInterface    string        `json:"sourceInterface,omitempty"`
	SourceAddress      string        `json:"sourceAddress,omitempty"`
	Interval           time.Duration `json:"interval,string"`
	Timeout            time.Duration `json:"timeout,string"`
	HealthyThreshold   int           `json:"healthyThreshold,string"`
	UnhealthyThreshold int           `json:"unhealthyThreshold,string"`
}

func (s HealthCheckSpec) Probe() HealthProbe {
	protocol := strings.ToLower(strings.TrimSpace(s.Protocol))
	if protocol == "" {
		protocol = "icmp"
	}
	family := strings.ToLower(strings.TrimSpace(s.AddressFamily))
	if family == "" {
		family = "ipv4"
	}
	port := s.Port
	if port == 0 {
		switch protocol {
		case "tcp":
			port = 443
		case "dns":
			port = 53
		case "http":
			port = 80
		}
	}
	interval, err := time.ParseDuration(s.Interval)
	if err != nil || interval <= 0 {
		interval = 30 * time.Second
	}
	timeout, err := time.ParseDuration(s.Timeout)
	if err != nil || timeout <= 0 {
		timeout = 3 * time.Second
	}
	healthy, unhealthy := s.HealthyThreshold, s.UnhealthyThreshold
	if healthy <= 0 {
		healthy = 1
	}
	if unhealthy <= 0 {
		unhealthy = 3
	}
	normalizeIP := func(value string) string {
		value = strings.TrimSpace(value)
		if address, err := netip.ParseAddr(value); err == nil {
			return address.Unmap().String()
		}
		return value
	}
	return HealthProbe{
		Target: normalizeIP(s.Target), Protocol: protocol, AddressFamily: family, Port: port,
		Via: normalizeIP(s.Via), FwMark: s.FwMark, SourceInterface: strings.TrimSpace(s.SourceInterface), SourceAddress: normalizeIP(s.SourceAddress),
		Interval: interval, Timeout: timeout, HealthyThreshold: healthy, UnhealthyThreshold: unhealthy,
	}
}

func (p HealthProbe) Freshness() time.Duration {
	if p.Interval <= 0 || p.Timeout <= 0 {
		return 2 * time.Minute
	}
	if p.Interval > (time.Duration(math.MaxInt64)-p.Timeout)/3 {
		return time.Duration(math.MaxInt64)
	}
	return max(2*time.Minute, 3*p.Interval+p.Timeout)
}

// HealthObservation crosses the worker and persistence boundaries as one typed
// sample. Retained event/display history is not another routing input.
type HealthObservation struct {
	Probe             HealthProbe `json:"probe"`
	Phase             string      `json:"phase"`
	CheckedAt         time.Time   `json:"lastCheckedAt"`
	ConsecutiveFailed int         `json:"consecutiveFailed"`
}

func (o HealthObservation) Current(now time.Time) bool {
	p := o.Probe
	if p.Target == "" || p.Interval <= 0 || p.Timeout <= 0 || p.HealthyThreshold <= 0 || p.UnhealthyThreshold <= 0 ||
		p.Port < 0 || p.Port > 65535 || p.FwMark < 0 || uint64(p.FwMark) > 1<<32-1 || o.ConsecutiveFailed < 0 {
		return false
	}
	switch p.Protocol {
	case "icmp", "tcp", "dns", "http":
	default:
		return false
	}
	if p.AddressFamily != "ipv4" && p.AddressFamily != "ipv6" {
		return false
	}
	switch o.Phase {
	case "Healthy", "Passing", "Failing", "Unhealthy":
	default:
		return false
	}
	return !o.CheckedAt.IsZero() && !now.Before(o.CheckedAt) && now.Sub(o.CheckedAt) <= p.Freshness()
}

func (o HealthObservation) Usable(now time.Time, spec HealthCheckSpec) bool {
	if !BoolDefault(spec.Enabled, true) || o.Probe != spec.Probe() || !o.Current(now) {
		return false
	}
	switch o.Phase {
	case "Healthy":
		return true
	case "Failing":
		return o.ConsecutiveFailed > 0 && o.ConsecutiveFailed < o.Probe.UnhealthyThreshold
	default:
		return false
	}
}
