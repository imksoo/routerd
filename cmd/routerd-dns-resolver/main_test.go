// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/imksoo/routerd/pkg/logstore"
)

func TestSelftest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolver.json")
	if err := os.WriteFile(path, []byte(`{"resource":"lab","spec":{"listen":[{"addresses":["127.0.0.1"],"port":5053}],"sources":[{"name":"default","kind":"upstream","match":["."],"upstreams":["https://1.1.1.1/dns-query"]}]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"selftest", "--resource", "lab", "--config-file", path}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"resource":"lab"`) || !strings.Contains(out.String(), "https://1.1.1.1/dns-query") {
		t.Fatalf("unexpected selftest output: %s", out.String())
	}
}

func TestReloadSwapsForwardSources(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	initial := testForwardConfig("old", "udp://127.0.0.1:5301")
	writeRuntimeConfig(t, configPath, initial)
	d := newTestDaemon(t, configPath, initial, true)

	updated := testForwardConfig("new", "udp://127.0.0.1:5302")
	writeRuntimeConfig(t, configPath, updated)
	if _, err := d.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	if len(d.sources) != 1 || d.sources[0].Spec.Name != "new" {
		t.Fatalf("sources = %#v, want swapped source named new", d.sources)
	}
	if d.sources[0].Pool == nil || d.sources[0].Pool.Snapshot()[0].Address != "127.0.0.1:5302" {
		t.Fatalf("source pool was not rebuilt: %#v", d.sources[0].Pool)
	}
}

func TestReloadPreservesDynamicLeaseRecords(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig([]int{5053})
	writeRuntimeConfig(t, configPath, initial)
	d := newTestDaemon(t, configPath, initial, true)
	d.zones.ApplyLease(dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, MAC: "02:00:00:00:00:01", IP: "192.0.2.55", Hostname: "leasehost"})

	writeRuntimeConfig(t, configPath, initial)
	if _, err := d.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := new(dns.Msg)
	req.SetQuestion("leasehost.lab.example.", dns.TypeA)
	d.stateMu.RLock()
	zones := d.zones
	d.stateMu.RUnlock()
	resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("dynamic record was not preserved: ok=%v resp=%v", ok, resp)
	}
	if a, isA := resp.Answer[0].(*dns.A); !isA || a.A.String() != "192.0.2.55" {
		t.Fatalf("dynamic answer = %v, want 192.0.2.55", resp.Answer[0])
	}
}

func TestReloadActiveLeaseEventTakesPrecedenceOverNewStickySnapshot(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig([]int{5053})
	initial.Zones[0].DHCPHostRecords = []resolvercfg.RuntimeDHCPHostRecord{{
		Hostname: "client",
		IP:       "192.0.2.127",
	}}
	writeRuntimeConfig(t, configPath, initial)
	d := newTestDaemon(t, configPath, initial, true)
	d.zones.ApplyLease(dhcpLeaseEvent{
		Action:   daemonapi.DHCPLeaseActionAdded,
		MAC:      "02:00:00:00:00:01",
		IP:       "192.0.2.128",
		Hostname: "client",
	})

	writeRuntimeConfig(t, configPath, initial)
	if _, err := d.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.stateMu.RLock()
	zones := d.zones
	d.stateMu.RUnlock()
	assertZoneARecord(t, zones, "client.lab.example.", "192.0.2.128")
	assertZonePTRRecord(t, zones, "128.2.0.192.in-addr.arpa.", "client.lab.example.")
	assertZoneNoPTRRecord(t, zones, "127.2.0.192.in-addr.arpa.")
}

func TestReloadStaticAndLeaseFileTakePrecedenceOverOldActiveEvent(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T, *resolvercfg.RuntimeConfig)
		wantIP    string
		wantPTR   string
	}{
		{
			name: "static",
			configure: func(t *testing.T, config *resolvercfg.RuntimeConfig) {
				config.Zones[0].Spec.Records = append(config.Zones[0].Spec.Records, api.DNSZoneRecordSpec{
					Hostname: "client",
					IPv4:     "192.0.2.129",
				})
			},
			wantIP:  "192.0.2.129",
			wantPTR: "client.lab.example.",
		},
		{
			name: "active lease file",
			configure: func(t *testing.T, config *resolvercfg.RuntimeConfig) {
				leaseFile := filepath.Join(t.TempDir(), "dnsmasq.leases")
				if err := os.WriteFile(leaseFile, []byte("1789306800 02:00:00:00:00:02 192.0.2.130 client *\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				config.Zones[0].Spec.DHCPDerived.LeaseFile = leaseFile
			},
			wantIP:  "192.0.2.130",
			wantPTR: "client.lab.example.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "resolver.json")
			initial := testResolverConfig([]int{5053})
			writeRuntimeConfig(t, configPath, initial)
			d := newTestDaemon(t, configPath, initial, true)
			d.zones.ApplyLease(dhcpLeaseEvent{
				Action:   daemonapi.DHCPLeaseActionAdded,
				MAC:      "02:00:00:00:00:01",
				IP:       "192.0.2.128",
				Hostname: "client",
			})

			updated := testResolverConfig([]int{5053})
			tt.configure(t, &updated)
			writeRuntimeConfig(t, configPath, updated)
			if _, err := d.reload(context.Background()); err != nil {
				t.Fatal(err)
			}
			d.stateMu.RLock()
			zones := d.zones
			d.stateMu.RUnlock()
			assertZoneARecord(t, zones, "client.lab.example.", tt.wantIP)
			ptr, err := dns.ReverseAddr(tt.wantIP)
			if err != nil {
				t.Fatal(err)
			}
			assertZonePTRRecord(t, zones, ptr, tt.wantPTR)
			assertZoneNoPTRRecord(t, zones, "128.2.0.192.in-addr.arpa.")
		})
	}
}

