// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/dnsresolver"
)

func DNSResolverSystemdSpec(name string, spec api.DNSResolverSpec, binaryPath, configPath string) api.SystemdUnitSpec {
	_ = dnsresolver.NormalizeSpec(spec)
	if strings.TrimSpace(binaryPath) == "" {
		binaryPath = "/usr/local/sbin/routerd-dns-resolver"
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = "/var/lib/routerd/dns-resolver/" + name + "/config.json"
	}
	exec := []string{
		binaryPath,
		"daemon",
		"--resource", name,
		"--config-file", configPath,
	}
	noNewPrivileges := true
	privateTmp := true
	return api.SystemdUnitSpec{
		Description:             "routerd DNS resolver " + name,
		ExecStart:               exec,
		Wants:                   []string{"network-online.target"},
		After:                   []string{"network-online.target"},
		WantedBy:                []string{"multi-user.target"},
		Restart:                 "always",
		RestartSec:              "5s",
		RuntimeDirectory:        []string{"routerd/dns-resolver"},
		StateDirectory:          []string{"routerd/dns-resolver", "routerd/dns-resolver/" + name},
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
