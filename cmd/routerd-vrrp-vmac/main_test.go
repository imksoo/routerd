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
	if got := commandsFor(opts); len(got) != 5 || got[0][0] != "ip" || got[0][1] != "link" || got[0][2] != "add" || got[4][0] != "sysctl" || got[4][2] != "net.ipv6.conf.wan-vmac.accept_ra=2" {
		t.Fatalf("activate commands: %#v", got)
	}
}

func TestWANFailoverVMACKeepsDelegatedAddressesAcrossDown(t *testing.T) {
	wan, err := parseOptions([]string{
		"activate", "--vmac", "wan,wan-vmac,02:00:5e:00:01:13",
	})
	if err != nil {
		t.Fatalf("parse WAN VMAC: %v", err)
	}
	want := [][]string{
		{"ip", "link", "add", "link", "wan", "name", "wan-vmac", "type", "macvlan", "mode", "private"},
		{"ip", "link", "set", "dev", "wan-vmac", "address", "02:00:5e:00:01:13"},
		{"sysctl", "-w", "net.ipv6.conf.wan-vmac.keep_addr_on_down=1"},
		{"ip", "link", "set", "dev", "wan-vmac", "up"},
		{"sysctl", "-w", "net.ipv6.conf.wan-vmac.accept_ra=2"},
	}
	if got := commandsFor(wan); !reflect.DeepEqual(got, want) {
		t.Fatalf("WAN commands = %#v, want %#v", got, want)
	}
	wan.action = "deactivate"
	if got, want := commandsFor(wan), [][]string{{"ip", "link", "add", "link", "wan", "name", "wan-vmac", "type", "macvlan", "mode", "private"}, {"ip", "link", "set", "dev", "wan-vmac", "address", "02:00:5e:00:01:13"}, {"sysctl", "-w", "net.ipv6.conf.wan-vmac.keep_addr_on_down=1"}, {"ip", "link", "set", "dev", "wan-vmac", "down"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("WAN deactivate commands = %#v, want %#v", got, want)
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

// The additional LAN VMAC is the client router identity.  It must never keep
// an automatic link-local address alongside the shared address.  A BACKUP
// must remove link-local identity before it goes DOWN, but retain the
// PD-derived global address so the next MASTER reconciliation can use it.
func TestLANFailoverVMACCommandsHaveOneSharedLinkLocalAndCleanBackup(t *testing.T) {
	lan, err := parseOptions([]string{
		"activate", "--vmac", "ens19,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	})
	if err != nil {
		t.Fatalf("parse LAN VMAC: %v", err)
	}
	if lan.vmacs[0].linkLocal != "fe80::5eff:fe00:112" || !lan.vmacs[0].withdraw {
		t.Fatalf("LAN tuple lost link-local or withdraw-ra flag: %#v", lan.vmacs[0])
	}
	commands := commandsFor(lan)
	assertCommandBefore(t, commands,
		[]string{"ip", "link", "set", "dev", "lan-vrrp", "addrgenmode", "none"},
		[]string{"ip", "link", "set", "dev", "lan-vrrp", "up"},
	)
	assertCommandBefore(t, commands,
		[]string{"sysctl", "-w", "net.ipv6.conf.lan-vrrp.keep_addr_on_down=1"},
		[]string{"ip", "link", "set", "dev", "lan-vrrp", "up"},
	)
	assertContainsCommand(t, commands, []string{"ip", "-6", "addr", "flush", "dev", "lan-vrrp", "scope", "link"})
	assertDoesNotContainCommand(t, commands, []string{"ip", "-6", "addr", "flush", "dev", "lan-vrrp"})
	assertContainsCommand(t, commands, []string{"ip", "-6", "addr", "replace", "fe80::5eff:fe00:112/64", "dev", "lan-vrrp", "nodad"})

	lan.action = "deactivate"
	deactivate := commandsFor(lan)
	assertCommandBefore(t, deactivate,
		[]string{"ip", "-6", "addr", "flush", "dev", "lan-vrrp", "scope", "link"},
		[]string{"ip", "link", "set", "dev", "lan-vrrp", "down"},
	)
	assertCommandBefore(t, deactivate,
		[]string{"sysctl", "-w", "net.ipv6.conf.lan-vrrp.keep_addr_on_down=1"},
		[]string{"ip", "link", "set", "dev", "lan-vrrp", "down"},
	)
	assertCommandBefore(t, deactivate,
		[]string{"ip", "-6", "addr", "flush", "dev", "lan-vrrp", "scope", "link"},
		[]string{"sysctl", "-w", "net.ipv6.conf.lan-vrrp.keep_addr_on_down=1"},
	)
	assertDoesNotContainCommand(t, deactivate, []string{"ip", "-6", "addr", "flush", "dev", "lan-vrrp"})
}

func assertDoesNotContainCommand(t *testing.T, commands [][]string, unwanted []string) {
	t.Helper()
	for _, command := range commands {
		if reflect.DeepEqual(command, unwanted) {
			t.Fatalf("commands %#v must not contain %#v", commands, unwanted)
		}
	}
}

func assertContainsCommand(t *testing.T, commands [][]string, want []string) {
	t.Helper()
	for _, command := range commands {
		if reflect.DeepEqual(command, want) {
			return
		}
	}
	t.Fatalf("commands %#v do not contain %#v", commands, want)
}

func assertCommandBefore(t *testing.T, commands [][]string, first, second []string) {
	t.Helper()
	firstIndex, secondIndex := -1, -1
	for index, command := range commands {
		if reflect.DeepEqual(command, first) {
			firstIndex = index
		}
		if reflect.DeepEqual(command, second) {
			secondIndex = index
		}
	}
	if firstIndex < 0 || secondIndex < 0 || firstIndex >= secondIndex {
		t.Fatalf("commands %#v must place %#v before %#v", commands, first, second)
	}
}
