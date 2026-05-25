// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	resolverruntime "routerd/pkg/dnsresolver"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func TestReconcileWritesConfigAndSkipsReloadWhenSocketMissing(t *testing.T) {
	dir := t.TempDir()
	store := mapStore{}
	controller := Controller{
		Router:     dnsResolverRouter([]string{"127.0.0.1"}, nil),
		Store:      store,
		RuntimeDir: filepath.Join(dir, "run"),
		StateDir:   filepath.Join(dir, "state"),
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "state", "dns-resolver", "lan-resolver", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config was not written: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}
}

func TestReconcileReloadsResolverWhenConfigChanges(t *testing.T) {
	dir := t.TempDir()
	var reloads atomic.Int32
	withResolverHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/reload" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		reloads.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	store := mapStore{}
	controller := Controller{
		Router:     dnsResolverRouter([]string{"127.0.0.1"}, nil),
		Store:      store,
		RuntimeDir: filepath.Join(dir, "run"),
		StateDir:   filepath.Join(dir, "state"),
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reloads.Load(); got != 1 {
		t.Fatalf("reloads = %d, want 1", got)
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := reloads.Load(); got != 1 {
		t.Fatalf("unchanged config reloads = %d, want 1", got)
	}
}

func TestReconcileMarksDegradedWhenReloadFails(t *testing.T) {
	dir := t.TempDir()
	withResolverHTTPClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad config", http.StatusInternalServerError)
	}))

	store := mapStore{}
	controller := Controller{
		Router:     dnsResolverRouter([]string{"127.0.0.1"}, nil),
		Store:      store,
		RuntimeDir: filepath.Join(dir, "run"),
		StateDir:   filepath.Join(dir, "state"),
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Degraded" || !strings.Contains(status["reason"].(string), "ReloadFailed") {
		t.Fatalf("status = %#v", status)
	}
}

func withResolverHTTPClient(t *testing.T, handler http.Handler) {
	t.Helper()
	old := newResolverHTTPClient
	newResolverHTTPClient = func(_ string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			return rec.Result(), nil
		})}
	}
	t.Cleanup(func() {
		newResolverHTTPClient = old
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestReconcileResolvesListenAddressStatusRef(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/IPv6DelegatedAddress/lan-base": {"address": "2409:10:3d60:1200::1/64"},
	}
	controller := Controller{
		Router: dnsResolverRouter(nil, []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}
	resolved, _ := status["listenAddresses"].([]string)
	if len(resolved) != 1 || resolved[0] != "2409:10:3d60:1200::1" {
		t.Fatalf("listenAddresses = %#v", status["listenAddresses"])
	}
}

func TestReconcilePendingWhenListenAddressUnresolved(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: dnsResolverRouter(nil, []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Pending" {
		t.Fatalf("status = %#v", status)
	}
	if !strings.Contains(strings.TrimSpace(status["message"].(string)), "AddressUnresolved") {
		t.Fatalf("message = %#v", status["message"])
	}
}

func TestReconcileDegradedWhenOneListenAddressWaits(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: dnsResolverRouter([]string{"127.0.0.1"}, []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Degraded" {
		t.Fatalf("status = %#v", status)
	}
	resolved, _ := status["listenAddresses"].([]string)
	if len(resolved) != 1 || resolved[0] != "127.0.0.1" {
		t.Fatalf("listenAddresses = %#v", status["listenAddresses"])
	}
	waiting, _ := status["waiting"].([]map[string]string)
	if len(waiting) != 1 || waiting[0]["kind"] != "listen" || waiting[0]["source"] != "IPv6DelegatedAddress/lan-base" {
		t.Fatalf("waiting = %#v", status["waiting"])
	}

	spec, err := controller.Router.Spec.Resources[0].DNSResolverSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec = resolverruntime.NormalizeSpec(spec)
	spec, waiting, blockReason, err := controller.expandSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if blockReason != "" || len(waiting) != 1 {
		t.Fatalf("waiting=%#v blockReason=%q", waiting, blockReason)
	}
	if len(spec.Listen) != 1 || len(spec.Listen[0].Addresses) != 1 || spec.Listen[0].Addresses[0] != "127.0.0.1" || len(spec.Listen[0].AddressFrom) != 0 {
		t.Fatalf("listen = %#v", spec.Listen)
	}
}

func dnsResolverRouterWithUpstreamFrom(source api.StatusValueSourceSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{{
					Name:         "ngn-aftr",
					Kind:         "forward",
					Match:        []string{"gw.transix.jp"},
					UpstreamFrom: []api.StatusValueSourceSpec{source},
				}},
			},
		},
	}}}
}

