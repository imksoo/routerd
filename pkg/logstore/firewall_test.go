// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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
		FirstSeen:           now.Add(-2 * time.Minute),
		LastSeen:            now.Add(-30 * time.Second),
		L3Proto:             "ipv4",
		Protocol:            "tcp",
		SrcAddress:          "172.18.0.10",
		SrcPort:             53168,
		DstAddress:          "198.51.100.10",
		DstPort:             443,
		AppName:             "tls",
		AppCategory:         "web",
		AppConfidence:       90,
		DetectedProtocol:    "tls",
		ApplicationProtocol: "tls",
		Category:            "web",
		Confidence:          90,
		Metadata:            map[string]string{"tls.sni": "cached.example"},
		Engine:              "ndpi-agent",
		Source:              "ndpi-agent",
		TLSSNI:              "cached.example",
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
	if !ok || flow.TLSSNI != "cached.example" || flow.AppName != "tls" || flow.ApplicationProtocol != "tls" || flow.Category != "web" || flow.Confidence != 90 || flow.Metadata["tls.sni"] != "cached.example" || flow.Engine != "ndpi-agent" || flow.Source != "ndpi-agent" {
		t.Fatalf("flow ok=%v flow=%+v", ok, flow)
	}
	flows, err := log.ListDPIFlows(context.Background(), DPIFlowFilter{Since: now.Add(-time.Hour), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].AppName != "tls" || flows[0].Engine != "ndpi-agent" || flows[0].Source != "ndpi-agent" {
		t.Fatalf("dpi flows = %+v", flows)
	}
}

func TestFirewallLogRecordsUnknownDPIFlow(t *testing.T) {
	log, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall-logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	if err := log.RecordDPIFlow(context.Background(), DPIFlowEntry{
		FirstSeen:     now.Add(-time.Second),
		LastSeen:      now,
		L3Proto:       "ipv4",
		Protocol:      "udp",
		SrcAddress:    "172.18.0.10",
		SrcPort:       53000,
		DstAddress:    "198.51.100.10",
		DstPort:       443,
		AppName:       "unknown",
		Metadata:      map[string]string{"reason": "no_application_signal"},
		Engine:        "builtin",
		Source:        "builtin",
		PacketCount:   1,
		AppConfidence: 0,
	}, time.Hour, 100000); err != nil {
		t.Fatal(err)
	}
	if err := log.RecordDPIFlow(context.Background(), DPIFlowEntry{
		LastSeen:    now.Add(time.Second),
		L3Proto:     "ipv4",
		Protocol:    "udp",
		SrcAddress:  "172.18.0.10",
		SrcPort:     53000,
		DstAddress:  "198.51.100.10",
		DstPort:     443,
		AppName:     "unknown",
		Metadata:    map[string]string{"reason": "no_application_signal"},
		Engine:      "builtin",
		Source:      "builtin",
		PacketCount: 1,
	}, time.Hour, 100000); err != nil {
		t.Fatal(err)
	}
	flow, ok, err := log.FindDPIFlowForFirewallEntry(context.Background(), FirewallLogEntry{
		Protocol:   "udp",
		SrcAddress: "172.18.0.10",
		SrcPort:    53000,
		DstAddress: "198.51.100.10",
		DstPort:    443,
	}, now.Add(time.Second), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || flow.AppName != "unknown" || flow.PacketCount != 2 || !flow.ClassifiedAt.IsZero() || flow.Metadata["reason"] != "no_application_signal" {
		t.Fatalf("flow ok=%v flow=%+v", ok, flow)
	}
}

func TestFirewallLogMigratesDPIFlowSourceColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE firewall_logs (
  id INTEGER PRIMARY KEY,
  ts INTEGER NOT NULL,
  zone_from TEXT,
  zone_to TEXT,
  action TEXT NOT NULL,
  src_address TEXT NOT NULL,
  dst_address TEXT NOT NULL,
  protocol TEXT NOT NULL,
  l3_proto TEXT NOT NULL
);
CREATE TABLE dpi_flow (
  flow_id TEXT PRIMARY KEY,
  ts_first INTEGER NOT NULL,
  ts_last INTEGER NOT NULL,
  l3_proto TEXT,
  protocol TEXT NOT NULL,
  src_address TEXT NOT NULL,
  src_port INTEGER,
  dst_address TEXT NOT NULL,
  dst_port INTEGER,
  app_name TEXT,
  app_category TEXT,
  app_confidence INTEGER,
  tls_sni TEXT,
  http_host TEXT,
  dns_query TEXT,
  classified_at INTEGER,
  packet_count INTEGER
)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	if err := log.RecordDPIFlow(context.Background(), DPIFlowEntry{
		FirstSeen:     now,
		LastSeen:      now,
		Protocol:      "tcp",
		SrcAddress:    "172.18.0.10",
		SrcPort:       12345,
		DstAddress:    "1.1.1.1",
		DstPort:       443,
		AppName:       "tls",
		Engine:        "ndpi-agent",
		Source:        "ndpi-agent",
		ClassifiedAt:  now,
		AppConfidence: 90,
	}, time.Hour, 100000); err != nil {
		t.Fatal(err)
	}
	flows, err := log.ListDPIFlows(context.Background(), DPIFlowFilter{Since: now.Add(-time.Minute), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].Engine != "ndpi-agent" || flows[0].Source != "ndpi-agent" {
		t.Fatalf("flows = %#v", flows)
	}
}

func TestFirewallLogReadOnlyToleratesLegacyDPIFlowColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`
CREATE TABLE dpi_flow (
  flow_id TEXT PRIMARY KEY,
  ts_first INTEGER NOT NULL,
  ts_last INTEGER NOT NULL,
  l3_proto TEXT,
  protocol TEXT NOT NULL,
  src_address TEXT NOT NULL,
  src_port INTEGER,
  dst_address TEXT NOT NULL,
  dst_port INTEGER,
  app_name TEXT,
  app_category TEXT,
  app_confidence INTEGER,
  tls_sni TEXT,
  http_host TEXT,
  dns_query TEXT,
  classified_at INTEGER,
  packet_count INTEGER
);
INSERT INTO dpi_flow(flow_id,ts_first,ts_last,l3_proto,protocol,src_address,src_port,dst_address,dst_port,app_name,app_category,app_confidence,tls_sni,classified_at,packet_count)
VALUES('legacy',?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		now.Add(-time.Minute).UnixNano(), now.UnixNano(), "ipv4", "tcp", "172.18.0.10", 12345, "1.1.1.1", 443, "tls", "web", 90, "legacy.example", now.UnixNano(), 1); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := OpenFirewallLogReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	flow, ok, err := log.FindDPIFlowForFirewallEntry(context.Background(), FirewallLogEntry{Protocol: "tcp", SrcAddress: "172.18.0.10", SrcPort: 12345, DstAddress: "1.1.1.1", DstPort: 443}, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || flow.AppName != "tls" || flow.TLSSNI != "legacy.example" || flow.Engine != "" || flow.ApplicationProtocol != "" {
		t.Fatalf("flow ok=%v flow=%+v", ok, flow)
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
