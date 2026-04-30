package inventory

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestCollectLinuxInventory(t *testing.T) {
	commands := map[string]bool{
		"uname":               true,
		"systemd-detect-virt": true,
		"systemctl":           true,
		"nft":                 true,
		"dnsmasq":             true,
		"sysctl":              true,
		"dig":                 true,
		"ping":                true,
		"tcpdump":             true,
		"tracepath":           true,
		"ip":                  true,
		"ss":                  true,
		"journalctl":          true,
	}
	files := map[string]string{
		"/sys/class/dmi/id/sys_vendor":   "QEMU",
		"/sys/class/dmi/id/product_name": "Standard PC",
	}
	collector := Collector{
		GOOS: "linux",
		LookPath: func(name string) (string, error) {
			if commands[name] {
				return "/usr/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		CommandOutput: func(name string, args ...string) ([]byte, error) {
			switch {
			case name == "uname" && len(args) == 1 && args[0] == "-s":
				return []byte("Linux\n"), nil
			case name == "uname" && len(args) == 1 && args[0] == "-r":
				return []byte("6.8.0-test\n"), nil
			case name == "uname" && len(args) == 1 && args[0] == "-v":
				return []byte("#1 SMP\n"), nil
			case name == "uname" && len(args) == 1 && args[0] == "-a":
				return []byte("Linux router 6.8.0-test #1 SMP\n"), nil
			case name == "systemd-detect-virt":
				return []byte("kvm\n"), nil
			default:
				return nil, errors.New("unexpected command")
			}
		},
		ReadFile: func(path string) ([]byte, error) {
			if value, ok := files[path]; ok {
				return []byte(value + "\n"), nil
			}
			return nil, os.ErrNotExist
		},
		Stat: func(path string) (os.FileInfo, error) {
			if path == "/run/systemd/system" {
				return nil, nil
			}
			return nil, os.ErrNotExist
		},
	}

	got := collector.Collect()
	if got.OS.GOOS != "linux" || got.OS.KernelName != "Linux" || got.OS.KernelRelease != "6.8.0-test" {
		t.Fatalf("OS inventory = %#v", got.OS)
	}
	if got.Virtualization.Type != "kvm" {
		t.Fatalf("virtualization = %#v", got.Virtualization)
	}
	if got.DMI.SysVendor != "QEMU" || got.DMI.ProductName != "Standard PC" {
		t.Fatalf("DMI = %#v", got.DMI)
	}
	if got.ServiceManager != "systemd" {
		t.Fatalf("service manager = %q", got.ServiceManager)
	}
	if !got.Commands["nft"] || !got.Commands["dig"] || !got.Commands["tcpdump"] || !got.Commands["tracepath"] || got.Commands["dhcp6c"] {
		t.Fatalf("commands = %#v", got.Commands)
	}
}

func TestCollectFreeBSDInventory(t *testing.T) {
	collector := Collector{
		GOOS: "freebsd",
		LookPath: func(name string) (string, error) {
			if name == "sysctl" || name == "uname" || name == "dhcp6c" || name == "dig" || name == "ping6" || name == "traceroute" || name == "netstat" || name == "sockstat" || name == "pfctl" {
				return "/usr/sbin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		CommandOutput: func(name string, args ...string) ([]byte, error) {
			if name == "sysctl" {
				return []byte("kvm\n"), nil
			}
			if name == "uname" && len(args) == 1 && args[0] == "-s" {
				return []byte("FreeBSD\n"), nil
			}
			return []byte(""), nil
		},
		ReadFile: func(path string) ([]byte, error) { return nil, os.ErrNotExist },
		Stat:     func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist },
	}

	got := collector.Collect()
	if got.Virtualization.Type != "kvm" {
		t.Fatalf("virtualization = %#v", got.Virtualization)
	}
	if got.ServiceManager != "rc.d" {
		t.Fatalf("service manager = %q", got.ServiceManager)
	}
	if !got.Commands["dhcp6c"] || !got.Commands["dig"] || !got.Commands["traceroute"] || !got.Commands["netstat"] || got.Commands["nft"] {
		t.Fatalf("commands = %#v", got.Commands)
	}
}
