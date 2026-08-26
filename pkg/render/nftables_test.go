// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func TestNftOutboundAliasesResolveWireGuardIfName(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-svnet1"},
			Spec:     api.WireGuardInterfaceSpec{IfName: "wg-transport0"},
		},
	}}}
	aliases, err := nftOutboundAliases(router)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"wg-svnet1": "wg-transport0"}
	if !reflect.DeepEqual(aliases, want) {
		t.Fatalf("aliases = %#v, want %#v", aliases, want)
	}
}

func TestNftablesFirewallKeepsStandbyVMACAndDSLiteInterfaces(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth0", Managed: false}},
		// These are intentionally declared even while the backup keeps them
		// down.  A promoted node must already classify return packets from
		// its replicated conntrack table before each tunnel link is recreated.
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-vmac"}, Spec: api.InterfaceSpec{IfName: "wan-vmac", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.InterfaceSpec{IfName: "ds-lite-a", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "ds-lite-b"}, Spec: api.InterfaceSpec{IfName: "ds-lite-b", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "ds-lite-c"}, Spec: api.InterfaceSpec{IfName: "ds-lite-c", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "ds-lite-ra"}, Spec: api.InterfaceSpec{IfName: "ds-lite-ra", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"Interface/wan", "Interface/wan-vmac", "Interface/ds-lite-a", "Interface/ds-lite-b", "Interface/ds-lite-c", "Interface/ds-lite-ra"}}},
	}}}

	data, err := NftablesFirewall(router, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"\"wan-vmac\"", "\"ds-lite-a\"", "\"ds-lite-b\"", "\"ds-lite-c\"", "\"ds-lite-ra\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("standby firewall is missing %s:\n%s", want, got)
		}
	}
}

func TestNftablesNAT44RuleSourceNATFields(t *testing.T) {
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

	data, err := NftablesNAT44Rule(router)
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

func TestNftablesRouterdTablesCarryOwnerMarker(t *testing.T) {
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
					OutboundInterface: "wan",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
				},
			},
		}},
	}
	data, err := NftablesNAT44(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	want := `comment "` + NftablesRouterdOwnerMarker + `"`
	if !strings.Contains(string(data), want) {
		t.Fatalf("nftables output missing owner marker %q:\n%s", want, string(data))
	}
}

func TestNftablesNAT44RuleCanUseDSLiteTunnel(t *testing.T) {
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
				Metadata: api.ObjectMeta{Name: "lan-to-transix"},
				Spec: api.NAT44RuleSpec{
					OutboundInterface: "transix",
					SourceCIDRs:       []string{"192.168.10.0/24"},
					Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
				},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
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
	data, err := NftablesNAT44Rule(router)
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

func TestNftablesNAT44FlowDNATPinhole(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a", Interface: "lan", AFTRIPv6: "2001:db8::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "atomcam-clients"}, Spec: api.IPAddressSetSpec{Addresses: []string{"172.18.1.92"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "lan-to-dslite"}, Spec: api.NAT44RuleSpec{
			Type:            "masquerade",
			EgressInterface: "ds-lite-a",
			SourceRanges:    []string{"172.18.0.0/16"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44FlowDNATPinhole"}, Metadata: api.ObjectMeta{Name: "atomcam-cloud-udp"}, Spec: api.NAT44FlowDNATPinholeSpec{
			FromInterfaceRefs: []string{"DSLiteTunnel/ds-lite-a"},
			Outbound: api.NAT44FlowDNATPinholeOutboundSpec{
				SourceSetRef:     "IPAddressSet/atomcam-clients",
				Protocol:         "udp",
				DestinationPorts: []api.FirewallPort{"32100"},
			},
			Inbound: api.NAT44FlowDNATPinholeInboundSpec{SourcePorts: []api.FirewallPort{"32099"}},
			Timeout: "3m",
		}},
	}}}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`map flow_dnat_atomcam_cloud_udp { type inet_service : ipv4_addr . inet_service; flags dynamic,timeout; }`,
		`iifname "ds-lite-a" udp sport 32099 dnat ip addr . port to udp dport map @flow_dnat_atomcam_cloud_udp comment "routerd NAT44FlowDNATPinhole atomcam-cloud-udp dnat"`,
		`oifname "ds-lite-a" ip saddr @ip_address_set_atomcam_clients udp dport 32100 update @flow_dnat_atomcam_cloud_udp { udp sport timeout 180s : ip saddr . udp sport } counter comment "routerd NAT44FlowDNATPinhole atomcam-cloud-udp learn"`,
		`oifname "ds-lite-a" ip saddr 172.18.0.0/16 masquerade`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nat44 output missing %q:\n%s", want, got)
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
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "skipped: egressInterface is resolved by ipv4-default at runtime") {
		t.Fatalf("nftables output missing runtime-resolved NAT44 skip comment:\n%s", got)
	}
}

