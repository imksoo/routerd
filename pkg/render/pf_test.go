// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"bytes"
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
		`pass in quick on $lan_if inet6 proto icmp6 icmp6-type { routersol, routeradv, neighbrsol, neighbradv } to { (em1), ff02::1, ff02::2, ff02::1:ff00:0/104 } keep state label "routerd:lan:ipv6-control"`,
		`pass in quick on $lan_if to (em1) keep state`,
		`pass in quick on $mgmt_if to (em2) keep state`,
		`block drop in quick on $lan_if to (em2:network) label "routerd:lan-to-mgmt-deny"`,
		`pass in quick on $lan_if keep state label "routerd:lan-to-wan"`,
		`pass in quick on $wan_if proto udp to (em0) port 546 keep state label "routerd:dhcpv6-client"`,
		`pass in log quick on $wan_if proto tcp from 192.0.2.0/24 to 198.51.100.10/32 port 22 keep state label "routerd:wan-ssh"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf output missing %q:\n%s", want, got)
		}
	}
}

func TestPFEgressRouteHashUsesRoundRobinStickyAddress(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "em0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-a"}, Spec: api.InterfaceSpec{IfName: "em1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "em2"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "lan-balance"}, Spec: api.EgressRoutePolicySpec{
			Mode:                    "hash",
			HashFields:              []string{"sourceAddress"},
			SourceCIDRs:             []string{"192.0.2.0/24"},
			ExcludeDestinationCIDRs: []string{"198.51.100.0/24"},
			Candidates: []api.EgressRoutePolicyCandidate{{Name: "pool", Targets: []api.EgressRoutePolicyTarget{
				{Name: "a", Interface: "wan-a", Gateway: "203.0.113.1", GatewaySource: "static"},
				{Name: "b", Interface: "wan-b", Gateway: "198.51.100.1", GatewaySource: "static"},
			}}},
		}},
	}}}

	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render PF: %v", err)
	}
	got := string(data)
	want := "pass in quick route-to { (em1 203.0.113.1), (em2 198.51.100.1) } round-robin sticky-address inet from 192.0.2.0/24 to ! 198.51.100.0/24 keep state label \"routerd:egress-route:lan-balance\""
	if !strings.Contains(got, want) {
		t.Fatalf("PF route-to rule missing %q:\\n%s", want, got)
	}
	if !strings.Contains(got, "pass all keep state") {
		t.Fatalf("expected pass-all fallback after route-to:\\n%s", got)
	}
}

func TestPFEgressRouteHashUsesIPv6StaticRoutehosts(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-a"}, Spec: api.InterfaceSpec{IfName: "em1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "em2"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "lan6-balance"}, Spec: api.EgressRoutePolicySpec{
			Family:           "ipv6",
			Mode:             "hash",
			HashFields:       []string{"sourceAddress"},
			SourceCIDRs:      []string{"2001:db8:10::/64"},
			DestinationCIDRs: []string{"2001:db8:ffff::/48"},
			Candidates: []api.EgressRoutePolicyCandidate{{Name: "pool", Targets: []api.EgressRoutePolicyTarget{
				{Name: "a", Interface: "wan-a", Gateway: "2001:db8:100::1", GatewaySource: "static"},
				{Name: "b", Interface: "wan-b", Gateway: "2001:db8:200::1", GatewaySource: "static"},
			}}},
		}},
	}}}

	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render PF: %v", err)
	}
	want := "pass in quick route-to { (em1 2001:db8:100::1), (em2 2001:db8:200::1) } round-robin sticky-address inet6 from 2001:db8:10::/64 to 2001:db8:ffff::/48 keep state label \"routerd:egress-route:lan6-balance\""
	if !strings.Contains(string(data), want) {
		t.Fatalf("PF IPv6 route-to rule missing %q:\n%s", want, data)
	}
}

func TestPFEgressRouteHashRejectsUnsafeOrUnsupportedShapes(t *testing.T) {
	base := func() *api.Router {
		return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-a"}, Spec: api.InterfaceSpec{IfName: "em1"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "em2"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "balanced"}, Spec: api.EgressRoutePolicySpec{
				Mode: "hash", HashFields: []string{"sourceAddress"}, SourceCIDRs: []string{"192.0.2.0/24"},
				Candidates: []api.EgressRoutePolicyCandidate{{Targets: []api.EgressRoutePolicyTarget{
					{Interface: "wan-a", Gateway: "203.0.113.1", GatewaySource: "static"},
					{Interface: "wan-b", Gateway: "198.51.100.1", GatewaySource: "static"},
				}}},
			}},
		}}}
	}
	tests := []struct {
		name string
		edit func(*api.Router)
		want string
	}{
		{name: "destination-hash", edit: func(r *api.Router) {
			s := r.Spec.Resources[2].Spec.(api.EgressRoutePolicySpec)
			s.HashFields = []string{"destinationAddress"}
			r.Spec.Resources[2].Spec = s
		}, want: "hashFields"},
		{name: "missing-gateway", edit: func(r *api.Router) {
			s := r.Spec.Resources[2].Spec.(api.EgressRoutePolicySpec)
			s.Candidates[0].Targets[0].Gateway = ""
			r.Spec.Resources[2].Spec = s
		}, want: "static gateway"},
		{name: "dynamic-gateway", edit: func(r *api.Router) {
			s := r.Spec.Resources[2].Spec.(api.EgressRoutePolicySpec)
			s.Candidates[0].Targets[0].GatewaySource = "dhcpv4"
			r.Spec.Resources[2].Spec = s
		}, want: "static gateway"},
		{name: "ipv6-link-local-gateway", edit: func(r *api.Router) {
			s := r.Spec.Resources[2].Spec.(api.EgressRoutePolicySpec)
			s.Family = "ipv6"
			s.SourceCIDRs = []string{"2001:db8:10::/64"}
			s.Candidates[0].Targets[0].Gateway = "fe80::1"
			s.Candidates[0].Targets[1].Gateway = "2001:db8:2::1"
			r.Spec.Resources[2].Spec = s
		}, want: "non-link-local ipv6"},
		{name: "ambiguous-target-interface", edit: func(r *api.Router) {
			s := r.Spec.Resources[2].Spec.(api.EgressRoutePolicySpec)
			s.Candidates[0].Targets[0].OutboundInterface = "wan-b"
			r.Spec.Resources[2].Spec = s
		}, want: "both interface and outboundInterface"},
		{name: "explicit-firewall-rule", edit: func(r *api.Router) {
			r.Spec.Resources = append(r.Spec.Resources,
				api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"wan-a"}}},
				api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "allow-lan"}, Spec: api.FirewallRuleSpec{FromZone: "lan", ToZone: "self", Action: "accept"}},
			)
		}, want: "explicit routerd firewall rules"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := base()
			tc.edit(router)
			_, err := PF(router, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PF error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestPFEgressRouteHashCoexistsOnlyWithBroadZoneForwarding(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "em0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "em1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-a"}, Spec: api.InterfaceSpec{IfName: "em2"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "em3"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "mgmt"}, Spec: api.InterfaceSpec{IfName: "em4"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "mgmt"}, Spec: api.FirewallZoneSpec{Role: "mgmt", Interfaces: []string{"mgmt"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "balanced"}, Spec: api.EgressRoutePolicySpec{
			Mode: "hash", HashFields: []string{"sourceAddress"}, SourceCIDRs: []string{"192.0.2.0/24"},
			Candidates: []api.EgressRoutePolicyCandidate{{Targets: []api.EgressRoutePolicyTarget{
				{Interface: "wan-a", GatewaySource: "static", Gateway: "203.0.113.1"},
				{Interface: "wan-b", GatewaySource: "static", Gateway: "198.51.100.1"},
			}}},
		}},
	}}}
	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render PF: %v", err)
	}
	got := string(data)
	block := "block drop in quick on $lan_if to (em4:network)"
	local := "pass in quick on $lan_if to { (em0), (em1), (em2), (em3), (em4) } keep state label \"routerd:lan:route-to-self\""
	connected := "pass in quick on $lan_if to { (em0:network), (em1:network) } keep state label \"routerd:lan:route-to-connected\""
	route := "pass in quick on $lan_if route-to { (em2 203.0.113.1), (em3 198.51.100.1) } round-robin sticky-address inet from 192.0.2.0/24 to any keep state label \"routerd:egress-route:balanced\""
	broad := "pass in quick on $lan_if keep state label \"routerd:lan-to-wan\""
	for _, want := range []string{block, local, connected, route, broad} {
		if !strings.Contains(got, want) {
			t.Fatalf("PF coexistence output missing %q:\n%s", want, got)
		}
	}
	if !(strings.Index(got, block) < strings.Index(got, local) && strings.Index(got, local) < strings.Index(got, connected) && strings.Index(got, connected) < strings.Index(got, route) && strings.Index(got, route) < strings.Index(got, broad)) {
		t.Fatalf("PF coexistence ordering does not preserve blocks/internal routes/broad pass:\n%s", got)
	}
}

func TestPFEgressRouteHashRejectsExplicitFilterCoexistence(t *testing.T) {
	base := func() *api.Router {
		return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "em0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-a"}, Spec: api.InterfaceSpec{IfName: "em1"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan-b"}, Spec: api.InterfaceSpec{IfName: "em2"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"}, Metadata: api.ObjectMeta{Name: "balanced"}, Spec: api.EgressRoutePolicySpec{
				Mode: "hash", HashFields: []string{"sourceAddress"}, SourceCIDRs: []string{"192.0.2.0/24"},
				Candidates: []api.EgressRoutePolicyCandidate{{Targets: []api.EgressRoutePolicyTarget{
					{Interface: "wan-a", GatewaySource: "static", Gateway: "203.0.113.1"},
					{Interface: "wan-b", GatewaySource: "static", Gateway: "198.51.100.1"},
				}}},
			}},
		}}}
	}
	tests := []struct {
		name  string
		add   api.Resource
		holes []FirewallHole
	}{
		{name: "rule", add: api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "allow"}, Spec: api.FirewallRuleSpec{FromZone: "lan", ToZone: "self", Action: "accept"}}},
		{name: "client-policy", add: api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"}, Metadata: api.ObjectMeta{Name: "guests"}, Spec: api.ClientPolicySpec{Mode: "include"}}},
		{name: "log", add: api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallEventLog"}, Metadata: api.ObjectMeta{Name: "events"}, Spec: api.FirewallEventLogSpec{Enabled: true}}},
		{name: "hole", holes: []FirewallHole{{Name: "allow", FromZone: "lan", ToZone: "self", Action: "accept"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := base()
			if tc.add.Kind != "" {
				router.Spec.Resources = append(router.Spec.Resources, tc.add)
			}
			if _, err := PF(router, tc.holes); err == nil || !strings.Contains(err.Error(), "explicit routerd firewall") {
				t.Fatalf("PF coexistence error = %v, want explicit filter rejection", err)
			}
		})
	}
}

func TestPFDisallowedForwardDestinationsApplyToEverySourceRole(t *testing.T) {
	from := firewallZone{Name: "wan", Role: "untrust", IfNames: []string{"em0"}}
	zones := map[string]firewallZone{
		"wan":  from,
		"mgmt": {Name: "mgmt", Role: "mgmt", IfNames: []string{"em1"}},
	}
	var buf bytes.Buffer
	if err := writePFDisallowedForwardDestinations(&buf, from, zones, firewallPolicy{}); err != nil {
		t.Fatalf("write disallowed forwards: %v", err)
	}
	want := `block drop in quick on $wan_if to (em1:network) label "routerd:wan-to-mgmt-deny"`
	if !strings.Contains(buf.String(), want) {
		t.Fatalf("untrust connected deny missing %q:\n%s", want, buf.String())
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
	if !strings.Contains(got, `pass in quick on $lan_if to (em1) keep state`) {
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
		`pass in quick on vtnet1 proto udp from 192.168.160.184 to (vtnet1) port 53 keep state label "routerd:client-policy:guest-devices:dns"`,
		`pass in quick on vtnet1 proto udp from 192.168.160.184 to (vtnet1) port 67 keep state label "routerd:client-policy:guest-devices:dhcp"`,
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

func TestPFClientPolicyRendersExplicitIPv6GuestDenyBeforeICMPv6Pass(t *testing.T) {
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
				Mode:             "include",
				Interfaces:       []string{"lan"},
				GuestEgressDeny:  []string{"fd00:2::/64"},
				GuestEgressAllow: []string{"2001:db8:3::/64"},
				Classification: []api.ClientPolicyClassSpec{{
					Mode:          "guest",
					Match:         api.ClientPolicyClassMatchSpec{MACs: []string{"02:00:00:00:00:44"}},
					IPv6Addresses: []string{"fd00:1::10"},
				}},
			},
		},
	}}}
	data, err := PF(router, nil)
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	got := string(data)
	deny := `block drop in log quick on vtnet1 inet6 from fd00:1::10 to fd00:2::/64 label "routerd:client-policy:guest-devices:deny"`
	allow := `pass in quick on vtnet1 inet6 from fd00:1::10 to 2001:db8:3::/64 keep state label "routerd:client-policy:guest-devices:allow"`
	if !strings.Contains(got, deny) || !strings.Contains(got, allow) {
		t.Fatalf("pf output missing explicit IPv6 ClientPolicy rules:\n%s", got)
	}
	control := `pass in quick on $lan_if inet6 proto icmp6 icmp6-type { routersol, routeradv, neighbrsol, neighbradv }`
	if strings.Index(got, deny) > strings.Index(got, control) {
		t.Fatalf("IPv6 guest deny must precede local IPv6 control pass:\n%s", got)
	}
	if strings.Contains(got, "ether ") || strings.Contains(got, "fd00:1::10 to 192.168.0.0/16") {
		t.Fatalf("pf output must use only explicit IPv6 identity and family-safe destinations:\n%s", got)
	}
}

func TestPFClientPolicyDoesNotInferIPv6Identity(t *testing.T) {
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
			TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"},
			Metadata: api.ObjectMeta{Name: "guest-devices"},
			Spec: api.ClientPolicySpec{
				Mode:            "include",
				Interfaces:      []string{"lan"},
				GuestEgressDeny: []string{"fd00:2::/64"},
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
	if strings.Contains(string(data), "inet6 from") {
		t.Fatalf("IPv4 reservation must not infer an IPv6 ClientPolicy identity:\n%s", data)
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
