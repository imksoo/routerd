// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package main

import (
	"fmt"
	"net"
	"time"
)

func bindSocketToDevice(_ int, _ string) error {
	return fmt.Errorf("routerd-dhcpv4-client requires Linux SO_BINDTODEVICE; use the platform DHCPv4 path on this OS")
}

func listenDHCPv4(_ string) (dhcpv4PacketConn, error) {
	return nil, fmt.Errorf("routerd-dhcpv4-client is not implemented on this OS")
}

var _ dhcpv4PacketConn = unsupportedPacketConn{}

type unsupportedPacketConn struct{}

func (unsupportedPacketConn) ReadFromUDP([]byte) (int, *net.UDPAddr, error) { return 0, nil, nil }
func (unsupportedPacketConn) WriteToUDP([]byte, *net.UDPAddr) (int, error)  { return 0, nil }
func (unsupportedPacketConn) SetReadDeadline(time.Time) error               { return nil }
func (unsupportedPacketConn) Close() error                                  { return nil }
