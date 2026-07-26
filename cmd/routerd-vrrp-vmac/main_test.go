// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"reflect"
	"testing"
)

func TestParseOptions(t *testing.T) {
	opts, err := parseOptions([]string{"activate", "--parent", "eth0", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13"})
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if opts.action != "activate" || opts.parent != "eth0" || opts.ifname != "wan-vmac" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if got := commandsFor(opts); len(got) != 4 || got[0][0] != "ip" || got[0][1] != "link" || got[0][2] != "add" || got[3][0] != "sysctl" || got[3][2] != "net.ipv6.conf.wan-vmac.accept_ra=2" {
		t.Fatalf("activate commands: %#v", got)
	}
}

func TestParseOptionsRejectsInvalidInterface(t *testing.T) {
	if _, err := parseOptions([]string{"activate", "--parent", "eth0", "--interface", "bad name", "--mac", "02:00:5e:00:01:13"}); err == nil {
		t.Fatal("expected invalid interface error")
	}
}

func TestPreferVMACDefaultCommand(t *testing.T) {
	command, ok := preferVMACDefaultCommand("default via fe80::1 dev wan-vmac proto ra metric 1024\n", "wan-vmac")
	if !ok {
		t.Fatal("expected VMAC default route")
	}
	want := []string{"ip", "-6", "route", "replace", "default", "via", "fe80::1", "dev", "wan-vmac", "metric", "50"}
	if len(command) != len(want) {
		t.Fatalf("command = %#v", command)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %#v, want %#v", command, want)
		}
	}
	if _, ok := preferVMACDefaultCommand("default via fe80::1 dev eth0 proto ra\n", "wan-vmac"); ok {
		t.Fatal("physical route must not be selected")
	}
}

func TestConntrackdRoleCommandsFollowFTFWPrimaryBackupSequence(t *testing.T) {
	if got, want := conntrackdRoleCommands("activate"), [][]string{{"-c"}, {"-R"}, {"-B"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("activate commands = %#v, want %#v", got, want)
	}
	if got, want := conntrackdRoleCommands("deactivate"), [][]string{{"-t"}, {"-n"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deactivate commands = %#v, want %#v", got, want)
	}
	if got := conntrackdRoleCommands("unknown"); got != nil {
		t.Fatalf("unknown action commands = %#v", got)
	}
}

func TestConntrackdRoleForAction(t *testing.T) {
	for action, want := range map[string]string{
		"activate": "master", "deactivate": "backup", "withdraw-ra": "backup", "unknown": "",
	} {
		if got := conntrackdRoleForAction(action); got != want {
			t.Fatalf("role for %q = %q, want %q", action, got, want)
		}
	}
}
