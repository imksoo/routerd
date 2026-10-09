// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFindDPIFlowsForFirewallEntries(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	forward := DPIFlowEntry{FlowID: "older", FirstSeen: now.Add(-time.Minute), LastSeen: now.Add(-time.Minute), Protocol: "tcp", SrcAddress: "10.0.0.2", SrcPort: 12345, DstAddress: "192.0.2.1", DstPort: 443, AppName: "tls"}
	newest := forward
	newest.FlowID = "newest"
	newest.LastSeen = now
	newest.AppName = "https"
	newest.Risk = []string{"test"}
	newest.Metadata = map[string]string{"tls.sni": "example.test"}
	reverse := forward
	reverse.FlowID = "reverse"
	reverse.SrcAddress, reverse.DstAddress = forward.DstAddress, forward.SrcAddress
	reverse.SrcPort, reverse.DstPort = forward.DstPort, forward.SrcPort
	reverse.LastSeen = now.Add(-time.Second)
	expired := forward
	expired.FlowID = "expired"
	expired.SrcPort = 4444
	expired.LastSeen = now.Add(-2 * time.Hour)
	boundary := forward
	boundary.FlowID = "boundary"
	boundary.SrcPort = 5555
	boundary.LastSeen = now.Add(-time.Hour)
	zeroPort := forward
	zeroPort.FlowID = "zero"
	zeroPort.Protocol = "udp"
	zeroPort.SrcPort = 0
	zeroPort.DstPort = 0
	if err := log.RecordDPIFlows(context.Background(), []DPIFlowEntry{forward, newest, reverse, expired, boundary, zeroPort}, 24*time.Hour, 10000); err != nil {
		t.Fatal(err)
	}
	entry := FirewallLogEntry{Protocol: " TCP ", SrcAddress: forward.SrcAddress, SrcPort: forward.SrcPort, DstAddress: forward.DstAddress, DstPort: forward.DstPort}
	rev := entry
	rev.SrcAddress, rev.DstAddress = entry.DstAddress, entry.SrcAddress
	rev.SrcPort, rev.DstPort = entry.DstPort, entry.SrcPort
	old := entry
	old.SrcPort = 4444
	edge := entry
	edge.SrcPort = 5555
	zero := entry
	zero.Protocol = "udp"
	zero.SrcPort = 0
	zero.DstPort = 0
	missing := entry
	missing.SrcPort = 9999
	entries := []FirewallLogEntry{entry, rev, {}, old, edge, zero, missing}
	want := []string{"newest", "newest", "", "", "boundary", "zero", ""}
	// Cross multiple statement boundaries, retaining misses and duplicate order.
	for len(entries) < dpiLookupBatchSize*2+7 {
		entries = append(entries, entry)
		want = append(want, "newest")
	}
	matches, err := log.FindDPIFlowsForFirewallEntries(context.Background(), entries, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != len(entries) {
		t.Fatalf("results=%d inputs=%d", len(matches), len(entries))
	}
	for i, id := range want {
		if id == "" {
			if matches[i] != nil {
				t.Fatalf("result %d unexpectedly matched %+v", i, matches[i])
			}
			continue
		}
		if matches[i] == nil || matches[i].FlowID != id {
			t.Fatalf("result %d=%+v want %q", i, matches[i], id)
		}
	}
	if !reflect.DeepEqual(matches[0].Risk, newest.Risk) || !reflect.DeepEqual(matches[0].Metadata, newest.Metadata) {
		t.Fatalf("enrichment fields lost: %+v", matches[0])
	}
	matches[0].Metadata["tls.sni"] = "changed"
	if matches[1].Metadata["tls.sni"] != "example.test" {
		t.Fatal("duplicate results alias metadata")
	}
	for _, ttl := range []time.Duration{0, -time.Second} {
		got, err := log.FindDPIFlowsForFirewallEntries(context.Background(), []FirewallLogEntry{entry, old}, time.Time{}, ttl)
		if err != nil || got[0] == nil || got[1] != nil {
			t.Fatalf("defaults: results=%v err=%v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := log.FindDPIFlowsForFirewallEntries(ctx, entries, now, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), now.Add(-time.Second))
	defer cancel()
	if _, err := log.FindDPIFlowsForFirewallEntries(ctx, entries, now, time.Hour); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error=%v", err)
	}
	if got, err := log.FindDPIFlowsForFirewallEntries(context.Background(), nil, now, time.Hour); err != nil || len(got) != 0 {
		t.Fatalf("empty: %v %v", got, err)
	}
	var absent *FirewallLog
	if got, err := absent.FindDPIFlowsForFirewallEntries(context.Background(), entries, now, time.Hour); err != nil || len(got) != len(entries) {
		t.Fatalf("nil store: %v %v", got, err)
	}
}

func BenchmarkDPIFlowLookup(b *testing.B) {
	log, err := OpenFirewallLog(filepath.Join(b.TempDir(), "firewall.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	flows := make([]DPIFlowEntry, 1000)
	entries := make([]FirewallLogEntry, len(flows))
	for i := range flows {
		flows[i] = DPIFlowEntry{FirstSeen: now, LastSeen: now, Protocol: "tcp", SrcAddress: "10.0.0.2", SrcPort: 10000 + i, DstAddress: "192.0.2.1", DstPort: 443, AppName: "tls"}
		entries[i] = FirewallLogEntry{Protocol: flows[i].Protocol, SrcAddress: flows[i].SrcAddress, SrcPort: flows[i].SrcPort, DstAddress: flows[i].DstAddress, DstPort: flows[i].DstPort}
	}
	if err := log.RecordDPIFlows(context.Background(), flows, time.Hour, 10000); err != nil {
		b.Fatal(err)
	}
	for _, n := range []int{100, 1000} {
		for _, batch := range []bool{false, true} {
			b.Run(fmt.Sprintf("rows=%d/batch=%t", n, batch), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if batch {
						if _, err := log.FindDPIFlowsForFirewallEntries(context.Background(), entries[:n], now, time.Hour); err != nil {
							b.Fatal(err)
						}
					} else {
						for _, entry := range entries[:n] {
							if _, _, err := log.FindDPIFlowForFirewallEntry(context.Background(), entry, now, time.Hour); err != nil {
								b.Fatal(err)
							}
						}
					}
				}
			})
		}
	}
}
