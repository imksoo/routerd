// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"routerd/pkg/api"
)

type OpenRCConfig struct {
	InitScripts map[string][]byte
	Services    []OpenRCService
	Warnings    []string
}

type OpenRCService struct {
	Name    string
	Enabled bool
	Started bool
}

type OpenRCOptions struct {
	IncludeDnsmasq bool
}

func OpenRC(router *api.Router) (OpenRCConfig, error) {
	return OpenRCWithOptions(router, OpenRCOptions{IncludeDnsmasq: true})
}

func OpenRCWithOptions(router *api.Router, options OpenRCOptions) (OpenRCConfig, error) {
	out := map[string][]byte{}
	explicit := map[string]bool{}
	var services []OpenRCService
	telemetryEnv, err := TelemetryEnvironment(router)
	if err != nil {
		return OpenRCConfig{}, err
	}
	dpiSocket := ""
	if hasSystemdUnit(router, DPIClassifierUnitName) {
		dpiSocket = "/run/routerd/dpi-classifier/default.sock"
	}
	wantsNDPIAgent := RouterWantsNDPIAgent(router)
	for _, res := range router.Spec.Resources {
		if res.Kind != "SystemdUnit" {
			continue
		}
		spec, err := res.SystemdUnitSpec()
		if err != nil {
			return OpenRCConfig{}, err
		}
		if defaultString(spec.State, "present") == "absent" {
			continue
		}
		name := openRCServiceName(defaultString(spec.UnitName, res.Metadata.Name))
		explicit[name] = true
		spec = MaybeAugmentDPIClassifierSpec(defaultString(spec.UnitName, res.Metadata.Name), spec, openRCServiceName(NDPIAgentUnitName))
		spec.Environment = mergeEnvironment(spec.Environment, telemetryEnv)
		data, err := OpenRCScript(name, spec)
		if err != nil {
			return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
		services = append(services, OpenRCService{Name: name, Enabled: api.BoolDefault(spec.Enabled, true), Started: api.BoolDefault(spec.Started, true)})
	}
	if wantsNDPIAgent {
		name := openRCServiceName(NDPIAgentUnitName)
		if !explicit[name] {
			data, err := OpenRCScript(name, NDPIAgentSystemdSpec("/run"))
			if err != nil {
				return OpenRCConfig{}, err
			}
			out[name] = data
			services = append(services, OpenRCService{Name: name, Enabled: true, Started: true})
		}
	}
	aliases := linkAliases(router)
	if !openRCRouterdSupervisesClientDaemons(router) {
		for _, res := range router.Spec.Resources {
			if res.Kind != "DHCPv6PrefixDelegation" {
				continue
			}
			spec, err := res.DHCPv6PrefixDelegationSpec()
			if err != nil {
				return OpenRCConfig{}, err
			}
			ifname := aliases[spec.Interface]
			if ifname == "" {
				return OpenRCConfig{}, fmt.Errorf("%s references interface %q with no ifname", res.ID(), spec.Interface)
			}
			name := openRCServiceName("routerd-dhcpv6-client@" + res.Metadata.Name + ".service")
			if explicit[name] {
				continue
			}
			data, err := OpenRCScript(name, dhcpv6PrefixDelegationOpenRCSystemdSpec(res.Metadata.Name, ifname, spec, telemetryEnv))
			if err != nil {
				return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
			}
			out[name] = data
			services = append(services, OpenRCService{Name: name, Enabled: true, Started: true})
		}
		for _, res := range router.Spec.Resources {
			if res.Kind != "DHCPv4Lease" {
				continue
			}
			spec, err := res.DHCPv4LeaseSpec()
			if err != nil {
				return OpenRCConfig{}, err
			}
			ifname := aliases[spec.Interface]
			if ifname == "" {
				return OpenRCConfig{}, fmt.Errorf("%s references interface %q with no ifname", res.ID(), spec.Interface)
			}
			name := openRCServiceName("routerd-dhcpv4-client@" + res.Metadata.Name + ".service")
			if explicit[name] {
				continue
			}
			data, err := OpenRCScript(name, dhcpv4ClientSystemdSpec(res.Metadata.Name, ifname, spec, telemetryEnv))
			if err != nil {
				return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
			}
			out[name] = data
			services = append(services, OpenRCService{Name: name, Enabled: true, Started: true})
		}
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "HealthCheck" {
			continue
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return OpenRCConfig{}, err
		}
		if spec.Daemon != "routerd-healthcheck" {
			continue
		}
		name := openRCServiceName("routerd-healthcheck@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		unit := HealthCheckDaemonSystemdSpec(HealthCheckDaemonUnitOptions{
			Resource:    res.Metadata.Name,
			Spec:        spec,
			Router:      router,
			Aliases:     aliases,
			Environment: telemetryEnv,
			RuntimeRoot: "/run",
			StateRoot:   "/var/lib",
			LogRoot:     "/var/log",
		})
		data, err := OpenRCScript(name, unit)
		if err != nil {
			return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
		services = append(services, OpenRCService{Name: name, Enabled: true, Started: true})
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "DNSResolver" {
			continue
		}
		spec, err := res.DNSResolverSpec()
		if err != nil {
			return OpenRCConfig{}, err
		}
		name := openRCServiceName("routerd-dns-resolver@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		data, err := OpenRCScript(name, DNSResolverSystemdSpec(res.Metadata.Name, spec, "/usr/local/sbin/routerd-dns-resolver", "/var/lib/routerd/dns-resolver/"+res.Metadata.Name+"/config.json"))
		if err != nil {
			return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
		services = append(services, OpenRCService{Name: name, Enabled: false, Started: false})
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "PPPoESession" {
			continue
		}
		spec, err := res.PPPoESessionSpec()
		if err != nil {
			return OpenRCConfig{}, err
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return OpenRCConfig{}, fmt.Errorf("%s references interface %q with no ifname", res.ID(), spec.Interface)
		}
		name := openRCServiceName("routerd-pppoe-client@" + res.Metadata.Name + ".service")
		if explicit[name] {
			continue
		}
		unit, err := pppoeSessionSystemdSpec(res.Metadata.Name, ifname, spec, telemetryEnv)
		if err != nil {
			return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
		}
		data, err := OpenRCScript(name, unit)
		if err != nil {
			return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
		services = append(services, OpenRCService{Name: name, Enabled: true, Started: true})
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "TailscaleNode" {
			continue
		}
		spec, err := res.TailscaleNodeSpec()
		if err != nil {
			return OpenRCConfig{}, err
		}
		if firstNonEmpty(spec.State, "present") == "absent" {
			continue
		}
		spec.BinaryPath = firstNonEmpty(spec.BinaryPath, "/usr/bin/tailscale")
		name := openRCServiceName(TailscaleUnitName(res.Metadata.Name))
		if explicit[name] {
			continue
		}
		unit := TailscaleSystemdSpec(res.Metadata.Name, spec)
		unit.ExecStartPre = []string{"rc-service", "tailscaled", "start"}
		data, err := OpenRCScript(name, unit)
		if err != nil {
			return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
		services = append(services, OpenRCService{Name: name, Enabled: true, Started: true})
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "FirewallLog" {
			continue
		}
		spec, err := res.FirewallLogSpec()
		if err != nil {
			return OpenRCConfig{}, err
		}
		if !spec.Enabled {
			continue
		}
		name := openRCServiceName("routerd-firewall-logger.service")
		if explicit[name] {
			continue
		}
		data, err := OpenRCScript(name, firewallLoggerSystemdSpec(spec, dpiSocket))
		if err != nil {
			return OpenRCConfig{}, fmt.Errorf("%s: %w", res.ID(), err)
		}
		out[name] = data
		services = append(services, OpenRCService{Name: name, Enabled: true, Started: true})
	}
	if options.IncludeDnsmasq && nixOSNeedsDnsmasq(router) {
		name := "routerd_dnsmasq"
		if !explicit[name] {
			data, err := OpenRCScript(name, dnsmasqOpenRCSystemdSpec("", ""))
			if err != nil {
				return OpenRCConfig{}, err
			}
			out[name] = data
			services = append(services, OpenRCService{Name: name, Enabled: true, Started: true})
		}
	}
	sort.SliceStable(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return OpenRCConfig{InitScripts: out, Services: services}, nil
}

func OpenRCScript(name string, spec api.SystemdUnitSpec) ([]byte, error) {
	name = openRCServiceName(name)
	if len(spec.ExecStart) == 0 {
		return nil, fmt.Errorf("execStart is required")
	}
	description := defaultString(spec.Description, "routerd managed "+name)
	command := strings.TrimSpace(spec.ExecStart[0])
	if command == "" {
		return nil, fmt.Errorf("execStart[0] is required")
	}
	var buf bytes.Buffer
	buf.WriteString("#!/sbin/openrc-run\n")
	buf.WriteString("# Generated by routerd. Do not edit by hand.\n\n")
	buf.WriteString("name=" + shellQuote(name) + "\n")
	buf.WriteString("description=" + shellQuote(description) + "\n")
	buf.WriteString("command=" + shellQuote(command) + "\n")
	if len(spec.ExecStart) > 1 {
		buf.WriteString("command_args=" + strconv.Quote(shellJoin(spec.ExecStart[1:])) + "\n")
	}
	if defaultString(spec.Type, "simple") != "oneshot" {
		buf.WriteString("command_background=\"yes\"\n")
		buf.WriteString("pidfile=\"/run/routerd/openrc/${RC_SVCNAME}.pid\"\n")
	}
	if spec.User != "" {
		user := spec.User
		if spec.Group != "" {
			user += ":" + spec.Group
		}
		buf.WriteString("command_user=" + shellQuote(user) + "\n")
	}
	if spec.WorkingDirectory != "" {
		buf.WriteString("directory=" + shellQuote(spec.WorkingDirectory) + "\n")
	}
	for _, env := range spec.Environment {
		env = strings.TrimSpace(env)
		if env == "" {
			continue
		}
		key, value, ok := strings.Cut(env, "=")
		if !ok || !openRCEnvName(key) {
			continue
		}
		buf.WriteString("export " + key + "=" + shellQuote(value) + "\n")
	}
	buf.WriteString("\ndepend() {\n")
	buf.WriteString("\tuse net\n")
	if len(spec.After) > 0 {
		for _, service := range openRCAfterServices(spec.After) {
			buf.WriteString("\tafter " + service + "\n")
		}
	}
	buf.WriteString("}\n")
	buf.WriteString("\nstart_pre() {\n")
	buf.WriteString("\tcheckpath -d -m 0755 /run/routerd/openrc\n")
	for _, dir := range openRCServiceDirs("/run", spec.RuntimeDirectory) {
		buf.WriteString("\tcheckpath -d -m 0755 " + shellQuote(dir) + "\n")
	}
	for _, dir := range openRCServiceDirs("/var/lib", spec.StateDirectory) {
		buf.WriteString("\tcheckpath -d -m 0755 " + shellQuote(dir) + "\n")
	}
	for _, dir := range openRCServiceDirs("/var/log", spec.LogsDirectory) {
		buf.WriteString("\tcheckpath -d -m 0755 " + shellQuote(dir) + "\n")
	}
	if len(spec.ExecStartPre) > 0 {
		buf.WriteString("\t" + shellJoin(spec.ExecStartPre) + "\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

func dnsmasqOpenRCSystemdSpec(dnsmasqPath, configPath string) api.SystemdUnitSpec {
	if strings.TrimSpace(dnsmasqPath) == "" {
		dnsmasqPath = "/usr/sbin/dnsmasq"
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = "/usr/local/etc/routerd/dnsmasq.conf"
	}
	return api.SystemdUnitSpec{
		Description:              "routerd managed dnsmasq DHCP service",
		ExecStart:                []string{dnsmasqPath, "--keep-in-foreground", "--user=root", "--group=root", "--conf-file=" + configPath, "--pid-file=/run/routerd/dnsmasq.pid"},
		After:                    []string{"routerd"},
		Restart:                  "on-failure",
		RestartSec:               "2s",
		RuntimeDirectory:         []string{"routerd"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd"},
		ReadWritePaths:           []string{"/run/routerd", "/var/lib/routerd", "/usr/local/etc/routerd"},
	}
}

func DnsmasqOpenRCScript(configPath, dnsmasqPath string) ([]byte, error) {
	return OpenRCScript("routerd_dnsmasq", dnsmasqOpenRCSystemdSpec(dnsmasqPath, configPath))
}

func dhcpv6PrefixDelegationOpenRCSystemdSpec(resource, ifname string, spec api.DHCPv6PrefixDelegationSpec, telemetryEnv []string) api.SystemdUnitSpec {
	noNewPrivileges := true
	privateTmp := true
	exec := []string{
		"/usr/local/sbin/routerd-dhcpv6-client",
		"daemon",
		"--resource", resource,
		"--interface", ifname,
		"--socket", "/run/routerd/dhcpv6-client/" + resource + ".sock",
		"--lease-file", "/var/lib/routerd/dhcpv6-client/" + resource + "/lease.json",
		"--event-file", "/var/lib/routerd/dhcpv6-client/" + resource + "/events.jsonl",
	}
	if spec.IAID != "" {
		exec = append(exec, "--iaid", spec.IAID)
	}
	return api.SystemdUnitSpec{
		Description:              "routerd DHCPv6-PD client " + resource,
		ExecStart:                exec,
		Environment:              telemetryEnv,
		Wants:                    []string{"network-online.target"},
		After:                    []string{"network-online.target"},
		Restart:                  "always",
		RestartSec:               "5s",
		RuntimeDirectory:         []string{"routerd/dhcpv6-client"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/dhcpv6-client", "routerd/dhcpv6-client/" + resource},
		LogsDirectory:            []string{"routerd"},
		ReadWritePaths:           []string{"/run/routerd", "/var/lib/routerd", "/var/log/routerd"},
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET6", "AF_NETLINK"},
		ProtectSystem:            "strict",
		ProtectHome:              "true",
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
	}
}

func openRCRouterdSupervisesClientDaemons(router *api.Router) bool {
	for _, res := range router.Spec.Resources {
		if res.Kind != "SystemdUnit" {
			continue
		}
		spec, err := res.SystemdUnitSpec()
		if err != nil || defaultString(spec.State, "present") == "absent" {
			continue
		}
		unitName := defaultString(spec.UnitName, res.Metadata.Name)
		if openRCServiceName(unitName) != "routerd" {
			continue
		}
		if !containsInitArg(spec.ExecStart, "--controller-chain") {
			continue
		}
		if initBoolFlagValue(spec.ExecStart, "--controller-chain-supervise-client-daemons", true) {
			return true
		}
	}
	return false
}

func containsInitArg(args []string, needle string) bool {
	for _, arg := range args {
		if arg == needle {
			return true
		}
	}
	return false
}

func initBoolFlagValue(args []string, flag string, defaultValue bool) bool {
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

func openRCServiceDirs(root string, values []string) []string {
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

func openRCAfterServices(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSuffix(strings.TrimSpace(value), ".service")
		switch value {
		case "", "network-online.target", "network.target", "multi-user.target":
			continue
		case "routerd":
			// keep routerd as-is
		default:
			value = openRCServiceName(value)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

var unsafeOpenRCServiceName = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func openRCServiceName(value string) string {
	value = strings.TrimSuffix(value, ".service")
	value = unsafeOpenRCServiceName.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "routerd_service"
	}
	return value
}

func OpenRCServiceName(value string) string {
	return openRCServiceName(value)
}

func shellJoin(args []string) string {
	var quoted []string
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

var openRCEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func openRCEnvName(value string) bool {
	return openRCEnvNamePattern.MatchString(value)
}
