// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/binary"
	"testing"
)

func TestVLANEthernetFrame(t *testing.T) {
	frame := make([]byte, 18)
	for i := range frame {
		frame[i] = byte(i)
	}
	binary.BigEndian.PutUint16(frame[12:14], etherTypeIPv4)
	tagged := vlanEthernetFrame(frame, 3132)
	if len(tagged) != len(frame)+4 || binary.BigEndian.Uint16(tagged[12:14]) != etherTypeVLAN || binary.BigEndian.Uint16(tagged[14:16]) != 3132 || binary.BigEndian.Uint16(tagged[16:18]) != etherTypeIPv4 {
		t.Fatalf("tagged Ethernet frame = %x", tagged)
	}
	if string(tagged[:12]) != string(frame[:12]) || string(tagged[18:]) != string(frame[14:]) {
		t.Fatalf("frame payload changed: %x", tagged)
	}
}
