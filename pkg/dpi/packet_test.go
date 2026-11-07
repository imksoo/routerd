// SPDX-License-Identifier: BSD-3-Clause

package dpi

import (
	"encoding/binary"
	"testing"
)

func ipv4UDPForTest(body []byte) []byte {
	p := make([]byte, 28+len(body))
	p[0] = 0x45
	p[9] = 17
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
	copy(p[12:20], []byte{192, 0, 2, 1, 198, 51, 100, 2})
	binary.BigEndian.PutUint16(p[20:22], 50000)
	binary.BigEndian.PutUint16(p[22:24], 5353)
	binary.BigEndian.PutUint16(p[24:26], uint16(8+len(body)))
	copy(p[28:], body)
	return p
}

func TestPacketLengthAndFragmentBoundsDoNotInventApplicationBytes(t *testing.T) {
	p := ipv4UDPForTest(nil)
	p = append(p, MinimalTLSClientHello("padding.example")...)
	info, ok := InspectPacket(p)
	if !ok || len(info.Payload) != 0 || len(info.Packet) != 28 {
		t.Fatalf("padding exposed: %+v", info)
	}
	if got := Classify(ClassifyRequest{Packet: p}); got.TLSSNI != "" {
		t.Fatalf("padding classified: %+v", got)
	}
	p = ipv4UDPForTest(MinimalTLSClientHello("fragment.example"))
	p[7] = 1
	info, ok = InspectPacket(p)
	if !ok || info.SrcPort != 0 || len(info.Payload) != 0 {
		t.Fatalf("noninitial fragment exposed transport: %+v", info)
	}
}

func TestIPv6KnownExtensionsAndOpaqueESP(t *testing.T) {
	for _, next := range []byte{0, 43, 60, 44, 51} {
		t.Run(protocolName(next), func(t *testing.T) {
			udp := ipv4UDPForTest([]byte("data"))[20:]
			p := make([]byte, 48+len(udp))
			p[0] = 0x60
			p[6] = next
			p[40] = 17
			if next == 51 {
				p[41] = 0
			} // eight-byte AH minimum encoded length
			binary.BigEndian.PutUint16(p[4:6], uint16(len(p)-40))
			copy(p[48:], udp)
			info, ok := InspectPacket(p)
			if !ok || info.DstPort != 5353 || string(info.Payload) != "data" {
				t.Fatalf("extension %d: %+v", next, info)
			}
			if next == 44 {
				p[43] = 8
				info, _ = InspectPacket(p)
				if info.DstPort != 0 || len(info.Payload) != 0 {
					t.Fatal("noninitial v6 fragment has ports")
				}
			}
		})
	}
	p := make([]byte, 56)
	p[0] = 0x60
	p[6] = 50
	p[40] = 17
	binary.BigEndian.PutUint16(p[4:6], 16)
	info, ok := InspectPacket(p)
	if !ok || info.TransportProtocol != "50" || info.SrcPort != 0 {
		t.Fatalf("ESP parsed as extension: %+v", info)
	}
}

func TestTruncatedCaptureRetainsKnownTransportMetadata(t *testing.T) {
	full := ipv4UDPForTest([]byte("not-captured"))
	info, ok := InspectPacket(full[:24])
	if !ok || info.SrcPort != 50000 || info.DstPort != 5353 || info.Payload != nil {
		t.Fatalf("usable truncated capture metadata lost: %+v", info)
	}
}
