// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

// The generated config must keep IPv4 DHCP off IPv6-only listeners while
// preserving declared IPv4 service/relay on a shared physical interface.
// These are renderer contracts; actual packet exchange is checked separately.
func TestDnsmasqConfigIPv6OnlyListener(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		v4, relay, shared, raOnly bool
		exclude                   bool
	}{
		{name: "split DHCP families", v4: true, exclude: true},
		{name: "shared physical interface via alias", v4: true, shared: true},
		{name: "RA only", raOnly: true, exclude: true},
		{name: "IPv4 relay and IPv6", relay: true},
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
			if tc.relay {
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"v6"}, Upstream: "192.0.2.1"}})
			}
			if !tc.raOnly {
				resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "v6"}, Spec: api.DHCPv6ServerSpec{Interface: "v6", Mode: "stateless"}})
			}
			resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "v6", MTU: 1454}})
			data, _, err := DnsmasqConfig(&api.Router{Spec: api.RouterSpec{Resources: resources}}, DnsmasqRuntime{})
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			count := strings.Count(got, "no-dhcpv4-interface=lan-vrrp\n")
			want := 0
			if tc.exclude {
				want = 1
			}
			if count != want {
				t.Fatalf("IPv4 exclusion count = %d, want %d:\n%s", count, want, got)
			}
			for _, prohibited := range []string{"no-dhcp-interface=", "no-dhcpv6-interface=", "no-dhcpv4-interface=lan-parent"} {
				if strings.Contains(got, prohibited) {
					t.Fatalf("unexpected family/service exclusion %q:\n%s", prohibited, got)
				}
			}
			if !strings.Contains(got, "enable-ra\n") || !strings.Contains(got, "ra-param=lan-vrrp,mtu:1454,0\n") {
				t.Fatalf("IPv6 RA was lost:\n%s", got)
			}
			if !tc.raOnly && !strings.Contains(got, "constructor:lan-vrrp,ra-stateless") {
				t.Fatalf("DHCPv6 was lost:\n%s", got)
			}
		})
	}
}
