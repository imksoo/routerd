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
	aliases, err := nftOutboundAliases(router)
	if err != nil {
		return nil, err
	}
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
		data, err := FreeBSDRCDScript(name, freeBSDDHCPv6ClientSystemdSpec(res.Metadata.Name, ifname, spec, telemetryEnv))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
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
		if res.Kind != "WireGuardInterface" {
			continue
		}
		spec, err := res.WireGuardInterfaceSpec()
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(spec.PrivateKey) == "" && strings.TrimSpace(spec.PrivateKeyFile) == "" {
			continue
		}
		name := freeBSDServiceName("routerd-wireguard@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		data, err := FreeBSDWireGuardRCDScript(res.Metadata.Name, spec, wireGuardPeerSpecs(router, res.Metadata.Name), wireGuardIPv4Addresses(router, res.Metadata.Name))
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
		unit := TailscaleSystemdSpec(res.Metadata.Name, spec)
		unit.ExecStartPre = []string{"service", "tailscaled", "onestart"}
		data, err := FreeBSDRCDScript(name, unit)
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

func wireGuardPeerSpecs(router *api.Router, ifname string) []api.WireGuardPeerSpec {
	var peers []api.WireGuardPeerSpec
	for _, res := range router.Spec.Resources {
		if res.Kind != "WireGuardPeer" {
			continue
		}
		spec, err := res.WireGuardPeerSpec()
		if err != nil || spec.Interface != ifname {
			continue
		}
		peers = append(peers, spec)
	}
	sort.SliceStable(peers, func(i, j int) bool { return peers[i].PublicKey < peers[j].PublicKey })
	return peers
}

func wireGuardIPv4Addresses(router *api.Router, ifname string) []string {
	var addresses []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv4StaticAddress" {
			continue
		}
		spec, err := res.IPv4StaticAddressSpec()
		if err != nil || spec.Interface != ifname {
			continue
		}
		address, err := freeBSDIPv4CIDR(spec.Address)
		if err != nil {
			continue
		}
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	return addresses
}

