// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
)

func (h Handler) enrichConnectionsWithRemoteIdentity(ctx context.Context, table *observe.ConnectionTable) error {
	if table == nil || len(table.Entries) == 0 {
		return nil
	}
	addresses := make([]string, 0, len(table.Entries))
	seen := map[string]bool{}
	for i := range table.Entries {
		entry := &table.Entries[i]
		annotateTupleServices(&entry.Original, entry.Protocol)
		annotateTupleServices(&entry.Reply, entry.Protocol)
		for _, address := range []string{entry.Original.Source, entry.Original.Destination, entry.Reply.Source, entry.Reply.Destination} {
			if !shouldReverseLookup(address) || seen[address] {
				continue
			}
			seen[address] = true
			addresses = append(addresses, address)
		}
	}
	if len(addresses) == 0 || h.reverseDNS == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	labels := h.reverseDNS.lookupMany(ctx, addresses, h.opts.ReverseLookup)
	for i := range table.Entries {
		entry := &table.Entries[i]
		annotateTupleHostnames(&entry.Original, labels)
		annotateTupleHostnames(&entry.Reply, labels)
		applyConnectionPortFallback(entry)
	}
	return nil
}

func (h Handler) enrichFirewallLogsWithRemoteIdentity(ctx context.Context, logs []logstore.FirewallLogEntry) error {
	if len(logs) == 0 {
		return nil
	}
	addresses := make([]string, 0, len(logs)*2)
	seen := map[string]bool{}
	for i := range logs {
		entry := &logs[i]
		if entry.SrcService == "" {
			entry.SrcService = serviceNameForPort(entry.Protocol, entry.SrcPort)
		}
		if entry.DstService == "" {
			entry.DstService = serviceNameForPort(entry.Protocol, entry.DstPort)
		}
		for _, address := range []string{entry.SrcAddress, entry.DstAddress} {
			if !shouldReverseLookup(address) || seen[address] {
				continue
			}
			seen[address] = true
			addresses = append(addresses, address)
		}
	}
	if len(addresses) == 0 || h.reverseDNS == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	labels := h.reverseDNS.lookupMany(ctx, addresses, h.opts.ReverseLookup)
	for i := range logs {
		if logs[i].SrcHostname == "" {
			logs[i].SrcHostname = labels[logs[i].SrcAddress]
		}
		if logs[i].DstHostname == "" {
			logs[i].DstHostname = labels[logs[i].DstAddress]
		}
	}
	return nil
}

func annotateTupleServices(tuple *observe.ConntrackTuple, protocol string) {
	if tuple == nil {
		return
	}
	if tuple.SourceService == "" {
		tuple.SourceService = serviceNameForPort(protocol, atoiDefault(tuple.SourcePort, 0))
	}
	if tuple.DestinationService == "" {
		tuple.DestinationService = serviceNameForPort(protocol, atoiDefault(tuple.DestinationPort, 0))
	}
}

func annotateTupleHostnames(tuple *observe.ConntrackTuple, labels map[string]string) {
	if tuple == nil {
		return
	}
	if tuple.SourceHostname == "" {
		tuple.SourceHostname = labels[tuple.Source]
	}
	if tuple.DestinationHostname == "" {
		tuple.DestinationHostname = labels[tuple.Destination]
	}
}

func shouldReverseLookup(address string) bool {
	address = strings.TrimSpace(address)
	if address == "" {
		return false
	}
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	return addr.IsValid() && !addr.IsUnspecified() && !addr.IsMulticast()
}

// reverseDNSCacheMaxEntries caps the size of the in-memory reverse DNS cache.
// Without an upper bound, every distinct remote IP seen by /api/v1/summary
// (firewall logs, connection table, flow log) would accumulate a permanent
// entry: TTL alone only governs re-lookup, not pruning. 4096 covers a
// generous "every endpoint a busy home network has ever talked to" working
// set while keeping the heap bounded.
const reverseDNSCacheMaxEntries = 4096

// reverseDNSLookupConcurrency bounds the number of worker goroutines a single
// lookupMany call spawns. Without this bound, lookupMany spawned one goroutine
// per pending address: a /api/v1/summary request capped at 1000 rows could
// produce ~1000 goroutines (mostly blocked on a semaphore). A fixed-size
// worker pool keeps the goroutine count flat regardless of len(pending) while
// preserving the same effective in-flight concurrency.
const reverseDNSLookupConcurrency = 8

