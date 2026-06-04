// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func validateWANResource(res api.Resource, targetOS platform.OS) (bool, error) {
	switch res.Kind {
	case "PPPoESession":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.PPPoESessionSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.Username == "" {
			return true, fmt.Errorf("%s spec.username is required", res.ID())
		}
		if spec.Password == "" && spec.PasswordFile == "" {
			return true, fmt.Errorf("%s spec.password or spec.passwordFile is required", res.ID())
		}
		if spec.Password != "" && spec.PasswordFile != "" {
			return true, fmt.Errorf("%s spec.password and spec.passwordFile are mutually exclusive", res.ID())
		}
		if spec.IfName != "" && strings.ContainsAny(spec.IfName, " \t\n/") {
			return true, fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if spec.IfName != "" && len(spec.IfName) > 15 {
			return true, fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if spec.IfName == "" && len("ppp-"+res.Metadata.Name) > 15 {
			return true, fmt.Errorf("%s spec.ifname is required when default PPP interface name exceeds 15 characters", res.ID())
		}
		if spec.ServiceName != "" && strings.ContainsAny(spec.ServiceName, "\n\r") {
			return true, fmt.Errorf("%s spec.serviceName contains invalid newline", res.ID())
		}
		if spec.ACName != "" && strings.ContainsAny(spec.ACName, "\n\r") {
			return true, fmt.Errorf("%s spec.acName contains invalid newline", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 1500) {
			return true, fmt.Errorf("%s spec.mtu must be within 576-1500", res.ID())
		}
		if spec.MRU != 0 && (spec.MRU < 576 || spec.MRU > 1500) {
			return true, fmt.Errorf("%s spec.mru must be within 576-1500", res.ID())
		}
		if spec.LCPInterval < 0 || spec.LCPFailure < 0 {
			return true, fmt.Errorf("%s spec.lcpInterval and spec.lcpFailure must be non-negative", res.ID())
		}
		switch spec.AuthMethod {
		case "", "chap", "pap", "both":
		default:
			return true, fmt.Errorf("%s spec.authMethod must be chap, pap, or both", res.ID())
		}
		if spec.LCPEchoInterval < 0 || spec.LCPEchoFailure < 0 {
			return true, fmt.Errorf("%s spec.lcpEchoInterval and spec.lcpEchoFailure must be non-negative", res.ID())
		}
		switch spec.SecretEncoding {
		case "", "plain":
		default:
			return true, fmt.Errorf("%s spec.secretEncoding must be plain", res.ID())
		}
	case "DHCPv6Address", "IPv6RAAddress":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
	case "DHCPv4Client":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4ClientSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.RequestedAddress != "" {
			addr, err := netip.ParseAddr(spec.RequestedAddress)
			if err != nil || !addr.Is4() {
				return true, fmt.Errorf("%s spec.requestedAddress must be an IPv4 address", res.ID())
			}
		}
		if strings.ContainsAny(spec.Hostname, " \t\n\r") {
			return true, fmt.Errorf("%s spec.hostname must not contain whitespace", res.ID())
		}
	case "IPv4StaticRoute":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4StaticRouteSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		destination, err := netip.ParsePrefix(spec.Destination)
		if err != nil || !destination.Addr().Is4() {
			return true, fmt.Errorf("%s spec.destination must be an IPv4 CIDR", res.ID())
		}
		via, err := netip.ParseAddr(spec.Via)
		if err != nil || !via.Is4() {
			return true, fmt.Errorf("%s spec.via must be an IPv4 address", res.ID())
		}
	case "IPv6StaticRoute":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6StaticRouteSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		destination, err := netip.ParsePrefix(spec.Destination)
		if err != nil || !destination.Addr().Is6() {
			return true, fmt.Errorf("%s spec.destination must be an IPv6 CIDR", res.ID())
		}
		via, err := netip.ParseAddr(spec.Via)
		if err != nil || !via.Is6() {
			return true, fmt.Errorf("%s spec.via must be an IPv6 address", res.ID())
		}
	case "DHCPv6PrefixDelegation":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv6PrefixDelegationSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		switch spec.Profile {
		case "", api.IPv6PDProfileDefault, api.IPv6PDProfileNTTNGNDirectHikariDenwa, api.IPv6PDProfileNTTHGWLANPD:
		default:
			return true, fmt.Errorf("%s spec.profile must be default, ntt-ngn-direct-hikari-denwa, or ntt-hgw-lan-pd", res.ID())
		}
		if spec.PrefixLength != 0 && (spec.PrefixLength < 1 || spec.PrefixLength > 128) {
			return true, fmt.Errorf("%s spec.prefixLength must be within 1-128", res.ID())
		}
		if spec.IAID != "" && !validIAID(spec.IAID) {
			return true, fmt.Errorf("%s spec.iaid must be a decimal, 0x-prefixed hex, or 8-digit hex uint32", res.ID())
		}
		if spec.ClientDUID != "" {
			if strings.ContainsAny(spec.ClientDUID, " \t\n\r:") {
				return true, fmt.Errorf("%s spec.clientDUID must be plain hex without separators", res.ID())
			}
			if _, err := hex.DecodeString(spec.ClientDUID); err != nil {
				return true, fmt.Errorf("%s spec.clientDUID must be valid hex: %w", res.ID(), err)
			}
		}
		if spec.DUIDType != "" {
			return true, fmt.Errorf("%s spec.duidType is not supported; use spec.profile or spec.clientDUID", res.ID())
		}
	case "IPv6DelegatedAddress":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err != nil {
			return true, err
		}
		if spec.PrefixDelegation == "" {
			return true, fmt.Errorf("%s spec.prefixDelegation is required", res.ID())
		}
		if spec.PrefixSource != "" {
			return true, fmt.Errorf("%s spec.prefixSource was removed; use spec.prefixDelegation and spec.dependsOn", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return true, fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.AddressSuffix == "" {
			return true, fmt.Errorf("%s spec.addressSuffix is required", res.ID())
		}
		addr, err := netip.ParseAddr(spec.AddressSuffix)
		if err != nil || !addr.Is6() {
			return true, fmt.Errorf("%s spec.addressSuffix must be an IPv6 address suffix such as ::3", res.ID())
		}
		if spec.SubnetID != "" {
			if strings.HasPrefix(spec.SubnetID, "-") || strings.ContainsAny(spec.SubnetID, " \t\n/") {
				return true, fmt.Errorf("%s spec.subnetID must be a non-negative subnet id", res.ID())
			}
		}
	case "DHCPv6Information":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv6InformationSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return true, fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
	case "DSLiteTunnel":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			return true, err
		}
		if spec.Interface == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.AFTRSource != "" {
			return true, fmt.Errorf("%s spec.aftrSource was removed; use spec.aftrFrom", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return true, fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		if spec.AFTRFrom.Resource == "" && spec.AFTRFQDN == "" && spec.AFTRIPv6 == "" && spec.RemoteAddress == "" {
			return true, fmt.Errorf("%s spec.aftrFrom, spec.aftrFQDN, spec.aftrIPv6, or spec.remoteAddress is required", res.ID())
		}
		if spec.AFTRFQDN != "" && strings.ContainsAny(spec.AFTRFQDN, " \t\n/") {
			return true, fmt.Errorf("%s spec.aftrFQDN contains invalid whitespace or slash", res.ID())
		}
		if spec.AFTRIPv6 != "" {
			addr, err := netip.ParseAddr(spec.AFTRIPv6)
			if err != nil || !addr.Is6() {
				return true, fmt.Errorf("%s spec.aftrIPv6 must be an IPv6 address", res.ID())
			}
		}
		if spec.AFTRAddressOrdinal < 0 {
			return true, fmt.Errorf("%s spec.aftrAddressOrdinal must be greater than 0", res.ID())
		}
		switch defaultString(spec.AFTRAddressSelection, "ordinal") {
		case "ordinal", "ordinalModulo":
		default:
			return true, fmt.Errorf("%s spec.aftrAddressSelection must be ordinal or ordinalModulo", res.ID())
		}
		if spec.RemoteAddress != "" {
			addr, err := netip.ParseAddr(spec.RemoteAddress)
			if err != nil || !addr.Is6() {
				return true, fmt.Errorf("%s spec.remoteAddress must be an IPv6 address", res.ID())
			}
		}
		if spec.LocalAddress != "" {
			addr, err := netip.ParseAddr(spec.LocalAddress)
			if err != nil || !addr.Is6() {
				return true, fmt.Errorf("%s spec.localAddress must be an IPv6 address", res.ID())
			}
		}
		if spec.LocalAddressFrom.Resource != "" && spec.LocalAddressFrom.Field == "" {
			return true, fmt.Errorf("%s spec.localAddressFrom.field is required", res.ID())
		}
		if spec.LocalAddressFrom.Resource != "" {
			if err := validateSourceResourceRef(spec.LocalAddressFrom.Resource); err != nil {
				return true, fmt.Errorf("%s spec.localAddressFrom.resource: %w", res.ID(), err)
			}
		}
		localAddressSource := defaultString(spec.LocalAddressSource, "interface")
		switch localAddressSource {
		case "interface":
		case "static":
			if spec.LocalAddress == "" {
				return true, fmt.Errorf("%s spec.localAddress is required when localAddressSource is static", res.ID())
			}
		case "delegatedAddress":
			if spec.LocalDelegatedAddress == "" {
				return true, fmt.Errorf("%s spec.localDelegatedAddress is required when localAddressSource is delegatedAddress", res.ID())
			}
			if spec.LocalAddressSuffix != "" {
				addr, err := netip.ParseAddr(spec.LocalAddressSuffix)
				if err != nil || !addr.Is6() {
					return true, fmt.Errorf("%s spec.localAddressSuffix must be an IPv6 suffix such as ::100", res.ID())
				}
			}
		default:
			return true, fmt.Errorf("%s spec.localAddressSource must be interface, static, or delegatedAddress", res.ID())
		}
		for _, server := range spec.AFTRDNSServers {
			addr, err := netip.ParseAddr(server)
			if err != nil || (!addr.Is4() && !addr.Is6()) {
				return true, fmt.Errorf("%s spec.aftrDNSServers contains invalid address %q", res.ID(), server)
			}
		}
		if spec.MTU != 0 && (spec.MTU < 1280 || spec.MTU > 65535) {
			return true, fmt.Errorf("%s spec.mtu must be within 1280-65535", res.ID())
		}
	case "HealthCheck":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.HealthCheckSpec()
		if err != nil {
			return true, err
		}
		if spec.Daemon != "" || spec.FwMark != 0 || spec.SourceInterface != "" || spec.SourceAddress != "" || spec.SourceAddressFrom.Resource != "" || spec.Via != "" {
			return true, fmt.Errorf("%s health-check daemon, source binding, via, and fwmark fields are not supported; routerd derives them from referenced route/interface resources", res.ID())
		}
		switch defaultString(spec.Type, "ping") {
		case "ping":
		default:
			return true, fmt.Errorf("%s spec.type must be ping", res.ID())
		}
		switch spec.Protocol {
		case "", "icmp", "tcp", "dns", "http":
		default:
			return true, fmt.Errorf("%s spec.protocol must be icmp, tcp, dns, or http", res.ID())
		}
		if spec.Port < 0 || spec.Port > 65535 {
			return true, fmt.Errorf("%s spec.port must be within 0-65535", res.ID())
		}
		switch defaultString(spec.Role, "next-hop") {
		case "link", "next-hop", "internet", "service", "policy":
		default:
			return true, fmt.Errorf("%s spec.role must be link, next-hop, internet, service, or policy", res.ID())
		}
		addressFamily := defaultString(spec.AddressFamily, "ipv4")
		switch addressFamily {
		case "ipv4", "ipv6":
		default:
			return true, fmt.Errorf("%s spec.addressFamily must be ipv4 or ipv6", res.ID())
		}
		switch defaultString(spec.TargetSource, "auto") {
		case "auto", "static", "defaultGateway", "dsliteRemote":
		default:
			return true, fmt.Errorf("%s spec.targetSource must be auto, static, defaultGateway, or dsliteRemote", res.ID())
		}
		if spec.Target != "" {
			addr, err := netip.ParseAddr(spec.Target)
			if err != nil {
				return true, fmt.Errorf("%s spec.target must be an IP address", res.ID())
			}
			if addressFamily == "ipv4" && !addr.Is4() {
				return true, fmt.Errorf("%s spec.target must be an IPv4 address", res.ID())
			}
			if addressFamily == "ipv6" && !addr.Is6() {
				return true, fmt.Errorf("%s spec.target must be an IPv6 address", res.ID())
			}
		}
		if defaultString(spec.TargetSource, "auto") == "static" && spec.Target == "" {
			return true, fmt.Errorf("%s spec.target is required when targetSource is static", res.ID())
		}
		interval := defaultString(spec.Interval, "30s")
		if d, err := time.ParseDuration(interval); err != nil || d <= 0 {
			return true, fmt.Errorf("%s spec.interval must be a positive duration", res.ID())
		}
		timeout := defaultString(spec.Timeout, "3s")
		if d, err := time.ParseDuration(timeout); err != nil || d <= 0 {
			return true, fmt.Errorf("%s spec.timeout must be a positive duration", res.ID())
		}
		if spec.HealthyThreshold < 0 || spec.UnhealthyThreshold < 0 {
			return true, fmt.Errorf("%s spec.healthyThreshold and spec.unhealthyThreshold must be non-negative", res.ID())
		}
	default:
		return false, nil
	}
	return true, nil
}
