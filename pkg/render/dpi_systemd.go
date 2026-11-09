// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"path/filepath"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	DPIClassifierUnitName = "routerd-dpi-classifier.service"
	NDPIAgentUnitName     = "routerd-ndpi-agent.service"
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

func DPIClassifierSystemdSpec(runtimeRoot string) api.SystemdUnitSpec {
	noNewPrivileges := true
	socket := filepath.Join(runtimeRoot, "routerd/dpi-classifier/default.sock")
	agentSocket := filepath.Join(runtimeRoot, "routerd/ndpi-agent/default.sock")
	return api.SystemdUnitSpec{
		Description:      "routerd DPI classifier",
		ExecStart:        []string{"/usr/local/sbin/routerd-dpi-classifier", "daemon", "--socket", socket, "--engine", "auto", "--ndpi-agent-socket", agentSocket},
		Wants:            []string{"network-online.target", NDPIAgentUnitName},
		After:            []string{"network-online.target", NDPIAgentUnitName},
		WantedBy:         []string{"multi-user.target"},
		Restart:          "on-failure",
		RuntimeDirectory: []string{"routerd/dpi-classifier"},
		NoNewPrivileges:  &noNewPrivileges,
	}
}

func RouterWantsNDPIAgent(router *api.Router) bool {
	return RouterWantsDPIClassifier(router)
}

func RouterWantsDPIClassifier(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "TrafficFlowLog":
			spec, err := res.TrafficFlowLogSpec()
			if err == nil && spec.Enabled && (spec.IncludeApplicationLayer || spec.IncludeTLSSNI) {
				return true
			}
		case "FirewallEventLog":
			spec, err := res.FirewallEventLogSpec()
			if err == nil && spec.Enabled {
				return true
			}
		}
	}
	return false
}
