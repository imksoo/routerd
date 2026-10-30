// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"encoding/json"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"gopkg.in/yaml.v3"
)

func TestPublicNetworkKindsCrossActualYAMLAndJSONAdmission(t *testing.T) {
	for _, resource := range []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "bridge"}, Spec: api.BridgeSpec{IfName: "bridge0", Members: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "vx"}, Spec: api.VXLANSegmentSpec{IfName: "vxlan1", VNI: 42, LocalAddress: "192.0.2.1", Remotes: []string{"192.0.2.2"}, UnderlayInterface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"}, Metadata: api.ObjectMeta{Name: "route6"}, Spec: api.IPv6StaticRouteSpec{Interface: "wan", Destination: "2001:db8:42::/64", Via: "fe80::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "route4"}, Spec: api.IPv4RouteSpec{Device: "eth0", Destination: "192.0.2.0/24", Gateway: "198.51.100.1"}},
	} {
		router := api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth0", Managed: true}}, resource}}}
		for _, encode := range []func(any) ([]byte, error){json.Marshal, yaml.Marshal} {
			encoded, err := encode(router)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadBytes(encoded, "operator")
			if err != nil {
				t.Fatalf("%s actual decode: %v", resource.Kind, err)
			}
			for _, target := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
				if err := ValidateForOS(loaded, target); err != nil {
					t.Fatalf("%s %s actual admission: %v", target, resource.Kind, err)
				}
			}
		}
	}
}

func TestPrivateIPv4RouteAliasOnlyAcceptsTrustedEffectiveInput(t *testing.T) {
	router := api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticRoute"}, Metadata: api.ObjectMeta{Name: "private"}, Spec: api.IPv4StaticRouteSpec{Interface: "wan", Destination: "192.0.2.0/24", Via: "198.51.100.1"}},
	}}}
	for _, encode := range []func(any) ([]byte, error){json.Marshal, yaml.Marshal} {
		data, err := encode(router)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := LoadBytes(data, "operator"); err == nil {
			t.Fatal("private route alias accepted as operator intent")
		}
	}
	if err := ValidateForOS(&router, platform.OSLinux); err == nil {
		t.Fatal("programmatic static admission bypassed private alias rejection")
	}
	if err := ValidateEffectiveForOS(&router, platform.OSLinux); err != nil {
		t.Fatalf("trusted ClusterNetworkRoute lowering rejected: %v", err)
	}
}
