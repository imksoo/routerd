// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestPFRenderFirewallAndNAT(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "em0", Managed: false, Owner: "external"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "em1", Managed: true, Owner: "routerd"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "mgmt"},
			Spec:     api.InterfaceSpec{IfName: "em2", Managed: true, Owner: "routerd"},
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
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "mgmt"},
			Spec:     api.FirewallZoneSpec{Role: "mgmt", Interfaces: []string{"mgmt"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
			Metadata: api.ObjectMeta{Name: "default-home"},
			Spec:     api.FirewallPolicySpec{LogDeny: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.FirewallLogSpec{Enabled: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"},
			Metadata: api.ObjectMeta{Name: "wan-ssh"},
			Spec:     api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", SourceCIDRs: []string{"192.0.2.0/24"}, DestinationCIDRs: []string{"198.51.100.10/32"}, Protocol: "tcp", Port: 22, Action: "accept", Log: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
			Metadata: api.ObjectMeta{Name: "lan-nat"},
			Spec: api.NAT44RuleSpec{
				OutboundInterface: "wan",
				SourceCIDRs:       []string{"172.18.0.0/16"},
				Translation:       api.IPv4NATTranslationSpec{Type: "interfaceAddress"},
			},
		},
	}}}
	data, err := PF(router, []FirewallHole{{Name: "dhcpv6-client", FromZone: "wan", ToZone: "self", Protocol: "udp", Port: 546, Action: "accept"}})
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`set skip on lo0`,
		`lan_if = "em1"`,
		`mgmt_if = "em2"`,
		`wan_if = "em0"`,
		`nat on em0 from 172.18.0.0/16 to any -> (em0)`,
		`block drop all`,
		`pass out quick all keep state`,
		`pass quick inet6 proto icmp6 all keep state`,
		`pass in quick on $lan_if to self keep state`,
		`pass in quick on $mgmt_if to self keep state`,
		`block drop in quick on $lan_if to (em2:network) label "routerd:lan-to-mgmt-deny"`,
		`pass in quick on $lan_if keep state label "routerd:lan-to-wan"`,
		`pass in quick on $wan_if proto udp to self port 546 keep state label "routerd:dhcpv6-client"`,
		`pass in log quick on $wan_if proto tcp from 192.0.2.0/24 to 198.51.100.10/32 port 22 keep state label "routerd:wan-ssh"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf output missing %q:\n%s", want, got)
		}
	}
}

func TestPFPortForwardRendersRDRPass(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "em0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "em1"},
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
	}}}
	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`rdr pass on em0 inet proto tcp from any to 203.0.113.10 port 8443 -> 172.18.1.88 port 443`,
		`rdr pass on em1 inet proto tcp from any to 203.0.113.10 port 8443 -> 172.18.1.88 port 443`,
		`nat on em1 inet proto tcp from (em1:network) to 172.18.1.88 port 443 -> (em1)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf output missing %q:\n%s", want, got)
		}
	}
}

func TestPFSkipsRedundantSelfHolesWhenZoneAlreadyAcceptsSelf(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "em1"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"},
			Metadata: api.ObjectMeta{Name: "lan-dhcp4"},
			Spec:     api.DHCPv4ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Server"},
			Metadata: api.ObjectMeta{Name: "lan-dhcp6"},
			Spec:     api.DHCPv6ServerSpec{Server: "dnsmasq", Managed: true, ListenInterfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: "lan-resolver"},
			Spec: api.DNSResolverSpec{
				Listen:  []api.DNSResolverListenSpec{{Addresses: []string{"192.0.2.1"}, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{{Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://1.1.1.1:53"}}},
			},
		},
	}}}
	data, err := PF(router, InternalFirewallHoles(router))
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `pass in quick on $lan_if to self keep state`) {
		t.Fatalf("pf output missing broad trust-to-self rule:\n%s", got)
	}
	for _, redundant := range []string{
		`routerd:lan-dhcp4-dhcpv4-server-lan`,
		`routerd:lan-dhcp6-dhcpv6-server-lan`,
		`routerd:lan-resolver-dns-udp-lan`,
		`routerd:lan-resolver-dns-tcp-lan`,
	} {
		if strings.Contains(got, redundant) {
			t.Fatalf("pf output should not render redundant self hole %q:\n%s", redundant, got)
		}
	}
}

