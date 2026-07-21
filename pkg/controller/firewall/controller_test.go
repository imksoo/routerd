// SPDX-License-Identifier: BSD-3-Clause

package firewall

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/render"
)

func TestDeriveHolesIncludesDSLiteAndDHCPv6(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-routerd"}},
	}}}
	holes := render.InternalFirewallHoles(router)
	seen := map[string]bool{}
	for _, hole := range holes {
		seen[hole.Name+"|"+hole.FromZone+"|"+hole.ToZone+"|"+hole.Protocol] = true
	}
	if !seen["wan-pd-dhcpv6-client|wan|self|udp"] {
		t.Fatalf("missing DHCPv6 client hole: %#v", holes)
	}
	if !seen["ds-lite-dslite-ipip|self|wan|ipip"] {
		t.Fatalf("missing DS-Lite IPIP hole: %#v", holes)
	}
}

func TestWireGuardListenPortOpensUntrustToSelfHole(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg0"}, Spec: api.WireGuardInterfaceSpec{ListenPort: 51820}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TailscaleNode"}, Metadata: api.ObjectMeta{Name: "tailnet"}, Spec: api.TailscaleNodeSpec{}},
	}}}

	holes := render.InternalFirewallHoles(router)
	var foundWG, foundTailscale bool
	for _, hole := range holes {
		if hole.Name == "wg0-wireguard" && hole.FromZone == "wan" && hole.ToZone == "self" && hole.Protocol == "udp" && hole.Port == 51820 {
			foundWG = true
		}
		if hole.Name == "tailnet-tailscale" && hole.FromZone == "wan" && hole.ToZone == "self" && hole.Protocol == "udp" && hole.Port == 41641 {
			foundTailscale = true
		}
	}
	if !foundWG {
		t.Fatalf("missing WireGuard listen hole: %#v", holes)
	}
	if !foundTailscale {
		t.Fatalf("missing Tailscale listen hole: %#v", holes)
	}

	data, err := render.NftablesFirewall(router, holes)
	if err != nil {
		t.Fatal(err)
	}
	ruleset := string(data)
	for _, want := range []string{
		"chain wan_to_self",
		"udp dport 51820 counter accept comment \"net.routerd.net/v1alpha1/WireGuardInterface/wg0\"",
		"udp dport 41641 counter accept comment \"net.routerd.net/v1alpha1/TailscaleNode/tailnet\"",
	} {
		if !strings.Contains(ruleset, want) {
			t.Fatalf("rendered nftables ruleset missing %q:\n%s", want, ruleset)
		}
	}
}

func TestFirewallControllerPropagatesBackendApplyErrorToStatusAndBus(t *testing.T) {
	if runtime.GOOS == "freebsd" {
		t.Skip("Linux nft backend fixture; native PF behavior is covered separately")
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallRule"}, Metadata: api.ObjectMeta{Name: "allow-web"}, Spec: api.FirewallRuleSpec{FromZone: "lan", ToZone: "self", Protocol: "tcp", Port: 443, Action: "accept"}},
	}}}
	store := firewallMapStore{}
	eventBus := bus.New()
	ch, unsubscribe := eventBus.Subscribe(context.Background(), bus.Subscription{Topics: []string{"routerd.firewall.**"}}, 4)
	defer unsubscribe()
	controller := Controller{
		Router:       router,
		Store:        store,
		Bus:          eventBus,
		NftablesPath: t.TempDir() + "/firewall.nft",
		NftCommand:   "false",
	}
	if err := controller.Reconcile(context.Background()); err == nil {
		t.Fatal("Reconcile succeeded, want backend apply error")
	}
	status := store.ObjectStatus(api.FirewallAPIVersion, "FirewallRule", "allow-web")
	if status["phase"] != "Error" || status["reason"] != "ApplyFailed" || !strings.Contains(statusString(status["error"]), "false -c -f") {
		t.Fatalf("status = %#v", status)
	}
	select {
	case event := <-ch:
		if event.Type != "routerd.firewall.rules.error" || event.Attributes["reason"] != "ApplyFailed" {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for firewall error event")
	}
}

type firewallMapStore map[string]map[string]any

func (s firewallMapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s firewallMapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s[apiVersion+"/"+kind+"/"+name]
}

func statusString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
