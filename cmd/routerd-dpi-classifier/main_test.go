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

	"routerd/pkg/dpi"
)

func TestSelftestClassifiesTLS(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"selftest", "--ndpi-reader", "definitely-not-installed-routerd-ndpi"}, &out); err != nil {
		t.Fatal(err)
	}
	var resp classifyResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TLSSNI != "routerd-dpi-selftest.example" {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Engine != "builtin" || resp.Source != "builtin" {
		t.Fatalf("engine/source = %+v", resp)
	}
	if resp.ApplicationProtocol != "tls" || resp.Category != "web" || resp.Confidence != 90 || resp.Metadata["tls.sni"] != "routerd-dpi-selftest.example" {
		t.Fatalf("typed fields = %+v metadata=%+v", resp.ClassifyResult, resp.Metadata)
	}
}

func TestNDPIReaderAvailabilityDoesNotChangeClassification(t *testing.T) {
	dir := t.TempDir()
	reader := filepath.Join(dir, "ndpiReader")
	if err := os.WriteFile(reader, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := run([]string{"selftest", "--ndpi-reader", "ndpiReader"}, &out); err != nil {
		t.Fatal(err)
	}
	var resp classifyResponse
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TLSSNI != "routerd-dpi-selftest.example" {
		t.Fatalf("response = %+v", resp)
	}
	if resp.Engine != "builtin" || resp.Source != "builtin" {
		t.Fatalf("engine/source = %+v", resp)
	}
}

func TestClassifyCommandReadsJSON(t *testing.T) {
	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example")}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runClassify(nil, bytes.NewReader(data), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"tlsSNI":"routerd.example"`) {
		t.Fatalf("output = %s", out.String())
	}
}

func TestDaemonServesUnixSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "classifier.sock")
	opts := options{socket: socket, name: "test", ndpiReader: "definitely-not-installed-routerd-ndpi", timeout: time.Second}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: newHandler(opts)}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	client := unixHTTPClient(socket, time.Second)
	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example")}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post("http://unix/v1/classify", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got classifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TLSSNI != "routerd.example" {
		t.Fatalf("response = %+v", got)
	}
}

func TestDaemonStatusReportsClassifierStats(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "classifier.sock")
	opts := options{socket: socket, name: "test", engine: "builtin", timeout: time.Second}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: newHandler(opts)}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	client := unixHTTPClient(socket, time.Second)
	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example"), TransportProtocol: "tcp", DstPort: 443}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post("http://unix/v1/classify", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	statusResp, err := client.Get("http://unix/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var got statusResponse
	if err := json.NewDecoder(statusResp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Stats.Requests != 1 || got.Stats.BuiltinPackets != 1 || got.Stats.BuiltinClassified != 1 {
		t.Fatalf("status = %+v", got)
	}
}

func TestAutoEngineFallsBackWhenAgentUnavailable(t *testing.T) {
	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example"), TransportProtocol: "tcp", DstPort: 443}
	got := classifyWithFallback(context.Background(), options{
		engine:          "auto",
		ndpiAgentSocket: filepath.Join(t.TempDir(), "missing.sock"),
		timeout:         10 * time.Millisecond,
	}, req)
	if got.TLSSNI != "routerd.example" || got.Engine != "builtin" || got.Source != "builtin" || got.FallbackReason == "" {
		t.Fatalf("response = %+v", got)
	}
}

func TestAutoEngineFallsBackWhenAgentTimesOut(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/observe-packet", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(dpi.ClassifyResult{
			AppName: "tls",
			Engine:  "ndpi-agent",
			Source:  "ndpi-agent",
		})
	})
	server := &http.Server{Handler: mux}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example"), TransportProtocol: "tcp", DstPort: 443}
	got := classifyWithFallback(context.Background(), options{
		engine:          "auto",
		ndpiAgentSocket: socket,
		timeout:         time.Millisecond,
	}, req)
	if got.TLSSNI != "routerd.example" || got.Engine != "builtin" || got.Source != "builtin" || got.FallbackReason == "" {
		t.Fatalf("response = %+v", got)
	}
}

func TestAutoEngineTimeoutUpdatesStats(t *testing.T) {
	agentSocket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", agentSocket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/observe-packet", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(dpi.ClassifyResult{AppName: "tls", Engine: "ndpi-agent", Source: "ndpi-agent"})
	})
	server := &http.Server{Handler: mux}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	runtime := &classifierRuntime{opts: options{engine: "auto", ndpiAgentSocket: agentSocket, timeout: time.Millisecond}}
	got := runtime.classify(context.Background(), dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example"), TransportProtocol: "tcp", DstPort: 443})
	if got.FallbackReason == "" || got.Engine != "builtin" {
		t.Fatalf("response = %+v", got)
	}
	status := runtime.status()
	if status.Stats.Requests != 1 || status.Stats.AgentPackets != 1 || status.Stats.Fallbacks != 1 || status.Stats.TimeoutErrors != 1 || status.Stats.AgentErrors != 1 {
		t.Fatalf("stats = %+v", status.Stats)
	}
}

func TestAutoEngineUsesAgentResult(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/observe-packet", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(dpi.ClassifyResult{
			AppName:       "tls",
			AppCategory:   "web",
			AppConfidence: 99,
			TLSSNI:        "agent.example",
			Engine:        "ndpi-agent",
			Source:        "ndpi-agent",
		})
	})
	server := &http.Server{Handler: mux}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example"), TransportProtocol: "tcp", DstPort: 443}
	got := classifyWithFallback(context.Background(), options{
		engine:          "auto",
		ndpiAgentSocket: socket,
		timeout:         time.Second,
	}, req)
	if got.TLSSNI != "agent.example" || got.Engine != "ndpi-agent" || got.Source != "ndpi-agent" || got.FallbackReason != "" {
		t.Fatalf("response = %+v", got)
	}
}

func TestAutoEnginePreservesBuiltinPayloadHints(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/observe-packet", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(dpi.ClassifyResult{
			AppName:       "tls",
			AppCategory:   "web",
			AppConfidence: 95,
			Engine:        "ndpi-agent",
			Source:        "ndpi-agent",
		})
	})
	server := &http.Server{Handler: mux}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	req := dpi.ClassifyRequest{L4Payload: dpi.MinimalTLSClientHello("routerd.example"), TransportProtocol: "tcp", DstPort: 443}
	got := classifyWithFallback(context.Background(), options{
		engine:          "auto",
		ndpiAgentSocket: socket,
		timeout:         time.Second,
	}, req)
	if got.TLSSNI != "routerd.example" || got.Engine != "ndpi-agent" || got.Source != "ndpi-agent" {
		t.Fatalf("response = %+v", got)
	}
}

func TestStatusKeepsBuiltinActiveWhenAgentHasNoLibNDPI(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":            true,
			"libndpiLoaded": false,
			"reason":        "libndpi backend is not enabled in this build",
		})
	})
	server := &http.Server{Handler: mux}
	defer server.Shutdown(context.Background())
	go server.Serve(listener)

	got := status(options{engine: "auto", ndpiAgentSocket: socket, timeout: time.Second})
	if got.ActiveEngine != "builtin" || got.Agent == nil || got.Agent.Available {
		t.Fatalf("status = %+v agent=%+v", got, got.Agent)
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
