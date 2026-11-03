// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestVMACDefaultPreferenceNativeIPRouteOutput(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires root in an explicitly isolated network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := os.Readlink("/proc/1/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 || current == initial {
		t.Fatal("refuse native mutations outside an isolated root network namespace")
	}
	command := func(name string, args ...string) ([]byte, error) {
		return exec.CommandContext(t.Context(), name, args...).CombinedOutput()
	}
	native := func(name string, args ...string) string {
		out, err := command(name, args...)
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return string(out)
	}
	for _, name := range []string{"rd15-wan", "rd15-mgmt"} {
		native("ip", "link", "add", name, "type", "dummy")
		t.Cleanup(func() { command("ip", "link", "del", name) })
		native("ip", "link", "set", name, "up")
	}
	native("ip", "link", "add", "rd15-vmac", "link", "rd15-wan", "type", "macvlan", "mode", "private")
	native("ip", "link", "set", "rd15-vmac", "address", "02:00:5e:00:01:31", "up")
	native("ip", "-6", "addr", "add", "fe80::131/64", "dev", "rd15-vmac", "nodad")
	native("ip", "-6", "addr", "add", "2001:db8:25::131/64", "dev", "rd15-vmac", "nodad")
	native("ip", "-6", "addr", "add", "fe80::2/64", "dev", "rd15-mgmt", "nodad")
	native("ip", "-6", "route", "add", "default", "via", "fe80::1", "dev", "rd15-mgmt", "proto", "ra", "metric", "100", "expires", "90")
	native("ip", "-6", "route", "add", "default", "via", "fe80::1", "dev", "rd15-vmac", "proto", "ra", "metric", "1024", "expires", "90")
	before := native("ip", "-6", "route", "get", "2001:db8:ffff::80")
	if !strings.Contains(before, "dev rd15-mgmt") {
		t.Fatalf("missing competing management default: %s", before)
	}
	solicitations := 0
	hooks := runHooks{
		command:                    command,
		inspectVMAC:                inspectVMACRuntimeState,
		requestRA:                  func(string) error { solicitations++; return nil },
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	}
	for range 2 {
		if err := runWithHooks([]string{"activate", "--vmac", "rd15-wan,rd15-vmac,02:00:5e:00:01:31,fe80::131,false"}, hooks); err != nil {
			t.Fatal(err)
		}
		after := native("ip", "-6", "route", "get", "2001:db8:ffff::80")
		if !strings.Contains(after, "dev rd15-vmac") || !strings.Contains(after, "src 2001:db8:25::131") {
			t.Fatalf("usable VMAC RA default did not win over management: %s", after)
		}
	}
	if solicitations != 0 {
		t.Fatalf("existing usable RA state triggered %d unnecessary solicitations", solicitations)
	}
	routes := native("ip", "-6", "route", "show", "default")
	if !strings.Contains(routes, "dev rd15-mgmt") || !strings.Contains(routes, "metric 50") || !strings.Contains(routes, "expires") {
		t.Fatalf("promotion removed foreign state or lost finite RA authority: %s", routes)
	}
}
