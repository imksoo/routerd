package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestDnsmasqConfigUsesSelfDNSWithDHCPv4Upstream(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv4"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.10.3/24"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv4"},
				Spec: api.DHCPv4ServerSpec{
					Server:  "dnsmasq",
					Managed: true,
					ListenInterfaces: []string{
						"lan",
					},
					DNS: api.DHCPv4ServerDNSSpec{
						Enabled:           true,
						UpstreamSource:    "dhcpv4",
						UpstreamInterface: "wan",
						CacheSize:         1000,
					},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:        "dhcpv4",
					Interface:     "lan",
					RangeStart:    "192.168.10.130",
					RangeEnd:      "192.168.10.139",
					LeaseTime:     "12h",
					RouterSource:  "interfaceAddress",
					DNSSource:     "self",
					Authoritative: true,
				},
			},
		}},
	}

	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{
		DHCPv4DNSServersByInterface: map[string][]string{
			"ens18": {"192.168.1.66", "192.168.1.67", "2001:db8:3d60:1200::1"},
		},
	})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"port=53",
		"bind-interfaces",
		"except-interface=ens18",
		"listen-address=127.0.0.1,192.168.10.3,::1",
		"cache-size=1000",
		"server=192.168.1.66",
		"server=192.168.1.67",
		"dhcp-range=set:lan-dhcpv4,192.168.10.130,192.168.10.139,255.255.255.0,12h",
		"dhcp-option=tag:lan-dhcpv4,option:router,192.168.10.3",
		"dhcp-option=tag:lan-dhcpv4,option:dns-server,192.168.10.3",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dnsmasq output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "except-interface=ens19") {
		t.Fatalf("dnsmasq should not exclude the served LAN interface:\n%s", got)
	}
}

func TestDnsmasqConfigRendersDHCPv4Reservation(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv4"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv4"},
				Spec:     api.DHCPv4ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:     "dhcpv4",
					Interface:  "lan",
					RangeStart: "192.0.2.100",
					RangeEnd:   "192.0.2.150",
					LeaseTime:  "12h",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"},
				Metadata: api.ObjectMeta{Name: "printer"},
				Spec: api.DHCPv4ReservationSpec{
					Scope:      "lan-dhcpv4",
					MACAddress: "02:00:00:00:01:50",
					IPAddress:  "192.0.2.120",
					Hostname:   "printer",
					LeaseTime:  "infinite",
				},
			},
		}},
	}

	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	got := string(data)
	if want := "dhcp-host=02:00:00:00:01:50,192.0.2.120,printer,infinite"; !strings.Contains(got, want) {
		t.Fatalf("dnsmasq output missing %q:\n%s", want, got)
	}
}

func TestDnsmasqConfigRendersDirectLANServiceKinds(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-v4"}, Spec: api.DHCPv4ServerSpec{
				Managed:     true,
				Interface:   "lan",
				AddressPool: api.DHCPAddressPoolSpec{Start: "192.168.10.100", End: "192.168.10.199", LeaseTime: "8h"},
				Gateway:     "192.168.10.1",
				DNSServers:  []string{"192.168.10.1"},
				NTPServers:  []string{"192.168.10.1"},
				Domain:      "lan",
				Options:     []api.DHCPv4OptionSpec{{Name: "domain-search", Value: "lan"}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"}, Metadata: api.ObjectMeta{Name: "printer"}, Spec: api.DHCPv4ReservationSpec{
				Server:     "lan-v4",
				MACAddress: "02:00:00:00:01:50",
				Hostname:   "printer",
				IPAddress:  "192.168.10.150",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.DHCPv6ServerSpec{
				Interface:    "lan",
				Mode:         "both",
				AddressPool:  api.DHCPAddressPoolSpec{Start: "::100", End: "::1ff", LeaseTime: "6h"},
				DNSServers:   []string{"2001:db8::53"},
				SNTPServers:  []string{"2001:db8::123"},
				DomainSearch: []string{"lan"},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{
				Interface:         "lan",
				PrefixSource:      "${IPv6DelegatedAddress/lan.status.address}",
				RDNSS:             []string{"2001:db8::53"},
				DNSSL:             []string{"lan"},
				MTU:               1500,
				PRFPreference:     "high",
				ValidLifetime:     "7200",
				PreferredLifetime: "3600",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSAnswerScope"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DNSAnswerScopeSpec{
				Interface:   "lan",
				LocalDomain: "lan",
				DDNS:        true,
				DNSSEC:      true,
				HostRecords: []api.DNSHostRecord{{Hostname: "router.lan", IPv4: "192.168.10.1", IPv6: "2001:db8::1"}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Relay"}, Metadata: api.ObjectMeta{Name: "relay"}, Spec: api.DHCPv4RelaySpec{Interfaces: []string{"lan"}, Upstream: "192.0.2.53"}},
		}},
	}

	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"dhcp-range=set:lan-v4,192.168.10.100,192.168.10.199,8h",
		"dhcp-option=tag:lan-v4,option:router,192.168.10.1",
		"dhcp-host=02:00:00:00:01:50,set:printer,printer,192.168.10.150",
		"dhcp-range=set:lan-v6,::100,::1ff,constructor:ens19,slaac,64,6h",
		"dhcp-option=tag:lan-v6,option6:dns-server,[2001:db8::53]",
		"ra-param=ens19,mtu:1500,high,0,7200",
		"dhcp-option=option6:domain-search,lan",
		"host-record=router.lan,192.168.10.1,2001:db8::1",
		"domain=lan",
		"local=/lan/",
		"dhcp-fqdn",
		"dnssec",
		"dhcp-relay=0.0.0.0,192.0.2.53,ens19",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dnsmasq output missing %q:\n%s", want, got)
		}
	}
}

