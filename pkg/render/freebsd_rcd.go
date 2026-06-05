// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

func freeBSDRCDScripts(router *api.Router) (map[string][]byte, error) {
	out := map[string][]byte{}
	explicit := map[string]bool{}
	telemetryEnv, err := TelemetryEnvironment(router)
	if err != nil {
		return nil, err
	}
	routerdSpec := RouterdServiceSystemdSpec()
	routerdData, err := FreeBSDRCDScript("routerd", routerdSpec)
	if err != nil {
		return nil, err
	}
	out["routerd"] = routerdData
	explicit["routerd"] = true
	dpiSocket := ""
	if RouterWantsDPIClassifier(router) {
		dpiSocket = "/var/run/routerd/dpi-classifier/default.sock"
	}
	wantsNDPIAgent := RouterWantsNDPIAgent(router)
	if RouterWantsDPIClassifier(router) {
		name := freeBSDServiceName(DPIClassifierUnitName)
		data, err := FreeBSDRCDScript(name, DPIClassifierSystemdSpec("/var/run"))
		if err != nil {
			return nil, err
		}
		out[name] = data
	}
	if wantsNDPIAgent {
		name := freeBSDServiceName(NDPIAgentUnitName)
		if !explicit[name] {
			data, err := FreeBSDRCDScript(name, NDPIAgentSystemdSpec("/var/run"))
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
			data, err := FreeBSDRCDScript(name, freeBSDDHCPv6ClientSystemdSpec(res.Metadata.Name, ifname, spec, telemetryEnv))
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
			data, err := FreeBSDRCDScript(name, freeBSDDHCPv4ClientSystemdSpec(res.Metadata.Name, ifname, spec, telemetryEnv))
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
		unit := HealthCheckDaemonSystemdSpec(HealthCheckDaemonUnitOptions{
			Resource:    res.Metadata.Name,
			Spec:        spec,
			Router:      router,
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
		data, err := FreeBSDRCDScript(name, freeBSDDNSResolverSystemdSpec(res.Metadata.Name, spec))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "EventGroup" {
			continue
		}
		if _, err := res.EventGroupSpec(); err != nil {
			return nil, err
		}
		name := freeBSDServiceName("routerd-eventd@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		data, err := FreeBSDRCDScript(name, freeBSDEventdSystemdSpec(res.Metadata.Name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	carpConfig, err := CARPConfig(router, aliases)
	if err != nil {
		return nil, err
	}
	if len(carpConfig.Interfaces) > 0 {
		name := "routerd_carp"
		if !explicit[name] {
			out[name] = FreeBSDCARPRCDScript(carpConfig)
		}
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
		data, err := FreeBSDRCDScript(name, freeBSDFirewallLoggerSystemdSpec(spec, dpiSocket))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
	}
	return out, nil
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

func FreeBSDCARPRCDScript(config CARPConfigData) []byte {
	var buf bytes.Buffer
	buf.WriteString("#!/bin/sh\n")
	buf.WriteString("#\n")
	buf.WriteString("# Managed by routerd.\n")
	buf.WriteString("#\n")
	buf.WriteString("# PROVIDE: routerd_carp\n")
	buf.WriteString("# REQUIRE: NETWORKING\n")
	buf.WriteString("# KEYWORD: shutdown\n\n")
	buf.WriteString(". /etc/rc.subr\n\n")
	buf.WriteString("name=\"routerd_carp\"\n")
	buf.WriteString("rcvar=\"routerd_carp_enable\"\n")
	buf.WriteString("start_cmd=\"routerd_carp_start\"\n")
	buf.WriteString("stop_cmd=\"routerd_carp_stop\"\n")
	buf.WriteString("status_cmd=\"routerd_carp_status\"\n\n")
	buf.WriteString("routerd_carp_start() {\n")
	buf.WriteString("  kldload carp >/dev/null 2>&1 || true\n")
	buf.WriteString("  sysctl net.inet.carp.preempt=" + shellSingleQuote(config.PreemptSysctlValue()) + " >/dev/null\n")
	for _, command := range config.IfconfigCommands() {
		buf.WriteString("  ifconfig")
		for _, arg := range command {
			buf.WriteString(" " + shellSingleQuote(arg))
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n\n")
	buf.WriteString("routerd_carp_stop() {\n")
	for _, iface := range config.Interfaces {
		buf.WriteString("  ifconfig " + shellSingleQuote(iface.Interface) + " inet " + shellSingleQuote(iface.Address) + " -alias >/dev/null 2>&1 || true\n")
	}
	buf.WriteString("}\n\n")
	buf.WriteString("routerd_carp_status() {\n")
	if len(config.Interfaces) == 0 {
		buf.WriteString("  return 1\n")
	} else {
		buf.WriteString("  true")
		for _, iface := range config.Interfaces {
			buf.WriteString(" && ifconfig " + shellSingleQuote(iface.Interface) + " | grep -q " + shellSingleQuote("vhid "+fmt.Sprintf("%d", iface.VirtualHostID)))
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n\n")
	buf.WriteString("load_rc_config $name\n")
	buf.WriteString(": ${routerd_carp_enable:=\"YES\"}\n")
	buf.WriteString("run_rc_command \"$1\"\n")
	return buf.Bytes()
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
	buf.WriteString("# Managed by routerd.\n")
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
		if strings.TrimSpace(peer.PresharedKeyFile) != "" {
			buf.WriteString(" preshared-key " + shellSingleQuote(peer.PresharedKeyFile))
		} else if strings.TrimSpace(peer.PresharedKey) != "" {
			keyFile := "/var/run/routerd/wireguard-" + ifname + "-" + safeWireGuardKeyFilePart(peer.PublicKey[:min(8, len(peer.PublicKey))]) + ".psk"
			buf.WriteString("\n")
			buf.WriteString("  install -d -m 0700 /var/run/routerd\n")
			buf.WriteString("  umask 077; printf '%s\\n' " + shellSingleQuote(peer.PresharedKey) + " > " + shellSingleQuote(keyFile) + "\n")
			buf.WriteString("  wg set " + shellSingleQuote(ifname) + " peer " + shellSingleQuote(peer.PublicKey) + " preshared-key " + shellSingleQuote(keyFile))
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

func safeWireGuardKeyFilePart(value string) string {
	value = regexp.MustCompile(`[^A-Za-z0-9_.-]+`).ReplaceAllString(value, "_")
	if value == "" {
		return "peer"
	}
	return value
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
	privateTmp := true
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
		ReadWritePaths:           []string{"/var/run/routerd", "/var/db/routerd", "/var/log/routerd"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET"},
		ProtectSystem:            "strict",
		ProtectHome:              "true",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}

func freeBSDFirewallLoggerSystemdSpec(spec api.FirewallLogSpec, dpiSocket string) api.SystemdUnitSpec {
	path := spec.Path
	if path == "" {
		path = "/var/db/routerd/firewall-logs.db"
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
	privateTmp := true
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
		buf.WriteString("daemon_pidfile=\"/var/run/${name}/${name}.daemon.pid\"\n")
		buf.WriteString("child_pidfile=\"/var/run/${name}/${name}.pid\"\n")
		buf.WriteString("daemon_command=\"/usr/sbin/daemon\"\n")
		buf.WriteString("daemon_args=\"-P ${daemon_pidfile} -p ${child_pidfile} -r -f --")
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
		buf.WriteString("    if [ -n \"${_pid}\" ] && kill -0 \"${_pid}\" 2>/dev/null; then\n")
		buf.WriteString("      echo \"${_pid}\"\n")
		buf.WriteString("      return 0\n")
		buf.WriteString("    fi\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  return 1\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_pgrep_child() {\n")
		buf.WriteString("  ps -axo pid,command | awk -v exe=")
		buf.WriteString(shellSingleQuote(freeBSDRCDExecPath(spec.ExecStart)))
		buf.WriteString(" -v pat=")
		buf.WriteString(shellSingleQuote(freeBSDRCDPgrepPattern(spec.ExecStart)))
		buf.WriteString(" '$0 ~ exe && $0 ~ pat { print $1; exit }'\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_managed_child_pid() {\n")
		buf.WriteString("  _candidate_pid=$(" + name + "_child_pid)\n")
		buf.WriteString("  if [ -z \"${_candidate_pid}\" ]; then\n")
		buf.WriteString("    return 1\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  _parent_pid=$(ps -o ppid= -p \"${_candidate_pid}\" 2>/dev/null | tr -d ' ')\n")
		buf.WriteString("  _supervisor_pid=$(" + name + "_supervisor_pid)\n")
		buf.WriteString("  if [ -n \"${_supervisor_pid}\" ] && [ \"${_parent_pid}\" = \"${_supervisor_pid}\" ]; then\n")
		buf.WriteString("    echo \"${_candidate_pid}\"\n")
		buf.WriteString("    return 0\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  return 1\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_parent_daemon_pid() {\n")
		buf.WriteString("  _child_pid=$(" + name + "_child_pid)\n")
		buf.WriteString("  if [ -z \"${_child_pid}\" ]; then\n")
		buf.WriteString("    return 1\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  _parent_pid=$(ps -o ppid= -p \"${_child_pid}\" 2>/dev/null | tr -d ' ')\n")
		buf.WriteString("  if [ -z \"${_parent_pid}\" ] || [ \"${_parent_pid}\" = \"1\" ]; then\n")
		buf.WriteString("    return 1\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  _parent_command=$(ps -o command= -p \"${_parent_pid}\" 2>/dev/null)\n")
		buf.WriteString("  case \"${_parent_command}\" in\n")
		buf.WriteString("  daemon:*|*/daemon*)\n")
		buf.WriteString("    echo \"${_parent_pid}\"\n")
		buf.WriteString("    return 0\n")
		buf.WriteString("    ;;\n")
		buf.WriteString("  esac\n")
		buf.WriteString("  return 1\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_supervisor_pid() {\n")
		buf.WriteString("  " + name + "_read_pidfile \"${daemon_pidfile}\" || " + name + "_parent_daemon_pid\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_child_pid() {\n")
		buf.WriteString("  " + name + "_read_pidfile \"${child_pidfile}\" || " + name + "_pgrep_child\n")
		buf.WriteString("}\n\n")
		buf.WriteString(name + "_start() {\n")
		buf.WriteString("  _child_pid=$(${name}_child_pid)\n")
		buf.WriteString("  if [ -n \"${_child_pid}\" ]; then\n")
		buf.WriteString("    echo \"${name} is already running as pid ${_child_pid}.\"\n")
		buf.WriteString("    return 0\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  echo \"Starting ${name}.\"\n")
		buf.WriteString("  eval \"${daemon_command} ${daemon_args}\"\n")
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
		buf.WriteString("    echo \"${name} is not running.\"\n")
		buf.WriteString("    return 0\n")
		buf.WriteString("  fi\n")
		buf.WriteString("  echo \"Stopping ${name}.\"\n")
		buf.WriteString("  if [ -n \"${_supervisor_pid}\" ]; then kill \"${_supervisor_pid}\" 2>/dev/null || true; fi\n")
		buf.WriteString("  if [ -n \"${_child_pid}\" ]; then kill \"${_child_pid}\" 2>/dev/null || true; fi\n")
		buf.WriteString("  for _try in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do\n")
		buf.WriteString("    if ! ${name}_supervisor_pid >/dev/null 2>&1 && ! ${name}_managed_child_pid >/dev/null 2>&1; then\n")
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
		buf.WriteString("    if ! ${name}_supervisor_pid >/dev/null 2>&1 && ! ${name}_managed_child_pid >/dev/null 2>&1; then\n")
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

func freeBSDRCDPgrepPattern(args []string) string {
	var parts []string
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		parts = append(parts, regexp.QuoteMeta(arg))
		if len(parts) >= 5 {
			break
		}
	}
	return strings.Join(parts, " .*")
}

func freeBSDRCDExecPath(args []string) string {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg != "" {
			return arg
		}
	}
	return ""
}

func sortedByteMapKeys(values map[string][]byte) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
