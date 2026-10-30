// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func freeBSDRCDScripts(router *api.Router) (map[string][]byte, error) {
	out := map[string][]byte{}
	explicit := map[string]bool{}
	telemetryEnv, err := TelemetryEnvironment(router)
	if err != nil {
		return nil, err
	}
	renderService := func(name string, spec api.SystemdUnitSpec) ([]byte, error) {
		spec.Environment = mergeEnvironment(spec.Environment, telemetryEnv)
		return FreeBSDRCDScript(name, spec)
	}
	routerdSpec := RouterdServiceInitSpec()
	routerdData, err := renderService("routerd", routerdSpec)
	if err != nil {
		return nil, err
	}
	out["routerd"] = routerdData
	explicit["routerd"] = true
	if freeBSDRouterHasBGP(router) {
		name := freeBSDServiceName(BGPUnitName)
		data, err := renderService(name, FreeBSDBGPSystemdSpec())
		if err != nil {
			return nil, err
		}
		out[name] = data
	}
	dpiSocket := ""
	if RouterWantsDPIClassifier(router) {
		dpiSocket = "/var/run/routerd/dpi-classifier/default.sock"
	}
	wantsNDPIAgent := RouterWantsNDPIAgent(router)
	if RouterWantsDPIClassifier(router) {
		name := freeBSDServiceName(DPIClassifierUnitName)
		data, err := renderService(name, DPIClassifierSystemdSpec("/var/run"))
		if err != nil {
			return nil, err
		}
		out[name] = data
	}
	if wantsNDPIAgent {
		name := freeBSDServiceName(NDPIAgentUnitName)
		if !explicit[name] {
			data, err := renderService(name, NDPIAgentSystemdSpec("/var/run"))
			if err != nil {
				return nil, err
			}
			out[name] = data
		}
	}
	synthesizeClientDaemons := !freeBSDRouterdSupervisesClientDaemons(router)
	aliases, err := nftOutboundAliases(router)
	if err != nil {
		return nil, err
	}
	if synthesizeClientDaemons {
		for _, res := range router.Spec.Resources {
			if res.Kind != "DHCPv6PrefixDelegation" {
				continue
			}
			spec, err := res.DHCPv6PrefixDelegationSpec()
			if err != nil {
				return nil, err
			}
			ifname := aliases[spec.Interface]
			if ifname == "" {
				return nil, fmt.Errorf("%s references interface %q with no ifname", res.ID(), spec.Interface)
			}
			name := freeBSDServiceName("routerd-dhcpv6-client@" + res.Metadata.Name + ".service")
			if explicit[name] {
				continue
			}
			data, err := renderService(name, freeBSDDHCPv6ClientSystemdSpec(res.Metadata.Name, ifname, spec, telemetryEnv))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", res.ID(), err)
			}
			out[name] = data
		}
		for _, res := range router.Spec.Resources {
			if res.Kind != "DHCPv4Client" {
				continue
			}
			spec, err := res.DHCPv4ClientSpec()
			if err != nil {
				return nil, err
			}
			ifname := aliases[spec.Interface]
			if ifname == "" {
				return nil, fmt.Errorf("%s references interface %q with no ifname", res.ID(), spec.Interface)
			}
			name := freeBSDServiceName("routerd-dhcpv4-client@" + res.Metadata.Name + ".service")
			if explicit[name] {
				continue
			}
			data, err := renderService(name, freeBSDDHCPv4ClientSystemdSpec(res.Metadata.Name, ifname, spec, telemetryEnv))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", res.ID(), err)
			}
			out[name] = data
		}
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "HealthCheck" {
			continue
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return nil, err
		}
		name := freeBSDServiceName("routerd-healthcheck@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		unit, err := HealthCheckDaemonSystemdSpec(HealthCheckDaemonUnitOptions{
			Resource:    res.Metadata.Name,
			Spec:        spec,
			Router:      router,
			Environment: telemetryEnv,
			RuntimeRoot: "/var/run",
			StateRoot:   "/var/db",
			LogRoot:     "/var/log",
		})
		if err != nil {
			return nil, err
		}
		data, err := renderService(name, unit)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "DNSResolver" {
			continue
		}
		spec, err := res.DNSResolverSpec()
		if err != nil {
			return nil, err
		}
		name := freeBSDServiceName("routerd-dns-resolver@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		data, err := renderService(name, freeBSDDNSResolverSystemdSpec(res.Metadata.Name, spec))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "EventGroup" {
			continue
		}
		spec, err := res.EventGroupSpec()
		if err != nil {
			return nil, err
		}
		if !freeBSDEventGroupRunsEventd(spec) {
			continue
		}
		name := freeBSDServiceName("routerd-eventd@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		data, err := renderService(name, freeBSDEventdSystemdSpec(res.Metadata.Name))
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
		if stringutil.FirstNonBlank(spec.State, "present") == "absent" {
			continue
		}
		spec.BinaryPath = stringutil.FirstNonBlank(spec.BinaryPath, "/usr/local/bin/tailscale")
		name := freeBSDServiceName(TailscaleUnitName(res.Metadata.Name))
		if explicit[name] {
			continue
		}
		out[name] = FreeBSDTailscaleRCDScript(name, spec)
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "FirewallEventLog" {
			continue
		}
		spec, err := res.FirewallEventLogSpec()
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
		data, err := renderService(name, freeBSDFirewallLoggerSystemdSpec(spec, dpiSocket))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	return out, nil
}

