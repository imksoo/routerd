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
	if got, want := defaults.LockFile(), defaults.RuntimeDir+"/routerd.lock"; got != want {
		t.Errorf("LockFile = %q, want %q", got, want)
	}
	if got, want := defaults.LedgerFile(), defaults.StateDir+"/artifacts.json"; got != want {
		t.Errorf("LedgerFile = %q, want %q", got, want)
	}
	if got, want := defaults.ConfigFile(), defaults.SysconfDir+"/router.yaml"; got != want {
		t.Errorf("ConfigFile = %q, want %q", got, want)
	}
}

func TestLinuxFeatureFlagsConsistency(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only invariants")
	}
	_, features := Current()
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
}
