// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"context"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
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
		api.NetAPIVersion + "/VirtualIPv4Address/k8s-api-vip":         {"address": "192.168.123.250/32"},
		api.NetAPIVersion + "/VirtualIPv6Address/k8s-api-vip-v6":      {"address": "fd00:1234::250/128"},
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
		api.NetAPIVersion + "/VirtualIPv4Address/k8s-api-vip": {"address": "192.168.123.250/32"},
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
	if !dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address", Name: "k8s-api-vip"}) {
		t.Fatal("expected dependency on VirtualIPv4Address/k8s-api-vip")
	}
	if !dnsResolverDependsOn(router, daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv6Address", Name: "k8s-api-vip-v6"}) {
		t.Fatal("expected dependency on VirtualIPv6Address/k8s-api-vip-v6")
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
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"},
			Metadata: api.ObjectMeta{Name: "k8s-api-vip"},
			Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "192.168.123.250/32",
				Hostname:  "k8s-api.lain.local",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv6Address"},
			Metadata: api.ObjectMeta{Name: "k8s-api-vip-v6"},
			Spec: api.VirtualIPv6AddressSpec{
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
