// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFilterRouterByWhenPreservesVRFFirewallZoneAlias(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "tenant"}, Spec: api.VRFSpec{IfName: "vrf-tenant", RouteTable: 110, Members: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "tenant"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"tenant"}}},
	}}}

	filtered, err := FilterRouterByWhen(router, statefulMapStore{})
	if err != nil {
		t.Fatal(err)
	}
	zone, ok := findResource(filtered, "FirewallZone", "tenant")
	if !ok {
		t.Fatal("FirewallZone/tenant missing")
	}
	spec, err := zone.FirewallZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tenant"}; !reflect.DeepEqual(spec.Interfaces, want) {
		t.Fatalf("interfaces = %#v, want %#v", spec.Interfaces, want)
	}
}

func TestFilterRouterByWhenPreservesTunnelInterfaceFirewallZoneAliases(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "ipip"}, Spec: api.TunnelInterfaceSpec{Mode: "ipip", Local: "198.51.100.1", Remote: "198.51.100.2", TrustedUnderlay: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "ipip"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"ipip", "TunnelInterface/ipip"}}},
	}}}

	filtered, err := FilterRouterByWhen(router, statefulMapStore{})
	if err != nil {
		t.Fatal(err)
	}
	zone, ok := findResource(filtered, "FirewallZone", "ipip")
	if !ok {
		t.Fatal("FirewallZone/ipip missing")
	}
	spec, err := zone.FirewallZoneSpec()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ipip", "TunnelInterface/ipip"}; !reflect.DeepEqual(spec.Interfaces, want) {
		t.Fatalf("interfaces = %#v, want %#v", spec.Interfaces, want)
	}
}