func TestNftablesDefaultRoutePolicyFlushesTable(t *testing.T) {
	data, err := NftablesEgressRoutePolicyDefaultMarks(
		"net.routerd.net/v1alpha1/EgressRoutePolicy/default",
		api.EgressRoutePolicySpec{SourceCIDRs: []string{"172.18.0.0/16"}},
		api.EgressRoutePolicyCandidate{Name: "wan", Mark: 100},
		[]api.EgressRoutePolicyCandidate{{Name: "wan", Mark: 100}},
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

	data, err := NftablesNAT44Rule(router)
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

	data, err := NftablesNAT44Rule(router)
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

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table inet routerd_filter",
		"flush table inet routerd_filter",
		"destroy set inet routerd_filter if_wan",
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

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	want := `meta l4proto ipv6-icmp counter accept`
	if !strings.Contains(got, want) {
		t.Fatalf("nftables output missing bridge ICMP accept rule %q:\n%s", want, got)
	}
}

func TestNftablesNAT44RuleAddressPortRange(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ds0", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
				Metadata: api.ObjectMeta{Name: "lan-to-wan"},
				Spec: api.NAT44RuleSpec{
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

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	if !strings.Contains(string(data), `snat to 192.0.0.2:1024-65535`) {
		t.Fatalf("nftables output missing address SNAT port range:\n%s", string(data))
	}
}

func TestNftablesEgressRoutePolicyMark(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "lan-via-transix"},
				Spec: api.EgressRoutePolicySpec{
					Mode:             "mark",
					SourceCIDRs:      []string{"192.168.10.0/24"},
					DestinationCIDRs: []string{"0.0.0.0/0"},
					Candidates: []api.EgressRoutePolicyCandidate{{
						Interface: "transix",
						Table:     100,
						Priority:  10000,
						Mark:      256,
					}},
				},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
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

func TestNftablesEgressRoutePolicyHash(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "lan-balance"},
				Spec: api.EgressRoutePolicySpec{
					Mode:             "hash",
					HashFields:       []string{"sourceAddress", "destinationAddress"},
					SourceCIDRs:      []string{"192.168.10.0/24"},
					DestinationCIDRs: []string{"0.0.0.0/0"},
					Candidates: []api.EgressRoutePolicyCandidate{{
						Name: "lan-balance",
						Targets: []api.EgressRoutePolicyTarget{
							{Interface: "transix-a", Table: 100, Priority: 10000, Mark: 256},
							{Interface: "transix-b", Table: 101, Priority: 10001, Mark: 257},
						},
					}},
				},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark != 0x0 meta mark set ct mark",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark 0x0 meta mark set jhash ip saddr . ip daddr mod 256 seed 0xf643cafe map {",
		"ip saddr 192.168.10.0/24 ip daddr 0.0.0.0/0 ct mark 0x0 ct mark set meta mark",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesEgressRoutePolicyHashExcludesDestinations(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "lan-balance"},
				Spec: api.EgressRoutePolicySpec{
					Mode:                    "hash",
					HashFields:              []string{"sourceAddress"},
					SourceCIDRs:             []string{"172.18.0.0/16"},
					DestinationCIDRs:        []string{"0.0.0.0/0"},
					ExcludeDestinationCIDRs: []string{"192.168.1.0/24", "192.168.123.0/24"},
					Candidates: []api.EgressRoutePolicyCandidate{{
						Name: "dslite",
						Targets: []api.EgressRoutePolicyTarget{
							{Interface: "ds-lite-a", Table: 110, Priority: 10110, Mark: 0x110},
							{Interface: "ds-lite-b", Table: 111, Priority: 10111, Mark: 0x111},
						},
					}},
				},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	want := "ip saddr 172.18.0.0/16 ip daddr 0.0.0.0/0 ip daddr !=192.168.1.0/24 ip daddr !=192.168.123.0/24 ct mark 0x0 meta mark set jhash ip saddr mod 256 seed 0xf643cafe map {"
	if !strings.Contains(got, want) {
		t.Fatalf("nftables output missing excluded destination match %q:\n%s", want, got)
	}
}

func TestEgressRoutePolicyRendezvousHashKeepsUnaffectedBucketsStable(t *testing.T) {
	targets := []api.EgressRoutePolicyTarget{
		{Name: "a", Interface: "ds-lite-a", Table: 110, Mark: 0x110},
		{Name: "b", Interface: "ds-lite-b", Table: 111, Mark: 0x111},
		{Name: "c", Interface: "ds-lite-c", Table: 112, Mark: 0x112},
	}
	all := egressHashBucketMarks(targets)
	reordered := egressHashBucketMarks([]api.EgressRoutePolicyTarget{targets[2], targets[0], targets[1]})
	if !reflect.DeepEqual(all, reordered) {
		t.Fatal("target order changed rendezvous bucket ownership")
	}

	withoutB := egressHashBucketMarks([]api.EgressRoutePolicyTarget{targets[0], targets[2]})
	bucketsOwnedByB := 0
	for bucket, mark := range all {
		if mark == targets[1].Mark {
			bucketsOwnedByB++
			continue
		}
		if withoutB[bucket] != mark {
			t.Fatalf("removing b changed unaffected bucket %d from %#x to %#x", bucket, mark, withoutB[bucket])
		}
	}
	if bucketsOwnedByB == 0 {
		t.Fatal("test fixture assigned no buckets to b")
	}
	if recovered := egressHashBucketMarks(targets); !reflect.DeepEqual(all, recovered) {
		t.Fatal("target recovery did not restore the original bucket assignment")
	}

	withoutC := egressHashBucketMarks(targets[:2])
	for bucket, mark := range all {
		if mark != targets[2].Mark && withoutC[bucket] != mark {
			t.Fatalf("adding c changed unaffected bucket %d from %#x to %#x", bucket, withoutC[bucket], mark)
		}
	}
}

func TestEgressRoutePolicyRendezvousHashUsesOnlyRuntimeReadyTargets(t *testing.T) {
	ready := true
	unready := false
	candidate := api.EgressRoutePolicyCandidate{Name: "dslite", Targets: []api.EgressRoutePolicyTarget{
		{Name: "a", Interface: "ds-lite-a", Table: 110, Mark: 0x110, RuntimeReady: &ready},
		{Name: "b", Interface: "ds-lite-b", Table: 111, Mark: 0x111, RuntimeReady: &unready},
		{Name: "c", Interface: "ds-lite-c", Table: 112, Mark: 0x112, RuntimeReady: &ready},
	}}
	markMap, err := nftEgressRoutePolicyTargetMarkMap("EgressRoutePolicy/ipv4-default", 0, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markMap, nftMark(0x111)) {
		t.Fatalf("runtime-unready target was present in mark map: %s", markMap)
	}
	for _, want := range []string{nftMark(0x110), nftMark(0x112)} {
		if !strings.Contains(markMap, want) {
			t.Fatalf("ready target %s was absent from mark map", want)
		}
	}
}

func TestEgressRoutePolicyRendezvousHashSyntaxSmoke(t *testing.T) {
	if os.Getenv("ROUTERD_NFT_SYNTAX") != "1" {
		t.Skip("set ROUTERD_NFT_SYNTAX=1 to run nft syntax smoke")
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare is not installed")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft is not installed")
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
		Metadata: api.ObjectMeta{Name: "ipv4-default"},
		Spec: api.EgressRoutePolicySpec{Mode: "hash", HashFields: []string{"sourceAddress"}, Candidates: []api.EgressRoutePolicyCandidate{{
			Name: "dslite",
			Targets: []api.EgressRoutePolicyTarget{
				{Name: "a", Interface: "ds-lite-a", Table: 110, Mark: 0x110},
				{Name: "b", Interface: "ds-lite-b", Table: 111, Mark: 0x111},
				{Name: "c", Interface: "ds-lite-c", Table: 112, Mark: 0x112},
			},
		}}},
	}}}}
	data, err := NftablesIPv4PolicyRoutes(router)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("unshare", "-Urn", "nft", "-c", "-f", "-")
	cmd.Stdin = strings.NewReader(string(data))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nft syntax check failed: %v\n%s\n%s", err, output, data)
	}
}

