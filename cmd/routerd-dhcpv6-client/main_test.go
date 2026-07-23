// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreLeaseIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatalf("write lease: %v", err)
	}
	daemon := &dhcpv6Daemon{opts: options{leaseFile: path}}
	if err := daemon.restoreLease(context.Background()); err != nil {
		t.Fatalf("restore empty lease: %v", err)
	}
}

func TestSelectLinkLocalIPv6(t *testing.T) {
	got, err := selectLinkLocalIPv6([]net.Addr{
		&net.IPNet{IP: net.ParseIP("2001:db8::10"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("fe80::10"), Mask: net.CIDRMask(64, 128)},
	})
	if err != nil {
		t.Fatalf("select link-local: %v", err)
	}
	if got != "fe80::10" {
		t.Fatalf("link-local = %q, want fe80::10", got)
	}
}

func TestSelectLinkLocalIPv6RequiresLinkLocalAddress(t *testing.T) {
	_, err := selectLinkLocalIPv6([]net.Addr{
		&net.IPNet{IP: net.ParseIP("2001:db8::10"), Mask: net.CIDRMask(64, 128)},
	})
	if err == nil || !strings.Contains(err.Error(), "link-local") {
		t.Fatalf("error = %v, want missing link-local", err)
	}
}

func TestDHCPv6ListenAddressesAreInterfaceScoped(t *testing.T) {
	first, err := dhcpv6ListenAddr("fe80::10", "wan0", 546)
	if err != nil {
		t.Fatalf("first listen address: %v", err)
	}
	second, err := dhcpv6ListenAddr("fe80::20", "wan1", 546)
	if err != nil {
		t.Fatalf("second listen address: %v", err)
	}
	if first.IP.IsUnspecified() || second.IP.IsUnspecified() {
		t.Fatalf("DHCPv6 client bind must not be wildcard: first=%v second=%v", first, second)
	}
	if first.Zone != "wan0" || second.Zone != "wan1" || first.IP.Equal(second.IP) {
		t.Fatalf("scoped addresses = %v, %v", first, second)
	}
}
