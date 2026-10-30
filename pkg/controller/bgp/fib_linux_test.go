// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package bgp

import (
	"net"
	"reflect"
	"testing"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/vishvananda/netlink"
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
