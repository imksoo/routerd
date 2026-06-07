// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resource"
)

type ArtifactTeardownExecutor interface {
	Features() platform.Features
	Run(name string, args ...string) error
	Remove(path string) error
	RemoveAll(path string) error
	DeleteIPv4FwmarkRule(priority, mark, table int) error
	FlushIPv4RouteTable(table int) error
}

type ArtifactTeardown struct {
	Kind        string
	Priority    int
	Eligible    func(resource.Artifact) bool
	Remediation func(resource.Artifact) string
	Teardown    func(ArtifactTeardownExecutor, resource.Artifact) (string, error)
}

func ArtifactCleanupEligible(artifact resource.Artifact) bool {
	if teardown, ok := ArtifactTeardownFor(artifact); ok {
		return teardown.Eligible == nil || teardown.Eligible(artifact)
	}
	return false
}

func ArtifactCleanupPriority(artifact resource.Artifact) int {
	if teardown, ok := ArtifactTeardownFor(artifact); ok {
		return teardown.Priority
	}
	return 50
}

func ArtifactCleanupRemediation(artifact resource.Artifact) string {
	if teardown, ok := ArtifactTeardownFor(artifact); ok && (teardown.Eligible == nil || teardown.Eligible(artifact)) {
		if teardown.Remediation != nil {
			return teardown.Remediation(artifact)
		}
	}
	return ""
}

func CleanupArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	teardown, ok := ArtifactTeardownFor(artifact)
	if !ok || teardown.Teardown == nil || (teardown.Eligible != nil && !teardown.Eligible(artifact)) {
		return "", nil
	}
	return teardown.Teardown(exec, artifact)
}

func ArtifactTeardownFor(artifact resource.Artifact) (ArtifactTeardown, bool) {
	for _, teardown := range artifactTeardowns {
		if teardown.Kind == artifact.Kind {
			return teardown, true
		}
	}
	return ArtifactTeardown{}, false
}

func ArtifactTeardownRegistry() []ArtifactTeardown {
	out := append([]ArtifactTeardown(nil), artifactTeardowns...)
	return out
}

var artifactTeardowns = []ArtifactTeardown{
	{
		Kind:     "linux.ipip6.tunnel",
		Priority: 50,
		Remediation: func(artifact resource.Artifact) string {
			return "delete ipip6 tunnel " + artifact.Name
		},
		Teardown: cleanupIPIP6TunnelArtifact,
	},
	{
		Kind:     "linux.ipv4.fwmarkRule",
		Priority: 0,
		Remediation: func(artifact resource.Artifact) string {
			return "delete ip rule " + artifact.Name
		},
		Teardown: cleanupIPv4FwmarkRuleArtifact,
	},
	{
		Kind:     "linux.ipv4.routeTable",
		Priority: 5,
		Remediation: func(artifact resource.Artifact) string {
			return "flush ip route table " + artifact.Attributes["table"]
		},
		Teardown: cleanupIPv4RouteTableArtifact,
	},
	{
		Kind:     "nft.table",
		Priority: 50,
		Eligible: func(artifact resource.Artifact) bool {
			return strings.HasPrefix(artifact.Attributes["name"], "routerd_")
		},
		Remediation: func(artifact resource.Artifact) string {
			return "delete nft table " + artifact.Attributes["family"] + " " + artifact.Attributes["name"]
		},
		Teardown: cleanupNftTableArtifact,
	},
	{
		Kind:     "systemd.service",
		Priority: 10,
		Remediation: func(artifact resource.Artifact) string {
			return "disable and stop systemd service " + artifact.Name
		},
		Teardown: cleanupSystemdServiceArtifact,
	},
	{
		Kind:     "file",
		Priority: 20,
		Eligible: IsPPPoEPeerFileArtifact,
		Remediation: func(artifact resource.Artifact) string {
			return "delete file " + artifact.Name
		},
		Teardown: cleanupFileArtifact,
	},
	{
		Kind:     "unix.socket",
		Priority: 30,
		Eligible: IsPPPoERuntimeSocketArtifact,
		Remediation: func(artifact resource.Artifact) string {
			return "delete Unix socket " + artifact.Name
		},
		Teardown: cleanupUnixSocketArtifact,
	},
	{
		Kind:     "directory",
		Priority: 40,
		Eligible: IsPPPoERuntimeDirectoryArtifact,
		Remediation: func(artifact resource.Artifact) string {
			return "delete directory " + artifact.Name
		},
		Teardown: cleanupDirectoryArtifact,
	},
	{
		Kind:     "net.ipv4.address",
		Priority: 50,
		Eligible: IsDSLiteIPv4AddressArtifact,
		Remediation: func(artifact resource.Artifact) string {
			return "remove IPv4 address " + artifact.Name
		},
		Teardown: cleanupIPv4AddressArtifact,
	},
	{
		Kind:     "net.ipv6.address",
		Priority: 50,
		Eligible: IsStaticIPv6AddressArtifact,
		Remediation: func(artifact resource.Artifact) string {
			return "remove IPv6 address " + artifact.Name
		},
		Teardown: cleanupIPv6AddressArtifact,
	},
}

