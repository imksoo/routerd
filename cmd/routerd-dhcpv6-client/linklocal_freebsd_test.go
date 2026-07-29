// SPDX-License-Identifier: BSD-3-Clause
//go:build freebsd

package main

import (
	"net"
	"testing"
)

func TestEnsureInterfaceLinkLocalIPv6WaitsForExistingTentativeAddress(t *testing.T) {
	oldRun := runIfconfig
	t.Cleanup(func() { runIfconfig = oldRun })
	address := "fe80::5eff:fe00:113"
	calls := 0
	aliasCalls := 0
	runIfconfig = func(args ...string) ([]byte, error) {
		calls++
		if len(args) > 1 {
			aliasCalls++
		}
		if calls == 1 {
			return []byte("\tinet6 " + address + "%em0 prefixlen 64 scopeid 0x1 <link> tentative\n"), nil
		}
		return []byte("\tinet6 " + address + "%em0 prefixlen 64 scopeid 0x1 <link>\n"), nil
	}
	ifi := &net.Interface{Name: "em0", HardwareAddr: net.HardwareAddr{0x02, 0x00, 0x5e, 0x00, 0x01, 0x13}}
	got, err := ensureInterfaceLinkLocalIPv6(ifi)
	if err != nil || got != address {
		t.Fatalf("ensureInterfaceLinkLocalIPv6() = %q, %v", got, err)
	}
	if aliasCalls != 0 {
		t.Fatalf("alias calls = %d, want 0 for an existing tentative address", aliasCalls)
	}
}

func TestEnsureInterfaceLinkLocalIPv6AddsAbsentAddressOnce(t *testing.T) {
	oldRun := runIfconfig
	t.Cleanup(func() { runIfconfig = oldRun })
	address := "fe80::5eff:fe00:113"
	aliasCalls := 0
	ready := false
	runIfconfig = func(args ...string) ([]byte, error) {
		if len(args) > 1 {
			aliasCalls++
			ready = true
			return nil, nil
		}
		if ready {
			return []byte("\tinet6 " + address + "%em0 prefixlen 64 scopeid 0x1 <link>\n"), nil
		}
		return []byte(""), nil
	}
	ifi := &net.Interface{Name: "em0", HardwareAddr: net.HardwareAddr{0x02, 0x00, 0x5e, 0x00, 0x01, 0x13}}
	if _, err := ensureInterfaceLinkLocalIPv6(ifi); err != nil {
		t.Fatalf("ensureInterfaceLinkLocalIPv6(): %v", err)
	}
	if aliasCalls != 1 {
		t.Fatalf("alias calls = %d, want 1 for an absent address", aliasCalls)
	}
}
