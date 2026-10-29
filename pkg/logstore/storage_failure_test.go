// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetentionRejectsUnreadableSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyRetention(context.Background(), RetentionTarget{File: path, Retention: time.Hour}, false)
	if err == nil || result.Skipped {
		t.Fatalf("unreadable existing database reported as retained: result=%+v error=%v", result, err)
	}
}

func TestReadOnlyLogUsesLiteralFilesystemPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries?#%25.db")
	writer, err := OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Record(context.Background(), DNSQuery{Timestamp: time.Now().Add(-2 * time.Hour), QuestionName: "literal.example", QuestionType: "A", ClientAddress: "192.0.2.1"}); err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := OpenDNSQueryLogReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rows, err := reader.List(context.Background(), DNSQueryFilter{})
	if err != nil || len(rows) != 1 || rows[0].QuestionName != "literal.example" {
		t.Fatalf("literal database path: rows=%+v error=%v", rows, err)
	}
	if err := reader.Record(context.Background(), DNSQuery{QuestionName: "must-not-write.example"}); err == nil {
		t.Fatal("read-only connection accepted a write")
	}
	result, err := ApplyRetention(context.Background(), RetentionTarget{File: path, Retention: time.Hour, Signals: []string{RetentionSignalDNSQueries}}, false)
	if err != nil || result.Deleted != 1 {
		t.Fatalf("literal path retention: result=%+v error=%v", result, err)
	}
}

func TestTrafficReadOnlyRequiresCurrentSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "traffic.db")
	writer, err := OpenTrafficFlowLog(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	flow := TrafficFlow{Protocol: "tcp", ClientAddress: "192.0.2.2", ClientPort: 1000, PeerAddress: "198.51.100.1", PeerPort: 443, Engine: "current-engine", Source: "current-source", Metadata: map[string]string{"tls.sni": "current.example"}}
	if err := writer.UpsertActive(ctx, flow); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenTrafficFlowLogReadOnly(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rows, err := reader.List(ctx, TrafficFlowFilter{})
	if err != nil || len(rows) != 1 || rows[0].Engine != flow.Engine || rows[0].Source != flow.Source || rows[0].Metadata["tls.sni"] != "current.example" {
		t.Fatalf("current schema read: rows=%+v error=%v", rows, err)
	}
	if _, err := writer.db.ExecContext(ctx, "ALTER TABLE flows DROP COLUMN engine"); err != nil {
		t.Fatal(err)
	}
	if rows, err := reader.List(ctx, TrafficFlowFilter{}); err == nil {
		t.Fatalf("missing required column became empty engine: %+v", rows)
	}
	if err := writer.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := writer.UpsertActive(ctx, flow); err == nil {
		t.Fatal("Init repaired a discarded database schema")
	}
}

func TestDPIReadOnlyRequiresCurrentSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "firewall.db")
	writer, err := OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	now := time.Now().UTC()
	flow := DPIFlowEntry{FirstSeen: now, LastSeen: now, Protocol: "tcp", SrcAddress: "192.0.2.2", SrcPort: 1000, DstAddress: "198.51.100.1", DstPort: 443, AppName: "tls", Engine: "current-engine", Source: "current-source", Metadata: map[string]string{"tls.sni": "current.example"}}
	if err := writer.RecordDPIFlow(ctx, flow, time.Hour, 1000); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenFirewallLogReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entry := FirewallLogEntry{Protocol: flow.Protocol, SrcAddress: flow.SrcAddress, SrcPort: flow.SrcPort, DstAddress: flow.DstAddress, DstPort: flow.DstPort}
	rows, err := reader.ListDPIFlows(ctx, DPIFlowFilter{})
	if err != nil || len(rows) != 1 || rows[0].Engine != flow.Engine || rows[0].Source != flow.Source || rows[0].Metadata["tls.sni"] != "current.example" {
		t.Fatalf("current schema read: rows=%+v error=%v", rows, err)
	}
	if found, ok, err := reader.FindDPIFlowForFirewallEntry(ctx, entry, now, time.Hour); err != nil || !ok || found.Engine != flow.Engine {
		t.Fatalf("current schema lookup: found=%+v ok=%v error=%v", found, ok, err)
	}
	if _, err := writer.db.ExecContext(ctx, "ALTER TABLE dpi_flow DROP COLUMN engine"); err != nil {
		t.Fatal(err)
	}
	if rows, err := reader.ListDPIFlows(ctx, DPIFlowFilter{}); err == nil {
		t.Fatalf("missing required column became empty engine: %+v", rows)
	}
	if found, ok, err := reader.FindDPIFlowForFirewallEntry(ctx, entry, now, time.Hour); err == nil {
		t.Fatalf("missing required column became successful lookup: found=%+v ok=%v", found, ok)
	}
	if err := writer.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := writer.RecordDPIFlow(ctx, flow, time.Hour, 1000); err == nil {
		t.Fatal("Init repaired a discarded database schema")
	}
}

