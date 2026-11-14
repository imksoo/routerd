// SPDX-License-Identifier: BSD-3-Clause

package daemonapi

import "testing"

func TestCommandVocabulary(t *testing.T) {
	got := []string{CommandRenew, CommandRebind, CommandRelease, CommandReload, CommandStop, CommandStart, CommandFlush}
	want := []string{"renew", "rebind", "release", "reload", "stop", "start", "flush"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("command[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