func dnsResolverRouterWithPartialUpstreamFrom(source api.StatusValueSourceSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{
					{
						Name:      "default",
						Kind:      "upstream",
						Match:     []string{"."},
						Upstreams: []string{"udp://1.1.1.1:53"},
					},
					{
						Name:         "ngn-aftr",
						Kind:         "forward",
						Match:        []string{"gw.transix.jp"},
						UpstreamFrom: []api.StatusValueSourceSpec{source},
					},
				},
			},
		},
	}}}
}

// A forward source whose only upstream is derived from another resource's status
// (e.g. a DHCPv6Information server's dnsServers) must wait as Pending until that
// status is populated, not fail validation during bootstrap.
func TestReconcilePendingWhenUpstreamFromUnresolved(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: dnsResolverRouterWithUpstreamFrom(api.StatusValueSourceSpec{Resource: "DHCPv6Information/wan-info", Field: "dnsServers"}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Pending" {
		t.Fatalf("status = %#v", status)
	}
	if !strings.Contains(strings.TrimSpace(status["message"].(string)), "UpstreamUnresolved") {
		t.Fatalf("message = %#v", status["message"])
	}
}

func TestReconcileDegradedWhenOneSourceWaitsForUpstream(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: dnsResolverRouterWithPartialUpstreamFrom(api.StatusValueSourceSpec{Resource: "DHCPv6Information/wan-info", Field: "dnsServers"}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Degraded" {
		t.Fatalf("status = %#v", status)
	}
	waiting, _ := status["waiting"].([]map[string]string)
	if len(waiting) != 1 || waiting[0]["kind"] != "source" || waiting[0]["name"] != "ngn-aftr" || waiting[0]["source"] != "DHCPv6Information/wan-info" {
		t.Fatalf("waiting = %#v", status["waiting"])
	}

	spec, err := controller.Router.Spec.Resources[0].DNSResolverSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec = resolverruntime.NormalizeSpec(spec)
	spec, waiting, blockReason, err := controller.expandSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if blockReason != "" || len(waiting) != 1 {
		t.Fatalf("waiting=%#v blockReason=%q", waiting, blockReason)
	}
	if len(spec.Listen) != 1 || spec.Listen[0].Addresses[0] != "127.0.0.1" {
		t.Fatalf("listen = %#v", spec.Listen)
	}
	if len(spec.Sources) != 1 || spec.Sources[0].Name != "default" {
		t.Fatalf("sources = %#v", spec.Sources)
	}
}

func TestReconcileResolvesUpstreamFromStatus(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6Information/wan-info": {"dnsServers": []any{"2409:10:3d60:1200:1eb1:7fff:fe73:76d8"}},
	}
	controller := Controller{
		Router: dnsResolverRouterWithUpstreamFrom(api.StatusValueSourceSpec{Resource: "DHCPv6Information/wan-info", Field: "dnsServers"}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}
}

func TestReconcileConvergesFromDegradedWhenUpstreamResolves(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: dnsResolverRouterWithPartialUpstreamFrom(api.StatusValueSourceSpec{Resource: "DHCPv6Information/wan-info", Field: "dnsServers"}),
		Store:  store,
		DryRun: true,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Degraded" {
		t.Fatalf("status = %#v", status)
	}

	store[api.NetAPIVersion+"/DHCPv6Information/wan-info"] = map[string]any{"dnsServers": []any{"2409:10:3d60:1200:1eb1:7fff:fe73:76d8"}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.NetAPIVersion, "DNSResolver", "lan-resolver")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}

	spec, err := controller.Router.Spec.Resources[0].DNSResolverSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec = resolverruntime.NormalizeSpec(spec)
	spec, waiting, blockReason, err := controller.expandSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	if blockReason != "" || len(waiting) != 0 {
		t.Fatalf("waiting=%#v blockReason=%q", waiting, blockReason)
	}
	if len(spec.Sources) != 2 || spec.Sources[1].Name != "ngn-aftr" {
		t.Fatalf("sources = %#v", spec.Sources)
	}
	if len(spec.Sources[1].Upstreams) != 1 || spec.Sources[1].Upstreams[0] != "udp://[2409:10:3d60:1200:1eb1:7fff:fe73:76d8]:53" {
		t.Fatalf("ngn-aftr upstreams = %#v", spec.Sources[1].Upstreams)
	}
}

func TestDNSResolverDependsOnExplicitAddressSource(t *testing.T) {
	router := dnsResolverRouter([]string{"172.18.0.1"}, []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}})
	if !dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress", Name: "lan-base"}) {
		t.Fatal("expected dependency on IPv6DelegatedAddress/lan-base")
	}
	if dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress", Name: "other"}) {
		t.Fatal("unexpected dependency on IPv6DelegatedAddress/other")
	}
}

