// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"strings"
	"testing"
)

func TestDHCPv6RangesSeparateServiceFromRouterAdvertisements(t *testing.T) {
	for _, tc := range []struct{ mode, start, end, want string }{
		{"stateless", "", "", "dhcp-range=tag:lan0,set:server,::,static,64,12h"},
		{"ra-only", "", "", ""},
		{"stateful", "fd00:30::100", "fd00:30::1ff", "dhcp-range=tag:lan0,set:server,fd00:30::100,fd00:30::1ff,64,12h"},
		{"both", "::100", "::1ff", "dhcp-range=tag:lan0,set:server,::100,::1ff,constructor:lan0,64,12h"},
	} {
		got := DnsmasqDHCPv6Range("server", "lan0", api.DHCPv6ServerSpec{Mode: tc.mode, AddressPool: api.DHCPAddressPoolSpec{Start: tc.start, End: tc.end}})
		if got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.mode, got, tc.want)
		}
		if strings.Contains(got, "slaac") || strings.Contains(got, "ra-stateless") {
			t.Fatal("DHCP retains RA ownership")
		}
	}
}
