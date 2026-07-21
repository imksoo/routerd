// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net"
	"net/netip"
	"path"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func validateFirewallResource(res api.Resource, targetOS platform.OS) (bool, error) {
	switch res.Kind {
	case "ManagementAccess":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.ManagementAccessSpec()
		if err != nil {
			return true, err
		}
		if len(spec.Interfaces) == 0 {
			return true, fmt.Errorf("%s spec.interfaces is required", res.ID())
		}
		seen := map[string]bool{}
		for i, ref := range spec.Interfaces {
			kind, name := splitFirewallInterfaceRef(ref)
			if kind != "Interface" || !validManagementInterfaceName(name) {
				return true, fmt.Errorf("%s spec.interfaces[%d] must reference an Interface name or Interface/<name>", res.ID(), i)
			}
			if seen[name] {
				return true, fmt.Errorf("%s spec.interfaces[%d] duplicates %q", res.ID(), i, ref)
			}
			seen[name] = true
		}
		for i, cidr := range spec.AllowSourceCIDRs {
			if _, err := netip.ParsePrefix(strings.TrimSpace(cidr)); err != nil {
				return true, fmt.Errorf("%s spec.allowSourceCIDRs[%d] is invalid: %w", res.ID(), i, err)
			}
		}
	case "TrafficFlowLog":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.TrafficFlowLogSpec()
		if err != nil {
			return true, err
		}
		if spec.Enabled && strings.TrimSpace(spec.Path) == "" {
			return true, fmt.Errorf("%s spec.path is required when enabled is true", res.ID())
		}
		switch spec.Source {
		case "", "conntrack":
		default:
			return true, fmt.Errorf("%s spec.source must be conntrack", res.ID())
		}
	case "NAT44Rule":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.NAT44RuleSpec()
		if err != nil {
			return true, err
		}
		if spec.OutboundInterface != "" || len(spec.SourceCIDRs) > 0 || spec.Translation.Type != "" {
			if err := validateNAT44SourceNATShape(res.ID(), spec); err != nil {
				return true, err
			}
			break
		}
		switch spec.Type {
		case "masquerade", "snat":
		default:
			return true, fmt.Errorf("%s spec.type must be masquerade or snat", res.ID())
		}
		if spec.EgressInterface == "" && spec.EgressPolicyRef == "" {
			return true, fmt.Errorf("%s spec.egressInterface or spec.egressPolicyRef is required", res.ID())
		}
		if len(spec.SourceRanges) == 0 {
			return true, fmt.Errorf("%s spec.sourceRanges is required", res.ID())
		}
		for _, cidr := range spec.SourceRanges {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return true, fmt.Errorf("%s spec.sourceRanges entries must be IPv4 prefixes", res.ID())
			}
		}
		for _, cidr := range spec.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return true, fmt.Errorf("%s spec.destinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs); err != nil {
			return true, err
		}
		for _, cidr := range spec.ExcludeDestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return true, fmt.Errorf("%s spec.excludeDestinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs); err != nil {
			return true, err
		}
		if spec.Type == "snat" {
			if spec.SNATAddress == "" && spec.SNATAddressFrom.Resource == "" {
				return true, fmt.Errorf("%s spec.snatAddress or spec.snatAddressFrom is required when type is snat", res.ID())
			}
			if spec.SNATAddress != "" && spec.SNATAddressFrom.Resource != "" {
				return true, fmt.Errorf("%s spec.snatAddress and spec.snatAddressFrom are mutually exclusive", res.ID())
			}
			if spec.SNATAddressFrom.Resource != "" && spec.SNATAddressFrom.Field == "" {
				return true, fmt.Errorf("%s spec.snatAddressFrom.field is required", res.ID())
			}
			addr, err := netip.ParseAddr(spec.SNATAddress)
			if spec.SNATAddress != "" && (err != nil || !addr.Is4()) {
				return true, fmt.Errorf("%s spec.snatAddress must be an IPv4 address when type is snat", res.ID())
			}
		}
		if spec.Type == "masquerade" && (spec.SNATAddress != "" || spec.SNATAddressFrom.Resource != "") {
			return true, fmt.Errorf("%s spec.snatAddress and spec.snatAddressFrom are only valid when type is snat", res.ID())
		}
	case "NAT44FlowDNATPinhole":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.NAT44FlowDNATPinholeSpec()
		if err != nil {
			return true, err
		}
		if len(spec.FromInterfaceRefs) == 0 {
			return true, fmt.Errorf("%s spec.fromInterfaceRefs is required", res.ID())
		}
		if spec.Outbound.Protocol != "udp" {
			return true, fmt.Errorf("%s spec.outbound.protocol must be udp", res.ID())
		}
		switch spec.Correlation.Key {
		case "", "localPort":
		default:
			return true, fmt.Errorf("%s spec.correlation.key must be localPort", res.ID())
		}
		if spec.Outbound.SourceSetRef == "" && len(spec.Outbound.SourceCIDRs) == 0 {
			return true, fmt.Errorf("%s spec.outbound.sourceSetRef or spec.outbound.sourceCIDRs is required", res.ID())
		}
		if spec.Outbound.SourceSetRef != "" {
			kind, name := splitResourceRef(spec.Outbound.SourceSetRef)
			if kind != "IPAddressSet" || strings.TrimSpace(name) == "" {
				return true, fmt.Errorf("%s spec.outbound.sourceSetRef must reference IPAddressSet", res.ID())
			}
		}
		for i, cidr := range spec.Outbound.SourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return true, fmt.Errorf("%s spec.outbound.sourceCIDRs[%d] must be an IPv4 prefix", res.ID(), i)
			}
		}
		for i, cidr := range spec.Outbound.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return true, fmt.Errorf("%s spec.outbound.destinationCIDRs[%d] must be an IPv4 prefix", res.ID(), i)
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.outbound.destinationSetRefs", spec.Outbound.DestinationSetRefs); err != nil {
			return true, err
		}
		if len(spec.Outbound.DestinationPorts) == 0 {
			return true, fmt.Errorf("%s spec.outbound.destinationPorts is required", res.ID())
		}
		if err := validateFirewallPortList(res.ID(), "spec.outbound.destinationPorts", spec.Outbound.DestinationPorts); err != nil {
			return true, err
		}
		if len(spec.Inbound.SourcePorts) == 0 {
			return true, fmt.Errorf("%s spec.inbound.sourcePorts is required", res.ID())
		}
		if err := validateFirewallPortList(res.ID(), "spec.inbound.sourcePorts", spec.Inbound.SourcePorts); err != nil {
			return true, err
		}
		timeout := strings.TrimSpace(spec.Timeout)
		if timeout != "" {
			d, err := time.ParseDuration(timeout)
			if err != nil {
				return true, fmt.Errorf("%s spec.timeout must be a duration: %w", res.ID(), err)
			}
			if d <= 0 {
				return true, fmt.Errorf("%s spec.timeout must be greater than zero", res.ID())
			}
		}
	case "NAT44SessionSync":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.NAT44SessionSyncSpec()
		if err != nil {
			return true, err
		}
		if spec.Mode != "" && spec.Mode != "event-stream" {
			return true, fmt.Errorf("%s spec.mode must be event-stream", res.ID())
		}
		if strings.TrimSpace(spec.Interval) != "" {
			return true, fmt.Errorf("%s spec.interval is not supported; NAT44SessionSync always uses event-stream", res.ID())
		}
		if strings.ContainsAny(spec.ConntrackCommand, "\n\r") {
			return true, fmt.Errorf("%s spec.conntrackCommand must not contain newline", res.ID())
		}
		if len(spec.SNATAddresses) == 0 && len(spec.NATRules) == 0 {
			return true, fmt.Errorf("%s spec.snatAddresses or spec.natRules is required", res.ID())
		}
		for i, address := range spec.SNATAddresses {
			addr, err := netip.ParseAddr(strings.TrimSpace(address))
			if err != nil || !addr.Is4() {
				return true, fmt.Errorf("%s spec.snatAddresses[%d] must be an IPv4 address", res.ID(), i)
			}
		}
		for i, ref := range spec.NATRules {
			if err := validateNAT44SessionSyncNATRuleRef(ref); err != nil {
				return true, fmt.Errorf("%s spec.natRules[%d]: %w", res.ID(), i, err)
			}
		}
		for i, ref := range spec.ExcludeNATRules {
			if err := validateNAT44SessionSyncNATRuleRef(ref); err != nil {
				return true, fmt.Errorf("%s spec.excludeNatRules[%d]: %w", res.ID(), i, err)
			}
		}
		if len(spec.Targets) == 0 {
			return true, fmt.Errorf("%s spec.targets is required", res.ID())
		}
		for i, target := range spec.Targets {
			if strings.TrimSpace(target.Host) == "" {
				return true, fmt.Errorf("%s spec.targets[%d].host is required", res.ID(), i)
			}
			if strings.ContainsAny(target.Host, "\n\r") {
				return true, fmt.Errorf("%s spec.targets[%d].host must not contain newline", res.ID(), i)
			}
			if strings.ContainsAny(target.User, "\n\r") {
				return true, fmt.Errorf("%s spec.targets[%d].user must not contain newline", res.ID(), i)
			}
			for j, opt := range target.SSHOptions {
				if strings.ContainsAny(opt, "\n\r") {
					return true, fmt.Errorf("%s spec.targets[%d].sshOptions[%d] must not contain newline", res.ID(), i, j)
				}
			}
			for j, part := range target.RestoreCommand {
				if strings.TrimSpace(part) == "" {
					return true, fmt.Errorf("%s spec.targets[%d].restoreCommand[%d] must not be empty", res.ID(), i, j)
				}
				if strings.ContainsAny(part, "\n\r") {
					return true, fmt.Errorf("%s spec.targets[%d].restoreCommand[%d] must not contain newline", res.ID(), i, j)
				}
			}
		}
	case "PortForward":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.PortForwardSpec()
		if err != nil {
			return true, err
		}
		if err := validateIngressListen(res.ID(), "spec.listen", spec.Listen); err != nil {
			return true, err
		}
		if err := validateIngressTarget(res.ID(), "spec.target", spec.Target.Address, spec.Target.AddressFrom, spec.Target.Port, false); err != nil {
			return true, err
		}
		if err := validateIngressHairpin(res.ID(), "spec.hairpin", spec.Listen, spec.Hairpin); err != nil {
			return true, err
		}
	case "IngressService":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.IngressServiceSpec()
		if err != nil {
			return true, err
		}
		if err := validateIngressListen(res.ID(), "spec.listen", spec.Listen); err != nil {
			return true, err
		}
		if err := validateIngressHairpin(res.ID(), "spec.hairpin", spec.Listen, spec.Hairpin); err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.Hostname) != "" {
			if err := validateFQDN(spec.Hostname); err != nil {
				return true, fmt.Errorf("%s spec.hostname is invalid: %w", res.ID(), err)
			}
		}
		if len(spec.Backends) == 0 {
			return true, fmt.Errorf("%s spec.backends is required", res.ID())
		}
		if spec.HealthCheck.Protocol != "" {
			switch spec.HealthCheck.Protocol {
			case "tcp", "http", "https":
			default:
				return true, fmt.Errorf("%s spec.healthCheck.protocol must be tcp, http, or https", res.ID())
			}
		}
		for field, value := range map[string]string{"interval": spec.HealthCheck.Interval, "timeout": spec.HealthCheck.Timeout} {
			if value == "" {
				continue
			}
			if _, err := time.ParseDuration(value); err != nil {
				return true, fmt.Errorf("%s spec.healthCheck.%s is invalid: %w", res.ID(), field, err)
			}
		}
		if spec.HealthCheck.Path != "" && !strings.HasPrefix(spec.HealthCheck.Path, "/") {
			return true, fmt.Errorf("%s spec.healthCheck.path must be an absolute HTTP path", res.ID())
		}
		if strings.ContainsAny(spec.HealthCheck.Host, " \t\x00\n\r") {
			return true, fmt.Errorf("%s spec.healthCheck.host contains invalid characters", res.ID())
		}
		for i, code := range spec.HealthCheck.ExpectedStatus {
			if code < 100 || code > 599 {
				return true, fmt.Errorf("%s spec.healthCheck.expectedStatus[%d] must be within 100-599", res.ID(), i)
			}
		}
		if spec.HealthCheck.HealthyThreshold < 0 {
			return true, fmt.Errorf("%s spec.healthCheck.healthyThreshold must be non-negative and at least 1 when set", res.ID())
		}
		if spec.HealthCheck.UnhealthyThreshold < 0 {
			return true, fmt.Errorf("%s spec.healthCheck.unhealthyThreshold must be non-negative and at least 1 when set", res.ID())
		}
		if spec.Policy.Selection != "" {
			switch spec.Policy.Selection {
			case "failover", "sourceHash", "random":
			default:
				return true, fmt.Errorf("%s spec.policy.selection must be failover, sourceHash, or random", res.ID())
			}
		}
		if spec.Policy.OnNoHealthyBackends != "" {
			switch spec.Policy.OnNoHealthyBackends {
			case "drop", "reject":
			default:
				return true, fmt.Errorf("%s spec.policy.onNoHealthyBackends must be drop or reject", res.ID())
			}
		}
		for i, backend := range spec.Backends {
			if err := validateIngressTarget(res.ID(), fmt.Sprintf("spec.backends[%d]", i), backend.Address, backend.AddressFrom, backend.Port, true); err != nil {
				return true, err
			}
			if backend.Weight < 0 {
				return true, fmt.Errorf("%s spec.backends[%d].weight must be non-negative", res.ID(), i)
			}
		}
	case "IPAddressSet":
		if res.APIVersion != api.NetAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPAddressSetSpec()
		if err != nil {
			return true, err
		}
		if len(spec.Addresses) == 0 && len(spec.Names) == 0 {
			return true, fmt.Errorf("%s spec.addresses or spec.names is required", res.ID())
		}
		seenAddresses := map[string]bool{}
		for i, value := range spec.Addresses {
			addr, err := netip.ParseAddr(value)
			if err != nil {
				return true, fmt.Errorf("%s spec.addresses[%d] must be an IP address", res.ID(), i)
			}
			addr = addr.Unmap()
			if seenAddresses[addr.String()] {
				return true, fmt.Errorf("%s spec.addresses[%d] duplicates address %q", res.ID(), i, addr.String())
			}
			seenAddresses[addr.String()] = true
		}
		seenNames := map[string]bool{}
		for i, value := range spec.Names {
			value = strings.TrimSpace(value)
			if value == "" {
				return true, fmt.Errorf("%s spec.names[%d] must not be empty", res.ID(), i)
			}
			if err := validateDomainValue(value); err != nil {
				return true, fmt.Errorf("%s spec.names[%d] is invalid: %w", res.ID(), i, err)
			}
			if seenNames[value] {
				return true, fmt.Errorf("%s spec.names[%d] duplicates name %q", res.ID(), i, value)
			}
			seenNames[value] = true
		}
		if spec.RefreshInterval != "" {
			if _, err := time.ParseDuration(spec.RefreshInterval); err != nil {
				return true, fmt.Errorf("%s spec.refreshInterval is invalid: %w", res.ID(), err)
			}
		}
	case "LocalServiceRedirect":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.LocalServiceRedirectSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.Interface) == "" {
			return true, fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if len(spec.Rules) == 0 {
			return true, fmt.Errorf("%s spec.rules is required", res.ID())
		}
		for i, rule := range spec.Rules {
			if len(rule.Protocols) == 0 {
				return true, fmt.Errorf("%s spec.rules[%d].protocols is required", res.ID(), i)
			}
			seenProtocols := map[string]bool{}
			for j, proto := range rule.Protocols {
				switch proto {
				case "tcp", "udp":
				default:
					return true, fmt.Errorf("%s spec.rules[%d].protocols[%d] must be tcp or udp", res.ID(), i, j)
				}
				if seenProtocols[proto] {
					return true, fmt.Errorf("%s spec.rules[%d].protocols[%d] duplicates protocol %q", res.ID(), i, j, proto)
				}
				seenProtocols[proto] = true
			}
			if strings.TrimSpace(rule.DestinationSetRef) == "" {
				return true, fmt.Errorf("%s spec.rules[%d].destinationSetRef is required", res.ID(), i)
			}
			if rule.DestinationPort < 1 || rule.DestinationPort > 65535 {
				return true, fmt.Errorf("%s spec.rules[%d].destinationPort must be within 1-65535", res.ID(), i)
			}
			if rule.RedirectPort < 1 || rule.RedirectPort > 65535 {
				return true, fmt.Errorf("%s spec.rules[%d].redirectPort must be within 1-65535", res.ID(), i)
			}
		}
	case "FirewallZone":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.FirewallZoneSpec()
		if err != nil {
			return true, err
		}
		switch spec.Role {
		case "untrust", "trust", "mgmt":
		default:
			return true, fmt.Errorf("%s spec.role must be untrust, trust, or mgmt", res.ID())
		}
		if len(spec.Interfaces) == 0 {
			return true, fmt.Errorf("%s spec.interfaces is required", res.ID())
		}
		seen := map[string]bool{}
		for i, name := range spec.Interfaces {
			if name == "" {
				return true, fmt.Errorf("%s spec.interfaces[%d] is required", res.ID(), i)
			}
			if seen[name] {
				return true, fmt.Errorf("%s spec.interfaces[%d] duplicates %q", res.ID(), i, name)
			}
			seen[name] = true
		}
	case "FirewallPolicy":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		if _, err := res.FirewallPolicySpec(); err != nil {
			return true, err
		}
	case "ClientPolicy":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.ClientPolicySpec()
		if err != nil {
			return true, err
		}
		switch spec.Mode {
		case "include", "exclude":
		default:
			return true, fmt.Errorf("%s spec.mode must be include or exclude", res.ID())
		}
		seenInterfaces := map[string]bool{}
		for i, name := range spec.Interfaces {
			if strings.TrimSpace(name) == "" {
				return true, fmt.Errorf("%s spec.interfaces[%d] is required", res.ID(), i)
			}
			if seenInterfaces[name] {
				return true, fmt.Errorf("%s spec.interfaces[%d] duplicates %q", res.ID(), i, name)
			}
			seenInterfaces[name] = true
		}
		seenMACs := map[string]bool{}
		for i, value := range spec.MACs {
			mac, err := net.ParseMAC(value)
			if err != nil {
				return true, fmt.Errorf("%s spec.macs[%d] is invalid: %w", res.ID(), i, err)
			}
			normalizedMAC := strings.ToLower(mac.String())
			if seenMACs[normalizedMAC] {
				return true, fmt.Errorf("%s spec.macs[%d] duplicates %q", res.ID(), i, normalizedMAC)
			}
			seenMACs[normalizedMAC] = true
		}
		seenIPv6Addresses := map[string]bool{}
		for i, entry := range spec.Classification {
			switch entry.Mode {
			case "trusted", "guest", "isolated":
			default:
				return true, fmt.Errorf("%s spec.classification[%d].mode must be trusted, guest, or isolated", res.ID(), i)
			}
			if len(entry.Match.MACs) == 0 && len(entry.Match.OUIPrefixes) == 0 && len(entry.Match.HostnamePatterns) == 0 && len(entry.Match.DHCPFingerprints) == 0 {
				return true, fmt.Errorf("%s spec.classification[%d].match must contain at least one selector", res.ID(), i)
			}
			for j, value := range entry.Match.MACs {
				mac, err := net.ParseMAC(value)
				if err != nil {
					return true, fmt.Errorf("%s spec.classification[%d].match.macs[%d] is invalid: %w", res.ID(), i, j, err)
				}
				normalizedMAC := strings.ToLower(mac.String())
				if seenMACs[normalizedMAC] {
					return true, fmt.Errorf("%s spec.classification[%d].match.macs[%d] duplicates %q", res.ID(), i, j, normalizedMAC)
				}
				seenMACs[normalizedMAC] = true
			}
			seenOUI := map[string]bool{}
			for j, value := range entry.Match.OUIPrefixes {
				normalized, err := normalizeOUIPrefix(value)
				if err != nil {
					return true, fmt.Errorf("%s spec.classification[%d].match.ouiPrefixes[%d] is invalid: %w", res.ID(), i, j, err)
				}
				if seenOUI[normalized] {
					return true, fmt.Errorf("%s spec.classification[%d].match.ouiPrefixes[%d] duplicates %q", res.ID(), i, j, normalized)
				}
				seenOUI[normalized] = true
			}
			for j, pattern := range entry.Match.HostnamePatterns {
				if strings.TrimSpace(pattern) == "" {
					return true, fmt.Errorf("%s spec.classification[%d].match.hostnamePatterns[%d] is required", res.ID(), i, j)
				}
				if _, err := path.Match(pattern, "routerd-test-hostname"); err != nil {
					return true, fmt.Errorf("%s spec.classification[%d].match.hostnamePatterns[%d] is invalid: %w", res.ID(), i, j, err)
				}
			}
			seenFingerprints := map[string]bool{}
			for j, value := range entry.Match.DHCPFingerprints {
				value = strings.TrimSpace(value)
				if value == "" || strings.ContainsAny(value, " \t\n\r") {
					return true, fmt.Errorf("%s spec.classification[%d].match.dhcpFingerprints[%d] must be a non-empty token", res.ID(), i, j)
				}
				if seenFingerprints[value] {
					return true, fmt.Errorf("%s spec.classification[%d].match.dhcpFingerprints[%d] duplicates %q", res.ID(), i, j, value)
				}
				seenFingerprints[value] = true
			}
			if strings.Contains(entry.IPv4Reservation, "/") {
				return true, fmt.Errorf("%s spec.classification[%d].ipv4Reservation must be a DHCPv4Reservation name, not Kind/name", res.ID(), i)
			}
			for j, value := range entry.IPv6Addresses {
				address, err := netip.ParseAddr(strings.TrimSpace(value))
				if err != nil || !address.Is6() || address.Is4In6() {
					return true, fmt.Errorf("%s spec.classification[%d].ipv6Addresses[%d] must be an IPv6 address", res.ID(), i, j)
				}
				normalized := address.String()
				if seenIPv6Addresses[normalized] {
					return true, fmt.Errorf("%s spec.classification[%d].ipv6Addresses[%d] duplicates %q", res.ID(), i, j, normalized)
				}
				seenIPv6Addresses[normalized] = true
			}
		}
		for i, service := range spec.GuestServices {
			switch service {
			case "dns", "dhcp", "ntp", "mdns", "ssdp":
			default:
				return true, fmt.Errorf("%s spec.guestServices[%d] must be dns, dhcp, ntp, mdns, or ssdp", res.ID(), i)
			}
		}
		for key, value := range map[string]string{
			"lanInternet":   spec.Isolation.LANInternet,
			"lanLAN":        spec.Isolation.LANLAN,
			"lanMgmt":       spec.Isolation.LANMgmt,
			"mDNSBroadcast": spec.Isolation.MDNSBroadcast,
		} {
			switch value {
			case "", "allow", "deny":
			default:
				return true, fmt.Errorf("%s spec.isolation.%s must be allow or deny", res.ID(), key)
			}
		}
		for i, cidr := range append(append([]string{}, spec.GuestEgressDeny...), spec.GuestEgressAllow...) {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return true, fmt.Errorf("%s guest egress CIDR[%d] is invalid: %w", res.ID(), i, err)
			}
		}
	case "FirewallEventLog":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.FirewallEventLogSpec()
		if err != nil {
			return true, err
		}
		for i, event := range spec.Events {
			switch event {
			case "deny", "allow", "rateLimit", "connLimit":
			default:
				return true, fmt.Errorf("%s spec.events[%d] must be deny, allow, rateLimit, or connLimit", res.ID(), i)
			}
		}
		if spec.NFLogGroup < 0 || spec.NFLogGroup > 65535 {
			return true, fmt.Errorf("%s spec.nflogGroup must be between 0 and 65535", res.ID())
		}
		if spec.SampleRate < 0 {
			return true, fmt.Errorf("%s spec.sampleRate must be greater than or equal to 0", res.ID())
		}
		if spec.Log.CopyRange < 0 {
			return true, fmt.Errorf("%s spec.log.copyRange must be greater than or equal to 0", res.ID())
		}
	case "FirewallFlowPinhole":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.FirewallFlowPinholeSpec()
		if err != nil {
			return true, err
		}
		if spec.FromZone == "" {
			return true, fmt.Errorf("%s spec.fromZone is required", res.ID())
		}
		if spec.ToZone == "" {
			return true, fmt.Errorf("%s spec.toZone is required", res.ID())
		}
		if spec.FromZone == spec.ToZone {
			return true, fmt.Errorf("%s spec.fromZone and spec.toZone must be different", res.ID())
		}
		if spec.Outbound.Protocol != "udp" {
			return true, fmt.Errorf("%s spec.outbound.protocol must be udp", res.ID())
		}
		switch spec.Correlation.Key {
		case "", "remoteAddress", "localEndpoint":
		default:
			return true, fmt.Errorf("%s spec.correlation.key must be remoteAddress or localEndpoint", res.ID())
		}
		if spec.Outbound.SourceSetRef == "" && len(spec.Outbound.SourceCIDRs) == 0 {
			return true, fmt.Errorf("%s spec.outbound.sourceSetRef or spec.outbound.sourceCIDRs is required", res.ID())
		}
		if spec.Outbound.SourceSetRef != "" {
			kind, name := splitResourceRef(spec.Outbound.SourceSetRef)
			if kind != "IPAddressSet" || strings.TrimSpace(name) == "" {
				return true, fmt.Errorf("%s spec.outbound.sourceSetRef must reference IPAddressSet", res.ID())
			}
		}
		for i, cidr := range spec.Outbound.SourceCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return true, fmt.Errorf("%s spec.outbound.sourceCIDRs[%d] must be an IPv4 prefix", res.ID(), i)
			}
		}
		for i, cidr := range spec.Outbound.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return true, fmt.Errorf("%s spec.outbound.destinationCIDRs[%d] must be an IPv4 prefix", res.ID(), i)
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.outbound.destinationSetRefs", spec.Outbound.DestinationSetRefs); err != nil {
			return true, err
		}
		if len(spec.Outbound.DestinationPorts) == 0 {
			return true, fmt.Errorf("%s spec.outbound.destinationPorts is required", res.ID())
		}
		if err := validateFirewallPortList(res.ID(), "spec.outbound.destinationPorts", spec.Outbound.DestinationPorts); err != nil {
			return true, err
		}
		if len(spec.Inbound.SourcePorts) == 0 {
			return true, fmt.Errorf("%s spec.inbound.sourcePorts is required", res.ID())
		}
		if err := validateFirewallPortList(res.ID(), "spec.inbound.sourcePorts", spec.Inbound.SourcePorts); err != nil {
			return true, err
		}
		timeout := strings.TrimSpace(spec.Timeout)
		if timeout != "" {
			d, err := time.ParseDuration(timeout)
			if err != nil {
				return true, fmt.Errorf("%s spec.timeout must be a duration: %w", res.ID(), err)
			}
			if d <= 0 {
				return true, fmt.Errorf("%s spec.timeout must be greater than zero", res.ID())
			}
		}
	case "FirewallRule":
		if res.APIVersion != api.FirewallAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.FirewallRuleSpec()
		if err != nil {
			return true, err
		}
		if spec.FromZone == "" {
			return true, fmt.Errorf("%s spec.fromZone is required", res.ID())
		}
		if spec.ToZone == "" {
			return true, fmt.Errorf("%s spec.toZone is required", res.ID())
		}
		switch spec.Action {
		case "accept", "drop", "reject":
		default:
			return true, fmt.Errorf("%s spec.action must be accept, drop, or reject", res.ID())
		}
		switch spec.Protocol {
		case "", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip":
		default:
			return true, fmt.Errorf("%s spec.protocol must be tcp, udp, icmp, icmpv6, ipv6-icmp, or ipip", res.ID())
		}
		if spec.Port != 0 && len(spec.DestinationPorts) > 0 {
			return true, fmt.Errorf("%s spec.port and spec.destinationPorts are mutually exclusive", res.ID())
		}
		if err := validateFirewallRulePorts(res.ID(), spec); err != nil {
			return true, err
		}
		if err := validateFirewallRuleICMP(res.ID(), spec); err != nil {
			return true, err
		}
		if err := validateFirewallRateLimit(res.ID(), spec.RateLimit); err != nil {
			return true, err
		}
		if spec.ConnLimit.MaxPerSource < 0 {
			return true, fmt.Errorf("%s spec.connLimit.maxPerSource must be greater than or equal to 0", res.ID())
		}
		for i, cidr := range spec.SourceCIDRs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return true, fmt.Errorf("%s spec.srcCIDRs[%d] is invalid: %w", res.ID(), i, err)
			}
		}
		for i, cidr := range spec.DestinationCIDRs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return true, fmt.Errorf("%s spec.destinationCIDRs[%d] is invalid: %w", res.ID(), i, err)
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs); err != nil {
			return true, err
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs); err != nil {
			return true, err
		}
	default:
		return false, nil
	}
	return true, nil
}

func validateNAT44SessionSyncNATRuleRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.ContainsAny(ref, "\n\r") {
		return fmt.Errorf("must not contain newline")
	}
	if kind, name, ok := strings.Cut(ref, "/"); ok {
		if kind != "NAT44Rule" || strings.TrimSpace(name) == "" || strings.Contains(name, "/") {
			return fmt.Errorf("must be NAT44Rule/name or name")
		}
	}
	return nil
}
