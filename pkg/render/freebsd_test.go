package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestFreeBSDRendersRouter01Basics(t *testing.T) {
	disabled := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: true, Owner: "routerd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vtnet1", Managed: true, Owner: "routerd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "mgmt"}, Spec: api.InterfaceSpec{IfName: "vtnet2", Managed: true, Owner: "routerd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPAddress"}, Metadata: api.ObjectMeta{Name: "wan-dhcp4"}, Spec: api.IPv4DHCPAddressSpec{Interface: "wan", Client: "dhclient"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4DHCPAddress"}, Metadata: api.ObjectMeta{Name: "mgmt-dhcp4"}, Spec: api.IPv4DHCPAddressSpec{Interface: "mgmt", Client: "dhclient", UseRoutes: &disabled, UseDNS: &disabled}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DHCPAddress"}, Metadata: api.ObjectMeta{Name: "wan-dhcp6"}, Spec: api.IPv6DHCPAddressSpec{Interface: "wan", Client: "dhcp6c"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.IPv6PrefixDelegationSpec{Interface: "wan", Client: "dhcp6c", Profile: "ntt-hgw-lan-pd", PrefixLength: 60, IAID: "00000001"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"}, Metadata: api.ObjectMeta{Name: "wan-pppoe"}, Spec: api.PPPoEInterfaceSpec{Interface: "wan", IfName: "ppp0", Username: "user@example.jp", Password: "secret", Managed: true, DefaultRoute: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv4"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "0", AddressSuffix: "::1", Announce: true}},
	}}}

	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	rc := string(got.RCConf)
	for _, want := range []string{
		`ifconfig_vtnet0="DHCP"`,
		`ifconfig_vtnet2="DHCP"`,
		`ifconfig_vtnet0_ipv6="inet6 accept_rtadv"`,
		`ifconfig_vtnet1="inet 192.168.10.1/24"`,
		`dhcp6c_enable="YES"`,
		`dhcp6c_interfaces="vtnet0"`,
		`dhcp6c_flags="-n"`,
		`mpd_enable="YES"`,
		`mpd_flags="-b"`,
	} {
		if !strings.Contains(rc, want) {
			t.Fatalf("rc.conf output missing %q:\n%s", want, rc)
		}
	}
	dhclient := string(got.DHCPClient)
	for _, want := range []string{
		`interface "vtnet2"`,
		`ignore routers, domain-name, domain-name-servers, domain-search;`,
		"}",
	} {
		if !strings.Contains(dhclient, want) {
			t.Fatalf("dhclient output missing %q:\n%s", want, dhclient)
		}
	}
	dhcp6c := string(got.DHCP6C)
	for _, want := range []string{
		"interface vtnet0",
		"send ia-pd 1",
		"id-assoc pd 1",
		"prefix-interface vtnet1",
		"sla-len 4",
	} {
		if !strings.Contains(dhcp6c, want) {
			t.Fatalf("dhcp6c output missing %q:\n%s", want, dhcp6c)
		}
	}
	if strings.Contains(dhcp6c, "ifid") {
		t.Fatalf("dhcp6c output must not include unsupported ifid statement:\n%s", dhcp6c)
	}
	if strings.Contains(dhcp6c, "prefix ::/60;") {
		t.Fatalf("dhcp6c output must not render unsupported length-only prefix hints:\n%s", dhcp6c)
	}
	if strings.Contains(dhcp6c, "prefix 2001:db8") {
		t.Fatalf("dhcp6c output must not render explicit prefix hints for the NTT profile:\n%s", dhcp6c)
	}
	mpd5 := string(got.MPD5)
	for _, want := range []string{
		"default:",
		"load routerd_wan_pppoe",
		"routerd_wan_pppoe:",
		"create bundle static Bwan_pppoe",
		"set iface name ppp0",
		"set iface route default",
		"create link static Lwan_pppoe pppoe",
		`set auth authname "user@example.jp"`,
		`set auth password "secret"`,
		"set pppoe iface vtnet0",
		"open",
	} {
		if !strings.Contains(mpd5, want) {
			t.Fatalf("mpd5 output missing %q:\n%s", want, mpd5)
		}
	}
}
