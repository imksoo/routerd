// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type mapStore map[string]map[string]any

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s[apiVersion+"/"+kind+"/"+name]
}

type statefulMapStore struct {
	mapStore
	vars map[string]routerstate.Value
	now  time.Time
}

func (s statefulMapStore) Get(name string) routerstate.Value {
	if value, ok := s.vars[name]; ok {
		return value
	}
	now := s.Now()
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: now, UpdatedAt: now}
}

func (s statefulMapStore) Age(name string) time.Duration {
	return s.Now().Sub(s.Get(name).Since)
}

func (s statefulMapStore) Now() time.Time {
	if s.now.IsZero() {
		return time.Now().UTC()
	}
	return s.now
}

func TestDependencyReadyUsesKindAPIVersion(t *testing.T) {
	store := mapStore{
		api.SystemAPIVersion + "/Package/router-runtime": {
			"phase": "Applied",
		},
	}

	if !DependencyReady(store, api.ResourceDependencySpec{
		Resource: "Package/router-runtime",
		Phase:    "Applied",
	}) {
		t.Fatalf("expected system resource dependency to be ready")
	}
}

func TestDependencyReadyUsesObservedPhase(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/DHCPv6PrefixDelegation/wan-pd": {
			"phase":  "Pending",
			"reason": "WhenFalse",
			"observed": map[string]any{
				"phase": "Bound",
			},
		},
	}

	if !DependencyReady(store, api.ResourceDependencySpec{
		Resource: "DHCPv6PrefixDelegation/wan-pd",
		Phase:    "Bound",
	}) {
		t.Fatalf("expected observed phase to satisfy dependency")
	}
}

func TestValueUsesKindAPIVersion(t *testing.T) {
	store := mapStore{
		api.SystemAPIVersion + "/Package/router-runtime": {
			"phase":       "Applied",
			"packageList": []string{"nftables", "conntrack"},
		},
	}

	values := Values(store, api.StatusValueSourceSpec{
		Resource: "Package/router-runtime",
		Field:    "packageList",
	})
	if len(values) != 2 || values[0] != "nftables" || values[1] != "conntrack" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestValuesFromRouterResolvesDNSZone(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "home"}, Spec: api.DNSZoneSpec{Zone: "home.internal"}},
	}}}

	values := ValuesFromRouter(router, api.StatusValueSourceSpec{Resource: "DNSZone/home", Field: "zone"})
	if len(values) != 1 || values[0] != "home.internal" {
		t.Fatalf("unexpected values: %#v", values)
	}
	if values := ValuesFromRouter(router, api.StatusValueSourceSpec{Resource: "DNSZone/home"}); len(values) != 0 {
		t.Fatalf("expected field to be explicit, got %#v", values)
	}
}

func TestValuesFromRouterResolvesIPv4StaticAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "172.18.0.2/16"}},
	}}}

	values := ValuesFromRouter(router, api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base", Field: "address"})
	if len(values) != 1 || values[0] != "172.18.0.2/16" {
		t.Fatalf("unexpected values: %#v", values)
	}
	if values := ValuesFromRouter(router, api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base"}); len(values) != 0 {
		t.Fatalf("expected field to be explicit, got %#v", values)
	}
}

func TestSourceReadyUsesKindAPIVersion(t *testing.T) {
	store := mapStore{
		api.FirewallAPIVersion + "/FirewallPolicy/default": {
			"phase": "Applied",
		},
	}

	if !SourceReady(store, "FirewallPolicy/default") {
		t.Fatalf("expected firewall resource source to be ready")
	}
}

func TestSourceReadyUsesObservedPhase(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {
			"phase": "Pending",
			"observed": map[string]any{
				"phase": "Up",
			},
		},
	}

	if !SourceReady(store, "DSLiteTunnel/ds-lite") {
		t.Fatalf("expected observed phase to make source ready")
	}
}

func TestResourceWhenMatchesObjectStatusField(t *testing.T) {
	now := time.Date(2026, 6, 4, 15, 30, 0, 0, time.UTC)
	store := statefulMapStore{
		mapStore: mapStore{
			api.NetAPIVersion + "/VirtualAddress/lan-vip": {
				"role":                 "master",
				"lastRoleTransitionAt": now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
			},
		},
		now: now,
	}
	when := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
		"VirtualAddress/lan-vip.role": {Equals: "master", For: "1m"},
	}}

	if !ResourceWhenMatches(when, store) {
		t.Fatalf("expected VirtualAddress status role to satisfy when")
	}
}

