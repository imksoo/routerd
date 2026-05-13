// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestNftablesIPv4SourceNAT(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation: api.IPv4NATTranslationSpec{
						Type: "interfaceAddress",
						PortMapping: api.IPv4NATPortMappingSpec{
							Type:  "range",
							Start: 1024,
							End:   65535,
						},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table ip routerd_nat",
		"flush table ip routerd_nat",
		"table ip routerd_nat",
		"type nat hook postrouting priority srcnat; policy accept;",
		`oifname "ens18" ip saddr 192.168.10.0/24 meta l4proto { tcp, udp } masquerade to :1024-65535`,
		`oifname "ens18" ip saddr 192.168.10.0/24 masquerade`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesIPv4SourceNATCanUseDSLiteTunnel(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "transix"},
				Spec: api.DSLiteTunnelSpec{
					Interface:  "wan",
					TunnelName: "ds-transix",
					AFTRFQDN:   "gw.transix.jp",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-to-transix"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "transix",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	if !strings.Contains(string(data), `oifname "ds-transix" ip saddr 192.168.10.0/24 masquerade`) {
		t.Fatalf("nftables output missing DS-Lite tunnel oif:\n%s", string(data))
	}
}

func TestNftablesNAT44Rule(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.NAT44RuleSpec{
					Type:            "masquerade",
					EgressInterface: "wan",
					SourceRanges:    []string{"192.168.10.0/24", "10.0.0.0/8"},
				},
			},
		}},
	}
	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table ip routerd_nat",
		"flush table ip routerd_nat",
		`oifname "ens18" ip saddr 10.0.0.0/8 masquerade`,
		`oifname "ens18" ip saddr 192.168.10.0/24 masquerade`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesNAT44ResolvedSNATRule(t *testing.T) {
	data, err := NftablesNAT44Rules([]NAT44RenderRule{{
		Name:            "lan-to-wan",
		Type:            "snat",
		EgressInterface: "ppp0",
		SourceRanges:    []string{"192.168.10.0/24"},
		SNATAddress:     "198.51.100.10",
	}})
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table ip routerd_nat",
		"flush table ip routerd_nat",
		`oifname "ppp0" ip saddr 192.168.10.0/24 snat to 198.51.100.10`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesSkipsRuntimeResolvedNAT44Policy(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
				Metadata: api.ObjectMeta{Name: "lan-to-egress"},
				Spec: api.NAT44RuleSpec{
					Type:            "masquerade",
					EgressPolicyRef: "ipv4-default",
					SourceRanges:    []string{"172.18.0.0/16"},
				},
			},
		}},
	}
	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "skipped: egressInterface is resolved by ipv4-default at runtime") {
		t.Fatalf("nftables output missing runtime-resolved NAT44 skip comment:\n%s", got)
	}
}

func TestNftablesDefaultRoutePolicyFlushesTable(t *testing.T) {
	data, err := NftablesIPv4DefaultRoutePolicy(
		"net.routerd.net/v1alpha1/IPv4DefaultRoutePolicy/default",
		api.IPv4DefaultRoutePolicySpec{SourceCIDRs: []string{"172.18.0.0/16"}},
		api.IPv4DefaultRoutePolicyCandidate{Name: "wan", Mark: 100},
		[]api.IPv4DefaultRoutePolicyCandidate{{Name: "wan", Mark: 100}},
		nil,
	)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table ip routerd_default_route",
		"flush table ip routerd_default_route",
		"table ip routerd_default_route",
		"ip saddr 172.18.0.0/16 ct mark 0x0 meta mark set 0x64 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesNAT44RuleCanExcludeDestinations(t *testing.T) {
	data, err := NftablesNAT44Rules([]NAT44RenderRule{{
		Name:                    "lan-to-hgw",
		Type:                    "masquerade",
		EgressInterface:         "ens18",
		SourceRanges:            []string{"172.18.0.0/16"},
		ExcludeDestinationCIDRs: []string{"192.168.0.0/16", "172.16.0.0/12", "10.0.0.0/8"},
	}})
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	want := `oifname "ens18" ip saddr 172.18.0.0/16 ip daddr !=192.168.0.0/16 ip daddr !=172.16.0.0/12 ip daddr !=10.0.0.0/8 masquerade`
	if !strings.Contains(got, want) {
		t.Fatalf("nftables output missing excluded destination match %q:\n%s", want, got)
	}
}

