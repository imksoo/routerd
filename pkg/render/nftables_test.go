// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

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

func TestNftablesObservesInitialPacketsForTrafficFlowDPI(t *testing.T) {
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
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TrafficFlowLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec: api.TrafficFlowLogSpec{
				Enabled:                 true,
				IncludeApplicationLayer: true,
				IncludeTLSSNI:           true,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec: api.FirewallLogSpec{
				Enabled:    true,
				NFLogGroup: 7,
				Log:        api.FirewallLogPolicySpec{AcceptSampleRate: 0, CopyRange: 512},
			},
		},
	}}}
	data, err := NftablesFirewall(router, nil)
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"chain dpi_observe",
		"type filter hook forward priority filter + 1; policy accept;",
		`ct state { new, established } ct packets < 10 log prefix "routerd dpi observe accept " group 7 snaplen 2048`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("nftables output missing DPI observation rule %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "routerd firewall forward accept") {
		t.Fatalf("acceptSampleRate=0 must keep accepted-event sampling disabled:\n%s", got)
	}
}

func TestNftablesAllowsWANIPv6ClientControlPlane(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
					Metadata: api.ObjectMeta{Name: "wan"},
					Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false},
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
			Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "ens18", Managed: false},
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
			Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false},
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
			Spec:     api.InterfaceSpec{IfName: "ens19", Managed: false},
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
