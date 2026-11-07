// SPDX-License-Identifier: BSD-3-Clause

package inventory

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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
		CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
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

	got, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
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
		CommandOutput: func(_ context.Context, name string, args ...string) ([]byte, error) {
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

	got, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
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

func TestVirtualizationDoesNotConvertProbeFailureIntoPhysicalHost(t *testing.T) {
	exitOne := exec.Command("/bin/sh", "-c", "exit 1").Run()
	exitTwo := exec.Command("/bin/sh", "-c", "exit 2").Run()
	for _, tt := range []struct {
		name, vm, container, want string
		vmErr, containerErr       error
	}{
		{name: "explicit-empty", vm: "none", container: "none", want: "none"},
		{name: "normal-nonvirtual-exit", vm: "none", container: "none", want: "none", vmErr: exitOne, containerErr: exitOne},
		{name: "unavailable", want: "unknown", vmErr: errors.New("permission denied"), containerErr: errors.New("permission denied")},
		{name: "failed-output-is-not-proof", vm: "none", container: "none", want: "unknown", vmErr: exitOne, containerErr: exitTwo},
		{name: "known-container", container: "docker", want: "docker", vmErr: errors.New("vm query unavailable")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			collector := Collector{GOOS: "linux", LookPath: func(string) (string, error) { return "/fixture/systemd-detect-virt", nil }, CommandOutput: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if args[0] == "--vm" {
					return []byte(tt.vm), tt.vmErr
				}
				return []byte(tt.container), tt.containerErr
			}}
			if got := collector.virtualizationType(context.Background()); got != tt.want {
				t.Fatalf("virtualization=%q want=%q", got, tt.want)
			}
		})
	}
}

func TestInventoryActualSubprocessHonorsCollectionCancellation(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "uname"), []byte("#!/bin/sh\nexec /bin/sleep 10\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := Collect(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled host inventory returned %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("actual inventory command ignored cancellation: %s", elapsed)
	}
}
