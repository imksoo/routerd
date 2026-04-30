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

type Collector struct {
	GOOS          string
	LookPath      func(string) (string, error)
	CommandOutput func(string, ...string) ([]byte, error)
	ReadFile      func(string) ([]byte, error)
	Stat          func(string) (os.FileInfo, error)
}

func Collect() Status {
	return DefaultCollector().Collect()
}

func DefaultCollector() Collector {
	return Collector{
		GOOS:          runtime.GOOS,
		LookPath:      exec.LookPath,
		CommandOutput: commandOutput,
		ReadFile:      os.ReadFile,
		Stat:          os.Stat,
	}
}

func commandOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func (c Collector) Collect() Status {
	c = c.withDefaults()
	status := Status{
		OS:       OSInfo{GOOS: c.GOOS},
		Commands: map[string]bool{},
	}
	status.OS.KernelName = c.trimmedCommand("uname", "-s")
	status.OS.KernelRelease = c.trimmedCommand("uname", "-r")
	status.OS.KernelVersion = c.trimmedCommand("uname", "-v")
	status.OS.Uname = c.trimmedCommand("uname", "-a")
	status.Virtualization.Type = c.virtualizationType()
	status.DMI = c.dmi()
	status.ServiceManager = c.serviceManager()
	for _, name := range []string{
		"systemd-detect-virt",
		"nft",
		"pf",
		"dnsmasq",
		"dhcp6c",
		"sysctl",
		"dig",
		"ping",
		"ping6",
		"tcpdump",
		"tracepath",
		"traceroute",
		"ip",
		"ss",
		"resolvectl",
		"networkctl",
		"journalctl",
		"netstat",
		"sockstat",
		"pfctl",
	} {
		status.Commands[name] = c.commandExists(name)
	}
	return status
}

func (c Collector) withDefaults() Collector {
	if c.GOOS == "" {
		c.GOOS = runtime.GOOS
	}
	if c.LookPath == nil {
		c.LookPath = exec.LookPath
	}
	if c.CommandOutput == nil {
		c.CommandOutput = commandOutput
	}
	if c.ReadFile == nil {
		c.ReadFile = os.ReadFile
	}
	if c.Stat == nil {
		c.Stat = os.Stat
	}
	return c
}

func (c Collector) virtualizationType() string {
	switch c.GOOS {
	case "linux":
		if !c.commandExists("systemd-detect-virt") {
			return "unknown"
		}
		out, err := c.CommandOutput("systemd-detect-virt", "--vm")
		value := strings.TrimSpace(string(out))
		if err == nil && value != "" {
			return value
		}
		out, err = c.CommandOutput("systemd-detect-virt", "--container")
		value = strings.TrimSpace(string(out))
		if err == nil && value != "" {
			return value
		}
		return "none"
	case "freebsd":
		if !c.commandExists("sysctl") {
			return "unknown"
		}
		value := c.trimmedCommand("sysctl", "-n", "kern.vm_guest")
		if value == "" {
			return "unknown"
		}
		return value
	default:
		return "unknown"
	}
}

func (c Collector) dmi() DMIInfo {
	return DMIInfo{
		SysVendor:      c.trimmedFile("/sys/class/dmi/id/sys_vendor"),
		ProductName:    c.trimmedFile("/sys/class/dmi/id/product_name"),
		ProductVersion: c.trimmedFile("/sys/class/dmi/id/product_version"),
		BoardVendor:    c.trimmedFile("/sys/class/dmi/id/board_vendor"),
		BoardName:      c.trimmedFile("/sys/class/dmi/id/board_name"),
	}
}

func (c Collector) serviceManager() string {
	if c.commandExists("systemctl") && c.pathExists("/run/systemd/system") {
		return "systemd"
	}
	if c.GOOS == "freebsd" || c.pathExists("/etc/rc.d") {
		return "rc.d"
	}
	return "unknown"
}

func (c Collector) commandExists(name string) bool {
	_, err := c.LookPath(name)
	return err == nil
}

func (c Collector) pathExists(path string) bool {
	_, err := c.Stat(path)
	return err == nil
}

func (c Collector) trimmedCommand(name string, args ...string) string {
	if !c.commandExists(name) {
		return ""
	}
	out, err := c.CommandOutput(name, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c Collector) trimmedFile(path string) string {
	data, err := c.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
