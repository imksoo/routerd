package config

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"routerd/pkg/api"
)

func Validate(router *api.Router) error {
	if router.APIVersion != api.RouterAPIVersion {
		return fmt.Errorf("router apiVersion must be %s", api.RouterAPIVersion)
	}
	if router.Kind != "Router" {
		return fmt.Errorf("router kind must be Router")
	}
	if router.Metadata.Name == "" {
		return fmt.Errorf("router metadata.name is required")
	}
	if err := validateReconcilePolicy(router.Spec.Reconcile); err != nil {
		return err
	}

	seen := map[string]bool{}
	baseInterfaces := map[string]bool{}
	interfaces := map[string]bool{}
	dhcp4Servers := map[string]bool{}
	dhcp4ServerSpecs := map[string]api.IPv4DHCPServerSpec{}
	dhcp6Servers := map[string]bool{}
	dhcp6ServerSpecs := map[string]api.IPv6DHCPServerSpec{}
	dhcp6Scopes := map[string]bool{}
	prefixDelegations := map[string]bool{}
	delegatedAddresses := map[string]bool{}
	delegatedAddressInterfaces := map[string]string{}
	selfAddressPolicies := map[string]bool{}
	dsliteTunnels := map[string]bool{}
	routeSets := map[string]bool{}
	healthChecks := map[string]bool{}
	zones := map[string]bool{}
	staticByInterfaceAddress := map[string]string{}
	for _, res := range router.Spec.Resources {
		if err := validateResource(res); err != nil {
			return err
		}
		if seen[res.ID()] {
			return fmt.Errorf("duplicate resource %s", res.ID())
		}
		seen[res.ID()] = true
		if res.APIVersion == api.NetAPIVersion && res.Kind == "Interface" {
			baseInterfaces[res.Metadata.Name] = true
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "PPPoEInterface" {
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv4DHCPServer" {
			dhcp4Servers[res.Metadata.Name] = true
			spec, err := res.IPv4DHCPServerSpec()
			if err != nil {
				return err
			}
			dhcp4ServerSpecs[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6DHCPServer" {
			dhcp6Servers[res.Metadata.Name] = true
			spec, err := res.IPv6DHCPServerSpec()
			if err != nil {
				return err
			}
			dhcp6ServerSpecs[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6DHCPScope" {
			dhcp6Scopes[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6PrefixDelegation" {
			prefixDelegations[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6DelegatedAddress" {
			delegatedAddresses[res.Metadata.Name] = true
			spec, err := res.IPv6DelegatedAddressSpec()
			if err != nil {
				return err
			}
			delegatedAddressInterfaces[res.Metadata.Name] = spec.Interface
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "SelfAddressPolicy" {
			selfAddressPolicies[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DSLiteTunnel" {
			dsliteTunnels[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv4PolicyRouteSet" {
			routeSets[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "HealthCheck" {
			healthChecks[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv4StaticAddress" {
			spec, err := res.IPv4StaticAddressSpec()
			if err != nil {
				return err
			}
			prefix, err := netip.ParsePrefix(spec.Address)
			if err != nil {
				return fmt.Errorf("%s spec.address is invalid: %w", res.ID(), err)
			}
			key := spec.Interface + "|" + prefix.Masked().String()
			if existing := staticByInterfaceAddress[key]; existing != "" {
				return fmt.Errorf("%s duplicates IPv4 static address already declared by %s", res.ID(), existing)
			}
			staticByInterfaceAddress[key] = res.ID()
		}
		if res.APIVersion == api.FirewallAPIVersion && res.Kind == "Zone" {
			zones[res.Metadata.Name] = true
		}
	}
	for i, name := range router.Spec.Reconcile.ProtectedInterfaces {
		if !interfaces[name] {
			return fmt.Errorf("spec.reconcile.protectedInterfaces[%d] references missing Interface %q", i, name)
		}
	}
	for i, name := range router.Spec.Reconcile.ProtectedZones {
		if !zones[name] {
			return fmt.Errorf("spec.reconcile.protectedZones[%d] references missing Zone %q", i, name)
		}
	}

	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "IPv4StaticAddress", "IPv4DHCPAddress", "IPv4DHCPScope", "IPv6DHCPAddress", "IPv6PrefixDelegation", "IPv6DelegatedAddress", "DSLiteTunnel", "PPPoEInterface":
			name, err := interfaceRef(res)
			if err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("%s spec.interface is required", res.ID())
			}
			if !interfaces[name] {
				return fmt.Errorf("%s references missing Interface %q", res.ID(), name)
			}
			if res.Kind == "PPPoEInterface" && !baseInterfaces[name] {
				return fmt.Errorf("%s spec.interface must reference a base Interface %q", res.ID(), name)
			}
		}
		if res.Kind == "IPv4DHCPScope" {
			spec, err := res.IPv4DHCPScopeSpec()
			if err != nil {
				return err
			}
			if !dhcp4Servers[spec.Server] {
				return fmt.Errorf("%s references missing IPv4DHCPServer %q", res.ID(), spec.Server)
			}
			if !stringInSlice(spec.Interface, dhcp4ServerSpecs[spec.Server].ListenInterfaces) {
				return fmt.Errorf("%s spec.interface %q must be listed in IPv4DHCPServer %q spec.listenInterfaces", res.ID(), spec.Interface, spec.Server)
			}
			if spec.DNSInterface != "" && !interfaces[spec.DNSInterface] {
				return fmt.Errorf("%s references missing DNS Interface %q", res.ID(), spec.DNSInterface)
			}
		}
		if res.Kind == "NTPClient" {
			spec, err := res.NTPClientSpec()
			if err != nil {
				return err
			}
			if spec.Interface != "" && !interfaces[spec.Interface] {
				return fmt.Errorf("%s references missing Interface %q", res.ID(), spec.Interface)
			}
		}
		if res.Kind == "IPv4DHCPServer" {
			spec, err := res.IPv4DHCPServerSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.ListenInterfaces {
				if !interfaces[name] {
					return fmt.Errorf("%s spec.listenInterfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
		}
		if res.Kind == "IPv6DHCPServer" {
			spec, err := res.IPv6DHCPServerSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.ListenInterfaces {
				if !interfaces[name] {
					return fmt.Errorf("%s spec.listenInterfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
		}
		if res.Kind == "IPv4SourceNAT" {
			spec, err := res.IPv4SourceNATSpec()
			if err != nil {
				return err
			}
			if !interfaces[spec.OutboundInterface] && !dsliteTunnels[spec.OutboundInterface] {
				return fmt.Errorf("%s references missing outbound Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), spec.OutboundInterface)
			}
		}
		if res.Kind == "IPv4PolicyRoute" {
			spec, err := res.IPv4PolicyRouteSpec()
			if err != nil {
				return err
			}
			if !interfaces[spec.OutboundInterface] && !dsliteTunnels[spec.OutboundInterface] {
				return fmt.Errorf("%s references missing outbound Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), spec.OutboundInterface)
			}
		}
		if res.Kind == "IPv4PolicyRouteSet" {
			spec, err := res.IPv4PolicyRouteSetSpec()
			if err != nil {
				return err
			}
			for _, target := range spec.Targets {
				if !interfaces[target.OutboundInterface] && !dsliteTunnels[target.OutboundInterface] {
					return fmt.Errorf("%s target %q references missing outbound Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), target.Name, target.OutboundInterface)
				}
			}
		}
		if res.Kind == "IPv4ReversePathFilter" {
			spec, err := res.IPv4ReversePathFilterSpec()
			if err != nil {
				return err
			}
			if spec.Target == "interface" && !interfaces[spec.Interface] && !dsliteTunnels[spec.Interface] {
				return fmt.Errorf("%s references missing Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), spec.Interface)
			}
		}
		if res.Kind == "PathMTUPolicy" {
			spec, err := res.PathMTUPolicySpec()
			if err != nil {
				return err
			}
			if !interfaces[spec.FromInterface] && !dsliteTunnels[spec.FromInterface] {
				return fmt.Errorf("%s spec.fromInterface references missing Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), spec.FromInterface)
			}
			for i, name := range spec.ToInterfaces {
				if !interfaces[name] && !dsliteTunnels[name] {
					return fmt.Errorf("%s spec.toInterfaces[%d] references missing Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), i, name)
				}
			}
			if spec.IPv6RA.Enabled && !dhcp6Scopes[spec.IPv6RA.Scope] {
				return fmt.Errorf("%s spec.ipv6RA.scope references missing IPv6DHCPScope %q", res.ID(), spec.IPv6RA.Scope)
			}
		}
		if res.Kind == "IPv6DelegatedAddress" {
			spec, err := res.IPv6DelegatedAddressSpec()
			if err != nil {
				return err
			}
			if !prefixDelegations[spec.PrefixDelegation] {
				return fmt.Errorf("%s references missing IPv6PrefixDelegation %q", res.ID(), spec.PrefixDelegation)
			}
		}
		if res.Kind == "IPv6DHCPScope" {
			spec, err := res.IPv6DHCPScopeSpec()
			if err != nil {
				return err
			}
			if !dhcp6Servers[spec.Server] {
				return fmt.Errorf("%s references missing IPv6DHCPServer %q", res.ID(), spec.Server)
			}
			if !delegatedAddresses[spec.DelegatedAddress] {
				return fmt.Errorf("%s references missing IPv6DelegatedAddress %q", res.ID(), spec.DelegatedAddress)
			}
			if !stringInSlice(delegatedAddressInterfaces[spec.DelegatedAddress], dhcp6ServerSpecs[spec.Server].ListenInterfaces) {
				return fmt.Errorf("%s delegatedAddress interface %q must be listed in IPv6DHCPServer %q spec.listenInterfaces", res.ID(), delegatedAddressInterfaces[spec.DelegatedAddress], spec.Server)
			}
			if spec.SelfAddressPolicy != "" && !selfAddressPolicies[spec.SelfAddressPolicy] {
				return fmt.Errorf("%s references missing SelfAddressPolicy %q", res.ID(), spec.SelfAddressPolicy)
			}
		}
		if res.Kind == "SelfAddressPolicy" {
			spec, err := res.SelfAddressPolicySpec()
			if err != nil {
				return err
			}
			for i, candidate := range spec.Candidates {
				if candidate.Interface != "" && !interfaces[candidate.Interface] {
					return fmt.Errorf("%s spec.candidates[%d] references missing Interface %q", res.ID(), i, candidate.Interface)
				}
				if candidate.DelegatedAddress != "" && !delegatedAddresses[candidate.DelegatedAddress] {
					return fmt.Errorf("%s spec.candidates[%d] references missing IPv6DelegatedAddress %q", res.ID(), i, candidate.DelegatedAddress)
				}
			}
		}
		if res.Kind == "DNSConditionalForwarder" {
			spec, err := res.DNSConditionalForwarderSpec()
			if err != nil {
				return err
			}
			if spec.UpstreamInterface != "" && !interfaces[spec.UpstreamInterface] {
				return fmt.Errorf("%s references missing upstream Interface %q", res.ID(), spec.UpstreamInterface)
			}
		}
		if res.Kind == "DSLiteTunnel" {
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return err
			}
			if spec.LocalDelegatedAddress != "" && !delegatedAddresses[spec.LocalDelegatedAddress] {
				return fmt.Errorf("%s references missing local IPv6DelegatedAddress %q", res.ID(), spec.LocalDelegatedAddress)
			}
		}
		if res.Kind == "HealthCheck" {
			spec, err := res.HealthCheckSpec()
			if err != nil {
				return err
			}
			if spec.Interface != "" && !interfaces[spec.Interface] && !dsliteTunnels[spec.Interface] {
				return fmt.Errorf("%s references missing Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), spec.Interface)
			}
		}
		if res.Kind == "IPv4DefaultRoutePolicy" {
			spec, err := res.IPv4DefaultRoutePolicySpec()
			if err != nil {
				return err
			}
			for i, candidate := range spec.Candidates {
				if candidate.Interface != "" && !interfaces[candidate.Interface] && !dsliteTunnels[candidate.Interface] {
					return fmt.Errorf("%s spec.candidates[%d] references missing Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), i, candidate.Interface)
				}
				if candidate.RouteSet != "" && !routeSets[candidate.RouteSet] {
					return fmt.Errorf("%s spec.candidates[%d] references missing IPv4PolicyRouteSet %q", res.ID(), i, candidate.RouteSet)
				}
				if candidate.HealthCheck != "" && !healthChecks[candidate.HealthCheck] {
					return fmt.Errorf("%s spec.candidates[%d] references missing HealthCheck %q", res.ID(), i, candidate.HealthCheck)
				}
			}
		}
		if res.Kind == "Zone" {
			spec, err := res.ZoneSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.Interfaces {
				if !interfaces[name] && !dsliteTunnels[name] {
					return fmt.Errorf("%s spec.interfaces[%d] references missing Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), i, name)
				}
			}
		}
		if res.Kind == "FirewallPolicy" {
			spec, err := res.FirewallPolicySpec()
			if err != nil {
				return err
			}
			if err := validateRouterAccessZoneRefs(res.ID()+".spec.routerAccess.ssh", spec.RouterAccess.SSH, zones); err != nil {
				return err
			}
			if err := validateRouterAccessZoneRefs(res.ID()+".spec.routerAccess.dns", spec.RouterAccess.DNS, zones); err != nil {
				return err
			}
			if err := validateRouterAccessZoneRefs(res.ID()+".spec.routerAccess.dhcp", spec.RouterAccess.DHCP, zones); err != nil {
				return err
			}
		}
		if res.Kind == "ExposeService" {
			spec, err := res.ExposeServiceSpec()
			if err != nil {
				return err
			}
			if !zones[spec.FromZone] {
				return fmt.Errorf("%s spec.fromZone references missing Zone %q", res.ID(), spec.FromZone)
			}
			if spec.ViaInterface != "" && !interfaces[spec.ViaInterface] && !dsliteTunnels[spec.ViaInterface] {
				return fmt.Errorf("%s spec.viaInterface references missing Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), spec.ViaInterface)
			}
		}
	}
	return nil
}

func validateReconcilePolicy(spec api.ReconcilePolicySpec) error {
	switch spec.Mode {
	case "", "strict", "progressive":
	default:
		return fmt.Errorf("spec.reconcile.mode must be strict or progressive")
	}
	for _, name := range spec.ProtectedInterfaces {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("spec.reconcile.protectedInterfaces must not contain empty names")
		}
	}
	for _, name := range spec.ProtectedZones {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("spec.reconcile.protectedZones must not contain empty names")
		}
	}
	return nil
}

func validateRouterAccessZoneRefs(field string, spec api.FirewallRouterServiceSpec, zones map[string]bool) error {
	for i, zone := range spec.FromZones {
		if !zones[zone] {
			return fmt.Errorf("%s.fromZones[%d] references missing Zone %q", field, i, zone)
		}
	}
	return nil
}

func validateResource(res api.Resource) error {
	if res.APIVersion == "" {
		return fmt.Errorf("resource apiVersion is required")
	}
	if res.Kind == "" {
		return fmt.Errorf("resource kind is required")
	}
	if res.Metadata.Name == "" {
		return fmt.Errorf("%s/%s metadata.name is required", res.APIVersion, res.Kind)
	}

	switch res.Kind {
	case "LogSink":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.LogSinkSpec()
		if err != nil {
			return err
		}
		switch spec.Type {
		case "syslog":
			switch spec.Syslog.Network {
			case "", "unix", "unixgram", "tcp", "udp":
			default:
				return fmt.Errorf("%s spec.syslog.network must be unix, unixgram, tcp, or udp", res.ID())
			}
			switch defaultString(spec.Syslog.Facility, "local6") {
			case "kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7":
			default:
				return fmt.Errorf("%s spec.syslog.facility is invalid", res.ID())
			}
		case "plugin":
			if spec.Plugin.Path == "" {
				return fmt.Errorf("%s spec.plugin.path is required when type is plugin", res.ID())
			}
			if spec.Plugin.Timeout != "" {
				if _, err := time.ParseDuration(spec.Plugin.Timeout); err != nil {
					return fmt.Errorf("%s spec.plugin.timeout is invalid: %w", res.ID(), err)
				}
			}
		default:
			return fmt.Errorf("%s spec.type must be syslog or plugin", res.ID())
		}
		switch defaultString(spec.MinLevel, "info") {
		case "debug", "info", "warning", "error":
		default:
			return fmt.Errorf("%s spec.minLevel must be debug, info, warning, or error", res.ID())
		}
	case "Sysctl":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.SysctlSpec()
		if err != nil {
			return err
		}
		key := spec.Key
		if key == "" {
			return fmt.Errorf("%s spec.key is required", res.ID())
		}
		if strings.ContainsAny(key, " \t\n/") {
			return fmt.Errorf("%s spec.key contains invalid whitespace or slash", res.ID())
		}
		if spec.Value == "" {
			return fmt.Errorf("%s spec.value is required", res.ID())
		}
	case "NTPClient":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.NTPClientSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Provider, "systemd-timesyncd") {
		case "systemd-timesyncd":
		default:
			return fmt.Errorf("%s spec.provider must be systemd-timesyncd", res.ID())
		}
		switch defaultString(spec.Source, "static") {
		case "static":
		default:
			return fmt.Errorf("%s spec.source must be static", res.ID())
		}
		if spec.Managed && len(spec.Servers) == 0 {
			return fmt.Errorf("%s spec.servers is required when managed is true", res.ID())
		}
		for i, server := range spec.Servers {
			if strings.TrimSpace(server) == "" {
				return fmt.Errorf("%s spec.servers[%d] must not be empty", res.ID(), i)
			}
			if strings.ContainsAny(server, " \t\n\r") {
				return fmt.Errorf("%s spec.servers[%d] must be a single hostname or IP address", res.ID(), i)
			}
		}
	case "NixOSHost":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.NixOSHostSpec()
		if err != nil {
			return err
		}
		if spec.Hostname != "" && strings.ContainsAny(spec.Hostname, " \t\n/") {
			return fmt.Errorf("%s spec.hostname contains invalid whitespace or slash", res.ID())
		}
		if spec.Domain != "" && strings.ContainsAny(spec.Domain, " \t\n/") {
			return fmt.Errorf("%s spec.domain contains invalid whitespace or slash", res.ID())
		}
		switch spec.Boot.Loader {
		case "", "grub":
		default:
			return fmt.Errorf("%s spec.boot.loader is invalid", res.ID())
		}
		if spec.Boot.GrubDevice != "" && strings.ContainsAny(spec.Boot.GrubDevice, " \t\n\r") {
			return fmt.Errorf("%s spec.boot.grubDevice must be a single device path", res.ID())
		}
		switch spec.SSH.PermitRootLogin {
		case "", "no", "yes", "prohibit-password", "forced-commands-only":
		default:
			return fmt.Errorf("%s spec.ssh.permitRootLogin is invalid", res.ID())
		}
		for i, user := range spec.Users {
			if user.Name == "" {
				return fmt.Errorf("%s spec.users[%d].name is required", res.ID(), i)
			}
			if strings.ContainsAny(user.Name, " \t\n/:") {
				return fmt.Errorf("%s spec.users[%d].name contains invalid whitespace, slash, or colon", res.ID(), i)
			}
			for j, group := range user.Groups {
				if group == "" || strings.ContainsAny(group, " \t\n/:") {
					return fmt.Errorf("%s spec.users[%d].groups[%d] is invalid", res.ID(), i, j)
				}
			}
			for j, key := range user.SSHAuthorizedKeys {
				if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\n\r") {
					return fmt.Errorf("%s spec.users[%d].sshAuthorizedKeys[%d] is invalid", res.ID(), i, j)
				}
			}
		}
	case "Interface":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.InterfaceSpec()
		if err != nil {
			return err
		}
		if spec.IfName == "" {
			return fmt.Errorf("%s spec.ifname is required", res.ID())
		}
	case "PPPoEInterface":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.PPPoEInterfaceSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.Username == "" {
			return fmt.Errorf("%s spec.username is required", res.ID())
		}
		if spec.Password == "" && spec.PasswordFile == "" {
			return fmt.Errorf("%s spec.password or spec.passwordFile is required", res.ID())
		}
		if spec.Password != "" && spec.PasswordFile != "" {
			return fmt.Errorf("%s spec.password and spec.passwordFile are mutually exclusive", res.ID())
		}
		if spec.IfName != "" && strings.ContainsAny(spec.IfName, " \t\n/") {
			return fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if spec.IfName != "" && len(spec.IfName) > 15 {
			return fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if spec.IfName == "" && len("ppp-"+res.Metadata.Name) > 15 {
			return fmt.Errorf("%s spec.ifname is required when default PPP interface name exceeds 15 characters", res.ID())
		}
		if spec.ServiceName != "" && strings.ContainsAny(spec.ServiceName, "\n\r") {
			return fmt.Errorf("%s spec.serviceName contains invalid newline", res.ID())
		}
		if spec.ACName != "" && strings.ContainsAny(spec.ACName, "\n\r") {
			return fmt.Errorf("%s spec.acName contains invalid newline", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 1500) {
			return fmt.Errorf("%s spec.mtu must be within 576-1500", res.ID())
		}
		if spec.MRU != 0 && (spec.MRU < 576 || spec.MRU > 1500) {
			return fmt.Errorf("%s spec.mru must be within 576-1500", res.ID())
		}
		if spec.LCPInterval < 0 || spec.LCPFailure < 0 {
			return fmt.Errorf("%s spec.lcpInterval and spec.lcpFailure must be non-negative", res.ID())
		}
		switch spec.SecretEncoding {
		case "", "plain":
		default:
			return fmt.Errorf("%s spec.secretEncoding must be plain", res.ID())
		}
	case "IPv4StaticAddress":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4StaticAddressSpec()
		if err != nil {
			return err
		}
		addr := spec.Address
		if addr == "" {
			return fmt.Errorf("%s spec.address is required", res.ID())
		}
		if _, err := netip.ParsePrefix(addr); err != nil {
			return fmt.Errorf("%s spec.address is invalid: %w", res.ID(), err)
		}
		if spec.AllowOverlap && spec.AllowOverlapReason == "" {
			return fmt.Errorf("%s spec.allowOverlapReason is required when allowOverlap is true", res.ID())
		}
	case "IPv4DHCPAddress", "IPv6DHCPAddress":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
	case "IPv6PrefixDelegation":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6PrefixDelegationSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		switch spec.Profile {
		case "", "default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd":
		default:
			return fmt.Errorf("%s spec.profile must be default, ntt-ngn-direct-hikari-denwa, or ntt-hgw-lan-pd", res.ID())
		}
		if spec.PrefixLength != 0 && (spec.PrefixLength < 1 || spec.PrefixLength > 128) {
			return fmt.Errorf("%s spec.prefixLength must be within 1-128", res.ID())
		}
	case "IPv6DelegatedAddress":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return err
		}
		if spec.PrefixDelegation == "" {
			return fmt.Errorf("%s spec.prefixDelegation is required", res.ID())
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.AddressSuffix == "" {
			return fmt.Errorf("%s spec.addressSuffix is required", res.ID())
		}
		addr, err := netip.ParseAddr(spec.AddressSuffix)
		if err != nil || !addr.Is6() {
			return fmt.Errorf("%s spec.addressSuffix must be an IPv6 address suffix such as ::3", res.ID())
		}
		if spec.SubnetID != "" {
			if strings.HasPrefix(spec.SubnetID, "-") || strings.ContainsAny(spec.SubnetID, " \t\n/") {
				return fmt.Errorf("%s spec.subnetID must be a non-negative subnet id", res.ID())
			}
		}
	case "IPv4DHCPServer":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4DHCPServerSpec()
		if err != nil {
			return err
		}
		switch spec.Server {
		case "", "dnsmasq", "kea", "dhcpd":
		default:
			return fmt.Errorf("%s spec.server must be dnsmasq, kea, or dhcpd", res.ID())
		}
		switch spec.DNS.UpstreamSource {
		case "", "dhcp4", "static", "system", "none":
		default:
			return fmt.Errorf("%s spec.dns.upstreamSource must be dhcp4, static, system, or none", res.ID())
		}
		if spec.DNS.UpstreamSource == "dhcp4" && spec.DNS.UpstreamInterface == "" {
			return fmt.Errorf("%s spec.dns.upstreamInterface is required when dns.upstreamSource is dhcp4", res.ID())
		}
		if spec.DNS.UpstreamSource == "static" && len(spec.DNS.UpstreamServers) == 0 {
			return fmt.Errorf("%s spec.dns.upstreamServers is required when dns.upstreamSource is static", res.ID())
		}
		for _, dns := range spec.DNS.UpstreamServers {
			addr, err := netip.ParseAddr(dns)
			if err != nil {
				return fmt.Errorf("%s spec.dns.upstreamServers contains invalid address %q", res.ID(), dns)
			}
			if !addr.Is4() && !addr.Is6() {
				return fmt.Errorf("%s spec.dns.upstreamServers contains invalid address %q", res.ID(), dns)
			}
		}
		for i, name := range spec.ListenInterfaces {
			if name == "" {
				return fmt.Errorf("%s spec.listenInterfaces[%d] must not be empty", res.ID(), i)
			}
		}
	case "IPv4DHCPScope":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4DHCPScopeSpec()
		if err != nil {
			return err
		}
		if spec.Server == "" {
			return fmt.Errorf("%s spec.server is required", res.ID())
		}
		if spec.RangeStart == "" {
			return fmt.Errorf("%s spec.rangeStart is required", res.ID())
		}
		if spec.RangeEnd == "" {
			return fmt.Errorf("%s spec.rangeEnd is required", res.ID())
		}
		start, err := netip.ParseAddr(spec.RangeStart)
		if err != nil || !start.Is4() {
			return fmt.Errorf("%s spec.rangeStart must be an IPv4 address", res.ID())
		}
		end, err := netip.ParseAddr(spec.RangeEnd)
		if err != nil || !end.Is4() {
			return fmt.Errorf("%s spec.rangeEnd must be an IPv4 address", res.ID())
		}
		if start.Compare(end) > 0 {
			return fmt.Errorf("%s DHCP range start must be less than or equal to range end", res.ID())
		}
		routerSource := defaultString(spec.RouterSource, "interfaceAddress")
		switch routerSource {
		case "interfaceAddress", "none":
		case "static":
			if spec.Router == "" {
				return fmt.Errorf("%s spec.router is required when routerSource is static", res.ID())
			}
		default:
			return fmt.Errorf("%s spec.routerSource must be interfaceAddress, static, or none", res.ID())
		}
		if spec.Router != "" {
			router, err := netip.ParseAddr(spec.Router)
			if err != nil || !router.Is4() {
				return fmt.Errorf("%s spec.router must be an IPv4 address", res.ID())
			}
		}
		dnsSource := defaultString(spec.DNSSource, "self")
		switch dnsSource {
		case "dhcp4":
			if spec.DNSInterface == "" {
				return fmt.Errorf("%s spec.dnsInterface is required when dnsSource is dhcp4", res.ID())
			}
		case "static":
			if len(spec.DNSServers) == 0 {
				return fmt.Errorf("%s spec.dnsServers is required when dnsSource is static", res.ID())
			}
		case "self", "none":
		default:
			return fmt.Errorf("%s spec.dnsSource must be dhcp4, static, self, or none", res.ID())
		}
		for _, dns := range spec.DNSServers {
			addr, err := netip.ParseAddr(dns)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("%s spec.dnsServers entries must be IPv4 addresses", res.ID())
			}
		}
	case "IPv6DHCPServer":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6DHCPServerSpec()
		if err != nil {
			return err
		}
		switch spec.Server {
		case "", "dnsmasq":
		default:
			return fmt.Errorf("%s spec.server must be dnsmasq", res.ID())
		}
		for i, name := range spec.ListenInterfaces {
			if name == "" {
				return fmt.Errorf("%s spec.listenInterfaces[%d] must not be empty", res.ID(), i)
			}
		}
	case "IPv6DHCPScope":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6DHCPScopeSpec()
		if err != nil {
			return err
		}
		if spec.Server == "" {
			return fmt.Errorf("%s spec.server is required", res.ID())
		}
		if spec.DelegatedAddress == "" {
			return fmt.Errorf("%s spec.delegatedAddress is required", res.ID())
		}
		switch defaultString(spec.Mode, "stateless") {
		case "stateless", "ra-only":
		default:
			return fmt.Errorf("%s spec.mode must be stateless or ra-only", res.ID())
		}
		switch defaultString(spec.DNSSource, "self") {
		case "self", "none":
		case "static":
			if len(spec.DNSServers) == 0 {
				return fmt.Errorf("%s spec.dnsServers is required when dnsSource is static", res.ID())
			}
		default:
			return fmt.Errorf("%s spec.dnsSource must be self, static, or none", res.ID())
		}
		for _, dns := range spec.DNSServers {
			addr, err := netip.ParseAddr(dns)
			if err != nil || !addr.Is6() {
				return fmt.Errorf("%s spec.dnsServers entries must be IPv6 addresses", res.ID())
			}
		}
	case "SelfAddressPolicy":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.SelfAddressPolicySpec()
		if err != nil {
			return err
		}
		switch spec.AddressFamily {
		case "ipv4", "ipv6":
		default:
			return fmt.Errorf("%s spec.addressFamily must be ipv4 or ipv6", res.ID())
		}
		if len(spec.Candidates) == 0 {
			return fmt.Errorf("%s spec.candidates is required", res.ID())
		}
		for i, candidate := range spec.Candidates {
			switch candidate.Source {
			case "delegatedAddress":
				if spec.AddressFamily != "ipv6" {
					return fmt.Errorf("%s spec.candidates[%d].source delegatedAddress is only valid for ipv6", res.ID(), i)
				}
				if candidate.DelegatedAddress == "" {
					return fmt.Errorf("%s spec.candidates[%d].delegatedAddress is required", res.ID(), i)
				}
				if candidate.Address != "" || candidate.Interface != "" {
					return fmt.Errorf("%s spec.candidates[%d] delegatedAddress candidate cannot set address or interface", res.ID(), i)
				}
				if candidate.AddressSuffix != "" {
					addr, err := netip.ParseAddr(candidate.AddressSuffix)
					if err != nil || !addr.Is6() {
						return fmt.Errorf("%s spec.candidates[%d].addressSuffix must be an IPv6 suffix", res.ID(), i)
					}
				}
			case "interfaceAddress":
				if candidate.Interface == "" {
					return fmt.Errorf("%s spec.candidates[%d].interface is required", res.ID(), i)
				}
				if candidate.MatchSuffix != "" {
					addr, err := netip.ParseAddr(candidate.MatchSuffix)
					if err != nil || !addr.Is6() {
						return fmt.Errorf("%s spec.candidates[%d].matchSuffix must be an IPv6 suffix", res.ID(), i)
					}
				}
			case "static":
				if candidate.Address == "" {
					return fmt.Errorf("%s spec.candidates[%d].address is required", res.ID(), i)
				}
				addr, err := netip.ParseAddr(candidate.Address)
				if err != nil || (spec.AddressFamily == "ipv4" && !addr.Is4()) || (spec.AddressFamily == "ipv6" && !addr.Is6()) {
					return fmt.Errorf("%s spec.candidates[%d].address must match addressFamily", res.ID(), i)
				}
			default:
				return fmt.Errorf("%s spec.candidates[%d].source must be delegatedAddress, interfaceAddress, or static", res.ID(), i)
			}
			if candidate.Ordinal < 0 {
				return fmt.Errorf("%s spec.candidates[%d].ordinal must be greater than 0", res.ID(), i)
			}
		}
	case "DNSConditionalForwarder":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DNSConditionalForwarderSpec()
		if err != nil {
			return err
		}
		if spec.Domain == "" {
			return fmt.Errorf("%s spec.domain is required", res.ID())
		}
		if strings.ContainsAny(spec.Domain, " \t\n/") {
			return fmt.Errorf("%s spec.domain contains invalid whitespace or slash", res.ID())
		}
		upstreamSource := defaultString(spec.UpstreamSource, "static")
		switch upstreamSource {
		case "static":
			if len(spec.UpstreamServers) == 0 {
				return fmt.Errorf("%s spec.upstreamServers is required when upstreamSource is static", res.ID())
			}
		case "dhcp4", "dhcp6":
			if spec.UpstreamInterface == "" {
				return fmt.Errorf("%s spec.upstreamInterface is required when upstreamSource is %s", res.ID(), upstreamSource)
			}
		default:
			return fmt.Errorf("%s spec.upstreamSource must be static, dhcp4, or dhcp6", res.ID())
		}
		for _, server := range spec.UpstreamServers {
			addr, err := netip.ParseAddr(server)
			if err != nil || (!addr.Is4() && !addr.Is6()) {
				return fmt.Errorf("%s spec.upstreamServers contains invalid address %q", res.ID(), server)
			}
		}
	case "DSLiteTunnel":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.AFTRFQDN == "" && spec.RemoteAddress == "" {
			return fmt.Errorf("%s spec.aftrFQDN or spec.remoteAddress is required", res.ID())
		}
		if spec.AFTRFQDN != "" && strings.ContainsAny(spec.AFTRFQDN, " \t\n/") {
			return fmt.Errorf("%s spec.aftrFQDN contains invalid whitespace or slash", res.ID())
		}
		if spec.AFTRAddressOrdinal < 0 {
			return fmt.Errorf("%s spec.aftrAddressOrdinal must be greater than 0", res.ID())
		}
		switch defaultString(spec.AFTRAddressSelection, "ordinal") {
		case "ordinal", "ordinalModulo":
		default:
			return fmt.Errorf("%s spec.aftrAddressSelection must be ordinal or ordinalModulo", res.ID())
		}
		if spec.RemoteAddress != "" {
			addr, err := netip.ParseAddr(spec.RemoteAddress)
			if err != nil || !addr.Is6() {
				return fmt.Errorf("%s spec.remoteAddress must be an IPv6 address", res.ID())
			}
		}
		if spec.LocalAddress != "" {
			addr, err := netip.ParseAddr(spec.LocalAddress)
			if err != nil || !addr.Is6() {
				return fmt.Errorf("%s spec.localAddress must be an IPv6 address", res.ID())
			}
		}
		localAddressSource := defaultString(spec.LocalAddressSource, "interface")
		switch localAddressSource {
		case "interface":
		case "static":
			if spec.LocalAddress == "" {
				return fmt.Errorf("%s spec.localAddress is required when localAddressSource is static", res.ID())
			}
		case "delegatedAddress":
			if spec.LocalDelegatedAddress == "" {
				return fmt.Errorf("%s spec.localDelegatedAddress is required when localAddressSource is delegatedAddress", res.ID())
			}
			if spec.LocalAddressSuffix != "" {
				addr, err := netip.ParseAddr(spec.LocalAddressSuffix)
				if err != nil || !addr.Is6() {
					return fmt.Errorf("%s spec.localAddressSuffix must be an IPv6 suffix such as ::100", res.ID())
				}
			}
		default:
			return fmt.Errorf("%s spec.localAddressSource must be interface, static, or delegatedAddress", res.ID())
		}
		for _, server := range spec.AFTRDNSServers {
			addr, err := netip.ParseAddr(server)
			if err != nil || (!addr.Is4() && !addr.Is6()) {
				return fmt.Errorf("%s spec.aftrDNSServers contains invalid address %q", res.ID(), server)
			}
		}
		if spec.MTU != 0 && (spec.MTU < 1280 || spec.MTU > 65535) {
			return fmt.Errorf("%s spec.mtu must be within 1280-65535", res.ID())
		}
	case "StatePolicy":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.StatePolicySpec()
		if err != nil {
			return err
		}
		if spec.Variable == "" {
			return fmt.Errorf("%s spec.variable is required", res.ID())
		}
		if len(spec.Values) == 0 {
			return fmt.Errorf("%s spec.values is required", res.ID())
		}
		values := map[string]bool{}
		for i, value := range spec.Values {
			if value.Value == "" {
				return fmt.Errorf("%s spec.values[%d].value is required", res.ID(), i)
			}
			if values[value.Value] {
				return fmt.Errorf("%s duplicates value %q", res.ID(), value.Value)
			}
			values[value.Value] = true
			if value.When.DNSResolve.Name != "" {
				if strings.ContainsAny(value.When.DNSResolve.Name, " \t\n/") {
					return fmt.Errorf("%s spec.values[%d].when.dnsResolve.name contains invalid whitespace or slash", res.ID(), i)
				}
				if defaultString(value.When.DNSResolve.Type, "AAAA") != "AAAA" {
					return fmt.Errorf("%s spec.values[%d].when.dnsResolve.type must be AAAA", res.ID(), i)
				}
				switch defaultString(value.When.DNSResolve.UpstreamSource, "system") {
				case "system", "static", "dhcp4", "dhcp6":
				default:
					return fmt.Errorf("%s spec.values[%d].when.dnsResolve.upstreamSource must be system, static, dhcp4, or dhcp6", res.ID(), i)
				}
				for _, server := range value.When.DNSResolve.UpstreamServers {
					addr, err := netip.ParseAddr(server)
					if err != nil || (!addr.Is4() && !addr.Is6()) {
						return fmt.Errorf("%s spec.values[%d].when.dnsResolve.upstreamServers contains invalid address %q", res.ID(), i, server)
					}
				}
			}
			if value.When.IPv6PrefixDelegation.UnavailableFor != "" {
				if _, err := time.ParseDuration(value.When.IPv6PrefixDelegation.UnavailableFor); err != nil {
					return fmt.Errorf("%s spec.values[%d].when.ipv6PrefixDelegation.unavailableFor is invalid: %w", res.ID(), i, err)
				}
			}
		}
	case "HealthCheck":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Type, "ping") {
		case "ping":
		default:
			return fmt.Errorf("%s spec.type must be ping", res.ID())
		}
		switch defaultString(spec.Role, "next-hop") {
		case "link", "next-hop", "internet", "service", "policy":
		default:
			return fmt.Errorf("%s spec.role must be link, next-hop, internet, service, or policy", res.ID())
		}
		addressFamily := defaultString(spec.AddressFamily, "ipv4")
		switch addressFamily {
		case "ipv4", "ipv6":
		default:
			return fmt.Errorf("%s spec.addressFamily must be ipv4 or ipv6", res.ID())
		}
		switch defaultString(spec.TargetSource, "auto") {
		case "auto", "static", "defaultGateway", "dsliteRemote":
		default:
			return fmt.Errorf("%s spec.targetSource must be auto, static, defaultGateway, or dsliteRemote", res.ID())
		}
		if spec.Target != "" {
			addr, err := netip.ParseAddr(spec.Target)
			if err != nil {
				return fmt.Errorf("%s spec.target must be an IP address", res.ID())
			}
			if addressFamily == "ipv4" && !addr.Is4() {
				return fmt.Errorf("%s spec.target must be an IPv4 address", res.ID())
			}
			if addressFamily == "ipv6" && !addr.Is6() {
				return fmt.Errorf("%s spec.target must be an IPv6 address", res.ID())
			}
		}
		if defaultString(spec.TargetSource, "auto") == "static" && spec.Target == "" {
			return fmt.Errorf("%s spec.target is required when targetSource is static", res.ID())
		}
		interval := defaultString(spec.Interval, "60s")
		if d, err := time.ParseDuration(interval); err != nil || d <= 0 {
			return fmt.Errorf("%s spec.interval must be a positive duration", res.ID())
		}
		timeout := defaultString(spec.Timeout, "3s")
		if d, err := time.ParseDuration(timeout); err != nil || d <= 0 {
			return fmt.Errorf("%s spec.timeout must be a positive duration", res.ID())
		}
		if spec.HealthyThreshold < 0 || spec.UnhealthyThreshold < 0 {
			return fmt.Errorf("%s spec.healthyThreshold and spec.unhealthyThreshold must be non-negative", res.ID())
		}
	case "IPv4DefaultRoutePolicy":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4DefaultRoutePolicySpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Mode, "priority") {
		case "priority":
		default:
			return fmt.Errorf("%s spec.mode must be priority", res.ID())
		}
		if len(spec.Candidates) == 0 {
			return fmt.Errorf("%s spec.candidates is required", res.ID())
		}
		for _, cidr := range spec.SourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.sourceCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		for _, cidr := range spec.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.destinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		seenPriorities := map[int]bool{}
		seenMarks := map[int]bool{}
		seenTables := map[int]bool{}
		for i, candidate := range spec.Candidates {
			if (candidate.Interface == "") == (candidate.RouteSet == "") {
				return fmt.Errorf("%s spec.candidates[%d] must set exactly one of interface or routeSet", res.ID(), i)
			}
			isRouteSet := candidate.RouteSet != ""
			source := defaultString(candidate.GatewaySource, "none")
			if isRouteSet {
				if candidate.GatewaySource != "" || candidate.Gateway != "" || candidate.Table != 0 || candidate.Mark != 0 || candidate.RouteMetric != 0 {
					return fmt.Errorf("%s spec.candidates[%d] routeSet candidates cannot set gatewaySource, gateway, table, mark, or routeMetric", res.ID(), i)
				}
			} else {
				switch source {
				case "none":
					if candidate.Gateway != "" {
						return fmt.Errorf("%s spec.candidates[%d].gateway is only valid when gatewaySource is static", res.ID(), i)
					}
				case "dhcp4":
				case "static":
					gateway := candidate.Gateway
					if gateway == "" {
						return fmt.Errorf("%s spec.candidates[%d].gateway is required when gatewaySource is static", res.ID(), i)
					}
					addr, err := netip.ParseAddr(gateway)
					if err != nil || !addr.Is4() {
						return fmt.Errorf("%s spec.candidates[%d].gateway must be an IPv4 address", res.ID(), i)
					}
				default:
					return fmt.Errorf("%s spec.candidates[%d].gatewaySource must be none, dhcp4, or static", res.ID(), i)
				}
			}
			if candidate.Priority < 1 {
				return fmt.Errorf("%s spec.candidates[%d].priority must be greater than 0", res.ID(), i)
			}
			if seenPriorities[candidate.Priority] {
				return fmt.Errorf("%s spec.candidates[%d].priority duplicates another candidate", res.ID(), i)
			}
			seenPriorities[candidate.Priority] = true
			if !isRouteSet {
				if candidate.Table < 1 {
					return fmt.Errorf("%s spec.candidates[%d].table must be greater than 0", res.ID(), i)
				}
				if candidate.Mark < 1 {
					return fmt.Errorf("%s spec.candidates[%d].mark must be greater than 0", res.ID(), i)
				}
				if seenMarks[candidate.Mark] {
					return fmt.Errorf("%s spec.candidates[%d].mark duplicates another candidate", res.ID(), i)
				}
				if seenTables[candidate.Table] {
					return fmt.Errorf("%s spec.candidates[%d].table duplicates another candidate", res.ID(), i)
				}
				seenMarks[candidate.Mark] = true
				seenTables[candidate.Table] = true
			}
		}
	case "IPv4SourceNAT":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4SourceNATSpec()
		if err != nil {
			return err
		}
		if spec.OutboundInterface == "" {
			return fmt.Errorf("%s spec.outboundInterface is required", res.ID())
		}
		if len(spec.SourceCIDRs) == 0 {
			return fmt.Errorf("%s spec.sourceCIDRs is required", res.ID())
		}
		for _, cidr := range spec.SourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.sourceCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		switch spec.Translation.Type {
		case "interfaceAddress":
		case "address":
			addr, err := netip.ParseAddr(spec.Translation.Address)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("%s spec.translation.address must be an IPv4 address", res.ID())
			}
		case "pool":
			if len(spec.Translation.Addresses) == 0 {
				return fmt.Errorf("%s spec.translation.addresses is required when translation.type is pool", res.ID())
			}
			for _, value := range spec.Translation.Addresses {
				addr, err := netip.ParseAddr(value)
				if err != nil || !addr.Is4() {
					return fmt.Errorf("%s spec.translation.addresses entries must be IPv4 addresses", res.ID())
				}
			}
		default:
			return fmt.Errorf("%s spec.translation.type must be interfaceAddress, address, or pool", res.ID())
		}
		portMappingType := defaultString(spec.Translation.PortMapping.Type, "auto")
		switch portMappingType {
		case "auto", "preserve":
			if spec.Translation.PortMapping.Start != 0 || spec.Translation.PortMapping.End != 0 {
				return fmt.Errorf("%s spec.translation.portMapping start/end are only valid when type is range", res.ID())
			}
		case "range":
			start := spec.Translation.PortMapping.Start
			end := spec.Translation.PortMapping.End
			if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
				return fmt.Errorf("%s spec.translation.portMapping range must be within 1-65535 and start must be <= end", res.ID())
			}
		default:
			return fmt.Errorf("%s spec.translation.portMapping.type must be auto, preserve, or range", res.ID())
		}
	case "IPv4PolicyRoute":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4PolicyRouteSpec()
		if err != nil {
			return err
		}
		if spec.OutboundInterface == "" {
			return fmt.Errorf("%s spec.outboundInterface is required", res.ID())
		}
		if spec.Table < 1 {
			return fmt.Errorf("%s spec.table must be greater than 0", res.ID())
		}
		if spec.Priority < 1 || spec.Priority > 32765 {
			return fmt.Errorf("%s spec.priority must be within 1-32765", res.ID())
		}
		if spec.Mark < 1 {
			return fmt.Errorf("%s spec.mark must be greater than 0", res.ID())
		}
		if len(spec.SourceCIDRs) == 0 && len(spec.DestinationCIDRs) == 0 {
			return fmt.Errorf("%s spec.sourceCIDRs or spec.destinationCIDRs is required", res.ID())
		}
		for _, cidr := range spec.SourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.sourceCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		for _, cidr := range spec.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.destinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
	case "IPv4PolicyRouteSet":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4PolicyRouteSetSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Mode, "hash") {
		case "hash":
		default:
			return fmt.Errorf("%s spec.mode must be hash", res.ID())
		}
		if len(spec.HashFields) == 0 {
			return fmt.Errorf("%s spec.hashFields is required", res.ID())
		}
		for _, field := range spec.HashFields {
			switch field {
			case "sourceAddress", "destinationAddress":
			default:
				return fmt.Errorf("%s spec.hashFields entries must be sourceAddress or destinationAddress", res.ID())
			}
		}
		if len(spec.SourceCIDRs) == 0 && len(spec.DestinationCIDRs) == 0 {
			return fmt.Errorf("%s spec.sourceCIDRs or spec.destinationCIDRs is required", res.ID())
		}
		for _, cidr := range spec.SourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.sourceCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		for _, cidr := range spec.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.destinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		if len(spec.Targets) < 2 {
			return fmt.Errorf("%s spec.targets must contain at least two targets", res.ID())
		}
		seenMarks := map[int]bool{}
		seenPriorities := map[int]bool{}
		for i, target := range spec.Targets {
			if target.OutboundInterface == "" {
				return fmt.Errorf("%s spec.targets[%d].outboundInterface is required", res.ID(), i)
			}
			if target.Table < 1 {
				return fmt.Errorf("%s spec.targets[%d].table must be greater than 0", res.ID(), i)
			}
			if target.Priority < 1 || target.Priority > 32765 {
				return fmt.Errorf("%s spec.targets[%d].priority must be within 1-32765", res.ID(), i)
			}
			if target.Mark < 1 {
				return fmt.Errorf("%s spec.targets[%d].mark must be greater than 0", res.ID(), i)
			}
			if seenMarks[target.Mark] {
				return fmt.Errorf("%s spec.targets[%d].mark duplicates another target", res.ID(), i)
			}
			if seenPriorities[target.Priority] {
				return fmt.Errorf("%s spec.targets[%d].priority duplicates another target", res.ID(), i)
			}
			seenMarks[target.Mark] = true
			seenPriorities[target.Priority] = true
		}
	case "IPv4ReversePathFilter":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4ReversePathFilterSpec()
		if err != nil {
			return err
		}
		switch spec.Target {
		case "all", "default":
			if spec.Interface != "" {
				return fmt.Errorf("%s spec.interface is only valid when target is interface", res.ID())
			}
		case "interface":
			if spec.Interface == "" {
				return fmt.Errorf("%s spec.interface is required when target is interface", res.ID())
			}
		default:
			return fmt.Errorf("%s spec.target must be all, default, or interface", res.ID())
		}
		switch spec.Mode {
		case "disabled", "strict", "loose":
		default:
			return fmt.Errorf("%s spec.mode must be disabled, strict, or loose", res.ID())
		}
	case "PathMTUPolicy":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.PathMTUPolicySpec()
		if err != nil {
			return err
		}
		if spec.FromInterface == "" {
			return fmt.Errorf("%s spec.fromInterface is required", res.ID())
		}
		if len(spec.ToInterfaces) == 0 {
			return fmt.Errorf("%s spec.toInterfaces is required", res.ID())
		}
		for i, name := range spec.ToInterfaces {
			if name == "" {
				return fmt.Errorf("%s spec.toInterfaces[%d] must not be empty", res.ID(), i)
			}
		}
		switch defaultString(spec.MTU.Source, "minInterface") {
		case "minInterface":
			if spec.MTU.Value != 0 {
				return fmt.Errorf("%s spec.mtu.value is only valid when mtu.source is static", res.ID())
			}
		case "static":
			if spec.MTU.Value < 1280 || spec.MTU.Value > 65535 {
				return fmt.Errorf("%s spec.mtu.value must be within 1280-65535", res.ID())
			}
		default:
			return fmt.Errorf("%s spec.mtu.source must be minInterface or static", res.ID())
		}
		if spec.IPv6RA.Enabled && spec.IPv6RA.Scope == "" {
			return fmt.Errorf("%s spec.ipv6RA.scope is required when ipv6RA.enabled is true", res.ID())
		}
		if spec.TCPMSSClamp.Enabled {
			seenFamilies := map[string]bool{}
			for i, family := range spec.TCPMSSClamp.Families {
				switch family {
				case "ipv4", "ipv6":
				default:
					return fmt.Errorf("%s spec.tcpMSSClamp.families[%d] must be ipv4 or ipv6", res.ID(), i)
				}
				if seenFamilies[family] {
					return fmt.Errorf("%s spec.tcpMSSClamp.families[%d] duplicates another family", res.ID(), i)
				}
				seenFamilies[family] = true
			}
		}
	case "Zone":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.ZoneSpec()
		if err != nil {
			return err
		}
		if len(spec.Interfaces) == 0 {
			return fmt.Errorf("%s spec.interfaces is required", res.ID())
		}
		seen := map[string]bool{}
		for i, name := range spec.Interfaces {
			if name == "" {
				return fmt.Errorf("%s spec.interfaces[%d] is required", res.ID(), i)
			}
			if seen[name] {
				return fmt.Errorf("%s spec.interfaces[%d] duplicates %q", res.ID(), i, name)
			}
			seen[name] = true
		}
	case "FirewallPolicy":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.FirewallPolicySpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Preset, "home-router") {
		case "home-router":
		default:
			return fmt.Errorf("%s spec.preset must be home-router", res.ID())
		}
		for field, value := range map[string]string{
			"input.default":   defaultString(spec.Input.Default, "drop"),
			"forward.default": defaultString(spec.Forward.Default, "drop"),
		} {
			switch value {
			case "accept", "drop":
			default:
				return fmt.Errorf("%s spec.%s must be accept or drop", res.ID(), field)
			}
		}
	case "ExposeService":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.ExposeServiceSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Family, "ipv4") {
		case "ipv4":
		case "ipv6":
			return fmt.Errorf("%s spec.family=ipv6 is reserved for future pinhole support", res.ID())
		default:
			return fmt.Errorf("%s spec.family must be ipv4 or ipv6", res.ID())
		}
		if spec.FromZone == "" {
			return fmt.Errorf("%s spec.fromZone is required", res.ID())
		}
		switch spec.Protocol {
		case "tcp", "udp":
		default:
			return fmt.Errorf("%s spec.protocol must be tcp or udp", res.ID())
		}
		if spec.ExternalPort < 1 || spec.ExternalPort > 65535 {
			return fmt.Errorf("%s spec.externalPort must be within 1-65535", res.ID())
		}
		if spec.InternalPort < 1 || spec.InternalPort > 65535 {
			return fmt.Errorf("%s spec.internalPort must be within 1-65535", res.ID())
		}
		addr, err := netip.ParseAddr(spec.InternalAddress)
		if err != nil || !addr.Is4() {
			return fmt.Errorf("%s spec.internalAddress must be an IPv4 address", res.ID())
		}
		for i, cidr := range spec.Sources {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.sources[%d] must be an IPv4 prefix", res.ID(), i)
			}
		}
	case "Hostname":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.HostnameSpec()
		if err != nil {
			return err
		}
		hostname := spec.Hostname
		if hostname == "" {
			return fmt.Errorf("%s spec.hostname is required", res.ID())
		}
		if strings.ContainsAny(hostname, " \t\n/") {
			return fmt.Errorf("%s spec.hostname contains invalid whitespace or slash", res.ID())
		}
	default:
		return fmt.Errorf("unsupported resource kind %s in %s", res.Kind, res.ID())
	}
	return nil
}

func interfaceRef(res api.Resource) (string, error) {
	switch res.Kind {
	case "IPv4StaticAddress":
		spec, err := res.IPv4StaticAddressSpec()
		return spec.Interface, err
	case "IPv4DHCPAddress":
		spec, err := res.IPv4DHCPAddressSpec()
		return spec.Interface, err
	case "IPv4DHCPServer", "IPv6DHCPServer":
		return "", nil
	case "IPv4DHCPScope":
		spec, err := res.IPv4DHCPScopeSpec()
		return spec.Interface, err
	case "IPv6DHCPAddress":
		spec, err := res.IPv6DHCPAddressSpec()
		return spec.Interface, err
	case "IPv6PrefixDelegation":
		spec, err := res.IPv6PrefixDelegationSpec()
		return spec.Interface, err
	case "IPv6DelegatedAddress":
		spec, err := res.IPv6DelegatedAddressSpec()
		return spec.Interface, err
	case "IPv6DHCPScope":
		return "", nil
	case "DNSConditionalForwarder":
		return "", nil
	case "DSLiteTunnel":
		spec, err := res.DSLiteTunnelSpec()
		return spec.Interface, err
	case "PPPoEInterface":
		spec, err := res.PPPoEInterfaceSpec()
		return spec.Interface, err
	default:
		return "", fmt.Errorf("%s has no interface reference", res.ID())
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func stringInSlice(value string, values []string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