func FreeBSDWireGuardRCDScript(ifname string, spec api.WireGuardInterfaceSpec, peers []api.WireGuardPeerSpec, addresses []string) ([]byte, error) {
	if strings.TrimSpace(ifname) == "" {
		return nil, fmt.Errorf("ifname is required")
	}
	if strings.TrimSpace(spec.PrivateKey) == "" && strings.TrimSpace(spec.PrivateKeyFile) == "" {
		return nil, fmt.Errorf("privateKeyFile is required for FreeBSD WireGuard service")
	}
	name := freeBSDServiceName("routerd-wireguard@" + ifname + ".service")
	var buf bytes.Buffer
	buf.WriteString("#!/bin/sh\n")
	buf.WriteString("#\n")
	buf.WriteString("# PROVIDE: " + name + "\n")
	buf.WriteString("# REQUIRE: NETWORKING\n")
	buf.WriteString("# KEYWORD: shutdown\n\n")
	buf.WriteString(". /etc/rc.subr\n\n")
	buf.WriteString("name=" + shellSingleQuote(name) + "\n")
	buf.WriteString("rcvar=\"${name}_enable\"\n")
	buf.WriteString("start_cmd=\"${name}_start\"\n")
	buf.WriteString("stop_cmd=\"${name}_stop\"\n")
	buf.WriteString("status_cmd=\"${name}_status\"\n\n")
	buf.WriteString(name + "_start() {\n")
	buf.WriteString("  kldload if_wg >/dev/null 2>&1 || true\n")
	buf.WriteString("  if ! ifconfig " + shellSingleQuote(ifname) + " >/dev/null 2>&1; then\n")
	buf.WriteString("    ifconfig " + shellSingleQuote(ifname) + " create >/dev/null 2>&1 || ifconfig wg create name " + shellSingleQuote(ifname) + "\n")
	buf.WriteString("  fi\n")
	if spec.MTU != 0 {
		buf.WriteString("  ifconfig " + shellSingleQuote(ifname) + " mtu " + shellSingleQuote(fmt.Sprintf("%d", spec.MTU)) + "\n")
	}
	if strings.TrimSpace(spec.PrivateKeyFile) != "" {
		buf.WriteString("  wg set " + shellSingleQuote(ifname))
		if spec.ListenPort != 0 {
			buf.WriteString(" listen-port " + shellSingleQuote(fmt.Sprintf("%d", spec.ListenPort)))
		}
		buf.WriteString(" private-key " + shellSingleQuote(spec.PrivateKeyFile) + "\n")
	} else {
		keyFile := "/var/run/routerd/wireguard-" + ifname + ".key"
		buf.WriteString("  install -d -m 0700 /var/run/routerd\n")
		buf.WriteString("  umask 077; printf '%s\\n' " + shellSingleQuote(spec.PrivateKey) + " > " + shellSingleQuote(keyFile) + "\n")
		buf.WriteString("  wg set " + shellSingleQuote(ifname))
		if spec.ListenPort != 0 {
			buf.WriteString(" listen-port " + shellSingleQuote(fmt.Sprintf("%d", spec.ListenPort)))
		}
		buf.WriteString(" private-key " + shellSingleQuote(keyFile) + "\n")
	}
	for _, peer := range peers {
		if strings.TrimSpace(peer.PublicKey) == "" {
			return nil, fmt.Errorf("peer publicKey is required")
		}
		if len(peer.AllowedIPs) == 0 {
			return nil, fmt.Errorf("peer allowedIPs is required")
		}
		buf.WriteString("  wg set " + shellSingleQuote(ifname) + " peer " + shellSingleQuote(peer.PublicKey))
		buf.WriteString(" allowed-ips " + shellSingleQuote(strings.Join(peer.AllowedIPs, ",")))
		if strings.TrimSpace(peer.Endpoint) != "" {
			buf.WriteString(" endpoint " + shellSingleQuote(peer.Endpoint))
		}
		if peer.PersistentKeepalive != 0 {
			buf.WriteString(" persistent-keepalive " + shellSingleQuote(fmt.Sprintf("%d", peer.PersistentKeepalive)))
		}
		buf.WriteString("\n")
	}
	for _, address := range addresses {
		buf.WriteString("  ifconfig " + shellSingleQuote(ifname) + " inet " + shellSingleQuote(address) + " alias >/dev/null 2>&1 || true\n")
	}
	buf.WriteString("  ifconfig " + shellSingleQuote(ifname) + " up\n")
	buf.WriteString("}\n\n")
	buf.WriteString(name + "_stop() {\n")
	buf.WriteString("  ifconfig " + shellSingleQuote(ifname) + " destroy >/dev/null 2>&1 || true\n")
	buf.WriteString("}\n\n")
	buf.WriteString(name + "_status() {\n")
	buf.WriteString("  ifconfig " + shellSingleQuote(ifname) + " >/dev/null 2>&1 && wg show " + shellSingleQuote(ifname) + " >/dev/null 2>&1\n")
	buf.WriteString("}\n\n")
	buf.WriteString("load_rc_config $name\n")
	buf.WriteString(": ${" + name + "_enable:=\"YES\"}\n")
	buf.WriteString("run_rc_command \"$1\"\n")
	return buf.Bytes(), nil
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
	noNewPrivileges := true
	privateTmp := true
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
		ReadWritePaths:           []string{"/var/run/routerd", "/var/db/routerd", "/var/log/routerd"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET6"},
		ProtectSystem:            "strict",
		ProtectHome:              "true",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
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
		buf.WriteString("procname=\"/usr/sbin/daemon\"\n")
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
	if len(spec.RuntimeDirectory) > 0 || len(spec.StateDirectory) > 0 || len(spec.LogsDirectory) > 0 || len(spec.ExecStartPre) > 0 {
		buf.WriteString("start_precmd=\"${name}_prestart\"\n")
	}
	buf.WriteString("\n")
	if len(spec.RuntimeDirectory) > 0 || len(spec.StateDirectory) > 0 || len(spec.LogsDirectory) > 0 || len(spec.ExecStartPre) > 0 {
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
		if len(spec.ExecStartPre) > 0 {
			buf.WriteString("  ")
			for i, arg := range spec.ExecStartPre {
				if i > 0 {
					buf.WriteString(" ")
				}
				buf.WriteString(shellSingleQuote(arg))
			}
			buf.WriteString("\n")
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

func FreeBSDServiceName(value string) string {
	return freeBSDServiceName(value)
}

func sortedByteMapKeys(values map[string][]byte) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
