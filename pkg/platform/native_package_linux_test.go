//go:build linux

// SPDX-License-Identifier: BSD-3-Clause

package platform

import "testing"

func TestLinuxNativePackagePaths(t *testing.T) {
	d, _ := Current()
	for name, pair := range map[string][2]string{
		"prefix":        {d.PrefixDir, "/usr"},
		"binaries":      {d.BinDir, "/usr/sbin"},
		"configuration": {d.SysconfDir, "/etc/routerd"},
		"plugins":       {d.PluginDir, "/usr/libexec/routerd/plugins"},
		"vendor units":  {d.SystemdUnitDir, "/usr/lib/systemd/system"},
		"runtime":       {d.RuntimeDir, "/run/routerd"},
		"state":         {d.StateDir, "/var/lib/routerd"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s: got %q, want %q", name, pair[0], pair[1])
		}
	}
}