func TestDnsmasqConfigCanPassThroughDHCPv4DNS(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:       "dhcpv4",
					Interface:    "lan",
					RangeStart:   "192.168.10.130",
					RangeEnd:     "192.168.10.139",
					RouterSource: "interfaceAddress",
					DNSSource:    "dhcpv4",
					DNSInterface: "wan",
				},
			},
		}},
	}

	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{
		DHCPv4DNSServersByInterface: map[string][]string{"ens18": {"192.168.1.66"}},
	})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "port=53") {
		t.Fatalf("dnsmasq should not enable DNS listener for pass-through DNS:\n%s", got)
	}
	if !strings.Contains(got, "dhcp-option=tag:lan-dhcpv4,option:dns-server,192.168.1.66") {
		t.Fatalf("dnsmasq output missing pass-through DNS option:\n%s", got)
	}
}

func TestDnsmasqConfigSkipsTransitDHCPServerRole(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv4"},
				Spec:     api.DHCPv4ServerSpec{Server: "dnsmasq", Managed: true, Role: "transit", ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:     "dhcpv4",
					Interface:  "lan",
					RangeStart: "192.0.2.100",
					RangeEnd:   "192.0.2.120",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv6"},
				Spec:     api.DHCPv6ServerSpec{Server: "dnsmasq", Managed: true, Role: "transit", ListenInterfaces: []string{"lan"}},
			},
		}},
	}

	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	got := string(data)
	for _, forbidden := range []string{"dhcp-range", "enable-ra", "interface=ens19"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("dnsmasq output should not include %q for transit roles:\n%s", forbidden, got)
		}
	}
}

func TestDnsmasqConfigRendersIPv6StatelessScope(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"},
				Metadata: api.ObjectMeta{Name: "lan-ipv6"},
				Spec: api.IPv6DelegatedAddressSpec{
					PrefixDelegation: "wan-pd",
					Interface:        "lan",
					AddressSuffix:    "::3",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
				Metadata: api.ObjectMeta{Name: "dhcpv6"},
				Spec:     api.DHCPv6ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv6"},
				Spec: api.DHCPv6ScopeSpec{
					Server:           "dhcpv6",
					DelegatedAddress: "lan-ipv6",
					Mode:             "stateless",
					LeaseTime:        "12h",
					DefaultRoute:     true,
					DNSSource:        "self",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "ds-lite-a"},
				Spec:     api.DSLiteTunnelSpec{Interface: "wan", RemoteAddress: "2001:db8::1"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"},
				Metadata: api.ObjectMeta{Name: "lan-wan-mtu"},
				Spec: api.PathMTUPolicySpec{
					FromInterface: "lan",
					ToInterfaces:  []string{"ds-lite-a"},
					MTU:           api.PathMTUPolicyMTUSpec{Source: "minInterface"},
					IPv6RA:        api.PathMTUPolicyIPv6RASpec{Enabled: true, Scope: "lan-dhcpv6"},
				},
			},
		}},
	}

	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{
		IPv6AddressesByInterface: map[string][]string{
			"ens19": {"2001:db8:3d60:1220::100", "2001:db8:3d60:1220::3", "fe80::1"},
		},
		IPv6PrefixesByInterface: map[string][]string{
			"ens19": {"2001:db8:3d60:1220::/64"},
		},
	})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"enable-ra",
		"listen-address=127.0.0.1,2001:db8:3d60:1220::3,::1",
		"dhcp-range=set:lan-dhcpv6,::,constructor:ens19,ra-stateless,64,12h",
		"ra-param=ens19,1454",
		"dhcp-option=tag:lan-dhcpv6,option6:dns-server,[2001:db8:3d60:1220::3]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("dnsmasq output missing %q:\n%s", want, got)
		}
	}
}

