// SPDX-License-Identifier: BSD-3-Clause

package firewallbackend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFirewallBackendApplyWritesChangedRulesetInDryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.nft")
	data := []byte("table inet routerd_filter {}\n")
	changed, err := Nftables{}.Apply(context.Background(), Ruleset{Backend: "nftables", Path: path, Data: data}, true)
	if err != nil {
		t.Fatalf("apply dry-run: %v", err)
	}
	if !changed {
		t.Fatal("first dry-run apply should still report changed")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written ruleset: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("ruleset data = %q, want %q", got, data)
	}
}

func TestFirewallBackendRendersEdgeCaseResourceNames(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan.edge"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan-zone"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan.edge"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "allow-dash.dot"}, Spec: api.FirewallRuleSpec{FromZone: "lan-zone", ToZone: "self", Protocol: "tcp", Port: 443, Action: "accept"}},
	}}}
	ruleset, err := Nftables{}.Render(router, "", nil)
	if err != nil {
		t.Fatalf("render edge ruleset: %v", err)
	}
	if !strings.Contains(string(ruleset.Data), "chain lan_zone_to_self") || !strings.Contains(string(ruleset.Data), "tcp dport 443") {
		t.Fatalf("edge ruleset missing expected chain/rule:\n%s", ruleset.Data)
	}
}

func TestPFBespokeSyntaxSurvivesBackendRefactor(t *testing.T) {
	ruleset, err := PF{}.Render(pfBespokeRouter(), "", nil)
	if err != nil {
		t.Fatalf("render pf firewall: %v", err)
	}
	got := string(ruleset.Data)
	for _, want := range []string{
		`nat-anchor "routerd_nat"`,
		`rdr pass on em0 inet proto tcp from any to 203.0.113.10 port 6443 -> 172.18.1.10 port 6443`,
		`nat on em1 inet proto tcp from (em1:network) to 172.18.1.10 port 6443 -> (em1)`,
		`pass in quick on $wan_if proto tcp to 172.18.1.10/32 port 6443 keep state label "routerd:api"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("pf output missing %q:\n%s", want, got)
		}
	}
}

func pfBespokeRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "em0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "em1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.FirewallRuleSpec{FromZone: "wan", ToZone: "lan", Protocol: "tcp", Port: 6443, DestinationCIDRs: []string{"172.18.1.10/32"}, Action: "accept"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 6443},
			Backends: []api.IngressBackendSpec{{Address: "172.18.1.10", Port: 6443}},
			Hairpin:  api.IngressHairpinSpec{Enabled: true, Interfaces: []string{"lan"}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "dynamic"}, Spec: api.NAT44RuleSpec{EgressPolicyRef: "EgressRoutePolicy/default", SourceRanges: []string{"172.18.0.0/16"}}},
	}}}
}