func TestFilterRouterByWhenUsesObjectStatusField(t *testing.T) {
	dhcp := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
		Metadata: api.ObjectMeta{Name: "lan"},
		Spec: api.DHCPv4ServerSpec{
			Interface: "lan",
			AddressPool: api.DHCPAddressPoolSpec{
				Start: "192.168.10.100",
				End:   "192.168.10.199",
			},
			When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
				"VirtualAddress/lan-vip.role": {Equals: "master"},
			}},
		},
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{dhcp}}}
	store := statefulMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}

	if got := FilterRouterByWhen(router, store); len(got.Spec.Resources) != 0 {
		t.Fatalf("backup role resources = %d, want 0", len(got.Spec.Resources))
	}
	store.mapStore[api.NetAPIVersion+"/VirtualAddress/lan-vip"]["role"] = "master"
	if got := FilterRouterByWhen(router, store); len(got.Spec.Resources) != 1 {
		t.Fatalf("master role resources = %d, want 1", len(got.Spec.Resources))
	}
}

func TestFilterRouterByWhenClearsFilteredBFDRef(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "rr"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/fabric",
				PeerASN:   64512,
				Peers:     []string{"10.99.0.2"},
				BFD:       "BFD/fabric",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"},
			Metadata: api.ObjectMeta{Name: "fabric"},
			Spec: api.BFDSpec{
				Peer: "BGPPeer/rr",
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"fabric.bfd.enabled": {Equals: "true"},
				}},
			},
		},
	}}}
	store := statefulMapStore{}

	got := FilterRouterByWhen(router, store)
	if len(got.Spec.Resources) != 1 {
		t.Fatalf("resources = %d, want only BGPPeer", len(got.Spec.Resources))
	}
	spec, err := got.Spec.Resources[0].BGPPeerSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.BFD != "" {
		t.Fatalf("BGPPeer BFD ref = %q, want cleared", spec.BFD)
	}
}

func TestFilterRouterByWhenPreservesImplicitBFDRef(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "rr"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/fabric",
				PeerASN:   64512,
				Peers:     []string{"10.99.0.2"},
				BFD:       "BFD/implicit",
			},
		},
	}}}

	got := FilterRouterByWhen(router, statefulMapStore{})
	if len(got.Spec.Resources) != 1 {
		t.Fatalf("resources = %d, want only BGPPeer", len(got.Spec.Resources))
	}
	spec, err := got.Spec.Resources[0].BGPPeerSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.BFD != "BFD/implicit" {
		t.Fatalf("BGPPeer BFD ref = %q, want implicit ref preserved", spec.BFD)
	}
}

func TestFilterRouterByWhenPrunesDNSForwarderForFilteredResolver(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.DNSResolverSpec{
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/lan-vip.role": {Equals: "master"},
				}},
				Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 53, Sources: []string{"default"}}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSUpstream"},
			Metadata: api.ObjectMeta{Name: "cloudflare"},
			Spec:     api.DNSUpstreamSpec{Protocol: "udp", Address: "1.1.1.1"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.DNSForwarderSpec{Resolver: "DNSResolver/lan", Match: []string{"."}, Upstreams: []string{"DNSUpstream/cloudflare"}},
		},
	}}}
	store := statefulMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}

	got := FilterRouterByWhen(router, store)
	if hasResource(got, "DNSResolver", "lan") {
		t.Fatal("DNSResolver/lan remained in backup role")
	}
	if hasResource(got, "DNSForwarder", "default") {
		t.Fatal("DNSForwarder/default remained after its resolver was filtered")
	}

	store.mapStore[api.NetAPIVersion+"/VirtualAddress/lan-vip"]["role"] = "master"
	got = FilterRouterByWhen(router, store)
	if !hasResource(got, "DNSResolver", "lan") {
		t.Fatal("DNSResolver/lan missing in master role")
	}
	if !hasResource(got, "DNSForwarder", "default") {
		t.Fatal("DNSForwarder/default missing in master role")
	}
}

func TestFilterRouterByWhenPrunesDNSResolverListenSourcesForFilteredForwarder(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 53, Sources: []string{"local", "default"}}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"},
			Metadata: api.ObjectMeta{Name: "local-zone"},
			Spec:     api.DNSZoneSpec{Zone: "home.internal"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"},
			Metadata: api.ObjectMeta{Name: "local"},
			Spec:     api.DNSForwarderSpec{Resolver: "DNSResolver/lan", Match: []string{"home.internal"}, ZoneRefs: []string{"DNSZone/local-zone"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSUpstream"},
			Metadata: api.ObjectMeta{Name: "cloudflare"},
			Spec:     api.DNSUpstreamSpec{Protocol: "udp", Address: "1.1.1.1"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSForwarder"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec: api.DNSForwarderSpec{
				Resolver:  "DNSResolver/lan",
				Match:     []string{"."},
				Upstreams: []string{"DNSUpstream/cloudflare"},
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/lan-vip.role": {Equals: "master"},
				}},
			},
		},
	}}}
	store := statefulMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}

	got := FilterRouterByWhen(router, store)
	res, ok := findResource(got, "DNSResolver", "lan")
	if !ok {
		t.Fatal("DNSResolver/lan missing")
	}
	spec, err := res.DNSResolverSpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Listen) != 1 || len(spec.Listen[0].Sources) != 1 || spec.Listen[0].Sources[0] != "local" {
		t.Fatalf("listen sources = %#v, want only local", spec.Listen)
	}
}

