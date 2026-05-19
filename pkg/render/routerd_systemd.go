// SPDX-License-Identifier: BSD-3-Clause

package render

import "routerd/pkg/api"

const RouterdUnitName = "routerd.service"

func RouterdServiceSystemdSpec() api.SystemdUnitSpec {
	noNewPrivileges := false
	enabled := true
	started := true
	return api.SystemdUnitSpec{
		Description: "routerd daemon",
		ExecStartPre: []string{
			"/usr/local/sbin/routerd",
			"check",
		},
		ExecStart: []string{
			"/usr/local/sbin/routerd",
			"serve",
		},
		RuntimeDirectory:         []string{"routerd", "routerd/dhcpv6-client", "routerd/dhcpv4-client", "routerd/pppoe-client", "routerd/dns-resolver"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd"},
		LogsDirectory:            []string{"routerd"},
		ReadWritePaths: []string{
			"/run/routerd",
			"/var/lib/routerd",
			"/var/log/routerd",
			"/var/lib/misc",
			"/etc/sysctl.d",
			"/etc/systemd/network",
			"/etc/systemd/resolved.conf.d",
			"/etc/systemd/system",
			"/usr/local/etc/routerd",
			"/var/cache/apt",
			"/var/lib/apt",
			"/var/lib/dpkg",
			"/var/log/apt",
		},
		AmbientCapabilities:     []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE", "CAP_SETUID", "CAP_SETGID", "CAP_CHOWN"},
		CapabilityBoundingSet:   []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE", "CAP_SETUID", "CAP_SETGID", "CAP_CHOWN"},
		RestrictAddressFamilies: []string{"AF_UNIX", "AF_INET", "AF_INET6", "AF_NETLINK"},
		ProtectSystem:           "no",
		NoNewPrivileges:         &noNewPrivileges,
		Enabled:                 &enabled,
		Started:                 &started,
	}
}