func TestPFClientPolicyUsesIPv4Reservations(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet1"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Reservation"},
			Metadata: api.ObjectMeta{Name: "guest-phone"},
			Spec:     api.DHCPv4ReservationSpec{MACAddress: "02:00:00:00:00:44", IPAddress: "192.168.160.184"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallPolicy"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.FirewallPolicySpec{LogDeny: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"},
			Metadata: api.ObjectMeta{Name: "guest-devices"},
			Spec: api.ClientPolicySpec{
				Mode:          "include",
				Interfaces:    []string{"lan"},
				GuestServices: []string{"dns", "dhcp", "ntp"},
				Classification: []api.ClientPolicyClassSpec{{
					Mode:            "guest",
					Match:           api.ClientPolicyClassMatchSpec{MACs: []string{"02:00:00:00:00:44"}},
					IPv4Reservation: "guest-phone",
				}},
			},
		},
	}}}
	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`pass in quick on vtnet1 proto udp from 192.168.160.184 to self port 53 keep state label "routerd:client-policy:guest-devices:dns"`,
		`pass in quick on vtnet1 proto udp from 192.168.160.184 to self port 67 keep state label "routerd:client-policy:guest-devices:dhcp"`,
		`block drop in log quick on vtnet1 from 192.168.160.184 to 192.168.0.0/16 label "routerd:client-policy:guest-devices:deny"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `192.168.160.184 to fc00::/7`) {
		t.Fatalf("pf output must not combine IPv4 client addresses with IPv6 deny CIDRs:\n%s", got)
	}
	if strings.Contains(got, `dhcpv6-server`) {
		t.Fatalf("pf output must not render DHCPv6 service holes for IPv4-only ClientPolicy reservations:\n%s", got)
	}
}

func TestPFClientPolicyRequiresReservation(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "vtnet1"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"},
			Metadata: api.ObjectMeta{Name: "guest-devices"},
			Spec: api.ClientPolicySpec{
				Mode:       "include",
				Interfaces: []string{"lan"},
				Classification: []api.ClientPolicyClassSpec{{
					Mode:  "guest",
					Match: api.ClientPolicyClassMatchSpec{MACs: []string{"02:00:00:00:00:44"}},
				}},
			},
		},
	}}}
	_, err := PF(router, nil)
	if err == nil || !strings.Contains(err.Error(), "needs ipv4Reservation") {
		t.Fatalf("expected ipv4Reservation error, got %v", err)
	}
}

func TestPFInternalWANHolesUseOwningInterface(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "em0"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite"},
			Spec:     api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "gif41"},
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
	data, err := PF(router, InternalFirewallHoles(router))
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	want := `pass in quick on em0 proto udp to (em0) port 546 keep state label "routerd:wan-pd-dhcpv6-client"`
	if !strings.Contains(got, want) {
		t.Fatalf("pf output missing interface-scoped DHCPv6 hole %q:\n%s", want, got)
	}
	if strings.Contains(got, `pass in quick on gif41 proto udp to self port 546`) ||
		strings.Contains(got, `pass in quick on $wan_if proto udp to self port 546`) {
		t.Fatalf("pf output should not render DHCPv6 client hole on all WAN members:\n%s", got)
	}
}

func TestPFRenderNAT44SNAT(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.InterfaceSpec{IfName: "em0", Managed: false, Owner: "external"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
			Metadata: api.ObjectMeta{Name: "snat"},
			Spec: api.NAT44RuleSpec{
				Type:            "snat",
				EgressInterface: "wan",
				SourceRanges:    []string{"10.0.0.0/24"},
				SNATAddress:     "198.51.100.10",
			},
		},
	}}}
	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `nat on em0 from 10.0.0.0/24 to any -> 198.51.100.10`) {
		t.Fatalf("pf output missing SNAT rule:\n%s", got)
	}
}

func TestPFSkipsRuntimeResolvedNAT44Policy(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"},
			Metadata: api.ObjectMeta{Name: "dynamic"},
			Spec: api.NAT44RuleSpec{
				Type:            "masquerade",
				EgressPolicyRef: "EgressRoutePolicy/default",
				SourceRanges:    []string{"10.0.0.0/24"},
			},
		},
	}}}
	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `nat-anchor "routerd_nat"`) {
		t.Fatalf("pf output missing runtime NAT anchor:\n%s", got)
	}
	if !strings.Contains(got, "skipped: egressInterface is resolved by EgressRoutePolicy/default at runtime") {
		t.Fatalf("pf output missing runtime skip comment:\n%s", got)
	}
}

func TestPFNAT44RulesRenderResolvedMasquerade(t *testing.T) {
	data, err := PFNAT44Rules([]NAT44RenderRule{{
		Name:                    "lan-to-dslite",
		Type:                    "masquerade",
		EgressInterface:         "gif41",
		SourceRanges:            []string{"192.168.160.0/24"},
		ExcludeDestinationCIDRs: []string{"192.168.0.0/16", "10.0.0.0/8"},
	}})
	if err != nil {
		t.Fatalf("render pf NAT44 rules: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`no nat on gif41 from 192.168.160.0/24 to { 10.0.0.0/8, 192.168.0.0/16 }`,
		`nat on gif41 from 192.168.160.0/24 to any -> (gif41)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf NAT44 output missing %q:\n%s", want, got)
		}
	}
}

