// SPDX-License-Identifier: BSD-3-Clause

package platform

import (
	"runtime"
	"strings"
	"testing"
)

func TestCurrentMatchesGOOS(t *testing.T) {
	defaults, _ := Current()
	switch runtime.GOOS {
	case "linux":
		if defaults.OS != OSLinux {
			t.Fatalf("expected OSLinux, got %q", defaults.OS)
		}
	case "freebsd":
		if defaults.OS != OSFreeBSD {
			t.Fatalf("expected OSFreeBSD, got %q", defaults.OS)
		}
	default:
		if defaults.OS != OSOther {
			t.Fatalf("expected OSOther on %s, got %q", runtime.GOOS, defaults.OS)
		}
	}
}

func TestDefaultsAreAbsolute(t *testing.T) {
	defaults, _ := Current()
	for label, path := range map[string]string{
		"PrefixDir":  defaults.PrefixDir,
		"BinDir":     defaults.BinDir,
		"SysconfDir": defaults.SysconfDir,
		"PluginDir":  defaults.PluginDir,
		"RuntimeDir": defaults.RuntimeDir,
		"StateDir":   defaults.StateDir,
	} {
		if path == "" {
			t.Errorf("%s is empty", label)
			continue
		}
		if !strings.HasPrefix(path, "/") {
			t.Errorf("%s is not absolute: %q", label, path)
		}
	}
}

func TestDerivedPaths(t *testing.T) {
	defaults, _ := Current()
	if got, want := defaults.StatusFile(), defaults.RuntimeDir+"/status.json"; got != want {
		t.Errorf("StatusFile = %q, want %q", got, want)
	}
	if got, want := defaults.SocketFile(), defaults.RuntimeDir+"/routerd.sock"; got != want {
		t.Errorf("SocketFile = %q, want %q", got, want)
	}
	if got, want := defaults.StatusSocketFile(), defaults.RuntimeDir+"/routerd-status.sock"; got != want {
		t.Errorf("StatusSocketFile = %q, want %q", got, want)
	}
	if got, want := defaults.LockFile(), defaults.RuntimeDir+"/routerd.lock"; got != want {
		t.Errorf("LockFile = %q, want %q", got, want)
	}
	if got, want := defaults.LedgerFile(), defaults.StateDir+"/artifacts.json"; got != want {
		t.Errorf("LedgerFile = %q, want %q", got, want)
	}
	if got, want := defaults.DBFile(), defaults.StateDir+"/routerd.db"; got != want {
		t.Errorf("DBFile = %q, want %q", got, want)
	}
	if got, want := defaults.FirewallLogFile(), strings.TrimRight(defaults.StateDir, "/")+"/firewall-logs.db"; got != want {
		t.Errorf("FirewallLogFile = %q, want %q", got, want)
	}
	if got, want := defaults.DnsmasqLeaseFile(), strings.TrimRight(defaults.StateDir, "/")+"/dnsmasq/dnsmasq.leases"; got != want {
		t.Errorf("DnsmasqLeaseFile = %q, want %q", got, want)
	}
	if got, want := defaults.RuntimeDnsmasqLeaseFile(), strings.TrimRight(defaults.RuntimeDir, "/")+"/dnsmasq.leases"; got != want {
		t.Errorf("RuntimeDnsmasqLeaseFile = %q, want %q", got, want)
	}
	if got, want := defaults.ConfigFile(), defaults.SysconfDir+"/router.yaml"; got != want {
		t.Errorf("ConfigFile = %q, want %q", got, want)
	}
}

func TestCanonicalResourcePaths(t *testing.T) {
	for _, tt := range []struct {
		name         string
		defaults     Defaults
		firewallLogs string
		dnsmasqLease string
	}{
		{
			name:         "linux",
			defaults:     Defaults{StateDir: "/var/lib/routerd"},
			firewallLogs: "/var/lib/routerd/firewall-logs.db",
			dnsmasqLease: "/var/lib/routerd/dnsmasq/dnsmasq.leases",
		},
		{
			name:         "freebsd",
			defaults:     Defaults{StateDir: "/var/db/routerd"},
			firewallLogs: "/var/db/routerd/firewall-logs.db",
			dnsmasqLease: "/var/db/routerd/dnsmasq/dnsmasq.leases",
		},
		{
			name:         "trim slash",
			defaults:     Defaults{StateDir: "/tmp/routerd/state/"},
			firewallLogs: "/tmp/routerd/state/firewall-logs.db",
			dnsmasqLease: "/tmp/routerd/state/dnsmasq/dnsmasq.leases",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.defaults.FirewallLogFile(); got != tt.firewallLogs {
				t.Fatalf("FirewallLogFile = %q, want %q", got, tt.firewallLogs)
			}
			if got := tt.defaults.DnsmasqLeaseFile(); got != tt.dnsmasqLease {
				t.Fatalf("DnsmasqLeaseFile = %q, want %q", got, tt.dnsmasqLease)
			}
		})
	}
}

func TestDnsmasqLeaseCandidatesPreferActualManagedPath(t *testing.T) {
	defaults := Defaults{RuntimeDir: "/run/routerd", StateDir: "/var/lib/routerd"}
	if got := DnsmasqLeaseCandidates(defaults, Features{}); got[0] != "/run/routerd/dnsmasq.leases" {
		t.Fatalf("systemd candidates = %#v, want runtime path first", got)
	}
	if got := DnsmasqLeaseCandidates(defaults, Features{HasOpenRC: true}); got[0] != "/var/lib/routerd/dnsmasq/dnsmasq.leases" {
		t.Fatalf("openrc candidates = %#v, want state path first", got)
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

func TestLinuxFeatureFlagsConsistency(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only invariants")
	}
	_, features := Current()
	if IsAlpineHost() {
		if !features.HasOpenRC {
			t.Error("alpine Defaults must declare HasOpenRC")
		}
		if features.HasSystemd {
			t.Error("alpine Defaults must not declare HasSystemd")
		}
		if features.HasNetplan {
			t.Error("alpine Defaults must not declare HasNetplan")
		}
		return
	}
	if !features.HasSystemd {
		t.Error("linux Defaults must declare HasSystemd")
	}
	if !features.HasNftables {
		t.Error("linux Defaults must declare HasNftables")
	}
	if !features.HasIproute2 {
		t.Error("linux Defaults must declare HasIproute2")
	}
}

func TestFreeBSDFeatureFlagsConsistency(t *testing.T) {
	if runtime.GOOS != "freebsd" {
		t.Skip("freebsd-only invariants")
	}
	_, features := Current()
	if features.HasSystemd {
		t.Error("freebsd Defaults must not declare HasSystemd")
	}
	if !features.HasRCD {
		t.Error("freebsd Defaults must declare HasRCD")
	}
	if !features.HasPF {
		t.Error("freebsd Defaults must declare HasPF")
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
	if !features.HasOpenRC && defaults.OpenRCScriptDir != "" {
		t.Errorf("OpenRCScriptDir set on non-OpenRC platform: %q", defaults.OpenRCScriptDir)
	}
	if features.HasOpenRC && defaults.OpenRCScriptDir == "" {
		t.Error("OpenRCScriptDir is empty on OpenRC platform")
	}
}
