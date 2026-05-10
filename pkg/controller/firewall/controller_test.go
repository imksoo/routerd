// SPDX-License-Identifier: BSD-3-Clause

package firewall

import (
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/render"
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
