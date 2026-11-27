// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"path/filepath"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	NDPIAgentUnitName = "routerd-ndpi-agent.service"
)

func NDPIAgentSystemdSpec(runtimeRoot string) api.SystemdUnitSpec {
	noNewPrivileges := true
	socket := filepath.Join(runtimeRoot, "routerd/ndpi-agent/default.sock")
	return api.SystemdUnitSpec{
		Description:              "routerd nDPI analysis agent",
		ExecStart:                []string{"/usr/local/sbin/routerd-ndpi-agent", "daemon", "--socket", socket},
		Wants:                    []string{"network-online.target"},
		After:                    []string{"network-online.target"},
		WantedBy:                 []string{"multi-user.target"},
		Restart:                  "on-failure",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/ndpi-agent"},
		RuntimeDirectoryPreserve: "yes",
		NoNewPrivileges:          &noNewPrivileges,
	}
}

func RouterWantsNDPIAgent(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "FirewallEventLog" {
			continue
		}
		spec, err := res.FirewallEventLogSpec()
		if err == nil && spec.Enabled {
			return true
		}
	}
	return false
}