func egressHashBucketMarks(targets []api.EgressRoutePolicyTarget) []int {
	candidate := api.EgressRoutePolicyCandidate{Name: "dslite", Targets: targets}
	marks := make([]int, nftEgressRoutePolicyHashBuckets)
	for bucket := range marks {
		marks[bucket] = rendezvousEgressTarget("EgressRoutePolicy/ipv4-default", 0, candidate, targets, bucket).Mark
	}
	return marks
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
				Metadata: api.ObjectMeta{Name: "wan-pppoe"},
				Spec: api.PPPoESessionSpec{
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
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan-pppoe", "ds-lite-a"}},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table inet routerd_mss",
		"flush table inet routerd_mss",
		"table inet routerd_mss",
		"type filter hook forward priority mangle; policy accept;",
		`iifname "ens19" oifname "ds-lite-a" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1414 tcp option maxseg size set 1414`,
		`iifname "ens19" oifname "ds-lite-a" meta nfproto ipv6 tcp flags syn / syn,rst tcp option maxseg size > 1394 tcp option maxseg size set 1394`,
		`iifname "ens19" oifname "ppp0" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1452 tcp option maxseg size set 1452`,
		`iifname "ens19" oifname "ppp0" meta nfproto ipv6 tcp flags syn / syn,rst tcp option maxseg size > 1432 tcp option maxseg size set 1432`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesTCPMSSClampOnlyLowersMSS(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "tailscale"},
				Spec:     api.InterfaceSpec{IfName: "tailscale0", MTU: 1280, Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
				Metadata: api.ObjectMeta{Name: "ds-lite-a"},
				Spec:     api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-lite-a", RemoteAddress: "2001:db8::1"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan", "tailscale"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite-a"}},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`iifname "ens19" oifname "ds-lite-a" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1414 tcp option maxseg size set 1414`,
		`iifname "ens19" oifname "ds-lite-a" meta nfproto ipv6 tcp flags syn / syn,rst tcp option maxseg size > 1394 tcp option maxseg size set 1394`,
		`iifname "tailscale0" oifname "ds-lite-a" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1240 tcp option maxseg size set 1240`,
		`iifname "tailscale0" oifname "ds-lite-a" meta nfproto ipv6 tcp flags syn / syn,rst tcp option maxseg size > 1220 tcp option maxseg size set 1220`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing guarded MSS clamp %q:\n%s", want, got)
		}
	}
}

