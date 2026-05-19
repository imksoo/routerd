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
