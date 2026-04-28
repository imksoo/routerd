package state

import (
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

type PDLease struct {
	LastPrefix         string `json:"lastPrefix,omitempty"`
	LastObservedServer string `json:"lastObservedServer,omitempty"`
	PreferredLifetime  string `json:"preferredLifetime,omitempty"`
	ValidLifetime      string `json:"validLifetime,omitempty"`
	LastObservedAt     string `json:"lastObservedAt,omitempty"`
}

func EncodePDLease(lease PDLease) string {
	data, err := json.Marshal(lease)
	if err != nil {
		return ""
	}
	return string(data)
}

func DecodePDLease(value string) (PDLease, bool) {
	var lease PDLease
	if strings.TrimSpace(value) == "" {
		return lease, false
	}
	if err := json.Unmarshal([]byte(value), &lease); err != nil {
		return lease, false
	}
	return lease, true
}

func PDLeaseHintPrefix(lease PDLease, now time.Time) (string, bool) {
	prefix := strings.TrimSpace(lease.LastPrefix)
	if prefix == "" {
		return "", false
	}
	if _, err := netip.ParsePrefix(prefix); err != nil {
		return "", false
	}
	validLifetime := strings.TrimSpace(lease.ValidLifetime)
	if validLifetime == "" {
		return prefix, true
	}
	observedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(lease.LastObservedAt))
	if err != nil {
		return "", false
	}
	lifetime, ok := ParseLeaseLifetime(validLifetime)
	if !ok {
		return "", false
	}
	if lifetime >= 0 && !now.UTC().Before(observedAt.UTC().Add(lifetime)) {
		return "", false
	}
	return prefix, true
}

func ParseLeaseLifetime(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.EqualFold(value, "infinity") || strings.EqualFold(value, "infinite") {
		return -1, true
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, false
	}
	return duration, true
}
