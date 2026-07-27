// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package main

import (
	"context"
	"net"
	"testing"
)

func TestDNSListenConfigBindsNonLocalIPv4Address(t *testing.T) {
	config := dnsListenConfig()
	ctx := context.Background()
	addr := net.JoinHostPort("192.0.2.248", "0")
	packetConn, err := config.ListenPacket(ctx, "udp", addr)
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatalf("ListenPacket(%s) failed: %v", addr, err)
	}
	defer packetConn.Close()

	listener, err := config.Listen(ctx, "tcp", addr)
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatalf("Listen(%s) failed: %v", addr, err)
	}
	defer listener.Close()
}

func TestDNSListenConfigBindsNonLocalIPv6Address(t *testing.T) {
	config := dnsListenConfig()
	addr := net.JoinHostPort("2001:db8::248", "0")
	packetConn, err := config.ListenPacket(context.Background(), "udp6", addr)
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatalf("ListenPacket(%s): %v", addr, err)
	}
	defer packetConn.Close()
	listener, err := config.Listen(context.Background(), "tcp6", addr)
	if err != nil {
		skipIfListenNotPermitted(t, err)
		t.Fatalf("Listen(%s): %v", addr, err)
	}
	defer listener.Close()
}
