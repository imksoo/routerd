// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/logstore"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSummaryCollectionAndMetricsDistinguishEmptySkippedAndFailed(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	write := func(path, text string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), mode); err != nil {
			t.Fatal(err)
		}
	}
	// Execute only test-owned observation commands; never read host neighbors.
	for _, name := range []string{"ip", "arp", "ndp"} {
		write(filepath.Join(dir, name), "#!/bin/sh\nprintf '[]'\n", 0o755)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	statePath := filepath.Join(dir, "state.db")
	store, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "wan", map[string]any{"phase": "Healthy"}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	leasePath := filepath.Join(dir, "leases")
	write(leasePath, fmt.Sprintf("%d 02:00:00:00:00:01 192.0.2.2 client-one *\n", time.Now().Add(time.Hour).Unix()), 0o600)
	stickyPath := filepath.Join(dir, "sticky.db")
	sticky, err := logstore.OpenDHCPStickyLog(stickyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sticky.Close()
	if err := sticky.RecordLeaseEvent(ctx, "removed", "02:00:00:00:00:02", "2001:db8::2", "held-client", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.db")
	h := New(Options{Store: store, DHCPLeasePaths: []string{leasePath}, DHCPStickyLogPath: stickyPath, DHCPFingerprintLogPath: missing})
	server := httptest.NewServer(h)
	defer server.Close()
	request := func(query string) Snapshot {
		t.Helper()
		response, err := http.Get(server.URL + "/api/v1/summary?events=-1&connections=-1&firewallLogs=-1&dnsQueries=-1&trafficFlows=-1&vpn=0&" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var snapshot Snapshot
		if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("summary status = %d", response.StatusCode)
		}
		return snapshot
	}
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer provider.Shutdown(ctx)
	metrics := newConsoleMetrics(provider.Meter("routerd"))
	record := func(snapshot Snapshot) {
		t.Helper()
		rows, err := h.dhcpStickyLeaseList(ctx, logstore.DHCPStickyFilter{HeldOnly: true, Now: time.Now(), Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		metrics.record(ctx, snapshot, rows, time.Now())
	}
	assertGauges := func(phase, active, held, clients int64) {
		t.Helper()
		var collected metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &collected); err != nil {
			t.Fatal(err)
		}
		for _, check := range []struct {
			name, key, label string
			want             int64
		}{
			{"routerd.resource.phase.count", "routerd.resource.phase", "Healthy", phase},
			{"routerd.dhcp.lease.active", "network.address.family", "ipv4", active},
			{"routerd.dhcp.sticky.held", "network.address.family", "ipv6", held},
			{"routerd.client.active.count", "", "", clients},
		} {
			found := false
			for _, scope := range collected.ScopeMetrics {
				for _, metric := range scope.Metrics {
					if metric.Name != check.name {
						continue
					}
					gauge, ok := metric.Data.(metricdata.Gauge[int64])
					if !ok {
						t.Fatalf("%s has type %T", check.name, metric.Data)
					}
					for _, point := range gauge.DataPoints {
						label, _ := point.Attributes.Value(attribute.Key(check.key))
						if check.key == "" || label.AsString() == check.label {
							found = true
							if point.Value != check.want {
								t.Errorf("%s[%s]=%d, want %d", check.name, check.label, point.Value, check.want)
							}
						}
					}
				}
			}
			if !found {
				t.Errorf("missing gauge %s[%s]", check.name, check.label)
			}
		}
	}
	initial := request("resources=1&dhcpLeases=1&clients=1")
	if len(initial.Errors) != 0 || !initial.collected(SectionResources) || !initial.collected(SectionDHCPLeases) || !initial.collected(SectionClients) || len(initial.Clients) != 2 {
		t.Fatalf("initial collection = %+v", initial)
	}
	record(initial)
	assertGauges(1, 1, 1, 2)
	partial := request("resources=0&dhcpLeases=0&clients=0")
	if partial.collected(SectionResources) || partial.collected(SectionDHCPLeases) || partial.collected(SectionClients) {
		t.Fatalf("skipped sections marked collected: %v", partial.Collected)
	}
	record(partial)
	assertGauges(1, 1, 1, 2)
	// Real read errors must not publish a fresh zero for failed or derived data.
	if _, err := db.Exec("UPDATE objects SET status='{broken'"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(leasePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(leasePath, 0o700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(dir, "ip"), "#!/bin/sh\nexit 1\n", 0o755)
	failed := request("resources=1&dhcpLeases=1&clients=1")
	if len(failed.Errors) == 0 || failed.collected(SectionResources) || failed.collected(SectionDHCPLeases) || failed.collected(SectionNeighbors) || failed.collected(SectionClients) {
		t.Fatalf("failed reads advertised as fresh: collected=%v errors=%v", failed.Collected, failed.Errors)
	}
	record(failed)
	assertGauges(1, 1, 1, 2)
	if _, err := db.Exec("DELETE FROM objects"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(leasePath); err != nil {
		t.Fatal(err)
	}
	write(leasePath, "", 0o600)
	write(filepath.Join(dir, "ip"), "#!/bin/sh\nprintf '[]'\n", 0o755)
	if err := sticky.RecordLeaseEvent(ctx, "removed", "02:00:00:00:00:02", "2001:db8::2", "held-client", 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	empty := request("resources=1&dhcpLeases=1&clients=1")
	if len(empty.Errors) != 0 || !empty.collected(SectionResources) || !empty.collected(SectionDHCPLeases) || !empty.collected(SectionClients) || len(empty.Resources)+len(empty.DHCPLeases)+len(empty.Clients) != 0 {
		t.Fatalf("empty successful collection = %+v", empty)
	}
	record(empty)
	assertGauges(0, 0, 0, 0)
}

func TestFreeBSDNeighborPartialFailureIsNotCompleteObservation(t *testing.T) {
	dir := t.TempDir()
	for name, command := range map[string]string{
		"arp": "#!/bin/sh\nprintf '? (192.0.2.1) at 02:00:00:00:00:01 on test0 expires in 100 seconds [ethernet]\\n'\n",
		"ndp": "#!/bin/sh\nexit 1\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(command), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	neighbors, err := freeBSDNeighborList(context.Background())
	if len(neighbors) != 1 || neighbors[0].IP != "192.0.2.1" || err == nil {
		t.Fatalf("partial neighbor observation: neighbors=%+v err=%v", neighbors, err)
	}
}