func FreeBSDTailscaleRCDScript(name string, spec api.TailscaleNodeSpec) []byte {
	name = freeBSDServiceName(name)
	args := TailscaleUpArgs(spec)
	authKeyEnv := tailscaleAuthKeyEnv(spec)
	var b bytes.Buffer
	b.WriteString("#!/bin/sh\n# Managed by routerd.\n")
	b.WriteString("# PROVIDE: " + name + "\n# REQUIRE: NETWORKING\n# KEYWORD: shutdown\n\n. /etc/rc.subr\n\n")
	b.WriteString("name=" + shellSingleQuote(name) + "\nrcvar=\"${name}_enable\"\n")
	b.WriteString("marker_dir=" + shellSingleQuote("/var/run/routerd/tailscale") + "\nmarker=\"${marker_dir}/" + name + ".owner\"\n")
	b.WriteString("pidfile=\"${TAILSCALED_PIDFILE:-/var/run/tailscaled.pid}\"\n")
	if authKeyEnv != "" {
		b.WriteString("auth_key_env=" + shellSingleQuote(authKeyEnv) + "\nauth_key_file=" + shellSingleQuote(spec.AuthKeyFile) + "\n")
		b.WriteString(name + "_load_auth_key() {\n  auth_key=\n  if [ -n \"${auth_key_file}\" ]; then\n    [ -r \"${auth_key_file}\" ] || { echo \"tailscale auth-key file is unreadable\" >&2; return 1; }\n    auth_key_found=0\n    while IFS= read -r line || [ -n \"${line}\" ]; do\n      case \"${line}\" in\n        \"${auth_key_env}\"=*)\n          [ \"${auth_key_found}\" -eq 0 ] || { echo \"tailscale auth-key file has duplicate selected variable\" >&2; return 1; }\n          auth_key=${line#*=}\n          auth_key_found=1\n          ;;\n      esac\n    done < \"${auth_key_file}\"\n    [ \"${auth_key_found}\" -eq 1 ] || { echo \"tailscale auth-key variable is missing\" >&2; return 1; }\n  else\n    auth_key=$(/usr/bin/printenv \"${auth_key_env}\") || { echo \"tailscale auth-key environment variable is missing\" >&2; return 1; }\n  fi\n  [ -n \"${auth_key}\" ] || { echo \"tailscale auth-key is empty\" >&2; return 1; }\n}\n\n")
	}
	b.WriteString("start_cmd=\"${name}_start\"\nstop_cmd=\"${name}_stop\"\nstatus_cmd=\"${name}_status\"\n\n")
	b.WriteString(name + "_tailscaled_running() {\n  [ -r \"${pidfile}\" ] || return 1\n  tailscaled_pid=\n  IFS= read -r tailscaled_pid < \"${pidfile}\" || [ -n \"${tailscaled_pid}\" ] || return 1\n  case \"${tailscaled_pid}\" in ''|*[!0-9]*) return 1 ;; esac\n  kill -0 \"${tailscaled_pid}\" 2>/dev/null\n}\n\n")
	b.WriteString(name + "_clear_stale_tailscaled_pidfile() {\n  [ -e \"${pidfile}\" ] || return 0\n  tailscaled_pid=\n  IFS= read -r tailscaled_pid < \"${pidfile}\" || [ -n \"${tailscaled_pid}\" ] || tailscaled_pid=\n  case \"${tailscaled_pid}\" in ''|*[!0-9]*) rm -f \"${pidfile}\"; return 0 ;; esac\n  kill -0 \"${tailscaled_pid}\" 2>/dev/null || rm -f \"${pidfile}\"\n}\n\n")
	b.WriteString(name + "_rollback() {\n  [ \"${started_here:-0}\" = 1 ] || return 0\n  /usr/bin/timeout -k 2 15 service tailscaled onestop >/dev/null 2>&1\n  stop_rc=$?\n  if " + name + "_tailscaled_running; then\n    echo \"tailscaled service stop failed; retaining ownership marker\" >&2\n    [ \"${stop_rc}\" -ne 0 ] || stop_rc=1\n    return \"${stop_rc}\"\n  fi\n  rm -f \"${marker}\" \"${marker}.$$\"\n}\n\n")
	b.WriteString(name + "_start() {\n  started_here=0\n")
	if authKeyEnv != "" {
		b.WriteString("  " + name + "_load_auth_key || return 1\n")
	}
	b.WriteString("  " + name + "_clear_stale_tailscaled_pidfile\n")
	b.WriteString("  if " + name + "_tailscaled_running; then\n    [ -r \"${marker}\" ] || { echo \"foreign tailscaled service is already running; refusing mutation\" >&2; return 1; }\n    read owned < \"${marker}\"\n    [ \"${owned}\" = \"${name}\" ] || { echo \"routerd ownership marker mismatch\" >&2; return 1; }\n  else\n    if ! mkdir -p \"${marker_dir}\" || ! printf '%s\\n' \"${name}\" > \"${marker}.$$\" || ! mv -f \"${marker}.$$\" \"${marker}\"; then\n      echo \"unable to publish routerd tailscaled ownership marker\" >&2\n      return 1\n    fi\n    started_here=1\n    service tailscaled onestart >/dev/null 2>&1 &\n    launcher_pid=$!\n    launcher_done=0\n    deadline=$(( $(date +%s) + 15 ))\n    ready=0\n    while [ \"$(date +%s)\" -lt \"${deadline}\" ]; do\n      if " + name + "_tailscaled_running; then ready=1; break; fi\n      if [ \"${launcher_done}\" -eq 0 ] && ! kill -0 \"${launcher_pid}\" 2>/dev/null; then\n        wait \"${launcher_pid}\"; rc=$?\n        if [ \"${rc}\" -ne 0 ]; then\n          " + name + "_rollback || true\n          echo \"tailscaled service exited before readiness\" >&2\n          return \"${rc}\"\n        fi\n        launcher_done=1\n        launcher_pid=\n      fi\n      sleep 1\n    done\n    if [ \"${ready}\" -ne 1 ]; then\n      if [ \"${launcher_done}\" -eq 0 ]; then\n        kill \"${launcher_pid}\" 2>/dev/null || true\n        wait \"${launcher_pid}\" 2>/dev/null || true\n      fi\n      " + name + "_rollback || true\n      echo \"tailscaled service readiness timed out\" >&2\n      return 124\n    fi\n  fi\n  /usr/bin/timeout -k 2 45 ")
	for i, arg := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if authKeyEnv != "" && arg == "--auth-key=${"+authKeyEnv+"}" {
			b.WriteString("\"--auth-key=${auth_key}\"")
		} else {
			b.WriteString(shellSingleQuote(arg))
		}
	}
	b.WriteString(" >/dev/null 2>&1\n  rc=$?\n  if [ \"${rc}\" -ne 0 ]; then\n    " + name + "_rollback || true\n    echo \"tailscale enrollment failed\" >&2\n    return \"${rc}\"\n  fi\n}\n\n")
	// tailscale up has a CLI-native timeout, while the service lifecycle needs
	// a separate FreeBSD base timeout bound.
	b.WriteString(name + "_stop() {\n  [ -r \"${marker}\" ] || { echo \"foreign tailscaled service ownership is unknown; refusing mutation\" >&2; return 1; }\n  read owned < \"${marker}\"\n  [ \"${owned}\" = \"${name}\" ] || { echo \"routerd ownership marker mismatch\" >&2; return 1; }\n  /usr/bin/timeout -k 2 45 " + shellSingleQuote(args[0]) + " down >/dev/null 2>&1\n  rc=$?\n  if [ \"${rc}\" -ne 0 ]; then\n    started_here=1\n    " + name + "_rollback || true\n    echo \"tailscale shutdown failed\" >&2\n    return \"${rc}\"\n  fi\n  started_here=1\n  " + name + "_rollback\n}\n\n")
	b.WriteString(name + "_status() {\n  [ -r \"${marker}\" ] && " + name + "_tailscaled_running\n}\n\nload_rc_config $name\n: ${" + name + "_enable:=\"YES\"}\nrun_rc_command \"$1\"\n")
	return b.Bytes()
}

