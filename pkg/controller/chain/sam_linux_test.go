// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestSAMProxyNeighborMatchesKernelProxyDump(t *testing.T) {
	expected := &netlink.Neigh{LinkIndex: 7, Family: netlink.FAMILY_V4, IP: net.ParseIP("192.0.2.99"), State: netlink.NUD_NONE, Flags: netlink.NTF_PROXY}
	dump := []netlink.Neigh{*expected}
	if present, err := samMatchProxyNeighbors(expected, dump); err != nil || !present {
		t.Fatalf("kernel proxy entry: present=%t err=%v", present, err)
	}
	dump[0].Flags |= netlink.NTF_ROUTER
	if _, err := samMatchProxyNeighbors(expected, dump); err == nil {
		t.Fatal("foreign proxy flags accepted")
	}
	if _, err := samMatchProxyNeighbors(expected, []netlink.Neigh{*expected, *expected}); err == nil {
		t.Fatal("duplicate proxy entries accepted")
	}
}

func TestBuildGratuitousARPFrameUsesUnassignedAddress(t *testing.T) {
	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x30}
	ip := net.IPv4(192, 0, 2, 99)

	frame := buildGratuitousARPFrame(mac, ip)
	if len(frame) != 42 {
		t.Fatalf("frame length = %d, want 42", len(frame))
	}
	if got := net.HardwareAddr(frame[0:6]).String(); got != "ff:ff:ff:ff:ff:ff" {
		t.Fatalf("ethernet dst = %s, want broadcast", got)
	}
	if got := net.HardwareAddr(frame[6:12]).String(); got != mac.String() {
		t.Fatalf("ethernet src = %s, want %s", got, mac)
	}
	if got := net.IP(frame[28:32]).String(); got != "192.0.2.99" {
		t.Fatalf("arp sender IP = %s", got)
	}
	if got := net.IP(frame[38:42]).String(); got != "192.0.2.99" {
		t.Fatalf("arp target IP = %s", got)
	}
}
