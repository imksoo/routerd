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
	opts := options{socket: socket, name: "test", timeout: time.Second}
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
	if got.TLSSNI != "routerd.example" || got.Engine != "builtin" || got.Source != "builtin-payload" || got.FallbackReason == "" {
		t.Fatalf("response = %+v", got)
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
