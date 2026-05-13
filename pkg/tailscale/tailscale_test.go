// SPDX-License-Identifier: BSD-3-Clause

package tailscale

import "testing"

func TestParseStatusJSONSortsOnlineActivePeers(t *testing.T) {
	data := []byte(`{
		"BackendState": "Running",
		"CurrentTailnet": {"Name": "example.ts.net", "MagicDNSSuffix": "example.ts.net", "MagicDNSEnabled": true},
		"Self": {
			"HostName": "edge",
			"DNSName": "edge.example.ts.net.",
			"TailscaleIPs": ["100.64.0.1"],
			"AllowedIPs": ["100.64.0.1/32"],
			"Online": true,
			"Active": true,
			"ExitNode": true
		},
		"Peer": {
			"node-old": {
				"HostName": "old",
				"DNSName": "old.example.ts.net.",
				"TailscaleIPs": ["100.64.0.20"],
				"AllowedIPs": ["100.64.0.20/32"],
				"Online": false,
				"Active": false,
				"LastSeen": "2026-05-13T00:00:00Z"
			},
			"node-new": {
				"HostName": "new",
				"DNSName": "new.example.ts.net.",
				"TailscaleIPs": ["100.64.0.10"],
				"AllowedIPs": ["100.64.0.10/32", "192.168.50.0/24"],
				"Online": true,
				"Active": true,
				"Relay": "tok",
				"LastSeen": "2026-05-13T01:00:00Z",
				"RxBytes": 123,
				"TxBytes": 456
			}
		}
	}`)
	status, err := ParseStatusJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if status.BackendState != "Running" || status.HostName != "edge" {
		t.Fatalf("status = %#v", status)
	}
	if got := OnlinePeerCount(status); got != 1 {
		t.Fatalf("online peers = %d", got)
	}
	if len(status.Peers) != 2 || status.Peers[0].ID != "node-new" {
		t.Fatalf("peer order = %#v", status.Peers)
	}
	if status.Peers[0].Relay != "tok" || status.Peers[0].RxBytes != 123 || status.Peers[0].TxBytes != 456 {
		t.Fatalf("peer fields = %#v", status.Peers[0])
	}
}
