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
			Spec:     api.FirewallRuleSpec{FromZone: "wan", ToZone: "self", SourceCIDRs: []string{"192.0.2.0/24"}, Protocol: "tcp", Port: 22, Action: "accept", Log: true},
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
		`lan_if = em1`,
		`wan_if = em0`,
		`nat on em0 from 172.18.0.0/16 to any -> (em0)`,
		`block drop all`,
		`pass quick inet6 proto icmp6 all keep state`,
		`pass in quick on $lan_if to self keep state`,
		`pass in quick on $lan_if keep state label "routerd:lan-to-wan"`,
		`pass in quick on $wan_if proto udp to self port 546 keep state label "routerd:dhcpv6-client"`,
		`pass in log quick on $wan_if proto tcp from 192.0.2.0/24 to self port 22 keep state label "routerd:wan-ssh"`,
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