func TestNftablesL2TCPMSSClampForVXLANTunnelBridge(t *testing.T) {
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "l2"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19", MTU: 1500, Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "legacy"}, Spec: api.BridgeSpec{IfName: "br-legacy", Members: []string{"lan"}, MTU: 1370}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wg"}, Spec: api.InterfaceSpec{IfName: "wg0", MTU: 1420, Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "overlay"}, Spec: api.VXLANTunnelSpec{IfName: "vx-l2", VNI: 200001, LocalAddress: "198.18.0.1", Peers: []string{"198.18.0.2"}, UnderlayInterface: "wg", MTU: 1280, Bridge: "legacy", TCPMSSClamp: true}},
	}}}
	data, err := NftablesL2TCPMSSClamp(router)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"table bridge routerd_l2_mss",
		NftablesL2MSSPrivateProofMarker,
		`iifname "ens19" oifname "vx-l2" ether type ip ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1240 tcp option maxseg size set 1240`,
		`iifname "vx-l2" oifname "ens19" ether type ip6 meta l4proto tcp tcp flags syn / syn,rst tcp option maxseg size > 1220 tcp option maxseg size set 1220`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("L2 MSS output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "maxseg size set 1460") {
		t.Fatalf("rule must lower only to the effective MTU ceiling:\n%s", got)
	}
}

func TestNftablesL2TCPMSSClampDisabledProducesNoTable(t *testing.T) {
	router := bridgeVXLANL2MSSRouter(false)
	data, err := NftablesL2TCPMSSClamp(router)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("disabled clamp rendered state:\n%s", data)
	}
}

func bridgeVXLANL2MSSRouter(enabled bool) *api.Router {
	return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "l2"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", MTU: 1500, Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br"}, Spec: api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "underlay0", MTU: 1500, Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx"}, Spec: api.VXLANTunnelSpec{IfName: "vx0", VNI: 42, LocalAddress: "198.18.0.1", UnderlayInterface: "underlay", MTU: 1280, Bridge: "br", TCPMSSClamp: enabled}},
	}}}
}

