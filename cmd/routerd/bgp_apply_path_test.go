// SPDX-License-Identifier: BSD-3-Clause
package main

import "testing"

func TestApplyPreservesBGPDaemonPaths(t *testing.T) {
	original := applyOptions{BGPSocketPath: "/custom/run/bgp.sock", BGPControlSocketPath: "/custom/run/control.sock", BGPStatePath: "/custom/state/applied.json"}
	got := applyChainControllerOptions(original)
	if got.BGPSocketPath != original.BGPSocketPath || got.BGPControlSocketPath != original.BGPControlSocketPath || got.BGPStatePath != original.BGPStatePath {
		t.Fatalf("BGP path overrides were lost: %#v", got)
	}
}
