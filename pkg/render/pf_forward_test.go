// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func pfForwardRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "lan", api.InterfaceSpec{IfName: "em0"}), netResource("Interface", "wan", api.InterfaceSpec{IfName: "em1"}), netResource("Interface", "mgmt", api.InterfaceSpec{IfName: "em2"}),
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "mgmt"}, Spec: api.FirewallZoneSpec{Role: "mgmt", Interfaces: []string{"mgmt"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "deny-web"}, Spec: api.FirewallRuleSpec{FromZone: "lan", ToZone: "wan", Protocol: "tcp", DestinationPorts: []api.FirewallPort{"443"}, Action: "drop"}},
	}}}
}
func TestPFForwardRulesBindActualEgressAndPrecedeImplicitAccept(t *testing.T) {
	data, err := PF(pfForwardRouter(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`pass in quick on "em0" all no state tag "routerd_ingress_em0"`, `anchor "routerd_forward_lan_wan_em0" out on $wan_if tagged "routerd_ingress_em0" {`, `block drop out quick on $wan_if proto tcp to any port 443 label "routerd:deny-web"`, `pass out quick on $wan_if all keep state label "routerd:lan-to-wan"`, `block drop out quick tagged "routerd_ingress_em0" label "routerd:lan:unmatched-forward"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s\n%s", want, s)
		}
	}
	if strings.Index(s, `label "routerd:deny-web"`) > strings.Index(s, `label "routerd:lan-to-wan"`) {
		t.Fatal("explicit deny follows implicit accept")
	}
	if strings.Contains(s, `pass in quick on $lan_if keep state label "routerd:lan-to-wan"`) {
		t.Fatal("ingress-only pass still bypasses actual egress")
	}
}
func TestPFLocalOutputRuleCannotAuthorizeForwardedTraffic(t *testing.T) {
	r := pfForwardRouter()
	r.Spec.Resources = append(r.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "self-web"}, Spec: api.FirewallRuleSpec{FromZone: "self", ToZone: "wan", Protocol: "tcp", Port: 443, Action: "accept"}})
	data, err := PF(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	block := strings.Index(s, `label "routerd:lan:unmatched-forward"`)
	local := strings.Index(s, `label "routerd:self-web"`)
	fallback := strings.Index(s, `pass out quick all keep state`)
	if block < 0 || local < block || fallback < local {
		t.Fatalf("local output can accept transit first:\n%s", s)
	}
	if !strings.Contains(s, `to { (self), 255.255.255.255`) {
		t.Fatal("self input does not track changing native addresses")
	}
}

func TestPFSourceSelectorsUseUntranslatedTupleAndRejectOnlyAffectedSNAT(t *testing.T) {
	r := pfForwardRouter()
	rule := r.Spec.Resources[6]
	spec, _ := rule.FirewallRuleSpec()
	spec.SourceCIDRs = []string{"192.0.2.0/24"}
	spec.SourcePorts = []api.FirewallPort{"1024-2048"}
	rule.Spec = spec
	r.Spec.Resources[6] = rule
	data, err := PF(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `from 192.0.2.0/24 port 1024:2048 to any port 443`) {
		t.Fatalf("original source selectors lost: %s", data)
	}
	nat := netResource("NAT44Rule", "nat", api.NAT44RuleSpec{OutboundInterface: "wan", SourceCIDRs: []string{"192.0.2.0/24"}, Translation: api.IPv4NATTranslationSpec{Type: "interfaceAddress"}})
	r.Spec.Resources = append(r.Spec.Resources, nat)
	if _, err := PF(r, nil); err == nil || !strings.Contains(err.Error(), "original srcCIDRs/sourcePorts") {
		t.Fatalf("translated source predicate accepted: %v", err)
	}
	nat.Spec = api.NAT44RuleSpec{OutboundInterface: "mgmt", SourceCIDRs: []string{"192.0.2.0/24"}, Translation: api.IPv4NATTranslationSpec{Type: "interfaceAddress"}}
	r.Spec.Resources[7] = nat
	if _, err := PF(r, nil); err != nil {
		t.Fatalf("unrelated egress SNAT rejected: %v", err)
	}
	spec.SourceCIDRs = []string{"2001:db8::/64"}
	spec.SourcePorts = nil
	spec.Protocol = "icmpv6"
	spec.DestinationPorts = nil
	spec.ICMPv6Type = "echo-request"
	rule.Spec = spec
	r.Spec.Resources[6] = rule
	nat.Spec = api.NAT44RuleSpec{OutboundInterface: "wan", SourceCIDRs: []string{"192.0.2.0/24"}, Translation: api.IPv4NATTranslationSpec{Type: "interfaceAddress"}}
	r.Spec.Resources[7] = nat
	if _, err := PF(r, nil); err != nil {
		t.Fatalf("IPv6-only rule cannot be translated by NAT44: %v", err)
	}
}

func TestPFForwardRejectKeepsActualDestinationAndProtocol(t *testing.T) {
	r := pfForwardRouter()
	rule := r.Spec.Resources[6]
	spec, _ := rule.FirewallRuleSpec()
	spec.Action = "reject"
	spec.Protocol = "icmpv6"
	spec.ICMPv6Type = "echo-request"
	spec.DestinationPorts = nil
	spec.DestinationCIDRs = []string{"2001:db8:20::/64"}
	rule.Spec = spec
	r.Spec.Resources[6] = rule
	data, err := PF(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	want := `block return out quick on $wan_if inet6 proto icmp6 to 2001:db8:20::/64 icmp6-type echoreq label "routerd:deny-web"`
	if !strings.Contains(s, want) {
		t.Fatalf("IPv6 reject lost selectors: %s", s)
	}
}

func TestPFForwardHoleRetainsOwningIngressInterface(t *testing.T) {
	r := pfForwardRouter()
	spec, _ := r.Spec.Resources[3].FirewallZoneSpec()
	spec.Interfaces = append(spec.Interfaces, "mgmt")
	r.Spec.Resources[3].Spec = spec
	data, err := PF(r, []FirewallHole{{Name: "one-ingress", FromZone: "lan", ToZone: "wan", Protocol: "udp", Port: 123, IfNames: []string{"em0"}}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	name := `label "routerd:one-ingress"`
	if strings.Count(s, name) != 1 {
		t.Fatalf("hole broadened to another ingress interface: %s", s)
	}
	anchor := strings.Index(s, `anchor "routerd_forward_lan_wan_em0"`)
	next := strings.Index(s[anchor+1:], `anchor `) + anchor + 1
	hole := strings.Index(s, name)
	if anchor < 0 || hole < anchor || next <= hole {
		t.Fatalf("hole is outside actual owning ingress anchor: %s", s)
	}
}

func TestPFSameRoleForwardingUsesRealEgressInterfaces(t *testing.T) {
	r := pfForwardRouter()
	spec, _ := r.Spec.Resources[5].FirewallZoneSpec()
	spec.Role = "untrust"
	r.Spec.Resources[5].Spec = spec
	data, err := PF(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`anchor "routerd_forward_wan_mgmt_em1" out on $mgmt_if tagged "routerd_ingress_em1" {`, `pass out quick on $mgmt_if all keep state label "routerd:wan-to-mgmt"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("same-role policy lost actual egress: %s", s)
		}
	}
}
