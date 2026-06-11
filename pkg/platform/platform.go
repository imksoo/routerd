// SPDX-License-Identifier: BSD-3-Clause

package platform

import (
	"os"
	"strings"
)

// OS identifies the host operating system family routerd is running on.
type OS string

const (
	OSLinux   OS = "linux"
	OSFreeBSD OS = "freebsd"
	OSOther   OS = "other"
)

// Defaults groups OS-specific filesystem locations used by routerd.
//
// Path values are intentionally absolute so they can be embedded in
// generated systemd units, rc.d scripts, NixOS modules, packaging
// manifests, and command-line flag defaults.
type Defaults struct {
	OS OS

	// PrefixDir is the install prefix used by source builds.
	PrefixDir string
	// BinDir holds routerd and routerctl binaries.
	BinDir string
	// SysconfDir holds router.yaml and rendered configuration.
	SysconfDir string
	// PluginDir holds trusted local plugins.
	PluginDir string
	// RuntimeDir holds runtime sockets, status files, and pid files.
	RuntimeDir string
	// StateDir holds the ownership ledger and other persistent state.
	StateDir string
	// SystemdUnitDir is where the routerd systemd unit is installed
	// on Linux. Empty on platforms that do not use systemd.
	SystemdUnitDir string
	// RCScriptDir is where the FreeBSD rc.d script is installed.
	// Empty on platforms that do not use rc.d.
	RCScriptDir string
	// OpenRCScriptDir is where OpenRC init scripts are installed.
	// Empty on platforms that do not use OpenRC.
	OpenRCScriptDir string

	// NetplanFile is the routerd-managed netplan drop-in path.
	// Empty on platforms that do not use netplan.
	NetplanFile string
	// NetworkdDropinDir is the systemd-networkd drop-in directory.
	// Empty on platforms without systemd-networkd.
	NetworkdDropinDir string
	// SystemdSystemDir is the directory for routerd-managed systemd
	// service units. Empty on platforms without systemd.
	SystemdSystemDir string
	// TimesyncdDropinFile is the timesyncd drop-in routerd manages.
	// Empty on platforms without systemd-timesyncd.
	TimesyncdDropinFile string

	// DnsmasqConfigFile is the routerd-managed dnsmasq config.
	DnsmasqConfigFile string
	// DnsmasqServiceFile is the routerd-managed dnsmasq service unit,
	// rc.d script, or OpenRC init script.
	DnsmasqServiceFile string
	// FreeBSDDHClientConfigFile is the dhclient.conf path on FreeBSD.
	FreeBSDDHClientConfigFile string
	// FreeBSDDHCPv6CConfigFile is the dhcp6c.conf path on FreeBSD.
	FreeBSDDHCPv6CConfigFile string
	// FreeBSDDHCPv6CDUIDFile is the KAME dhcp6c client DUID file on FreeBSD.
	FreeBSDDHCPv6CDUIDFile string
	// NftablesFile is the routerd-managed nftables ruleset path.
	// Empty on platforms without nftables.
	NftablesFile string
	// DefaultRouteNftablesFile is the nftables ruleset for the IPv4
	// default-route policy.
	DefaultRouteNftablesFile string

	// PPPoEChapSecretsFile is the system-wide PPP CHAP secrets file.
	PPPoEChapSecretsFile string
	// PPPoEPapSecretsFile is the system-wide PPP PAP secrets file.
	PPPoEPapSecretsFile string
	// FreeBSDMPD5ConfigFile is the mpd5 configuration file used for
	// FreeBSD PPPoE sessions.
	FreeBSDMPD5ConfigFile string
	// FreeBSDPFConfigFile is the pf.conf path on FreeBSD.
	FreeBSDPFConfigFile string
}

// Features describes which host integrations the current platform
// supports. Renderers and appliers consult these flags rather than
// inspecting runtime.GOOS directly.
type Features struct {
	// HasSystemd indicates that systemctl is the service manager.
	HasSystemd bool
	// HasNetplan indicates the host uses netplan for interface config.
	HasNetplan bool
	// HasSystemdNetworkd indicates the host uses systemd-networkd.
	HasSystemdNetworkd bool
	// HasSystemdTimesyncd indicates the host uses systemd-timesyncd.
	HasSystemdTimesyncd bool
	// HasNftables indicates nftables is available for firewall/NAT.
	HasNftables bool
	// HasPF indicates the BSD pf packet filter is available.
	HasPF bool
	// HasIproute2 indicates the iproute2 toolchain (ip, ss, etc.).
	HasIproute2 bool
	// HasResolvectl indicates systemd-resolved is available.
	HasResolvectl bool
	// HasRCD indicates the FreeBSD rc.d service framework.
	HasRCD bool
	// HasOpenRC indicates an OpenRC-managed Linux host such as Alpine.
	HasOpenRC bool
}

func osReleaseID() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "ID" {
			continue
		}
		return strings.Trim(value, `"`)
	}
	return ""
}

// Current returns the defaults and features for the OS this binary was
// built for. It is selected at compile time via build tags so the
// compiler can drop unused branches.
func Current() (Defaults, Features) {
	return currentDefaults(), currentFeatures()
}

// CurrentOS returns the OS identifier for the build.
func CurrentOS() OS {
	return currentDefaults().OS
}

// IsNixOSHost reports whether the running Linux host is NixOS.
//
// NixOS is still a Linux platform from routerd's point of view, but service
// activation is owned by nixos-rebuild instead of direct systemd unit writes.
func IsNixOSHost() bool {
	return osReleaseID() == "nixos"
}

// IsAlpineHost reports whether the running Linux host is Alpine Linux.
func IsAlpineHost() bool {
	return osReleaseID() == "alpine"
}

// StatusFile returns the default status file path.
func (d Defaults) StatusFile() string {
	return d.RuntimeDir + "/status.json"
}

// SocketFile returns the default control-API Unix socket path.
func (d Defaults) SocketFile() string {
	return d.RuntimeDir + "/routerd.sock"
}

// StatusSocketFile returns the default read-only status Unix socket path.
func (d Defaults) StatusSocketFile() string {
	return d.RuntimeDir + "/routerd-status.sock"
}

// LockFile returns the default apply lock file path.
func (d Defaults) LockFile() string {
	return d.RuntimeDir + "/routerd.lock"
}

// LedgerFile returns the default ownership ledger path.
func (d Defaults) LedgerFile() string {
	return d.StateDir + "/artifacts.json"
}

// DBFile returns the default structured state and ownership database path.
func (d Defaults) DBFile() string {
	return d.StateDir + "/routerd.db"
}

// FirewallLogFile returns the default firewall event log database path.
func (d Defaults) FirewallLogFile() string {
	return strings.TrimRight(d.StateDir, "/") + "/firewall-logs.db"
}

// DnsmasqLeaseFile returns the default managed dnsmasq lease file path.
func (d Defaults) DnsmasqLeaseFile() string {
	return strings.TrimRight(d.StateDir, "/") + "/dnsmasq/dnsmasq.leases"
}

// RuntimeDnsmasqLeaseFile returns the legacy runtime dnsmasq lease file path.
func (d Defaults) RuntimeDnsmasqLeaseFile() string {
	return strings.TrimRight(d.RuntimeDir, "/") + "/dnsmasq.leases"
}

// DnsmasqLeaseCandidates returns the managed dnsmasq lease path.
func DnsmasqLeaseCandidates(d Defaults, f Features) []string {
	_ = f
	return []string{d.DnsmasqLeaseFile()}
}

// ConfigFile returns the default router.yaml path.
func (d Defaults) ConfigFile() string {
	return d.SysconfDir + "/router.yaml"
}