func freeBSDInterfacePresenceFunction(service, ifname string) string {
	return service + "_present() {\n  target_interface=${1:-" + shellSingleQuote(ifname) + "}\n  native_interfaces=$(/sbin/ifconfig -l) || return 2\n  for native_interface in ${native_interfaces}; do\n    [ \"${native_interface}\" != \"${target_interface}\" ] || return 0\n  done\n  return 1\n}\n\n"
}

func freeBSDRouterHasBGP(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.NetAPIVersion && (res.Kind == "BGPRouter" || res.Kind == "BGPPeer") {
			return true
		}
	}
	return false
}

func freeBSDDNSResolverSystemdSpec(name string, spec api.DNSResolverSpec) api.SystemdUnitSpec {
	unit := DNSResolverSystemdSpec(name, spec, "/usr/local/sbin/routerd-dns-resolver", "/var/db/routerd/dns-resolver/"+name+"/config.json")
	unit.ExecStart = append(unit.ExecStart,
		"--socket", "/var/run/routerd/dns-resolver/"+name+".sock",
		"--state-file", "/var/db/routerd/dns-resolver/"+name+"/state.json",
		"--event-file", "/var/db/routerd/dns-resolver/"+name+"/events.jsonl",
	)
	return unit
}

func freeBSDEventdSystemdSpec(group string) api.SystemdUnitSpec {
	return EventdSystemdSpec(group, "/usr/local/sbin/routerd-eventd", "/var/db/routerd/eventd/"+group+"/config.json")
}

