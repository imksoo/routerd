// SPDX-License-Identifier: BSD-3-Clause

package render

import "github.com/imksoo/routerd/pkg/api"

const RouterdUnitName = "routerd.service"

// The native dnsmasq owner must inspect and signal its helper after the helper
// drops UID. CAP_SYS_PTRACE permits exact procfs identity checks; CAP_KILL
// permits pidfd signaling across UIDs. Neither bypasses the ownership checks.
func RouterdServiceSystemdSpec() api.SystemdUnitSpec {
	noNewPrivileges := false
	enabled := true
	started := true
	return api.SystemdUnitSpec{
		Description: "routerd daemon",
		Type:        "notify",
		ExecStart: []string{
			"/usr/local/sbin/routerd",
			"serve",
		},
		RuntimeDirectory:         []string{"routerd", "routerd/bgp", "routerd/dhcpv6-client", "routerd/dhcpv4-client", "routerd/pppoe-client", "routerd/dns-resolver"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd"},
		LogsDirectory:            []string{"routerd"},
		AmbientCapabilities:      []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE", "CAP_SETUID", "CAP_SETGID", "CAP_CHOWN", "CAP_SYS_PTRACE", "CAP_KILL"},
		CapabilityBoundingSet:    []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE", "CAP_SETUID", "CAP_SETGID", "CAP_CHOWN", "CAP_SYS_PTRACE", "CAP_KILL"},
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