// reverseDNSPendingMax bounds how many addresses a single lookupMany call
// will resolve, independent of the caller's own limit. Summary callers
// already cap query rows at 1000; this keeps the bound local to the cache
// utility. Excess addresses are simply not resolved this call and get
// picked up on a subsequent lookup once earlier results are cached.
const reverseDNSPendingMax = 1000

type reverseDNSCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]reverseDNSEntry
}

type reverseDNSEntry struct {
	name    string
	expires time.Time
}

func newReverseDNSCache(ttl time.Duration) *reverseDNSCache {
	return &reverseDNSCache{ttl: ttl, entries: map[string]reverseDNSEntry{}}
}

// pruneLocked drops expired entries first, then if the cache is still over
// capacity, removes the oldest-expiring entries until we are back under the
// cap. Caller must hold c.mu.
func (c *reverseDNSCache) pruneLocked(now time.Time) {
	for address, entry := range c.entries {
		if now.After(entry.expires) {
			delete(c.entries, address)
		}
	}
	if len(c.entries) <= reverseDNSCacheMaxEntries {
		return
	}
	type item struct {
		address string
		expires time.Time
	}
	items := make([]item, 0, len(c.entries))
	for address, entry := range c.entries {
		items = append(items, item{address: address, expires: entry.expires})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].expires.Before(items[j].expires)
	})
	excess := len(c.entries) - reverseDNSCacheMaxEntries
	for i := 0; i < excess && i < len(items); i++ {
		delete(c.entries, items[i].address)
	}
}

func (c *reverseDNSCache) lookupMany(ctx context.Context, addresses []string, lookup func(context.Context, string) ([]string, error)) map[string]string {
	now := time.Now()
	out := map[string]string{}
	var pending []string
	c.mu.Lock()
	c.pruneLocked(now)
	for _, address := range addresses {
		if entry, ok := c.entries[address]; ok && now.Before(entry.expires) {
			if entry.name != "" {
				out[address] = entry.name
			}
			continue
		}
		pending = append(pending, address)
	}
	c.mu.Unlock()
	if len(pending) == 0 || lookup == nil {
		return out
	}
	// Cap the work this call does, independent of the caller's own limit.
	// Excess addresses stay unresolved this call and are picked up next time.
	if len(pending) > reverseDNSPendingMax {
		pending = pending[:reverseDNSPendingMax]
	}
	type result struct {
		address string
		name    string
	}
	// Bound the goroutine count with a fixed-size worker pool. Each worker
	// pulls addresses from jobs, resolves them, and reports on results. The
	// total goroutines spawned per call is reverseDNSLookupConcurrency (capped
	// further when len(pending) is smaller) plus the feeder and the closer,
	// independent of len(pending).
	jobs := make(chan string)
	results := make(chan result, len(pending))
	workers := reverseDNSLookupConcurrency
	if len(pending) < workers {
		workers = len(pending)
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for address := range jobs {
				select {
				case <-ctx.Done():
					results <- result{address: address}
					continue
				default:
				}
				names, err := lookup(ctx, address)
				if err != nil {
					results <- result{address: address}
					continue
				}
				results <- result{address: address, name: normalizeReverseDNSName(names)}
			}
		}()
	}
	// Feeder: queue every pending address, stopping early if ctx is cancelled.
	// Addresses left unqueued on cancellation simply get retried next call.
	go func() {
		defer close(jobs)
		for _, address := range pending {
			select {
			case jobs <- address:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	for item := range results {
		c.store(item.address, item.name, now.Add(c.ttl))
		if item.name != "" {
			out[item.address] = item.name
		}
	}
	// Run pruneLocked a second time at the end of the call so the post-
	// store state is strictly within reverseDNSCacheMaxEntries. The entry-
	// time prune alone leaves a short window where, if a single summary
	// request resolves many never-seen addresses, the cache can briefly
	// hold (cap + new entries) until the next lookupMany call shrinks it
	// again. With this exit prune the hard cap is invariant across every
	// boundary an external observer can see.
	c.mu.Lock()
	c.pruneLocked(time.Now())
	c.mu.Unlock()
	return out
}

func (c *reverseDNSCache) store(address string, name string, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[address] = reverseDNSEntry{name: name, expires: expires}
}

func normalizeReverseDNSName(names []string) string {
	for _, name := range names {
		name = strings.TrimSuffix(strings.TrimSpace(name), ".")
		if name != "" {
			return name
		}
	}
	return ""
}
