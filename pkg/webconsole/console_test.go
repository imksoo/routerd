// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"routerd/pkg/apply"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"
	routerstate "routerd/pkg/state"
)

type fakeStore struct {
	resources []routerstate.ObjectStatus
	events    []routerstate.StoredEvent
	latest    int64
}

func (s fakeStore) Get(string) routerstate.Value                            { return routerstate.Value{} }
func (s fakeStore) Set(string, string, string) routerstate.Value            { return routerstate.Value{} }
func (s fakeStore) Unset(string, string) routerstate.Value                  { return routerstate.Value{} }
func (s fakeStore) Forget(string, string) routerstate.Value                 { return routerstate.Value{} }
func (s fakeStore) Delete(string)                                           {}
func (s fakeStore) Age(string) time.Duration                                { return 0 }
func (s fakeStore) Now() time.Time                                          { return time.Now() }
func (s fakeStore) Save(string) error                                       { return nil }
func (s fakeStore) Variables() map[string]routerstate.Value                 { return nil }
func (s fakeStore) LatestGeneration() int64                                 { return s.latest }
func (s fakeStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) { return s.resources, nil }
func (s fakeStore) ListEvents(routerstate.EventQuery) ([]routerstate.StoredEvent, error) {
	return s.events, nil
}

func TestHandlerServesReadOnlySummary(t *testing.T) {
	queryLog := t.TempDir() + "/dns-queries.db"
	trafficLog := t.TempDir() + "/traffic-flows.db"
	firewallLogPath := t.TempDir() + "/firewall-logs.db"
	dnsLog, err := logstore.OpenDNSQueryLog(queryLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(reqContext(), logstore.DNSQuery{Timestamp: time.Now(), ClientAddress: "172.18.0.2", QuestionName: "example.com", QuestionType: "A", Answers: []string{"93.184.216.34"}, ResponseCode: "NOERROR"}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	flows, err := logstore.OpenTrafficFlowLog(trafficLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := flows.UpsertActive(context.Background(), logstore.TrafficFlow{StartedAt: time.Now(), ClientAddress: "172.18.0.2", PeerAddress: "93.184.216.34", Protocol: "tcp"}); err != nil {
		t.Fatal(err)
	}
	_ = flows.Close()
	firewallLog, err := logstore.OpenFirewallLog(firewallLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := firewallLog.Record(context.Background(), logstore.FirewallLogEntry{Timestamp: time.Now(), Action: "drop", SrcAddress: "172.18.0.2", DstAddress: "198.51.100.1", Protocol: "tcp", TCPFlags: "SYN", L3Proto: "ipv4"}); err != nil {
		t.Fatal(err)
	}
	_ = firewallLog.Close()
	handler := New(Options{
		Store: fakeStore{
			resources: []routerstate.ObjectStatus{{APIVersion: "net.routerd.net/v1alpha1", Kind: "HealthCheck", Name: "internet", Status: map[string]any{"phase": "Healthy"}}},
			events:    []routerstate.StoredEvent{{ID: 1, Topic: "routerd.dhcp.lease.renewed", CreatedAt: time.Date(2026, 5, 4, 1, 2, 3, 0, time.UTC), Attributes: map[string]any{"mac": "18:ec:e7:33:12:6c", "ip": "172.18.0.150", "hostname": "aiseg2"}}},
			latest:    11,
		},
		Result: func() *apply.Result {
			return &apply.Result{Phase: "Healthy", Generation: 7, Resources: []apply.ResourceResult{{ID: "x", Phase: "Healthy"}}}
		},
		Connections: func(limit int) (*observe.ConnectionTable, error) {
			return &observe.ConnectionTable{Count: 3, Max: 262144}, nil
		},
		VPNStatus: func() (VPNStatus, error) {
			return VPNStatus{Tailscale: &TailscaleStatus{HostName: "homert02", BackendState: "Running"}}, nil
		},
		DNSQueryLogPath:    queryLog,
		TrafficFlowLogPath: trafficLog,
		FirewallLogPath:    firewallLogPath,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"phase": "Healthy"`, `"generation": 11`, `"HealthCheck"`, `"connections"`, `"dnsQueries"`, `"trafficFlows"`, `"firewallLogs"`, `"tcpFlags": "SYN"`, `"tailscale"`, `"homert02"`, "example.com", `"resolvedHostname": "example.com"`, `"topic": "routerd.dhcp.lease.renewed"`, `"mac": "18:ec:e7:33:12:6c"`, `"ip": "172.18.0.150"`, `"hostname": "aiseg2"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("summary missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerServesVPNStatus(t *testing.T) {
	handler := New(Options{VPNStatus: func() (VPNStatus, error) {
		return VPNStatus{
			WireGuard: []WireGuardInterfaceStatus{{
				Name:       "wg0",
				PublicKey:  "interface-public",
				ListenPort: 51820,
				Peers: []WireGuardPeerStatus{{
					PublicKey:       "peer-public",
					Endpoint:        "203.0.113.2:51820",
					AllowedIPs:      []string{"10.0.0.2/32"},
					TransferRxBytes: 100,
					TransferTxBytes: 200,
				}},
			}},
			Tailscale: &TailscaleStatus{
				BackendState: "Running",
				HostName:     "homert02",
				TailscaleIPs: []string{"100.64.87.102"},
				Peers:        []TailscalePeerStatus{{HostName: "phone", Online: true}},
			},
		}, nil
	}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vpn", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"wireGuard"`, `"wg0"`, `"peer-public"`, `"tailscale"`, `"homert02"`, `"phone"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("vpn response missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerStreamsBusEventsOverSSE(t *testing.T) {
	eventBus := bus.New()
	handler := New(Options{Bus: eventBus})
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("content type = %q", contentType)
	}

	reader := bufio.NewReader(resp.Body)
	if line, err := reader.ReadString('\n'); err != nil || strings.TrimSpace(line) != "event: connected" {
		t.Fatalf("connected event line = %q err=%v", line, err)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "test"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "HealthCheck", Name: "internet"}
	event.Attributes = map[string]string{"phase": "Healthy"}
	if err := eventBus.Publish(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	var sawEvent bool
	var sawData bool
	deadline := time.After(2 * time.Second)
	for !(sawEvent && sawData) {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for streamed event")
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "event: routerd-event" {
			sawEvent = true
		}
		if strings.HasPrefix(trimmed, "data: ") && strings.Contains(trimmed, "routerd.resource.status.changed") && strings.Contains(trimmed, "HealthCheck") {
			sawData = true
		}
	}
}

func TestHandlerServesEmbeddedAppShell(t *testing.T) {
	handler := New(Options{Title: "homert02"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`window.__ROUTERD_WEB_CONSOLE__`, `basePath: "/"`, `title: "homert02"`, `type="module"`, `id="root"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("index missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerServesGenerationHistoryAndDiff(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := store.BeginGeneration("hash-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(first, "kind: Router\nmetadata:\n  name: old\n"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishGeneration(first, "Healthy", nil); err != nil {
		t.Fatal(err)
	}
	second, err := store.BeginGeneration("hash-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordGenerationConfig(second, "kind: Router\nmetadata:\n  name: new\n"); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishGeneration(second, "Healthy", nil); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{Store: store})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/generations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"generation": 2`, `"configHash": "hash-b"`, `"hasYaml": true`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("generations missing %q:\n%s", want, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/generations/1/diff/2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"--- generation-1.yaml", "+++ generation-2.yaml", "-  name: old", "+  name: new"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("diff missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func reqContext() context.Context { return context.Background() }

func TestHandlerRejectsWriteMethods(t *testing.T) {
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandlerServesDNSQueries(t *testing.T) {
	queryLog := t.TempDir() + "/dns-queries.db"
	dnsLog, err := logstore.OpenDNSQueryLog(queryLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now(), ClientAddress: "172.18.0.2", QuestionName: "www.example.com", QuestionType: "AAAA", ResponseCode: "NOERROR"}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	handler := New(Options{DNSQueryLogPath: queryLog})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dns-queries?since=1h&limit=10", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "www.example.com") {
		t.Fatalf("dns queries missing row:\n%s", rec.Body.String())
	}
}

func TestHandlerServesTrafficFlows(t *testing.T) {
	path := t.TempDir() + "/traffic-flows.db"
	queryLog := t.TempDir() + "/dns-queries.db"
	dnsLog, err := logstore.OpenDNSQueryLog(queryLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now(), ClientAddress: "172.18.0.2", QuestionName: "one.one.one.one", QuestionType: "A", ResponseCode: "NOERROR", Answers: []string{"1.1.1.1"}}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	flowLog, err := logstore.OpenTrafficFlowLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := flowLog.UpsertActive(context.Background(), logstore.TrafficFlow{StartedAt: time.Now(), ClientAddress: "172.18.0.2", PeerAddress: "1.1.1.1", PeerPort: 443, Protocol: "tcp"}); err != nil {
		t.Fatal(err)
	}
	_ = flowLog.Close()
	handler := New(Options{TrafficFlowLogPath: path, DNSQueryLogPath: queryLog})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traffic-flows?since=1h&limit=10", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "one.one.one.one") {
		t.Fatalf("traffic flows missing row:\n%s", rec.Body.String())
	}
}

func TestHandlerServesFirewallLogs(t *testing.T) {
	path := t.TempDir() + "/firewall-logs.db"
	firewallLog, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := firewallLog.Record(context.Background(), logstore.FirewallLogEntry{Timestamp: time.Now(), Action: "drop", SrcAddress: "172.18.0.2", DstAddress: "198.51.100.1", Protocol: "tcp", TCPFlags: "SYN", L3Proto: "ipv4"}); err != nil {
		t.Fatal(err)
	}
	_ = firewallLog.Close()
	handler := New(Options{FirewallLogPath: path})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/firewall-logs?since=1h&action=drop", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "198.51.100.1") || !strings.Contains(rec.Body.String(), `"tcpFlags": "SYN"`) {
		t.Fatalf("firewall logs missing row:\n%s", rec.Body.String())
	}
}

func TestHandlerEnrichesConnectionsWithDPI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall-logs.db")
	firewallLog, err := logstore.OpenFirewallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := firewallLog.RecordDPIFlow(context.Background(), logstore.DPIFlowEntry{
		FirstSeen:     now.Add(-time.Minute),
		LastSeen:      now,
		Protocol:      "tcp",
		SrcAddress:    "172.18.0.10",
		SrcPort:       53168,
		DstAddress:    "198.51.100.10",
		DstPort:       443,
		AppName:       "tls",
		AppCategory:   "web",
		AppConfidence: 90,
		TLSSNI:        "cached.example",
	}, time.Hour, 100000); err != nil {
		t.Fatal(err)
	}
	_ = firewallLog.Close()
	handler := New(Options{
		FirewallLogPath: path,
		Connections: func(limit int) (*observe.ConnectionTable, error) {
			return &observe.ConnectionTable{Entries: []observe.ConnectionEntry{{
				Family:   "ipv4",
				Protocol: "tcp",
				Original: observe.ConntrackTuple{
					Source:          "172.18.0.10",
					SourcePort:      "53168",
					Destination:     "198.51.100.10",
					DestinationPort: "443",
				},
			}}}, nil
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"appName": "tls"`, `"tlsSNI": "cached.example"`, `"appConfidence": 90`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("connections missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerIncludesDHCPLeases(t *testing.T) {
	leasePath := filepath.Join(t.TempDir(), "dnsmasq.leases")
	if err := os.WriteFile(leasePath, []byte("1778014867 7c:dd:e9:01:40:15 172.18.1.78 ATOM 01:7c:dd:e9:01:40:15\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{DHCPLeasePaths: []string{leasePath}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"172.18.1.78", "7c:dd:e9:01:40:15", "ATOM", "ATOM tech Inc."} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("summary missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestReadDnsmasqLeases(t *testing.T) {
	leasePath := filepath.Join(t.TempDir(), "dnsmasq.leases")
	if err := os.WriteFile(leasePath, []byte("1778014867 18:ec:e7:33:12:6c 172.18.0.150 aiseg2 01:18:ec:e7:33:12:6c\n1778014867 bc:24:11:e0:8e:3a 2409:10:3d60:1271::20 host6 00:03:00:01:bc:24:11:e0:8e:3a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	leases, err := readDnsmasqLeases(leasePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 2 {
		t.Fatalf("leases = %d", len(leases))
	}
	lease := leases[0]
	if lease.IP != "172.18.0.150" || lease.Hostname != "aiseg2" || lease.Vendor != "Panasonic" || lease.Family != "ipv4" {
		t.Fatalf("lease = %+v", lease)
	}
	if leases[1].IP != "2409:10:3d60:1271::20" || leases[1].Family != "ipv6" {
		t.Fatalf("ipv6 lease = %+v", leases[1])
	}
}

func TestParseWireGuardAllDump(t *testing.T) {
	rows, err := parseWireGuardAllDump([]byte("wg0\tprivate-key-must-not-leak\tinterface-public\t51820\toff\nwg0\tpeer-public\tpreshared-key-must-not-leak\t203.0.113.2:51820\t10.0.0.2/32,fd00::2/128\t1710000000\t100\t200\t25\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("interfaces = %d", len(rows))
	}
	if rows[0].Name != "wg0" || rows[0].PublicKey != "interface-public" || rows[0].ListenPort != 51820 {
		t.Fatalf("interface = %+v", rows[0])
	}
	if strings.Contains(fmt.Sprintf("%+v", rows), "private-key") || strings.Contains(fmt.Sprintf("%+v", rows), "preshared-key") {
		t.Fatalf("secret material leaked: %+v", rows)
	}
	if len(rows[0].Peers) != 1 || rows[0].Peers[0].PublicKey != "peer-public" || rows[0].Peers[0].TransferTxBytes != 200 || rows[0].Peers[0].PersistentKeepaliveSec != 25 {
		t.Fatalf("peer = %+v", rows[0].Peers)
	}
	if got := rows[0].Peers[0].AllowedIPs; len(got) != 2 || got[1] != "fd00::2/128" {
		t.Fatalf("allowed ips = %+v", got)
	}
	if rows[0].Peers[0].LatestHandshake.IsZero() {
		t.Fatalf("handshake not parsed: %+v", rows[0].Peers[0])
	}
}

func TestParseTailscaleStatusJSON(t *testing.T) {
	status, err := parseTailscaleStatusJSON([]byte(`{
	  "BackendState": "Running",
	  "CurrentTailnet": {"Name": "example@example.com", "MagicDNSSuffix": "example.ts.net", "MagicDNSEnabled": true},
	  "CertDomains": ["homert02.example.ts.net"],
	  "Self": {
	    "HostName": "homert02",
	    "DNSName": "homert02.example.ts.net.",
	    "TailscaleIPs": ["100.64.87.102", "fd7a:115c:a1e0::1"],
	    "AllowedIPs": ["100.64.87.102/32"],
	    "Online": true,
	    "ExitNodeOption": true
	  },
	  "Peer": {
	    "node-b": {"HostName": "phone", "Online": false, "LastSeen": "2026-05-07T10:00:00Z"},
	    "node-a": {"HostName": "laptop", "Online": true, "Active": true, "TailscaleIPs": ["100.64.0.2"], "Relay": "tok", "LastSeen": "2026-05-07T11:00:00Z", "RxBytes": 10, "TxBytes": 20}
	  }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || status.HostName != "homert02" || status.BackendState != "Running" || !status.Online {
		t.Fatalf("status = %+v", status)
	}
	if status.TailnetName != "example@example.com" || status.MagicDNSSuffix != "example.ts.net" || !status.MagicDNSEnabled || len(status.CertDomains) != 1 {
		t.Fatalf("tailnet fields = %+v", status)
	}
	if len(status.Peers) != 2 || status.Peers[0].HostName != "laptop" || !status.Peers[0].Active {
		t.Fatalf("peers not sorted/parsed: %+v", status.Peers)
	}
}

func TestHostCommandPathKeepsAbsolutePath(t *testing.T) {
	if got := hostCommandPath("/usr/local/bin/tailscale"); got != "/usr/local/bin/tailscale" {
		t.Fatalf("hostCommandPath = %q", got)
	}
}

func TestParseIPNeighborJSON(t *testing.T) {
	rows, err := parseIPNeighborJSON([]byte(`[
	  {"dst":"172.18.1.110","dev":"ens19","lladdr":"4e:20:15:aa:e0:67","state":["REACHABLE"]},
	  {"dst":"2409:10:3d60:1271::abcd","dev":"ens19","lladdr":"4e:20:15:aa:e0:67","state":["STALE"]}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].MAC != "4e:20:15:aa:e0:67" || rows[0].Vendor != "Apple private address" {
		t.Fatalf("row = %+v", rows[0])
	}
}

func TestParseFreeBSDNeighbors(t *testing.T) {
	arp := parseFreeBSDARP([]byte(`? (192.168.160.182) at bc:24:11:df:9e:c2 on vtnet1 expires in 1197 seconds [ethernet]
? (192.168.160.190) at (incomplete) on vtnet1 expired [ethernet]
`))
	if len(arp) != 1 || arp[0].IP != "192.168.160.182" || arp[0].IfName != "vtnet1" {
		t.Fatalf("arp = %+v", arp)
	}
	ndp := parseFreeBSDNDP([]byte(`Neighbor                             Linklayer Address  Netif Expire    S Flags
2409:10:3d60:1250::abcd             bc:24:11:df:9e:c2 vtnet1 23h59m37s S R
fe80::be24:11ff:fedf:9ec2%vtnet1    bc:24:11:df:9e:c2 vtnet1 permanent R
`))
	if len(ndp) != 2 || ndp[0].IP != "2409:10:3d60:1250::abcd" || ndp[1].IP != "fe80::be24:11ff:fedf:9ec2" {
		t.Fatalf("ndp = %+v", ndp)
	}
}

func TestCorrelateClientsMergesDHCPLeaseAndIPv6NeighborByMAC(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "4e:20:15:aa:e0:67",
			IP:       "172.18.1.110",
			Hostname: "MacBookAir",
			Vendor:   "Apple private address",
		}},
		[]NeighborEntry{
			{MAC: "4e:20:15:aa:e0:67", IP: "172.18.1.110", IfName: "ens19", State: "REACHABLE", Source: "ip-neigh"},
			{MAC: "4e:20:15:aa:e0:67", IP: "2409:10:3d60:1271::abcd", IfName: "ens19", State: "STALE", Source: "ip-neigh"},
			{MAC: "4e:20:15:aa:e0:67", IP: "fe80::14c5:6fd7:b848:a739", IfName: "ens19", State: "STALE", Source: "ip-neigh"},
		},
		[]logstore.TrafficFlow{{ClientAddress: "2409:10:3d60:1271::abcd", PeerAddress: "2001:4860:4860::8888", Accounting: true, BytesOut: 120, BytesIn: 240}},
		[]logstore.DNSQuery{{ClientAddress: "2409:10:3d60:1271::abcd", QuestionName: "www.icloud.com", QuestionType: "AAAA"}},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.MAC != "4e:20:15:aa:e0:67" || row.Hostname != "MacBookAir" {
		t.Fatalf("row identity = %+v", row)
	}
	for _, want := range []string{"172.18.1.110", "2409:10:3d60:1271::abcd", "fe80::14c5:6fd7:b848:a739"} {
		if !containsString(row.Addresses, want) {
			t.Fatalf("addresses missing %s: %+v", want, row.Addresses)
		}
	}
	if row.BytesOut != 120 || row.BytesIn != 240 {
		t.Fatalf("traffic was not joined to client: %+v", row)
	}
	if row.InferredOSFamily != "Apple" || row.FingerprintConfidence == 0 {
		t.Fatalf("fingerprint missing: %+v", row)
	}
}

func TestCorrelateClientsAddsDPIActivitySummary(t *testing.T) {
	now := time.Now().UTC()
	rows := correlateClients(
		nil,
		nil,
		[]logstore.TrafficFlow{
			{
				ClientAddress: "172.18.1.120",
				PeerAddress:   "93.184.216.34",
				AppName:       "tls",
				TLSSNI:        "example.com",
				Accounting:    true,
				BytesOut:      1600,
				BytesIn:       6400,
				EndedAt:       now.Add(-2 * time.Minute),
			},
			{
				ClientAddress:    "172.18.1.120",
				PeerAddress:      "1.1.1.1",
				AppName:          "dns",
				ResolvedHostname: "one.one.one.one",
				Accounting:       true,
				BytesOut:         120,
				BytesIn:          240,
				EndedAt:          now,
			},
		},
		nil,
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.PrimaryActivity != "web-heavy" {
		t.Fatalf("primary activity = %q, row = %+v", row.PrimaryActivity, row)
	}
	if row.LastProtocol != "dns" || row.LastProtocolDetail != "DNS-query=one.one.one.one" {
		t.Fatalf("last protocol = %q detail = %q", row.LastProtocol, row.LastProtocolDetail)
	}
	for _, want := range []string{"tls", "dns"} {
		if !containsString(row.ProtocolMix, want) {
			t.Fatalf("protocol mix missing %q: %+v", want, row.ProtocolMix)
		}
	}
}

func TestCorrelateClientsSkipsFailedNeighbors(t *testing.T) {
	rows := correlateClients(nil, []NeighborEntry{
		{IP: "192.168.178.40", IfName: "ens18", State: "FAILED", Source: "ip-neigh"},
		{IP: "172.18.1.110", IfName: "ens19", MAC: "4e:20:15:aa:e0:67", State: "REACHABLE", Source: "ip-neigh"},
	}, nil, nil, nil)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if containsString(rows[0].Addresses, "192.168.178.40") {
		t.Fatalf("failed neighbor leaked into clients: %+v", rows[0])
	}
	if !containsString(rows[0].Addresses, "172.18.1.110") {
		t.Fatalf("reachable neighbor missing from clients: %+v", rows[0])
	}
}

func TestCorrelateClientsGroupsPrivacyAddressByFingerprintWhenUnique(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "4e:20:15:aa:e0:67",
			IP:       "172.18.1.110",
			Hostname: "mcberry-iPhone",
			Vendor:   "Apple private address",
		}},
		nil,
		[]logstore.TrafficFlow{{ClientAddress: "2409:10:3d60:1271:abcd::1234", PeerAddress: "17.253.144.10", ResolvedHostname: "gateway.icloud.com"}},
		[]logstore.DNSQuery{{ClientAddress: "2409:10:3d60:1271:abcd::1234", QuestionName: "gateway.icloud.com", QuestionType: "AAAA"}},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if !containsString(rows[0].Addresses, "2409:10:3d60:1271:abcd::1234") {
		t.Fatalf("privacy address not grouped: %+v", rows[0])
	}
	if rows[0].InferredOSFamily != "Apple" || rows[0].InferredDeviceClass != "phone" {
		t.Fatalf("fingerprint = %+v", rows[0])
	}
}

func TestCorrelateClientsKeepsDeviceClassWithinInferredOSFamily(t *testing.T) {
	rows := correlateClients(
		[]DHCPLease{{
			MAC:      "66:1f:8c:8d:0d:58",
			IP:       "172.18.1.133",
			Hostname: "iPhone",
		}},
		nil,
		nil,
		[]logstore.DNSQuery{
			{ClientAddress: "172.18.1.133", QuestionName: "www.icloud.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.133", QuestionName: "login.microsoft.com", QuestionType: "A"},
			{ClientAddress: "172.18.1.133", QuestionName: "office365.com", QuestionType: "A"},
		},
		nil,
	)
	if len(rows) != 1 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[0].InferredOSFamily != "Apple" || rows[0].InferredDeviceClass != "phone" {
		t.Fatalf("mixed signals should keep iPhone as Apple phone: %+v", rows[0])
	}
}

func TestHandlerServesConfigReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "router.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: routerd.net/v1alpha1\nkind: Router\n"), 0644); err != nil {
		t.Fatal(err)
	}
	handler := New(Options{ConfigPath: path})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{path, "apiVersion: routerd.net/v1alpha1", "kind: Router"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("config response missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestHandlerRendersUsableBasePath(t *testing.T) {
	handler := New(Options{BasePath: "/", Title: "homert02"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`window.__ROUTERD_WEB_CONSOLE__`, `basePath: "/"`, `title: "homert02"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q:\n%s", want, body)
		}
	}
}

func TestHandlerRejectsLegacyAPIPaths(t *testing.T) {
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandlerServesEmbeddedAssets(t *testing.T) {
	assets, err := fs.Glob(staticFiles, "static/assets/*.css")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) == 0 {
		t.Fatal("embedded web console css asset not found")
	}
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/"+strings.TrimPrefix(assets[0], "static/"), nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
