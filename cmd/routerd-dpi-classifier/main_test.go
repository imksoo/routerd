// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
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
	if resp.TLSSNI != "routerd-dpi-selftest.example" || resp.NDPIToolAvailable {
		t.Fatalf("response = %+v", resp)
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