func TestNftablesIPv4ForceFragmentForBGPMobilityOverlay(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-hybrid"},
			Spec:     api.WireGuardInterfaceSpec{MTU: 1420},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
			Metadata: api.ObjectMeta{Name: "onprem-main"},
			Spec: api.OverlayPeerSpec{
				Role:     "onprem",
				NodeID:   "onprem-router",
				Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-hybrid"},
				PathMTU:  api.PathMTUOptions{ForceFragmentIPv4: true},
			},
		},
	}}}
	data, err := NftablesIPv4ForceFragmentWithLocalCaptureIntents(router, []dynamicconfig.LocalCaptureIntent{{
		ID: "cloudedge/10.77.60.9", PoolRef: "cloudedge", Address: "10.77.60.9/32",
		Disposition: dynamicconfig.CaptureProtectExisting, CaptureType: "provider-secondary-ip", CaptureInterface: "ens3",
	}})
	if err != nil {
		t.Fatalf("render forcefrag: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table ip routerd_forcefrag",
		"flush table ip routerd_forcefrag",
		"table ip routerd_forcefrag",
		`type filter hook prerouting priority mangle; policy accept;`,
		`iifname "ens3" fib daddr oifname "wg-hybrid" ip length > 1340 ip frag-off 0x4000 ip frag-off set 0`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("forcefrag output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "meta nfproto ipv6") {
		t.Fatalf("forcefrag must be IPv4-only:\n%s", got)
	}
}

func TestNftablesTCPMSSClampForBGPMobilityOverlay(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
				Metadata: api.ObjectMeta{Name: "wg-hybrid"},
				Spec:     api.WireGuardInterfaceSpec{MTU: 1420},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
				Metadata: api.ObjectMeta{Name: "onprem-main"},
				Spec: api.OverlayPeerSpec{
					Role:     "onprem",
					NodeID:   "onprem-router",
					Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-hybrid"},
				},
			},
		}},
	}

	data, err := NftablesTCPMSSClampWithLocalCaptureIntents(router, []dynamicconfig.LocalCaptureIntent{{
		ID: "cloudedge/10.77.60.9", PoolRef: "cloudedge", Address: "10.77.60.9/32",
		Disposition: dynamicconfig.CaptureProtectExisting, CaptureType: "provider-secondary-ip", CaptureInterface: "ens3",
	}})
	if err != nil {
		t.Fatalf("render TCP MSS clamp: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"table inet routerd_mss",
		`iifname "ens3" oifname "wg-hybrid" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1300 tcp option maxseg size set 1300`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing BGP mobility clamp %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `iifname "ens21"`) {
		t.Fatalf("BGP mobility MSS clamp should only use the self member capture interface:\n%s", got)
	}
	if strings.Contains(got, "meta nfproto ipv6") {
		t.Fatalf("BGP mobility MSS clamp should be IPv4-only:\n%s", got)
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
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
				Metadata: api.ObjectMeta{Name: "wan-pppoe"},
				Spec: api.PPPoESessionSpec{
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
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"},
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

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"add table inet routerd_filter",
		"flush table inet routerd_filter",
		"destroy set inet routerd_filter if_lan",
		"destroy set inet routerd_filter if_wan",
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

func TestNftablesPortForwardRendersDNATAndFirewallAccept(t *testing.T) {
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
				Spec:     api.InterfaceSpec{IfName: "ens19"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "PortForward"},
				Metadata: api.ObjectMeta{Name: "web-admin"},
				Spec: api.PortForwardSpec{
					Listen: api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 8443},
					Target: api.IngressTargetSpec{Address: "172.18.1.88", Port: 443},
					Hairpin: api.IngressHairpinSpec{
						Enabled:    true,
						Interfaces: []string{"lan"},
					},
				},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`type nat hook prerouting priority dstnat; policy accept;`,
		`iifname "ens18" ip daddr 203.0.113.10 tcp dport 8443 counter dnat to 172.18.1.88:443 comment "routerd PortForward web-admin"`,
		`iifname "ens19" ip daddr 203.0.113.10 tcp dport 8443 counter dnat to 172.18.1.88:443 comment "routerd PortForward web-admin hairpin"`,
		`iifname "ens19" ip daddr 172.18.1.88 tcp dport 443 ct original ip daddr 203.0.113.10 ct original proto-dst 8443 counter masquerade comment "routerd PortForward web-admin hairpin"`,
		`iifname "ens18" ip daddr 172.18.1.88 tcp dport 443 counter accept comment "routerd PortForward web-admin"`,
		`iifname "ens19" ip daddr 172.18.1.88 tcp dport 443 counter accept comment "routerd PortForward web-admin hairpin"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesIngressServiceResolvesListenAddressFromStaticAddress(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "wan-ip"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "wan", Address: "203.0.113.10/32"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
				Metadata: api.ObjectMeta{Name: "app"},
				Spec: api.IngressServiceSpec{
					Listen:   api.IngressListenSpec{Interface: "wan", AddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/wan-ip", Field: "address"}, Protocol: "udp", Port: 5353},
					Backends: []api.IngressBackendSpec{{Address: "172.18.1.89", Port: 5353}},
				},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	want := `iifname "ens18" ip daddr 203.0.113.10 udp dport 5353 counter dnat to 172.18.1.89:5353 comment "routerd IngressService app"`
	if !strings.Contains(got, want) {
		t.Fatalf("nftables output missing %q:\n%s", want, got)
	}
}

func TestNftablesIngressServiceAutoHairpinForSameInterfaceSubnet(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-ip"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.168.1.1/24"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "kubernetes-api"}, Spec: api.IngressServiceSpec{
				Listen: api.IngressListenSpec{Interface: "lan", Address: "192.168.1.248", Protocol: "tcp", Port: 6443},
				Backends: []api.IngressBackendSpec{
					{Name: "cp-01", Address: "192.168.1.54", Port: 6443},
					{Name: "cp-02", Address: "192.168.1.55", Port: 6443},
				},
			}},
		}},
	}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`iifname "ens18" ip daddr 192.168.1.248 tcp dport 6443 counter dnat to 192.168.1.54:6443 comment "routerd IngressService kubernetes-api"`,
		`iifname "ens18" ip daddr 192.168.1.54 tcp dport 6443 ct original ip daddr 192.168.1.248 ct original proto-dst 6443 counter masquerade comment "routerd IngressService kubernetes-api hairpin"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `dnat to 192.168.1.54:6443 comment "routerd IngressService kubernetes-api hairpin"`) {
		t.Fatalf("unexpected duplicate same-interface hairpin DNAT:\n%s", got)
	}
}

func TestNftablesIngressServiceAutoHairpinFallsBackToPrivateSlash24(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "kubernetes-api"}, Spec: api.IngressServiceSpec{
				Listen: api.IngressListenSpec{Interface: "lan", Address: "192.168.1.248", Protocol: "tcp", Port: 6443},
				Backends: []api.IngressBackendSpec{
					{Name: "cp-01", Address: "192.168.1.54", Port: 6443},
					{Name: "cp-02", Address: "192.168.1.55", Port: 6443},
					{Name: "cp-03", Address: "192.168.1.56", Port: 6443},
				},
			}},
		}},
	}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	want := `iifname "eth0" ip daddr 192.168.1.54 tcp dport 6443 ct original ip daddr 192.168.1.248 ct original proto-dst 6443 counter masquerade comment "routerd IngressService kubernetes-api hairpin"`
	if !strings.Contains(got, want) {
		t.Fatalf("nftables output missing fallback auto hairpin SNAT %q:\n%s", want, got)
	}
}

