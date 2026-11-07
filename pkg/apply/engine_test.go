// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestPlanEffectiveRejectsMissingDeclaredPD(t *testing.T) {
	declared := declaredPDDelegatedDNSRouter()
	declared.Spec.Resources = append([]api.Resource(nil), declared.Spec.Resources[0], declared.Spec.Resources[1], declared.Spec.Resources[3])

	if _, err := (&Engine{}).PlanEffective(declared, declared); err == nil {
		t.Fatal("plan effective succeeded without declared DHCPv6PrefixDelegation")
	}
}

func declaredPDDelegatedDNSRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "backup-effective"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0", Managed: false, Owner: "external"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", Managed: false, Owner: "external"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::1", DependsOn: []api.ResourceDependencySpec{{Resource: "DHCPv6PrefixDelegation/wan-pd", Phase: "Bound"}}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan-resolver"}, Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{AddressFrom: []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}}}}, Sources: []api.DNSResolverSourceSpec{{Name: "upstream", Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://[2001:db8::53]:53"}}}}},
		}},
	}
}

func TestNormalizeSysctlValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "single", in: "1\n", want: "1"},
		{name: "tabSeparated", in: "1024\t65535\n", want: "1024 65535"},
		{name: "multiSpace", in: "4096  87380\t16777216\n", want: "4096 87380 16777216"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeSysctlValue(tc.in); got != tc.want {
				t.Fatalf("normalizeSysctlValue(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func staticDefaultRouteRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
				Metadata: api.ObjectMeta{Name: "wan"},
				Spec:     api.InterfaceSpec{IfName: "ens18", Managed: true, Owner: "routerd"},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
				Metadata: api.ObjectMeta{Name: "default-v4"},
				Spec: api.EgressRoutePolicySpec{
					Mode: "priority",
					Candidates: []api.EgressRoutePolicyCandidate{
						{Interface: "wan", GatewaySource: "static", Gateway: "192.168.1.254", Priority: 10, Table: 100, Mark: 256},
					},
				},
			},
		}},
	}
}

func TestParseIPv4PrefixesFromIfconfig(t *testing.T) {
	got := parseIPv4Prefixes("vtnet1: flags=...\n\tinet 192.168.10.1 netmask 0xffffff00 broadcast 192.168.10.255\n")
	if len(got) != 1 || got[0].String() != "192.168.10.0/24" {
		t.Fatalf("prefixes = %v, want 192.168.10.0/24", got)
	}
}

func findResult(result *Result, id string) *ResourceResult {
	for i := range result.Resources {
		if result.Resources[i].ID == id {
			return &result.Resources[i]
		}
	}
	return nil
}
