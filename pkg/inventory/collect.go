// SPDX-License-Identifier: BSD-3-Clause

package inventory

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Status struct {
	OS             OSInfo          `json:"os" yaml:"os"`
	Virtualization Virtualization  `json:"virtualization" yaml:"virtualization"`
	DMI            DMIInfo         `json:"dmi,omitempty" yaml:"dmi,omitempty"`
	ServiceManager string          `json:"serviceManager,omitempty" yaml:"serviceManager,omitempty"`
	Commands       map[string]bool `json:"commands" yaml:"commands"`
}

type OSInfo struct {
	GOOS          string `json:"goos" yaml:"goos"`
	KernelName    string `json:"kernelName,omitempty" yaml:"kernelName,omitempty"`
	KernelRelease string `json:"kernelRelease,omitempty" yaml:"kernelRelease,omitempty"`
	KernelVersion string `json:"kernelVersion,omitempty" yaml:"kernelVersion,omitempty"`
	Uname         string `json:"uname,omitempty" yaml:"uname,omitempty"`
}

type Virtualization struct {
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
}

type DMIInfo struct {
	SysVendor      string `json:"sysVendor,omitempty" yaml:"sysVendor,omitempty"`
	ProductName    string `json:"productName,omitempty" yaml:"productName,omitempty"`
	ProductVersion string `json:"productVersion,omitempty" yaml:"productVersion,omitempty"`
	BoardVendor    string `json:"boardVendor,omitempty" yaml:"boardVendor,omitempty"`
	BoardName      string `json:"boardName,omitempty" yaml:"boardName,omitempty"`
}

func Collect() Status {
	status := Status{
		OS:       OSInfo{GOOS: runtime.GOOS},
		Commands: map[string]bool{},
	}
	status.OS.KernelName = trimmedCommand("uname", "-s")
	status.OS.KernelRelease = trimmedCommand("uname", "-r")
	status.OS.KernelVersion = trimmedCommand("uname", "-v")
	status.OS.Uname = trimmedCommand("uname", "-a")
	status.Virtualization.Type = virtualizationType()
	status.DMI = dmi()
	status.ServiceManager = serviceManager()
	for _, name := range []string{
		"systemd-detect-virt",
		"nft",
		"pf",
		"dnsmasq",
		"dhcp6c",
		"conntrack",
		"sysctl",
		"dig",
		"ping",
		"ping6",
		"tcpdump",
		"tracepath",
		"traceroute",
		"ip",
		"bridge",
		"wg",
		"swanctl",
		"pppd",
		"ppp",
		"modprobe",
		"ss",
		"resolvectl",
		"networkctl",
		"hostnamectl",
		"timedatectl",
		"journalctl",
		"netplan",
		"netstat",
		"sockstat",
		"pfctl",
		"service",
		"sysrc",
	} {
		status.Commands[name] = commandExists(name)
	}
	return status
}

func commandOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func virtualizationType() string {
	switch runtime.GOOS {
	case "linux":
		if !commandExists("systemd-detect-virt") {
			return "unknown"
		}
		out, err := commandOutput("systemd-detect-virt", "--vm")
		value := strings.TrimSpace(string(out))
		if err == nil && value != "" {
			return value
		}
		out, err = commandOutput("systemd-detect-virt", "--container")
		value = strings.TrimSpace(string(out))
		if err == nil && value != "" {
			return value
		}
		return "none"
	case "freebsd":
		if !commandExists("sysctl") {
			return "unknown"
		}
		value := trimmedCommand("sysctl", "-n", "kern.vm_guest")
		if value == "" {
			return "unknown"
		}
		return value
	default:
		return "unknown"
	}
}

func dmi() DMIInfo {
	return DMIInfo{
		SysVendor:      trimmedFile("/sys/class/dmi/id/sys_vendor"),
		ProductName:    trimmedFile("/sys/class/dmi/id/product_name"),
		ProductVersion: trimmedFile("/sys/class/dmi/id/product_version"),
		BoardVendor:    trimmedFile("/sys/class/dmi/id/board_vendor"),
		BoardName:      trimmedFile("/sys/class/dmi/id/board_name"),
	}
}

func serviceManager() string {
	if commandExists("systemctl") && pathExists("/run/systemd/system") {
		return "systemd"
	}
	if runtime.GOOS == "freebsd" || pathExists("/etc/rc.d") {
		return "rc.d"
	}
	return "unknown"
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func trimmedCommand(name string, args ...string) string {
	if !commandExists(name) {
		return ""
	}
	out, err := commandOutput(name, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func trimmedFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