func cleanupIPIP6TunnelArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	features := exec.Features()
	if features.HasIproute2 {
		if err := exec.Run("ip", "-6", "tunnel", "del", artifact.Name); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	}
	if features.HasPF {
		if err := exec.Run("ifconfig", artifact.Name, "destroy"); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	}
	return "", nil
}

func cleanupIPv4FwmarkRuleArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	if !exec.Features().HasIproute2 {
		return "", nil
	}
	rule, ok := IPv4FwmarkRuleFromArtifact(artifact)
	if !ok {
		return "", nil
	}
	if err := exec.DeleteIPv4FwmarkRule(rule.Priority, rule.Mark, rule.Table); err != nil {
		return "", err
	}
	return artifact.Kind + "/" + artifact.Name, nil
}

func cleanupIPv4RouteTableArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	if !exec.Features().HasIproute2 {
		return "", nil
	}
	table, err := strconv.Atoi(artifact.Attributes["table"])
	if err != nil || table == 0 {
		return "", nil
	}
	if err := exec.FlushIPv4RouteTable(table); err != nil {
		return "", err
	}
	return artifact.Kind + "/" + artifact.Name, nil
}

func cleanupNftTableArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	if !exec.Features().HasNftables {
		return "", nil
	}
	family := artifact.Attributes["family"]
	name := artifact.Attributes["name"]
	if err := exec.Run("nft", "delete", "table", family, name); err != nil {
		return "", err
	}
	return artifact.Kind + "/" + name, nil
}

func cleanupSystemdServiceArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	if !exec.Features().HasSystemd {
		return "", nil
	}
	if !strings.HasPrefix(artifact.Name, "routerd-") || !strings.HasSuffix(artifact.Name, ".service") {
		return "", nil
	}
	if err := exec.Run("systemctl", "disable", "--now", artifact.Name); err != nil {
		return "", err
	}
	_ = exec.Run("systemctl", "reset-failed", artifact.Name)
	if err := exec.Remove("/etc/systemd/system/" + artifact.Name); err != nil {
		return "", err
	}
	if err := exec.Run("systemctl", "daemon-reload"); err != nil {
		return "", err
	}
	return artifact.Kind + "/" + artifact.Name, nil
}

func cleanupFileArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	if err := exec.Remove(artifact.Name); err != nil {
		return "", err
	}
	return artifact.Kind + "/" + artifact.Name, nil
}

func cleanupUnixSocketArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	if err := exec.Remove(artifact.Name); err != nil {
		return "", err
	}
	return artifact.Kind + "/" + artifact.Name, nil
}

func cleanupDirectoryArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	if err := exec.RemoveAll(artifact.Name); err != nil {
		return "", err
	}
	return artifact.Kind + "/" + artifact.Name, nil
}

func cleanupIPv4AddressArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	ifname, address, ok := strings.Cut(artifact.Name, ":")
	if !ok || ifname == "" || address == "" {
		return "", nil
	}
	features := exec.Features()
	if features.HasIproute2 {
		if err := exec.Run("ip", "-4", "addr", "del", address, "dev", ifname); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	}
	if features.HasPF {
		if strings.HasPrefix(ifname, "gif") && strings.Contains(artifact.Owner, "/IPv4StaticAddress/ds-lite-source") {
			if err := exec.Run("ifconfig", ifname, "destroy"); err != nil {
				return "", err
			}
			return "freebsd.gif.tunnel/" + ifname, nil
		}
		addr := strings.SplitN(address, "/", 2)[0]
		if err := exec.Run("ifconfig", ifname, "inet", addr, "-alias"); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	}
	return "", nil
}

