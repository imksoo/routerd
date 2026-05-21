// SPDX-License-Identifier: BSD-3-Clause

package bgp

import "testing"

func TestDiffReportsPeerAndPrefixChanges(t *testing.T) {
	state := Normalize(State{
		Peers:    []Peer{{Address: "10.0.0.21", State: "Established"}},
		Prefixes: []Prefix{{Prefix: "10.0.0.200/32", Best: true, Valid: true, Installed: true}},
	})
	events := Diff(State{}, state)
	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	state2 := State{Peers: []Peer{{Address: "10.0.0.21", State: "Idle"}}}
	events = Diff(state, state2)
	seenDown := false
	seenWithdraw := false
	for _, event := range events {
		if event.Type == EventPeerDown && event.Peer == "10.0.0.21" {
			seenDown = true
		}
		if event.Type == EventPrefixWithdrawn && event.Prefix == "10.0.0.200/32" {
			seenWithdraw = true
		}
	}
	if !seenDown || !seenWithdraw {
		t.Fatalf("withdraw/down events = %#v", events)
	}
}

func TestNormalizeAnnotatesPrefixSelection(t *testing.T) {
	state := Normalize(State{Prefixes: []Prefix{
		{Prefix: "10.250.0.0/24", Valid: true, SelectDeferred: true, SelectionReason: "waiting for EOR"},
		{Prefix: "10.250.1.0/24", Best: true, Valid: true, Installed: true},
	}})
	byPrefix := map[string]Prefix{}
	for _, prefix := range state.Prefixes {
		byPrefix[prefix.Prefix] = prefix
	}
	if got := byPrefix["10.250.0.0/24"]; !got.SelectDeferred || got.SelectionState != "selectDeferred" {
		t.Fatalf("deferred prefix = %#v", got)
	}
	if got := byPrefix["10.250.1.0/24"]; !got.Best || !got.Installed || got.SelectionState != "installed" {
		t.Fatalf("installed prefix = %#v", got)
	}
}

func TestLimitPrefixes(t *testing.T) {
	state := State{Prefixes: []Prefix{{Prefix: "10.0.0.1/32"}, {Prefix: "10.0.0.2/32"}}}
	limited, truncated := LimitPrefixes(state, 1)
	if !truncated || len(limited.Prefixes) != 1 {
		t.Fatalf("limited=%#v truncated=%v", limited, truncated)
	}
	if len(state.Prefixes) != 2 {
		t.Fatalf("LimitPrefixes mutated input: %#v", state)
	}
}