func TestRuntimeConfigResolvesDNSZoneRecordAddressSources(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/IPv6DelegatedAddress/lan-base": {"address": "2409:10:3d60:1200::1/64"},
	}
	controller := Controller{
		Router: dnsResolverRouterWithZone(api.DNSZoneRecordSpec{
			Hostname: "router",
			IPv4:     "192.168.160.5",
			IPv6From: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
		}),
		Store:  store,
		DryRun: true,
	}
	spec := api.DNSResolverSpec{
		Listen:  []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
		Sources: []api.DNSResolverSourceSpec{{Name: "local", Kind: "zone", Match: []string{"lab.example"}, ZoneRef: []string{"DNSZone/lan-zone"}}},
	}
	config, err := controller.runtimeConfig("lan-resolver", spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Zones) != 1 || len(config.Zones[0].Spec.Records) != 1 {
		t.Fatalf("zones = %#v", config.Zones)
	}
	record := config.Zones[0].Spec.Records[0]
	if record.IPv6 != "2409:10:3d60:1200::1" || record.IPv6From.Resource != "" {
		t.Fatalf("record = %#v", record)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSZone", "lan-zone")
	if status["phase"] != "Applied" {
		t.Fatalf("zone status = %#v", status)
	}
}

func TestRuntimeConfigMarksDNSZoneRecordPendingWhenSourceUnresolved(t *testing.T) {
	store := mapStore{}
	controller := Controller{
		Router: dnsResolverRouterWithZone(api.DNSZoneRecordSpec{
			Hostname: "router",
			IPv4:     "192.168.160.5",
			IPv6From: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
		}),
		Store:  store,
		DryRun: true,
	}
	spec := api.DNSResolverSpec{
		Listen:  []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
		Sources: []api.DNSResolverSourceSpec{{Name: "local", Kind: "zone", Match: []string{"lab.example"}, ZoneRef: []string{"DNSZone/lan-zone"}}},
	}
	config, err := controller.runtimeConfig("lan-resolver", spec)
	if err != nil {
		t.Fatal(err)
	}
	record := config.Zones[0].Spec.Records[0]
	if record.IPv6 != "" || record.IPv4 != "192.168.160.5" {
		t.Fatalf("record = %#v", record)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DNSZone", "lan-zone")
	if status["phase"] != "Pending" {
		t.Fatalf("zone status = %#v", status)
	}
	pending, _ := status["pendingRecords"].([]map[string]string)
	if len(pending) != 1 || pending[0]["field"] != "ipv6" {
		t.Fatalf("pendingRecords = %#v", status["pendingRecords"])
	}
}

func TestRuntimeConfigAddsHostnameRecordsFromVIPAndIngress(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/k8s-api-vip":             {"address": "192.168.123.250/32"},
		api.NetAPIVersion + "/VirtualAddress/k8s-api-vip-v6":          {"address": "fd00:1234::250/128"},
		api.FirewallAPIVersion + "/IngressService/kubernetes-api-alt": {"listenAddress": "192.168.123.251"},
	}
	controller := Controller{
		Router: dnsResolverRouterWithHostnameResources(),
		Store:  store,
		DryRun: true,
	}
	spec := api.DNSResolverSpec{
		Listen:  []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
		Sources: []api.DNSResolverSourceSpec{{Name: "local", Kind: "zone", Match: []string{"lain.local"}, ZoneRef: []string{"DNSZone/lan-zone"}}},
	}
	config, err := controller.runtimeConfig("lan-resolver", spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Zones) != 1 {
		t.Fatalf("zones = %#v", config.Zones)
	}
	records := map[string]string{}
	recordsV6 := map[string]string{}
	for _, record := range config.Zones[0].Spec.Records {
		records[record.Hostname] = record.IPv4
		recordsV6[record.Hostname] = record.IPv6
	}
	if records["k8s-api"] != "192.168.123.250" {
		t.Fatalf("k8s-api record = %#v", records)
	}
	if recordsV6["k8s-api-v6"] != "fd00:1234::250" {
		t.Fatalf("k8s-api-v6 record = %#v", recordsV6)
	}
	if records["k8s-api-alt"] != "192.168.123.251" {
		t.Fatalf("k8s-api-alt record = %#v", records)
	}
}

func TestRuntimeConfigKeepsExplicitDNSZoneRecordOverHostnameRecord(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/k8s-api-vip": {"address": "192.168.123.250/32"},
	}
	router := dnsResolverRouterWithHostnameResources()
	zoneSpec := router.Spec.Resources[0].Spec.(api.DNSZoneSpec)
	zoneSpec.Records = []api.DNSZoneRecordSpec{{Hostname: "k8s-api", IPv4: "192.168.123.10"}}
	router.Spec.Resources[0].Spec = zoneSpec
	controller := Controller{Router: router, Store: store, DryRun: true}
	spec := api.DNSResolverSpec{
		Listen:  []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
		Sources: []api.DNSResolverSourceSpec{{Name: "local", Kind: "zone", Match: []string{"lain.local"}, ZoneRef: []string{"DNSZone/lan-zone"}}},
	}
	config, err := controller.runtimeConfig("lan-resolver", spec)
	if err != nil {
		t.Fatal(err)
	}
	records := map[string]string{}
	for _, record := range config.Zones[0].Spec.Records {
		records[record.Hostname] = record.IPv4
	}
	if records["k8s-api"] != "192.168.123.10" {
		t.Fatalf("explicit record was overwritten: %#v", records)
	}
}

