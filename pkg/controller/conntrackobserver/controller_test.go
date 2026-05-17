// SPDX-License-Identifier: BSD-3-Clause

package conntrackobserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/conntrack"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"
)

type testStore struct {
	status map[string]map[string]any
}

func TestControllerRecordsTrafficFlowLog(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	maxPath := filepath.Join(dir, "max")
	entriesPath := filepath.Join(dir, "entries")
	flowPath := filepath.Join(dir, "traffic-flows.db")
	firewallPath := filepath.Join(dir, "firewall-logs.db")
	if err := os.WriteFile(countPath, []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maxPath, []byte("10\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entriesPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	store := &testStore{}
	controller := &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TrafficFlowLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.TrafficFlowLogSpec{Enabled: true, Path: flowPath, Source: "conntrack"},
		}, {
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.FirewallLogSpec{Enabled: true, Path: firewallPath},
		}}}},
		Bus:   bus.New(),
		Store: store,
		Paths: conntrack.Paths{Entries: entriesPath, Count: countPath, Max: maxPath},
		Connections: func(limit int) (*observe.ConnectionTable, error) {
			return &observe.ConnectionTable{Entries: []observe.ConnectionEntry{{
				Protocol: "tcp",
				Original: observe.ConntrackTuple{Source: "172.18.0.10", SourcePort: "12345", Destination: "1.1.1.1", DestinationPort: "443", Packets: 3, Bytes: 300, Accounting: true},
				Reply:    observe.ConntrackTuple{Source: "1.1.1.1", SourcePort: "443", Destination: "172.18.0.10", DestinationPort: "12345", Packets: 4, Bytes: 1200, Accounting: true},
			}}}, nil
		},
	}
	firewallLog, err := logstore.OpenFirewallLog(firewallPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := firewallLog.RecordDPIFlow(context.Background(), logstore.DPIFlowEntry{
		FirstSeen:           time.Now().UTC().Add(-time.Minute),
		LastSeen:            time.Now().UTC(),
		Protocol:            "tcp",
		SrcAddress:          "172.18.0.10",
		SrcPort:             12345,
		DstAddress:          "1.1.1.1",
		DstPort:             443,
		AppName:             "google",
		AppCategory:         "web",
		AppConfidence:       95,
		DetectedProtocol:    "google",
		ApplicationProtocol: "tls",
		Category:            "web",
		Confidence:          95,
		Metadata:            map[string]string{"tls.sni": "www.google.com"},
		Engine:              "ndpi-agent",
		Source:              "ndpi-agent",
		TLSSNI:              "www.google.com",
		DNSQuery:            "www.google.com",
		ClassifiedAt:        time.Now().UTC(),
		PacketCount:         1,
	}, time.Hour, 100); err != nil {
		t.Fatal(err)
	}
	if err := firewallLog.Close(); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	flowLog, err := logstore.OpenTrafficFlowLog(flowPath)
	if err != nil {
		t.Fatal(err)
	}
	defer flowLog.Close()
	flows, err := flowLog.List(context.Background(), logstore.TrafficFlowFilter{Client: "172.18.0.10", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].PeerAddress != "1.1.1.1" || flows[0].PeerPort != 443 || !flows[0].Accounting || flows[0].BytesOut != 300 || flows[0].BytesIn != 1200 {
		t.Fatalf("flows = %#v", flows)
	}
	if flows[0].AppName != "google" || flows[0].Engine != "ndpi-agent" || flows[0].Source != "ndpi-agent" || flows[0].TLSSNI != "www.google.com" || flows[0].DNSQuery != "www.google.com" {
		t.Fatalf("dpi flow fields missing: %#v", flows[0])
	}
	if flows[0].DetectedProtocol != "google" || flows[0].ApplicationProtocol != "tls" || flows[0].Category != "web" || flows[0].Confidence != 95 || flows[0].Metadata["tls.sni"] != "www.google.com" {
		t.Fatalf("typed dpi flow fields missing: %#v", flows[0])
	}
	if protocol := trafficMetricProtocol(flows[0]); protocol != "tls" {
		t.Fatalf("metric protocol = %q", protocol)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "TrafficFlowLog", "default")
	if status["phase"] != "Observed" || status["activeFlows"] != 1 {
		t.Fatalf("traffic status = %#v", status)
	}
}

func TestTrafficFlowFromConnectionKeepsDPIFields(t *testing.T) {
	flow := trafficFlowFromConnection(observe.ConnectionEntry{
		Protocol:      "tcp",
		AppName:       "tls",
		AppCategory:   "web",
		AppConfidence: 90,
		TLSSNI:        "example.com",
		Original:      observe.ConntrackTuple{Source: "172.18.0.10", SourcePort: "12345", Destination: "93.184.216.34", DestinationPort: "443"},
		Reply:         observe.ConntrackTuple{Source: "93.184.216.34", SourcePort: "443", Destination: "172.18.0.10", DestinationPort: "12345"},
	}, time.Now().UTC())
	if flow.AppName != "tls" || flow.AppCategory != "web" || flow.AppConfidence != 90 || flow.TLSSNI != "example.com" {
		t.Fatalf("dpi fields missing: %#v", flow)
	}
	if protocol := trafficMetricProtocol(flow); protocol != "tls" {
		t.Fatalf("metric protocol = %q", protocol)
	}
}

func TestTrafficMetricProtocolRecognizesTailscalePort(t *testing.T) {
	flow := logstore.TrafficFlow{Protocol: "udp", PeerPort: 41641}
	if protocol := trafficMetricProtocol(flow); protocol != "tailscale" {
		t.Fatalf("metric protocol = %q", protocol)
	}
}

func TestPositiveDeltaHandlesReset(t *testing.T) {
	if got := positiveDelta(1200, 1000); got != 200 {
		t.Fatalf("delta = %d", got)
	}
	if got := positiveDelta(300, 1000); got != 300 {
		t.Fatalf("reset delta = %d", got)
	}
	if got := positiveDelta(0, 1000); got != 0 {
		t.Fatalf("zero delta = %d", got)
	}
}

func (s *testStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.status == nil {
		s.status = map[string]map[string]any{}
	}
	s.status[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s *testStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.status == nil {
		return map[string]any{}
	}
	if status := s.status[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestControllerRecordsStatusWithoutSnapshotEvent(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	maxPath := filepath.Join(dir, "max")
	entriesPath := filepath.Join(dir, "entries")
	if err := os.WriteFile(countPath, []byte("8\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maxPath, []byte("10\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entriesPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	store := &testStore{}
	eventBus := bus.New()
	controller := &Controller{
		Bus:            eventBus,
		Store:          store,
		Paths:          conntrack.Paths{Entries: entriesPath, Count: countPath, Max: maxPath},
		ThresholdRatio: 0.9,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "ConntrackObserver", "default")
	if status["phase"] != "Observed" || status["count"] != 8 || status["max"] != 10 {
		t.Fatalf("status = %#v", status)
	}
	if events := eventBus.Recent("routerd.conntrack.snapshot"); len(events) != 0 {
		t.Fatalf("snapshot events = %#v", events)
	}
}

func TestControllerRecordsUnavailableWhenConntrackProcfsMissing(t *testing.T) {
	dir := t.TempDir()
	store := &testStore{}
	controller := &Controller{
		Bus:   bus.New(),
		Store: store,
		Paths: conntrack.Paths{
			Entries: filepath.Join(dir, "missing-entries"),
			Count:   filepath.Join(dir, "missing-count"),
			Max:     filepath.Join(dir, "missing-max"),
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "ConntrackObserver", "default")
	if status["phase"] != "Unavailable" || status["reason"] != "ConntrackUnavailable" {
		t.Fatalf("status = %#v", status)
	}
	if status["message"] == "" {
		t.Fatalf("missing unavailable message: %#v", status)
	}
}