func cleanupIPv6AddressArtifact(exec ArtifactTeardownExecutor, artifact resource.Artifact) (string, error) {
	if exec == nil {
		return "", nil
	}
	ifname, address, ok := strings.Cut(artifact.Name, ":")
	if !ok || ifname == "" || address == "" {
		return "", nil
	}
	features := exec.Features()
	if features.HasIproute2 {
		if err := exec.Run("ip", "-6", "addr", "del", address, "dev", ifname); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	}
	if features.HasPF {
		addr := strings.SplitN(address, "/", 2)[0]
		if err := exec.Run("ifconfig", ifname, "inet6", addr, "-alias"); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	}
	return "", nil
}

func IsDSLiteIPv4AddressArtifact(artifact resource.Artifact) bool {
	return strings.Contains(artifact.Owner, "/IPv4StaticAddress/ds-lite") ||
		strings.Contains(artifact.Name, ":192.168.160.249/32") ||
		strings.Contains(artifact.Name, ":192.168.160.250/32") ||
		strings.Contains(artifact.Name, ":192.168.160.251/32") ||
		strings.Contains(artifact.Name, ":192.168.160.252/32") ||
		strings.Contains(artifact.Name, ":172.18.255.249/32") ||
		strings.Contains(artifact.Name, ":172.18.255.250/32") ||
		strings.Contains(artifact.Name, ":172.18.255.251/32") ||
		strings.Contains(artifact.Name, ":172.18.255.252/32")
}

func IsStaticIPv6AddressArtifact(artifact resource.Artifact) bool {
	return strings.Contains(artifact.Owner, "/VirtualAddress/") &&
		strings.Contains(artifact.Name, ":") &&
		strings.Contains(artifact.Name, "/")
}

func IsPPPoEPeerFileArtifact(artifact resource.Artifact) bool {
	if !strings.Contains(artifact.Owner, "/PPPoESession/") {
		return false
	}
	name := filepath.Clean(artifact.Name)
	return strings.HasPrefix(name, "/etc/ppp/peers/routerd-")
}

func IsPPPoERuntimeSocketArtifact(artifact resource.Artifact) bool {
	if !strings.Contains(artifact.Owner, "/PPPoESession/") {
		return false
	}
	name := filepath.Clean(artifact.Name)
	return strings.HasPrefix(name, "/run/routerd/pppoe-client/") && strings.HasSuffix(name, ".sock")
}

func IsPPPoERuntimeDirectoryArtifact(artifact resource.Artifact) bool {
	if !strings.Contains(artifact.Owner, "/PPPoESession/") {
		return false
	}
	name := filepath.Clean(artifact.Name)
	return strings.HasPrefix(name, "/run/routerd/pppoe-client/") ||
		strings.HasPrefix(name, "/var/lib/routerd/pppoe-client/")
}

type IPv4FwmarkRule struct {
	Priority int
	Mark     int
	Table    int
}

func IPv4FwmarkRuleFromArtifact(artifact resource.Artifact) (IPv4FwmarkRule, bool) {
	priority, err := strconv.Atoi(artifact.Attributes["priority"])
	if err != nil || priority == 0 {
		return IPv4FwmarkRule{}, false
	}
	mark, err := strconv.ParseInt(artifact.Attributes["mark"], 0, 64)
	if err != nil || mark == 0 {
		return IPv4FwmarkRule{}, false
	}
	table, err := strconv.Atoi(artifact.Attributes["table"])
	if err != nil || table == 0 {
		return IPv4FwmarkRule{}, false
	}
	return IPv4FwmarkRule{Priority: priority, Mark: int(mark), Table: table}, true
}

func LabelForArtifact(artifact resource.Artifact) string {
	if artifact.Kind == "" {
		return ""
	}
	name := artifact.Name
	if artifact.Kind == "nft.table" && artifact.Attributes["name"] != "" {
		name = artifact.Attributes["name"]
	}
	if name == "" {
		return artifact.Kind
	}
	return artifact.Kind + "/" + name
}
