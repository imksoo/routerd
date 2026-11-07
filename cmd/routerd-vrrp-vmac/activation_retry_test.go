// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"errors"
	"testing"
)

func TestInterruptedMasterActivationRetainsRetryWithoutPublishingAuthority(t *testing.T) {
	role, conntrackRole := "backup", "backup"
	fail := true
	commands := 0
	hooks := runHooks{
		readElectionRole: func(string) (string, error) { return role, nil },
		electionTransitionNeeded: func(_ string, action string) (bool, error) {
			return role != conntrackdRoleForAction(action), nil
		},
		writeElectionRole: func(_ string, next string) error { role = next; return nil },
		conntrackdTransitionNeeded: func(action string) (bool, error) {
			return conntrackRole != conntrackdRoleForAction(action), nil
		},
		reconcileConntrackdRole: func(string) error {
			if fail {
				return errors.New("conntrackd restarted during cache import")
			}
			return nil
		},
		writeConntrackdRole: func(action string) error { conntrackRole = conntrackdRoleForAction(action); return nil },
		command:             func(string, ...string) ([]byte, error) { commands++; return nil, nil },
		requestRA:           func(string) error { return nil },
		notifyRouterd:       func() error { return nil },
	}
	args := []string{"activate", "--resource", "gateway", "--deferred-address", "198.18.30.1/32", "--deferred-interface", "lan", "--parent", "wan", "--interface", "wan-vmac", "--mac", "02:00:5e:00:01:31"}
	if err := runWithHooks(args, hooks); err == nil {
		t.Fatal("failed import reported success")
	}
	if role != "activating" || conntrackRole != "backup" || commands != 0 {
		t.Fatalf("failed import lost retry or exposed identity: role=%q cache=%q commands=%d", role, conntrackRole, commands)
	}
	// An authoritative BACKUP notification cancels the unfinished election.
	// A queued activation repair must not revive it after taking the lock.
	role = "backup"
	fail = false
	if err := runWithHooks(append(args, "--reconcile"), hooks); err != nil {
		t.Fatal(err)
	}
	if role != "backup" || conntrackRole != "backup" || commands != 0 {
		t.Fatal("stale activation undid BACKUP")
	}
	role = "activating"
	fail = false
	if err := runWithHooks(append(args, "--reconcile"), hooks); err != nil {
		t.Fatal(err)
	}
	if role != "master" || conntrackRole != "master" || commands == 0 {
		t.Fatalf("retry did not complete: role=%q cache=%q commands=%d", role, conntrackRole, commands)
	}
}
