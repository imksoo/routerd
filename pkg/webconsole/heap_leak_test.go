// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"testing"
	"time"
)

// TestGetConsoleMetricsReturnsSameInstruments asserts that getConsoleMetrics()
// always hands out the same instrument objects, so /api/v1/summary polling
// cannot accumulate duplicate OTel instrument metadata in the SDK over time.
//
// Background: recordConsoleMetrics() used to call meter.Int64Gauge / Float64Gauge
// inside the request path. Every call returned a fresh struct (the OTel SDK
// reuses metadata internally, but the Go-side instrument value the caller
// observed was a new allocation per call). Switching to a sync.Once singleton
// removes that per-call allocation entirely.
func TestGetConsoleMetricsReturnsSameInstruments(t *testing.T) {
	first := getConsoleMetrics()
	second := getConsoleMetrics()
	if first.dryRunGauge != second.dryRunGauge {
		t.Fatalf("dryRunGauge differs between calls")
	}
	if first.controllerErrorGauge != second.controllerErrorGauge {
		t.Fatalf("controllerErrorGauge differs between calls")
	}
	if first.controllerLastDurationGauge != second.controllerLastDurationGauge {
		t.Fatalf("controllerLastDurationGauge differs between calls")
	}
	if first.phaseGauge != second.phaseGauge {
		t.Fatalf("phaseGauge differs between calls")
	}
	if first.leaseGauge != second.leaseGauge {
		t.Fatalf("leaseGauge differs between calls")
	}
	if first.stickyGauge != second.stickyGauge {
		t.Fatalf("stickyGauge differs between calls")
	}
	if first.clientGauge != second.clientGauge {
		t.Fatalf("clientGauge differs between calls")
	}
}

// TestReverseDNSCachePrunesExpiredEntries asserts the cache drops entries
// past their TTL on the next lookupMany call (via pruneLocked).
func TestReverseDNSCachePrunesExpiredEntries(t *testing.T) {
	c := newReverseDNSCache(time.Hour)
	now := time.Now()
	c.store("203.0.113.10", "example-a", now.Add(-time.Minute)) // already expired
	c.store("203.0.113.11", "example-b", now.Add(time.Hour))    // live
	c.store("203.0.113.12", "example-c", now.Add(-time.Hour))   // already expired

	c.mu.Lock()
	if got := len(c.entries); got != 3 {
		c.mu.Unlock()
		t.Fatalf("setup: cache size = %d, want 3", got)
	}
	c.pruneLocked(now)
	got := len(c.entries)
	_, stillHasExpired1 := c.entries["203.0.113.10"]
	_, stillHasExpired2 := c.entries["203.0.113.12"]
	_, stillHasLive := c.entries["203.0.113.11"]
	c.mu.Unlock()

	if got != 1 {
		t.Fatalf("after prune: cache size = %d, want 1", got)
	}
	if stillHasExpired1 || stillHasExpired2 {
		t.Fatalf("after prune: expired entries still present (10=%v 12=%v)", stillHasExpired1, stillHasExpired2)
	}
	if !stillHasLive {
		t.Fatalf("after prune: live entry dropped")
	}
}

// TestReverseDNSCacheCapsAtMaxEntries asserts pruneLocked enforces the
// reverseDNSCacheMaxEntries cap even when every entry is still within TTL.
// Without this cap, distinct remote IPs would accumulate without bound.
func TestReverseDNSCacheCapsAtMaxEntries(t *testing.T) {
	c := newReverseDNSCache(time.Hour)
	now := time.Now()
	// Insert cap + 100 live entries with staggered expiries so prune has
	// a deterministic ordering to drop the oldest.
	overflow := 100
	for i := 0; i < reverseDNSCacheMaxEntries+overflow; i++ {
		addr := encodeIPv4Address(uint32(i))
		c.store(addr, "host", now.Add(time.Duration(i)*time.Second))
	}

	c.mu.Lock()
	if got := len(c.entries); got != reverseDNSCacheMaxEntries+overflow {
		c.mu.Unlock()
		t.Fatalf("setup: cache size = %d, want %d", got, reverseDNSCacheMaxEntries+overflow)
	}
	c.pruneLocked(now)
	got := len(c.entries)
	c.mu.Unlock()

	if got != reverseDNSCacheMaxEntries {
		t.Fatalf("after prune: cache size = %d, want %d", got, reverseDNSCacheMaxEntries)
	}
}

// encodeIPv4Address turns a uint32 into a dotted-quad string so each test
// entry has a unique map key without going through net.IP allocation.
func encodeIPv4Address(n uint32) string {
	a := byte(n >> 24)
	b := byte(n >> 16)
	c := byte(n >> 8)
	d := byte(n)
	const digits = "0123456789"
	buf := make([]byte, 0, 16)
	for _, octet := range [...]byte{a, b, c, d} {
		if octet >= 100 {
			buf = append(buf, digits[octet/100])
			buf = append(buf, digits[(octet/10)%10])
			buf = append(buf, digits[octet%10])
		} else if octet >= 10 {
			buf = append(buf, digits[octet/10])
			buf = append(buf, digits[octet%10])
		} else {
			buf = append(buf, digits[octet])
		}
		buf = append(buf, '.')
	}
	return string(buf[:len(buf)-1])
}