func freeBSDEventGroupRunsEventd(spec api.EventGroupSpec) bool {
	return strings.TrimSpace(spec.Auth.SecretFile) != ""
}

func freeBSDRouterdSupervisesClientDaemons(router *api.Router) bool {
	return true
}

func containsFreeBSDArg(args []string, needle string) bool {
	for _, arg := range args {
		if arg == needle {
			return true
		}
	}
	return false
}

func freeBSDBoolFlagValue(args []string, flag string, defaultValue bool) bool {
	for i, arg := range args {
		if arg == flag {
			if i+1 >= len(args) {
				return true
			}
			switch strings.ToLower(strings.TrimSpace(args[i+1])) {
			case "false", "0", "no", "off":
				return false
			case "true", "1", "yes", "on":
				return true
			default:
				return true
			}
		}
		if strings.HasPrefix(arg, flag+"=") {
			value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, flag+"=")))
			switch value {
			case "false", "0", "no", "off":
				return false
			case "true", "1", "yes", "on":
				return true
			default:
				return defaultValue
			}
		}
	}
	return defaultValue
}

func freeBSDDHCPv6ClientSystemdSpec(resource, ifname string, spec api.DHCPv6PrefixDelegationSpec, telemetryEnv []string) api.SystemdUnitSpec {
	args := []string{
		"/usr/local/sbin/routerd-dhcpv6-client",
		"--resource", resource,
		"--interface", ifname,
		"--socket", "/var/run/routerd/dhcpv6-client/" + resource + ".sock",
		"--lease-file", "/var/db/routerd/dhcpv6-client/" + resource + "/lease.json",
		"--event-file", "/var/db/routerd/dhcpv6-client/" + resource + "/events.jsonl",
	}
	if spec.IAID != "" {
		args = append(args, "--iaid", spec.IAID)
	}
	if spec.ClientDUID != "" {
		args = append(args, "--client-duid", spec.ClientDUID)
	}
	noNewPrivileges := true
	return api.SystemdUnitSpec{
		Description:              "routerd DHCPv6-PD client " + resource,
		ExecStart:                args,
		Environment:              telemetryEnv,
		Wants:                    []string{"NETWORKING"},
		After:                    []string{"NETWORKING"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd", "routerd/dhcpv6-client"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd", "routerd/dhcpv6-client", "routerd/dhcpv6-client/" + resource},
		LogsDirectory:            []string{"routerd"},
		NoNewPrivileges:          &noNewPrivileges,
	}
}

