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
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "wan0", Managed: false}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", Managed: false}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv6DelegatedAddressSpec{PrefixDelegation: "wan-pd", Interface: "lan", AddressSuffix: "::1", DependsOn: []api.ResourceDependencySpec{{Resource: "DHCPv6PrefixDelegation/wan-pd", Phase: "Bound"}}}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan-resolver"}, Spec: api.DNSResolverSpec{Listen: []api.DNSResolverListenSpec{{AddressFrom: []api.StatusValueSourceSpec{{Resource: "IPv6DelegatedAddress/lan-base", Field: "address"}}}}, Sources: []api.DNSResolverSourceSpec{{Name: "upstream", Kind: "upstream", Match: []string{"."}, Upstreams: []string{"udp://[2001:db8::53]:53"}}}}},
		}},
	}
}
