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