func TestDNSQueryReadRejectsMalformedStoredAnswers(t *testing.T) {
	ctx := context.Background()
	log, err := OpenDNSQueryLog(filepath.Join(t.TempDir(), "dns.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if err := log.Record(ctx, DNSQuery{QuestionName: "example.test", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	for _, valid := range []string{"null", "[]"} {
		if _, err := log.db.ExecContext(ctx, "UPDATE dns_queries SET answers_json=?", valid); err != nil {
			t.Fatal(err)
		}
		if rows, err := log.List(ctx, DNSQueryFilter{}); err != nil || len(rows) != 1 || len(rows[0].Answers) != 0 {
			t.Fatalf("valid empty answers: rows=%+v error=%v", rows, err)
		}
	}
	if _, err := log.db.ExecContext(ctx, "UPDATE dns_queries SET answers_json='['"); err != nil {
		t.Fatal(err)
	}
	if rows, err := log.List(ctx, DNSQueryFilter{}); err == nil {
		t.Fatalf("corrupt answers became empty success: %+v", rows)
	}
}

func TestFlowReadsRejectMalformedStoredJSON(t *testing.T) {
	ctx := context.Background()
	traffic, err := OpenTrafficFlowLog(t.Context(), filepath.Join(t.TempDir(), "traffic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer traffic.Close()
	if err := traffic.UpsertActive(ctx, TrafficFlow{Protocol: "tcp", ClientAddress: "192.0.2.2", ClientPort: 1000, PeerAddress: "198.51.100.1", PeerPort: 443}); err != nil {
		t.Fatal(err)
	}
	firewall, err := OpenFirewallLog(filepath.Join(t.TempDir(), "firewall.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer firewall.Close()
	now := time.Now().UTC()
	flow := DPIFlowEntry{FirstSeen: now, LastSeen: now, Protocol: "tcp", SrcAddress: "192.0.2.2", SrcPort: 1000, DstAddress: "198.51.100.1", DstPort: 443, AppName: "tls"}
	if err := firewall.RecordDPIFlow(ctx, flow, time.Hour, 1000); err != nil {
		t.Fatal(err)
	}
	entry := FirewallLogEntry{Protocol: flow.Protocol, SrcAddress: flow.SrcAddress, SrcPort: flow.SrcPort, DstAddress: flow.DstAddress, DstPort: flow.DstPort}
	if _, err := traffic.db.ExecContext(ctx, "UPDATE flows SET risk='null',metadata_json='null'"); err != nil {
		t.Fatal(err)
	}
	if rows, err := traffic.List(ctx, TrafficFlowFilter{}); err != nil || len(rows) != 1 || len(rows[0].Risk)+len(rows[0].Metadata) != 0 {
		t.Fatalf("valid null traffic fields: rows=%+v error=%v", rows, err)
	}
	if _, err := firewall.db.ExecContext(ctx, "UPDATE dpi_flow SET risk='null',metadata_json='null'"); err != nil {
		t.Fatal(err)
	}
	if rows, err := firewall.ListDPIFlows(ctx, DPIFlowFilter{}); err != nil || len(rows) != 1 || len(rows[0].Risk)+len(rows[0].Metadata) != 0 {
		t.Fatalf("valid null DPI fields: rows=%+v error=%v", rows, err)
	}
	for _, column := range []string{"risk", "metadata_json"} {
		if _, err := traffic.db.ExecContext(ctx, "UPDATE flows SET risk='null',metadata_json='null'"); err != nil {
			t.Fatal(err)
		}
		if _, err := traffic.db.ExecContext(ctx, "UPDATE flows SET "+column+"='{' "); err != nil {
			t.Fatal(err)
		}
		if rows, err := traffic.List(ctx, TrafficFlowFilter{}); err == nil {
			t.Fatalf("corrupt traffic %s became empty success: %+v", column, rows)
		}
		if _, err := firewall.db.ExecContext(ctx, "UPDATE dpi_flow SET risk='null',metadata_json='null'"); err != nil {
			t.Fatal(err)
		}
		if _, err := firewall.db.ExecContext(ctx, "UPDATE dpi_flow SET "+column+"='{' "); err != nil {
			t.Fatal(err)
		}
		if rows, err := firewall.ListDPIFlows(ctx, DPIFlowFilter{}); err == nil {
			t.Fatalf("corrupt DPI %s became empty success: %+v", column, rows)
		}
		if found, ok, err := firewall.FindDPIFlowForFirewallEntry(ctx, entry, now, time.Hour); err == nil {
			t.Fatalf("corrupt DPI %s became successful lookup: found=%+v ok=%v", column, found, ok)
		}
	}
}
