// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package main

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func bindSocketToDevice(fd int, ifname string) error {
	return unix.SetsockoptString(fd, unix.SOL_SOCKET, unix.SO_BINDTODEVICE, ifname)
}

func listenDHCPv4(ifname string) (dhcpv4PacketConn, error) {
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
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}