func freeBSDDHCPv4ClientSystemdSpec(resource, ifname string, spec api.DHCPv4ClientSpec, telemetryEnv []string) api.SystemdUnitSpec {
	args := []string{
		"/usr/local/sbin/routerd-dhcpv4-client",
		"daemon",
		"--resource", resource,
		"--interface", ifname,
		"--socket", "/var/run/routerd/dhcpv4-client/" + resource + ".sock",
		"--lease-file", "/var/db/routerd/dhcpv4-client/" + resource + "/lease.json",
		"--event-file", "/var/db/routerd/dhcpv4-client/" + resource + "/events.jsonl",
	}
	if spec.Hostname != "" {
		args = append(args, "--hostname", spec.Hostname)
	}
	if spec.RequestedAddress != "" {
		args = append(args, "--requested-address", spec.RequestedAddress)
	}
	if spec.ClassID != "" {
		args = append(args, "--class-id", spec.ClassID)
	}
	if spec.ClientID != "" {
		args = append(args, "--client-id", spec.ClientID)
	}
	noNewPrivileges := true
	return api.SystemdUnitSpec{
		Description:              "routerd DHCPv4 client " + resource,
		ExecStart:                args,
		Environment:              telemetryEnv,
		Wants:                    []string{"NETWORKING"},
		After:                    []string{"NETWORKING"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd", "routerd/dhcpv4-client"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd", "routerd/dhcpv4-client", "routerd/dhcpv4-client/" + resource},
		LogsDirectory:            []string{"routerd"},
		NoNewPrivileges:          &noNewPrivileges,
	}
}