func TestNftablesIngressServiceSourceHashDistributesBackends(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
				Metadata: api.ObjectMeta{Name: "api"},
				Spec: api.IngressServiceSpec{
					Listen: api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 6443},
					Backends: []api.IngressBackendSpec{
						{Name: "cp-01", Address: "10.0.0.11", Port: 6443},
						{Name: "cp-02", Address: "10.0.0.12", Port: 6443},
					},
					Policy: api.IngressServicePolicySpec{Selection: "sourceHash"},
				},
			},
		}},
	}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`chain ingress_ingressservice_api_0`,
		`tcp dport 6443 counter dnat to 10.0.0.11:6443 comment "routerd IngressService api cp-01"`,
		`tcp dport 6443 counter dnat to 10.0.0.12:6443 comment "routerd IngressService api cp-02"`,
		`iifname "ens18" ip daddr 203.0.113.10 tcp dport 6443 jhash ip saddr mod 2 vmap { 0 : jump ingress_ingressservice_api_0, 1 : jump ingress_ingressservice_api_1 } comment "routerd IngressService api"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesIngressServiceRandomDistributesBackends(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
				Listen:   api.IngressListenSpec{Interface: "wan", Protocol: "udp", Port: 5353},
				Backends: []api.IngressBackendSpec{{Name: "a", Address: "10.0.0.11", Port: 5353}, {Name: "b", Address: "10.0.0.12", Port: 5353}},
				Policy:   api.IngressServicePolicySpec{Selection: "random"},
			}},
		}},
	}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	if got := string(data); !strings.Contains(got, `udp dport 5353 numgen random mod 2 vmap`) {
		t.Fatalf("nftables output missing random distribution:\n%s", got)
	}
}

func TestNftablesLocalServiceRedirectUsesDestinationAddressSets(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "lan"},
				Spec:     api.InterfaceSpec{IfName: "ens19"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"},
				Metadata: api.ObjectMeta{Name: "public-dns"},
				Spec:     api.IPAddressSetSpec{Addresses: []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"},
				Metadata: api.ObjectMeta{Name: "public-ntp"},
				Spec:     api.IPAddressSetSpec{Names: []string{"ntp.example.test"}},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "LocalServiceRedirect"},
				Metadata: api.ObjectMeta{Name: "lan-local-services"},
				Spec: api.LocalServiceRedirectSpec{
					Interface: "lan",
					Rules: []api.LocalServiceRedirectRuleSpec{
						{
							Name:              "public-dns",
							Protocols:         []string{"udp", "tcp"},
							DestinationSetRef: "IPAddressSet/public-dns",
							DestinationPort:   53,
							RedirectPort:      53,
						},
						{
							Name:              "public-ntp",
							Protocols:         []string{"udp"},
							DestinationSetRef: "public-ntp",
							DestinationPort:   123,
							RedirectPort:      123,
						},
					},
				},
			},
		}},
	}

	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`set ip_address_set_public_dns { type ipv4_addr; elements = { 1.1.1.1, 8.8.8.8 }; }`,
		`set ip_address_set_public_ntp { type ipv4_addr; }`,
		`table ip6 routerd_nat`,
		`set ip_address_set_public_dns { type ipv6_addr; elements = { 2001:4860:4860::8888 }; }`,
		`set ip_address_set_public_ntp { type ipv6_addr; }`,
		`iifname "ens19" ip daddr @ip_address_set_public_dns tcp dport 53 counter redirect to :53 comment "routerd LocalServiceRedirect lan-local-services public-dns"`,
		`iifname "ens19" ip daddr @ip_address_set_public_dns udp dport 53 counter redirect to :53 comment "routerd LocalServiceRedirect lan-local-services public-dns"`,
		`iifname "ens19" ip daddr @ip_address_set_public_ntp udp dport 123 counter redirect to :123 comment "routerd LocalServiceRedirect lan-local-services public-ntp"`,
		`iifname "ens19" ip6 daddr @ip_address_set_public_dns tcp dport 53 counter redirect to :53 comment "routerd LocalServiceRedirect lan-local-services public-dns"`,
		`iifname "ens19" ip6 daddr @ip_address_set_public_dns udp dport 53 counter redirect to :53 comment "routerd LocalServiceRedirect lan-local-services public-dns"`,
		`iifname "ens19" ip6 daddr @ip_address_set_public_ntp udp dport 123 counter redirect to :123 comment "routerd LocalServiceRedirect lan-local-services public-ntp"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "dport 443") || strings.Contains(got, "dport 853") {
		t.Fatalf("local redirect must not touch DoH or DoT ports:\n%s", got)
	}
}