func TestDnsmasqConfigSkipsIPv6ScopeUntilDelegatedPrefixExists(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:       "dhcpv4",
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv6"},
				Spec: api.DHCPv6ScopeSpec{
					Server:           "dhcpv6",
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

func TestDnsmasqServiceUnitDoesNotOwnRouterdRuntimeDirectory(t *testing.T) {
	unit := string(DnsmasqServiceUnit("/run/routerd/routerd-dnsmasq.conf", "/run/current-system/sw/bin/dnsmasq"))
	if strings.Contains(unit, "RuntimeDirectory=routerd") {
		t.Fatalf("dnsmasq unit must not own /run/routerd because it can remove the routerd control socket:\n%s", unit)
	}
	if !strings.Contains(unit, "--pid-file=/run/routerd/dnsmasq.pid") {
		t.Fatalf("dnsmasq unit should keep the managed pid path:\n%s", unit)
	}
}

func TestDnsmasqRCScriptUsesFreeBSDRuntimeDirectory(t *testing.T) {
	script := string(DnsmasqRCScript("/usr/local/etc/routerd/dnsmasq.conf", "/var/run/routerd"))
	for _, want := range []string{
		`name="routerd_dnsmasq"`,
		`rcvar="routerd_dnsmasq_enable"`,
		`command="/usr/local/sbin/dnsmasq"`,
		`pidfile="/var/run/routerd/dnsmasq.pid"`,
		`--conf-file=/usr/local/etc/routerd/dnsmasq.conf`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, script)
		}
	}
}

func TestDnsmasqConfigDerivesIPv6SelfDNSFromRoutePrefix(t *testing.T) {
	spec := api.DHCPv6ScopeSpec{DNSSource: "self", DelegatedAddress: "lan-ipv6"}
	delegated := delegatedIPv6Address{
		Interface:     "lan",
		IfName:        "ens19",
		AddressSuffix: "::3",
	}
	got, err := dnsmasqIPv6DNSServers(spec, delegated, api.SelfAddressPolicySpec{}, map[string]string{"lan": "ens19"}, map[string]delegatedIPv6Address{"lan-ipv6": delegated}, DnsmasqRuntime{
		IPv6PrefixesByInterface: map[string][]string{"ens19": {"2001:db8:3d60:1220::/64"}},
	})
	if err != nil {
		t.Fatalf("derive dns server: %v", err)
	}
	if len(got) != 1 || got[0] != "2001:db8:3d60:1220::3" {
		t.Fatalf("dns servers = %v, want delegated ::3", got)
	}
}

func TestDnsmasqConfigPrefersObservedIPv6SelfDNSMatchingSuffix(t *testing.T) {
	spec := api.DHCPv6ScopeSpec{DNSSource: "self", DelegatedAddress: "lan-ipv6"}
	delegated := delegatedIPv6Address{
		Interface:     "lan",
		IfName:        "ens19",
		AddressSuffix: "::3",
	}
	got, err := dnsmasqIPv6DNSServers(spec, delegated, api.SelfAddressPolicySpec{}, map[string]string{"lan": "ens19"}, map[string]delegatedIPv6Address{"lan-ipv6": delegated}, DnsmasqRuntime{
		IPv6AddressesByInterface: map[string][]string{"ens19": {
			"2001:db8:3d60:1220::100",
			"2001:db8:3d60:1220::3",
		}},
	})
	if err != nil {
		t.Fatalf("select dns server: %v", err)
	}
	if len(got) != 1 || got[0] != "2001:db8:3d60:1220::3" {
		t.Fatalf("dns servers = %v, want delegated ::3", got)
	}
}

