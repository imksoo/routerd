// SPDX-License-Identifier: BSD-3-Clause

package render

import "github.com/imksoo/routerd/pkg/api"

const RouterdUnitName = "routerd.service"

func RouterdServiceSystemdSpec() api.SystemdUnitSpec {
	noNewPrivileges := false
	enabled := true
	started := true
	return api.SystemdUnitSpec{
		Description: "routerd daemon",
		ExecStart: []string{
			"/usr/local/sbin/routerd",
			"serve",
		},
		RuntimeDirectory:         []string{"routerd", "routerd/bgp", "routerd/dhcpv6-client", "routerd/dhcpv4-client", "routerd/pppoe-client", "routerd/dns-resolver"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd"},
		LogsDirectory:            []string{"routerd"},
		AmbientCapabilities:      []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE", "CAP_SETUID", "CAP_SETGID", "CAP_CHOWN"},
		CapabilityBoundingSet:    []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE", "CAP_SETUID", "CAP_SETGID", "CAP_CHOWN"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET", "AF_INET6", "AF_NETLINK"},
		NoNewPrivileges:          &noNewPrivileges,
		Enabled:                  &enabled,
		Started:                  &started,
	}
}

func RouterdServiceInitSpec() api.SystemdUnitSpec {
	spec := RouterdServiceSystemdSpec()
	spec.ExecStartPre = nil
	return spec
}

func RouterdServiceOpenRCSpec() api.SystemdUnitSpec {
	spec := RouterdServiceInitSpec()
	spec.ExecStart = []string{
		"/usr/local/sbin/routerd",
		"serve",
		"--config", "/usr/local/etc/routerd/router.yaml",
		"--socket", "/run/routerd/routerd.sock",
		"--status-socket", "/run/routerd/routerd-status.sock",
		"--apply-interval", "60s",
	}
	return spec
}
