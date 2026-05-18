// SPDX-License-Identifier: BSD-3-Clause

package bgp

import "testing"

func TestParseFRRStateAndDiff(t *testing.T) {
	summary := []byte(`{
	  "ipv4Unicast": {
	    "peers": {
	      "10.0.0.21": {"remoteAs": 64513, "state": "Established", "pfxRcd": 1}
	    }
	  }
	}`)
	routes := []byte(`{
	  "routes": {
	    "10.0.0.200/32": [{"valid": true, "bestpath": true}]
	  }
	}`)
	state, err := ParseFRRState(summary, routes)
	if err != nil {
		t.Fatalf("parse FRR state: %v", err)
	}
	if len(state.Peers) != 1 || !state.Peers[0].Established {
		t.Fatalf("peers = %#v", state.Peers)
	}
	if len(state.Prefixes) != 1 || state.Prefixes[0].Prefix != "10.0.0.200/32" {
		t.Fatalf("prefixes = %#v", state.Prefixes)
	}
	events := Diff(State{}, state)
	if len(events) != 2 {
		t.Fatalf("events = %#v", events)
	}
	state2 := State{Peers: []Peer{{Address: "10.0.0.21", State: "Idle"}}, Prefixes: nil}
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
