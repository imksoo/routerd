// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"net"
	"net/netip"
	"testing"

	"github.com/vishvananda/netlink"
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

func TestNetlinkRouteBuildsMultipathNextHops(t *testing.T) {
	route, ok := netlinkRoute(FIBRoute{
		Prefix:   "10.77.60.11/32",
		NextHops: []string{"10.255.1.2", "10.255.1.3"},
	})
	if !ok {
		t.Fatal("netlinkRoute returned ok=false")
	}
	if route.Gw != nil {
		t.Fatalf("route.Gw = %v, want nil for multipath route", route.Gw)
	}
	if len(route.MultiPath) != 2 {
		t.Fatalf("route.MultiPath len = %d, want 2: %#v", len(route.MultiPath), route.MultiPath)
	}
	if !route.MultiPath[0].Gw.Equal(net.ParseIP("10.255.1.2")) || !route.MultiPath[1].Gw.Equal(net.ParseIP("10.255.1.3")) {
		t.Fatalf("route.MultiPath = %#v, want 10.255.1.2 and 10.255.1.3", route.MultiPath)
	}
}

func TestLocalConnectedRouteCoversProviderObservedHost(t *testing.T) {
	_, connected, err := net.ParseCIDR("172.31.16.0/20")
	if err != nil {
		t.Fatal(err)
	}
	routes := []netlink.Route{{
		Dst:      connected,
		Protocol: 2,
		Scope:    netlink.SCOPE_LINK,
	}}
	if !localConnectedRouteCovers(netip.MustParsePrefix("172.31.29.5/32"), routes) {
		t.Fatal("localConnectedRouteCovers returned false for host inside connected provider subnet")
	}
}

func TestLocalConnectedRouteCoverIgnoresBGPRoutes(t *testing.T) {
	_, bgpDst, err := net.ParseCIDR("172.31.29.5/32")
	if err != nil {
		t.Fatal(err)
	}
	routes := []netlink.Route{{
		Dst:      bgpDst,
		Protocol: bgpRouteProtocol,
	}}
	if localConnectedRouteCovers(netip.MustParsePrefix("172.31.29.5/32"), routes) {
		t.Fatal("localConnectedRouteCovers treated an existing BGP route as a connected cover")
	}
}
