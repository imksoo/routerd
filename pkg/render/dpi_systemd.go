// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"path/filepath"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	DPIClassifierUnitName = "routerd-dpi-classifier.service"
	NDPIAgentUnitName     = "routerd-ndpi-agent.service"
)

func NDPIAgentSystemdSpec(runtimeRoot string) api.SystemdUnitSpec {
	noNewPrivileges := true
	privateTmp := true
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
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET", "AF_INET6"},
		ProtectSystem:            "strict",
		ProtectHome:              "true",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}

func DPIClassifierSystemdSpec(runtimeRoot string) api.SystemdUnitSpec {
	noNewPrivileges := true
	socket := filepath.Join(runtimeRoot, "routerd/dpi-classifier/default.sock")
	agentSocket := filepath.Join(runtimeRoot, "routerd/ndpi-agent/default.sock")
	return api.SystemdUnitSpec{
		Description:             "routerd DPI classifier",
		ExecStart:               []string{"/usr/local/sbin/routerd-dpi-classifier", "daemon", "--socket", socket, "--engine", "auto", "--ndpi-agent-socket", agentSocket},
		Wants:                   []string{"network-online.target", NDPIAgentUnitName},
		After:                   []string{"network-online.target", NDPIAgentUnitName},
		WantedBy:                []string{"multi-user.target"},
		Restart:                 "on-failure",
		RuntimeDirectory:        []string{"routerd/dpi-classifier"},
		RestrictAddressFamilies: []string{"AF_UNIX"},
		NoNewPrivileges:         &noNewPrivileges,
	}
}

func MaybeAugmentDPIClassifierSpec(unitName string, spec api.SystemdUnitSpec, agentUnitName string) api.SystemdUnitSpec {
	if unitName != DPIClassifierUnitName || !SystemdUnitUsesNDPIAgent(spec) {
		return spec
	}
	spec.Wants = appendUniqueString(spec.Wants, agentUnitName)
	spec.After = appendUniqueString(spec.After, agentUnitName)
	return spec
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

func SystemdUnitUsesNDPIAgent(spec api.SystemdUnitSpec) bool {
	args := spec.ExecStart
	if len(args) == 0 || !strings.HasSuffix(filepath.Base(args[0]), "routerd-dpi-classifier") {
		return false
	}
	for i, arg := range args {
		if arg == "--engine" && i+1 < len(args) {
			return isNDPIAgentEngine(args[i+1])
		}
		if strings.HasPrefix(arg, "--engine=") {
			return isNDPIAgentEngine(strings.TrimPrefix(arg, "--engine="))
		}
	}
	return false
}

func isNDPIAgentEngine(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "auto", "ndpi-agent":
		return true
	default:
		return false
	}
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