func TestFilterRouterByWhenClearsDNSZoneRecordSourceForFilteredResource(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "lan-vip"},
			Spec:     api.VirtualAddressSpec{Address: "172.18.0.1/32"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
			Metadata: api.ObjectMeta{Name: "lan-base"},
			Spec: api.IPv6DelegatedAddressSpec{
				Interface:        "lan",
				PrefixDelegation: "wan-pd",
				AddressSuffix:    "::1",
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/lan-vip.role": {Equals: "master"},
				}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"},
			Metadata: api.ObjectMeta{Name: "home"},
			Spec: api.DNSZoneSpec{
				Zone: "home.internal",
				Records: []api.DNSZoneRecordSpec{{
					Hostname: "router",
					IPv4From: api.StatusValueSourceSpec{Resource: "VirtualAddress/lan-vip", Field: "address"},
					IPv6From: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"},
				}},
			},
		},
	}}}
	store := statefulMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}

	got := FilterRouterByWhen(router, store)
	res, ok := findResource(got, "DNSZone", "home")
	if !ok {
		t.Fatal("DNSZone/home missing")
	}
	spec, err := res.DNSZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(spec.Records))
	}
	if spec.Records[0].IPv4From.Resource != "VirtualAddress/lan-vip" {
		t.Fatalf("IPv4From = %#v, want retained VirtualAddress ref", spec.Records[0].IPv4From)
	}
	if spec.Records[0].IPv6From.Resource != "" {
		t.Fatalf("IPv6From = %#v, want cleared filtered IPv6DelegatedAddress ref", spec.Records[0].IPv6From)
	}
}

func TestFilterRouterByWhenClearsFirewallZoneFilteredInterfaces(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "lan-vip"},
			Spec:     api.VirtualAddressSpec{Address: "172.18.0.1/32"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite-a"},
			Spec: api.DSLiteTunnelSpec{
				Interface: "wan",
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/lan-vip.role": {Equals: "master"},
				}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite-ra"},
			Spec:     api.DSLiteTunnelSpec{Interface: "wan"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"Interface/wan", "DSLiteTunnel/ds-lite-a", "DSLiteTunnel/ds-lite-ra"}},
		},
	}}}
	store := statefulMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}

	got := FilterRouterByWhen(router, store)
	res, ok := findResource(got, "FirewallZone", "wan")
	if !ok {
		t.Fatal("FirewallZone/wan missing")
	}
	spec, err := res.FirewallZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Interface/wan", "DSLiteTunnel/ds-lite-ra"}
	if !stringSlicesEqual(spec.Interfaces, want) {
		t.Fatalf("interfaces = %#v, want %#v", spec.Interfaces, want)
	}
}

func TestFilterRouterByWhenPreservesBareFirewallZoneInterfaceAliases(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan0"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "dslite0"},
			Spec:     api.DSLiteTunnelSpec{Interface: "wan0", TunnelName: "dslite0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
			Metadata: api.ObjectMeta{Name: "pppoe0"},
			Spec:     api.PPPoESessionSpec{Interface: "wan0", IfName: "pppoe0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan0", "dslite0", "pppoe0"}},
		},
	}}}

	got := FilterRouterByWhen(router, statefulMapStore{})
	res, ok := findResource(got, "FirewallZone", "wan")
	if !ok {
		t.Fatal("FirewallZone/wan missing")
	}
	spec, err := res.FirewallZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"wan0", "dslite0", "pppoe0"}
	if !stringSlicesEqual(spec.Interfaces, want) {
		t.Fatalf("interfaces = %#v, want %#v", spec.Interfaces, want)
	}
}

func TestFilterRouterByWhenPrunesBareFirewallZoneInterfaceAliasForFilteredResource(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "lan-vip"},
			Spec:     api.VirtualAddressSpec{Address: "172.18.0.1/32"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan0"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "dslite0"},
			Spec: api.DSLiteTunnelSpec{
				Interface:  "wan0",
				TunnelName: "dslite0",
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/lan-vip.role": {Equals: "master"},
				}},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan0", "dslite0"}},
		},
	}}}
	store := statefulMapStore{mapStore: mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-vip": {"role": "backup"},
	}}

	got := FilterRouterByWhen(router, store)
	res, ok := findResource(got, "FirewallZone", "wan")
	if !ok {
		t.Fatal("FirewallZone/wan missing")
	}
	spec, err := res.FirewallZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"wan0"}
	if !stringSlicesEqual(spec.Interfaces, want) {
		t.Fatalf("interfaces = %#v, want %#v", spec.Interfaces, want)
	}
}

func hasResource(router *api.Router, kind, name string) bool {
	_, ok := findResource(router, kind, name)
	return ok
}

func findResource(router *api.Router, kind, name string) (api.Resource, bool) {
	if router == nil {
		return api.Resource{}, false
	}
	for _, res := range router.Spec.Resources {
		if res.Kind == kind && res.Metadata.Name == name {
			return res, true
		}
	}
	return api.Resource{}, false
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
