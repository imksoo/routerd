// SPDX-License-Identifier: BSD-3-Clause

package bfd

import "testing"

func TestParseFRRBFDPeersJSON(t *testing.T) {
	got := ParseFRRBFDPeersJSON([]byte(`{
		"10.99.0.2": {"status": "up"},
		"peers": [
			{"peer": "10.99.0.3", "state": "down"},
			{"peerAddress": "10.99.0.4", "sessionState": "admin_down"}
		]
	}`))
	want := map[string]string{
		"10.99.0.2": "Up",
		"10.99.0.3": "Down",
		"10.99.0.4": "Down",
	}
	for address, state := range want {
		if got[address] != state {
			t.Fatalf("state[%s] = %q, want %q; all=%#v", address, got[address], state, got)
		}
	}
}
