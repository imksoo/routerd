// SPDX-License-Identifier: BSD-3-Clause

package chain

import "testing"

func TestLinuxDSLiteTunnelMatchesCanonicalIPv6(t *testing.T) {
	show := []byte("ds-lite-ra: ip/ipv6 remote 2404:8e00:0:0:0:0:feed:100 local 2409:10:3d60:1200:0:5eff:fe00:113 dev wan-vmac encaplimit none")
	if !linuxDSLiteTunnelMatches(show, "2404:8e00::feed:100", "2409:10:3d60:1200:0:5eff:fe00:113", "wan-vmac", "none") {
		t.Fatal("equivalent canonical IPv6 endpoints did not match")
	}
}
