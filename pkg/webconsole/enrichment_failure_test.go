// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestAddressSetObservationFailureIsReportedByHTTPConsumers(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "firewall.db")
	logs, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := logs.Record(context.Background(), logstore.FirewallLogEntry{Timestamp: time.Now(), Action: "drop", Protocol: "udp", SrcAddress: "192.0.2.2", DstAddress: "8.8.8.8", DstPort: 53, RuleName: "cloud"}); err != nil {
		t.Fatal(err)
	}
	if err := logs.Close(); err != nil {
		t.Fatal(err)
	}
	declaration := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "cloud"}, Spec: api.IPAddressSetSpec{Addresses: []string{"8.8.8.8"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"}, Metadata: api.ObjectMeta{Name: "redirect"}, Spec: api.LocalServiceRedirectSpec{Rules: []api.LocalServiceRedirectRuleSpec{{Protocols: []string{"udp"}, DestinationSetRef: "cloud", DestinationPort: 53, RedirectPort: 53}}}},
	}}}
	h := New(Options{
		Router: declaration, Store: store, FirewallLogPath: path,
		Connections: func(context.Context, int) (*observe.ConnectionTable, error) {
			return &observe.ConnectionTable{Entries: []observe.ConnectionEntry{{Protocol: "udp", Original: observe.ConntrackTuple{Source: "192.0.2.2", Destination: "8.8.8.8", DestinationPort: "53"}, Reply: observe.ConntrackTuple{Source: "192.0.2.1", SourcePort: "53"}}}}, nil
		},
		ReverseLookup: func(context.Context, string) ([]string, error) { return nil, nil },
	})
	server := httptest.NewServer(h)
	defer server.Close()
	for _, route := range []string{"connections", "firewall-logs"} {
		response, err := server.Client().Get(server.URL + "/api/v1/" + route)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "address set status") {
			t.Fatalf("%s masked failed state read as current membership: status=%d body=%s", route, response.StatusCode, body)
		}
	}
	response, err := server.Client().Get(server.URL + "/api/v1/summary?events=-1&connections=1&firewallLogs=1&dnsQueries=-1&trafficFlows=-1&vpn=0&resources=0&dhcpLeases=0")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var snapshot Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Errors) == 0 || !snapshot.collected(SectionConnections) || !snapshot.collected(SectionFirewallLogs) || len(snapshot.FirewallLogs) != 1 || len(snapshot.FirewallLogs[0].DestinationSets) != 0 || snapshot.Connections == nil || snapshot.Connections.Entries[0].LocalRedirect != nil {
		t.Fatalf("summary must retain base observations and report unavailable enrichment: %+v", snapshot)
	}
}

func TestLogHTTPFiltersRejectInvalidRequestedWindows(t *testing.T) {
	server := httptest.NewServer(New(Options{}))
	defer server.Close()
	for _, route := range []string{
		"dns-queries?since=not-a-duration",
		"traffic-flows?since=-1h",
		"firewall-logs?since=9223372036854775807d",
		"firewall/deny-timeline?range=bad",
		"firewall/deny-timeline?bucket=0s",
		"dns-queries?duration-min=-1ms",
		"traffic-flows?from=2026-02-02&until=2026-01-01",
	} {
		response, err := server.Client().Get(server.URL + "/api/v1/" + route)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s; must reject requested filter instead of substituting another window", route, response.StatusCode, body)
		}
	}
}
