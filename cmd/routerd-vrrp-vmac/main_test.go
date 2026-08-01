// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
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

func TestRepeatedActivateKeepsReadyVMACLinkAndRAStable(t *testing.T) {
	var commands []string
	requests := 0
	err := runWithHooks([]string{
		"activate", "--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			commands = append(commands, line)
			if line == "ip -6 route show default dev wan-vmac" {
				return []byte("default via fe80::1 dev wan-vmac metric 50\n"), nil
			}
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: true, macMatches: true, hasLinkLocal: true}, nil
		},
		requestRA: func(string) error {
			requests++
			return nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("repeat activate: %v", err)
	}
	for _, forbidden := range []string{
		"ip link add ",
		"ip link set dev wan-vmac address ",
		"ip link set dev wan-vmac up",
		"ip -6 route replace default ",
	} {
		if slicesContainPrefix(commands, forbidden) {
			t.Fatalf("ready VMAC ran disruptive command %q: %#v", forbidden, commands)
		}
	}
	if requests != 0 {
		t.Fatalf("router solicitations = %d, want 0", requests)
	}
	for _, want := range []string{
		"sysctl -w net.ipv6.conf.wan-vmac.keep_addr_on_down=1",
		"sysctl -w net.ipv6.conf.wan-vmac.accept_ra=2",
	} {
		if !slicesContain(commands, want) {
			t.Fatalf("steady-state repair omitted %q: %#v", want, commands)
		}
	}
}

