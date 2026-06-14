// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"net"
	"net/netip"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestFIBRouteFromNetlinkRoute(t *testing.T) {
	_, dst, err := net.ParseCIDR("10.77.60.10/32")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := fibRouteFromNetlinkRoute(netlink.Route{
		Dst:      dst,
		Gw:       net.ParseIP("10.255.0.11"),
		Src:      net.ParseIP("10.77.60.4"),
		Protocol: bgpRouteProtocol,
	})
	if !ok {
		t.Fatal("fibRouteFromNetlinkRoute returned ok=false")
	}
	want := FIBRoute{Prefix: "10.77.60.10/32", NextHops: []string{"10.255.0.11"}, PreferredSource: "10.77.60.4"}
	if !equalFIBRoute(got, want) {
		t.Fatalf("route = %#v, want %#v", got, want)
	}
}

func TestFIBRouteFromNetlinkRouteMultipath(t *testing.T) {
	_, dst, err := net.ParseCIDR("10.77.60.11/32")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := fibRouteFromNetlinkRoute(netlink.Route{
		Dst: dst,
		MultiPath: []*netlink.NexthopInfo{
			{Gw: net.ParseIP("10.255.0.12")},
			{Gw: net.ParseIP("10.255.0.11")},
		},
		Protocol: bgpRouteProtocol,
	})
	if !ok {
		t.Fatal("fibRouteFromNetlinkRoute returned ok=false")
	}
	want := FIBRoute{Prefix: "10.77.60.11/32", NextHops: []string{"10.255.0.11", "10.255.0.12"}}
	if !equalFIBRoute(got, want) {
		t.Fatalf("route = %#v, want %#v", got, want)
	}
}

func TestFilterLocalHostFIBRoutes(t *testing.T) {
	routes := map[string]FIBRoute{
		"10.77.60.5/32":  {Prefix: "10.77.60.5/32", NextHops: []string{"10.255.0.41"}},
		"10.77.60.11/32": {Prefix: "10.77.60.11/32", NextHops: []string{"10.255.0.41"}},
	}
	got := filterLocalHostFIBRoutes(routes, localIPv4HostPrefixes([]localIPv4Address{{
		Address: netip.MustParseAddr("10.77.60.5"),
		Prefix:  netip.MustParsePrefix("10.77.60.0/24"),
	}}))
	if _, ok := got["10.77.60.5/32"]; ok {
		t.Fatalf("local host route was kept: %#v", got)
	}
	if _, ok := got["10.77.60.11/32"]; !ok {
		t.Fatalf("remote route was removed: %#v", got)
	}
}

func TestInferPreferredSourceUsesConnectedAddressForRemoteMobilityPrefix(t *testing.T) {
	got := inferPreferredSource("10.77.60.12/32", []localIPv4Address{
		{Address: netip.MustParseAddr("10.255.70.4"), Prefix: netip.MustParsePrefix("10.255.70.4/31")},
		{Address: netip.MustParseAddr("10.77.60.4"), Prefix: netip.MustParsePrefix("10.77.60.0/24")},
	})
	if got != "10.77.60.4" {
		t.Fatalf("preferred source = %q, want 10.77.60.4", got)
	}
}

func TestInferPreferredSourceDoesNotUseDestinationAddress(t *testing.T) {
	got := inferPreferredSource("10.77.60.4/32", []localIPv4Address{
		{Address: netip.MustParseAddr("10.77.60.4"), Prefix: netip.MustParsePrefix("10.77.60.0/24")},
	})
	if got != "" {
		t.Fatalf("preferred source = %q, want empty for local destination", got)
	}
}

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
