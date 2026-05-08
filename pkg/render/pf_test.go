package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
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
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.FirewallLogSpec{Enabled: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"},
			Metadata: api.ObjectMeta{Name: "wan-ssh"},
			Spec:     api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", SourceCIDRs: []string{"192.0.2.0/24"}, DestinationCIDRs: []string{"198.51.100.10/32"}, Protocol: "tcp", Port: 22, Action: "accept", Log: true},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4SourceNAT"},
			Metadata: api.ObjectMeta{Name: "lan-nat"},
			Spec: api.IPv4SourceNATSpec{
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

func TestPFRenderInternalDHCPDNSHoles(t *testing.T) {
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
	for _, want := range []string{
		`pass in quick on $lan_if proto udp to self port 67 keep state label "routerd:lan-dhcp4-dhcpv4-server-lan"`,
		`pass in quick on $lan_if proto udp to self port 547 keep state label "routerd:lan-dhcp6-dhcpv6-server-lan"`,
		`pass in quick on $lan_if proto udp to self port 53 keep state label "routerd:lan-resolver-dns-udp-lan"`,
		`pass in quick on $lan_if proto tcp to self port 53 keep state label "routerd:lan-resolver-dns-tcp-lan"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf output missing %q:\n%s", want, got)
		}
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
	if !strings.Contains(got, "skipped: egressInterface is resolved by EgressRoutePolicy/default at runtime") {
		t.Fatalf("pf output missing runtime skip comment:\n%s", got)
	}
}
