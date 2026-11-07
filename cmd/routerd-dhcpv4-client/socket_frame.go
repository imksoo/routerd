// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/binary"
	"github.com/imksoo/routerd/pkg/dhcpv4client"
	"net"
)

// Before binding, DHCP must use 0.0.0.0 even if the interface has other IPv4
// addresses. Broadcast through the link socket instead of kernel source choice.
func ethernetBroadcast(src net.HardwareAddr, packet dhcpv4client.Packet) []byte {
	ipLen := 20 + 8 + len(packet.Payload)
	frame := make([]byte, 14+ipLen)
	for i := 0; i < 6; i++ {
		frame[i] = 0xff
	}
	copy(frame[6:12], src)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	ip := frame[14:]
	ip[0], ip[8], ip[9] = 0x45, 64, 17
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipLen))
	copy(ip[12:16], packet.Source.AsSlice())
	copy(ip[16:20], packet.Destination.AsSlice())
	binary.BigEndian.PutUint16(ip[10:12], ipv4Checksum(ip[:20]))
	udp := ip[20:]
	binary.BigEndian.PutUint16(udp[0:2], 68)
	binary.BigEndian.PutUint16(udp[2:4], 67)
	binary.BigEndian.PutUint16(udp[4:6], uint16(8+len(packet.Payload)))
	copy(udp[8:], packet.Payload)
	return frame
}

func ipv4Checksum(header []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(header); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(header[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