func TestPFNAT44RulesRenderPerInterfaceMasquerade(t *testing.T) {
	data, err := PFNAT44Rules([]NAT44RenderRule{
		{
			Name:                    "lan-to-dslite-a",
			Type:                    "masquerade",
			EgressInterface:         "gif41",
			SourceRanges:            []string{"192.168.160.0/24"},
			ExcludeDestinationCIDRs: []string{"192.168.0.0/16", "172.16.0.0/12", "10.0.0.0/8"},
		},
		{
			Name:                    "lan-to-dslite-b",
			Type:                    "masquerade",
			EgressInterface:         "gif42",
			SourceRanges:            []string{"192.168.160.0/24"},
			ExcludeDestinationCIDRs: []string{"192.168.0.0/16", "172.16.0.0/12", "10.0.0.0/8"},
		},
		{
			Name:                    "lan-to-hgw-direct",
			Type:                    "masquerade",
			EgressInterface:         "vtnet0",
			SourceRanges:            []string{"192.168.160.0/24"},
			ExcludeDestinationCIDRs: []string{"192.168.0.0/16", "172.16.0.0/12", "10.0.0.0/8"},
		},
	})
	if err != nil {
		t.Fatalf("render pf NAT44 rules: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`no nat on gif41 from 192.168.160.0/24 to { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16 }`,
		`nat on gif41 from 192.168.160.0/24 to any -> (gif41)`,
		`nat on gif42 from 192.168.160.0/24 to any -> (gif42)`,
		`nat on vtnet0 from 192.168.160.0/24 to any -> (vtnet0)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf NAT44 output missing %q:\n%s", want, got)
		}
	}
}

func TestPfRendersTCPMSSClamp(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: "em1"},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"},
			Metadata: api.ObjectMeta{Name: "ds-lite"},
			Spec:     api.DSLiteTunnelSpec{TunnelName: "gif40", MTU: 1454},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
			Metadata: api.ObjectMeta{Name: "wan"},
			Spec:     api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite"}},
		},
	}}}
	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`scrub out on gif40 proto tcp max-mss 1414`,
		`pass out quick all keep state`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf output missing %q:\n%s", want, got)
		}
	}
}
