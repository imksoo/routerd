// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"

	"routerd/pkg/api"
	resolvercfg "routerd/pkg/dnsresolver"
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

func TestReloadAddsListenAddressWithoutRebindingExisting(t *testing.T) {
	port1 := freeTCPPort(t)
	port2 := freeTCPPort(t)
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig([]int{port1})
	writeRuntimeConfig(t, configPath, initial)
	d := newTestDaemon(t, configPath, initial, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := d.startDNS(ctx); err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	defer d.shutdownDNSServers()

	addr1 := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port1))
	addr2 := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port2))
	assertDNSAnswer(t, addr1, "router.lab.example.", "192.0.2.1")
	d.stateMu.RLock()
	oldListener := d.listeners[addr1]
	d.stateMu.RUnlock()

	updated := testResolverConfig([]int{port1, port2})
	writeRuntimeConfig(t, configPath, updated)
	summary, err := d.reload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Listeners != 2 {
		t.Fatalf("listeners = %d, want 2", summary.Listeners)
	}
	d.stateMu.RLock()
	sameListener := d.listeners[addr1] == oldListener
	d.stateMu.RUnlock()
	if !sameListener {
		t.Fatalf("existing listener was rebound")
	}
	assertDNSAnswer(t, addr1, "router.lab.example.", "192.0.2.1")
	assertDNSAnswer(t, addr2, "router.lab.example.", "192.0.2.1")
}

func TestReloadRemovesListenAddress(t *testing.T) {
	port1 := freeTCPPort(t)
	port2 := freeTCPPort(t)
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig([]int{port1, port2})
	writeRuntimeConfig(t, configPath, initial)
	d := newTestDaemon(t, configPath, initial, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := d.startDNS(ctx); err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	defer d.shutdownDNSServers()

	addr1 := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port1))
	addr2 := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port2))
	assertDNSAnswer(t, addr1, "router.lab.example.", "192.0.2.1")
	assertDNSAnswer(t, addr2, "router.lab.example.", "192.0.2.1")

	updated := testResolverConfig([]int{port1})
	writeRuntimeConfig(t, configPath, updated)
	summary, err := d.reload(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Listeners != 1 {
		t.Fatalf("listeners = %d, want 1", summary.Listeners)
	}
	assertDNSAnswer(t, addr1, "router.lab.example.", "192.0.2.1")
	assertDNSNoResponse(t, addr2, "router.lab.example.")
}

func TestReloadInvalidConfigKeepsServingOldState(t *testing.T) {
	port := freeTCPPort(t)
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig([]int{port})
	writeRuntimeConfig(t, configPath, initial)
	d := newTestDaemon(t, configPath, initial, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := d.startDNS(ctx); err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	defer d.shutdownDNSServers()

	addr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", port))
	assertDNSAnswer(t, addr, "router.lab.example.", "192.0.2.1")
	d.stateMu.RLock()
	oldListener := d.listeners[addr]
	d.stateMu.RUnlock()

	writeRuntimeConfig(t, configPath, resolvercfg.RuntimeConfig{
		Resource: "lab",
		Spec: api.DNSResolverSpec{
			Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: port}},
		},
	})
	if _, err := d.reload(ctx); err == nil {
		t.Fatal("reload succeeded with invalid config")
	}
	d.stateMu.RLock()
	sameListener := d.listeners[addr] == oldListener
	d.stateMu.RUnlock()
	if !sameListener {
		t.Fatalf("listener changed after failed reload")
	}
	assertDNSAnswer(t, addr, "router.lab.example.", "192.0.2.1")
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
	d.zones.ApplyLease(dhcpLeaseEvent{Action: "add", MAC: "02:00:00:00:00:01", IP: "192.0.2.55", Hostname: "leasehost"})

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

func TestReloadHandlerStatusCodes(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig([]int{5053})
	writeRuntimeConfig(t, configPath, initial)
	d := newTestDaemon(t, configPath, initial, true)

	okReq := httptest.NewRequest(http.MethodPost, "/v1/reload", nil)
	okResp := httptest.NewRecorder()
	d.reloadHandler(okResp, okReq)
	if okResp.Code != http.StatusOK {
		t.Fatalf("success status = %d body=%s", okResp.Code, okResp.Body.String())
	}
	var summary reloadSummary
	if err := json.Unmarshal(okResp.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.Reloaded || summary.Sources != 1 {
		t.Fatalf("summary = %#v", summary)
	}

	writeRuntimeConfig(t, configPath, resolvercfg.RuntimeConfig{
		Resource: "lab",
		Spec: api.DNSResolverSpec{
			Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 5053}},
		},
	})
	failReq := httptest.NewRequest(http.MethodPost, "/v1/reload", nil)
	failResp := httptest.NewRecorder()
	d.reloadHandler(failResp, failReq)
	if failResp.Code != http.StatusBadRequest {
		t.Fatalf("failure status = %d body=%s", failResp.Code, failResp.Body.String())
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

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var out int
	if _, err := fmt.Sscanf(port, "%d", &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertDNSAnswer(t *testing.T, addr, qname, want string) {
	t.Helper()
	var lastErr error
	for i := 0; i < 20; i++ {
		req := new(dns.Msg)
		req.SetQuestion(qname, dns.TypeA)
		resp, _, err := (&dns.Client{Net: "udp", Timeout: 100 * time.Millisecond}).Exchange(req, addr)
		if err == nil && resp != nil && len(resp.Answer) == 1 {
			if a, ok := resp.Answer[0].(*dns.A); ok && a.A.String() == want {
				return
			}
			t.Fatalf("answer = %v, want %s", resp.Answer, want)
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("DNS query %s via %s failed: %v", qname, addr, lastErr)
}

func assertDNSNoResponse(t *testing.T, addr, qname string) {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(qname, dns.TypeA)
	resp, _, err := (&dns.Client{Net: "udp", Timeout: 100 * time.Millisecond}).Exchange(req, addr)
	if err == nil && resp != nil {
		t.Fatalf("unexpected DNS response via removed listener: %v", resp)
	}
}

func skipIfListenNotPermitted(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		t.Skipf("network listen is not permitted in this environment: %v", err)
	}
}