func TestNftablesIPv4PolicyRouteUsesDestinationAddressSet(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"},
			Metadata: api.ObjectMeta{Name: "cloud-service"},
			Spec: api.IPAddressSetSpec{
				Names: []string{"service.example.test"},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
			Metadata: api.ObjectMeta{Name: "cloud-via-alt"},
			Spec: api.EgressRoutePolicySpec{
				Mode:                    "mark",
				DestinationSetRefs:      []string{"IPAddressSet/cloud-service"},
				ExcludeDestinationCIDRs: []string{"10.0.0.0/8"},
				Candidates: []api.EgressRoutePolicyCandidate{{
					Interface: "wan-alt",
					Table:     200,
					Priority:  1200,
					Mark:      0x120,
				}},
			},
		},
	}}}
	data, err := NftablesIPv4PolicyRoutes(router)
	if err != nil {
		t.Fatalf("render policy route: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`table ip routerd_policy`,
		`set ip_address_set_cloud_service { type ipv4_addr; }`,
		`ip daddr @ip_address_set_cloud_service ip daddr !=10.0.0.0/8 meta mark set 0x120`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("policy route output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesFirewallRuleUsesDestinationAddressSet(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "cloud-service"}, Spec: api.IPAddressSetSpec{
			Names: []string{"service.example.test"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "lan-to-cloud"}, Spec: api.FirewallRuleSpec{
			FromZone:           "lan",
			ToZone:             "wan",
			Protocol:           "tcp",
			Port:               443,
			DestinationSetRefs: []string{"IPAddressSet/cloud-service"},
			Action:             "accept",
		}},
	}}}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`set ip_address_set_cloud_service_v4 { type ipv4_addr; }`,
		`set ip_address_set_cloud_service_v6 { type ipv6_addr; }`,
		`ip daddr @ip_address_set_cloud_service_v4 tcp dport 443 counter accept`,
		`ip6 daddr @ip_address_set_cloud_service_v6 tcp dport 443 counter accept`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("firewall output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesFirewallFlowPinhole(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ds-lite-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "atomcam-clients"}, Spec: api.IPAddressSetSpec{
			Addresses: []string{"172.18.1.92"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallFlowPinhole"}, Metadata: api.ObjectMeta{Name: "atomcam-cloud-udp"}, Spec: api.FirewallFlowPinholeSpec{
			FromZone: "lan",
			ToZone:   "wan",
			Outbound: api.FirewallFlowPinholeOutboundSpec{
				SourceSetRef:     "IPAddressSet/atomcam-clients",
				Protocol:         "udp",
				DestinationPorts: []api.FirewallPort{"32100"},
				DestinationCIDRs: []string{"3.114.54.243/32"},
			},
			Inbound: api.FirewallFlowPinholeInboundSpec{SourcePorts: []api.FirewallPort{"32099"}},
			Timeout: "3m",
		}},
	}}}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`set flow_pinhole_atomcam_cloud_udp { type ipv4_addr . ipv4_addr . inet_service; flags timeout; }`,
		`set ip_address_set_atomcam_clients_v4 { type ipv4_addr; elements = { 172.18.1.92 }; }`,
		`ip saddr @ip_address_set_atomcam_clients_v4 ip daddr 3.114.54.243/32 udp dport 32100 update @flow_pinhole_atomcam_cloud_udp { ip daddr . ip saddr . udp sport timeout 180s } counter comment "routerd FirewallFlowPinhole atomcam-cloud-udp learn"`,
		`ip saddr 3.114.54.243/32 ip daddr @ip_address_set_atomcam_clients_v4 udp sport 32099 ip saddr . ip daddr . udp dport @flow_pinhole_atomcam_cloud_udp counter accept comment "routerd FirewallFlowPinhole atomcam-cloud-udp allow"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("firewall output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesFirewallFlowPinholeLocalEndpointCorrelation(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ds-lite-a"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPAddressSet"}, Metadata: api.ObjectMeta{Name: "atomcam-clients"}, Spec: api.IPAddressSetSpec{
			Addresses: []string{"172.18.1.92"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallFlowPinhole"}, Metadata: api.ObjectMeta{Name: "atomcam-cloud-udp"}, Spec: api.FirewallFlowPinholeSpec{
			FromZone: "lan",
			ToZone:   "wan",
			Outbound: api.FirewallFlowPinholeOutboundSpec{
				SourceSetRef:     "IPAddressSet/atomcam-clients",
				Protocol:         "udp",
				DestinationPorts: []api.FirewallPort{"32100"},
				DestinationCIDRs: []string{"3.114.54.243/32"},
			},
			Inbound:     api.FirewallFlowPinholeInboundSpec{SourcePorts: []api.FirewallPort{"32099"}},
			Correlation: api.FirewallFlowPinholeCorrelationSpec{Key: "localEndpoint"},
			Timeout:     "3m",
		}},
	}}}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`set flow_pinhole_atomcam_cloud_udp { type ipv4_addr . inet_service; flags timeout; }`,
		`ip saddr @ip_address_set_atomcam_clients_v4 ip daddr 3.114.54.243/32 udp dport 32100 update @flow_pinhole_atomcam_cloud_udp { ip saddr . udp sport timeout 180s } counter comment "routerd FirewallFlowPinhole atomcam-cloud-udp learn"`,
		`ip saddr 3.114.54.243/32 ip daddr @ip_address_set_atomcam_clients_v4 udp sport 32099 ip daddr . udp dport @flow_pinhole_atomcam_cloud_udp counter accept comment "routerd FirewallFlowPinhole atomcam-cloud-udp allow"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("firewall output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesFirewallRuleStatefulExpressions(t *testing.T) {
	router := nftablesStatefulFirewallTestRouter()
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`tcp sport 1024-65535 tcp dport { 80, 443 } counter accept`,
		`ip protocol icmp icmp type echo-request counter accept`,
		`tcp dport 22 limit rate over 8/minute burst 16 packets log prefix "routerd firewall ssh-flood rate-limit " counter reject`,
		`meter routerd_conn_ssh_flood_v4 { ip saddr ct count over 4 } log prefix "routerd firewall ssh-flood conn-limit "`,
		`meta l4proto ipv6-icmp icmpv6 type nd-neighbor-solicit counter accept`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
	}
}

