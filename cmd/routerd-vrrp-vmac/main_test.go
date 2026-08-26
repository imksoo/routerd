// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
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
	if got := commandsFor(opts); len(got) != 7 || got[0][0] != "ip" || got[0][1] != "link" || got[0][2] != "add" || got[4][0] != "sysctl" || got[4][2] != "net.ipv6.conf.wan-vmac.accept_ra=2" || got[6][0] != "ip" || got[6][4] != "wan-vmac" {
		t.Fatalf("activate commands: %#v", got)
	}
}

func TestNotifyRouterdVRRPTransitionSignalsMainProcess(t *testing.T) {
	var got []string
	err := notifyRouterdVRRPTransition(func(name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("notify routerd: %v", err)
	}
	want := []string{"systemctl", "kill", "--kill-whom=main", "--signal=USR1", "routerd.service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestGracefulDeactivateWithdrawsVIPBeforeVMACAndConntrack(t *testing.T) {
	var events []string
	err := runWithHooks([]string{
		"deactivate",
		"--resource", "lan-gw-v4",
		"--deferred-address", "172.18.0.1/32",
		"--deferred-interface", "ens19",
		"--vmac", "lan,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			events = append(events, "command "+name+" "+strings.Join(args, " "))
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: true, macMatches: true, hasLinkLocal: true, configuredLinkLocalSet: true}, nil
		},
		withdrawRA:                 func(string) error { events = append(events, "withdraw-ra"); return nil },
		conntrackdTransitionNeeded: func(string) (bool, error) { return true, nil },
		reconcileConntrackdRole:    func(string) error { events = append(events, "conntrack-backup"); return nil },
		writeConntrackdRole:        func(string) error { return nil },
		electionTransitionNeeded: func(string, string) (bool, error) {
			return true, nil
		},
		writeElectionRole:   func(string, string) error { events = append(events, "election-backup"); return nil },
		withdrawDeferredVIP: func(options) error { events = append(events, "withdraw-vip"); return nil },
		notifyRouterd:       func() error { events = append(events, "notify-routerd"); return nil },
	})
	if err != nil {
		t.Fatalf("graceful deactivate: %v", err)
	}
	wantPrefix := []string{"withdraw-vip", "election-backup", "notify-routerd", "withdraw-ra"}
	if len(events) < len(wantPrefix) || !reflect.DeepEqual(events[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("events = %#v, want prefix %#v", events, wantPrefix)
	}
	conntrack := -1
	for index, event := range events {
		if event == "conntrack-backup" {
			conntrack = index
			break
		}
	}
	if conntrack < 0 || conntrack <= len(wantPrefix) {
		t.Fatalf("conntrack demotion must follow VIP/RA/VMAC withdrawal: %#v", events)
	}
}

func TestVMACActionOrderRaisesWANFirstAndLowersLANFirst(t *testing.T) {
	args := []string{
		"--vmac", "wan,wan-vmac,02:00:5e:00:01:13,fe80::5eff:fe00:113,false",
		"--vmac", "lan,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	}
	for _, test := range []struct {
		action string
		want   []string
	}{
		{action: "activate", want: []string{"wan-vmac", "lan-vrrp"}},
		{action: "deactivate", want: []string{"lan-vrrp", "wan-vmac"}},
	} {
		t.Run(test.action, func(t *testing.T) {
			var downOrUp []string
			linkState := "down"
			if test.action == "activate" {
				linkState = "up"
			}
			err := runWithHooks(append([]string{test.action}, args...), runHooks{
				command: func(name string, commandArgs ...string) ([]byte, error) {
					if name == "ip" && len(commandArgs) == 5 && commandArgs[0] == "link" && commandArgs[1] == "set" && commandArgs[2] == "dev" && commandArgs[4] == linkState {
						downOrUp = append(downOrUp, commandArgs[3])
					}
					return nil, nil
				},
				inspectVMAC: func(vmac) (vmacRuntimeState, error) { return vmacRuntimeState{}, nil },
				withdrawRA:  func(string) error { return nil },
				requestRA:   func(string) error { return nil },
				conntrackdTransitionNeeded: func(string) (bool, error) {
					return false, nil
				},
			})
			if err != nil {
				t.Fatalf("%s VMACs: %v", test.action, err)
			}
			if !reflect.DeepEqual(downOrUp, test.want) {
				t.Fatalf("%s VMAC order = %#v, want %#v", test.action, downOrUp, test.want)
			}
		})
	}
}

func TestGracefulActivatePublishesElectionOnlyAfterVMACAndRA(t *testing.T) {
	var events []string
	err := runWithHooks([]string{
		"activate",
		"--resource", "lan-gw-v4",
		"--deferred-address", "172.18.0.1/32",
		"--deferred-interface", "ens19",
		"--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			events = append(events, "command "+name+" "+strings.Join(args, " "))
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) { return vmacRuntimeState{}, nil },
		requestRA:   func(string) error { events = append(events, "request-ra"); return nil },
		conntrackdTransitionNeeded: func(string) (bool, error) {
			return true, nil
		},
		reconcileConntrackdRole: func(string) error { events = append(events, "conntrack-master"); return nil },
		writeConntrackdRole:     func(string) error { return nil },
		electionTransitionNeeded: func(string, string) (bool, error) {
			return true, nil
		},
		writeElectionRole:   func(string, string) error { events = append(events, "election-master"); return nil },
		withdrawDeferredVIP: func(options) error { return nil },
		notifyRouterd:       func() error { events = append(events, "notify-routerd"); return nil },
	})
	if err != nil {
		t.Fatalf("graceful activate: %v", err)
	}
	position := func(want string) int {
		for index, event := range events {
			if event == want {
				return index
			}
		}
		return -1
	}
	if position("conntrack-master") < 0 || position("request-ra") < position("conntrack-master") || position("election-master") < position("request-ra") || position("notify-routerd") < position("election-master") {
		t.Fatalf("unexpected activation order: %#v", events)
	}
}

func TestGracefulDeactivateDeleteFailureStillWakesBackupReconcile(t *testing.T) {
	var events []string
	err := runWithHooks([]string{
		"deactivate", "--resource", "lan-gw-v4", "--deferred-address", "172.18.0.1/32", "--deferred-interface", "ens19",
		"--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}, runHooks{
		command:                    func(string, ...string) ([]byte, error) { return nil, nil },
		inspectVMAC:                func(vmac) (vmacRuntimeState, error) { return vmacRuntimeState{}, nil },
		conntrackdTransitionNeeded: func(string) (bool, error) { return true, nil },
		reconcileConntrackdRole: func(string) error {
			events = append(events, "conntrack-backup")
			return nil
		},
		electionTransitionNeeded: func(string, string) (bool, error) { return true, nil },
		writeElectionRole: func(string, string) error {
			events = append(events, "election-backup")
			return nil
		},
		withdrawDeferredVIP: func(options) error {
			events = append(events, "withdraw-vip")
			return errors.New("netlink failure")
		},
		notifyRouterd: func() error { events = append(events, "notify-routerd"); return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "netlink failure") {
		t.Fatalf("error = %v, want delete failure", err)
	}
	want := []string{"withdraw-vip", "election-backup", "notify-routerd"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v; conntrack must remain primary until withdrawal retries", events, want)
	}
}

func TestGracefulSteadyBackupReconcileDoesNotSignalItself(t *testing.T) {
	notifications := 0
	err := runWithHooks([]string{
		"deactivate", "--resource", "lan-gw-v4", "--deferred-address", "172.18.0.1/32", "--deferred-interface", "ens19", "--reconcile",
	}, runHooks{
		command:                    func(string, ...string) ([]byte, error) { return nil, nil },
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
		electionTransitionNeeded:   func(string, string) (bool, error) { return false, nil },
		writeElectionRole:          func(string, string) error { return nil },
		withdrawDeferredVIP:        func(options) error { return nil },
		notifyRouterd: func() error {
			notifications++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("steady backup reconcile: %v", err)
	}
	if notifications != 0 {
		t.Fatalf("routerd reconciliation signalled itself %d times", notifications)
	}
}

func TestStagingReconcileGuardDiscardsDeactivateAfterMasterElection(t *testing.T) {
	locked := false
	unlocked := false
	commands := 0
	err := runWithHooks([]string{
		"deactivate",
		"--guard-resource", "lan-gw-v4",
		"--reconcile",
		"--vmac", "lan,lan-vrrp,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	}, runHooks{
		lockElection: func(resource string) (func() error, error) {
			if resource != "lan-gw-v4" {
				t.Fatalf("lock resource = %q", resource)
			}
			locked = true
			return func() error { unlocked = true; return nil }, nil
		},
		readElectionRole: func(resource string) (string, error) {
			return "master", nil
		},
		command: func(string, ...string) ([]byte, error) {
			commands++
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("guarded staging reconcile: %v", err)
	}
	if !locked || !unlocked {
		t.Fatalf("election lock lifecycle locked=%t unlocked=%t", locked, unlocked)
	}
	if commands != 0 {
		t.Fatalf("stale staging reconcile ran %d VMAC commands", commands)
	}
}

func TestGracefulReconcileDiscardsRoleObservedBeforeTransitionLock(t *testing.T) {
	var transitionLock sync.Mutex
	role := "backup"
	activationStarted := make(chan struct{})
	continueActivation := make(chan struct{})
	deactivateCalls := 0
	hooks := runHooks{
		lockElection: func(string) (func() error, error) {
			transitionLock.Lock()
			return func() error { transitionLock.Unlock(); return nil }, nil
		},
		readElectionRole: func(string) (string, error) { return role, nil },
		command: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			switch line {
			case "ip -6 route show default dev wan-vmac":
				return []byte("default via fe80::1 dev wan-vmac metric 50 src 2001:db8::13\n"), nil
			case "ip -6 -o addr show dev wan-vmac scope global":
				return []byte("7: wan-vmac inet6 2001:db8::13/64 scope global dynamic\n"), nil
			default:
				return nil, nil
			}
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: true, macMatches: true, hasLinkLocal: true}, nil
		},
		conntrackdTransitionNeeded: func(action string) (bool, error) {
			return action == "activate", nil
		},
		reconcileConntrackdRole: func(action string) error {
			if action == "activate" {
				close(activationStarted)
				<-continueActivation
			} else {
				deactivateCalls++
			}
			return nil
		},
		writeConntrackdRole: func(string) error { return nil },
		electionTransitionNeeded: func(_ string, action string) (bool, error) {
			return role != conntrackdRoleForAction(action), nil
		},
		writeElectionRole: func(_ string, action string) error {
			role = conntrackdRoleForAction(action)
			return nil
		},
		withdrawDeferredVIP: func(options) error { return nil },
		notifyRouterd:       func() error { return nil },
	}
	args := []string{
		"activate", "--resource", "lan-gw-v4", "--deferred-address", "172.18.0.1/32", "--deferred-interface", "ens19",
		"--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}
	activateResult := make(chan error, 1)
	go func() { activateResult <- runWithHooks(args, hooks) }()
	<-activationStarted

	reconcileResult := make(chan error, 1)
	staleArgs := append([]string{"deactivate"}, args[1:]...)
	staleArgs = append(staleArgs, "--reconcile")
	go func() { reconcileResult <- runWithHooks(staleArgs, hooks) }()
	close(continueActivation)

	if err := <-activateResult; err != nil {
		t.Fatalf("activate notification: %v", err)
	}
	if err := <-reconcileResult; err != nil {
		t.Fatalf("stale backup reconcile: %v", err)
	}
	if role != "master" {
		t.Fatalf("election role = %q, want master", role)
	}
	if deactivateCalls != 0 {
		t.Fatalf("stale reconcile performed %d deactivations", deactivateCalls)
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
		{"sysctl", "-w", "net.ipv4.conf.wan.arp_ignore=1"},
		{"sysctl", "-w", "net.ipv4.conf.wan-vmac.arp_ignore=1"},
		{"sysctl", "-w", "net.ipv6.conf.wan-vmac.accept_ra=2"},
		{"sysctl", "-w", "net.ipv6.conf.wan-vmac.keep_addr_on_down=1"},
		{"ip", "link", "set", "dev", "wan-vmac", "up"},
	}
	if got := commandsFor(wan); !reflect.DeepEqual(got, want) {
		t.Fatalf("WAN commands = %#v, want %#v", got, want)
	}
	wan.action = "deactivate"
	if got, want := commandsFor(wan), [][]string{{"ip", "link", "add", "link", "wan", "name", "wan-vmac", "type", "macvlan", "mode", "private"}, {"ip", "link", "set", "dev", "wan-vmac", "address", "02:00:5e:00:01:13"}, {"sysctl", "-w", "net.ipv4.conf.wan.arp_ignore=1"}, {"sysctl", "-w", "net.ipv4.conf.wan-vmac.arp_ignore=1"}, {"sysctl", "-w", "net.ipv6.conf.wan-vmac.accept_ra=2"}, {"sysctl", "-w", "net.ipv6.conf.wan-vmac.keep_addr_on_down=1"}, {"ip", "link", "set", "dev", "wan-vmac", "down"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("WAN deactivate commands = %#v, want %#v", got, want)
	}
}

func TestRunVMACCommandsReappliesHygieneWhenLinkAlreadyExists(t *testing.T) {
	commands := [][]string{
		{"ip", "link", "add", "link", "wan", "name", "wan-vmac", "type", "macvlan", "mode", "private"},
		{"sysctl", "-w", "net.ipv4.conf.wan.arp_ignore=1"},
		{"sysctl", "-w", "net.ipv4.conf.wan-vmac.arp_ignore=1"},
	}
	var executed []string
	err := runVMACCommands("activate", commands, func(name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		executed = append(executed, line)
		if strings.HasPrefix(line, "ip link add ") {
			return []byte("RTNETLINK answers: File exists\n"), errors.New("exit status 2")
		}
		return nil, nil
	})
	if err != nil {
		t.Fatalf("repeat VMAC activation: %v", err)
	}
	want := []string{
		"ip link add link wan name wan-vmac type macvlan mode private",
		"sysctl -w net.ipv4.conf.wan.arp_ignore=1",
		"sysctl -w net.ipv4.conf.wan-vmac.arp_ignore=1",
	}
	if !reflect.DeepEqual(executed, want) {
		t.Fatalf("executed = %#v, want %#v", executed, want)
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
			switch line {
			case "ip -6 route show default dev wan-vmac":
				return []byte("default via fe80::1 dev wan-vmac metric 50 src 2001:db8:1200::13\n"), nil
			case "ip -6 -o addr show dev wan-vmac scope global":
				return []byte("7: wan-vmac inet6 2001:db8:1200::13/64 scope global dynamic valid_lft 100sec preferred_lft 50sec\n"), nil
			default:
				return nil, nil
			}
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
		"sysctl -w net.ipv4.conf.wan.arp_ignore=1",
		"sysctl -w net.ipv4.conf.wan-vmac.arp_ignore=1",
		"sysctl -w net.ipv6.conf.wan-vmac.accept_ra=2",
		"sysctl -w net.ipv6.conf.wan-vmac.keep_addr_on_down=1",
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
			switch line {
			case "ip -6 route show default dev wan-vmac":
				return []byte("default via fe80::1 dev wan-vmac proto ra metric 1024\n"), nil
			case "ip -6 -o addr show dev wan-vmac scope global":
				return []byte("7: wan-vmac inet6 2001:db8:1200::13/64 scope global dynamic valid_lft 100sec preferred_lft 50sec\n"), nil
			default:
				return nil, nil
			}
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
	want := "ip -6 route replace default via fe80::1 dev wan-vmac metric 50 src 2001:db8:1200::13"
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

func TestMixedVMACHygieneDoesNotLeakLANPolicyToWAN(t *testing.T) {
	opts, err := parseOptions([]string{
		"activate",
		"--vmac", "wan,wan-vmac,02:00:5e:00:01:13,,false",
		"--vmac", "lan,lan-vmac,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	})
	if err != nil {
		t.Fatalf("parse mixed VMACs: %v", err)
	}
	groups := commandsForVMACs(opts)
	if len(groups) != 2 {
		t.Fatalf("VMAC command groups = %d, want 2", len(groups))
	}
	assertContainsCommand(t, groups[0], []string{"sysctl", "-w", "net.ipv6.conf.wan-vmac.accept_ra=2"})
	assertDoesNotContainCommand(t, groups[0], []string{"sysctl", "-w", "net.ipv6.conf.wan.accept_ra=0"})
	assertDoesNotContainCommand(t, groups[0], []string{"ip", "-6", "addr", "flush", "dev", "wan-vmac", "scope", "global", "dynamic"})
	assertContainsCommand(t, groups[1], []string{"sysctl", "-w", "net.ipv6.conf.lan.accept_ra=0"})
	assertContainsCommand(t, groups[1], []string{"sysctl", "-w", "net.ipv6.conf.lan-vmac.accept_ra=0"})
	assertContainsCommand(t, groups[1], []string{"ip", "-6", "addr", "flush", "dev", "lan", "scope", "global", "dynamic"})
}

func TestActivateStopsBeforeLinkUpWhenParentHygieneFails(t *testing.T) {
	var commands []string
	var wroteRole bool
	err := runWithHooks([]string{
		"activate", "--vmac", "lan,lan-vmac,02:00:5e:00:01:12,fe80::5eff:fe00:112,true",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			commands = append(commands, line)
			if line == "sysctl -w net.ipv4.conf.lan.arp_ignore=1" {
				return []byte("permission denied\n"), errors.New("exit status 1")
			}
			return nil, nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return true, nil },
		reconcileConntrackdRole:    func(string) error { return nil },
		writeConntrackdRole: func(string) error {
			wroteRole = true
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "net.ipv4.conf.lan.arp_ignore=1") {
		t.Fatalf("parent hygiene error = %v", err)
	}
	if slicesContain(commands, "ip link set dev lan-vmac up") {
		t.Fatalf("LAN VMAC was raised after hygiene failure: %#v", commands)
	}
	if wroteRole {
		t.Fatal("master role was written after hygiene failure")
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
	command, ok := preferVMACDefaultCommand("default via fe80::1 dev wan-vmac proto ra metric 1024\n", "wan-vmac", "2001:db8:1200::13")
	if !ok {
		t.Fatal("expected VMAC default route")
	}
	want := []string{"ip", "-6", "route", "replace", "default", "via", "fe80::1", "dev", "wan-vmac", "metric", "50", "src", "2001:db8:1200::13"}
	if len(command) != len(want) {
		t.Fatalf("command = %#v", command)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %#v, want %#v", command, want)
		}
	}
	if _, ok := preferVMACDefaultCommand("default via fe80::1 dev eth0 proto ra\n", "wan-vmac", "2001:db8:1200::13"); ok {
		t.Fatal("physical route must not be selected")
	}
	if _, ok := preferVMACDefaultCommand("default via fe80::1 dev wan-vmac proto ra\n", "wan-vmac", ""); ok {
		t.Fatal("VMAC default must wait for an eligible RA source")
	}
	if command, ok := preferVMACDefaultCommand("default via fe80::1 dev wan-vmac metric 50 src 2001:db8:1200::13\n", "wan-vmac", "2001:db8:1200::13"); ok || command != nil {
		t.Fatalf("already preferred VMAC default command = %#v, ok = %t", command, ok)
	}
}

func TestPreferredVMACGlobalAddressExcludesTunnelAndUnreadyAddresses(t *testing.T) {
	output := "" +
		"7: wan-vmac inet6 2001:db8:1221::23/128 scope global deprecated valid_lft forever preferred_lft 0sec\n" +
		"7: wan-vmac inet6 2001:db8:1200::12/64 scope global tentative valid_lft forever preferred_lft forever\n" +
		"7: wan-vmac inet6 2001:db8:1200::13/64 scope global dynamic valid_lft 100sec preferred_lft 50sec\n"
	if got, want := preferredVMACGlobalAddress(output), "2001:db8:1200::13"; got != want {
		t.Fatalf("preferred VMAC global address = %q, want %q", got, want)
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

func TestRoleTransitionNotifiesRouterdOnlyAfterDurableRoleWrite(t *testing.T) {
	var steps []string
	err := runWithHooks([]string{
		"activate", "--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}, runHooks{
		command: func(name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			if line == "ip -6 route show default dev wan-vmac" {
				return []byte("default via fe80::1 dev wan-vmac metric 50 src 2001:db8::13\n"), nil
			}
			if line == "ip -6 -o addr show dev wan-vmac scope global" {
				return []byte("7: wan-vmac inet6 2001:db8::13/64 scope global dynamic valid_lft 100sec preferred_lft 50sec\n"), nil
			}
			return nil, nil
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) {
			return vmacRuntimeState{exists: true, up: true, macMatches: true, hasLinkLocal: true}, nil
		},
		conntrackdTransitionNeeded: func(string) (bool, error) { return true, nil },
		reconcileConntrackdRole: func(string) error {
			steps = append(steps, "conntrackd")
			return nil
		},
		writeConntrackdRole: func(string) error {
			steps = append(steps, "role-written")
			return nil
		},
		notifyRouterd: func() error {
			steps = append(steps, "routerd-notified")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"conntrackd", "role-written", "routerd-notified"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("transition steps = %#v, want %#v", steps, want)
	}
}

func TestSteadyRoleDoesNotNotifyRouterd(t *testing.T) {
	notifications := 0
	err := runWithHooks([]string{
		"deactivate", "--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:13",
	}, runHooks{
		command:                    func(string, ...string) ([]byte, error) { return nil, nil },
		inspectVMAC:                func(vmac) (vmacRuntimeState, error) { return vmacRuntimeState{exists: true, macMatches: true}, nil },
		conntrackdTransitionNeeded: func(string) (bool, error) { return false, nil },
		notifyRouterd: func() error {
			notifications++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if notifications != 0 {
		t.Fatalf("steady role notifications = %d, want 0", notifications)
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
	assertContainsCommand(t, commands, []string{"sysctl", "-w", "net.ipv4.conf.ens19.arp_ignore=1"})
	assertContainsCommand(t, commands, []string{"sysctl", "-w", "net.ipv4.conf.lan-vrrp.arp_ignore=1"})
	assertContainsCommand(t, commands, []string{"sysctl", "-w", "net.ipv6.conf.ens19.accept_ra=0"})
	assertContainsCommand(t, commands, []string{"sysctl", "-w", "net.ipv6.conf.lan-vrrp.accept_ra=0"})
	assertCommandBefore(t, commands,
		[]string{"sysctl", "-w", "net.ipv6.conf.lan-vrrp.accept_ra=0"},
		[]string{"ip", "link", "set", "dev", "lan-vrrp", "up"},
	)
	assertContainsCommand(t, commands, []string{"ip", "-6", "addr", "flush", "dev", "lan-vrrp", "scope", "global", "dynamic"})
	assertContainsCommand(t, commands, []string{"ip", "-6", "addr", "flush", "dev", "ens19", "scope", "global", "dynamic"})
	assertDoesNotContainCommand(t, commands, []string{"ip", "-6", "addr", "flush", "dev", "lan-vrrp", "scope", "global"})
	assertDoesNotContainCommand(t, commands, []string{"ip", "-6", "addr", "flush", "dev", "ens19", "scope", "global"})
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
	assertContainsCommand(t, deactivate, []string{"sysctl", "-w", "net.ipv4.conf.ens19.arp_ignore=1"})
	assertContainsCommand(t, deactivate, []string{"sysctl", "-w", "net.ipv4.conf.lan-vrrp.arp_ignore=1"})
	assertContainsCommand(t, deactivate, []string{"sysctl", "-w", "net.ipv6.conf.ens19.accept_ra=0"})
	assertContainsCommand(t, deactivate, []string{"sysctl", "-w", "net.ipv6.conf.lan-vrrp.accept_ra=0"})
	assertCommandBefore(t, deactivate,
		[]string{"sysctl", "-w", "net.ipv4.conf.ens19.arp_ignore=1"},
		[]string{"ip", "link", "set", "dev", "lan-vrrp", "down"},
	)
	assertCommandBefore(t, deactivate,
		[]string{"sysctl", "-w", "net.ipv6.conf.lan-vrrp.accept_ra=0"},
		[]string{"ip", "link", "set", "dev", "lan-vrrp", "down"},
	)
	assertDoesNotContainCommand(t, deactivate, []string{"ip", "-6", "addr", "flush", "dev", "lan-vrrp", "scope", "global", "dynamic"})
	assertDoesNotContainCommand(t, deactivate, []string{"ip", "-6", "addr", "flush", "dev", "ens19", "scope", "global", "dynamic"})
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