func TestNftablesVXLANL2Filter(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "underlay"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"},
				Metadata: api.ObjectMeta{Name: "home-vxlan"},
				Spec: api.VXLANSegmentSpec{
					IfName:            "vxlan100",
					VNI:               100,
					LocalAddress:      "192.0.2.10",
					Remotes:           []string{"192.0.2.20"},
					UnderlayInterface: "underlay",
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table bridge routerd_l2_filter",
		"flush table bridge routerd_l2_filter",
		"table bridge routerd_l2_filter",
		`iifname "vxlan100" ether type ip udp sport { 67, 68 } counter drop`,
		`oifname "vxlan100" ether type ip6 udp dport { 546, 547 } counter drop`,
		`iifname "vxlan100" ether type ip6 icmpv6 type { nd-router-solicit, nd-router-advert, nd-neighbor-solicit, nd-neighbor-advert } counter drop`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesVXLANL2FilterCanBeDisabled(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "underlay"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"},
				Metadata: api.ObjectMeta{Name: "home-vxlan"},
				Spec: api.VXLANSegmentSpec{
					IfName:            "vxlan100",
					VNI:               100,
					LocalAddress:      "192.0.2.10",
					Remotes:           []string{"192.0.2.20"},
					UnderlayInterface: "underlay",
					L2Filter:          "none",
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	if strings.Contains(string(data), "routerd_l2_filter") {
		t.Fatalf("nftables output should not include L2 filter table:\n%s", string(data))
	}
}

func TestNftablesVXLANUnderlayUDPAcceptInputChain(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "underlay"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANSegment"},
				Metadata: api.ObjectMeta{Name: "home-vxlan"},
				Spec: api.VXLANSegmentSpec{
					IfName:            "vxlan100",
					VNI:               100,
					LocalAddress:      "192.0.2.10",
					Remotes:           []string{"192.0.2.20"},
					UnderlayInterface: "underlay",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
				Metadata: api.ObjectMeta{Name: "default-home"},
				Spec:     api.FirewallPolicySpec{LogDeny: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec: api.FirewallZoneSpec{
					Role:       "untrust",
					Interfaces: []string{"Interface/underlay"},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table inet routerd_filter",
		"flush table inet routerd_filter",
		"table inet routerd_filter",
		`udp dport 4789 counter accept comment "net.routerd.net/v1alpha1/VXLANSegment/home-vxlan"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesBridgeOverlayICMPAccept(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"},
				Metadata: api.ObjectMeta{Name: "br-vxlan-test"},
				Spec:     api.BridgeSpec{Members: []string{}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
				Metadata: api.ObjectMeta{Name: "default-home"},
				Spec:     api.FirewallPolicySpec{LogDeny: true},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	want := `meta l4proto ipv6-icmp counter accept`
	if !strings.Contains(got, want) {
		t.Fatalf("nftables output missing bridge ICMP accept rule %q:\n%s", want, got)
	}
}

func TestNftablesIPv4SourceNATAddressPortRange(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ds0", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.IPv4SourceNATSpec{
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation: api.IPv4NATTranslationSpec{
						Type:    "address",
						Address: "192.0.0.2",
						PortMapping: api.IPv4NATPortMappingSpec{
							Type:  "range",
							Start: 1024,
							End:   65535,
						},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	if !strings.Contains(string(data), `snat to 192.0.0.2:1024-65535`) {
		t.Fatalf("nftables output missing address SNAT port range:\n%s", string(data))
	}
}

func TestNftablesIPv4PolicyRoute(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRoute"},
				Metadata: api.ObjectMeta{Name: "lan-via-transix"},
				Spec: api.IPv4PolicyRouteSpec{
					OutboundInterface: "transix",
					Table:             100,
					Priority:          10000,
					Mark:              256,
					SourceCIDRs:       []string{"192.168.10.0/24"},
					DestinationCIDRs:  []string{"0.0.0.0/0"},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table ip routerd_policy",
		"flush table ip routerd_policy",
		"table ip routerd_policy",
		"type filter hook prerouting priority mangle; policy accept;",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 meta mark set 0x100",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesIPv4PolicyRouteSet(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"},
				Metadata: api.ObjectMeta{Name: "lan-balance"},
				Spec: api.IPv4PolicyRouteSetSpec{
					Mode:             "hash",
					HashFields:       []string{"sourceAddress", "destinationAddress"},
					SourceCIDRs:      []string{"192.168.10.0/24"},
					DestinationCIDRs: []string{"0.0.0.0/0"},
					Targets: []api.IPv4PolicyRouteTarget{
						{OutboundInterface: "transix-a", Table: 100, Priority: 10000, Mark: 256},
						{OutboundInterface: "transix-b", Table: 101, Priority: 10001, Mark: 257},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark != 0x0 meta mark set ct mark",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark 0x0 meta mark set jhash ip saddr . ip daddr mod 2 map { 0 : 0x100, 1 : 0x101 }",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark 0x0 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesIPv4PolicyRouteSetExcludesDestinations(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4PolicyRouteSet"},
				Metadata: api.ObjectMeta{Name: "lan-balance"},
				Spec: api.IPv4PolicyRouteSetSpec{
					Mode:                    "hash",
					HashFields:              []string{"sourceAddress"},
					SourceCIDRs:             []string{"172.18.0.0/16"},
					DestinationCIDRs:        []string{"0.0.0.0/0"},
					ExcludeDestinationCIDRs: []string{"192.168.1.0/24", "192.168.123.0/24"},
					Targets: []api.IPv4PolicyRouteTarget{
						{OutboundInterface: "ds-lite-a", Table: 110, Priority: 10110, Mark: 0x110},
						{OutboundInterface: "ds-lite-b", Table: 111, Priority: 10111, Mark: 0x111},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	want := "ip saddr 172.18.0.0/16 ip daddr 0.0.0.0/0 ip daddr !=192.168.1.0/24 ip daddr !=192.168.123.0/24 ct mark 0x0 meta mark set jhash ip saddr mod 2 map { 0 : 0x110, 1 : 0x111 }"
	if !strings.Contains(got, want) {
		t.Fatalf("nftables output missing excluded destination match %q:\n%s", want, got)
	}
}

func TestNftablesTCPMSSClamp(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"},
				Metadata: api.ObjectMeta{Name: "wan-pppoe"},
				Spec: api.PPPoEInterfaceSpec{
					Interface: "wan",
					IfName:    "ppp0",
					Username:  "user@example.jp",
					Password:  "secret",
					MTU:       1492,
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "ds-lite-a"},
				Spec:     api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-lite-a", RemoteAddress: "2001:db8::1"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PathMTUPolicy"},
				Metadata: api.ObjectMeta{Name: "lan-wan-mtu"},
				Spec: api.PathMTUPolicySpec{
					FromInterface: "lan",
					ToInterfaces:  []string{"wan-pppoe", "ds-lite-a"},
					MTU:           api.PathMTUPolicyMTUSpec{Source: "minInterface"},
					TCPMSSClamp: api.PathMTUPolicyTCPMSSSpec{
						Enabled:  true,
						Families: []string{"ipv4", "ipv6"},
					},
				},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table inet routerd_mss",
		"flush table inet routerd_mss",
		"table inet routerd_mss",
		"type filter hook forward priority mangle; policy accept;",
		`iifname "ens19" oifname { "ds-lite-a", "ppp0" } ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size set 1414`,
		`iifname "ens19" oifname { "ds-lite-a", "ppp0" } meta nfproto ipv6 tcp flags syn / syn,rst tcp option maxseg size set 1394`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesFirewallHomeRouter(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoEInterface"},
				Metadata: api.ObjectMeta{Name: "wan-pppoe"},
				Spec: api.PPPoEInterfaceSpec{
					Interface: "wan-ether",
					IfName:    "ppp0",
					Username:  "user@example.jp",
					Password:  "secret",
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan-pppoe"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
				Metadata: api.ObjectMeta{Name: "default-home"},
				Spec:     api.FirewallPolicySpec{LogDeny: true},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallLog"},
				Metadata: api.ObjectMeta{Name: "default"},
				Spec:     api.FirewallLogSpec{Enabled: true, NFLogGroup: 7, Path: "/var/lib/routerd/firewall-logs.db"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"},
				Metadata: api.ObjectMeta{Name: "nas-https"},
				Spec:     api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", Protocol: "tcp", Port: 443, SourceCIDRs: []string{"203.0.113.0/24"}, DestinationCIDRs: []string{"198.51.100.10/32"}, Action: "accept", Log: true},
			},
		}},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table inet routerd_filter",
		"flush table inet routerd_filter",
		"table inet routerd_filter",
		"type filter hook input priority filter; policy drop;",
		"ct state invalid counter drop",
		"ct state { established, related } counter accept",
		"meta l4proto ipv6-icmp counter accept",
		`set if_lan { type ifname; elements = { "ens19" } }`,
		`set if_wan { type ifname; elements = { "ppp0" } }`,
		`chain lan_to_wan`,
		`ip saddr 203.0.113.0/24 ip daddr 198.51.100.10/32 tcp dport 443 log prefix "routerd firewall nas-https " group 7 counter accept`,
		`counter log prefix "routerd firewall wan-to-lan deny " group 7 drop`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesKeepsProtectedZoneSSHOpen(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Apply: api.ApplyPolicySpec{ProtectedZones: []string{"mgmt"}},
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
					Metadata: api.ObjectMeta{Name: "mgmt"},
					Spec:     api.InterfaceSpec{IfName: "ens20", Managed: true, Owner: "routerd"},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
					Metadata: api.ObjectMeta{Name: "mgmt"},
					Spec:     api.FirewallZoneSpec{Role: "mgmt", Interfaces: []string{"mgmt"}},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
					Metadata: api.ObjectMeta{Name: "default-home"},
					Spec:     api.FirewallPolicySpec{},
				},
			},
		},
	}

	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `chain mgmt_to_self`) || !strings.Contains(got, `counter accept`) {
		t.Fatalf("nftables output does not keep mgmt self access open:\n%s", got)
	}
}

func TestNftablesSamplesAcceptedForwardFlows(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec: api.FirewallLogSpec{
				Enabled:    true,
				NFLogGroup: 7,
				Log:        api.FirewallLogPolicySpec{AcceptSampleRate: 100},
			},
		},
	}}}
	data, err := NftablesIPv4SourceNAT(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`ct state { new, established } numgen random mod 100 == 0 log prefix "routerd firewall forward accept " group 7`,
		`ct state new numgen random mod 100 == 0 log prefix "routerd firewall lan-to-wan accept " group 7 counter accept`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing sampled accept log %q:\n%s", want, got)
		}
	}
}

func TestNftablesAllowsWANIPv6ClientControlPlane(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
					Metadata: api.ObjectMeta{Name: "wan"},
					Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
					Metadata: api.ObjectMeta{Name: "wan"},
					Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
					Metadata: api.ObjectMeta{Name: "default-home"},
					Spec:     api.FirewallPolicySpec{},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
					Metadata: api.ObjectMeta{Name: "wan-pd"},
					Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
				},
			},
		},
	}

	data, err := NftablesFirewall(router, []FirewallHole{{Name: "dhcpv6-client", FromZone: "wan", ToZone: "self", Protocol: "udp", Port: 546, Action: "accept"}})
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table inet routerd_filter",
		"flush table inet routerd_filter",
		"meta l4proto ipv6-icmp counter accept",
		`udp dport 546 counter accept`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "udp sport 547") {
		t.Fatalf("DHCPv6 client rule must not constrain server source port:\n%s", got)
	}
}

func TestNftablesFirewallSkipsRedundantSelfHolesWhenZoneAlreadyAcceptsSelf(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
					Metadata: api.ObjectMeta{Name: "lan"},
					Spec:     api.InterfaceSpec{IfName: "ens19"},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
					Metadata: api.ObjectMeta{Name: "lan"},
					Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
					Metadata: api.ObjectMeta{Name: "lan-resolver"},
					Spec: api.DNSResolverSpec{
						Listen:  []api.DNSResolverListenSpec{{Addresses: []string{"192.0.2.1"}, Port: 53}},
						Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://1.1.1.1:53"}}},
					},
				},
			},
		},
	}
	data, err := NftablesFirewall(router, InternalFirewallHoles(router))
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "chain lan_to_self") {
		t.Fatalf("nftables output missing lan_to_self chain:\n%s", got)
	}
	if strings.Contains(got, "lan-resolver-dns") {
		t.Fatalf("nftables output should not render redundant self DNS hole:\n%s", got)
	}
}

func TestNftablesInternalWANHolesUseOwningInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite"},
			Spec:     api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-lite"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"Interface/wan", "DSLiteTunnel/ds-lite"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"},
			Metadata: api.ObjectMeta{Name: "wan-pd"},
			Spec:     api.DHCPv6PrefixDelegationSpec{Interface: "wan"},
		},
	}}}
	data, err := NftablesFirewall(router, InternalFirewallHoles(router))
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	want := `iifname "ens18" udp dport 546 counter accept comment "net.routerd.net/v1alpha1/DHCPv6PrefixDelegation/wan-pd"`
	if !strings.Contains(got, want) {
		t.Fatalf("nftables output missing interface-scoped DHCPv6 hole %q:\n%s", want, got)
	}
}

func TestNftablesClientPolicyIncludeGuestMACs(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"},
			Metadata: api.ObjectMeta{Name: "guest-devices"},
			Spec: api.ClientPolicySpec{
				Mode:          "include",
				Interfaces:    []string{"lan"},
				GuestServices: []string{"dns", "dhcp", "ntp", "mdns", "ssdp"},
				Classification: []api.ClientPolicyClassSpec{{
					MACAddress: "18:ec:e7:33:12:6c",
					As:         "guest",
					Name:       "aiseg2",
				}},
			},
		},
	}}}
	data, err := NftablesFirewall(router, nil)
	if err != nil {
		t.Fatalf("render nftables firewall: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`set client_policy_guest_devices { type ether_addr; elements = { 18:ec:e7:33:12:6c } }`,
		`iifname "ens19" ether saddr @client_policy_guest_devices udp dport 53 counter accept`,
		`iifname "ens19" ether saddr @client_policy_guest_devices udp dport { 67, 547 } counter accept`,
		`iifname "ens19" ether saddr @client_policy_guest_devices udp dport 5353 counter accept`,
		`iifname "ens19" ether saddr @client_policy_guest_devices udp dport 1900 counter accept`,
		`iifname "ens19" ether saddr @client_policy_guest_devices ip daddr 10.0.0.0/8 counter log prefix "routerd client-policy guest-devices deny " drop`,
		`iifname "ens19" ether saddr @client_policy_guest_devices ip6 daddr fc00::/7 counter log prefix "routerd client-policy guest-devices deny " drop`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesClientPolicyShortFormUsesTrustZonesAndDiscoveryDeny(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"},
			Metadata: api.ObjectMeta{Name: "guest-short"},
			Spec: api.ClientPolicySpec{
				Mode: "include",
				MACs: []string{"18:ec:e7:33:12:6c"},
				Isolation: api.ClientPolicyIsolationSpec{
					LANInternet:   "allow",
					LANLAN:        "deny",
					LANMgmt:       "deny",
					MDNSBroadcast: "deny",
				},
			},
		},
	}}}
	data, err := NftablesFirewall(router, nil)
	if err != nil {
		t.Fatalf("render nftables firewall: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`set client_policy_guest_short { type ether_addr; elements = { 18:ec:e7:33:12:6c } }`,
		`iifname "ens19" ether saddr @client_policy_guest_short ip daddr 224.0.0.251 udp dport 5353 counter log prefix "routerd client-policy guest-short discovery deny " drop`,
		`iifname "ens19" ether saddr @client_policy_guest_short ip daddr 239.255.255.250 udp dport 1900 counter log prefix "routerd client-policy guest-short discovery deny " drop`,
		`iifname "ens19" ether saddr @client_policy_guest_short udp dport { 137, 138 } counter log prefix "routerd client-policy guest-short netbios deny " drop`,
		`iifname "ens19" ether saddr @client_policy_guest_short ip daddr 192.168.0.0/16 counter log prefix "routerd client-policy guest-short deny " drop`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesClientPolicyExcludeTrustedMACs(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"},
			Metadata: api.ObjectMeta{Name: "byod-default-guest"},
			Spec: api.ClientPolicySpec{
				Mode:       "exclude",
				Interfaces: []string{"lan"},
				Classification: []api.ClientPolicyClassSpec{{
					MACAddress: "02:00:00:00:00:10",
					As:         "trusted",
				}},
			},
		},
	}}}
	data, err := NftablesFirewall(router, nil)
	if err != nil {
		t.Fatalf("render nftables firewall: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`set client_policy_byod_default_guest { type ether_addr; elements = { 02:00:00:00:00:10 } }`,
		`iifname "ens19" ether saddr != @client_policy_byod_default_guest ip daddr 192.168.0.0/16 counter log prefix "routerd client-policy byod-default-guest deny " drop`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}
