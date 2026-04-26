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

	seen := map[string]bool{}
	interfaces := map[string]bool{}
	dhcp4Servers := map[string]bool{}
	dhcp6Servers := map[string]bool{}
	prefixDelegations := map[string]bool{}
	delegatedAddresses := map[string]bool{}
	selfAddressPolicies := map[string]bool{}
	dsliteTunnels := map[string]bool{}
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
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv4DHCPServer" {
			dhcp4Servers[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6DHCPServer" {
			dhcp6Servers[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6PrefixDelegation" {
			prefixDelegations[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6DelegatedAddress" {
			delegatedAddresses[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "SelfAddressPolicy" {
			selfAddressPolicies[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DSLiteTunnel" {
			dsliteTunnels[res.Metadata.Name] = true
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
	}

	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "IPv4StaticAddress", "IPv4DHCPAddress", "IPv4DHCPScope", "IPv6DHCPAddress", "IPv6PrefixDelegation", "IPv6DelegatedAddress", "IPv4DefaultRoute", "DSLiteTunnel":
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
		}
		if res.Kind == "IPv4DHCPScope" {
			spec, err := res.IPv4DHCPScopeSpec()
			if err != nil {
				return err
			}
			if !dhcp4Servers[spec.Server] {
				return fmt.Errorf("%s references missing IPv4DHCPServer %q", res.ID(), spec.Server)
			}
			if spec.DNSInterface != "" && !interfaces[spec.DNSInterface] {
				return fmt.Errorf("%s references missing DNS Interface %q", res.ID(), spec.DNSInterface)
			}
		}
		if res.Kind == "IPv4SourceNAT" {
			spec, err := res.IPv4SourceNATSpec()
			if err != nil {
				return err
			}
			if !interfaces[spec.OutboundInterface] && !dsliteTunnels[spec.OutboundInterface] {
				return fmt.Errorf("%s references missing outbound Interface or DSLiteTunnel %q", res.ID(), spec.OutboundInterface)
			}
		}
		if res.Kind == "IPv4PolicyRoute" {
			spec, err := res.IPv4PolicyRouteSpec()
			if err != nil {
				return err
			}
			if !interfaces[spec.OutboundInterface] && !dsliteTunnels[spec.OutboundInterface] {
				return fmt.Errorf("%s references missing outbound Interface or DSLiteTunnel %q", res.ID(), spec.OutboundInterface)
			}
		}
		if res.Kind == "IPv4PolicyRouteSet" {
			spec, err := res.IPv4PolicyRouteSetSpec()
			if err != nil {
				return err
			}
			for _, target := range spec.Targets {
				if !interfaces[target.OutboundInterface] && !dsliteTunnels[target.OutboundInterface] {
					return fmt.Errorf("%s target %q references missing outbound Interface or DSLiteTunnel %q", res.ID(), target.Name, target.OutboundInterface)
				}
			}
		}
		if res.Kind == "IPv4ReversePathFilter" {
			spec, err := res.IPv4ReversePathFilterSpec()
			if err != nil {
				return err
			}
			if spec.Target == "interface" && !interfaces[spec.Interface] && !dsliteTunnels[spec.Interface] {
				return fmt.Errorf("%s references missing Interface or DSLiteTunnel %q", res.ID(), spec.Interface)
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
	case "IPv4DefaultRoute":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4DefaultRouteSpec()
		if err != nil {
			return err
		}
		source := spec.GatewaySource
		if source == "" {
			return fmt.Errorf("%s spec.gatewaySource is required", res.ID())
		}
		switch source {
		case "dhcp4":
		case "static":
			gateway := spec.Gateway
			if gateway == "" {
				return fmt.Errorf("%s spec.gateway is required when gatewaySource is static", res.ID())
			}
			addr, err := netip.ParseAddr(gateway)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("%s spec.gateway must be an IPv4 address", res.ID())
			}
		default:
			return fmt.Errorf("%s spec.gatewaySource must be dhcp4 or static", res.ID())
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
	case "IPv4DefaultRoute":
		spec, err := res.IPv4DefaultRouteSpec()
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
