package render

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"routerd/pkg/api"
)

func freeBSDRCDScripts(router *api.Router) (map[string][]byte, error) {
	out := map[string][]byte{}
	explicit := map[string]bool{}
	telemetryEnv, err := TelemetryEnvironment(router)
	if err != nil {
		return nil, err
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "SystemdUnit" {
			continue
		}
		spec, err := res.SystemdUnitSpec()
		if err != nil {
			return nil, err
		}
		if defaultString(spec.State, "present") == "absent" {
			continue
		}
		name := freeBSDServiceName(defaultString(spec.UnitName, res.Metadata.Name))
		explicit[name] = true
		spec.Environment = mergeEnvironment(spec.Environment, telemetryEnv)
		data, err := FreeBSDRCDScript(name, spec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	aliases := linkAliases(router)
	for _, res := range router.Spec.Resources {
		if res.Kind != "HealthCheck" {
			continue
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return nil, err
		}
		if spec.Daemon != "routerd-healthcheck" {
			continue
		}
		name := freeBSDServiceName("routerd-healthcheck@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		unit := HealthCheckDaemonSystemdSpec(HealthCheckDaemonUnitOptions{
			Resource:    res.Metadata.Name,
			Spec:        spec,
			Aliases:     aliases,
			Environment: telemetryEnv,
			RuntimeRoot: "/var/run",
			StateRoot:   "/var/db",
			LogRoot:     "/var/log",
		})
		data, err := FreeBSDRCDScript(name, unit)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "TailscaleNode" {
			continue
		}
		spec, err := res.TailscaleNodeSpec()
		if err != nil {
			return nil, err
		}
		if firstNonEmpty(spec.State, "present") == "absent" {
			continue
		}
		spec.BinaryPath = firstNonEmpty(spec.BinaryPath, "/usr/local/bin/tailscale")
		name := freeBSDServiceName(TailscaleUnitName(res.Metadata.Name))
		if explicit[name] {
			continue
		}
		data, err := FreeBSDRCDScript(name, TailscaleSystemdSpec(res.Metadata.Name, spec))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "FirewallLog" {
			continue
		}
		spec, err := res.FirewallLogSpec()
		if err != nil {
			return nil, err
		}
		if !spec.Enabled {
			continue
		}
		name := freeBSDServiceName("routerd-firewall-logger.service")
		if explicit[name] {
			continue
		}
		data, err := FreeBSDRCDScript(name, freeBSDFirewallLoggerSystemdSpec(spec))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	return out, nil
}

func freeBSDFirewallLoggerSystemdSpec(spec api.FirewallLogSpec) api.SystemdUnitSpec {
	path := spec.Path
	if path == "" {
		path = "/var/db/routerd/firewall-logs.db"
	}
	noNewPrivileges := true
	privateTmp := true
	return api.SystemdUnitSpec{
		Description:              "routerd firewall log collector",
		ExecStart:                []string{"/usr/local/sbin/routerd-firewall-logger", "daemon", "--path", path, "--pflog-interface", "pflog0"},
		Wants:                    []string{"NETWORKING"},
		After:                    []string{"NETWORKING"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd"},
		LogsDirectory:            []string{"routerd"},
		ReadWritePaths:           []string{"/var/db/routerd", "/var/log/routerd"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET", "AF_INET6"},
		ProtectSystem:            "strict",
		ProtectHome:              "true",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}

func FreeBSDRCDScript(name string, spec api.SystemdUnitSpec) ([]byte, error) {
	if len(spec.ExecStart) == 0 {
		return nil, fmt.Errorf("execStart is required")
	}
	name = freeBSDServiceName(name)
	var buf bytes.Buffer
	buf.WriteString("#!/bin/sh\n")
	buf.WriteString("#\n")
	buf.WriteString("# PROVIDE: " + name + "\n")
	buf.WriteString("# REQUIRE: NETWORKING\n")
	buf.WriteString("# KEYWORD: shutdown\n")
	buf.WriteString("\n")
	buf.WriteString(". /etc/rc.subr\n\n")
	buf.WriteString("name=" + shellSingleQuote(name) + "\n")
	buf.WriteString("rcvar=\"${name}_enable\"\n")
	if spec.Type == "oneshot" {
		if len(spec.Environment) > 0 {
			buf.WriteString("command=\"/usr/bin/env\"\n")
			buf.WriteString("command_args=\"")
			for _, env := range spec.Environment {
				buf.WriteString(" " + shellSingleQuote(env))
			}
			for _, arg := range spec.ExecStart {
				buf.WriteString(" " + shellSingleQuote(arg))
			}
			buf.WriteString("\"\n")
		} else {
			buf.WriteString("command=" + shellSingleQuote(spec.ExecStart[0]) + "\n")
			if len(spec.ExecStart) > 1 {
				buf.WriteString("command_args=\"")
				for _, arg := range spec.ExecStart[1:] {
					buf.WriteString(" " + shellSingleQuote(arg))
				}
				buf.WriteString("\"\n")
			}
		}
	} else {
		buf.WriteString("pidfile=\"/var/run/${name}.pid\"\n")
		buf.WriteString("command=\"/usr/sbin/daemon\"\n")
		buf.WriteString("procname=" + shellSingleQuote(spec.ExecStart[0]) + "\n")
		buf.WriteString("command_args=\"-P ${pidfile} -r -f --")
		if len(spec.Environment) > 0 {
			buf.WriteString(" env")
			for _, env := range spec.Environment {
				buf.WriteString(" " + shellSingleQuote(env))
			}
		}
		for _, arg := range spec.ExecStart {
			buf.WriteString(" " + shellSingleQuote(arg))
		}
		buf.WriteString("\"\n")
	}
	if len(spec.RuntimeDirectory) > 0 || len(spec.StateDirectory) > 0 || len(spec.LogsDirectory) > 0 {
		buf.WriteString("start_precmd=\"${name}_prestart\"\n")
	}
	buf.WriteString("\n")
	if len(spec.RuntimeDirectory) > 0 || len(spec.StateDirectory) > 0 || len(spec.LogsDirectory) > 0 {
		buf.WriteString(name + "_prestart() {\n")
		for _, dir := range freeBSDServiceDirs("/var/run", spec.RuntimeDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		for _, dir := range freeBSDServiceDirs("/var/db", spec.StateDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		for _, dir := range freeBSDServiceDirs("/var/log", spec.LogsDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		buf.WriteString("}\n\n")
	}
	buf.WriteString("load_rc_config $name\n")
	buf.WriteString(": ${" + name + "_enable:=\"")
	if api.BoolDefault(spec.Enabled, true) {
		buf.WriteString("YES")
	} else {
		buf.WriteString("NO")
	}
	buf.WriteString("\"}\n")
	buf.WriteString("run_rc_command \"$1\"\n")
	return buf.Bytes(), nil
}

func freeBSDServiceDirs(root string, values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.Trim(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		out = append(out, root+"/"+value)
	}
	sort.Strings(out)
	return out
}

var unsafeFreeBSDServiceName = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func freeBSDServiceName(value string) string {
	value = strings.TrimSuffix(value, ".service")
	value = unsafeFreeBSDServiceName.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "routerd_service"
	}
	return value
}

func sortedByteMapKeys(values map[string][]byte) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
