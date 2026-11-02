// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package main

import (
	"context"
	"net"
	"net/netip"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/dhcpv4client"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

const (
	etherTypeIPv4 = 0x0800
)

func bindSocketToDevice(fd int, ifname string) error {
	return unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifname)
}

func listenDHCPv4(ifname string) (dhcpv4PacketConn, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	rawFD, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(htons(etherTypeIPv4)))
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(rawFD, &unix.SockaddrLinklayer{Protocol: htons(etherTypeIPv4), Ifindex: ifi.Index}); err != nil {
		_ = unix.Close(rawFD)
		return nil, err
	}
	lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
		var sockErr error
		if err := c.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
			if sockErr == nil {
				sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
			}
			if sockErr == nil && ifname != "" {
				sockErr = bindSocketToDevice(int(fd), ifname)
			}
		}); err != nil && sockErr == nil {
			sockErr = err
		}
		return sockErr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":68")
	if err != nil {
		_ = unix.Close(rawFD)
		return nil, err
	}
	return &linuxDHCPv4PacketConn{rawFD: rawFD, write: pc.(*net.UDPConn), hwaddr: append(net.HardwareAddr(nil), ifi.HardwareAddr...), ifindex: ifi.Index}, nil
}

type linuxDHCPv4PacketConn struct {
	rawFD    int
	write    *net.UDPConn
	hwaddr   net.HardwareAddr
	ifindex  int
	deadline time.Time
}

func (c *linuxDHCPv4PacketConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	return c.write.ReadFromUDP(buf)
}

func (c *linuxDHCPv4PacketConn) Send(ctx context.Context, packet dhcpv4client.Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if packet.Destination == netip.MustParseAddr("255.255.255.255") {
		if err := unix.SetsockoptTimeval(c.rawFD, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &unix.Timeval{Sec: 2}); err != nil {
			return err
		}
		frame := ethernetBroadcast(c.hwaddr, packet)
		return unix.Sendto(c.rawFD, frame, 0, &unix.SockaddrLinklayer{Ifindex: c.ifindex, Protocol: htons(etherTypeIPv4)})
	}
	deadline := time.Now().Add(2 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err := c.write.SetWriteDeadline(deadline); err != nil {
		return err
	}
	_, err := ipv4.NewPacketConn(c.write).WriteTo(packet.Payload, &ipv4.ControlMessage{Src: net.IP(packet.Source.AsSlice()), IfIndex: c.ifindex}, &net.UDPAddr{IP: net.IP(packet.Destination.AsSlice()), Port: 67})
	return err
}

func (c *linuxDHCPv4PacketConn) SetReadDeadline(deadline time.Time) error {
	c.deadline = deadline
	return c.write.SetReadDeadline(deadline)
}

func (c *linuxDHCPv4PacketConn) Close() error {
	err1 := unix.Close(c.rawFD)
	err2 := c.write.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func htons(v uint16) uint16 {
	return (v<<8)&0xff00 | v>>8
}
