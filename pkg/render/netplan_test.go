package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
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
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.160.3/24"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPAddress"},
				Metadata: api.ObjectMeta{Name: "wan-dhcp4"},
				Spec:     api.IPv4DHCPAddressSpec{Interface: "wan"},
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
		"- 192.168.160.3/24",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("netplan output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "ens18") {
		t.Fatalf("netplan output includes external interface:\n%s", got)
	}
}