func TestRepeatedActivateRequestsRAOnlyWhenWANStateIsIncomplete(t *testing.T) {
	var commands []string
	requests := 0
	err := runWithHooks([]string{
		"activate", "--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: true, macMatches: true, hasLinkLocal: true}, nil
		},
		requestRA: func(string) error {
			requests++
			return nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("repeat activate without RA state: %v", err)
	}
	if requests != 1 {
		t.Fatalf("router solicitations = %d, want 1", requests)
	}
	if slicesContainPrefix(commands, "ip link set dev wan-vmac address ") || slicesContain(commands, "ip link set dev wan-vmac up") {
		t.Fatalf("missing RA state must not reset a healthy link: %#v", commands)
	}
}

func TestRepeatedActivateRepairsWANDefaultRouteWithoutSolicitingRA(t *testing.T) {
	var commands []string
	requests := 0
	err := runWithHooks([]string{
		"activate", "--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			commands = append(commands, line)
			if line == "ip -6 route show default dev wan-vmac" {
				return []byte("default via fe80::1 dev wan-vmac proto ra metric 1024\n"), nil
			}
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: true, macMatches: true, hasLinkLocal: true}, nil
		},
		requestRA: func(string) error {
			requests++
			return nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("repair WAN default: %v", err)
	}
	want := "ip -6 route replace default via fe80::1 dev wan-vmac metric 50"
	if !slicesContain(commands, want) {
		t.Fatalf("default route repair omitted %q: %#v", want, commands)
	}
	if requests != 0 {
		t.Fatalf("router solicitations = %d, want 0", requests)
	}
}

func TestRepeatedActivateKeepsReadyLANVMACAddressesStable(t *testing.T) {
	var commands []string
	requests := 0
	err := runWithHooks([]string{
		"activate", "--vmac", "lan,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: true, macMatches: true, hasLinkLocal: true, configuredLinkLocalSet: true}, nil
		},
		requestRA: func(string) error {
			requests++
			return nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("repeat LAN activate: %v", err)
	}
	for _, forbidden := range []string{
		"ip link ",
		"ip -6 addr flush ",
		"ip -6 addr replace ",
	} {
		if slicesContainPrefix(commands, forbidden) {
			t.Fatalf("ready LAN VMAC ran disruptive command %q: %#v", forbidden, commands)
		}
	}
	if requests != 0 {
		t.Fatalf("router solicitations = %d, want 0", requests)
	}
}

func TestActivateStillRepairsDownVMAC(t *testing.T) {
	var commands []string
	requests := 0
	err := runWithHooks([]string{
		"activate", "--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			commands = append(commands, line)
			if strings.HasPrefix(line, "ip link add ") {
				return []byte("RTNETLINK answers: File exists\n"), errors.New("exit status 2")
			}
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: false, macMatches: true, hasLinkLocal: true}, nil
		},
		requestRA: func(string) error {
			requests++
			return nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("repair down VMAC: %v", err)
	}
	for _, want := range []string{
		"ip link set dev wan-vmac address 02:00:5e:00:01:13",
		"ip link set dev wan-vmac up",
	} {
		if !slicesContain(commands, want) {
			t.Fatalf("repair omitted %q: %#v", want, commands)
		}
	}
	if requests != 1 {
		t.Fatalf("router solicitations = %d, want 1", requests)
	}
}

func TestRepeatedDeactivateKeepsStagedVMACStable(t *testing.T) {
	var commands []string
	withdrawals := 0
	err := runWithHooks([]string{
		"deactivate", "--vmac", "lan,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: false, macMatches: true}, nil
		},
		withdrawRA: func(string) error {
			withdrawals++
			return nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatalf("repeat deactivate: %v", err)
	}
	if withdrawals != 0 {
		t.Fatalf("RA withdrawals = %d, want 0", withdrawals)
	}
	if slicesContainPrefix(commands, "ip link ") || slicesContainPrefix(commands, "ip -6 addr ") {
		t.Fatalf("staged VMAC ran disruptive command: %#v", commands)
	}
}

func TestDeactivateWithdrawsRAOnlyOnRoleTransition(t *testing.T) {
	var commands []string
	withdrawals := 0
	staged := false
	hooks := runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			commands = append(commands, line)
			if strings.HasPrefix(line, "ip link add ") {
				return []byte("RTNETLINK answers: File exists\n"), errors.New("exit status 2")
			}
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			if staged {
				return vmacRuntimeState{exists: true, up: false, macMatches: true}, nil
			}
			return vmacRuntimeState{exists: true, up: true, macMatches: true, hasLinkLocal: true, configuredLinkLocalSet: true}, nil
		},
		withdrawRA: func(string) error {
			withdrawals++
			return nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	}
	args := []string{"deactivate", "--vmac", "lan,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true"}
	if err := runWithHooks(args, hooks); err != nil {
		t.Fatalf("initial deactivate: %v", err)
	}
	if withdrawals != 1 || !slicesContain(commands, "ip link set dev lan-vrrp down") {
		t.Fatalf("initial transition withdrawals = %d, commands = %#v", withdrawals, commands)
	}
	staged = true
	commands = nil
	if err := runWithHooks(args, hooks); err != nil {
		t.Fatalf("repeat deactivate: %v", err)
	}
	if withdrawals != 1 {
		t.Fatalf("repeat transition withdrawals = %d, want total 1", withdrawals)
	}
	if slicesContainPrefix(commands, "ip link ") || slicesContainPrefix(commands, "ip -6 addr ") {
		t.Fatalf("repeat deactivate changed the staged link: %#v", commands)
	}
}

func TestParseOptionsRejectsInvalidInterface(t *testing.T) {
	if _, err := parseOptions([]string{"activate", "--parent", "eth0", "--interface", "bad name", "--mac", "02:00:5e:00:01:13"}); err == nil {
		t.Fatal("expected invalid interface error")
	}
}

func TestParseOptionsRejectsWithdrawRAAction(t *testing.T) {
	if _, err := parseOptions([]string{"withdraw-ra", "--parent", "eth0"}); err == nil || !strings.Contains(err.Error(), "activate or deactivate") {
		t.Fatalf("withdraw-ra action error = %v", err)
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
	if command, ok := preferVMACDefaultCommand("default via fe80::1 dev wan-vmac metric 50\n", "wan-vmac"); ok || command != nil {
		t.Fatalf("already preferred VMAC default command = %#v, ok = %t", command, ok)
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
		"activate": "master", "deactivate": "backup", "withdraw-ra": "", "unknown": "",
	} {
		if got := conntrackdRoleForAction(action); got != want {
			t.Fatalf("role for %q = %q, want %q", action, got, want)
		}
	}
}

func TestDeactivateSkipsOnlyMissingVMACAndCompletesBackupTransition(t *testing.T) {
	var commands []string
	var conntrackd []string
	var wroteRole string
	err := runWithHooks([]string{
		"deactivate",
		"--vmac", "missing-parent,wan-vmac,02:00:5e:00:01:13,,false",
		"--vmac", "lan-parent,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			commands = append(commands, line)
			if line == "ip link add link missing-parent name wan-vmac type macvlan mode private" {
				return []byte("Cannot find device \"missing-parent\"\n"), errors.New("exit status 1")
			}
			return nil, nil
		},
		withdrawRA: func(ifname string) error {
			if ifname != "lan-parent" {
				t.Fatalf("withdraw RA interface = %q, want lan-parent", ifname)
			}
			return os.ErrNotExist
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return true, nil },
		reconcileConntrackdRole: func(action string) error {
			for _, args := range conntrackdRoleCommands(action) {
				conntrackd = append(conntrackd, strings.Join(args, " "))
			}
			return nil
		},
		writeConntrackdRole: func(action string) error {
			wroteRole = conntrackdRoleForAction(action)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if !slicesContain(commands, "ip link set dev lan-vrrp down") {
		t.Fatalf("later VMAC was not deactivated: %#v", commands)
	}
	if slicesContain(commands, "ip link set dev wan-vmac address 02:00:5e:00:01:13") {
		t.Fatalf("missing VMAC must not run remaining commands: %#v", commands)
	}
	if got, want := conntrackd, []string{"-t", "-n"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("conntrackd commands = %#v, want %#v", got, want)
	}
	if wroteRole != "backup" {
		t.Fatalf("written role = %q, want backup", wroteRole)
	}
}

func TestDeactivateWithdrawRAMissingInterfaceIsNoopButOtherErrorsFail(t *testing.T) {
	opts := []string{"deactivate", "--vmac", "lan-parent,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true"}
	base := runHooks{
		command:                    func(string, ...string) ([]byte, error) { return nil, nil },
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	}

	missing := base
	missing.withdrawRA = func(string) error { return os.ErrNotExist }
	if err := runWithHooks(opts, missing); err != nil {
		t.Fatalf("missing RA interface must be a no-op: %v", err)
	}

	missingNetworkInterface := base
	missingNetworkInterface.withdrawRA = func(string) error { return errors.New("route ip+net: no such network interface") }
	if err := runWithHooks(opts, missingNetworkInterface); err != nil {
		t.Fatalf("missing RA network interface must be a no-op: %v", err)
	}

	denied := base
	denied.withdrawRA = func(string) error { return errors.New("permission denied") }
	if err := runWithHooks(opts, denied); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("withdraw RA error = %v, want permission error", err)
	}
}

func TestDeactivateDoesNotIgnoreMissingExecutable(t *testing.T) {
	err := runWithHooks([]string{
		"deactivate", "--vmac", "wan,wan-vmac,02:00:5e:00:01:13,,false",
	}, runHooks{
		command:                    func(string, ...string) ([]byte, error) { return nil, os.ErrNotExist },
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
	})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing executable error = %v, want os.ErrNotExist", err)
	}
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func slicesContainPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
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
