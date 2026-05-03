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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "lan-bridge"}, Spec: api.BridgeSpec{IfName: "bridge0", Members: []string{"lan", "home-vxlan"}, RSTP: boolPtr(true)}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "home-vxlan"}, Spec: api.VXLANSegmentSpec{IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.10", Remotes: []string{"192.0.2.20"}, UnderlayInterface: "wan", UDPPort: 4789, MTU: 1450, Bridge: "lan-bridge"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Address"}, Metadata: api.ObjectMeta{Name: "wan-dhcpv4"}, Spec: api.DHCPv4AddressSpec{Interface: "wan", Client: "dhclient"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Address"}, Metadata: api.ObjectMeta{Name: "mgmt-dhcpv4"}, Spec: api.DHCPv4AddressSpec{Interface: "mgmt", Client: "dhclient", UseRoutes: &disabled, UseDNS: &disabled}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Address"}, Metadata: api.ObjectMeta{Name: "wan-dhcpv6"}, Spec: api.DHCPv6AddressSpec{Interface: "wan", Client: "dhcp6c"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", Client: "dhcp6c", Profile: "ntt-hgw-lan-pd", PrefixLength: 60, IAID: "00000001"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"}, Metadata: api.ObjectMeta{Name: "wan-pppoe"}, Spec: api.PPPoEInterfaceSpec{Interface: "wan", IfName: "ppp0", Username: "user@example.jp", Password: "secret", Managed: true, DefaultRoute: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv4"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "bridge-ipv4"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan-bridge", Address: "192.0.2.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticRoute"}, Metadata: api.ObjectMeta{Name: "lab-v4"}, Spec: api.IPv4StaticRouteSpec{Interface: "lan", Destination: "192.0.2.0/24", Via: "192.168.10.254"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6StaticRoute"}, Metadata: api.ObjectMeta{Name: "lab-v6"}, Spec: api.IPv6StaticRouteSpec{Interface: "wan", Destination: "2001:db8:1::/64", Via: "fe80::1"}},
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
		`ifconfig_vtnet1="inet 192.168.10.1/24"`,
		`mpd_enable="YES"`,
		`mpd_flags="-b"`,
		`cloned_interfaces="vxlan100 bridge0"`,
		`ifconfig_vxlan100="vxlanid 100 vxlanlocal 192.0.2.10 vxlanremote 192.0.2.20 vxlandev vtnet0 vxlanport 4789 mtu 1450 up"`,
		`ifconfig_bridge0="addm vtnet1 stp vtnet1 addm vxlan100 stp vxlan100 up"`,
		`ifconfig_bridge0_alias0="inet 192.0.2.1/24"`,
		`static_routes="lab_v4"`,
		`route_lab_v4="-net 192.0.2.0/24 192.168.10.254"`,
		`ipv6_static_routes="lab_v6"`,
		`ipv6_route_lab_v6="2001:db8:1::/64 fe80::1%vtnet0"`,
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
	for _, unwanted := range []string{"dhcp6c_enable", "dhcp6c_interfaces", "dhcp6c_flags"} {
		if strings.Contains(rc, unwanted) {
			t.Fatalf("FreeBSD rc.conf must not render legacy DHCPv6 client key %q:\n%s", unwanted, rc)
		}
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

func TestFreeBSDIgnoresPrefixDelegationClientRenderer(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: true, Owner: "routerd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan", Client: "dhcpcd", Profile: "ntt-hgw-lan-pd", PrefixLength: 60}},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	if strings.Contains(string(got.RCConf), "dhcp6c_") {
		t.Fatalf("FreeBSD rc.conf must not render legacy dhcp6c runtime details:\n%s", got.RCConf)
	}
}

func TestFreeBSDVXLANMultipleRemotesEmitsWarningAndUsesSeed(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: false, Owner: "external"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"}, Metadata: api.ObjectMeta{Name: "lab"}, Spec: api.VXLANSegmentSpec{
			IfName: "vxlan100", VNI: 100, LocalAddress: "192.0.2.10",
			Remotes:           []string{"192.0.2.20", "192.0.2.30", "192.0.2.40"},
			UnderlayInterface: "wan",
		}},
	}}}
	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("expected at least one warning for multi-remote VXLAN on FreeBSD")
	}
	want := "FreeBSD vxlan(4) supports a single unicast remote"
	if !strings.Contains(got.Warnings[0], want) {
		t.Fatalf("warning %q does not mention single-remote limitation", got.Warnings[0])
	}
	if !strings.Contains(string(got.RCConf), "vxlanremote 192.0.2.20") {
		t.Fatalf("FreeBSD rc.conf must use the first remote as seed:\n%s", got.RCConf)
	}
	if strings.Contains(string(got.RCConf), "vxlanremote 192.0.2.30") || strings.Contains(string(got.RCConf), "vxlanremote 192.0.2.40") {
		t.Fatalf("FreeBSD rc.conf must not emit additional remotes:\n%s", got.RCConf)
	}
}
