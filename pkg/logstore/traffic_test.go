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

func TestOpenTrafficFlowLogConfiguresWALCheckpoint(t *testing.T) {
	log, err := OpenTrafficFlowLog(filepath.Join(t.TempDir(), "traffic-flows.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	var autoCheckpoint int
	if err := log.db.QueryRow(`PRAGMA wal_autocheckpoint`).Scan(&autoCheckpoint); err != nil {
		t.Fatal(err)
	}
	if autoCheckpoint != trafficFlowWALAutoCheckpointPages {
		t.Fatalf("wal_autocheckpoint = %d, want %d", autoCheckpoint, trafficFlowWALAutoCheckpointPages)
	}

	var synchronous int
	if err := log.db.QueryRow(`PRAGMA synchronous`).Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 {
		t.Fatalf("synchronous = %d, want NORMAL", synchronous)
	}

	var journalSizeLimit int64
	if err := log.db.QueryRow(`PRAGMA journal_size_limit`).Scan(&journalSizeLimit); err != nil {
		t.Fatal(err)
	}
	if journalSizeLimit != trafficFlowJournalSizeLimitBytes {
		t.Fatalf("journal_size_limit = %d, want %d", journalSizeLimit, trafficFlowJournalSizeLimitBytes)
	}
}

func TestTrafficFlowLogFiltersAndAggregate(t *testing.T) {
	log, err := OpenTrafficFlowLog(filepath.Join(t.TempDir(), "traffic-flows.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	flows := []TrafficFlow{
		{StartedAt: base, ClientAddress: "192.168.1.10", ClientPort: 10001, PeerAddress: "8.8.8.8", PeerPort: 443, Protocol: "tcp", BytesOut: 100, BytesIn: 200, ResolvedHostname: "dns.google"},
		{StartedAt: base.Add(10 * time.Second), ClientAddress: "192.168.1.10", ClientPort: 10002, PeerAddress: "1.1.1.1", PeerPort: 443, Protocol: "tcp", BytesOut: 50, BytesIn: 0, ResolvedHostname: "one.one.one.one"},
		{StartedAt: base.Add(20 * time.Second), ClientAddress: "192.168.1.11", ClientPort: 10003, PeerAddress: "9.9.9.9", PeerPort: 53, Protocol: "udp", BytesOut: 60, BytesIn: 80, ResolvedHostname: "dns.quad9.net"},
		{StartedAt: base.Add(30 * time.Second), ClientAddress: "192.168.1.11", ClientPort: 10004, PeerAddress: "8.8.4.4", PeerPort: 443, Protocol: "tcp", BytesOut: 0, BytesIn: 90, ResolvedHostname: "dns.google"},
	}
	for _, f := range flows {
		f.FlowKey = FlowKey(f.Protocol, f.ClientAddress, f.ClientPort, f.PeerAddress, f.PeerPort)
		if err := log.UpsertActive(context.Background(), f); err != nil {
			t.Fatal(err)
		}
	}

	// Until filter
	rows, err := log.List(context.Background(), TrafficFlowFilter{Since: base.Add(-time.Minute), Until: base.Add(15 * time.Second), Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("until expected 2, got %d", len(rows))
	}

	// Protocol filter
	rows, err = log.List(context.Background(), TrafficFlowFilter{Since: base.Add(-time.Minute), Protocol: "udp", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Protocol != "udp" {
		t.Fatalf("udp filter rows=%#v", rows)
	}

	// Asymmetric filter (rx==0 OR tx==0)
	rows, err = log.List(context.Background(), TrafficFlowFilter{Since: base.Add(-time.Minute), Asymmetric: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("asymmetric expected 2, got %d (rows=%#v)", len(rows), rows)
	}

	// PeerSuffix matches against peer_address OR resolved_hostname.
	rows, err = log.List(context.Background(), TrafficFlowFilter{Since: base.Add(-time.Minute), PeerSuffix: "dns.google", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("peer-suffix expected 2, got %d", len(rows))
	}

	// Aggregate
	agg, err := log.Aggregate(context.Background(), TrafficFlowFilter{Since: base.Add(-time.Minute), Until: base.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if agg.Total != 4 {
		t.Fatalf("total = %d", agg.Total)
	}
	if agg.TotalBytesIn != 370 || agg.TotalBytesOut != 210 {
		t.Fatalf("bytes = (in=%d, out=%d)", agg.TotalBytesIn, agg.TotalBytesOut)
	}
	if agg.ByClient["192.168.1.10"] != 2 || agg.ByClient["192.168.1.11"] != 2 {
		t.Fatalf("ByClient = %#v", agg.ByClient)
	}
	if agg.ByProtocol["tcp"] != 3 || agg.ByProtocol["udp"] != 1 {
		t.Fatalf("ByProtocol = %#v", agg.ByProtocol)
	}
}

func TestTrafficFlowLogLimitHardCap(t *testing.T) {
	log, err := OpenTrafficFlowLog(filepath.Join(t.TempDir(), "traffic-flows.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		f := TrafficFlow{StartedAt: base.Add(time.Duration(i) * time.Second), ClientAddress: "192.168.1.10", ClientPort: 10000 + i, PeerAddress: "8.8.8.8", PeerPort: 443, Protocol: "tcp"}
		f.FlowKey = FlowKey(f.Protocol, f.ClientAddress, f.ClientPort, f.PeerAddress, f.PeerPort)
		if err := log.UpsertActive(context.Background(), f); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := log.List(context.Background(), TrafficFlowFilter{Since: base.Add(-time.Minute), Limit: TrafficFlowFilterLimitMax + 5000})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
}

func TestTrafficFlowLogUpsertAndEndMissing(t *testing.T) {
	log, err := OpenTrafficFlowLog(filepath.Join(t.TempDir(), "traffic-flows.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	flow := TrafficFlow{
		StartedAt:           time.Now().UTC(),
		ClientAddress:       "172.18.0.10",
		ClientPort:          12345,
		PeerAddress:         "1.1.1.1",
		PeerPort:            443,
		Protocol:            "tcp",
		Accounting:          true,
		BytesOut:            100,
		AppName:             "tls",
		AppCategory:         "web",
		AppConfidence:       95,
		DetectedProtocol:    "tls",
		ApplicationProtocol: "tls",
		Category:            "web",
		Confidence:          95,
		Metadata:            map[string]string{"tls.sni": "routerd.example"},
		Engine:              "ndpi-agent",
		Source:              "ndpi-agent",
		TLSSNI:              "routerd.example",
		HTTPHost:            "routerd.example",
	}
	flow.FlowKey = FlowKey(flow.Protocol, flow.ClientAddress, flow.ClientPort, flow.PeerAddress, flow.PeerPort)
	if err := log.UpsertActive(context.Background(), flow); err != nil {
		t.Fatal(err)
	}
	flow.BytesOut = 200
	flow.AppName = ""
	flow.AppCategory = ""
	flow.AppConfidence = 0
	flow.DetectedProtocol = ""
	flow.ApplicationProtocol = ""
	flow.Category = ""
	flow.Confidence = 0
	flow.Metadata = nil
	flow.Engine = ""
	flow.Source = ""
	flow.TLSSNI = ""
	flow.HTTPHost = ""
	flow.DNSQuery = ""
	flow.ResolvedHostname = ""
	if err := log.UpsertActive(context.Background(), flow); err != nil {
		t.Fatal(err)
	}
	if err := log.EndMissing(context.Background(), nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	rows, err := log.List(context.Background(), TrafficFlowFilter{Client: "172.18.0.10", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d", len(rows))
	}
	if !rows[0].Accounting || rows[0].BytesOut != 200 || rows[0].EndedAt.IsZero() {
		t.Fatalf("flow = %#v", rows[0])
	}
	if rows[0].AppName != "tls" || rows[0].AppCategory != "web" || rows[0].AppConfidence != 95 || rows[0].Engine != "ndpi-agent" || rows[0].Source != "ndpi-agent" || rows[0].TLSSNI != "routerd.example" || rows[0].HTTPHost != "routerd.example" {
		t.Fatalf("dpi fields were not preserved: %#v", rows[0])
	}
	if rows[0].DetectedProtocol != "tls" || rows[0].ApplicationProtocol != "tls" || rows[0].Category != "web" || rows[0].Confidence != 95 || rows[0].Metadata["tls.sni"] != "routerd.example" {
		t.Fatalf("typed dpi fields were not preserved: %#v", rows[0])
	}
}

func TestTrafficFlowLogSyncActiveBatchesUpsertAndEndMissing(t *testing.T) {
	log, err := OpenTrafficFlowLog(filepath.Join(t.TempDir(), "traffic-flows.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Now().UTC()
	first := TrafficFlow{
		StartedAt:     now.Add(-time.Minute),
		ClientAddress: "172.18.0.10",
		ClientPort:    12345,
		PeerAddress:   "1.1.1.1",
		PeerPort:      443,
		Protocol:      "tcp",
		BytesOut:      100,
	}
	first.FlowKey = FlowKey(first.Protocol, first.ClientAddress, first.ClientPort, first.PeerAddress, first.PeerPort)
	second := TrafficFlow{
		StartedAt:     now.Add(-time.Minute),
		ClientAddress: "172.18.0.11",
		ClientPort:    23456,
		PeerAddress:   "8.8.8.8",
		PeerPort:      53,
		Protocol:      "udp",
		BytesOut:      50,
	}
	second.FlowKey = FlowKey(second.Protocol, second.ClientAddress, second.ClientPort, second.PeerAddress, second.PeerPort)
	if err := log.SyncActive(context.Background(), []TrafficFlow{first, second}, []string{first.FlowKey, second.FlowKey}, now); err != nil {
		t.Fatal(err)
	}
	first.BytesOut = 200
	if err := log.SyncActive(context.Background(), []TrafficFlow{first}, []string{first.FlowKey}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	rows, err := log.List(context.Background(), TrafficFlowFilter{Since: now.Add(-time.Hour), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	byKey := map[string]TrafficFlow{}
	for _, row := range rows {
		byKey[row.FlowKey] = row
	}
	if byKey[first.FlowKey].BytesOut != 200 || !byKey[first.FlowKey].EndedAt.IsZero() {
		t.Fatalf("first flow = %#v", byKey[first.FlowKey])
	}
	if byKey[second.FlowKey].EndedAt.IsZero() {
		t.Fatalf("second flow should be ended: %#v", byKey[second.FlowKey])
	}
}

func TestTrafficFlowLogMigratesSourceColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic-flows.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE flows (
  id INTEGER PRIMARY KEY,
  flow_key TEXT UNIQUE NOT NULL,
  ts_started INTEGER NOT NULL,
  ts_ended INTEGER,
  client_address TEXT,
  client_port INTEGER,
  peer_address TEXT,
  peer_port INTEGER,
  protocol TEXT NOT NULL,
  nat_translated_address TEXT,
  accounting INTEGER,
  bytes_out INTEGER,
  bytes_in INTEGER,
  packets_out INTEGER,
  packets_in INTEGER,
  app_name TEXT,
  app_category TEXT,
  app_confidence INTEGER,
  tls_sni TEXT,
  http_host TEXT,
  dns_query TEXT,
  resolved_hostname TEXT
)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := OpenTrafficFlowLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	flow := TrafficFlow{
		StartedAt:     time.Now().UTC(),
		ClientAddress: "172.18.0.10",
		ClientPort:    12345,
		PeerAddress:   "1.1.1.1",
		PeerPort:      443,
		Protocol:      "tcp",
		AppName:       "tls",
		Engine:        "ndpi-agent",
		Source:        "ndpi-agent",
	}
	if err := log.UpsertActive(context.Background(), flow); err != nil {
		t.Fatal(err)
	}
	rows, err := log.List(context.Background(), TrafficFlowFilter{Client: "172.18.0.10", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Engine != "ndpi-agent" || rows[0].Source != "ndpi-agent" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestTrafficFlowLogReadOnlyToleratesLegacyColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic-flows.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := db.Exec(`
CREATE TABLE flows (
  id INTEGER PRIMARY KEY,
  flow_key TEXT UNIQUE NOT NULL,
  ts_started INTEGER NOT NULL,
  ts_ended INTEGER,
  client_address TEXT,
  client_port INTEGER,
  peer_address TEXT,
  peer_port INTEGER,
  protocol TEXT NOT NULL,
  nat_translated_address TEXT,
  bytes_out INTEGER,
  bytes_in INTEGER,
  packets_out INTEGER,
  packets_in INTEGER,
  app_name TEXT,
  app_category TEXT,
  app_confidence INTEGER,
  tls_sni TEXT,
  resolved_hostname TEXT
);
INSERT INTO flows(flow_key,ts_started,client_address,client_port,peer_address,peer_port,protocol,bytes_out,app_name,tls_sni)
VALUES('legacy',?,?,?,?,?,?,?,?,?)`, now.UnixNano(), "172.18.0.10", 12345, "1.1.1.1", 443, "tcp", 100, "tls", "legacy.example"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	log, err := OpenTrafficFlowLogReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	rows, err := log.List(context.Background(), TrafficFlowFilter{Client: "172.18.0.10", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AppName != "tls" || rows[0].TLSSNI != "legacy.example" || rows[0].Engine != "" || rows[0].ApplicationProtocol != "" {
		t.Fatalf("rows = %#v", rows)
	}
}