func TestActiveLeaseEventUpdatesLoadedLeaseFileBeforeReload(t *testing.T) {
	leaseFile := filepath.Join(t.TempDir(), "dnsmasq.leases")
	if err := os.WriteFile(leaseFile, []byte("1789306800 02:00:00:00:00:02 192.0.2.127 client *\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := testResolverConfig(nil)
	config.Zones[0].Spec.DHCPDerived.LeaseFile = leaseFile
	zones := newZoneTable(config.Zones)
	zones.ApplyLease(dhcpLeaseEvent{
		Action:   daemonapi.DHCPLeaseActionAdded,
		MAC:      "02:00:00:00:00:02",
		IP:       "192.0.2.128",
		Hostname: "client",
	})

	assertZoneARecord(t, zones, "client.lab.example.", "192.0.2.128")
	assertZonePTRRecord(t, zones, "128.2.0.192.in-addr.arpa.", "client.lab.example.")
	assertZoneNoPTRRecord(t, zones, "127.2.0.192.in-addr.arpa.")
}

func TestReloadPrefersUpdatedRecoveredLeaseOverOldDynamicRecord(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig([]int{5053})
	initial.Zones[0].DHCPHostRecords = []resolvercfg.RuntimeDHCPHostRecord{{
		Hostname: "leasehost",
		IP:       "192.0.2.127",
	}}
	writeRuntimeConfig(t, configPath, initial)
	d := newTestDaemon(t, configPath, initial, true)

	updated := testResolverConfig([]int{5053})
	updated.Zones[0].DHCPHostRecords = []resolvercfg.RuntimeDHCPHostRecord{{
		Hostname: "leasehost",
		IP:       "192.0.2.128",
	}}
	writeRuntimeConfig(t, configPath, updated)
	if _, err := d.reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	req := new(dns.Msg)
	req.SetQuestion("leasehost.lab.example.", dns.TypeA)
	d.stateMu.RLock()
	zones := d.zones
	d.stateMu.RUnlock()
	resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("updated recovered record missing: ok=%v resp=%v", ok, resp)
	}
	if a, isA := resp.Answer[0].(*dns.A); !isA || a.A.String() != "192.0.2.128" {
		t.Fatalf("answer after reload = %v, want 192.0.2.128", resp.Answer[0])
	}
	oldPTR := new(dns.Msg)
	oldPTR.SetQuestion("127.2.0.192.in-addr.arpa.", dns.TypePTR)
	if stale, answered := zones.Answer(oldPTR, []string{"DNSZone/lan-zone"}); answered {
		t.Fatalf("old PTR survived reload: %v", stale)
	}
	newPTR := new(dns.Msg)
	newPTR.SetQuestion("128.2.0.192.in-addr.arpa.", dns.TypePTR)
	ptrResp, answered := zones.Answer(newPTR, []string{"DNSZone/lan-zone"})
	if !answered || len(ptrResp.Answer) != 1 {
		t.Fatalf("updated PTR missing after reload: answered=%v resp=%v", answered, ptrResp)
	}
}

func assertZoneARecord(t *testing.T, zones *zoneTable, name, wantIP string) {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(name, dns.TypeA)
	resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("A record missing for %s: ok=%v resp=%v", name, ok, resp)
	}
	if a, isA := resp.Answer[0].(*dns.A); !isA || a.A.String() != wantIP {
		t.Fatalf("A record for %s = %v, want %s", name, resp.Answer[0], wantIP)
	}
}

func assertZonePTRRecord(t *testing.T, zones *zoneTable, name, wantTarget string) {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(name, dns.TypePTR)
	resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("PTR record missing for %s: ok=%v resp=%v", name, ok, resp)
	}
	if ptr, isPTR := resp.Answer[0].(*dns.PTR); !isPTR || ptr.Ptr != wantTarget {
		t.Fatalf("PTR record for %s = %v, want %s", name, resp.Answer[0], wantTarget)
	}
}

func assertZoneNoPTRRecord(t *testing.T, zones *zoneTable, name string) {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(name, dns.TypePTR)
	if resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"}); ok {
		t.Fatalf("unexpected PTR record for %s: %v", name, resp)
	}
}

