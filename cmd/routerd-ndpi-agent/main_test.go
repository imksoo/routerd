// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dpi"
)

func TestSelftestReportsBackendStatus(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"selftest"}, &out); err != nil {
		t.Fatal(err)
	}
	var resp statusResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Engine != "ndpi-agent" || resp.LibNDPILoaded != backendExpectedLoaded() {
		t.Fatalf("response = %+v", resp)
	}
	if resp.OK != backendExpectedLoaded() {
		t.Fatalf("status OK = %v, want backend loaded = %v", resp.OK, backendExpectedLoaded())
	}
	if resp.Selftest == nil || resp.Selftest.Engine != "ndpi-agent" {
		t.Fatalf("selftest = %+v", resp.Selftest)
	}
}

func TestDaemonReturnsUnavailableClassification(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "ndpi.sock")
	opts := options{socket: socket, name: "test", timeout: time.Second}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: newHandler(newAgent(opts, nil))}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	client := unixHTTPClient(socket, time.Second)
	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example"), TransportProtocol: "tcp", DstPort: 443}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post("http://unix/v1/observe-packet", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got dpi.ClassifyResult
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.AppName != "unknown" || got.Engine != "ndpi-agent" || got.Source != "ndpi-agent" {
		t.Fatalf("response = %+v", got)
	}
}

func TestAgentStopsAfterFirstPayloadPacketLimit(t *testing.T) {
	agent := newAgent(options{flowTTL: time.Hour, flowLimit: 100, firstPayloadPackets: 1}, nil)
	req := dpi.ClassifyRequest{
		L4Payload:         []byte{0x01, 0x02, 0x03},
		TransportProtocol: "udp",
		SrcAddress:        "192.0.2.10",
		SrcPort:           50000,
		DstAddress:        "198.51.100.10",
		DstPort:           50001,
	}
	_ = agent.Observe(context.Background(), req, time.Unix(100, 0))
	second := agent.Observe(context.Background(), req, time.Unix(101, 0))
	stats := agent.Status().Stats
	if second.Reason != "flow_packet_limit_reached" {
		t.Fatalf("second = %+v", second)
	}
	if stats.BackendPackets != 1 || stats.SkippedPackets != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestRunDaemonRejectsNonSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(path, []byte("do not remove"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runDaemon([]string{"--socket", path}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "refusing to remove non-socket") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("path should remain: %v", err)
	}
}
