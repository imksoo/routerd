// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

// The generated firewall must isolate local DHCPv4 delivery without disabling
// DHCPv6/RA in dnsmasq. Actual packet exchange is checked under tests/netns.
func TestNftablesIPv6OnlyDHCPListener(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		v4, relay, shared, raOnly, pendingV4, delegated bool
		exclude                                         bool
	}{
		{name: "split DHCP families", v4: true, exclude: true},
		{name: "shared physical interface via alias", v4: true, shared: true},
		{name: "RA only", raOnly: true, exclude: true},
		{name: "IPv4 relay and IPv6", relay: true},
		{name: "pending shared IPv4 scope", v4: true, shared: true, pendingV4: true},
		{name: "delegated IPv6 scope", delegated: true, exclude: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v4name := "lan-parent"
			if tc.shared {
				v4name = "lan-vrrp"
			}
			resources := []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: v4name}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.InterfaceSpec{IfName: "lan-vrrp"}},
			}
			if tc.v4 {
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "v4"}, Spec: api.DHCPv4ServerSpec{Interface: "lan", AddressPool: api.DHCPAddressPoolSpec{Start: "192.0.2.100", End: "192.0.2.110"}}})
			}
			if tc.pendingV4 {
				spec, _ := resources[len(resources)-1].DHCPv4ServerSpec()
				spec.GatewayFrom = api.StatusValueSourceSpec{Resource: "VirtualAddress/pending", Field: "address"}
				resources[len(resources)-1].Spec = spec
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "pending"}, Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "192.0.2.1/24", Mode: "vrrp"}})
			}

			if tc.relay {
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"v6"}, Upstream: "192.0.2.1"}})
			}
			if !tc.raOnly {
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.DHCPv6ServerSpec{Interface: "v6", Mode: "stateless"}})
			}
			if tc.delegated {
				resources[len(resources)-1].Spec = api.DHCPv6ServerSpec{DelegatedAddress: "prefix", Mode: "stateless"}
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "prefix"}, Spec: api.IPv6DelegatedAddressSpec{Interface: "v6", PrefixDelegation: "wan"}})
			}

			resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "v6", MTU: 1454}})

			resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan-zone"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan", "v6"}}})
			router := &api.Router{Spec: api.RouterSpec{Resources: resources}}
			for name, renderFirewall := range map[string]func() ([]byte, error){
				"filter":   func() ([]byte, error) { return NftablesFirewall(router, nil) },
				"combined": func() ([]byte, error) { return NftablesNAT44(router) },
			} {
				data, err := renderFirewall()
				if err != nil {
					t.Fatal(err)
				}
				guard := `meta nfproto ipv4 iifname { "lan-vrrp" } udp dport 67 counter drop`
				got := string(data)
				if strings.Contains(got, guard) != tc.exclude {
					t.Fatalf("%s IPv4 input guard mismatch:\n%s", name, got)
				}
				if tc.exclude && strings.Index(got, guard) > strings.Index(got, "ct state { established, related } counter accept") {
					t.Fatalf("%s guard must precede established acceptance", name)
				}
				if strings.Contains(got, `iifname { "lan-parent" } udp dport 67 counter drop`) {
					t.Fatalf("%s blocked the IPv4 parent", name)
				}
			}
			data, _, err := DnsmasqConfig(&api.Router{Spec: api.RouterSpec{Resources: resources}}, DnsmasqRuntime{})
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			for _, prohibited := range []string{"no-dhcp-interface=", "no-dhcpv4-interface=", "no-dhcpv6-interface="} {
				if strings.Contains(got, prohibited) {
					t.Fatalf("dnsmasq family exclusions break 2.90 IPv6 constructors: %s", got)
				}
			}

			if !strings.Contains(got, "enable-ra\n") || !strings.Contains(got, "ra-param=lan-vrrp,mtu:1454,0\n") {
				t.Fatalf("IPv6 RA was lost:\n%s", got)
			}
			if !tc.raOnly && !tc.delegated && !strings.Contains(got, "constructor:lan-vrrp,ra-stateless") {
				t.Fatalf("DHCPv6 was lost:\n%s", got)
			}
		})
	}
}
