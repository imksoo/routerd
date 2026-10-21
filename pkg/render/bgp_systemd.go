// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

const BGPUnitName = "routerd-bgp.service"

func BGPSystemdSpec(socketPath string) api.SystemdUnitSpec {
	defaults, _ := platform.Current()
	_, control, state := defaults.BGPPaths()
	return BGPSystemdSpecWithPaths(socketPath, control, state)
}

func BGPSystemdSpecWithPaths(socketPath, controlSocketPath, statePath string) api.SystemdUnitSpec {
	noNewPrivileges := true
	return api.SystemdUnitSpec{
		Description:              "routerd BGP daemon",
		ExecStart:                []string{"/usr/local/sbin/routerd-bgp", "daemon", "--socket", socketPath, "--control-socket", controlSocketPath, "--state-file", statePath},
		Wants:                    []string{"network-online.target"},
		After:                    []string{"network-online.target"},
		WantedBy:                 []string{"multi-user.target"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/bgp"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/bgp"},
		LogsDirectory:            []string{"routerd"},
		AmbientCapabilities:      []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE"},
		CapabilityBoundingSet:    []string{"CAP_NET_ADMIN", "CAP_NET_RAW", "CAP_NET_BIND_SERVICE"},
		NoNewPrivileges:          &noNewPrivileges,
	}
}

// FreeBSDBGPSystemdSpec supplies FreeBSD's native runtime and state paths to
// the generic rc.d renderer.  The renderer consumes SystemdUnitSpec as a
// portable process description; it does not require systemd at runtime.
func FreeBSDBGPSystemdSpec() api.SystemdUnitSpec {
	unit := BGPSystemdSpec("/var/run/routerd/bgp/gobgp.sock")
	unit.ExecStart = []string{
		"/usr/local/sbin/routerd-bgp", "daemon",
		"--socket", "/var/run/routerd/bgp/gobgp.sock",
		"--control-socket", "/var/run/routerd/bgp/control.sock",
		"--state-file", "/var/db/routerd/bgp/applied.json",
	}
	return unit
}
