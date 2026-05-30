// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// EventdSystemdSpec renders the systemd unit for one CloudEdge Event Federation
// daemon (routerd-eventd@<group>), mirroring DNSResolverSystemdSpec. The unit is
// long-lived (Restart=always) and reads its runtime config from configPath.
func EventdSystemdSpec(group, binaryPath, configPath string) api.SystemdUnitSpec {
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = "/usr/local/sbin/routerd-eventd"
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = "/var/lib/routerd/eventd/" + group + "/config.json"
	}
	exec := []string{
		binaryPath,
		"daemon",
		"--config-file", configPath,
	}
	noNewPrivileges := true
	privateTmp := true
	return api.SystemdUnitSpec{
		Description:             "routerd event federation " + group,
		ExecStart:               exec,
		Wants:                   []string{"network-online.target"},
		After:                   []string{"network-online.target"},
		WantedBy:                []string{"multi-user.target"},
		Restart:                 "always",
		RestartSec:              "5s",
		RuntimeDirectory:        []string{"routerd/eventd"},
		StateDirectory:          []string{"routerd/eventd", "routerd/eventd/" + group},
		LogsDirectory:           []string{"routerd"},
		ReadWritePaths:          []string{"/run/routerd", "/var/lib/routerd", "/var/log/routerd"},
		RestrictAddressFamilies: []string{"AF_UNIX", "AF_INET", "AF_INET6"},
		CapabilityBoundingSet:   []string{"CAP_NET_BIND_SERVICE"},
		AmbientCapabilities:     []string{"CAP_NET_BIND_SERVICE"},
		ProtectSystem:           "strict",
		ProtectHome:             "yes",
		NoNewPrivileges:         &noNewPrivileges,
		PrivateTmp:              &privateTmp,
	}
}
