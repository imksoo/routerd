// SPDX-License-Identifier: BSD-3-Clause

package dpi

import (
	"encoding/binary"
	"net/netip"
)

// PacketInfo describes only bytes in the observed IP packet. Payload may be a
// captured prefix; non-initial fragments never expose transport ports/payload.
// Packet and Payload borrow the caller's bytes.
type PacketInfo struct {
	L3Proto           string
	TransportProtocol string
	SrcAddress        string
	SrcPort           int
	DstAddress        string
	DstPort           int
	Length            int
	Packet            []byte
	Payload           []byte
	Transport         []byte
}

func InspectPacket(packet []byte) (PacketInfo, bool) {
	if len(packet) == 0 {
		return PacketInfo{}, false
	}
	var info PacketInfo
	var next byte
	var offset int
	switch packet[0] >> 4 {
	case 4:
		if len(packet) < 20 {
			return info, false
		}
		offset = int(packet[0]&15) * 4
		total := int(binary.BigEndian.Uint16(packet[2:4]))
		if offset < 20 || offset > len(packet) || total < offset {
			return info, false
		}
		info.L3Proto = "ipv4"
		info.SrcAddress = netip.AddrFrom4([4]byte(packet[12:16])).String()
		info.DstAddress = netip.AddrFrom4([4]byte(packet[16:20])).String()
		next = packet[9]
		info.Length = total
		if total < len(packet) {
			packet = packet[:total]
		}
		info.Packet = packet
		info.TransportProtocol = protocolName(next)
		if binary.BigEndian.Uint16(packet[6:8])&0x1fff != 0 {
			return info, true
		}
	case 6:
		if len(packet) < 40 {
			return info, false
		}
		info.L3Proto = "ipv6"
		info.SrcAddress = netip.AddrFrom16([16]byte(packet[8:24])).String()
		info.DstAddress = netip.AddrFrom16([16]byte(packet[24:40])).String()
		next = packet[6]
		offset = 40
		total := 40 + int(binary.BigEndian.Uint16(packet[4:6]))
		info.Length = total
		if total < len(packet) {
			packet = packet[:total]
		}
		info.Packet = packet
		// RFC 8200 section 4: only known extension formats may be traversed.
		for {
			info.TransportProtocol = protocolName(next)
			var size int
			switch next {
			case 0, 43, 60, 135, 139, 140:
				if offset+2 > len(packet) {
					return info, true
				}
				size = (int(packet[offset+1]) + 1) * 8
			case 51: // RFC 4302: AH Payload Len is in 32-bit words, excluding two.
				if offset+2 > len(packet) {
					return info, true
				}
				size = (int(packet[offset+1]) + 2) * 4
			case 44:
				size = 8
				if offset+size > len(packet) {
					return info, true
				}
				if binary.BigEndian.Uint16(packet[offset+2:offset+4])&0xfff8 != 0 {
					info.TransportProtocol = protocolName(packet[offset])
					return info, true
				}
			default: // ESP and unknown next headers remain opaque.
				return packetTransport(info, next, packet[offset:]), true
			}
			if offset+size > len(packet) {
				return info, true
			}
			next = packet[offset]
			offset += size
		}
	default:
		return info, false
	}
	return packetTransport(info, next, packet[offset:]), true
}

func packetTransport(info PacketInfo, next byte, segment []byte) PacketInfo {
	info.TransportProtocol = protocolName(next)
	if next != 6 && next != 17 {
		return info
	}
	// Snaplen may include the ports while omitting the rest of the transport
	// header. Keep that independent metadata without inventing app bytes.
	if len(segment) >= 4 {
		info.SrcPort = int(binary.BigEndian.Uint16(segment[:2]))
		info.DstPort = int(binary.BigEndian.Uint16(segment[2:4]))
	}
	switch next {
	case 6:
		if len(segment) < 13 {
			return info
		}
		offset := int(segment[12]>>4) * 4
		if offset < 20 {
			return info
		}
		info.Transport = segment
		if offset <= len(segment) {
			info.Payload = segment[offset:]
		}
	case 17:
		if len(segment) < 8 {
			return info
		}
		total := int(binary.BigEndian.Uint16(segment[4:6]))
		if total < 8 {
			return info
		}
		if total < len(segment) {
			segment = segment[:total]
		}
		info.Transport = segment
		info.Payload = segment[8:]
	}
	return info
}
