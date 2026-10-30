// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"encoding/binary"
	"net/netip"
)

// EthernetReply returns a bounded, unfragmented DHCP server UDP payload. Packet
// sockets and BPF capture do not provide an IP socket's framing validation.
func EthernetReply(frame []byte) ([]byte, netip.Addr) {
	if len(frame) < 14+20+8 || binary.BigEndian.Uint16(frame[12:14]) != 0x0800 {
		return nil, netip.Addr{}
	}
	ip := frame[14:]
	ihl := int(ip[0]&15) * 4
	total := int(binary.BigEndian.Uint16(ip[2:4]))
	if ip[0]>>4 != 4 || ihl < 20 || total < ihl+8 || total > len(ip) || ip[9] != 17 || binary.BigEndian.Uint16(ip[6:8])&0x3fff != 0 {
		return nil, netip.Addr{}
	}
	if checksumValue(checksumSum(ip[:ihl])) != 0 {
		return nil, netip.Addr{}
	}
	ip = ip[:total]
	udp := ip[ihl:]
	length := int(binary.BigEndian.Uint16(udp[4:6]))
	if binary.BigEndian.Uint16(udp[:2]) != 67 || binary.BigEndian.Uint16(udp[2:4]) != 68 || length < 8 || length > len(udp) {
		return nil, netip.Addr{}
	}
	if binary.BigEndian.Uint16(udp[6:8]) != 0 {
		sum := checksumSum(ip[12:20]) + uint32(ip[9]) + uint32(length) + checksumSum(udp[:length])
		if checksumValue(sum) != 0 {
			return nil, netip.Addr{}
		}
	}
	return udp[8:length], netip.AddrFrom4([4]byte{ip[12], ip[13], ip[14], ip[15]})
}

func checksumSum(data []byte) uint32 {
	var sum uint32
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
	}
	if len(data) > 0 {
		sum += uint32(data[0]) << 8
	}
	return sum
}

func checksumValue(sum uint32) uint16 {
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
