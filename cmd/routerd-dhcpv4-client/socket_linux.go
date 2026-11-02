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

func bindSocketToDevice(fd int, ifname string) error {
	return unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifname)
}

func listenDHCPv4(ifname string) (dhcpv4PacketConn, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	broadcastFD, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.IPPROTO_RAW)
	if err != nil {
		return nil, err
	}
	if err := unix.SetsockoptInt(broadcastFD, unix.IPPROTO_IP, unix.IP_HDRINCL, 1); err != nil {
		_ = unix.Close(broadcastFD)
		return nil, err
	}
	if err := unix.SetsockoptInt(broadcastFD, unix.SOL_SOCKET, unix.SO_BROADCAST, 1); err != nil {
		_ = unix.Close(broadcastFD)
		return nil, err
	}
	if err := bindSocketToDevice(broadcastFD, ifname); err != nil {
		_ = unix.Close(broadcastFD)
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
		_ = unix.Close(broadcastFD)
		return nil, err
	}
	return &linuxDHCPv4PacketConn{broadcastFD: broadcastFD, write: pc.(*net.UDPConn), ifindex: ifi.Index}, nil
}

type linuxDHCPv4PacketConn struct {
	broadcastFD int
	write       *net.UDPConn
	ifindex     int
	deadline    time.Time
}

func (c *linuxDHCPv4PacketConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	return c.write.ReadFromUDP(buf)
}

func (c *linuxDHCPv4PacketConn) Send(ctx context.Context, packet dhcpv4client.Packet) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if packet.Destination == netip.MustParseAddr("255.255.255.255") {
		if err := unix.SetsockoptTimeval(c.broadcastFD, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &unix.Timeval{Sec: 2}); err != nil {
			return err
		}
		return unix.Sendto(c.broadcastFD, ipv4Broadcast(packet), 0, &unix.SockaddrInet4{Addr: [4]byte{255, 255, 255, 255}})
	}
	deadline := time.Now().Add(2 * time.Second)
	if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
		deadline = end
	}
	if err := c.write.SetWriteDeadline(deadline); err != nil {
		return err
	}
	control := &ipv4.ControlMessage{IfIndex: c.ifindex}
	if !packet.Source.IsUnspecified() {
		control.Src = net.IP(packet.Source.AsSlice())
	}
	_, err := ipv4.NewPacketConn(c.write).WriteTo(packet.Payload, control, &net.UDPAddr{IP: net.IP(packet.Destination.AsSlice()), Port: 67})
	return err
}

func (c *linuxDHCPv4PacketConn) SetReadDeadline(deadline time.Time) error {
	c.deadline = deadline
	return c.write.SetReadDeadline(deadline)
}

func (c *linuxDHCPv4PacketConn) Close() error {
	err := unix.Close(c.broadcastFD)
	if closeErr := c.write.Close(); err == nil {
		return closeErr
	}
	return err
}
