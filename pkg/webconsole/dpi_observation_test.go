// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
)

func TestDPIStatusDecoderBoundsActualIPCBody(t *testing.T) {
	for _, body := range []string{`null`, `{"engine":"` + strings.Repeat("x", 64<<10) + `"}`} {
		status := decodeDPIServiceStatus("status.sock", strings.NewReader(body))
		if status.Available || status.Error == "" {
			t.Fatalf("unavailable status became an available service: %+v", status)
		}
	}
	status := decodeDPIServiceStatus("status.sock", strings.NewReader(`{"engine":"classifier","stats":{"classified":0}}`))
	if !status.Available || status.Engine != "classifier" || status.Error != "" || status.Stats["classified"] != float64(0) {
		t.Fatalf("valid zero counter lost: %+v", status)
	}
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	status = probeDPIService(context.Background(), filepath.Join(file, "status.sock"), "/v1/status")
	if status == nil || status.Error == "" || status.Available {
		t.Fatalf("failed native status path read became absent: %+v", status)
	}
}

func TestClientHTTPReportsFailedDPIRead(t *testing.T) {
	dir := t.TempDir()
	ip := filepath.Join(dir, "ip")
	if err := os.WriteFile(ip, []byte("#!/bin/sh\nprintf '[]'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	trafficPath := filepath.Join(dir, "traffic.db")
	traffic, err := logstore.OpenTrafficFlowLog(t.Context(), trafficPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := traffic.UpsertActive(context.Background(), logstore.TrafficFlow{StartedAt: time.Now(), ClientAddress: "192.0.2.2", ClientPort: 49152, PeerAddress: "198.51.100.2", PeerPort: 443, Protocol: "tcp"}); err != nil {
		t.Fatal(err)
	}
	if err := traffic.Close(); err != nil {
		t.Fatal(err)
	}
	firewallPath := filepath.Join(dir, "firewall.db")
	firewall, err := logstore.OpenFirewallLog(firewallPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := firewall.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", firewallPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE dpi_flow"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")
	server := httptest.NewServer(New(Options{
		DHCPLeasePaths: []string{missing}, DHCPStickyLogPath: missing, DHCPFingerprintLogPath: missing,
		TrafficFlowLogPath: trafficPath, DNSQueryLogPath: missing, FirewallLogPath: firewallPath,
	}))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/clients")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "dpi_flow") {
		t.Fatalf("failed enrichment became successful client data: status=%d body=%s", response.StatusCode, body)
	}
}