func TestDnsmasqConfigUsesSelfAddressPolicyOrder(t *testing.T) {
	spec := api.DHCPv6ScopeSpec{DNSSource: "self", DelegatedAddress: "lan-ipv6", SelfAddressPolicy: "lan-dns-self"}
	delegated := delegatedIPv6Address{Interface: "lan", IfName: "ens19", AddressSuffix: "::3"}
	policy := api.SelfAddressPolicySpec{
		AddressFamily: "ipv6",
		Candidates: []api.SelfAddressPolicyCandidate{
			{Source: "static", Address: "2001:db8::53"},
			{Source: "delegatedAddress", DelegatedAddress: "lan-ipv6"},
		},
	}
	got, err := dnsmasqIPv6DNSServers(spec, delegated, policy, map[string]string{"lan": "ens19"}, map[string]delegatedIPv6Address{"lan-ipv6": delegated}, DnsmasqRuntime{
		IPv6PrefixesByInterface: map[string][]string{"ens19": {"2001:db8:3d60:1220::/64"}},
	})
	if err != nil {
		t.Fatalf("select dns server: %v", err)
	}
	if len(got) != 1 || got[0] != "2001:db8::53" {
		t.Fatalf("dns servers = %v, want static policy candidate", got)
	}
}

func TestDnsmasqConfigRendersConditionalForwarder(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:       "dhcpv4",
					Interface:    "lan",
					RangeStart:   "192.168.10.130",
					RangeEnd:     "192.168.10.139",
					RouterSource: "interfaceAddress",
					DNSSource:    "self",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSConditionalForwarder"},
				Metadata: api.ObjectMeta{Name: "transix-aftr"},
				Spec: api.DNSConditionalForwarderSpec{
					Domain:         "gw.transix.jp",
					UpstreamSource: "static",
					UpstreamServers: []string{
						"2404:1a8:7f01:a::3",
					},
				},
			},
		}},
	}
	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	if got := string(data); !strings.Contains(got, "server=/gw.transix.jp/2404:1a8:7f01:a::3") {
		t.Fatalf("dnsmasq output missing conditional forwarder:\n%s", got)
	}
}

func TestDnsmasqConfigSkipsDHCPv4ConditionalForwarderWhenObservedEmpty(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: true, Owner: "routerd"},
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Scope"},
				Metadata: api.ObjectMeta{Name: "lan-dhcpv4"},
				Spec: api.DHCPv4ScopeSpec{
					Server:       "dhcpv4",
					Interface:    "lan",
					RangeStart:   "192.168.10.130",
					RangeEnd:     "192.168.10.139",
					RouterSource: "interfaceAddress",
					DNSSource:    "self",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSConditionalForwarder"},
				Metadata: api.ObjectMeta{Name: "wan-fwd"},
				Spec: api.DNSConditionalForwarderSpec{
					Domain:            "example.com",
					UpstreamSource:    "dhcpv4",
					UpstreamInterface: "wan",
				},
			},
		}},
	}
	data, warnings, err := DnsmasqConfig(router, DnsmasqRuntime{
		DHCPv4DNSServersByInterface: map[string][]string{},
	})
	if err != nil {
		t.Fatalf("render dnsmasq: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning when DHCPv4 DNS servers are not yet observed")
	}
	if strings.Contains(string(data), "server=/example.com/") {
		t.Fatalf("forwarder rule must be skipped when observed empty:\n%s", data)
	}
}

func TestDnsmasqConfigRendersDHCPv6ConditionalForwarder(t *testing.T) {
	spec := api.DNSConditionalForwarderSpec{
		Domain:            "example.net",
		UpstreamSource:    "dhcpv6",
		UpstreamInterface: "wan",
	}
	servers, err := conditionalForwarderServers(spec, map[string]string{"wan": "ens18"}, DnsmasqRuntime{
		DHCPv6DNSServersByInterface: map[string][]string{"ens18": {"2001:db8:3d60:1200:1eb1:7fff:fe73:76d8", "192.0.2.53"}},
	})
	if err != nil {
		t.Fatalf("conditional forwarder servers: %v", err)
	}
	if len(servers) != 1 || servers[0] != "2001:db8:3d60:1200:1eb1:7fff:fe73:76d8" {
		t.Fatalf("servers = %v, want DHCPv6 DNS only", servers)
	}
}