func TestRuntimeConfigSkipsExternalDNSHostnameRecords(t *testing.T) {
	store := mapStore{
		api.FirewallAPIVersion + "/IngressService/kubernetes-api-alt": {"listenAddress": "192.168.123.251"},
	}
	router := dnsResolverRouterWithHostnameResources()
	spec := router.Spec.Resources[4].Spec.(api.IngressServiceSpec)
	spec.ExternalDNS = true
	router.Spec.Resources[4].Spec = spec
	controller := Controller{Router: router, Store: store, DryRun: true}
	resolverSpec := api.DNSResolverSpec{
		Listen:  []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
		Sources: []api.DNSResolverSourceSpec{{Name: "local", Kind: "zone", Match: []string{"lain.local"}, ZoneRef: []string{"DNSZone/lan-zone"}}},
	}
	config, err := controller.runtimeConfig("lan-resolver", resolverSpec)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range config.Zones[0].Spec.Records {
		if record.Hostname == "k8s-api-alt" {
			t.Fatalf("externalDNS hostname record was exported: %#v", config.Zones[0].Spec.Records)
		}
	}
	if dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.FirewallAPIVersion, Kind: "IngressService", Name: "kubernetes-api-alt"}) {
		t.Fatal("externalDNS resource should not trigger DNSResolver dependency")
	}
}

func TestDNSResolverDependsOnDNSZoneRecordSource(t *testing.T) {
	router := dnsResolverRouterWithZone(api.DNSZoneRecordSpec{
		Hostname: "router",
		IPv6From: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
	})
	if !dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress", Name: "lan-base"}) {
		t.Fatal("expected dependency on IPv6DelegatedAddress/lan-base")
	}
}

func TestDNSResolverDependsOnHostnameResources(t *testing.T) {
	router := dnsResolverRouterWithHostnameResources()
	if !dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress", Name: "k8s-api-vip"}) {
		t.Fatal("expected dependency on VirtualAddress/k8s-api-vip")
	}
	if !dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress", Name: "k8s-api-vip-v6"}) {
		t.Fatal("expected dependency on VirtualAddress/k8s-api-vip-v6")
	}
	if !dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.FirewallAPIVersion, Kind: "IngressService", Name: "kubernetes-api-alt"}) {
		t.Fatal("expected dependency on IngressService/kubernetes-api-alt")
	}
}

func dnsResolverRouter(addresses []string, addressSources []api.StatusValueSourceSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Name: "lan", Addresses: addresses, AddressFrom: addressSources, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{{
					Name:      "default",
					Kind:      "upstream",
					Match:     []string{"."},
					Upstreams: []string{"udp://1.1.1.1:53"},
				}},
			},
		},
	}}}
}

func dnsResolverRouterWithZone(record api.DNSZoneRecordSpec) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"},
			Metadata: api.ObjectMeta{Name: "lan-zone"},
			Spec: api.DNSZoneSpec{
				Zone:    "lab.example",
				Records: []api.DNSZoneRecordSpec{record},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{{
					Name:    "local",
					Kind:    "zone",
					Match:   []string{"lab.example"},
					ZoneRef: []string{"DNSZone/lan-zone"},
				}},
			},
		},
	}}}
}

func dnsResolverRouterWithHostnameResources() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"},
			Metadata: api.ObjectMeta{Name: "lan-zone"},
			Spec: api.DNSZoneSpec{
				Zone: "lain.local",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{{
					Name:    "local",
					Kind:    "zone",
					Match:   []string{"lain.local"},
					ZoneRef: []string{"DNSZone/lan-zone"},
				}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "k8s-api-vip"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "192.168.123.250/32",
				Hostname:  "k8s-api.lain.local",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "k8s-api-vip-v6"},
			Spec: api.VirtualAddressSpec{Family: "ipv6",
				Interface: "lan",
				Address:   "fd00:1234::250/128",
				Hostname:  "k8s-api-v6.lain.local",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
			Metadata: api.ObjectMeta{Name: "kubernetes-api-alt"},
			Spec: api.IngressServiceSpec{
				Listen:   api.IngressListenSpec{Interface: "lan", Address: "192.168.123.251", Protocol: "tcp", Port: 6443},
				Hostname: "k8s-api-alt.lain.local",
				Backends: []api.IngressBackendSpec{{Name: "cp-01", Address: "192.168.123.11", Port: 6443}},
			},
		},
	}}}
}
