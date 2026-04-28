package render

import (
	"strings"
	"testing"
	"time"

	"routerd/pkg/api"
	routerstate "routerd/pkg/state"
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.IPv6PrefixDelegationSpec{Interface: "wan", Client: "dhcp6c", Profile: "ntt-hgw-lan-pd", PrefixLength: 60, IAID: "ca53095a"}},
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
		"send ia-pd 3394439514",
		"id-assoc pd 3394439514",
		"prefix ::/60 14400 14400;",
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

func TestFreeBSDRendersPrefixHintFromState(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: true, Owner: "routerd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vtnet1", Managed: true, Owner: "routerd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.IPv6PrefixDelegationSpec{Interface: "wan", Client: "dhcp6c", Profile: "ntt-hgw-lan-pd", PrefixLength: 60, IAID: "ca53095a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv6"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", SubnetID: "0", AddressSuffix: "::1", Announce: true}},
	}}}
	store := routerstate.New()
	lease := routerstate.PDLease{
		LastPrefix:     "2001:db8:1234:1240::/60",
		ValidLifetime:  "14400",
		LastObservedAt: time.Now().UTC().Format(time.RFC3339),
	}
	store.Set("ipv6PrefixDelegation.wan-pd.lease", routerstate.EncodePDLease(lease), "test")

	got, err := FreeBSDWithStateAndPPPoEPasswords(router, store, func(_ api.Resource, spec api.PPPoEInterfaceSpec) (string, error) {
		return spec.Password, nil
	})
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	dhcp6c := string(got.DHCP6C)
	if !strings.Contains(dhcp6c, "prefix 2001:db8:1234:1240::/60 14400 14400;") {
		t.Fatalf("dhcp6c output missing prefix hint:\n%s", dhcp6c)
	}
}

func TestFreeBSDRendersExplicitPrefixHintLifetimes(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vtnet0", Managed: true, Owner: "routerd"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.IPv6PrefixDelegationSpec{
			Interface:         "wan",
			Client:            "dhcp6c",
			Profile:           "ntt-hgw-lan-pd",
			PrefixLength:      60,
			PreferredLifetime: "7200",
			ValidLifetime:     "86400",
		}},
	}}}

	got, err := FreeBSD(router)
	if err != nil {
		t.Fatalf("render FreeBSD: %v", err)
	}
	if dhcp6c := string(got.DHCP6C); !strings.Contains(dhcp6c, "prefix ::/60 7200 86400;") {
		t.Fatalf("dhcp6c output missing explicit prefix hint lifetimes:\n%s", dhcp6c)
	}
}
