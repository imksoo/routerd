// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
)

func TestLogHelpersHonorCancellationBeforeOpeningStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	h := New(Options{DNSQueryLogPath: path, TrafficFlowLogPath: path, FirewallLogPath: path})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := map[string]func() error{
		"DNS list":          func() error { _, err := h.queryLogList(ctx, logstore.DNSQueryFilter{}); return err },
		"DNS aggregate":     func() error { _, err := h.queryLogAggregate(ctx, logstore.DNSQueryFilter{}); return err },
		"traffic list":      func() error { _, err := h.trafficFlowList(ctx, logstore.TrafficFlowFilter{}); return err },
		"traffic aggregate": func() error { _, err := h.trafficFlowAggregate(ctx, logstore.TrafficFlowFilter{}); return err },
		"firewall list":     func() error { _, err := h.firewallLogList(ctx, logstore.FirewallLogFilter{}); return err },
		"timeline": func() error {
			_, err := h.firewallDenyTimelineList(ctx, time.Now(), time.Now(), time.Minute)
			return err
		},
		"flow DPI": func() error {
			_, err := h.enrichTrafficFlowsWithDPI(ctx, []logstore.TrafficFlow{{Protocol: "tcp"}}, time.Now(), time.Hour)
			return err
		},
		"connection DPI": func() error {
			return h.enrichConnectionsWithDPI(ctx, &observe.ConnectionTable{Entries: []observe.ConnectionEntry{{Protocol: "tcp"}}}, time.Now(), time.Hour)
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v want context.Canceled", err)
			}
		})
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancelled read created database: %v", err)
	}
}

func TestLogRoutesPropagateRequestCancellation(t *testing.T) {
	h := New(Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, path := range []string{"dns-queries", "dns-queries?agg=1", "dns-queries/aggregate", "traffic-flows", "traffic-flows?agg=1", "traffic-flows/aggregate", "firewall-logs", "firewall/deny-timeline"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/"+path, nil).WithContext(ctx)
			h.ServeHTTP(w, r)
			if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), context.Canceled.Error()) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/summary?connections=-1&events=-1&vpn=0&dhcpLeases=0&resources=0", nil).WithContext(ctx)
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), context.Canceled.Error()) {
		t.Fatalf("summary lost context: %s", w.Body.String())
	}
}

func TestDPIEnrichmentBatchPreservesExistingFieldsAndFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.db")
	store, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.RecordDPIFlow(context.Background(), logstore.DPIFlowEntry{FirstSeen: now, LastSeen: now, Protocol: "tcp", SrcAddress: "10.0.0.2", SrcPort: 12345, DstAddress: "192.0.2.1", DstPort: 443, AppName: "tls", AppCategory: "web", AppConfidence: 90, TLSSNI: "example.test", Risk: []string{"test"}, Metadata: map[string]string{"tls.sni": "example.test"}}, time.Hour, 100); err != nil {
		t.Fatal(err)
	}
	store.Close()
	h := New(Options{FirewallLogPath: path})
	flows := []logstore.TrafficFlow{{Protocol: "tcp", ClientAddress: "10.0.0.2", ClientPort: 12345, PeerAddress: "192.0.2.1", PeerPort: 443, AppName: "existing", AppConfidence: 99}, {Protocol: "udp", ClientAddress: "10.0.0.3", ClientPort: 23456, PeerAddress: "192.0.2.2", PeerPort: 53}}
	enriched, err := h.enrichTrafficFlowsWithDPI(context.Background(), flows, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if enriched[0].AppName != "existing" || enriched[0].AppConfidence != 99 || enriched[0].TLSSNI != "example.test" || enriched[0].Metadata["tls.sni"] != "example.test" {
		t.Fatalf("existing fields or enrichment lost: %+v", enriched[0])
	}
	if enriched[1].AppName == "" {
		t.Fatalf("unmatched flow lost port fallback: %+v", enriched[1])
	}
	table := &observe.ConnectionTable{Entries: []observe.ConnectionEntry{{Protocol: "tcp", Original: observe.ConntrackTuple{Source: "192.0.2.1", SourcePort: "443", Destination: "10.0.0.2", DestinationPort: "12345"}}, {Protocol: "udp", Original: observe.ConntrackTuple{Source: "10.0.0.3", SourcePort: "23456", Destination: "192.0.2.2", DestinationPort: "53"}}}}
	if err := h.enrichConnectionsWithDPI(context.Background(), table, now, time.Hour); err != nil {
		t.Fatal(err)
	}
	if table.Entries[0].TLSSNI != "example.test" || table.Entries[1].AppName == "" {
		t.Fatalf("connection enrichment/fallback: %+v", table)
	}
}

func TestMissingOptionalDPIStoreRetainsFallbackWithoutCreatingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "firewall.db")
	h := New(Options{FirewallLogPath: path, Connections: func(int) (*observe.ConnectionTable, error) {
		return &observe.ConnectionTable{Entries: []observe.ConnectionEntry{{Protocol: "udp", Original: observe.ConntrackTuple{Source: "10.0.0.2", SourcePort: "12345", Destination: "192.0.2.1", DestinationPort: "53"}}}}, nil
	}, ReverseLookup: func(context.Context, string) ([]string, error) { return nil, nil }})
	flows, err := h.enrichTrafficFlowsWithDPI(context.Background(), []logstore.TrafficFlow{{Protocol: "udp", ClientAddress: "10.0.0.2", ClientPort: 12345, PeerAddress: "192.0.2.1", PeerPort: 53}}, time.Now(), time.Hour)
	if err != nil || flows[0].AppName == "" {
		t.Fatalf("fallback: flows=%+v err=%v", flows, err)
	}
	for _, path := range []string{"/api/v1/connections", "/api/v1/firewall/deny-timeline"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%s", path, w.Code, w.Body.String())
		}
		if strings.HasSuffix(path, "connections") && !strings.Contains(w.Body.String(), `"appName"`) {
			t.Fatalf("missing connection fallback: %s", w.Body.String())
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read-only request created database: %v", err)
	}
}
