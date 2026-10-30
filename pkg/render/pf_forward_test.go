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
