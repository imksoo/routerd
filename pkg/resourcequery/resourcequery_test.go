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
