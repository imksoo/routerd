// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"net"
	"testing"
)

func TestNetlinkRouteSetsPreferredSource(t *testing.T) {
	route, ok := netlinkRoute(FIBRoute{
		Prefix:          "10.77.60.11/32",
		NextHops:        []string{"10.99.0.2"},
		PreferredSource: "10.77.60.10",
	})
	if !ok {
		t.Fatal("netlinkRoute returned ok=false")
	}
	if !route.Src.Equal(net.ParseIP("10.77.60.10")) {
		t.Fatalf("route.Src = %v, want 10.77.60.10", route.Src)
	}
}
