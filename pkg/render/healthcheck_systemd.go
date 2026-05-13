// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"strconv"
	"strings"

	"routerd/pkg/api"
)

type HealthCheckSystemdOptions struct {
	BinaryPath      string
	Resource        string
	Target          string
	Protocol        string
	Via             string
	FwMark          int
	SourceInterface string
	SourceAddress   string
	Port            int
	Interval        string
	Timeout         string
	SocketPath      string
	StateFile       string
	EventFile       string
	Environment     []string
}

type HealthCheckDaemonUnitOptions struct {
	Resource    string
	Spec        api.HealthCheckSpec
	Router      *api.Router
	Aliases     map[string]string
	Environment []string
	RuntimeRoot string
	StateRoot   string
	LogRoot     string
}

func HealthCheckDaemonSystemdSpec(options HealthCheckDaemonUnitOptions) api.SystemdUnitSpec {
	resource := options.Resource
	spec := options.Spec
	runtimeRoot := defaultString(options.RuntimeRoot, "/run")
	stateRoot := defaultString(options.StateRoot, "/var/lib")
	logRoot := defaultString(options.LogRoot, "/var/log")
	sourceInterface := strings.TrimSpace(spec.SourceInterface)
	if options.Aliases != nil && options.Aliases[sourceInterface] != "" {
		sourceInterface = options.Aliases[sourceInterface]
	}
	sourceAddress := strings.TrimSpace(spec.SourceAddress)
	if sourceAddress == "" && strings.TrimSpace(spec.SourceAddressFrom.Resource) != "" {
		if value, err := renderAddressFromResource(options.Router, spec.SourceAddressFrom); err == nil {
			sourceAddress = value
		}
	}
	socket := strings.TrimSpace(spec.SocketSource)
	if socket == "" {
		socket = runtimeRoot + "/routerd/healthcheck/" + resource + ".sock"
	}
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
	privateTmp := true
	capabilities := healthCheckCapabilities(spec.FwMark)
	return api.SystemdUnitSpec{
		Description:              "routerd healthcheck " + resource,
		ExecStart:                execStart,
		Environment:              options.Environment,
		Enabled:                  boolValuePtr(true),
		WantedBy:                 []string{"multi-user.target"},
		After:                    []string{"network-online.target"},
		Wants:                    []string{"network-online.target"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/healthcheck"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/healthcheck"},
		LogsDirectory:            []string{"routerd"},
		ReadWritePaths:           []string{runtimeRoot + "/routerd", stateRoot + "/routerd", logRoot + "/routerd"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET", "AF_INET6"},
		CapabilityBoundingSet:    capabilities,
		AmbientCapabilities:      capabilities,
		ProtectSystem:            "strict",
		ProtectHome:              "yes",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}

func boolValuePtr(value bool) *bool {
	return &value
}

func healthCheckCapabilities(fwmark int) []string {
	if fwmark != 0 {
		return []string{"CAP_NET_ADMIN", "CAP_NET_RAW"}
	}
	return []string{"CAP_NET_RAW"}
}

func HealthCheckSystemdUnit(options HealthCheckSystemdOptions) []byte {
	binaryPath := options.BinaryPath
	if binaryPath == "" {
		binaryPath = "/usr/local/sbin/routerd-healthcheck"
	}
	resource := options.Resource
	if resource == "" {
		resource = "%i"
	}
	args := "--resource " + strconv.Quote(resource)
	if options.Target != "" {
		args += " --target " + strconv.Quote(options.Target)
	}
	if options.Protocol != "" {
		args += " --protocol " + strconv.Quote(options.Protocol)
	}
	if options.Via != "" {
		args += " --via " + strconv.Quote(options.Via)
	}
	if options.FwMark != 0 {
		args += fmt.Sprintf(" --fwmark 0x%x", options.FwMark)
	}
	if options.SourceInterface != "" {
		args += " --source-interface " + strconv.Quote(options.SourceInterface)
	}
	if options.SourceAddress != "" {
		args += " --source-address " + strconv.Quote(options.SourceAddress)
	}
	if options.Port != 0 {
		args += fmt.Sprintf(" --port %d", options.Port)
	}
	if options.Interval != "" {
		args += " --interval " + strconv.Quote(options.Interval)
	}
	if options.Timeout != "" {
		args += " --timeout " + strconv.Quote(options.Timeout)
	}
	if options.SocketPath != "" {
		args += " --socket " + strconv.Quote(options.SocketPath)
	}
	if options.StateFile != "" {
		args += " --state-file " + strconv.Quote(options.StateFile)
	}
	if options.EventFile != "" {
		args += " --event-file " + strconv.Quote(options.EventFile)
	}

	capabilities := strings.Join(healthCheckCapabilities(options.FwMark), " ")
	return []byte(fmt.Sprintf(`[Unit]
Description=routerd healthcheck %s
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
%s
ExecStart=%s %s
Restart=always
RestartSec=5s
RuntimeDirectory=routerd/healthcheck
RuntimeDirectoryPreserve=yes
StateDirectory=routerd/healthcheck
LogsDirectory=routerd
NoNewPrivileges=yes
PrivateTmp=yes
ProtectHome=yes
ProtectSystem=strict
ReadWritePaths=/run/routerd /var/lib/routerd /var/log/routerd
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
CapabilityBoundingSet=%s
AmbientCapabilities=%s

[Install]
WantedBy=multi-user.target
`, resource, systemdEnvironmentLines(options.Environment), binaryPath, args, capabilities, capabilities))
}

func systemdEnvironmentLines(values []string) string {
	if len(values) == 0 {
		return ""
	}
	var b strings.Builder
	for _, value := range values {
		b.WriteString("Environment=")
		b.WriteString(strconv.Quote(value))
		b.WriteString("\n")
	}
	return b.String()
}
