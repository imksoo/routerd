package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/config"
)

func TestNetplanRendersOnlyRouterdManagedInterfaces(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd", AdminUp: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv4"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.3/24"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Address"},
				Metadata: api.ObjectMeta{Name: "wan-dhcpv4"},
				Spec:     api.DHCPv4AddressSpec{Interface: "wan"},
			},
		}},
	}

	data, err := Netplan(router)
	if err != nil {
		t.Fatalf("render netplan: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"renderer: networkd",
		"ens19:",
		"dhcp4: false",
		"dhcp6: false",
		"accept-ra: false",
		"link-local: []",
		"- 192.168.10.3/24",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("netplan output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ens18") {
		t.Fatalf("netplan output includes external interface:\n%s", got)
	}
}

func TestNetplanReturnsNilWhenNoInterfacesAreManaged(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
		}},
	}
	data, err := Netplan(router)
	if err != nil {
		t.Fatalf("render netplan: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("netplan output = %q, want empty", string(data))
	}
}

func TestNetplanEnablesIPv6LinkLocalForDelegatedAddress(t *testing.T) {
	router, err := config.Load("../../examples/router-lab.yaml")
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	data, err := Netplan(router)
	if err != nil {
		t.Fatalf("render netplan: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "link-local:\n        - ipv6") {
		t.Fatalf("netplan output does not enable IPv6 link-local:\n%s", got)
	}
}

func TestNetplanRendersDHCPv4Overrides(t *testing.T) {
	disabled := false
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "mgmt"},
				Spec:     api.InterfaceSpec{IfName: "ens20", Managed: true, Owner: "routerd", AdminUp: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Address"},
				Metadata: api.ObjectMeta{Name: "mgmt-dhcpv4"},
				Spec:     api.DHCPv4AddressSpec{Interface: "mgmt", Client: "networkd", UseRoutes: &disabled, UseDNS: &disabled, RouteMetric: 900},
			},
		}},
	}
	data, err := Netplan(router)
	if err != nil {
		t.Fatalf("render netplan: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ens20:",
		"dhcp4: true",
		"dhcp4-overrides:",
		"use-routes: false",
		"use-dns: false",
		"route-metric: 900",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("netplan output missing %q:\n%s", want, got)
		}
	}
}
