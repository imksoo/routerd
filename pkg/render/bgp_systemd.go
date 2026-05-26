// SPDX-License-Identifier: BSD-3-Clause

package render

import "github.com/imksoo/routerd/pkg/api"

const BGPUnitName = "routerd-bgp.service"

func BGPSystemdSpec(socketPath string) api.SystemdUnitSpec {
	noNewPrivileges := true
	privateTmp := true
	return api.SystemdUnitSpec{
		Description:              "routerd BGP daemon",
		ExecStart:                []string{"/usr/local/sbin/routerd-bgp", "daemon", "--socket", socketPath, "--control-socket", "/run/routerd/bgp/control.sock", "--state-file", "/var/lib/routerd/bgp/applied.json"},
		Wants:                    []string{"network-online.target"},
		After:                    []string{"network-online.target"},
		WantedBy:                 []string{"multi-user.target"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/bgp"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/bgp"},
		LogsDirectory:            []string{"routerd"},
		ReadWritePaths:           []string{"/run/routerd", "/var/lib/routerd", "/var/log/routerd"},
		AmbientCapabilities:      []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE"},
		CapabilityBoundingSet:    []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET", "AF_INET6", "AF_NETLINK"},
		ProtectSystem:            "strict",
		ProtectHome:              "yes",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}
