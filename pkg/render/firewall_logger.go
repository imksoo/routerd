// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strconv"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func FirewallLoggerSystemdSpec(spec api.FirewallLogSpec) api.SystemdUnitSpec {
	defaults, _ := platform.Current()
	path := spec.Path
	if path == "" {
		path = defaults.FirewallLogFile()
	}
	group := spec.NFLogGroup
	if group == 0 {
		group = 1
	}
	exec := []string{"/usr/local/sbin/routerd-firewall-logger", "daemon", "--path", path, "--nflog-group", strconv.Itoa(group), "--dpi-socket", "/run/routerd/ndpi-agent/default.sock"}
	noNewPrivileges := true
	return api.SystemdUnitSpec{
		Description:              "routerd firewall log collector",
		ExecStart:                exec,
		Wants:                    []string{"network-online.target", NDPIAgentUnitName},
		After:                    []string{"network-online.target", NDPIAgentUnitName},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd"},
		LogsDirectory:            []string{"routerd"},
		NoNewPrivileges:          &noNewPrivileges,
	}
}
