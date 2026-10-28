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
	ip = ip[:total]
	udp := ip[ihl:]
	length := int(binary.BigEndian.Uint16(udp[4:6]))
	if binary.BigEndian.Uint16(udp[:2]) != 67 || binary.BigEndian.Uint16(udp[2:4]) != 68 || length < 8 || length > len(udp) {
		return nil, netip.Addr{}
	}
	return udp[8:length], netip.AddrFrom4([4]byte{ip[12], ip[13], ip[14], ip[15]})
}
