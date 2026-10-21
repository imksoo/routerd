// SPDX-License-Identifier: BSD-3-Clause
package platform

import (
	"path/filepath"
	"testing"
)

func TestDHCPQueueFollowsPersistentDefaultsNotConfigOrSocket(t *testing.T) {
	for _, state := range []string{"/var/lib/routerd", "/var/db/routerd", "/sandbox/var/lib/routerd", "/custom/persistent"} {
		d := Defaults{StateDir: state, RuntimeDir: "/custom/runtime", SysconfDir: "/custom/config"}
		if got, want := d.DHCPEventQueueFile(), filepath.Join(state, "dhcp-events", "pending.db"); got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
}
