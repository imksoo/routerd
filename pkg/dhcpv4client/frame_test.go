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
