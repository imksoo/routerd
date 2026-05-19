// SPDX-License-Identifier: BSD-3-Clause

package firewallbackend

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/render"
)

func TestNftablesBackendApplyChecksThenLoadsRuleset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.nft")
	var calls []string
	backend := Nftables{Command: "nft", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte("ok"), nil
	}}
	changed, err := backend.Apply(context.Background(), Ruleset{Path: path, Data: []byte("table inet routerd_filter {}\n")}, false)
	if err != nil {
		t.Fatalf("apply nftables: %v", err)
	}
	if !changed {
		t.Fatal("first apply should report changed")
	}
	want := []string{"nft -c -f " + path, "nft -f " + path}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestPFBackendApplyChecksThenLoadsRuleset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.pf")
	var calls []string
	backend := PF{Command: "pfctl", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte("ok"), nil
	}}
	changed, err := backend.Apply(context.Background(), Ruleset{Path: path, Data: []byte("pass all keep state\n")}, false)
	if err != nil {
		t.Fatalf("apply pf: %v", err)
	}
	if !changed {
		t.Fatal("first apply should report changed")
	}
	want := []string{"pfctl -n -f " + path, "pfctl -f " + path}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestFirewallBackendDiffSkipsUnchangedReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.nft")
	data := []byte("table inet routerd_filter {}\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	backend := Nftables{Command: "nft", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte("ok"), nil
	}}
	changed, err := backend.Apply(context.Background(), Ruleset{Path: path, Data: data}, true)
	if err != nil {
		t.Fatalf("apply nftables dry-run: %v", err)
	}
	if changed {
		t.Fatal("unchanged ruleset should not report changed")
	}
	if len(calls) != 0 {
		t.Fatalf("dry-run unchanged apply should not reload: %#v", calls)
	}
}

func TestNftablesBespokeExpressionsSurviveBackendRefactor(t *testing.T) {
	filter, err := Nftables{}.Render(nftBespokeRouter(), "")
	if err != nil {
		t.Fatalf("render nftables firewall: %v", err)
	}
	filterText := string(filter.Data)
	for _, want := range []string{
		"ct state invalid counter drop",
		"ct state { established, related } counter accept",
	} {
		if !strings.Contains(filterText, want) {
			t.Fatalf("nftables firewall output missing %q:\n%s", want, filterText)
		}
	}

	nat, err := render.NftablesIPv4SourceNAT(nftBespokeRouter())
	if err != nil {
		t.Fatalf("render nftables NAT: %v", err)
	}
	natText := string(nat)
	for _, want := range []string{
		`jhash ip saddr mod 2 vmap`,
		`numgen random mod 2 vmap`,
		`ct original ip daddr 203.0.113.10 ct original proto-dst 6443 counter masquerade`,
	} {
		if !strings.Contains(natText, want) {
			t.Fatalf("nftables NAT output missing %q:\n%s", want, natText)
		}
	}
}

func TestPFBespokeSyntaxSurvivesBackendRefactor(t *testing.T) {
	ruleset, err := PF{}.Render(pfBespokeRouter(), "")
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

func nftBespokeRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-ip"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "172.18.1.1/24"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "api"}, Spec: api.FirewallRuleSpec{FromZone: "wan", ToZone: "lan", Protocol: "tcp", Port: 6443, DestinationCIDRs: []string{"10.0.0.11/32"}, Action: "accept"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api-hash"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.10", Protocol: "tcp", Port: 6443},
			Backends: []api.IngressBackendSpec{{Name: "a", Address: "10.0.0.11", Port: 6443}, {Name: "b", Address: "10.0.0.12", Port: 6443}},
			Policy:   api.IngressServicePolicySpec{Selection: "sourceHash"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api-random"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "wan", Address: "203.0.113.20", Protocol: "udp", Port: 5353},
			Backends: []api.IngressBackendSpec{{Name: "a", Address: "10.0.0.21", Port: 5353}, {Name: "b", Address: "10.0.0.22", Port: 5353}},
			Policy:   api.IngressServicePolicySpec{Selection: "random"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "IngressService"}, Metadata: api.ObjectMeta{Name: "api-hairpin"}, Spec: api.IngressServiceSpec{
			Listen:   api.IngressListenSpec{Interface: "lan", Address: "203.0.113.10", Protocol: "tcp", Port: 6443},
			Backends: []api.IngressBackendSpec{{Name: "a", Address: "172.18.1.10", Port: 6443}},
			Hairpin:  api.IngressHairpinSpec{Enabled: true, Interfaces: []string{"lan"}},
		}},
	}}}
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "dynamic"}, Spec: api.NAT44RuleSpec{EgressPolicyRef: "IPv4DefaultRoutePolicy/default", SourceRanges: []string{"172.18.0.0/16"}}},
	}}}
}
