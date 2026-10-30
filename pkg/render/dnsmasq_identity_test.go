// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestDnsmasqPoolUsesMatchingInterfaceSubnet(t *testing.T) {
	resources := []api.Resource{
		netResource("Interface", "lan", api.InterfaceSpec{IfName: "em0"}),
		netResource("IPv4StaticAddress", "pool-address", api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/25"}),
		// The resource name deliberately collides with the logical interface
		// name; those are independent namespaces.
		netResource("IPv4StaticAddress", "lan", api.IPv4StaticAddressSpec{Interface: "lan", Address: "198.51.100.1/24"}),
		netResource("DHCPv4Server", "pool", api.DHCPv4ServerSpec{Interface: "lan", RangeStart: "192.0.2.10", RangeEnd: "192.0.2.20"}),
	}
	for _, reverse := range []bool{false, true} {
		if reverse {
			resources[1], resources[2] = resources[2], resources[1]
		}
		router := &api.Router{Spec: api.RouterSpec{Resources: resources}}
		data, _, err := DnsmasqConfig(router, DnsmasqRuntime{})
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"dhcp-range=set:pool,192.0.2.10,192.0.2.20,255.255.255.128,12h", "option:router,192.0.2.1", "option:dns-server,192.0.2.1"} {
			if !strings.Contains(string(data), expected) {
				t.Fatalf("declaration order %v lost pool subnet %q:\n%s", reverse, expected, data)
			}
		}
		aliases, addresses, delegated, _, err := dnsmasqInputs(router)
		if err != nil {
			t.Fatal(err)
		}
		got, err := dnsmasqResolveStatusSource(router, api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan"}, aliases, addresses, delegated, DnsmasqRuntime{})
		if err != nil || len(got) != 1 || got[0] != "198.51.100.1" {
			t.Fatalf("resource source changed with interface namespace/order: %v %v", got, err)
		}
	}
}

func TestDnsmasqInterfaceAddressSourceNeverUsesUpstreamDNS(t *testing.T) {
	aliases := map[string]string{"wan": "em0"}
	runtime := DnsmasqRuntime{
		DHCPv4DNSServersByInterface: map[string][]string{"em0": {"203.0.113.53"}},
		IPv4AddressesByInterface:    map[string][]string{"em0": {"192.0.2.5", "2001:db8::5"}},
	}
	source := api.StatusValueSourceSpec{Resource: "Interface/wan", Field: "ipv4Addresses"}
	got, err := dnsmasqResolveStatusSource(&api.Router{}, source, aliases, nil, nil, runtime)
	if err != nil || len(got) != 1 || got[0] != "192.0.2.5" {
		t.Fatalf("interface source returned %v, %v", got, err)
	}
	runtime.IPv4AddressesByInterface = nil
	got, err = dnsmasqResolveStatusSource(&api.Router{}, source, aliases, nil, nil, runtime)
	if err != nil || len(got) != 0 {
		t.Fatalf("unobserved interface source invented addresses: %v, %v", got, err)
	}
}
