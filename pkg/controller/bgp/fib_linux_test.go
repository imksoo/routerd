// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package bgp

import (
	"net"
	"net/netip"
	"reflect"
	"testing"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestNativeNetlinkReceiptRoundTrip(t *testing.T) {
	for _, hops := range [][]bgpstate.FIBNextHop{{{Gateway: "192.0.2.1", InterfaceIndex: 3}}, {{Gateway: "192.0.2.1", InterfaceIndex: 3}, {Gateway: "198.51.100.1", InterfaceIndex: 4}}} {
		receipt := bgpstate.NativeFIBRoute{Prefix: "10.77.60.11/32", NextHops: hops, PreferredSource: "10.77.60.4", Table: 254, Protocol: bgpRouteProtocol, Metric: 200}
		native := nativeNetlinkRoute(receipt)
		got, err := observedNetlinkFIBRoute(*native)
		if err != nil || !got.Exact || !reflect.DeepEqual(got.Route, receipt) {
			t.Fatalf("native receipt roundtrip=%+v, %v; want %+v", got, err, receipt)
		}
		native.Protocol = 99
		changed, err := observedNetlinkFIBRoute(*native)
		if err != nil || bgpstate.EqualNativeFIBRoute(changed.Route, receipt) {
			t.Fatalf("different native selector retained receipt: %+v, %v", changed, err)
		}
	}
}

func TestNativeIPv6NetlinkReceiptRoundTripPreservesForeignAttributes(t *testing.T) {
	for _, hops := range [][]bgpstate.FIBNextHop{
		{{Gateway: "2001:db8:1::2", InterfaceIndex: 3}},
		{{Gateway: "2001:db8:1::2", InterfaceIndex: 3}, {Gateway: "2001:db8:2::2", InterfaceIndex: 4}},
	} {
		receipt := bgpstate.NativeFIBRoute{Prefix: "2001:db8:40::1/128", NextHops: hops, PreferredSource: "2001:db8:1::1", Table: 254, Protocol: bgpRouteProtocol, Metric: 200}
		native := nativeNetlinkRoute(receipt)
		if native.Family != unix.AF_INET6 {
			t.Fatalf("IPv6 native write has family %d", native.Family)
		}
		observed, err := observedNetlinkFIBRoute(*native)
		if err != nil || !observed.Exact || !bgpstate.EqualNativeFIBRoute(observed.Route, receipt) {
			t.Fatalf("IPv6 receipt roundtrip=%+v err=%v", observed, err)
		}
		native.MTU = 1280
		observed, err = observedNetlinkFIBRoute(*native)
		if err != nil || observed.Exact {
			t.Fatalf("additional foreign IPv6 MTU was adopted: %+v err=%v", observed, err)
		}
	}
}

func TestNativeFIBAddressObservationPreservesIPv6DADState(t *testing.T) {
	for _, fixture := range []struct {
		address     string
		bits, flags int
	}{
		{"198.18.0.1", 24, 0}, {"2001:db8:1::1", 64, 0},
		{"2001:db8:1::1", 64, unix.IFA_F_TENTATIVE}, {"2001:db8:1::1", 64, unix.IFA_F_DADFAILED},
	} {
		address := netip.MustParseAddr(fixture.address)
		observed, err := observedNetlinkFIBAddress(netlink.Addr{IPNet: &net.IPNet{IP: net.ParseIP(fixture.address), Mask: net.CIDRMask(fixture.bits, address.BitLen())}, Flags: fixture.flags})
		if err != nil || observed.Address != address || observed.Prefix != netip.PrefixFrom(address, fixture.bits).Masked() || observed.Tentative != (fixture.flags&unix.IFA_F_TENTATIVE != 0) || observed.DADFailed != (fixture.flags&unix.IFA_F_DADFAILED != 0) {
			t.Fatalf("native address readiness lost: %+v err=%v", observed, err)
		}
	}
}

func TestFIBIPv6PreferredSourceReadinessDoesNotEraseLocalPresence(t *testing.T) {
	for _, state := range []string{"ready", "tentative", "dadfailed"} {
		t.Run(state, func(t *testing.T) {
			store, backend, owner := newFIBOwnerFixture(t)
			local := fibLocalAddress{Address: netip.MustParseAddr("2001:db8:40::1"), Prefix: netip.MustParsePrefix("2001:db8:40::/64"), Tentative: state == "tentative", DADFailed: state == "dadfailed"}
			backend.locals = []fibLocalAddress{local}
			routes := []FIBRoute{{Prefix: "2001:db8:40::2/128", NextHops: []string{"2001:db8:1::2"}, PreferredSource: local.Address.String()}}
			result, err := syncFIBFixture(t.Context(), owner, store, routes)
			if err != nil || !result.Installed[routes[0].Prefix] {
				t.Fatalf("IPv6 forwarding route: %+v err=%v", result, err)
			}
			if state == "ready" {
				if result.PreferredSource[routes[0].Prefix] != local.Address.String() {
					t.Fatal("ready native IPv6 source was not retained")
				}
			} else {
				want := "LocalAddressTentative"
				if state == "dadfailed" {
					want = "LocalAddressDADFailed"
				}
				if result.PreferredSource[routes[0].Prefix] != "" || !result.PreferredSourceSkipped[routes[0].Prefix] || result.PreferredSourceSkippedReason[routes[0].Prefix] != want {
					t.Fatalf("unready IPv6 source became usable or absent: %+v", result)
				}
			}
			// A tentative or failed address still exists locally. Never install
			// a remote host route over that native local identity.
			localRoute := []FIBRoute{{Prefix: "2001:db8:40::1/128", NextHops: []string{"2001:db8:1::2"}}}
			result, err = syncFIBFixture(t.Context(), owner, store, localRoute)
			if err != nil || result.Installed[localRoute[0].Prefix] {
				t.Fatalf("local IPv6 presence became a remote route: %+v err=%v", result, err)
			}
		})
	}
}

func TestNativeNetlinkObservationDoesNotTreatAdditionalAttributesAsOurs(t *testing.T) {
	receipt := bgpstate.NativeFIBRoute{Prefix: "10.77.60.11/32", NextHops: []bgpstate.FIBNextHop{{Gateway: "192.0.2.1", InterfaceIndex: 3}}, Table: 254, Protocol: bgpRouteProtocol, Metric: 200}
	native := nativeNetlinkRoute(receipt)
	native.MTU = 1200
	observed, err := observedNetlinkFIBRoute(*native)
	if err != nil || observed.Exact || observed.Route.Prefix != receipt.Prefix {
		t.Fatalf("foreign route was lost or became owned: %+v, %v", observed, err)
	}
	_, dst, _ := net.ParseCIDR("10.77.60.0/24")
	observed, err = observedNetlinkFIBRoute(netlink.Route{Dst: dst, Table: 254, LinkIndex: 7})
	if err != nil || observed.Exact || observed.Route.Prefix != "10.77.60.0/24" {
		t.Fatalf("connected route not retained as a conflict: %+v, %v", observed, err)
	}
}
