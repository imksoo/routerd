// SPDX-License-Identifier: BSD-3-Clause

package bgp

import "testing"

func TestParseFRRStateAndDiff(t *testing.T) {
	summary := []byte(`{
	  "ipv4Unicast": {
	    "peers": {
	      "10.0.0.21": {"remoteAs": "64513", "state": "Established", "pfxRcd": "1", "msgRcvd": "12", "msgSent": "11", "lastConnectionEstablished": "2026-05-18T10:00:00Z"}
	    }
	  }
	}`)
	routes := []byte(`{
	  "routes": {
	    "10.0.0.200/32": [{"valid": true, "bestpath": true, "community": {"string": "64513:100 no-export"}}]
	  }
	}`)
	state, err := ParseFRRState(summary, routes)
	if err != nil {
		t.Fatalf("parse FRR state: %v", err)
	}
	if len(state.Peers) != 1 || !state.Peers[0].Established {
		t.Fatalf("peers = %#v", state.Peers)
	}
	if state.Peers[0].LastEstablishedAt != "2026-05-18T10:00:00Z" {
		t.Fatalf("lastEstablishedAt = %#v", state.Peers[0])
	}
	if state.Peers[0].MessagesReceived != 12 || state.Peers[0].MessagesSent != 11 {
		t.Fatalf("message counters = %#v", state.Peers[0])
	}
	if len(state.Prefixes) != 1 || state.Prefixes[0].Prefix != "10.0.0.200/32" {
		t.Fatalf("prefixes = %#v", state.Prefixes)
	}
	if got := state.Prefixes[0].Communities; len(got) != 2 || got[0] != "64513:100" || got[1] != "no-export" {
		t.Fatalf("communities = %#v", got)
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

func TestParseFRRSummaryAcceptsRemoteASNVariants(t *testing.T) {
	peers, err := ParseFRRSummaryJSON([]byte(`{
	  "ipv4Unicast": {
	    "peers": {
	      "10.0.0.21": {"remoteAsn": 64513, "state": "Established"}
	    }
	  }
	}`))
	if err != nil {
		t.Fatalf("parse FRR summary: %v", err)
	}
	if len(peers) != 1 || peers[0].ASN != 64513 {
		t.Fatalf("peers = %#v", peers)
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

func TestParseFRRBFDPeersJSONAndAttach(t *testing.T) {
	data := []byte(`{
	  "peers": {
	    "10.0.0.21": {
	      "status": "up",
	      "lastUp": "2026-05-19T00:00:00Z"
	    },
	    "10.0.0.22": {
	      "status": "down",
	      "lastDown": "2026-05-19T00:01:00Z"
	    }
	  }
	}`)
	bfd, err := ParseFRRBFDPeersJSON(data)
	if err != nil {
		t.Fatalf("parse BFD peers: %v", err)
	}
	state := AttachBFD(State{Peers: []Peer{
		{Address: "10.0.0.21", State: "Established"},
		{Address: "10.0.0.23", State: "Idle"},
	}}, bfd)
	if state.Peers[0].BFD == nil || state.Peers[0].BFD.State != "up" || state.Peers[0].BFD.LastUp != "2026-05-19T00:00:00Z" {
		t.Fatalf("attached BFD = %#v", state.Peers[0].BFD)
	}
	if state.Peers[1].BFD != nil {
		t.Fatalf("unexpected BFD on unmatched peer: %#v", state.Peers[1].BFD)
	}
}
