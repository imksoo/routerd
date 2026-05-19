// SPDX-License-Identifier: BSD-3-Clause

package netconfigbackend

import (
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/render"
)

func TestDeclarationsFromRouterCollectsAddressAndRouteShape(t *testing.T) {
	declarations, err := DeclarationsFromRouter(networkConfigRouter())
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations.Addresses) != 2 {
		t.Fatalf("addresses = %#v", declarations.Addresses)
	}
	if len(declarations.Routes) != 2 {
		t.Fatalf("routes = %#v", declarations.Routes)
	}
	if declarations.Addresses[0].Family != "ipv4" || declarations.Addresses[0].Interface != "lan" || declarations.Addresses[0].Address != "192.0.2.1/24" {
		t.Fatalf("ipv4 address declaration = %#v", declarations.Addresses[0])
	}
	if declarations.Routes[1].Family != "ipv6" || declarations.Routes[1].Via != "fe80::1" {
		t.Fatalf("ipv6 route declaration = %#v", declarations.Routes[1])
	}
}

func TestBackendsRenderThroughExistingNativeRenderers(t *testing.T) {
	router := networkConfigRouter()
	tests := []struct {
		name    string
		backend Backend
		want    string
	}{
		{name: "netplan", backend: Netplan{Path: "/etc/netplan/90-routerd.yaml"}, want: "addresses:\n        - 192.0.2.1/24"},
		{name: "networkd", backend: Networkd{}, want: "Destination=198.51.100.0/24"},
		{name: "nixos", backend: NixOS{}, want: `systemd.network.networks."10-netplan-ens19"`},
		{name: "rcconf", backend: RCConf{}, want: `ifconfig_ens19="inet 192.0.2.1/24"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := tt.backend.Render(router)
			if err != nil {
				t.Fatalf("render %s: %v", tt.name, err)
			}
			got := filesText(files)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("%s output missing %q:\n%s", tt.name, tt.want, got)
			}
		})
	}
}

func TestBackendDeclarationsMatchAcrossOSBackends(t *testing.T) {
	router := networkConfigRouter()
	want, err := DeclarationsFromRouter(router)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []Backend{Netplan{}, Networkd{}, NixOS{}, RCConf{}} {
		got, err := backend.Declarations(router)
		if err != nil {
			t.Fatalf("%s declarations: %v", backend.Name(), err)
		}
		if len(got.Addresses) != len(want.Addresses) || len(got.Routes) != len(want.Routes) {
			t.Fatalf("%s declarations = %#v, want %#v", backend.Name(), got, want)
		}
	}
}

func TestBackendsRejectInvalidOutputPaths(t *testing.T) {
	router := networkConfigRouter()
	tests := []Backend{
		Netplan{Path: "bad\x00netplan.yaml"},
		NixOS{Path: "bad\x00module.nix"},
		RCConf{Path: "bad\x00rc.conf"},
	}
	for _, backend := range tests {
		t.Run(backend.Name(), func(t *testing.T) {
			if _, err := backend.Render(router); err == nil {
				t.Fatalf("%s accepted invalid output path", backend.Name())
			}
		})
	}
}

func TestDeclarationsRejectInvalidIPv6RouteVia(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"}, Metadata: api.ObjectMeta{Name: "bad-via"}, Spec: api.IPv6StaticRouteSpec{Interface: "wan", Destination: "2001:db8::/64", Via: "not-an-ip"}},
	}}}
	if _, err := DeclarationsFromRouter(router); err == nil || !strings.Contains(err.Error(), "spec.via is invalid") {
		t.Fatalf("DeclarationsFromRouter error = %v, want invalid via", err)
	}
}

func TestDeclarationsPreserveEdgeCasesForSemanticReview(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan duplicate one"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan-alias", Address: "192.0.2.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-duplicate-two"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan-alias", Address: "192.0.2.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticRoute"}, Metadata: api.ObjectMeta{Name: strings.Repeat("r", 64)}, Spec: api.IPv4StaticRouteSpec{Interface: "wan.alias", Destination: "198.51.100.0/24", Via: "192.0.2.254"}},
	}}}
	declarations, err := DeclarationsFromRouter(router)
	if err != nil {
		t.Fatal(err)
	}
	if len(declarations.Addresses) != 2 || declarations.Addresses[0].Address != declarations.Addresses[1].Address {
		t.Fatalf("duplicate subnet declarations should be preserved for validator/controller review: %#v", declarations.Addresses)
	}
	if got := declarations.Routes[0].Interface; got != "wan.alias" {
		t.Fatalf("interface alias route declaration = %q", got)
	}
}

func TestNetworkConfigSemanticEquivalenceAcrossBackends(t *testing.T) {
	router := networkConfigRouter()
	want, err := DeclarationsFromRouter(router)
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []Backend{Netplan{}, Networkd{}, NixOS{}, RCConf{}} {
		t.Run(backend.Name(), func(t *testing.T) {
			got, err := backend.Declarations(router)
			if err != nil {
				t.Fatalf("declarations: %v", err)
			}
			if files, err := backend.Render(router); err != nil || len(files) == 0 {
				t.Fatalf("render %s files=%d err=%v", backend.Name(), len(files), err)
			}
			if len(got.Addresses) != len(want.Addresses) || len(got.Routes) != len(want.Routes) {
				t.Fatalf("%s declarations = %#v, want %#v", backend.Name(), got, want)
			}
		})
	}
}

func networkConfigRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-ip"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{Interface: "lan", PrefixDelegation: "wan-pd", AddressSuffix: "::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticRoute"}, Metadata: api.ObjectMeta{Name: "wan-route"}, Spec: api.IPv4StaticRouteSpec{Interface: "wan", Destination: "198.51.100.0/24", Via: "192.0.2.254", Metric: 100}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"}, Metadata: api.ObjectMeta{Name: "wan-v6-route"}, Spec: api.IPv6StaticRouteSpec{Interface: "wan", Destination: "2001:db8:1::/64", Via: "fe80::1", Metric: 200}},
	}}}
}

func filesText(files []render.File) string {
	var out strings.Builder
	for _, file := range files {
		out.WriteString("### ")
		out.WriteString(file.Path)
		out.WriteByte('\n')
		out.Write(file.Data)
		if len(file.Data) == 0 || file.Data[len(file.Data)-1] != '\n' {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
