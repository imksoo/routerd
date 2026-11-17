// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestDnsmasqConfigExcludesStickyHostsConflictingWithReservation(t *testing.T) {
	const reservedMAC = "02:00:00:00:01:50"
	const reservedIP = "192.168.10.150"
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-ipv4"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.3/24"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-dhcpv4"}, Spec: api.DHCPv4ServerSpec{
				Interface: "lan", RangeStart: "192.168.10.100", RangeEnd: "192.168.10.199", LeaseTime: "12h", RouterSource: "none", DNSSource: "none",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{
				Server: "lan-dhcpv4", MACAddress: reservedMAC, IPAddress: reservedIP, Hostname: "printer",
			}},
		}},
	}
	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{StickyHosts: []DHCPStickyHost{
		{MACAddress: reservedMAC, IPAddress: reservedIP, Hostname: "exact", Family: "ipv4"},
		{MACAddress: reservedMAC, IPAddress: "192.168.10.151", Hostname: "old-address", Family: "ipv4"},
		{MACAddress: "02:00:00:00:01:51", IPAddress: reservedIP, Hostname: "old-client", Family: "ipv4"},
		{MACAddress: "02:00:00:00:01:52", IPAddress: "192.168.10.152", Hostname: "unrelated", Family: "ipv4"},
	}})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	got := string(data)
	if strings.Count(got, reservedIP) != 1 {
		t.Fatalf("reserved IP rendered more than once:\n%s", got)
	}
	for _, excluded := range []string{"exact", "old-address", "old-client"} {
		if strings.Contains(got, excluded) {
			t.Fatalf("conflicting sticky host %q was rendered:\n%s", excluded, got)
		}
	}
	if !strings.Contains(got, "dhcp-host=02:00:00:00:01:52,192.168.10.152,unrelated,12h") {
		t.Fatalf("unrelated sticky host was removed:\n%s", got)
	}
}

func TestExcludeReservedIPv4StickyHostsKeepsIPv6(t *testing.T) {
	hosts := []DHCPStickyHost{{MACAddress: "02:00:00:00:01:50", IPAddress: "2001:db8::150", Family: "ipv6"}}
	got := ExcludeReservedIPv4StickyHosts(hosts, []api.DHCPv4ReservationSpec{{MACAddress: "02:00:00:00:01:50", IPAddress: "192.0.2.150"}})
	if len(got) != 1 {
		t.Fatalf("IPv6 sticky host count = %d, want 1", len(got))
	}
}

func TestDnsmasqConfigSkipsIPv6ScopeUntilDelegatedPrefixExists(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv4"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.2/24"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv4"},
				Spec:     api.DHCPv4ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ServerSpec{

					Interface:    "lan",
					RangeStart:   "192.168.10.120",
					RangeEnd:     "192.168.10.129",
					RouterSource: "interfaceAddress",
					DNSSource:    "self",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv6"},
				Spec: api.IPv6DelegatedAddressSpec{
					PrefixDelegation: "wan-pd",
					Interface:        "lan",
					AddressSuffix:    "::2",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv6"},
				Spec:     api.DHCPv6ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv6"},
				Spec: api.DHCPv6ServerSpec{

					DelegatedAddress: "lan-ipv6",
					Mode:             "stateless",
					DNSSource:        "self",
				},
			},
		}},
	}

	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{
		IPv6PrefixesByInterface: map[string][]string{"ens19": {"fe80::/64"}},
	})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	got := string(data)
	for _, unwanted := range []string{
		"enable-ra",
		"constructor:ens19",
		"option6:dns-server",
		"fe80::2",
	} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("dnsmasq output contains %q before PD exists:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "dhcp-range=set:lan-dhcpv4,192.168.10.120,192.168.10.129,255.255.255.0") {
		t.Fatalf("dnsmasq output should keep IPv4 DHCP active:\n%s", got)
	}
}

func TestDnsmasqConfigExcludesDNSResolverSources(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv4"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.3/24"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv4"},
				Spec:     api.DHCPv4ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ServerSpec{

					Interface:    "lan",
					RangeStart:   "192.168.10.130",
					RangeEnd:     "192.168.10.139",
					RouterSource: "interfaceAddress",
					DNSSource:    "self",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
				Metadata: api.ObjectMeta{Name: "lan-resolver"},
				Spec: api.DNSResolverSpec{
					Listen:  []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 53}},
					Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"https://cloudflare-dns.com/dns-query"}}},
				},
			},
		}},
	}
	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	if got := string(data); strings.Contains(got, "server=") || strings.Contains(got, "listen-address=") {
		t.Fatalf("dnsmasq must not render DNS resolver sources:\n%s", got)
	}
}
