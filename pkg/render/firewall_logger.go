// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strconv"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func FirewallLoggerSystemdSpec(spec api.FirewallLogSpec, dpiSocket string) api.SystemdUnitSpec {
	defaults, _ := platform.Current()
	path := spec.Path
	if path == "" {
		path = defaults.FirewallLogFile()
	}
	group := spec.NFLogGroup
	if group == 0 {
		group = 1
	}
	exec := []string{"/usr/local/sbin/routerd-firewall-logger", "daemon", "--path", path, "--nflog-group", strconv.Itoa(group)}
	wants := []string{"network-online.target"}
	after := []string{"network-online.target"}
	if dpiSocket != "" {
		exec = append(exec, "--dpi-socket", dpiSocket)
		wants = append(wants, "routerd-dpi-classifier.service")
		after = append(after, "routerd-dpi-classifier.service")
	}
	noNewPrivileges := true
	return api.SystemdUnitSpec{
		Description:              "routerd firewall log collector",
		ExecStart:                exec,
		Wants:                    wants,
		After:                    after,
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd"},
		LogsDirectory:            []string{"routerd"},
		NoNewPrivileges:          &noNewPrivileges,
	}
}
