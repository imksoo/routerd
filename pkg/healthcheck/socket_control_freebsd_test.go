// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package healthcheck

import (
	"net"
	"testing"
)

func TestBindDialerToDeviceFreeBSDInvalidInterface(t *testing.T) {
	var dialer net.Dialer
	err := bindDialerToDevice(&dialer, "routerd-no-such-interface", "tcp4", "127.0.0.1:443", "", false)
	if err == nil {
		t.Fatal("expected invalid interface error")
	}
}

func TestSocketAddressFamilyFreeBSD(t *testing.T) {
	tests := []struct {
		name          string
		network       string
		address       string
		addressFamily string
		want          int
	}{
		{name: "network tcp6", network: "tcp6", address: "example.com:443", want: 6},
		{name: "network udp4", network: "udp4", address: "example.com:53", want: 4},
		{name: "literal ipv6", network: "tcp", address: "[2001:db8::1]:443", want: 6},
		{name: "literal ipv4", network: "tcp", address: "192.0.2.1:443", want: 4},
		{name: "address family wins", network: "tcp", address: "example.com:443", addressFamily: "ipv6", want: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := socketAddressFamily(tt.network, tt.address, tt.addressFamily)
			if got != tt.want {
				t.Fatalf("socketAddressFamily() = %d, want %d", got, tt.want)
			}
		})
	}
}
