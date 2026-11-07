// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"reflect"
	"testing"
)

// Requirement: declared IPv6 service intent, including pending scopes and OS
// aliases, must never be excluded merely because it cannot render a range yet.
func TestDnsmasqIPv4OnlyInterfaces(t *testing.T) {
	resource := func(kind, name string, spec api.ResourceSpec) api.Resource {
		return api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: name}, Spec: spec}
	}
	for _, tc := range []struct {
		name  string
		extra []api.Resource
		want  []string
	}{
		{"v4 only", nil, []string{"ens19"}},
		{"separate v6", []api.Resource{resource("DHCPv6Server", "v6", api.DHCPv6ServerSpec{Interface: "v6"})}, []string{"ens19"}},
		{"shared alias pending v6", []api.Resource{resource("DHCPv6Server", "v6", api.DHCPv6ServerSpec{Interface: "same", DNSServerFrom: []api.StatusValueSourceSpec{{Resource: "DHCPv6Information/pending", Field: "dnsServers"}}})}, nil},
		{"separate RA owner", []api.Resource{resource("IPv6RouterAdvertisement", "ra", api.IPv6RouterAdvertisementSpec{Interface: "same"})}, []string{"ens19"}},
		{"pending delegated IPv6", []api.Resource{resource("IPv6DelegatedAddress", "pd", api.IPv6DelegatedAddressSpec{Interface: "same", PrefixDelegation: "pending"}), resource("DHCPv6Server", "v6", api.DHCPv6ServerSpec{DelegatedAddress: "pd"})}, nil},
		{"unresolved delegated IPv6", []api.Resource{resource("DHCPv6Server", "v6", api.DHCPv6ServerSpec{DelegatedAddress: "missing"})}, nil},
		{"explicit v6 listener", []api.Resource{resource("DHCPv6Server", "v6", api.DHCPv6ServerSpec{ListenInterfaces: []string{"same"}})}, nil},
		{"relay and sorted deduplication", []api.Resource{resource("DHCPv4Relay", "relay", api.DHCPv4RelaySpec{Interfaces: []string{"other", "lan", "other"}})}, []string{"ens18", "ens19"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &api.Router{Spec: api.RouterSpec{Resources: append([]api.Resource{resource("DHCPv4Server", "v4", api.DHCPv4ServerSpec{Interface: "lan"})}, tc.extra...)}}
			got := DnsmasqIPv4OnlyInterfaces(r, map[string]string{"lan": "ens19", "same": "ens19", "v6": "lan-vrrp", "other": "ens18"})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	if got := DnsmasqIPv4OnlyInterfaces(nil, nil); len(got) != 0 {
		t.Fatalf("nil router: %v", got)
	}
}
