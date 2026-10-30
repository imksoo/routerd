// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"encoding/binary"
	"testing"
)

func TestEthernetReplyDoesNotUseCapturePaddingAsIPPayload(t *testing.T) {
	frame := make([]byte, 14+20+8+240+24)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	ip := frame[14:]
	ip[0] = 0x45
	ip[9] = 17
	binary.BigEndian.PutUint16(ip[2:4], 20+8+240)
	copy(ip[12:16], []byte{192, 0, 2, 1})
	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[:2], 67)
	binary.BigEndian.PutUint16(udp[2:4], 68)
	binary.BigEndian.PutUint16(udp[4:6], 8+240)
	binary.BigEndian.PutUint16(ip[10:12], checksumValue(checksumSum(ip[:20])))
	if payload, source := EthernetReply(frame); len(payload) != 240 || source.String() != "192.0.2.1" {
		t.Fatalf("valid reply: %d %s", len(payload), source)
	}
	// Previously total=1 caused ip[ihl:] to panic, while a large total was
	// silently replaced by capture length and treated as a complete packet.
	for _, length := range []uint16{1, 27, 65535} {
		binary.BigEndian.PutUint16(ip[2:4], length)
		if payload, _ := EthernetReply(frame); len(payload) != 0 {
			t.Fatalf("invalid IP length %d accepted", length)
		}
	}
	binary.BigEndian.PutUint16(ip[2:4], 20+8+240)
	binary.BigEndian.PutUint16(ip[6:8], 0x2000)
	if payload, _ := EthernetReply(frame); len(payload) != 0 {
		t.Fatal("fragment accepted as complete DHCP reply")
	}
}

func TestEthernetReplyValidatesIPAndOptionalUDPChecksums(t *testing.T) {
	frame := make([]byte, 14+20+8+5)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	ip := frame[14:]
	ip[0], ip[8], ip[9] = 0x45, 64, 17
	binary.BigEndian.PutUint16(ip[2:4], uint16(len(ip)))
	copy(ip[12:20], []byte{192, 0, 2, 1, 255, 255, 255, 255})
	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[:2], 67)
	binary.BigEndian.PutUint16(udp[2:4], 68)
	binary.BigEndian.PutUint16(udp[4:6], uint16(len(udp)))
	copy(udp[8:], []byte{1, 2, 3, 4, 5})
	fixIP := func() {
		ip[10], ip[11] = 0, 0
		binary.BigEndian.PutUint16(ip[10:12], checksumValue(checksumSum(ip[:20])))
	}
	fixIP()
	if payload, _ := EthernetReply(frame); len(payload) != 5 {
		t.Fatal("rejected permitted zero IPv4 UDP checksum")
	}
	ip[8] ^= 1
	if payload, _ := EthernetReply(frame); payload != nil {
		t.Fatal("accepted invalid IPv4 checksum")
	}
	ip[8] ^= 1
	binary.BigEndian.PutUint16(udp[6:8], checksumValue(checksumSum(ip[12:20])+17+uint32(len(udp))+checksumSum(udp)))
	if payload, _ := EthernetReply(frame); len(payload) != 5 {
		t.Fatal("rejected valid nonzero UDP checksum with odd payload")
	}
	udp[len(udp)-1] ^= 1
	if payload, _ := EthernetReply(frame); payload != nil {
		t.Fatal("accepted corrupted UDP payload")
	}
	udp[len(udp)-1] ^= 1
	ip[16] ^= 1
	fixIP()
	if payload, _ := EthernetReply(frame); payload != nil {
		t.Fatal("UDP validation omitted the pseudo-header")
	}
}
