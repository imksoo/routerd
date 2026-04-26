package config

import (
	"fmt"
	"net/netip"
	"strings"

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
	dhcpServers := map[string]bool{}
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
			dhcpServers[res.Metadata.Name] = true
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
		case "IPv4StaticAddress", "IPv4DHCPAddress", "IPv4DHCPScope", "IPv6DHCPAddress", "IPv4DefaultRoute":
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
			if !dhcpServers[spec.Server] {
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
			if !interfaces[spec.OutboundInterface] {
				return fmt.Errorf("%s references missing outbound Interface %q", res.ID(), spec.OutboundInterface)
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
	case "IPv4DHCPServer":
		return "", nil
	case "IPv4DHCPScope":
		spec, err := res.IPv4DHCPScopeSpec()
		return spec.Interface, err
	case "IPv6DHCPAddress":
		spec, err := res.IPv6DHCPAddressSpec()
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
