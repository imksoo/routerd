// SPDX-License-Identifier: BSD-3-Clause
//go:build freebsd

package bgp

import (
	"syscall"
	"testing"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"golang.org/x/net/route"
)

func nativeFreeBSDRouteMessage() *route.RouteMessage {
	addresses := make([]route.Addr, syscall.RTAX_MAX)
	addresses[syscall.RTAX_DST] = &route.Inet4Addr{IP: [4]byte{10, 77, 60, 0}}
	addresses[syscall.RTAX_NETMASK] = &route.Inet4Addr{IP: [4]byte{255, 255, 255, 0}}
	addresses[syscall.RTAX_GATEWAY] = &route.Inet4Addr{IP: [4]byte{192, 0, 2, 1}}
	addresses[syscall.RTAX_IFA] = &route.Inet4Addr{IP: [4]byte{192, 0, 2, 10}}
	return &route.RouteMessage{Version: syscall.RTM_VERSION, Type: syscall.RTM_GET, Flags: syscall.RTF_UP | syscall.RTF_GATEWAY | syscall.RTF_STATIC | syscall.RTF_PROTO1, Index: 7, Addrs: addresses}
}
func TestFreeBSDNativeRIBReceiptUsesActualInterfaceAndSource(t *testing.T) {
	message := nativeFreeBSDRouteMessage()
	rows, err := observedFreeBSDRoutes([]route.Message{message}, 3)
	expected := bgpstate.NativeFIBRoute{Prefix: "10.77.60.0/24", NextHops: []bgpstate.FIBNextHop{{Gateway: "192.0.2.1", InterfaceIndex: 7}}, PreferredSource: "192.0.2.10", Table: 3, Protocol: syscall.RTF_PROTO1}
	if err != nil || len(rows) != 1 || !rows[0].Exact || !bgpstate.EqualNativeFIBRoute(rows[0].Route, expected) {
		t.Fatalf("native receipt=%+v %v, want %+v", rows, err, expected)
	}
	foreign := nativeFreeBSDRouteMessage()
	foreign.Flags &^= syscall.RTF_PROTO1
	foreign.Addrs[syscall.RTAX_GATEWAY] = &route.Inet4Addr{IP: [4]byte{192, 0, 2, 99}}
	rows, err = observedFreeBSDRoutes([]route.Message{message, foreign}, 3)
	if err != nil || len(rows) != 2 || !rows[0].Exact || rows[1].Exact || rows[1].Route.Protocol != 0 {
		t.Fatalf("same-prefix foreign hop confused ownership: %+v %v", rows, err)
	}
}
func TestFreeBSDIncompleteNativeRIBCannotAcknowledgeReceiptAbsence(t *testing.T) {
	message := nativeFreeBSDRouteMessage()
	message.Addrs[syscall.RTAX_IFA] = nil
	if _, err := observedFreeBSDRoutes([]route.Message{message}, 0); err == nil {
		t.Fatal("missing native route IFA became route absence")
	}
	message = nativeFreeBSDRouteMessage()
	message.Addrs[syscall.RTAX_NETMASK] = &route.Inet4Addr{IP: [4]byte{255, 0, 255, 0}}
	if _, err := observedFreeBSDRoutes([]route.Message{message}, 0); err == nil {
		t.Fatal("malformed native mask became route absence")
	}
}
