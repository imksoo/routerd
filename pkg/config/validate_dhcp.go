// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/platform"
)

func validateDHCPResource(res api.Resource, targetOS platform.OS) (bool, error) {
	switch res.Kind {
	case "IPv6RouterAdvertisement":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6RouterAdvertisementSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.PrefixSource != "" {
			return true, fmt.Errorf("%s spec.prefixSource was removed; use spec.prefix or spec.prefixFrom", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return true, fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		if spec.Prefix == "" && spec.PrefixFrom.Resource == "" {
			return true, fmt.Errorf("%s spec.prefix or spec.prefixFrom is required", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 1280 || spec.MTU > 65535) {
			return true, fmt.Errorf("%s spec.mtu must be within 1280-65535", res.ID())
		}
		switch spec.PRFPreference {
		case "", "low", "medium", "high":
		default:
			return true, fmt.Errorf("%s spec.prfPreference must be low, medium, or high", res.ID())
		}
		for _, server := range spec.RDNSS {
			if strings.HasPrefix(strings.TrimSpace(server), "${") {
				return true, fmt.Errorf("%s spec.rdnss status expressions were removed; use spec.rdnssFrom", res.ID())
			}
			addr, err := netip.ParseAddr(server)
			if err != nil || !addr.Is6() {
				return true, fmt.Errorf("%s spec.rdnss entries must be IPv6 addresses or status references", res.ID())
			}
		}
		if len(spec.DNSSL) > 0 && len(spec.DNSSLFrom) > 0 {
			return true, fmt.Errorf("%s spec.dnssl and spec.dnsslFrom cannot both be set", res.ID())
		}
		for i, domain := range spec.DNSSL {
			if err := validateDomainValue(domain); err != nil {
				return true, fmt.Errorf("%s spec.dnssl[%d]: %w", res.ID(), i, err)
			}
		}
		for i, source := range spec.DNSSLFrom {
			if err := validateDNSZoneDomainSource(source); err != nil {
				return true, fmt.Errorf("%s spec.dnsslFrom[%d]: %w", res.ID(), i, err)
			}
		}
	case "DHCPv6Server":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv6ServerSpec()
		if err != nil {
			return true, err
		}
		if len(spec.ReadyWhen) > 0 {
			return true, fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		switch defaultString(spec.Role, "server") {
		case "server", "transit":
		default:
			return true, fmt.Errorf("%s spec.role must be server or transit", res.ID())
		}
		switch spec.Server {
		case "", "dnsmasq":
		default:
			return true, fmt.Errorf("%s spec.server must be dnsmasq", res.ID())
		}
		for i, name := range spec.ListenInterfaces {
			if name == "" {
				return true, fmt.Errorf("%s spec.listenInterfaces[%d] must not be empty", res.ID(), i)
			}
		}
		rendersLANService := spec.Interface != "" || spec.DelegatedAddress != "" || spec.Mode != "" || spec.AddressPool.Start != "" || spec.AddressPool.End != "" || len(spec.DNSServers) > 0 || len(spec.SNTPServers) > 0 || len(spec.DomainSearch) > 0 || len(spec.DomainSearchFrom) > 0
		if rendersLANService && spec.Interface == "" && spec.DelegatedAddress == "" {
			return true, fmt.Errorf("%s spec.interface is required when rendering DHCPv6 LAN service", res.ID())
		}
		switch spec.Mode {
		case "", "stateless", "stateful", "both", "ra-only":
		default:
			return true, fmt.Errorf("%s spec.mode must be stateless, stateful, both, or ra-only", res.ID())
		}
		if spec.DelegatedAddress != "" && spec.Mode != "" && defaultString(spec.Mode, "stateless") != "stateless" && spec.Mode != "ra-only" {
			return true, fmt.Errorf("%s spec.mode must be stateless or ra-only when spec.delegatedAddress is set", res.ID())
		}
		if spec.DelegatedAddress == "" && defaultString(spec.Mode, "stateless") != "stateless" {
			if spec.AddressPool.Start == "" || spec.AddressPool.End == "" {
				return true, fmt.Errorf("%s spec.addressPool.start and spec.addressPool.end are required for stateful modes", res.ID())
			}
			if err := validateIPv6AddressPair(spec.AddressPool.Start, spec.AddressPool.End); err != nil {
				return true, fmt.Errorf("%s spec.addressPool: %w", res.ID(), err)
			}
		}
		switch defaultString(spec.DNSSource, "self") {
		case "self", "none":
		case "static":
			if len(spec.DNSServers) == 0 {
				return true, fmt.Errorf("%s spec.dnsServers is required when dnsSource is static", res.ID())
			}
		default:
			return true, fmt.Errorf("%s spec.dnsSource must be self, static, or none", res.ID())
		}
		for i, server := range append(append([]string{}, spec.DNSServers...), spec.SNTPServers...) {
			if strings.ContainsAny(server, "\n\r") {
				return true, fmt.Errorf("%s DNS/SNTP server entry %d contains newline", res.ID(), i)
			}
			if strings.HasPrefix(strings.TrimSpace(server), "${") {
				return true, fmt.Errorf("%s DNS/SNTP server status expressions were removed; use dnsServerFrom or sntpServerFrom", res.ID())
			}
			addr, err := netip.ParseAddr(server)
			if err != nil || !addr.Is6() {
				return true, fmt.Errorf("%s DNS/SNTP server entry %q must be IPv6 or a status reference", res.ID(), server)
			}
		}
		if len(spec.DomainSearch) > 0 && len(spec.DomainSearchFrom) > 0 {
			return true, fmt.Errorf("%s spec.domainSearch and spec.domainSearchFrom cannot both be set", res.ID())
		}
		for i, domain := range spec.DomainSearch {
			if err := validateDomainValue(domain); err != nil {
				return true, fmt.Errorf("%s spec.domainSearch[%d]: %w", res.ID(), i, err)
			}
		}
		for i, source := range spec.DomainSearchFrom {
			if err := validateDNSZoneDomainSource(source); err != nil {
				return true, fmt.Errorf("%s spec.domainSearchFrom[%d]: %w", res.ID(), i, err)
			}
		}
	case "DHCPv4Server":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4ServerSpec()
		if err != nil {
			return true, err
		}
		switch defaultString(spec.Role, "server") {
		case "server", "transit":
		default:
			return true, fmt.Errorf("%s spec.role must be server or transit", res.ID())
		}
		switch spec.Server {
		case "", "dnsmasq", "kea", "dhcpd":
		default:
			return true, fmt.Errorf("%s spec.server must be dnsmasq, kea, or dhcpd", res.ID())
		}
		switch spec.DNS.UpstreamSource {
		case "", "dhcpv4", "static", "system", "none":
		default:
			return true, fmt.Errorf("%s spec.dns.upstreamSource must be dhcpv4, static, system, or none", res.ID())
		}
		if spec.DNS.UpstreamSource == "dhcpv4" && spec.DNS.UpstreamInterface == "" {
			return true, fmt.Errorf("%s spec.dns.upstreamInterface is required when dns.upstreamSource is dhcpv4", res.ID())
		}
		if spec.DNS.UpstreamSource == "static" && len(spec.DNS.UpstreamServers) == 0 {
			return true, fmt.Errorf("%s spec.dns.upstreamServers is required when dns.upstreamSource is static", res.ID())
		}
		for _, dns := range spec.DNS.UpstreamServers {
			addr, err := netip.ParseAddr(dns)
			if err != nil {
				return true, fmt.Errorf("%s spec.dns.upstreamServers contains invalid address %q", res.ID(), dns)
			}
			if !addr.Is4() && !addr.Is6() {
				return true, fmt.Errorf("%s spec.dns.upstreamServers contains invalid address %q", res.ID(), dns)
			}
		}
		for i, name := range spec.ListenInterfaces {
			if name == "" {
				return true, fmt.Errorf("%s spec.listenInterfaces[%d] must not be empty", res.ID(), i)
			}
		}
		if spec.Interface != "" {
			poolStart := defaultString(spec.AddressPool.Start, spec.RangeStart)
			poolEnd := defaultString(spec.AddressPool.End, spec.RangeEnd)
			if poolStart == "" || poolEnd == "" {
				return true, fmt.Errorf("%s spec.addressPool.start and spec.addressPool.end are required when spec.interface is set", res.ID())
			}
			if err := validateIPv4AddressPair(poolStart, poolEnd); err != nil {
				return true, fmt.Errorf("%s spec.addressPool: %w", res.ID(), err)
			}
			routerSource := defaultString(spec.RouterSource, "interfaceAddress")
			switch routerSource {
			case "interfaceAddress", "none":
			case "static":
				if spec.Router == "" {
					return true, fmt.Errorf("%s spec.router is required when routerSource is static", res.ID())
				}
			default:
				return true, fmt.Errorf("%s spec.routerSource must be interfaceAddress, static, or none", res.ID())
			}
			if spec.Router != "" {
				router, err := netip.ParseAddr(spec.Router)
				if err != nil || !router.Is4() {
					return true, fmt.Errorf("%s spec.router must be an IPv4 address", res.ID())
				}
			}
			dnsSource := defaultString(spec.DNSSource, "self")
			switch dnsSource {
			case "dhcpv4":
				if spec.DNSInterface == "" {
					return true, fmt.Errorf("%s spec.dnsInterface is required when dnsSource is dhcpv4", res.ID())
				}
			case "static":
				if len(spec.DNSServers) == 0 {
					return true, fmt.Errorf("%s spec.dnsServers is required when dnsSource is static", res.ID())
				}
			case "self", "none":
			default:
				return true, fmt.Errorf("%s spec.dnsSource must be dhcpv4, static, self, or none", res.ID())
			}
			if spec.Gateway != "" {
				addr, err := netip.ParseAddr(spec.Gateway)
				if err != nil || !addr.Is4() {
					return true, fmt.Errorf("%s spec.gateway must be an IPv4 address", res.ID())
				}
			}
			if spec.GatewayFrom.Resource != "" && spec.GatewayFrom.Field == "" {
				return true, fmt.Errorf("%s spec.gatewayFrom.field is required", res.ID())
			}
			for _, server := range append(append([]string{}, spec.DNSServers...), spec.NTPServers...) {
				addr, err := netip.ParseAddr(server)
				if err != nil || !addr.Is4() {
					return true, fmt.Errorf("%s dnsServers/ntpServers entries must be IPv4 addresses", res.ID())
				}
			}
			for i, source := range spec.DNSServerFrom {
				if source.Resource == "" || source.Field == "" {
					return true, fmt.Errorf("%s spec.dnsServerFrom[%d].resource and field are required", res.ID(), i)
				}
			}
			for i, source := range spec.NTPServerFrom {
				if source.Resource == "" || source.Field == "" {
					return true, fmt.Errorf("%s spec.ntpServerFrom[%d].resource and field are required", res.ID(), i)
				}
			}
			if spec.Domain != "" && spec.DomainFrom.Resource != "" {
				return true, fmt.Errorf("%s spec.domain and spec.domainFrom cannot both be set", res.ID())
			}
			if spec.Domain != "" {
				if err := validateDomainValue(spec.Domain); err != nil {
					return true, fmt.Errorf("%s spec.domain: %w", res.ID(), err)
				}
			}
			if spec.DomainFrom.Resource != "" || spec.DomainFrom.Field != "" {
				if err := validateDNSZoneDomainSource(spec.DomainFrom); err != nil {
					return true, fmt.Errorf("%s spec.domainFrom: %w", res.ID(), err)
				}
			}
			for i, option := range spec.Options {
				if err := validateDHCPv4Option(option); err != nil {
					return true, fmt.Errorf("%s spec.options[%d]: %w", res.ID(), i, err)
				}
			}
		}
	case "DHCPv4Reservation":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4ReservationSpec()
		if err != nil {
			return true, err
		}
		if spec.Scope != "" {
			return true, fmt.Errorf("%s spec.scope is not supported; use spec.server to reference DHCPv4Server", res.ID())
		}
		if spec.Server == "" {
			return true, fmt.Errorf("%s spec.server is required", res.ID())
		}
		if spec.MACAddress == "" {
			return true, fmt.Errorf("%s spec.macAddress is required", res.ID())
		}
		if _, err := net.ParseMAC(spec.MACAddress); err != nil {
			return true, fmt.Errorf("%s spec.macAddress must be a MAC address", res.ID())
		}
		if spec.IPAddress == "" {
			return true, fmt.Errorf("%s spec.ipAddress is required", res.ID())
		}
		if addr, err := netip.ParseAddr(spec.IPAddress); err != nil || !addr.Is4() {
			return true, fmt.Errorf("%s spec.ipAddress must be an IPv4 address", res.ID())
		}
		if spec.Hostname != "" && strings.ContainsAny(spec.Hostname, " \t\n,") {
			return true, fmt.Errorf("%s spec.hostname must not contain whitespace or commas", res.ID())
		}
		if strings.Contains(spec.LeaseTime, ",") {
			return true, fmt.Errorf("%s spec.leaseTime must not contain commas", res.ID())
		}
		for i, option := range spec.Options {
			if err := validateDHCPv4Option(option); err != nil {
				return true, fmt.Errorf("%s spec.options[%d]: %w", res.ID(), i, err)
			}
		}
	case "DHCPv4Relay":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4RelaySpec()
		if err != nil {
			return true, err
		}
		if len(spec.Interfaces) == 0 {
			return true, fmt.Errorf("%s spec.interfaces is required", res.ID())
		}
		if spec.Upstream == "" {
			return true, fmt.Errorf("%s spec.upstream is required", res.ID())
		}
		if addr, err := netip.ParseAddr(spec.Upstream); err != nil || !addr.Is4() {
			return true, fmt.Errorf("%s spec.upstream must be an IPv4 address", res.ID())
		}
	case "SelfAddressPolicy":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.SelfAddressPolicySpec()
		if err != nil {
			return true, err
		}
		switch spec.AddressFamily {
		case "ipv4", "ipv6":
		default:
			return true, fmt.Errorf("%s spec.addressFamily must be ipv4 or ipv6", res.ID())
		}
		if len(spec.Candidates) == 0 {
			return true, fmt.Errorf("%s spec.candidates is required", res.ID())
		}
		for i, candidate := range spec.Candidates {
			switch candidate.Source {
			case "delegatedAddress":
				if spec.AddressFamily != "ipv6" {
					return true, fmt.Errorf("%s spec.candidates[%d].source delegatedAddress is only valid for ipv6", res.ID(), i)
				}
				if candidate.DelegatedAddress == "" {
					return true, fmt.Errorf("%s spec.candidates[%d].delegatedAddress is required", res.ID(), i)
				}
				if candidate.Address != "" || candidate.Interface != "" {
					return true, fmt.Errorf("%s spec.candidates[%d] delegatedAddress candidate cannot set address or interface", res.ID(), i)
				}
				if candidate.AddressSuffix != "" {
					addr, err := netip.ParseAddr(candidate.AddressSuffix)
					if err != nil || !addr.Is6() {
						return true, fmt.Errorf("%s spec.candidates[%d].addressSuffix must be an IPv6 suffix", res.ID(), i)
					}
				}
			case "interfaceAddress":
				if candidate.Interface == "" {
					return true, fmt.Errorf("%s spec.candidates[%d].interface is required", res.ID(), i)
				}
				if candidate.MatchSuffix != "" {
					addr, err := netip.ParseAddr(candidate.MatchSuffix)
					if err != nil || !addr.Is6() {
						return true, fmt.Errorf("%s spec.candidates[%d].matchSuffix must be an IPv6 suffix", res.ID(), i)
					}
				}
			case "static":
				if candidate.Address == "" {
					return true, fmt.Errorf("%s spec.candidates[%d].address is required", res.ID(), i)
				}
				addr, err := netip.ParseAddr(candidate.Address)
				if err != nil || (spec.AddressFamily == "ipv4" && !addr.Is4()) || (spec.AddressFamily == "ipv6" && !addr.Is6()) {
					return true, fmt.Errorf("%s spec.candidates[%d].address must match addressFamily", res.ID(), i)
				}
			default:
				return true, fmt.Errorf("%s spec.candidates[%d].source must be delegatedAddress, interfaceAddress, or static", res.ID(), i)
			}
			if candidate.Ordinal < 0 {
				return true, fmt.Errorf("%s spec.candidates[%d].ordinal must be greater than 0", res.ID(), i)
			}
		}
	default:
		return false, nil
	}
	return true, nil
}