func TestNftablesFirewallRuleSyntaxSmoke(t *testing.T) {
	if os.Getenv("ROUTERD_NFT_SYNTAX") != "1" {
		t.Skip("set ROUTERD_NFT_SYNTAX=1 to run nft syntax smoke")
	}
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare is not installed")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft is not installed")
	}
	data, err := NftablesNAT44Rule(nftablesStatefulFirewallTestRouter())
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	cmd := exec.Command("unshare", "-Urn", "nft", "-c", "-f", "-")
	cmd.Stdin = strings.NewReader(string(data))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nft syntax check failed: %v\n%s\n%s", err, output, data)
	}
}

func nftablesStatefulFirewallTestRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "web"}, Spec: api.FirewallRuleSpec{
			FromZone:         "wan",
			ToZone:           "self",
			Protocol:         "tcp",
			SourcePorts:      []api.FirewallPort{"1024-65535"},
			DestinationPorts: []api.FirewallPort{"80", "443"},
			Action:           "accept",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "echo"}, Spec: api.FirewallRuleSpec{
			FromZone: "wan",
			ToZone:   "self",
			Protocol: "icmp",
			ICMPType: "echo-request",
			Action:   "accept",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "ssh-flood"}, Spec: api.FirewallRuleSpec{
			FromZone:         "wan",
			ToZone:           "self",
			Protocol:         "tcp",
			DestinationPorts: []api.FirewallPort{"22"},
			Action:           "reject",
			RateLimit:        api.FirewallRateLimitSpec{Rate: 8, Burst: 16, Unit: "packet", Per: "minute", Log: true},
			ConnLimit:        api.FirewallConnLimitSpec{MaxPerSource: 4, Log: true},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "nd"}, Spec: api.FirewallRuleSpec{
			FromZone:   "lan",
			ToZone:     "self",
			Protocol:   "icmpv6",
			ICMPv6Type: "neighbor-solicit",
			Action:     "accept",
		}},
	}}}
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

	data, err := NftablesNAT44Rule(router)
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
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec: api.FirewallLogSpec{
				Enabled:    true,
				NFLogGroup: 7,
				Log:        api.FirewallLogPolicySpec{AcceptSampleRate: 100, CopyRange: 2048},
			},
		},
	}}}
	data, err := NftablesNAT44Rule(router)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`ct state { new, established } numgen random mod 100 == 0 log prefix "routerd firewall forward accept " group 7 snaplen 2048`,
		`ct state new numgen random mod 100 == 0 log prefix "routerd firewall lan-to-wan accept " group 7 snaplen 2048 counter accept`,
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

func TestNftablesFirewallHolesForBGPVRRPAndIngress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "ens19"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.FirewallPolicySpec{LogDeny: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"Interface/lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "k8s"},
			Spec:     api.BGPRouterSpec{ASN: 64512, RouterID: "10.240.70.2"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "api-vip"},
			Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 50},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "api-vip-v6"},
			Spec: api.VirtualAddressSpec{Family: "ipv6",
				Interface: "lan",
				Address:   "fd00:1234::10/128",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 51},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"},
			Metadata: api.ObjectMeta{Name: "k8s-api"},
			Spec: api.IngressServiceSpec{
				Listen:   api.IngressListenSpec{Interface: "lan", Address: "10.240.70.10", Protocol: "tcp", Port: 6443},
				Backends: []api.IngressBackendSpec{{Address: "10.240.70.11", Port: 6443}},
			},
		},
	}}}
	data, err := NftablesFirewall(router, InternalFirewallHoles(router))
	if err != nil {
		t.Fatalf("render nftables firewall: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`iifname "ens19" tcp dport 179 counter accept comment "net.routerd.net/v1alpha1/BGPRouter/k8s"`,
		`iifname "ens19" ip protocol 112 counter accept comment "net.routerd.net/v1alpha1/VirtualAddress/api-vip"`,
		`iifname "ens19" ip6 nexthdr 112 counter accept comment "net.routerd.net/v1alpha1/VirtualAddress/api-vip-v6"`,
		`iifname "ens19" tcp dport 6443 counter accept comment "firewall.routerd.net/v1alpha1/IngressService/k8s-api"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing %q:\n%s", want, got)
		}
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
					Mode:  "guest",
					Name:  "aiseg2",
					Match: api.ClientPolicyClassMatchSpec{MACs: []string{"18:ec:e7:33:12:6c"}},
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
		`destroy set inet routerd_filter client_policy_guest_devices`,
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
					Mode:  "trusted",
					Match: api.ClientPolicyClassMatchSpec{MACs: []string{"02:00:00:00:00:10"}},
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
