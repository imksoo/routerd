// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestDnsmasqConfigAdvertisesDeclaredStaticPrefix(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8:1::123/64"}},
	}}}
	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{IPv6PrefixesByInterface: map[string][]string{"eth0": {"2001:db8:2::/64"}}})
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if !strings.Contains(output, "dhcp-range=set:lan-ra,2001:db8:1::,ra-only,64,12h\n") || strings.Contains(output, "constructor:") || strings.Contains(output, "2001:db8:2::") {
		t.Fatalf("static RA prefix did not own its range:\n%s", output)
	}
}

func TestStaticRARangeDoesNotConvertDelegatedAuthority(t *testing.T) {
	line, err := DnsmasqStaticRouterAdvertisementRange("lan", api.IPv6RouterAdvertisementSpec{PrefixFrom: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan", Field: "address"}})
	if err != nil || line != "" {
		t.Fatalf("delegated lifetimes became a fixed static range: %q, %v", line, err)
	}
	if _, err := DnsmasqStaticRouterAdvertisementRange("lan", api.IPv6RouterAdvertisementSpec{Prefix: "2001:db8::/56"}); err == nil {
		t.Fatal("render admitted a prefix length unsupported by dnsmasq RA")
	}
}
