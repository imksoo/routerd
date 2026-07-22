// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestRejectFreeBSDNonLocalBindIsActionable(t *testing.T) {
	previous := mustInterfaceAddrs
	mustInterfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("192.0.2.1"), Mask: net.CIDRMask(24, 32)}}, nil
	}
	t.Cleanup(func() { mustInterfaceAddrs = previous })

	err := rejectFreeBSDNonLocalBind("udp", "192.0.2.99:53", syscall.RawConn(nil))
	if err == nil || !strings.Contains(err.Error(), "assign listener address 192.0.2.99 before starting the resolver") {
		t.Fatalf("rejectFreeBSDNonLocalBind() error = %v, want actionable assigned-address boundary", err)
	}
}
