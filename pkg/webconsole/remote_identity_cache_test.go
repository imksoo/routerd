// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

// These tests verify cache behavior using injected lookup results, not live DNS.
// Channel synchronization proves that cancellation happens during a lookup;
// watchdogs only bound incomplete observations and do not measure performance.
func awaitReverseDNSCacheSignal(t *testing.T, signal <-chan struct{}, observation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("watchdog expired awaiting %s; lookup observation incomplete", observation)
	}
}

func TestReverseDNSCacheRetriesAfterInFlightCancellation(t *testing.T) {
	cache := newReverseDNSCache(time.Hour)
	addresses := []string{"192.0.2.1"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	var cancelled map[string]string
	go func() {
		cancelled = cache.lookupMany(ctx, addresses, func(ctx context.Context, address string) ([]string, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		close(finished)
	}()
	awaitReverseDNSCacheSignal(t, started, "lookup start")
	cancel()
	awaitReverseDNSCacheSignal(t, finished, "cancelled lookup completion")
	if len(cancelled) != 0 || len(cache.entries) != 0 {
		t.Fatalf("cancelled lookup left results=%v cache=%v", cancelled, cache.entries)
	}
	calls := 0
	got := cache.lookupMany(context.Background(), addresses, func(context.Context, string) ([]string, error) {
		calls++
		return []string{"recovered.example."}, nil
	})
	if calls != 1 || got[addresses[0]] != "recovered.example" {
		t.Fatalf("lookup was not retried after cancellation: calls=%d names=%v", calls, got)
	}
}

func TestReverseDNSCacheRetriesLookupFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"cancelled", context.Canceled},
		{"deadline", fmt.Errorf("lookup: %w", context.DeadlineExceeded)},
		{"timeout", &net.DNSError{Err: "timeout", IsTimeout: true}},
		{"temporary", &net.DNSError{Err: "temporary failure", IsTemporary: true}},
		{"server failure", &net.DNSError{Err: "server failure"}},
		{"other failure", errors.New("resolver unavailable")},
		{"temporary not found", &net.DNSError{Err: "temporary failure", IsNotFound: true, IsTemporary: true}},
		{"timed out not found", &net.DNSError{Err: "timeout", IsNotFound: true, IsTimeout: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := newReverseDNSCache(time.Hour)
			addresses := []string{"192.0.2.1"}
			calls := 0
			lookup := func(context.Context, string) ([]string, error) {
				calls++
				if calls == 1 {
					return nil, tc.err
				}
				return []string{"recovered.example."}, nil
			}
			if got := cache.lookupMany(context.Background(), addresses, lookup); len(got) != 0 || len(cache.entries) != 0 {
				t.Fatalf("failed lookup left results=%v cache=%v", got, cache.entries)
			}
			if got := cache.lookupMany(context.Background(), addresses, lookup); calls != 2 || got[addresses[0]] != "recovered.example" {
				t.Fatalf("failed lookup was not retried: calls=%d names=%v", calls, got)
			}
			if got := cache.lookupMany(context.Background(), addresses, lookup); calls != 2 || got[addresses[0]] != "recovered.example" {
				t.Fatalf("successful lookup was not cached: calls=%d names=%v", calls, got)
			}
		})
	}
}

func TestReverseDNSCacheUsesShortNegativeTTL(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		ttl  time.Duration
		want time.Duration
	}{
		{"not found", &net.DNSError{Err: "no such host", IsNotFound: true}, time.Hour, time.Minute},
		{"wrapped not found", fmt.Errorf("lookup: %w", &net.DNSError{Err: "no such host", IsNotFound: true}), time.Hour, time.Minute},
		{"empty success", nil, time.Hour, time.Minute},
		{"short cache", &net.DNSError{Err: "no such host", IsNotFound: true}, 10 * time.Second, 10 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := newReverseDNSCache(tc.ttl)
			address := "192.0.2.1"
			addresses := []string{address}
			calls := 0
			lookup := func(context.Context, string) ([]string, error) {
				calls++
				if calls == 1 {
					return nil, tc.err
				}
				return []string{"recovered.example."}, nil
			}
			before := time.Now()
			got := cache.lookupMany(context.Background(), addresses, lookup)
			after := time.Now()
			entry, ok := cache.entries[address]
			if len(got) != 0 || !ok || entry.name != "" {
				t.Fatalf("missing negative cache entry: names=%v entry=%+v present=%t", got, entry, ok)
			}
			if entry.expires.Before(before.Add(tc.want)) || entry.expires.After(after.Add(tc.want)) {
				t.Fatalf("negative entry expires at %s, want lookup time + %s", entry.expires, tc.want)
			}
			if got := cache.lookupMany(context.Background(), addresses, lookup); calls != 1 || len(got) != 0 {
				t.Fatalf("negative lookup was not cached: calls=%d names=%v", calls, got)
			}
			// Expire this deliberately modified fixture without sleeping.
			cache.store(address, "", time.Now().Add(-time.Second))
			if got := cache.lookupMany(context.Background(), addresses, lookup); calls != 2 || got[address] != "recovered.example" {
				t.Fatalf("expired negative lookup was not retried: calls=%d names=%v", calls, got)
			}
		})
	}
}
