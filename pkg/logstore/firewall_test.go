// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestFirewallLogRecordAndList(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	entry := FirewallLogEntry{
		Timestamp:  time.Now().UTC(),
		Action:     "drop",
		SrcAddress: "172.18.0.10",
		DstAddress: "198.51.100.1",
		Protocol:   "tcp",
		TCPFlags:   "SYN",
		L3Proto:    "ipv4",
		RuleName:   "deny-test",
	}
	if err := log.Record(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	rows, err := log.List(context.Background(), FirewallLogFilter{Action: "drop", Src: "172.18.0.10", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RuleName != "deny-test" {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0].TCPFlags != "SYN" {
		t.Fatalf("tcp flags = %#v", rows[0])
	}
}

func TestFirewallLogRecordAndListDPIFields(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	entry := FirewallLogEntry{
		Timestamp:     time.Now().UTC(),
		Action:        "drop",
		SrcAddress:    "172.18.0.10",
		DstAddress:    "198.51.100.1",
		Protocol:      "tcp",
		L3Proto:       "ipv4",
		DPIApp:        "tls",
		DPICategory:   "web",
		DPITLSSNI:     "blocked.example",
		DPIConfidence: 90,
	}
	if err := log.Record(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	rows, err := log.List(context.Background(), FirewallLogFilter{Action: "drop", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0].DPIApp != "tls" || rows[0].DPITLSSNI != "blocked.example" || rows[0].DPIConfidence != 90 {
		t.Fatalf("dpi fields = %#v", rows[0])
	}
}

func TestFirewallLogDenyTimelineAggregatesBeyondListLimit(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 250; i++ {
		if err := log.Record(context.Background(), FirewallLogEntry{
			Timestamp:  now.Add(-23 * time.Hour).Add(time.Duration(i%30) * time.Minute),
			Action:     "drop",
			SrcAddress: "172.18.0.10",
			DstAddress: "198.51.100.1",
			Protocol:   "tcp",
			L3Proto:    "ipv4",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Record(context.Background(), FirewallLogEntry{
		Timestamp:  now.Add(-22 * time.Hour),
		Action:     "accept",
		SrcAddress: "172.18.0.10",
		DstAddress: "198.51.100.2",
		Protocol:   "tcp",
		L3Proto:    "ipv4",
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := log.List(context.Background(), FirewallLogFilter{Since: now.Add(-24 * time.Hour), Action: "drop", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 200 {
		t.Fatalf("list rows = %d, want capped 200", len(rows))
	}
	timeline, err := log.DenyTimeline(context.Background(), now.Add(-24*time.Hour), now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 24 {
		t.Fatalf("timeline buckets = %d, want 24", len(timeline))
	}
	total := 0
	nonzero := false
	for _, bucket := range timeline {
		total += bucket.Count
		if bucket.Start.Equal(now.Add(-23*time.Hour)) && bucket.Count == 250 {
			nonzero = true
		}
	}
	if total != 250 || !nonzero {
		t.Fatalf("timeline total=%d nonzero=%v rows=%+v", total, nonzero, timeline)
	}
}

func TestFirewallLogExpiredReturnLookup(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	flow := ExpiredFlowEntry{
		Timestamp:    now.Add(-2 * time.Minute),
		L3Proto:      "ipv4",
		Protocol:     "tcp",
		OrigSrc:      "172.18.0.10",
		OrigSrcPort:  53168,
		OrigDst:      "198.51.100.10",
		OrigDstPort:  443,
		ReplySrc:     "198.51.100.10",
		ReplySrcPort: 443,
		ReplyDst:     "172.18.0.10",
		ReplyDstPort: 53168,
		Bytes:        12345,
	}
	if err := log.RecordExpiredFlow(context.Background(), flow, time.Hour, 100000); err != nil {
		t.Fatal(err)
	}
	match, ok, err := log.FindExpiredReturn(context.Background(), FirewallLogEntry{
		Action:     "drop",
		SrcAddress: "198.51.100.10",
		SrcPort:    443,
		DstAddress: "172.18.0.10",
		DstPort:    53168,
		Protocol:   "tcp",
		L3Proto:    "ipv4",
	}, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || match.OrigSrc != "172.18.0.10" || match.Bytes != 12345 {
		t.Fatalf("match ok=%v flow=%+v", ok, match)
	}
}

func TestFirewallLogDPIFlowLookupDirectAndReverse(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	if err := log.RecordDPIFlow(context.Background(), DPIFlowEntry{
		FirstSeen:     now.Add(-2 * time.Minute),
		LastSeen:      now.Add(-30 * time.Second),
		L3Proto:       "ipv4",
		Protocol:      "tcp",
		SrcAddress:    "172.18.0.10",
		SrcPort:       53168,
		DstAddress:    "198.51.100.10",
		DstPort:       443,
		AppName:       "tls",
		AppCategory:   "web",
		AppConfidence: 90,
		TLSSNI:        "cached.example",
	}, time.Hour, 100000); err != nil {
		t.Fatal(err)
	}
	flow, ok, err := log.FindDPIFlowForFirewallEntry(context.Background(), FirewallLogEntry{
		Action:     "drop",
		SrcAddress: "198.51.100.10",
		SrcPort:    443,
		DstAddress: "172.18.0.10",
		DstPort:    53168,
		Protocol:   "tcp",
		L3Proto:    "ipv4",
	}, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || flow.TLSSNI != "cached.example" || flow.AppName != "tls" {
		t.Fatalf("flow ok=%v flow=%+v", ok, flow)
	}
	flows, err := log.ListDPIFlows(context.Background(), DPIFlowFilter{Since: now.Add(-time.Hour), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].AppName != "tls" {
		t.Fatalf("dpi flows = %+v", flows)
	}
}

func TestFirewallLogListExpiredFlows(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	if err := log.RecordExpiredFlow(context.Background(), ExpiredFlowEntry{
		Timestamp:    now.Add(-time.Minute),
		Protocol:     "udp",
		OrigSrc:      "172.18.0.10",
		OrigSrcPort:  53000,
		OrigDst:      "198.51.100.10",
		OrigDstPort:  3478,
		ReplySrc:     "198.51.100.10",
		ReplySrcPort: 3478,
		ReplyDst:     "172.18.0.10",
		ReplyDstPort: 53000,
	}, time.Hour, 100000); err != nil {
		t.Fatal(err)
	}
	flows, err := log.ListExpiredFlows(context.Background(), ExpiredFlowFilter{Since: now.Add(-time.Hour), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].Protocol != "udp" {
		t.Fatalf("expired flows = %+v", flows)
	}
}
