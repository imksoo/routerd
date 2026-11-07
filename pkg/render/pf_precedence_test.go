// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestPFInputDenyPrecedesImplicitTrustedAccept(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		netResource("Interface", "lan", api.InterfaceSpec{IfName: "em0"}),
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "deny-echo"}, Spec: api.FirewallRuleSpec{FromZone: "lan", ToZone: "self", Protocol: "icmp", ICMPType: "echo-request", Action: "drop"}},
	}}}
	data, err := PF(router, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	deny := `block drop in quick on $lan_if inet proto icmp to any icmp-type echoreq label "routerd:deny-echo"`
	accept := `pass in quick on $lan_if to any keep state`
	anchor := `anchor "routerd_input_lan" in on $lan_if to { (self), 255.255.255.255, 224.0.0.0/24, ff02::/16 } {`
	if !strings.Contains(got, anchor) {
		t.Fatalf("input selectors can match forwarded traffic:\n%s", data)
	}
	denyIndex, acceptIndex := strings.Index(got, deny), strings.Index(got, accept)
	if denyIndex < 0 || acceptIndex < denyIndex {
		t.Fatalf("explicit deny is unreachable or uses invalid state/ICMP syntax:\n%s", data)
	}
}

func TestPFRejectsMissingForwardZone(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "only-wan"}, Spec: api.FirewallRuleSpec{FromZone: "lan", ToZone: "wan", Action: "accept"}}}}}
	if _, err := PF(router, nil); err == nil || !strings.Contains(err.Error(), "missing PF source zone") {
		t.Fatalf("forward selector was silently discarded: %v", err)
	}
}