func TestObservedStatusIncludesQueryLogStats(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	config := testResolverConfig([]int{5053})
	writeRuntimeConfig(t, configPath, config)
	d := newTestDaemon(t, configPath, config, true)
	queryLog, err := logstore.OpenDNSQueryLog(filepath.Join(t.TempDir(), "dns-queries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer queryLog.Close()
	d.queryLog = queryLog
	now := time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)
	if err := queryLog.Record(context.Background(), logstore.DNSQuery{
		Timestamp:     now,
		ClientAddress: "192.0.2.10",
		QuestionName:  "observed.example.",
		QuestionType:  "A",
		ResponseCode:  "NOERROR",
	}); err != nil {
		t.Fatal(err)
	}

	observed := d.observedStatus()
	if observed["queryLogEnabled"] != "true" {
		t.Fatalf("queryLogEnabled = %q", observed["queryLogEnabled"])
	}
	if observed["queryLogRecords"] != "1" || observed["queryLogRecordErrors"] != "0" {
		t.Fatalf("query log counts = %#v", observed)
	}
	if observed["queryLogDBBytes"] == "" || observed["queryLogWALBytes"] == "" {
		t.Fatalf("query log sizes missing: %#v", observed)
	}
	if observed["queryLogLastRecordAt"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("queryLogLastRecordAt = %q", observed["queryLogLastRecordAt"])
	}
}

func TestRecordQueryReopensClosedQueryLog(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	config := testResolverConfig([]int{5053})
	writeRuntimeConfig(t, configPath, config)
	d := newTestDaemon(t, configPath, config, true)
	path := filepath.Join(t.TempDir(), "dns-queries.db")
	queryLog, err := logstore.OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	d.queryLog = queryLog
	if err := queryLog.Close(); err != nil {
		t.Fatal(err)
	}

	req := new(dns.Msg)
	req.SetQuestion("reopen.example.", dns.TypeA)
	resp := new(dns.Msg)
	resp.SetReply(req)
	d.recordQuery("192.0.2.10:12345", req, resolveResult{Response: resp, ResponseCode: dns.RcodeToString[resp.Rcode]}, time.Millisecond)

	d.queryLogMu.Lock()
	reopened := d.queryLog
	d.queryLogMu.Unlock()
	defer d.closeQueryLog()
	if reopened == nil || reopened == queryLog {
		t.Fatal("query log was not reopened")
	}
	rows, err := reopened.List(context.Background(), logstore.DNSQueryFilter{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].QuestionName != "reopen.example" {
		t.Fatalf("unexpected query rows: %#v", rows)
	}
}

func testResolverConfig(ports []int) resolvercfg.RuntimeConfig {
	listen := make([]api.DNSResolverListenSpec, 0, len(ports))
	for _, port := range ports {
		listen = append(listen, api.DNSResolverListenSpec{
			Name:      fmt.Sprintf("listen-%d", port),
			Addresses: []string{"127.0.0.1"},
			Port:      port,
		})
	}
	return resolvercfg.RuntimeConfig{
		Resource: "lab",
		Spec: api.DNSResolverSpec{
			Listen: listen,
			Sources: []api.DNSResolverSourceSpec{{
				Name:    "zones",
				Kind:    "zone",
				Match:   []string{"lab.example"},
				ZoneRef: []string{"DNSZone/lan-zone"},
			}},
		},
		Zones: []resolvercfg.RuntimeZone{{
			Name: "lan-zone",
			Spec: api.DNSZoneSpec{
				Zone:        "lab.example",
				DHCPDerived: api.DNSZoneDHCPDerivedSpec{Sources: []string{"dhcpv4"}},
				Records: []api.DNSZoneRecordSpec{{
					Hostname: "router",
					IPv4:     "192.0.2.1",
				}},
			},
		}},
	}
}

func testForwardConfig(name, upstream string) resolvercfg.RuntimeConfig {
	return resolvercfg.RuntimeConfig{
		Resource: "lab",
		Spec: api.DNSResolverSpec{
			Listen: []api.DNSResolverListenSpec{{Name: "loopback", Addresses: []string{"127.0.0.1"}, Port: 5053}},
			Sources: []api.DNSResolverSourceSpec{{
				Name:      name,
				Kind:      "forward",
				Match:     []string{"."},
				Upstreams: []string{upstream},
			}},
		},
	}
}

func newTestDaemon(t *testing.T, configPath string, config resolvercfg.RuntimeConfig, dryRun bool) *daemon {
	t.Helper()
	d, err := newDaemon(options{
		resource:   "lab",
		configFile: configPath,
		stateFile:  filepath.Join(t.TempDir(), "state.json"),
		eventFile:  filepath.Join(t.TempDir(), "events.jsonl"),
		dryRun:     dryRun,
	}, config)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func writeRuntimeConfig(t *testing.T, path string, config resolvercfg.RuntimeConfig) {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func skipIfListenNotPermitted(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		t.Skipf("network listen is not permitted in this environment: %v", err)
	}
}
