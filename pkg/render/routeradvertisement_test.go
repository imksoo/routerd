// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestDnsmasqConfigLeavesAdvertisementsToServeOwner(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8:1::123/64"}},
	}}}
	data, _, err := DnsmasqConfig(router, DnsmasqRuntime{IPv6PrefixesByInterface: map[string][]string{"eth0": {"2001:db8:2::/64"}}})
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if strings.Contains(output, "enable-ra") || strings.Contains(output, "dhcp-range") || strings.Contains(output, "2001:db8:") {
		t.Fatalf("dnsmasq duplicates the serve-owned RA service:\n%s", output)
	}
}
