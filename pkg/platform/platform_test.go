// SPDX-License-Identifier: BSD-3-Clause

package platform

import (
	"testing"
)

func TestDnsmasqLeaseCandidatesPreferActualManagedPath(t *testing.T) {
	defaults := Defaults{RuntimeDir: "/run/routerd", StateDir: "/var/lib/routerd"}
	if got := DnsmasqLeaseCandidates(defaults, Features{}); got[0] != "/var/lib/routerd/dnsmasq/dnsmasq.leases" {
		t.Fatalf("systemd candidates = %#v, want state path first", got)
	}
	freebsd := Defaults{RuntimeDir: "/var/run/routerd", StateDir: "/var/db/routerd"}
	got := DnsmasqLeaseCandidates(freebsd, Features{HasRCD: true})
	if got[0] != "/var/db/routerd/dnsmasq/dnsmasq.leases" {
		t.Fatalf("freebsd candidates = %#v, want state path first", got)
	}
	if got[1] != "/var/run/routerd/dnsmasq.leases" {
		t.Fatalf("freebsd candidates = %#v, want runtime fallback second", got)
	}
}

func TestSystemdPathsRequireSystemd(t *testing.T) {
	defaults, features := Current()
	if !features.HasSystemd {
		if defaults.SystemdUnitDir != "" {
			t.Errorf("SystemdUnitDir set on non-systemd platform: %q", defaults.SystemdUnitDir)
		}
		if defaults.SystemdSystemDir != "" {
			t.Errorf("SystemdSystemDir set on non-systemd platform: %q", defaults.SystemdSystemDir)
		}
	}
	if !features.HasRCD && defaults.RCScriptDir != "" {
		t.Errorf("RCScriptDir set on non-rc.d platform: %q", defaults.RCScriptDir)
	}
}
