// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestTrafficAPIKeepsOptionalCounterPresenceAndPartialAggregate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	store, err := logstore.OpenTrafficFlowLog(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertActive(t.Context(), logstore.TrafficFlow{FlowKey: "partial", StartedAt: time.Now(), ClientAddress: "192.0.2.1", PeerAddress: "198.51.100.1", Protocol: "tcp", BytesOut: counterFixture(0)}); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{TrafficFlowLogPath: path})
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := http.Get(server.URL + "/api/v1/traffic-flows")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var rows []logstore.TrafficFlow
	if err := json.NewDecoder(response.Body).Decode(&rows); err != nil || len(rows) != 1 || rows[0].BytesOut == nil || *rows[0].BytesOut != 0 || rows[0].BytesIn != nil {
		t.Fatalf("HTTP counter presence: %+v %v", rows, err)
	}
	response, err = http.Get(server.URL + "/api/v1/traffic-flows/aggregate")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var aggregate logstore.TrafficFlowAggregate
	if err := json.NewDecoder(response.Body).Decode(&aggregate); err != nil || aggregate.TotalBytesOut == nil || *aggregate.TotalBytesOut != 0 || aggregate.TotalBytesIn != nil || aggregate.BytesOutIncomplete || !aggregate.BytesInIncomplete {
		t.Fatalf("HTTP aggregate invented completeness: %+v %v", aggregate, err)
	}
	clients := correlateClients(nil, nil, []logstore.TrafficFlow{{ClientAddress: "192.0.2.1", BytesOut: counterFixture(7)}, {ClientAddress: "192.0.2.1", BytesIn: counterFixture(9)}}, nil, nil)
	if len(clients) != 1 || clients[0].BytesOut == nil || *clients[0].BytesOut != 7 || clients[0].BytesIn == nil || *clients[0].BytesIn != 9 || !clients[0].BytesOutIncomplete || !clients[0].BytesInIncomplete {
		t.Fatalf("client merge lost observed sides or claimed complete: %+v", clients)
	}
}

func TestTrafficCounterBrowserRefreshesPresentZeroAndUnknown(t *testing.T) {
	if os.Getenv("ROUTERD_WEB_CONSOLE_CHROMIUM_PATH") == "" {
		t.Skip("set ROUTERD_WEB_CONSOLE_CHROMIUM_PATH for actual traffic-counter browser check")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "traffic.db")
	store, err := logstore.OpenTrafficFlowLog(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var mode atomic.Int32
	flowFor := func(value int32) logstore.TrafficFlow {
		row := logstore.TrafficFlow{FlowKey: "stable-flow", StartedAt: time.Now(), ClientAddress: "192.0.2.1", ClientPort: 1234, PeerAddress: "198.51.100.1", PeerPort: 443, Protocol: "tcp"}
		switch value {
		case 0:
			row.BytesOut = counterFixture(1024)
		case 1:
			row.BytesOut, row.BytesIn = counterFixture(0), counterFixture(0)
		case 3:
			row.BytesOut = counterFixture(2048)
		}
		return row
	}
	writeRows := func(ctx context.Context, value int32) error {
		first := flowFor(value)
		second := flowFor(value)
		second.FlowKey, second.ClientPort = "other-direction", 2234
		if value == 0 {
			second.BytesOut, second.BytesIn = nil, counterFixture(512)
		} else if value == 3 {
			second.BytesOut, second.BytesIn = nil, counterFixture(768)
		}
		if err := store.UpsertActive(ctx, first); err != nil {
			return err
		}
		return store.UpsertActive(ctx, second)
	}
	if err := writeRows(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(directory, "ip")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '[]\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	missing := filepath.Join(directory, "not-configured")
	handler := New(Options{Store: routerstate.NewMemory(), TrafficFlowLogPath: path, DNSQueryLogPath: missing, FirewallLogPath: missing, DHCPFingerprintLogPath: missing, DHCPStickyLogPath: missing, DHCPLeasePaths: []string{missing}, ReverseLookup: func(context.Context, string) ([]string, error) { return nil, nil }, Connections: func(context.Context, int) (*observe.ConnectionTable, error) {
		row := flowFor(mode.Load())
		return &observe.ConnectionTable{Count: 1, Entries: []observe.ConnectionEntry{{Family: "ipv4", Protocol: "tcp", Original: observe.ConntrackTuple{Source: "192.0.2.1", Destination: "198.51.100.1", SourcePort: "1234", DestinationPort: "443", Bytes: row.BytesOut}, Reply: observe.ConntrackTuple{Source: "198.51.100.1", Destination: "192.0.2.1", SourcePort: "443", DestinationPort: "1234", Bytes: row.BytesIn}}}}, nil
	}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/set-counter-mode" {
			next := int32(2)
			if r.URL.Query().Get("value") == "zero" {
				next = 1
			} else if r.URL.Query().Get("value") == "partial-update" {
				next = 3
			}
			if err := writeRows(r.Context(), next); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			mode.Store(next)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "webconsole", "scripts", "check-counter-presence.mjs"), server.URL)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("counter browser: %v\n%s", err, output)
	}
}
