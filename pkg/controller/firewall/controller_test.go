package firewall

import (
	"testing"

	"routerd/pkg/api"
)

func TestDeriveHolesIncludesDSLiteAndDHCPv6(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite"}, Spec: api.DSLiteTunnelSpec{Interface: "wan", TunnelName: "ds-routerd"}},
	}}}
	holes := deriveHoles(router)
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
