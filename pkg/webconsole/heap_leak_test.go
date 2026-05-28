// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// TestReverseDNSCacheLookupManyEnforcesCapAfterStore asserts that a single
// lookupMany() call, even when it resolves more new addresses than the cap
// allows, ends with len(entries) <= reverseDNSCacheMaxEntries. The entry-
// time pruneLocked alone left a short window where the cache could briefly
// exceed the cap; the exit-time prune was added to close it.
func TestReverseDNSCacheLookupManyEnforcesCapAfterStore(t *testing.T) {
	c := newReverseDNSCache(time.Hour)
	now := time.Now()
	// Pre-fill cap-100 live entries (oldest expiries first) so a single
	// lookupMany has to evict during the exit prune.
	for i := 0; i < reverseDNSCacheMaxEntries-100; i++ {
		c.store(encodeIPv4Address(uint32(i)), "host", now.Add(time.Duration(i)*time.Second))
	}
	// Request 200 brand-new addresses — more than the 100 slots free, so
	// the post-store cache size will exceed the cap by 100 entries until
	// the exit prune runs.
	pending := make([]string, 0, 200)
	for i := reverseDNSCacheMaxEntries - 100; i < reverseDNSCacheMaxEntries+100; i++ {
		pending = append(pending, encodeIPv4Address(uint32(i)))
	}
	lookup := func(ctx context.Context, address string) ([]string, error) {
		return []string{"resolved-" + address + "."}, nil
	}
	_ = c.lookupMany(context.Background(), pending, lookup)

	c.mu.Lock()
	got := len(c.entries)
	c.mu.Unlock()
	if got > reverseDNSCacheMaxEntries {
		t.Fatalf("after lookupMany: cache size = %d, want <= %d", got, reverseDNSCacheMaxEntries)
	}
}

// TestReverseDNSLookupManyBoundsGoroutines asserts that lookupMany resolves
// every pending address while never running more than
// reverseDNSLookupConcurrency lookups concurrently — i.e. the goroutine /
// in-flight count is flat regardless of len(pending). Counting goroutines via
// runtime.NumGoroutine() is flaky, so instead the lookup func records the peak
// number of simultaneously in-flight calls with atomics, which directly proves
// the concurrency bound that matters.
func TestReverseDNSLookupManyBoundsGoroutines(t *testing.T) {
	c := newReverseDNSCache(time.Hour)

	const pendingCount = 500
	pending := make([]string, 0, pendingCount)
	for i := 0; i < pendingCount; i++ {
		pending = append(pending, encodeIPv4Address(uint32(i)))
	}

	var inFlight int64
	var maxInFlight int64
	lookup := func(ctx context.Context, address string) ([]string, error) {
		cur := atomic.AddInt64(&inFlight, 1)
		// Track the high-water mark of concurrent calls.
		for {
			prev := atomic.LoadInt64(&maxInFlight)
			if cur <= prev || atomic.CompareAndSwapInt64(&maxInFlight, prev, cur) {
				break
			}
		}
		// Hold the slot briefly so many addresses pile up behind the pool,
		// forcing real contention without making the test slow.
		time.Sleep(time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		return []string{"resolved-" + address + "."}, nil
	}

	out := c.lookupMany(context.Background(), pending, lookup)

	if peak := atomic.LoadInt64(&maxInFlight); peak > reverseDNSLookupConcurrency {
		t.Fatalf("max in-flight lookups = %d, want <= %d", peak, reverseDNSLookupConcurrency)
	}
	if got := len(out); got != pendingCount {
		t.Fatalf("resolved %d addresses, want %d", got, pendingCount)
	}
	for i := 0; i < pendingCount; i++ {
		addr := encodeIPv4Address(uint32(i))
		want := fmt.Sprintf("resolved-%s", addr)
		if got := out[addr]; got != want {
			t.Fatalf("out[%s] = %q, want %q", addr, got, want)
		}
	}
}

// TestReverseDNSLookupManyCapsPending asserts that a single lookupMany call
// resolves at most reverseDNSPendingMax addresses, independent of how many the
// caller passes in. The lookup func records the distinct addresses it actually
// sees; that count must not exceed the local cap even when far more addresses
// are pending.
func TestReverseDNSLookupManyCapsPending(t *testing.T) {
	c := newReverseDNSCache(time.Hour)

	const pendingCount = 1200
	if pendingCount <= reverseDNSPendingMax {
		t.Fatalf("test setup: pendingCount %d must exceed reverseDNSPendingMax %d", pendingCount, reverseDNSPendingMax)
	}
	pending := make([]string, 0, pendingCount)
	for i := 0; i < pendingCount; i++ {
		pending = append(pending, encodeIPv4Address(uint32(i)))
	}

	var mu sync.Mutex
	seen := map[string]struct{}{}
	lookup := func(ctx context.Context, address string) ([]string, error) {
		mu.Lock()
		seen[address] = struct{}{}
		mu.Unlock()
		return []string{"resolved-" + address + "."}, nil
	}

	_ = c.lookupMany(context.Background(), pending, lookup)

	mu.Lock()
	got := len(seen)
	mu.Unlock()
	if got > reverseDNSPendingMax {
		t.Fatalf("lookup saw %d distinct addresses, want <= %d", got, reverseDNSPendingMax)
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
