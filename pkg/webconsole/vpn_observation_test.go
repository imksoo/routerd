// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVPNHTTPKeepsObservedZeroWireGuardCounters(t *testing.T) {
	dir := t.TempDir()
	writeWebConsoleTestCommand(t, filepath.Join(dir, "wg"), "#!/bin/sh\nprintf 'wg0\tprivate-key\tinterface-public\t51820\toff\nwg0\tpeer-public\tpreshared-key\t(none)\t10.0.0.2/32\t0\t0\t0\t0\n'\n")
	writeWebConsoleTestCommand(t, filepath.Join(dir, "tailscale"), "#!/bin/sh\nprintf '{\"BackendState\":\"Stopped\"}'\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	server := httptest.NewServer(New(Options{}))
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/api/v1/vpn")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var status struct {
		WireGuard []struct {
			Peers []struct {
				Rx *int64 `json:"transferRxBytes"`
				Tx *int64 `json:"transferTxBytes"`
			} `json:"peers"`
		} `json:"wireGuard"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if len(status.WireGuard) != 1 || len(status.WireGuard[0].Peers) != 1 {
		t.Fatalf("real command observation missing: %+v", status)
	}
	peer := status.WireGuard[0].Peers[0]
	if peer.Rx == nil || *peer.Rx != 0 || peer.Tx == nil || *peer.Tx != 0 {
		t.Fatalf("observed zero transfer counters became unavailable: %+v", peer)
	}
}