func freeBSDFirewallLoggerSystemdSpec(spec api.FirewallLogSpec, dpiSocket string) api.SystemdUnitSpec {
	path := spec.Path
	if path == "" {
		path = (platform.Defaults{StateDir: "/var/db/routerd"}).FirewallLogFile()
	}
	exec := []string{"/usr/local/sbin/routerd-firewall-logger", "daemon", "--path", path, "--pflog-interface", "pflog0"}
	wants := []string{"NETWORKING"}
	after := []string{"NETWORKING"}
	if dpiSocket != "" {
		exec = append(exec, "--dpi-socket", dpiSocket)
		wants = append(wants, "routerd_dpi_classifier")
		after = append(after, "routerd_dpi_classifier")
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

func FreeBSDRCDScript(name string, spec api.SystemdUnitSpec) ([]byte, error) {
	if len(spec.ExecStart) == 0 {
		return nil, fmt.Errorf("execStart is required")
	}
	name = freeBSDServiceName(name)
	execStartPre := spec.ExecStartPre
	var buf bytes.Buffer
	buf.WriteString("#!/bin/sh\n")
	buf.WriteString("#\n")
	buf.WriteString("# Managed by routerd.\n")
	buf.WriteString("#\n")
	buf.WriteString("# PROVIDE: " + name + "\n")
	buf.WriteString("# REQUIRE: " + strings.Join(freeBSDRCDRequires(spec), " ") + "\n")
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
				buf.WriteString(shellDoubleQuoteContents(" " + shellSingleQuote(env)))
			}
			for _, arg := range spec.ExecStart {
				buf.WriteString(shellDoubleQuoteContents(" " + shellSingleQuote(arg)))
			}
			buf.WriteString("\"\n")
		} else {
			buf.WriteString("command=" + shellSingleQuote(spec.ExecStart[0]) + "\n")
			if len(spec.ExecStart) > 1 {
				buf.WriteString("command_args=\"")
				for _, arg := range spec.ExecStart[1:] {
					buf.WriteString(shellDoubleQuoteContents(" " + shellSingleQuote(arg)))
				}
				buf.WriteString("\"\n")
			}
		}
	} else {
		buf.WriteString("daemon_pidfile=\"/var/run/${name}/${name}.daemon.pid\"\n")
		buf.WriteString("child_pidfile=\"/var/run/${name}/${name}.pid\"\n")
		buf.WriteString("daemon_command=\"/usr/sbin/daemon\"\n")
		buf.WriteString("daemon_args=\"-P ${daemon_pidfile} -p ${child_pidfile} -t ${name} -r -f --")
		if len(spec.Environment) > 0 {
			buf.WriteString(" env")
			for _, env := range spec.Environment {
				buf.WriteString(shellDoubleQuoteContents(" " + shellSingleQuote(env)))
			}
		}
		for _, arg := range spec.ExecStart {
			buf.WriteString(shellDoubleQuoteContents(" " + shellSingleQuote(arg)))
		}
		buf.WriteString("\"\n")
		buf.WriteString("start_cmd=\"${name}_start\"\n")
		buf.WriteString("stop_cmd=\"${name}_stop\"\n")
		buf.WriteString("status_cmd=\"${name}_status\"\n")
	}
	if len(spec.RuntimeDirectory) > 0 || len(spec.StateDirectory) > 0 || len(spec.LogsDirectory) > 0 || len(execStartPre) > 0 {
		buf.WriteString("start_precmd=\"${name}_prestart\"\n")
	}
	buf.WriteString("\n")
	if len(spec.RuntimeDirectory) > 0 || len(spec.StateDirectory) > 0 || len(spec.LogsDirectory) > 0 || len(execStartPre) > 0 {
		buf.WriteString(name + "_prestart() {\n")
		if spec.Type != "oneshot" {
			buf.WriteString("  mkdir -p \"/var/run/${name}\"\n")
		}
		for _, dir := range freeBSDServiceDirs("/var/run", spec.RuntimeDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		for _, dir := range freeBSDServiceDirs("/var/db", spec.StateDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		for _, dir := range freeBSDServiceDirs("/var/log", spec.LogsDirectory) {
			buf.WriteString("  mkdir -p " + shellSingleQuote(dir) + "\n")
		}
		if len(execStartPre) > 0 {
			buf.WriteString("  ")
			for i, arg := range execStartPre {
				if i > 0 {
					buf.WriteString(" ")
				}
				buf.WriteString(shellSingleQuote(arg))
			}
			buf.WriteString("\n")
		}
		buf.WriteString("}\n\n")
	}
	if spec.Type != "oneshot" {
		buf.WriteString(name + "_read_pidfile() {\n")
		buf.WriteString("  _file=\"$1\"\n")
		buf.WriteString("  if [ -r \"${_file}\" ]; then\n")
		buf.WriteString("    read _pid < \"${_file}\"\n")
		buf.WriteString("    case \"${_pid}\" in ''|*[!0-9]*) return 1 ;; esac\n")
		buf.WriteString("    if [ \"${_pid}\" -gt 1 ] && kill -0 \"${_pid}\" 2>/dev/null; then\n")
		buf.WriteString("      echo \"${_pid}\"\n")
		buf.WriteString("      return 0\n")
		buf.WriteString("    fi\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  return 1\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_supervisor_pid() {\n")
		buf.WriteString("  _candidate=$(" + name + "_read_pidfile \"${daemon_pidfile}\") || return 1\n")
		buf.WriteString("  _comm=$(ps -o comm= -p \"${_candidate}\" 2>/dev/null) || return 1\n")
		buf.WriteString("  [ \"${_comm}\" = daemon ] || [ \"${_comm}\" = /usr/sbin/daemon ] || return 1\n")
		buf.WriteString("  _title=$(ps -o command= -p \"${_candidate}\" 2>/dev/null) || return 1\n")
		buf.WriteString("  case \"${_title}\" in \"daemon: ${name}[\"*\"]\") ;; *) return 1 ;; esac\n")
		buf.WriteString("  echo \"${_candidate}\"\n}\n\n")
		buf.WriteString(name + "_child_pid() {\n")
		buf.WriteString("  _candidate_pid=$(" + name + "_read_pidfile \"${child_pidfile}\") || return 1\n")
		buf.WriteString("  _parent_pid=$(ps -o ppid= -p \"${_candidate_pid}\" 2>/dev/null | tr -d ' ')\n")
		buf.WriteString("  _supervisor_pid=$(" + name + "_supervisor_pid) || return 1\n")
		buf.WriteString("  [ \"${_parent_pid}\" = \"${_supervisor_pid}\" ] || return 1\n")
		buf.WriteString("  echo \"${_candidate_pid}\"\n}\n\n")
		buf.WriteString(name + "_start() {\n")
		buf.WriteString("  _child_pid=$(${name}_child_pid)\n")
		buf.WriteString("  if [ -n \"${_child_pid}\" ]; then\n")
		buf.WriteString("    echo \"${name} is already running as pid ${_child_pid}.\"\n")
		buf.WriteString("    return 0\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  echo \"Starting ${name}.\"\n")
		buf.WriteString("  if ! ${name}_supervisor_pid >/dev/null 2>&1; then\n    mkdir -p \"/var/run/${name}\" || return 1\n    eval \"${daemon_command} ${daemon_args}\" || return 1\n  fi\n")
		buf.WriteString("  for _try in 1 2 3 4 5 6 7 8 9 10; do\n")
		buf.WriteString("    if ${name}_child_pid >/dev/null 2>&1; then\n")
		buf.WriteString("      return 0\n")
		buf.WriteString("    fi\n")
		buf.WriteString("    sleep 1\n")
		buf.WriteString("  done\n")
		buf.WriteString("  warn \"failed to start ${name}\"\n")
		buf.WriteString("  return 1\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_stop() {\n")
		buf.WriteString("  _supervisor_pid=$(${name}_supervisor_pid)\n")
		buf.WriteString("  _child_pid=$(${name}_child_pid)\n")
		buf.WriteString("  if [ -z \"${_supervisor_pid}\" ] && [ -z \"${_child_pid}\" ]; then\n")
		buf.WriteString("    if ${name}_read_pidfile \"${child_pidfile}\" >/dev/null 2>&1 || ${name}_read_pidfile \"${daemon_pidfile}\" >/dev/null 2>&1; then\n      warn \"${name} child ownership is unknown; retaining pidfiles\"\n      return 1\n    fi\n")
		buf.WriteString("    echo \"${name} is not running.\"\n")
		buf.WriteString("    return 0\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  echo \"Stopping ${name}.\"\n")
		buf.WriteString("  if [ -n \"${_supervisor_pid}\" ]; then kill \"${_supervisor_pid}\" 2>/dev/null || true; fi\n")
		buf.WriteString("  if [ -n \"${_child_pid}\" ]; then kill \"${_child_pid}\" 2>/dev/null || true; fi\n")
		buf.WriteString("  for _try in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do\n")
		buf.WriteString("    if ! ${name}_read_pidfile \"${daemon_pidfile}\" >/dev/null 2>&1 && ! ${name}_read_pidfile \"${child_pidfile}\" >/dev/null 2>&1; then\n")
		buf.WriteString("      rm -f \"${daemon_pidfile}\" \"${child_pidfile}\"\n")
		buf.WriteString("      return 0\n")
		buf.WriteString("    fi\n")
		buf.WriteString("    sleep 1\n")
		buf.WriteString("  done\n")
		buf.WriteString("  warn \"${name} did not stop after TERM; sending KILL\"\n")
		buf.WriteString("  _supervisor_pid=$(${name}_supervisor_pid)\n")
		buf.WriteString("  _child_pid=$(${name}_child_pid)\n")
		buf.WriteString("  if [ -n \"${_supervisor_pid}\" ]; then kill -KILL \"${_supervisor_pid}\" 2>/dev/null || true; fi\n")
		buf.WriteString("  if [ -n \"${_child_pid}\" ]; then kill -KILL \"${_child_pid}\" 2>/dev/null || true; fi\n")
		buf.WriteString("  for _try in 1 2 3 4 5; do\n")
		buf.WriteString("    if ! ${name}_read_pidfile \"${daemon_pidfile}\" >/dev/null 2>&1 && ! ${name}_read_pidfile \"${child_pidfile}\" >/dev/null 2>&1; then\n")
		buf.WriteString("      rm -f \"${daemon_pidfile}\" \"${child_pidfile}\"\n")
		buf.WriteString("      return 0\n")
		buf.WriteString("    fi\n")
		buf.WriteString("    sleep 1\n")
		buf.WriteString("  done\n")
		buf.WriteString("  warn \"failed to stop ${name}\"\n")
		buf.WriteString("  return 1\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_status() {\n")
		buf.WriteString("  _child_pid=$(${name}_child_pid)\n")
		buf.WriteString("  if [ -z \"${_child_pid}\" ]; then\n")
		buf.WriteString("    echo \"${name} is not running.\"\n")
		buf.WriteString("    return 1\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  echo \"${name} is running as pid ${_child_pid}.\"\n")
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

func freeBSDRCDRequires(spec api.SystemdUnitSpec) []string {
	requires := []string{"NETWORKING"}
	seen := map[string]bool{"NETWORKING": true}
	for _, value := range append(append([]string{}, spec.After...), spec.Wants...) {
		value = strings.TrimSpace(value)
		if value == "" || value == "NETWORKING" || value == "network-online.target" || value == "network.target" {
			continue
		}
		service := freeBSDServiceName(value)
		if service == "" || seen[service] {
			continue
		}
		seen[service] = true
		requires = append(requires, service)
	}
	return requires
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

func FreeBSDServiceName(value string) string {
	return freeBSDServiceName(value)
}
