// SPDX-License-Identifier: BSD-3-Clause

package raobserver

import (
	"encoding/binary"
	"net"
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

func advertisementFrame(t *testing.T) []byte {
	t.Helper()
	frame := []byte{
		0x33, 0x33, 0x00, 0x00, 0x00, 0x01,
		0x02, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x86, 0xdd,
		0x60, 0x00, 0x00, 0x00, 0x00, 0x50, 0x3a, 0xff,
		0xfe, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0xff, 0xfe, 0x00, 0x00, 0x01,
		0xff, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		134, 0, 0, 0, 64, 0x48, 0x00, 0xb4,
		0, 0, 0, 0, 0, 0, 0, 0,
		3, 4, 64, 0xc0, 0, 0, 0x0e, 0x10, 0, 0, 0x07, 0x08,
		0, 0, 0, 0,
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x01, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		24, 3, 64, 0x08, 0, 0, 0x02, 0x58,
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x02, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		25, 3, 0, 0, 0, 0, 0x02, 0x58,
		0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0x53,
	}
	return signAdvertisement(t, frame)
}

func signAdvertisement(t *testing.T, frame []byte) []byte {
	t.Helper()
	ip := frame[14:]
	binary.BigEndian.PutUint16(ip[4:6], uint16(len(ip)-40))
	payload := ip[40:]
	message := icmp.Message{Type: ipv6.ICMPTypeRouterAdvertisement, Code: int(payload[1]), Body: &icmp.RawBody{Data: payload[4:]}}
	encoded, err := message.Marshal(icmp.IPv6PseudoHeader(net.IP(ip[8:24]), net.IP(ip[24:40])))
	if err != nil {
		t.Fatal(err)
	}
	copy(payload, encoded)
	return frame
}

func TestParseEthernetIPv6RA(t *testing.T) {
	frame := advertisementFrame(t)

	adv, ok, err := ParseEthernetIPv6RA(frame)
	if err != nil {
		t.Fatalf("parse RA: %v", err)
	}
	if !ok {
		t.Fatal("frame was not detected as RA")
	}
	if adv.SourceMAC != "02:00:00:00:00:01" || adv.SourceLLA != "fe80::ff:fe00:1" {
		t.Fatalf("source = %s %s", adv.SourceMAC, adv.SourceLLA)
	}
	if adv.RouterLifetime != 180 || adv.Preference != "high" || !adv.OtherConfig {
		t.Fatalf("header = %#v", adv)
	}
	if len(adv.Prefixes) != 1 || adv.Prefixes[0].Prefix != "2001:db8:1::/64" {
		t.Fatalf("prefixes = %#v", adv.Prefixes)
	}
	if len(adv.Routes) != 1 || adv.Routes[0].Prefix != "2001:db8:2::/64" || adv.Routes[0].Preference != "high" {
		t.Fatalf("routes = %#v", adv.Routes)
	}
	if len(adv.RDNSS) != 1 || len(adv.RDNSS[0].Servers) != 1 || adv.RDNSS[0].Servers[0] != "2001:db8::53" {
		t.Fatalf("rdnss = %#v", adv.RDNSS)
	}
}

func TestAdvertisementBoundsAndIndependentOptions(t *testing.T) {
	// Ignore Ethernet padding; do not interpret it as option bytes.
	frame := append(advertisementFrame(t), make([]byte, 24)...)
	if _, ok, err := ParseEthernetIPv6RA(frame); err != nil || !ok {
		t.Fatalf("padded RA rejected: %t %v", ok, err)
	}
	frame = advertisementFrame(t)
	frame[14+40+16+2] = 129 // unusable independent PIO, followed by usable RIO/DNS
	signAdvertisement(t, frame)
	adv, ok, err := ParseEthernetIPv6RA(frame)
	if err != nil || !ok || len(adv.Prefixes) != 0 || len(adv.Routes) != 1 || len(adv.RDNSS) != 1 {
		t.Fatalf("bad PIO discarded unrelated options: %+v %t %v", adv, ok, err)
	}
	frame = advertisementFrame(t)
	frame[14+7] = 64
	if _, ok, _ := ParseEthernetIPv6RA(frame); ok {
		t.Fatal("forwarded RA accepted")
	}
	frame = advertisementFrame(t)
	frame[14+40+6] ^= 1
	if _, ok, _ := ParseEthernetIPv6RA(frame); ok {
		t.Fatal("corrupted RA accepted")
	}
	frame = advertisementFrame(t)
	binary.BigEndian.PutUint16(frame[14+4:14+6], 65535)
	if _, ok, _ := ParseEthernetIPv6RA(frame); ok {
		t.Fatal("truncated IPv6 payload accepted")
	}
}
