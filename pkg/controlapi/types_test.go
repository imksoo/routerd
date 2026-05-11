// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import "testing"

func TestControllerModeReasonValues(t *testing.T) {
	got := []ControllerModeReason{
		ControllerModeReasonLive,
		ControllerModeReasonManual,
		ControllerModeReasonOSUnsupported,
		ControllerModeReasonDependencyUnmet,
		ControllerModeReasonSpecDisabled,
		ControllerModeReasonUnknown,
	}
	want := []string{
		"Live",
		"Manual",
		"OSUnsupported",
		"DependencyUnmet",
		"SpecDisabled",
		"Unknown",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	seen := map[string]bool{}
	for i, reason := range got {
		if string(reason) != want[i] {
			t.Fatalf("reason[%d] = %q, want %q", i, reason, want[i])
		}
		if seen[string(reason)] {
			t.Fatalf("duplicate reason %q", reason)
		}
		seen[string(reason)] = true
	}
}
