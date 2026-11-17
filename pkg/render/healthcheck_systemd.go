// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/healthcheck"
)

type HealthCheckDaemonUnitOptions struct {
	Resource    string
	Spec        api.HealthCheckSpec
	Router      *api.Router
	Store       healthcheck.Store
	Aliases     map[string]string
	Environment []string
	RuntimeRoot string
	StateRoot   string
	LogRoot     string
}

func HealthCheckDaemonSystemdSpec(options HealthCheckDaemonUnitOptions) api.SystemdUnitSpec {
	resource := options.Resource
	spec := healthcheck.ResolveSpecWithStoreForResource(options.Router, options.Store, resource, options.Spec)
	runtimeRoot := defaultString(options.RuntimeRoot, "/run")
	stateRoot := defaultString(options.StateRoot, "/var/lib")
	logRoot := defaultString(options.LogRoot, "/var/log")
	sourceInterface := strings.TrimSpace(spec.SourceInterface)
	if options.Aliases != nil && options.Aliases[sourceInterface] != "" {
		sourceInterface = options.Aliases[sourceInterface]
	}
	sourceAddress := strings.TrimSpace(spec.SourceAddress)
	if options.Store == nil && sourceAddress == "" && strings.TrimSpace(spec.SourceAddressFrom.Resource) != "" {
		if value, err := renderAddressFromResource(options.Router, spec.SourceAddressFrom); err == nil {
			sourceAddress = value
		}
	}
	socket := runtimeRoot + "/routerd/healthcheck/" + resource + ".sock"
	execStart := []string{"/usr/local/sbin/routerd-healthcheck", "daemon", "--resource", resource}
	appendFlag := func(flag, value string) {
		if strings.TrimSpace(value) != "" {
			execStart = append(execStart, flag, value)
		}
	}
	appendFlag("--target", spec.Target)
	appendFlag("--protocol", spec.Protocol)
	appendFlag("--address-family", spec.AddressFamily)
	appendFlag("--via", spec.Via)
	if spec.FwMark != 0 {
		execStart = append(execStart, "--fwmark", fmt.Sprintf("0x%x", spec.FwMark))
	}
	if healthcheck.ReferencesDSLiteTunnel(options.Router, resource) {
		execStart = append(execStart, "--require-dslite-binding")
	}
	appendFlag("--source-interface", sourceInterface)
	appendFlag("--source-address", sourceAddress)
	if spec.Port != 0 {
		execStart = append(execStart, "--port", strconv.Itoa(spec.Port))
	}
	appendFlag("--interval", spec.Interval)
	appendFlag("--timeout", spec.Timeout)
	if spec.HealthyThreshold != 0 {
		execStart = append(execStart, "--healthy-threshold", strconv.Itoa(spec.HealthyThreshold))
	}
	if spec.UnhealthyThreshold != 0 {
		execStart = append(execStart, "--unhealthy-threshold", strconv.Itoa(spec.UnhealthyThreshold))
	}
	execStart = append(execStart,
		"--socket", socket,
		"--state-file", stateRoot+"/routerd/healthcheck/"+resource+"/state.json",
		"--event-file", logRoot+"/routerd/healthcheck/"+resource+"/events.jsonl",
	)
	noNewPrivileges := true
	return api.SystemdUnitSpec{
		Description:              "routerd healthcheck " + resource,
		ExecStart:                execStart,
		Environment:              options.Environment,
		Enabled:                  boolValuePtr(true),
		WantedBy:                 []string{"multi-user.target"},
		After:                    []string{"routerd.service", "network-online.target"},
		BindsTo:                  []string{"routerd.service"},
		Wants:                    []string{"network-online.target"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/healthcheck"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/healthcheck"},
		LogsDirectory:            []string{"routerd"},
		CapabilityBoundingSet:    []string{"CAP_NET_ADMIN", "CAP_NET_RAW"},
		AmbientCapabilities:      []string{"CAP_NET_ADMIN", "CAP_NET_RAW"},
		NoNewPrivileges:          &noNewPrivileges,
	}
}

func boolValuePtr(value bool) *bool {
	return &value
}
