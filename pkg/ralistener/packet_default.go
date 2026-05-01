//go:build !freebsd

package ralistener

import "net"

func NewPacketConn(ifname string) (PacketConn, error) {
	return net.ListenPacket("ip6:ipv6-icmp", "::")
}
