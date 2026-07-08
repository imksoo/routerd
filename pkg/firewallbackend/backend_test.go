// SPDX-License-Identifier: BSD-3-Clause

package firewallbackend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
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

func TestNftablesBackendSkipsUnchangedLiveReloadWhenTableExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.nft")
	data := []byte("table inet routerd_filter {}\n")
	var calls []string
	backend := Nftables{Command: "nft", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return []byte("ok"), nil
	}}
	if changed, err := backend.Apply(context.Background(), Ruleset{Path: path, Data: data}, false); err != nil {
		t.Fatalf("first apply nftables: %v", err)
	} else if !changed {
		t.Fatal("first apply should report changed")
	}
	calls = nil
	changed, err := backend.Apply(context.Background(), Ruleset{Path: path, Data: data}, false)
	if err != nil {
		t.Fatalf("second apply nftables: %v", err)
	}
	if changed {
		t.Fatal("unchanged live apply should not report changed")
	}
	if len(calls) != 0 {
		t.Fatalf("recently verified unchanged live apply should not call nft: %#v", calls)
	}
}

func TestNftablesBackendReloadsUnchangedRulesetWhenTableMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.nft")
	data := []byte("table inet routerd_filter {}\n")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	backend := Nftables{Command: "nft", Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if len(args) >= 4 && args[0] == "list" && args[1] == "table" {
			return nil, errors.New("missing")
		}
		return []byte("ok"), nil
	}}
	changed, err := backend.Apply(context.Background(), Ruleset{Path: path, Data: data}, false)
	if err != nil {
		t.Fatalf("apply nftables: %v", err)
	}
	if changed {
		t.Fatal("unchanged file should not report changed even when table is restored")
	}
	want := []string{"nft list table inet routerd_filter", "nft -c -f " + path, "nft -f " + path}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestForPlatformSelectsNativeBackend(t *testing.T) {
	if got := ForPlatform(platform.OSLinux, "").Name(); got != "nftables" {
		t.Fatalf("linux backend = %q", got)
	}
	if got := ForPlatform(platform.OSFreeBSD, "").Name(); got != "pf" {
		t.Fatalf("freebsd backend = %q", got)
	}
}

func TestFirewallBackendRenderMetadata(t *testing.T) {
	router := nftBespokeRouter()
	nftRuleset, err := Nftables{}.Render(router, "/tmp/routerd/custom.nft")
	if err != nil {
		t.Fatalf("render nftables: %v", err)
	}
	if nftRuleset.Backend != "nftables" || nftRuleset.Path != "/tmp/routerd/custom.nft" || nftRuleset.InternalHoles == 0 {
		t.Fatalf("nft ruleset metadata = %#v", nftRuleset)
	}
	pfRuleset, err := PF{}.Render(pfBespokeRouter(), "/tmp/routerd/custom.nft")
	if err != nil {
		t.Fatalf("render pf: %v", err)
	}
	if pfRuleset.Backend != "pf" || pfRuleset.Path != "/tmp/routerd/custom.pf" {
		t.Fatalf("pf ruleset metadata = %#v", pfRuleset)
	}
}

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

func TestFirewallBackendRejectsInvalidRulesetPath(t *testing.T) {
	for _, backend := range []Backend{Nftables{}, PF{}} {
		t.Run(backend.Name(), func(t *testing.T) {
			if _, err := backend.Apply(context.Background(), Ruleset{Backend: backend.Name(), Data: []byte("pass\n")}, true); err == nil {
				t.Fatal("Apply with empty path succeeded, want error")
			}
			if err := backend.Reload(context.Background(), Ruleset{Backend: backend.Name(), Path: "bad\x00path", Data: []byte("pass\n")}); err == nil {
				t.Fatal("Reload with NUL path succeeded, want error")
			}
		})
	}
}

func TestFirewallBackendPropagatesCommandFailure(t *testing.T) {
	wantErr := errors.New("syntax failed")
	backend := Nftables{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("bad syntax"), wantErr
	}}
	err := backend.Reload(context.Background(), Ruleset{Backend: "nftables", Path: filepath.Join(t.TempDir(), "firewall.nft")})
	if err == nil || !strings.Contains(err.Error(), "bad syntax") || !strings.Contains(err.Error(), "nft -c -f") {
		t.Fatalf("Reload error = %v, want wrapped syntax error", err)
	}
}

func TestFirewallBackendConcurrentReloadIsRaceClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firewall.nft")
	backend := Nftables{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte("ok"), nil
	}}
	ruleset := Ruleset{Backend: "nftables", Path: path, Data: []byte("table inet routerd_filter {}\n")}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := backend.Reload(context.Background(), ruleset); err != nil {
				t.Errorf("Reload: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestFirewallBackendRendersEdgeCaseResourceNames(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan.edge"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan-zone"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan.edge"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "allow-dash.dot"}, Spec: api.FirewallRuleSpec{FromZone: "lan-zone", ToZone: "self", Protocol: "tcp", Port: 443, Action: "accept"}},
	}}}
	ruleset, err := Nftables{}.Render(router, "")
	if err != nil {
		t.Fatalf("render edge ruleset: %v", err)
	}
	if !strings.Contains(string(ruleset.Data), "chain lan_zone_to_self") || !strings.Contains(string(ruleset.Data), "tcp dport 443") {
		t.Fatalf("edge ruleset missing expected chain/rule:\n%s", ruleset.Data)
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

	nat, err := render.NftablesNAT44Rule(nftBespokeRouter())
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44Rule"}, Metadata: api.ObjectMeta{Name: "dynamic"}, Spec: api.NAT44RuleSpec{EgressPolicyRef: "EgressRoutePolicy/default", SourceRanges: []string{"172.18.0.0/16"}}},
	}}}
}
