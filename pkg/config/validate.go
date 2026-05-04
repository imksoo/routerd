package config

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/dnsresolver"
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
	if err := validateApplyPolicy(router.Spec.Apply); err != nil {
		return err
	}

	seen := map[string]bool{}
	baseInterfaces := map[string]bool{}
	interfaces := map[string]bool{}
	wireGuardInterfaces := map[string]bool{}
	dhcp4Servers := map[string]bool{}
	dhcp4ServerSpecs := map[string]api.DHCPv4ServerSpec{}
	directDHCPv4Servers := map[string]bool{}
	dhcp4Scopes := map[string]api.DHCPv4ScopeSpec{}
	dhcp6Servers := map[string]bool{}
	dhcp6ServerSpecs := map[string]api.DHCPv6ServerSpec{}
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
	dhcp6AddressByInterface := map[string]struct {
		id     string
		client string
	}{}
	externalPDByInterface := map[string]struct {
		id     string
		client string
	}{}
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
		if res.APIVersion == api.NetAPIVersion && res.Kind == "Bridge" {
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VXLANSegment" {
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "WireGuardInterface" {
			wireGuardInterfaces[res.Metadata.Name] = true
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VRF" {
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VXLANTunnel" {
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && (res.Kind == "PPPoEInterface" || res.Kind == "PPPoESession") {
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv4Server" {
			dhcp4Servers[res.Metadata.Name] = true
			spec, err := res.DHCPv4ServerSpec()
			if err != nil {
				return err
			}
			dhcp4ServerSpecs[res.Metadata.Name] = spec
			if spec.Interface != "" {
				directDHCPv4Servers[res.Metadata.Name] = true
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6Server" {
			dhcp6Servers[res.Metadata.Name] = true
			spec, err := res.DHCPv6ServerSpec()
			if err != nil {
				return err
			}
			dhcp6ServerSpecs[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6Scope" {
			dhcp6Scopes[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv4Scope" {
			spec, err := res.DHCPv4ScopeSpec()
			if err != nil {
				return err
			}
			dhcp4Scopes[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6PrefixDelegation" {
			prefixDelegations[res.Metadata.Name] = true
			spec, err := res.DHCPv6PrefixDelegationSpec()
			if err != nil {
				return err
			}
			if isExternalIPv6PDClient(spec.Client) {
				externalPDByInterface[spec.Interface] = struct {
					id     string
					client string
				}{id: res.ID(), client: spec.Client}
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6Address" {
			spec, err := res.DHCPv6AddressSpec()
			if err != nil {
				return err
			}
			dhcp6AddressByInterface[spec.Interface] = struct {
				id     string
				client string
			}{id: res.ID(), client: defaultString(spec.Client, "networkd")}
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
		if res.APIVersion == api.FirewallAPIVersion && res.Kind == "FirewallZone" {
			zones[res.Metadata.Name] = true
		}
	}
	for i, name := range router.Spec.Apply.ProtectedInterfaces {
		if !interfaces[name] {
			return fmt.Errorf("spec.apply.protectedInterfaces[%d] references missing Interface %q", i, name)
		}
	}
	for i, name := range router.Spec.Apply.ProtectedZones {
		if !zones[name] {
			return fmt.Errorf("spec.apply.protectedZones[%d] references missing FirewallZone %q", i, name)
		}
	}
	for iface, pd := range externalPDByInterface {
		if dhcpv6, ok := dhcp6AddressByInterface[iface]; ok && dhcpv6.client != pd.client {
			return fmt.Errorf("%s conflicts with %s on interface %q: client=%s must own DHCPv6 on that interface", pd.id, dhcpv6.id, iface, pd.client)
		}
	}

	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "IPv4StaticAddress", "DHCPv4Address", "DHCPv4Lease", "IPv4StaticRoute", "IPv6StaticRoute", "DHCPv4Scope", "DHCPv6Address", "IPv6RAAddress", "DHCPv6PrefixDelegation", "IPv6DelegatedAddress", "DSLiteTunnel", "PPPoEInterface", "PPPoESession":
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
			if (res.Kind == "PPPoEInterface" || res.Kind == "PPPoESession") && !baseInterfaces[name] {
				return fmt.Errorf("%s spec.interface must reference a base Interface %q", res.ID(), name)
			}
		}
		if res.Kind == "Bridge" {
			spec, err := res.BridgeSpec()
			if err != nil {
				return err
			}
			for i, member := range spec.Members {
				if !interfaces[member] {
					return fmt.Errorf("%s spec.members[%d] references missing Interface %q", res.ID(), i, member)
				}
				if member == res.Metadata.Name {
					return fmt.Errorf("%s spec.members[%d] must not reference the bridge itself", res.ID(), i)
				}
			}
		}
		if res.Kind == "VXLANSegment" {
			spec, err := res.VXLANSegmentSpec()
			if err != nil {
				return err
			}
			if !interfaces[spec.UnderlayInterface] {
				return fmt.Errorf("%s spec.underlayInterface references missing Interface %q", res.ID(), spec.UnderlayInterface)
			}
			if spec.Bridge != "" && !interfaces[spec.Bridge] {
				return fmt.Errorf("%s spec.bridge references missing Bridge %q", res.ID(), spec.Bridge)
			}
		}
		if res.Kind == "WireGuardPeer" {
			spec, err := res.WireGuardPeerSpec()
			if err != nil {
				return err
			}
			if !wireGuardInterfaces[spec.Interface] {
				return fmt.Errorf("%s spec.interface references missing WireGuardInterface %q", res.ID(), spec.Interface)
			}
		}
		if res.Kind == "VRF" {
			spec, err := res.VRFSpec()
			if err != nil {
				return err
			}
			for i, member := range spec.Members {
				if !interfaces[member] {
					return fmt.Errorf("%s spec.members[%d] references missing Interface %q", res.ID(), i, member)
				}
				if member == res.Metadata.Name {
					return fmt.Errorf("%s spec.members[%d] must not reference the VRF itself", res.ID(), i)
				}
			}
		}
		if res.Kind == "VXLANTunnel" {
			spec, err := res.VXLANTunnelSpec()
			if err != nil {
				return err
			}
			if !interfaces[spec.UnderlayInterface] {
				return fmt.Errorf("%s spec.underlayInterface references missing Interface %q", res.ID(), spec.UnderlayInterface)
			}
			if spec.Bridge != "" && !interfaces[spec.Bridge] {
				return fmt.Errorf("%s spec.bridge references missing Bridge %q", res.ID(), spec.Bridge)
			}
		}
		if res.Kind == "DHCPv4Scope" {
			spec, err := res.DHCPv4ScopeSpec()
			if err != nil {
				return err
			}
			if !dhcp4Servers[spec.Server] {
				return fmt.Errorf("%s references missing DHCPv4Server %q", res.ID(), spec.Server)
			}
			if !stringInSlice(spec.Interface, dhcp4ServerSpecs[spec.Server].ListenInterfaces) {
				return fmt.Errorf("%s spec.interface %q must be listed in DHCPv4Server %q spec.listenInterfaces", res.ID(), spec.Interface, spec.Server)
			}
			if spec.DNSInterface != "" && !interfaces[spec.DNSInterface] {
				return fmt.Errorf("%s references missing DNS Interface %q", res.ID(), spec.DNSInterface)
			}
		}
		if res.Kind == "DHCPv4Reservation" {
			spec, err := res.DHCPv4ReservationSpec()
			if err != nil {
				return err
			}
			if spec.Scope != "" {
				scope, ok := dhcp4Scopes[spec.Scope]
				if !ok {
					return fmt.Errorf("%s references missing DHCPv4Scope %q", res.ID(), spec.Scope)
				}
				ip, err := netip.ParseAddr(spec.IPAddress)
				if err != nil || !ip.Is4() {
					return fmt.Errorf("%s spec.ipAddress must be an IPv4 address", res.ID())
				}
				start := netip.MustParseAddr(scope.RangeStart)
				end := netip.MustParseAddr(scope.RangeEnd)
				if ip.Compare(start) < 0 || ip.Compare(end) > 0 {
					return fmt.Errorf("%s spec.ipAddress must be inside DHCPv4Scope %q range", res.ID(), spec.Scope)
				}
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
		if res.Kind == "DHCPv4Server" {
			spec, err := res.DHCPv4ServerSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.ListenInterfaces {
				if !interfaces[name] {
					return fmt.Errorf("%s spec.listenInterfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
			if spec.Interface != "" && !interfaces[spec.Interface] {
				return fmt.Errorf("%s spec.interface references missing Interface %q", res.ID(), spec.Interface)
			}
		}
		if res.Kind == "DHCPv6Server" {
			spec, err := res.DHCPv6ServerSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.ListenInterfaces {
				if !interfaces[name] {
					return fmt.Errorf("%s spec.listenInterfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
		}
		if res.Kind == "DHCPv4Reservation" {
			spec, err := res.DHCPv4ReservationSpec()
			if err != nil {
				return err
			}
			if spec.Server != "" {
				if !directDHCPv4Servers[spec.Server] {
					return fmt.Errorf("%s spec.server references missing direct DHCPv4Server %q", res.ID(), spec.Server)
				}
			} else if spec.Scope == "" && len(directDHCPv4Servers) != 1 {
				return fmt.Errorf("%s spec.server is required when direct DHCPv4Server count is not one", res.ID())
			}
		}
		if res.Kind == "DHCPv4Relay" {
			spec, err := res.DHCPv4RelaySpec()
			if err != nil {
				return err
			}
			for i, name := range spec.Interfaces {
				if !interfaces[name] {
					return fmt.Errorf("%s spec.interfaces[%d] references missing Interface %q", res.ID(), i, name)
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
				if target.HealthCheck != "" && !healthChecks[target.HealthCheck] {
					return fmt.Errorf("%s target %q references missing HealthCheck %q", res.ID(), target.Name, target.HealthCheck)
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
				return fmt.Errorf("%s spec.ipv6RA.scope references missing DHCPv6Scope %q", res.ID(), spec.IPv6RA.Scope)
			}
		}
		if res.Kind == "IPv6DelegatedAddress" {
			spec, err := res.IPv6DelegatedAddressSpec()
			if err != nil {
				return err
			}
			if !prefixDelegations[spec.PrefixDelegation] {
				return fmt.Errorf("%s references missing DHCPv6PrefixDelegation %q", res.ID(), spec.PrefixDelegation)
			}
		}
		if res.Kind == "DHCPv6Scope" {
			spec, err := res.DHCPv6ScopeSpec()
			if err != nil {
				return err
			}
			if !dhcp6Servers[spec.Server] {
				return fmt.Errorf("%s references missing DHCPv6Server %q", res.ID(), spec.Server)
			}
			if !delegatedAddresses[spec.DelegatedAddress] {
				return fmt.Errorf("%s references missing IPv6DelegatedAddress %q", res.ID(), spec.DelegatedAddress)
			}
			if !stringInSlice(delegatedAddressInterfaces[spec.DelegatedAddress], dhcp6ServerSpecs[spec.Server].ListenInterfaces) {
				return fmt.Errorf("%s delegatedAddress interface %q must be listed in DHCPv6Server %q spec.listenInterfaces", res.ID(), delegatedAddressInterfaces[spec.DelegatedAddress], spec.Server)
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
		if res.Kind == "DNSResolver" {
			spec, err := res.DNSResolverSpec()
			if err != nil {
				return err
			}
			for i, source := range spec.Sources {
				if source.ViaInterface != "" && !interfaces[refName(source.ViaInterface)] && !wireGuardInterfaces[refName(source.ViaInterface)] {
					return fmt.Errorf("%s spec.sources[%d].viaInterface references missing Interface or WireGuardInterface %q", res.ID(), i, source.ViaInterface)
				}
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
			if spec.SourceInterface != "" && !interfaces[spec.SourceInterface] && !dsliteTunnels[spec.SourceInterface] {
				return fmt.Errorf("%s references missing source Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), spec.SourceInterface)
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
		if res.Kind == "EgressRoutePolicy" {
			spec, err := res.EgressRoutePolicySpec()
			if err != nil {
				return err
			}
			for i, candidate := range spec.Candidates {
				if candidate.HealthCheck != "" && !healthChecks[candidate.HealthCheck] {
					return fmt.Errorf("%s spec.candidates[%d] references missing HealthCheck %q", res.ID(), i, candidate.HealthCheck)
				}
			}
		}
		if res.Kind == "FirewallZone" {
			spec, err := res.FirewallZoneSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.Interfaces {
				refKind, refName := splitFirewallInterfaceRef(name)
				switch refKind {
				case "Interface", "PPPoEInterface":
					if !interfaces[refName] && !dsliteTunnels[refName] {
						return fmt.Errorf("%s spec.interfaces[%d] references missing Interface, PPPoEInterface, or DSLiteTunnel %q", res.ID(), i, refName)
					}
				case "DSLiteTunnel":
					if !dsliteTunnels[refName] {
						return fmt.Errorf("%s spec.interfaces[%d] references missing DSLiteTunnel %q", res.ID(), i, refName)
					}
				default:
					return fmt.Errorf("%s spec.interfaces[%d] has unsupported reference %q", res.ID(), i, name)
				}
			}
		}
		if res.Kind == "FirewallRule" {
			spec, err := res.FirewallRuleSpec()
			if err != nil {
				return err
			}
			if spec.FromZone != "self" && !zones[spec.FromZone] {
				return fmt.Errorf("%s spec.fromZone references missing FirewallZone %q", res.ID(), spec.FromZone)
			}
			if spec.ToZone != "self" && !zones[spec.ToZone] {
				return fmt.Errorf("%s spec.toZone references missing FirewallZone %q", res.ID(), spec.ToZone)
			}
		}
		if res.Kind == "FirewallPolicy" {
			if _, err := res.FirewallPolicySpec(); err != nil {
				return err
			}
		}
	}
	return nil
}

func isExternalIPv6PDClient(client string) bool {
	switch client {
	case "dhcp6c", "dhcpcd":
		return true
	default:
		return false
	}
}

func splitFirewallInterfaceRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if kind, name, ok := strings.Cut(ref, "/"); ok {
		return kind, name
	}
	return "Interface", ref
}

func validateApplyPolicy(spec api.ApplyPolicySpec) error {
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
		if strings.TrimSpace(spec.ExpectedValue) == "" && spec.ExpectedValue != "" {
			return fmt.Errorf("%s spec.expectedValue must not be blank", res.ID())
		}
		switch defaultString(spec.Compare, "exact") {
		case "exact", "atLeast":
		default:
			return fmt.Errorf("%s spec.compare must be exact or atLeast", res.ID())
		}
	case "SysctlProfile":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.SysctlProfileSpec()
		if err != nil {
			return err
		}
		switch spec.Profile {
		case "router-linux":
		default:
			return fmt.Errorf("%s spec.profile must be router-linux", res.ID())
		}
		for key, value := range spec.Overrides {
			if strings.TrimSpace(key) == "" || strings.ContainsAny(key, " \t\n/") {
				return fmt.Errorf("%s spec.overrides contains invalid sysctl key %q", res.ID(), key)
			}
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%s spec.overrides[%s] must not be empty", res.ID(), key)
			}
		}
	case "Package":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.PackageSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.State, "present") {
		case "present":
		default:
			return fmt.Errorf("%s spec.state must be present", res.ID())
		}
		if len(spec.Packages) == 0 {
			return fmt.Errorf("%s spec.packages is required", res.ID())
		}
		for i, set := range spec.Packages {
			switch set.OS {
			case "ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "nixos", "freebsd":
			default:
				return fmt.Errorf("%s spec.packages[%d].os must be ubuntu, debian, fedora, rhel, rocky, almalinux, nixos, or freebsd", res.ID(), i)
			}
			switch defaultString(set.Manager, defaultPackageManager(set.OS)) {
			case "apt", "dnf", "nix", "pkg":
			default:
				return fmt.Errorf("%s spec.packages[%d].manager must be apt, dnf, nix, or pkg", res.ID(), i)
			}
			if len(set.Names) == 0 {
				return fmt.Errorf("%s spec.packages[%d].names is required", res.ID(), i)
			}
			for j, name := range set.Names {
				if strings.TrimSpace(name) == "" {
					return fmt.Errorf("%s spec.packages[%d].names[%d] must not be empty", res.ID(), i, j)
				}
				if strings.ContainsAny(name, " \t\n\r/") {
					return fmt.Errorf("%s spec.packages[%d].names[%d] must be a package name", res.ID(), i, j)
				}
			}
		}
	case "NetworkAdoption":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.NetworkAdoptionSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.State, "present") {
		case "present", "absent":
		default:
			return fmt.Errorf("%s spec.state must be present or absent", res.ID())
		}
		if spec.Interface == "" && spec.IfName == "" && !spec.SystemdResolved.DisableDNSStubListener {
			return fmt.Errorf("%s must set spec.interface, spec.ifname, or spec.systemdResolved.disableDNSStubListener", res.ID())
		}
		for field, value := range map[string]string{"interface": spec.Interface, "ifname": spec.IfName, "systemdNetworkd.dropinName": spec.SystemdNetworkd.DropinName, "systemdResolved.dropinName": spec.SystemdResolved.DropinName} {
			if strings.ContainsAny(value, "\x00\n\r") {
				return fmt.Errorf("%s spec.%s contains invalid characters", res.ID(), field)
			}
		}
	case "SystemdUnit":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.SystemdUnitSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.State, "present") {
		case "present", "absent":
		default:
			return fmt.Errorf("%s spec.state must be present or absent", res.ID())
		}
		unitName := defaultString(spec.UnitName, res.Metadata.Name)
		if !strings.HasSuffix(unitName, ".service") {
			return fmt.Errorf("%s spec.unitName must end with .service", res.ID())
		}
		if strings.ContainsAny(unitName, "/\x00\n\r") {
			return fmt.Errorf("%s spec.unitName contains invalid characters", res.ID())
		}
		if defaultString(spec.State, "present") == "present" && len(spec.ExecStart) == 0 {
			return fmt.Errorf("%s spec.execStart is required when state is present", res.ID())
		}
		for i, arg := range spec.ExecStart {
			if strings.TrimSpace(arg) == "" {
				return fmt.Errorf("%s spec.execStart[%d] must not be empty", res.ID(), i)
			}
			if strings.ContainsAny(arg, "\x00\n\r") {
				return fmt.Errorf("%s spec.execStart[%d] contains invalid characters", res.ID(), i)
			}
		}
		switch spec.Restart {
		case "", "no", "on-failure", "always":
		default:
			return fmt.Errorf("%s spec.restart must be no, on-failure, or always", res.ID())
		}
		switch spec.ProtectSystem {
		case "", "no", "false", "true", "full", "strict":
		default:
			return fmt.Errorf("%s spec.protectSystem must be no, false, true, full, or strict", res.ID())
		}
		switch spec.ProtectHome {
		case "", "true", "read-only", "tmpfs":
		default:
			return fmt.Errorf("%s spec.protectHome must be true, read-only, or tmpfs", res.ID())
		}
		switch spec.RuntimeDirectoryPreserve {
		case "", "no", "yes", "restart":
		default:
			return fmt.Errorf("%s spec.runtimeDirectoryPreserve must be no, yes, or restart", res.ID())
		}
		for i, path := range spec.ReadWritePaths {
			if strings.TrimSpace(path) == "" {
				return fmt.Errorf("%s spec.readWritePaths[%d] must not be empty", res.ID(), i)
			}
			if !strings.HasPrefix(path, "/") {
				return fmt.Errorf("%s spec.readWritePaths[%d] must be an absolute path", res.ID(), i)
			}
			if strings.ContainsAny(path, "\x00\n\r") {
				return fmt.Errorf("%s spec.readWritePaths[%d] contains invalid characters", res.ID(), i)
			}
		}
	case "LogRetention":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.LogRetentionSpec()
		if err != nil {
			return err
		}
		switch spec.Schedule {
		case "", "daily":
		default:
			return fmt.Errorf("%s spec.schedule must be daily", res.ID())
		}
		if len(spec.Targets) == 0 {
			return fmt.Errorf("%s spec.targets is required", res.ID())
		}
		for i, target := range spec.Targets {
			if !strings.HasPrefix(target.File, "/") {
				return fmt.Errorf("%s spec.targets[%d].file must be an absolute path", res.ID(), i)
			}
			if strings.TrimSpace(target.Retention) == "" {
				return fmt.Errorf("%s spec.targets[%d].retention is required", res.ID(), i)
			}
			if _, err := parseRetentionDuration(target.Retention); err != nil {
				return fmt.Errorf("%s spec.targets[%d].retention must be a duration", res.ID(), i)
			}
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
	case "WebConsole":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.WebConsoleSpec()
		if err != nil {
			return err
		}
		if strings.TrimSpace(spec.ListenAddress) != "" {
			if _, err := netip.ParseAddr(spec.ListenAddress); err != nil {
				return fmt.Errorf("%s spec.listenAddress must be an IP address", res.ID())
			}
		}
		if spec.Port < 0 || spec.Port > 65535 {
			return fmt.Errorf("%s spec.port must be omitted or between 1 and 65535", res.ID())
		}
		if spec.BasePath != "" {
			if !strings.HasPrefix(spec.BasePath, "/") || strings.ContainsAny(spec.BasePath, "\x00\r\n") {
				return fmt.Errorf("%s spec.basePath must be an absolute HTTP path", res.ID())
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
		if spec.RouterdService.BinaryPath != "" && strings.ContainsAny(spec.RouterdService.BinaryPath, " \t\n\r") {
			return fmt.Errorf("%s spec.routerdService.binaryPath must be a single path", res.ID())
		}
		if spec.RouterdService.ConfigFile != "" && strings.ContainsAny(spec.RouterdService.ConfigFile, " \t\n\r") {
			return fmt.Errorf("%s spec.routerdService.configFile must be a single path", res.ID())
		}
		if spec.RouterdService.Socket != "" && strings.ContainsAny(spec.RouterdService.Socket, " \t\n\r") {
			return fmt.Errorf("%s spec.routerdService.socket must be a single path", res.ID())
		}
		if spec.RouterdService.ApplyInterval != "" && strings.ContainsAny(spec.RouterdService.ApplyInterval, " \t\n\r") {
			return fmt.Errorf("%s spec.routerdService.applyInterval must be a single duration", res.ID())
		}
		for i, flag := range spec.RouterdService.ExtraFlags {
			if strings.TrimSpace(flag) == "" || strings.ContainsAny(flag, "\n\r") {
				return fmt.Errorf("%s spec.routerdService.extraFlags[%d] is invalid", res.ID(), i)
			}
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
	case "Inventory":
		if res.APIVersion != api.RouterAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.RouterAPIVersion)
		}
		if res.Metadata.Name != "host" {
			return fmt.Errorf("%s metadata.name must be host", res.ID())
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
	case "Link":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.LinkSpec()
		if err != nil {
			return err
		}
		if spec.IfName != "" && strings.ContainsAny(spec.IfName, " \t\n/") {
			return fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
	case "Bridge":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.BridgeSpec()
		if err != nil {
			return err
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		if strings.ContainsAny(ifname, " \t\n/") {
			return fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if len(ifname) > 15 {
			return fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if len(spec.Members) == 0 {
			return fmt.Errorf("%s spec.members must not be empty", res.ID())
		}
		seenMembers := map[string]bool{}
		for i, member := range spec.Members {
			if strings.TrimSpace(member) == "" {
				return fmt.Errorf("%s spec.members[%d] must not be empty", res.ID(), i)
			}
			if seenMembers[member] {
				return fmt.Errorf("%s spec.members[%d] duplicates %q", res.ID(), i, member)
			}
			seenMembers[member] = true
		}
		if spec.MACAddress != "" {
			if _, err := net.ParseMAC(spec.MACAddress); err != nil {
				return fmt.Errorf("%s spec.macAddress is invalid: %w", res.ID(), err)
			}
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
		if spec.ForwardDelay != 0 && (spec.ForwardDelay < 2 || spec.ForwardDelay > 30) {
			return fmt.Errorf("%s spec.forwardDelay must be within 2-30", res.ID())
		}
		if spec.HelloTime != 0 && (spec.HelloTime < 1 || spec.HelloTime > 10) {
			return fmt.Errorf("%s spec.helloTime must be within 1-10", res.ID())
		}
	case "VXLANSegment":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.VXLANSegmentSpec()
		if err != nil {
			return err
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		if strings.ContainsAny(ifname, " \t\n/") {
			return fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if len(ifname) > 15 {
			return fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if spec.VNI < 1 || spec.VNI > 16777215 {
			return fmt.Errorf("%s spec.vni must be within 1-16777215", res.ID())
		}
		if _, err := netip.ParseAddr(spec.LocalAddress); err != nil {
			return fmt.Errorf("%s spec.localAddress must be an IP address", res.ID())
		}
		if spec.UnderlayInterface == "" {
			return fmt.Errorf("%s spec.underlayInterface is required", res.ID())
		}
		if len(spec.Remotes) == 0 && spec.MulticastGroup == "" {
			return fmt.Errorf("%s spec.remotes or spec.multicastGroup is required", res.ID())
		}
		if len(spec.Remotes) > 0 && spec.MulticastGroup != "" {
			return fmt.Errorf("%s spec.remotes and spec.multicastGroup are mutually exclusive", res.ID())
		}
		for i, remote := range spec.Remotes {
			if _, err := netip.ParseAddr(remote); err != nil {
				return fmt.Errorf("%s spec.remotes[%d] must be an IP address", res.ID(), i)
			}
		}
		if spec.MulticastGroup != "" {
			addr, err := netip.ParseAddr(spec.MulticastGroup)
			if err != nil || !addr.Is4() || !addr.IsMulticast() {
				return fmt.Errorf("%s spec.multicastGroup must be an IPv4 multicast address", res.ID())
			}
		}
		if spec.UDPPort != 0 && (spec.UDPPort < 1 || spec.UDPPort > 65535) {
			return fmt.Errorf("%s spec.udpPort must be within 1-65535", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
		switch spec.L2Filter {
		case "", "default", "none":
		default:
			return fmt.Errorf("%s spec.l2Filter must be default or none", res.ID())
		}
	case "WireGuardInterface":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.WireGuardInterfaceSpec()
		if err != nil {
			return err
		}
		ifname := res.Metadata.Name
		if strings.ContainsAny(ifname, " \t\n/") {
			return fmt.Errorf("%s metadata.name must be usable as a WireGuard interface name", res.ID())
		}
		if len(ifname) > 15 {
			return fmt.Errorf("%s metadata.name must be 15 characters or fewer", res.ID())
		}
		if spec.ListenPort != 0 && (spec.ListenPort < 1 || spec.ListenPort > 65535) {
			return fmt.Errorf("%s spec.listenPort must be within 1-65535", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
	case "WireGuardPeer":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.WireGuardPeerSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.PublicKey == "" {
			return fmt.Errorf("%s spec.publicKey is required", res.ID())
		}
		if len(spec.AllowedIPs) == 0 {
			return fmt.Errorf("%s spec.allowedIPs is required", res.ID())
		}
		for i, allowed := range spec.AllowedIPs {
			if _, err := netip.ParsePrefix(allowed); err != nil {
				return fmt.Errorf("%s spec.allowedIPs[%d] must be an IP prefix", res.ID(), i)
			}
		}
		if spec.PersistentKeepalive < 0 || spec.PersistentKeepalive > 65535 {
			return fmt.Errorf("%s spec.persistentKeepalive must be within 0-65535", res.ID())
		}
	case "IPsecConnection":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPsecConnectionSpec()
		if err != nil {
			return err
		}
		if _, err := netip.ParseAddr(spec.LocalAddress); err != nil {
			return fmt.Errorf("%s spec.localAddress must be an IP address", res.ID())
		}
		if _, err := netip.ParseAddr(spec.RemoteAddress); err != nil {
			return fmt.Errorf("%s spec.remoteAddress must be an IP address", res.ID())
		}
		if spec.PreSharedKey == "" && spec.CertificateRef == "" {
			return fmt.Errorf("%s spec.preSharedKey or spec.certificateRef is required", res.ID())
		}
		if spec.PreSharedKey != "" && spec.CertificateRef != "" {
			return fmt.Errorf("%s spec.preSharedKey and spec.certificateRef are mutually exclusive", res.ID())
		}
		if _, err := netip.ParsePrefix(spec.LeftSubnet); err != nil {
			return fmt.Errorf("%s spec.leftSubnet must be an IP prefix", res.ID())
		}
		if _, err := netip.ParsePrefix(spec.RightSubnet); err != nil {
			return fmt.Errorf("%s spec.rightSubnet must be an IP prefix", res.ID())
		}
		switch spec.CloudProviderHint {
		case "", "aws", "azure", "gcp":
		default:
			return fmt.Errorf("%s spec.cloudProviderHint must be aws, azure, or gcp", res.ID())
		}
	case "VRF":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.VRFSpec()
		if err != nil {
			return err
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		if strings.ContainsAny(ifname, " \t\n/") {
			return fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if len(ifname) > 15 {
			return fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if spec.RouteTable < 1 {
			return fmt.Errorf("%s spec.routeTable is required", res.ID())
		}
	case "VXLANTunnel":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.VXLANTunnelSpec()
		if err != nil {
			return err
		}
		ifname := defaultString(spec.IfName, res.Metadata.Name)
		if strings.ContainsAny(ifname, " \t\n/") {
			return fmt.Errorf("%s spec.ifname contains invalid whitespace or slash", res.ID())
		}
		if len(ifname) > 15 {
			return fmt.Errorf("%s spec.ifname must be 15 characters or fewer", res.ID())
		}
		if spec.VNI < 1 || spec.VNI > 16777215 {
			return fmt.Errorf("%s spec.vni must be within 1-16777215", res.ID())
		}
		if _, err := netip.ParseAddr(spec.LocalAddress); err != nil {
			return fmt.Errorf("%s spec.localAddress must be an IP address", res.ID())
		}
		if spec.UnderlayInterface == "" {
			return fmt.Errorf("%s spec.underlayInterface is required", res.ID())
		}
		for i, peer := range spec.Peers {
			if _, err := netip.ParseAddr(peer); err != nil {
				return fmt.Errorf("%s spec.peers[%d] must be an IP address", res.ID(), i)
			}
		}
		if spec.UDPPort != 0 && (spec.UDPPort < 1 || spec.UDPPort > 65535) {
			return fmt.Errorf("%s spec.udpPort must be within 1-65535", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
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
	case "PPPoESession":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.PPPoESessionSpec()
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
		switch spec.AuthMethod {
		case "", "chap", "pap", "both":
		default:
			return fmt.Errorf("%s spec.authMethod must be chap, pap, or both", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 1500) {
			return fmt.Errorf("%s spec.mtu must be within 576-1500", res.ID())
		}
		if spec.MRU != 0 && (spec.MRU < 576 || spec.MRU > 1500) {
			return fmt.Errorf("%s spec.mru must be within 576-1500", res.ID())
		}
		if spec.LCPEchoInterval < 0 || spec.LCPEchoFailure < 0 {
			return fmt.Errorf("%s spec.lcpEchoInterval and spec.lcpEchoFailure must be non-negative", res.ID())
		}
		if strings.ContainsAny(spec.ServiceName, "\n\r") || strings.ContainsAny(spec.ACName, "\n\r") {
			return fmt.Errorf("%s spec.serviceName and spec.acName must not contain newlines", res.ID())
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
	case "DHCPv4Address", "DHCPv6Address", "IPv6RAAddress":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
	case "DHCPv4Lease":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4LeaseSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.RequestedAddress != "" {
			addr, err := netip.ParseAddr(spec.RequestedAddress)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("%s spec.requestedAddress must be an IPv4 address", res.ID())
			}
		}
		if strings.ContainsAny(spec.Hostname, " \t\n\r") {
			return fmt.Errorf("%s spec.hostname must not contain whitespace", res.ID())
		}
	case "IPv4StaticRoute":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4StaticRouteSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		destination, err := netip.ParsePrefix(spec.Destination)
		if err != nil || !destination.Addr().Is4() {
			return fmt.Errorf("%s spec.destination must be an IPv4 CIDR", res.ID())
		}
		via, err := netip.ParseAddr(spec.Via)
		if err != nil || !via.Is4() {
			return fmt.Errorf("%s spec.via must be an IPv4 address", res.ID())
		}
	case "IPv6StaticRoute":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6StaticRouteSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		destination, err := netip.ParsePrefix(spec.Destination)
		if err != nil || !destination.Addr().Is6() {
			return fmt.Errorf("%s spec.destination must be an IPv6 CIDR", res.ID())
		}
		via, err := netip.ParseAddr(spec.Via)
		if err != nil || !via.Is6() {
			return fmt.Errorf("%s spec.via must be an IPv6 address", res.ID())
		}
	case "DHCPv6PrefixDelegation":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv6PrefixDelegationSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		switch spec.Profile {
		case "", api.IPv6PDProfileDefault, api.IPv6PDProfileNTTNGNDirectHikariDenwa, api.IPv6PDProfileNTTHGWLANPD:
		default:
			return fmt.Errorf("%s spec.profile must be default, ntt-ngn-direct-hikari-denwa, or ntt-hgw-lan-pd", res.ID())
		}
		if spec.PrefixLength != 0 && (spec.PrefixLength < 1 || spec.PrefixLength > 128) {
			return fmt.Errorf("%s spec.prefixLength must be within 1-128", res.ID())
		}
		if spec.IAID != "" && !validIAID(spec.IAID) {
			return fmt.Errorf("%s spec.iaid must be a uint32 decimal value, 0x-prefixed hex value, or 8 hex digits", res.ID())
		}
		switch spec.DUIDType {
		case "", "vendor", "uuid", "link-layer-time", "link-layer":
		default:
			return fmt.Errorf("%s spec.duidType must be vendor, uuid, link-layer-time, or link-layer", res.ID())
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
		if spec.PrefixSource != "" {
			return fmt.Errorf("%s spec.prefixSource was removed; use spec.prefixDelegation and spec.dependsOn", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
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
	case "DHCPv6Information":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv6InformationSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
	case "IPv6RouterAdvertisement":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv6RouterAdvertisementSpec()
		if err != nil {
			return err
		}
		if spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if spec.PrefixSource != "" {
			return fmt.Errorf("%s spec.prefixSource was removed; use spec.prefix or spec.prefixFrom", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		if spec.Prefix == "" && spec.PrefixFrom.Resource == "" {
			return fmt.Errorf("%s spec.prefix or spec.prefixFrom is required", res.ID())
		}
		if spec.MTU != 0 && (spec.MTU < 1280 || spec.MTU > 65535) {
			return fmt.Errorf("%s spec.mtu must be within 1280-65535", res.ID())
		}
		switch spec.PRFPreference {
		case "", "low", "medium", "high":
		default:
			return fmt.Errorf("%s spec.prfPreference must be low, medium, or high", res.ID())
		}
		for _, server := range spec.RDNSS {
			if strings.HasPrefix(strings.TrimSpace(server), "${") {
				return fmt.Errorf("%s spec.rdnss status expressions were removed; use spec.rdnssFrom", res.ID())
			}
			addr, err := netip.ParseAddr(server)
			if err != nil || !addr.Is6() {
				return fmt.Errorf("%s spec.rdnss entries must be IPv6 addresses or status references", res.ID())
			}
		}
	case "DHCPv6Server":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv6ServerSpec()
		if err != nil {
			return err
		}
		if len(spec.ReadyWhen) > 0 {
			return fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		switch defaultString(spec.Role, "server") {
		case "server", "transit":
		default:
			return fmt.Errorf("%s spec.role must be server or transit", res.ID())
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
		rendersLANService := spec.Interface != "" || spec.Mode != "" || spec.AddressPool.Start != "" || spec.AddressPool.End != "" || len(spec.DNSServers) > 0 || len(spec.SNTPServers) > 0 || len(spec.DomainSearch) > 0
		if rendersLANService && spec.Interface == "" {
			return fmt.Errorf("%s spec.interface is required when rendering DHCPv6 LAN service", res.ID())
		}
		switch spec.Mode {
		case "", "stateless", "stateful", "both":
		default:
			return fmt.Errorf("%s spec.mode must be stateless, stateful, or both", res.ID())
		}
		if defaultString(spec.Mode, "stateless") != "stateless" {
			if spec.AddressPool.Start == "" || spec.AddressPool.End == "" {
				return fmt.Errorf("%s spec.addressPool.start and spec.addressPool.end are required for stateful modes", res.ID())
			}
			if err := validateIPv6AddressPair(spec.AddressPool.Start, spec.AddressPool.End); err != nil {
				return fmt.Errorf("%s spec.addressPool: %w", res.ID(), err)
			}
		}
		for i, server := range append(append([]string{}, spec.DNSServers...), spec.SNTPServers...) {
			if strings.ContainsAny(server, "\n\r") {
				return fmt.Errorf("%s DNS/SNTP server entry %d contains newline", res.ID(), i)
			}
			if strings.HasPrefix(strings.TrimSpace(server), "${") {
				return fmt.Errorf("%s DNS/SNTP server status expressions were removed; use dnsServerFrom or sntpServerFrom", res.ID())
			}
			addr, err := netip.ParseAddr(server)
			if err != nil || !addr.Is6() {
				return fmt.Errorf("%s DNS/SNTP server entry %q must be IPv6 or a status reference", res.ID(), server)
			}
		}
	case "DNSZone":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DNSZoneSpec()
		if err != nil {
			return err
		}
		if spec.Zone == "" {
			return fmt.Errorf("%s spec.zone is required", res.ID())
		}
		for i, record := range spec.Records {
			if record.Hostname == "" {
				return fmt.Errorf("%s spec.records[%d].hostname is required", res.ID(), i)
			}
			if strings.ContainsAny(record.Hostname, " \t\n,") {
				return fmt.Errorf("%s spec.records[%d].hostname is invalid", res.ID(), i)
			}
			if record.IPv4 != "" {
				addr, err := netip.ParseAddr(record.IPv4)
				if err != nil || !addr.Is4() {
					return fmt.Errorf("%s spec.records[%d].ipv4 must be an IPv4 address", res.ID(), i)
				}
			}
			if strings.TrimSpace(record.IPv4Source.Field) != "" {
				return fmt.Errorf("%s spec.records[%d].ipv4Source was removed; use ipv4From", res.ID(), i)
			}
			if record.IPv4From.Resource != "" && record.IPv4From.Field == "" {
				return fmt.Errorf("%s spec.records[%d].ipv4From.field is required", res.ID(), i)
			}
			if record.IPv6 != "" {
				addr, err := netip.ParseAddr(record.IPv6)
				if err != nil || !addr.Is6() {
					return fmt.Errorf("%s spec.records[%d].ipv6 must be an IPv6 address", res.ID(), i)
				}
			}
			if strings.TrimSpace(record.IPv6Source.Field) != "" {
				return fmt.Errorf("%s spec.records[%d].ipv6Source was removed; use ipv6From", res.ID(), i)
			}
			if record.IPv6From.Resource != "" && record.IPv6From.Field == "" {
				return fmt.Errorf("%s spec.records[%d].ipv6From.field is required", res.ID(), i)
			}
		}
	case "DNSResolver":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DNSResolverSpec()
		if err != nil {
			return err
		}
		if err := dnsresolver.Validate(spec); err != nil {
			return fmt.Errorf("%s: %w", res.ID(), err)
		}
		for i, listen := range spec.Listen {
			if len(listen.AddressSources) > 0 {
				return fmt.Errorf("%s spec.listen[%d].addressSources was removed; use addressFrom", res.ID(), i)
			}
			for _, sourceName := range listen.Sources {
				if !dnsSourceExists(spec.Sources, sourceName) {
					return fmt.Errorf("%s spec.listen[%d].sources references missing source %q", res.ID(), i, sourceName)
				}
			}
		}
	case "TrafficFlowLog":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.TrafficFlowLogSpec()
		if err != nil {
			return err
		}
		if spec.Enabled && strings.TrimSpace(spec.Path) == "" {
			return fmt.Errorf("%s spec.path is required when enabled is true", res.ID())
		}
		switch spec.Source {
		case "", "conntrack":
		default:
			return fmt.Errorf("%s spec.source must be conntrack", res.ID())
		}
	case "DHCPv4Server":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4ServerSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Role, "server") {
		case "server", "transit":
		default:
			return fmt.Errorf("%s spec.role must be server or transit", res.ID())
		}
		switch spec.Server {
		case "", "dnsmasq", "kea", "dhcpd":
		default:
			return fmt.Errorf("%s spec.server must be dnsmasq, kea, or dhcpd", res.ID())
		}
		switch spec.DNS.UpstreamSource {
		case "", "dhcpv4", "static", "system", "none":
		default:
			return fmt.Errorf("%s spec.dns.upstreamSource must be dhcpv4, static, system, or none", res.ID())
		}
		if spec.DNS.UpstreamSource == "dhcpv4" && spec.DNS.UpstreamInterface == "" {
			return fmt.Errorf("%s spec.dns.upstreamInterface is required when dns.upstreamSource is dhcpv4", res.ID())
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
		if spec.Interface != "" {
			if spec.AddressPool.Start == "" || spec.AddressPool.End == "" {
				return fmt.Errorf("%s spec.addressPool.start and spec.addressPool.end are required when spec.interface is set", res.ID())
			}
			if err := validateIPv4AddressPair(spec.AddressPool.Start, spec.AddressPool.End); err != nil {
				return fmt.Errorf("%s spec.addressPool: %w", res.ID(), err)
			}
			if spec.Gateway != "" {
				addr, err := netip.ParseAddr(spec.Gateway)
				if err != nil || !addr.Is4() {
					return fmt.Errorf("%s spec.gateway must be an IPv4 address", res.ID())
				}
			}
			for _, server := range append(append([]string{}, spec.DNSServers...), spec.NTPServers...) {
				addr, err := netip.ParseAddr(server)
				if err != nil || !addr.Is4() {
					return fmt.Errorf("%s dnsServers/ntpServers entries must be IPv4 addresses", res.ID())
				}
			}
			for i, option := range spec.Options {
				if err := validateDHCPv4Option(option); err != nil {
					return fmt.Errorf("%s spec.options[%d]: %w", res.ID(), i, err)
				}
			}
		}
	case "DHCPv4Scope":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4ScopeSpec()
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
		case "dhcpv4":
			if spec.DNSInterface == "" {
				return fmt.Errorf("%s spec.dnsInterface is required when dnsSource is dhcpv4", res.ID())
			}
		case "static":
			if len(spec.DNSServers) == 0 {
				return fmt.Errorf("%s spec.dnsServers is required when dnsSource is static", res.ID())
			}
		case "self", "none":
		default:
			return fmt.Errorf("%s spec.dnsSource must be dhcpv4, static, self, or none", res.ID())
		}
		for _, dns := range spec.DNSServers {
			addr, err := netip.ParseAddr(dns)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("%s spec.dnsServers entries must be IPv4 addresses", res.ID())
			}
		}
	case "DHCPv4Reservation":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4ReservationSpec()
		if err != nil {
			return err
		}
		if spec.Scope == "" && spec.Server == "" {
			return fmt.Errorf("%s spec.scope or spec.server is required", res.ID())
		}
		if spec.MACAddress == "" {
			return fmt.Errorf("%s spec.macAddress is required", res.ID())
		}
		if _, err := net.ParseMAC(spec.MACAddress); err != nil {
			return fmt.Errorf("%s spec.macAddress must be a MAC address", res.ID())
		}
		if spec.IPAddress == "" {
			return fmt.Errorf("%s spec.ipAddress is required", res.ID())
		}
		if addr, err := netip.ParseAddr(spec.IPAddress); err != nil || !addr.Is4() {
			return fmt.Errorf("%s spec.ipAddress must be an IPv4 address", res.ID())
		}
		if spec.Hostname != "" && strings.ContainsAny(spec.Hostname, " \t\n,") {
			return fmt.Errorf("%s spec.hostname must not contain whitespace or commas", res.ID())
		}
		if strings.Contains(spec.LeaseTime, ",") {
			return fmt.Errorf("%s spec.leaseTime must not contain commas", res.ID())
		}
		for i, option := range spec.Options {
			if err := validateDHCPv4Option(option); err != nil {
				return fmt.Errorf("%s spec.options[%d]: %w", res.ID(), i, err)
			}
		}
	case "DHCPv4Relay":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv4RelaySpec()
		if err != nil {
			return err
		}
		if len(spec.Interfaces) == 0 {
			return fmt.Errorf("%s spec.interfaces is required", res.ID())
		}
		if spec.Upstream == "" {
			return fmt.Errorf("%s spec.upstream is required", res.ID())
		}
		if addr, err := netip.ParseAddr(spec.Upstream); err != nil || !addr.Is4() {
			return fmt.Errorf("%s spec.upstream must be an IPv4 address", res.ID())
		}
	case "DHCPv6Scope":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DHCPv6ScopeSpec()
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
		if spec.AFTRSource != "" {
			return fmt.Errorf("%s spec.aftrSource was removed; use spec.aftrFrom", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		if spec.AFTRFrom.Resource == "" && spec.AFTRFQDN == "" && spec.AFTRIPv6 == "" && spec.RemoteAddress == "" {
			return fmt.Errorf("%s spec.aftrFrom, spec.aftrFQDN, spec.aftrIPv6, or spec.remoteAddress is required", res.ID())
		}
		if spec.AFTRFQDN != "" && strings.ContainsAny(spec.AFTRFQDN, " \t\n/") {
			return fmt.Errorf("%s spec.aftrFQDN contains invalid whitespace or slash", res.ID())
		}
		if spec.AFTRIPv6 != "" {
			addr, err := netip.ParseAddr(spec.AFTRIPv6)
			if err != nil || !addr.Is6() {
				return fmt.Errorf("%s spec.aftrIPv6 must be an IPv6 address", res.ID())
			}
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
				case "system", "static", "dhcpv4", "dhcpv6":
				default:
					return fmt.Errorf("%s spec.values[%d].when.dnsResolve.upstreamSource must be system, static, dhcpv4, or dhcpv6", res.ID(), i)
				}
				for _, server := range value.When.DNSResolve.UpstreamServers {
					addr, err := netip.ParseAddr(server)
					if err != nil || (!addr.Is4() && !addr.Is6()) {
						return fmt.Errorf("%s spec.values[%d].when.dnsResolve.upstreamServers contains invalid address %q", res.ID(), i, server)
					}
				}
			}
			if value.When.DHCPv6PrefixDelegation.UnavailableFor != "" {
				if _, err := time.ParseDuration(value.When.DHCPv6PrefixDelegation.UnavailableFor); err != nil {
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
		switch spec.Daemon {
		case "", "routerd-healthcheck":
		default:
			return fmt.Errorf("%s spec.daemon must be routerd-healthcheck", res.ID())
		}
		switch defaultString(spec.Type, "ping") {
		case "ping":
		default:
			return fmt.Errorf("%s spec.type must be ping", res.ID())
		}
		switch spec.Protocol {
		case "", "icmp", "tcp", "dns", "http":
		default:
			return fmt.Errorf("%s spec.protocol must be icmp, tcp, dns, or http", res.ID())
		}
		if spec.Port < 0 || spec.Port > 65535 {
			return fmt.Errorf("%s spec.port must be within 0-65535", res.ID())
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
		interval := defaultString(spec.Interval, "30s")
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
		for field, value := range map[string]string{"via": spec.Via, "sourceAddress": spec.SourceAddress} {
			if value == "" || strings.Contains(value, "${") {
				continue
			}
			addr, err := netip.ParseAddr(value)
			if err != nil {
				return fmt.Errorf("%s spec.%s must be an IP address or status path expression", res.ID(), field)
			}
			if spec.AddressFamily == "ipv4" && !addr.Is4() {
				return fmt.Errorf("%s spec.%s must be IPv4 when addressFamily is ipv4", res.ID(), field)
			}
			if spec.AddressFamily == "ipv6" && !addr.Is6() {
				return fmt.Errorf("%s spec.%s must be IPv6 when addressFamily is ipv6", res.ID(), field)
			}
		}
	case "EgressRoutePolicy":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.EgressRoutePolicySpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Family, "ipv4") {
		case "ipv4", "ipv6":
		default:
			return fmt.Errorf("%s spec.family must be ipv4 or ipv6", res.ID())
		}
		switch defaultString(spec.Selection, "highest-weight-ready") {
		case "highest-weight-ready", "weighted-ecmp":
		default:
			return fmt.Errorf("%s spec.selection must be highest-weight-ready or weighted-ecmp", res.ID())
		}
		for _, cidr := range spec.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil {
				return fmt.Errorf("%s spec.destinationCIDRs entries must be valid prefixes", res.ID())
			}
			switch defaultString(spec.Family, "ipv4") {
			case "ipv4":
				if !prefix.Addr().Is4() {
					return fmt.Errorf("%s spec.destinationCIDRs entries must be IPv4 prefixes when family is ipv4", res.ID())
				}
			case "ipv6":
				if !prefix.Addr().Is6() {
					return fmt.Errorf("%s spec.destinationCIDRs entries must be IPv6 prefixes when family is ipv6", res.ID())
				}
			}
		}
		if spec.Hysteresis != "" {
			if _, err := time.ParseDuration(spec.Hysteresis); err != nil {
				return fmt.Errorf("%s spec.hysteresis is invalid: %w", res.ID(), err)
			}
		}
		if len(spec.Candidates) == 0 {
			return fmt.Errorf("%s spec.candidates is required", res.ID())
		}
		for i, candidate := range spec.Candidates {
			if candidate.Name == "" && candidate.Source == "" {
				return fmt.Errorf("%s spec.candidates[%d] requires name or source", res.ID(), i)
			}
			if strings.Contains(candidate.Device, "${") {
				return fmt.Errorf("%s spec.candidates[%d].device status expressions were removed; use deviceFrom", res.ID(), i)
			}
			if strings.Contains(candidate.Gateway, "${") {
				return fmt.Errorf("%s spec.candidates[%d].gateway status expressions were removed; use gatewayFrom", res.ID(), i)
			}
			if len(candidate.ReadyWhen) > 0 {
				return fmt.Errorf("%s spec.candidates[%d].ready_when was removed; use dependsOn", res.ID(), i)
			}
			if candidate.Weight < 0 {
				return fmt.Errorf("%s spec.candidates[%d].weight must be non-negative", res.ID(), i)
			}
			source := defaultString(candidate.GatewaySource, "none")
			switch source {
			case "none":
				if candidate.Gateway != "" {
					return fmt.Errorf("%s spec.candidates[%d].gateway is only valid when gatewaySource is static", res.ID(), i)
				}
			case "static":
				if candidate.Gateway == "" {
					return fmt.Errorf("%s spec.candidates[%d].gateway is required when gatewaySource is static", res.ID(), i)
				}
				addr, err := netip.ParseAddr(candidate.Gateway)
				if err != nil {
					return fmt.Errorf("%s spec.candidates[%d].gateway must be an IP address", res.ID(), i)
				}
				if defaultString(spec.Family, "ipv4") == "ipv4" && !addr.Is4() {
					return fmt.Errorf("%s spec.candidates[%d].gateway must be an IPv4 address when family is ipv4", res.ID(), i)
				}
				if defaultString(spec.Family, "ipv4") == "ipv6" && !addr.Is6() {
					return fmt.Errorf("%s spec.candidates[%d].gateway must be an IPv6 address when family is ipv6", res.ID(), i)
				}
			case "dhcpv4", "dhcpv6":
			default:
				return fmt.Errorf("%s spec.candidates[%d].gatewaySource must be static, dhcpv4, dhcpv6, or none", res.ID(), i)
			}
		}
	case "EventRule":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.EventRuleSpec()
		if err != nil {
			return err
		}
		switch spec.Pattern.Operator {
		case "all_of", "any_of", "sequence", "window", "absence", "throttle", "debounce", "count":
		default:
			return fmt.Errorf("%s spec.pattern.operator must be one of all_of, any_of, sequence, window, absence, throttle, debounce, count", res.ID())
		}
		if spec.Pattern.Topic == "" && len(spec.Pattern.Topics) == 0 {
			if spec.Pattern.Trigger == "" && spec.Pattern.Expected == "" {
				return fmt.Errorf("%s spec.pattern.topic, spec.pattern.topics, spec.pattern.trigger, or spec.pattern.expected is required", res.ID())
			}
		}
		for field, value := range map[string]string{
			"duration": spec.Pattern.Duration,
			"window":   spec.Pattern.Window,
			"quiet":    spec.Pattern.Quiet,
			"interval": spec.Pattern.Interval,
		} {
			if value != "" {
				if _, err := time.ParseDuration(value); err != nil {
					return fmt.Errorf("%s spec.pattern.%s is invalid: %w", res.ID(), field, err)
				}
			}
		}
		if spec.Pattern.Threshold < 0 {
			return fmt.Errorf("%s spec.pattern.threshold must be non-negative", res.ID())
		}
		if spec.Pattern.Rate < 0 {
			return fmt.Errorf("%s spec.pattern.rate must be non-negative", res.ID())
		}
		if spec.Pattern.CorrelateBy != "" && !validEventRuleCorrelation(spec.Pattern.CorrelateBy) {
			return fmt.Errorf("%s spec.pattern.correlate_by must be attributes.<key>, resource.{name,kind,apiVersion}, or daemon.{instance,kind}", res.ID())
		}
		if spec.Emit.Topic == "" {
			return fmt.Errorf("%s spec.emit.topic is required", res.ID())
		}
	case "DerivedEvent":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.DerivedEventSpec()
		if err != nil {
			return err
		}
		if spec.Topic == "" {
			return fmt.Errorf("%s spec.topic is required", res.ID())
		}
		if len(spec.Inputs) == 0 {
			return fmt.Errorf("%s spec.inputs is required", res.ID())
		}
		switch defaultString(spec.EmitWhen, "all_true") {
		case "all_true", "any_true":
		default:
			return fmt.Errorf("%s spec.emitWhen must be all_true or any_true", res.ID())
		}
		switch defaultString(spec.RetractWhen, "any_false") {
		case "any_false", "all_false":
		default:
			return fmt.Errorf("%s spec.retractWhen must be any_false or all_false", res.ID())
		}
		if spec.Hysteresis != "" {
			if _, err := time.ParseDuration(spec.Hysteresis); err != nil {
				return fmt.Errorf("%s spec.hysteresis is invalid: %w", res.ID(), err)
			}
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
		for _, cidr := range spec.ExcludeDestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.excludeDestinationCIDRs entries must be IPv4 prefixes", res.ID())
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
				case "dhcpv4":
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
					return fmt.Errorf("%s spec.candidates[%d].gatewaySource must be none, dhcpv4, or static", res.ID(), i)
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
	case "NAT44Rule":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.NAT44RuleSpec()
		if err != nil {
			return err
		}
		switch spec.Type {
		case "masquerade", "snat":
		default:
			return fmt.Errorf("%s spec.type must be masquerade or snat", res.ID())
		}
		if spec.EgressInterface == "" && spec.EgressPolicyRef == "" {
			return fmt.Errorf("%s spec.egressInterface or spec.egressPolicyRef is required", res.ID())
		}
		if len(spec.SourceRanges) == 0 {
			return fmt.Errorf("%s spec.sourceRanges is required", res.ID())
		}
		for _, cidr := range spec.SourceRanges {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.sourceRanges entries must be IPv4 prefixes", res.ID())
			}
		}
		if spec.Type == "snat" {
			addr, err := netip.ParseAddr(spec.SNATAddress)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("%s spec.snatAddress must be an IPv4 address when type is snat", res.ID())
			}
		}
		if spec.Type == "masquerade" && spec.SNATAddress != "" {
			return fmt.Errorf("%s spec.snatAddress is only valid when type is snat", res.ID())
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
		for _, cidr := range spec.ExcludeDestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.excludeDestinationCIDRs entries must be IPv4 prefixes", res.ID())
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
		for _, cidr := range spec.ExcludeDestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.excludeDestinationCIDRs entries must be IPv4 prefixes", res.ID())
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
				return fmt.Errorf("%s spec.mtu.value is only valid when mtu.source is static or probe", res.ID())
			}
		case "static":
			if spec.MTU.Value < 1280 || spec.MTU.Value > 65535 {
				return fmt.Errorf("%s spec.mtu.value must be within 1280-65535", res.ID())
			}
		case "probe":
			probe := spec.MTU.Probe
			switch defaultString(probe.Family, "ipv4") {
			case "ipv4", "ipv6":
			default:
				return fmt.Errorf("%s spec.mtu.probe.family must be ipv4 or ipv6", res.ID())
			}
			if probe.Min != 0 && (probe.Min < 1280 || probe.Min > 65535) {
				return fmt.Errorf("%s spec.mtu.probe.min must be within 1280-65535", res.ID())
			}
			if probe.Max != 0 && (probe.Max < 1280 || probe.Max > 65535) {
				return fmt.Errorf("%s spec.mtu.probe.max must be within 1280-65535", res.ID())
			}
			if probe.Fallback != 0 && (probe.Fallback < 1280 || probe.Fallback > 65535) {
				return fmt.Errorf("%s spec.mtu.probe.fallback must be within 1280-65535", res.ID())
			}
			minMTU := probe.Min
			if minMTU == 0 {
				minMTU = 1280
			}
			maxMTU := probe.Max
			if maxMTU == 0 {
				maxMTU = spec.MTU.Value
			}
			if maxMTU == 0 {
				maxMTU = 1500
			}
			if maxMTU < minMTU {
				return fmt.Errorf("%s spec.mtu.probe.max must be greater than or equal to spec.mtu.probe.min", res.ID())
			}
			if probe.Interval != "" {
				if _, err := time.ParseDuration(probe.Interval); err != nil {
					return fmt.Errorf("%s spec.mtu.probe.interval is invalid: %w", res.ID(), err)
				}
			}
			if probe.Timeout != "" {
				if _, err := time.ParseDuration(probe.Timeout); err != nil {
					return fmt.Errorf("%s spec.mtu.probe.timeout is invalid: %w", res.ID(), err)
				}
			}
			for i, target := range probe.Targets {
				if strings.TrimSpace(target) == "" || strings.ContainsAny(target, " \t\n\r") {
					return fmt.Errorf("%s spec.mtu.probe.targets[%d] must be a single address or hostname", res.ID(), i)
				}
			}
		default:
			return fmt.Errorf("%s spec.mtu.source must be minInterface, static, or probe", res.ID())
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
	case "FirewallZone":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.FirewallZoneSpec()
		if err != nil {
			return err
		}
		switch spec.Role {
		case "untrust", "trust", "mgmt":
		default:
			return fmt.Errorf("%s spec.role must be untrust, trust, or mgmt", res.ID())
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
		if _, err := res.FirewallPolicySpec(); err != nil {
			return err
		}
	case "FirewallLog":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.FirewallLogSpec()
		if err != nil {
			return err
		}
		if spec.Enabled && strings.TrimSpace(spec.Path) == "" {
			return fmt.Errorf("%s spec.path is required when enabled is true", res.ID())
		}
		if spec.NFLogGroup < 0 || spec.NFLogGroup > 65535 {
			return fmt.Errorf("%s spec.nflogGroup must be between 0 and 65535", res.ID())
		}
	case "FirewallRule":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.FirewallRuleSpec()
		if err != nil {
			return err
		}
		if spec.FromZone == "" {
			return fmt.Errorf("%s spec.fromZone is required", res.ID())
		}
		if spec.ToZone == "" {
			return fmt.Errorf("%s spec.toZone is required", res.ID())
		}
		switch spec.Action {
		case "accept", "drop", "reject":
		default:
			return fmt.Errorf("%s spec.action must be accept, drop, or reject", res.ID())
		}
		switch spec.Protocol {
		case "", "tcp", "udp", "icmp", "icmpv6", "ipv6-icmp", "ipip":
		default:
			return fmt.Errorf("%s spec.protocol must be tcp, udp, icmp, icmpv6, ipv6-icmp, or ipip", res.ID())
		}
		if spec.Port != 0 && spec.Protocol != "tcp" && spec.Protocol != "udp" {
			return fmt.Errorf("%s spec.port requires protocol tcp or udp", res.ID())
		}
		if spec.Port < 0 || spec.Port > 65535 {
			return fmt.Errorf("%s spec.port must be between 1 and 65535", res.ID())
		}
		for i, cidr := range spec.SourceCIDRs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("%s spec.srcCIDRs[%d] is invalid: %w", res.ID(), i, err)
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
	case "IPv4Route":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPv4RouteSpec()
		if err != nil {
			return err
		}
		if spec.Destination == "" {
			return fmt.Errorf("%s spec.destination is required", res.ID())
		}
		if _, err := netip.ParsePrefix(spec.Destination); err != nil {
			return fmt.Errorf("%s spec.destination is invalid: %w", res.ID(), err)
		}
		if strings.Contains(spec.Device, "${") {
			return fmt.Errorf("%s spec.device status expressions were removed; use deviceFrom", res.ID())
		}
		if strings.Contains(spec.Gateway, "${") {
			return fmt.Errorf("%s spec.gateway status expressions were removed; use gatewayFrom", res.ID())
		}
		if len(spec.ReadyWhen) > 0 {
			return fmt.Errorf("%s spec.ready_when was removed; use spec.dependsOn", res.ID())
		}
		if spec.Gateway != "" {
			addr, err := netip.ParseAddr(spec.Gateway)
			if err != nil || !addr.Is4() {
				return fmt.Errorf("%s spec.gateway must be an IPv4 address", res.ID())
			}
		}
	default:
		return fmt.Errorf("unsupported resource kind %s in %s", res.Kind, res.ID())
	}
	return nil
}

func validIAID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		_, err := strconv.ParseUint(value[2:], 16, 32)
		return err == nil
	}
	if len(value) == 8 && validHex(value) {
		_, err := strconv.ParseUint(value, 16, 32)
		return err == nil
	}
	_, err := strconv.ParseUint(value, 10, 32)
	return err == nil
}

func validHex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range strings.ToLower(value) {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validEventRuleCorrelation(value string) bool {
	switch value {
	case "resource.name", "resource.kind", "resource.apiVersion", "daemon.instance", "daemon.kind":
		return true
	default:
		return strings.HasPrefix(value, "attributes.") && strings.TrimPrefix(value, "attributes.") != ""
	}
}

func validateIPv4AddressPair(start, end string) error {
	startAddr, err := netip.ParseAddr(start)
	if err != nil || !startAddr.Is4() {
		return fmt.Errorf("start must be an IPv4 address")
	}
	endAddr, err := netip.ParseAddr(end)
	if err != nil || !endAddr.Is4() {
		return fmt.Errorf("end must be an IPv4 address")
	}
	if startAddr.Compare(endAddr) > 0 {
		return fmt.Errorf("start must be less than or equal to end")
	}
	return nil
}

func validateIPv6AddressPair(start, end string) error {
	startAddr, err := netip.ParseAddr(start)
	if err != nil || !startAddr.Is6() {
		return fmt.Errorf("start must be an IPv6 address")
	}
	endAddr, err := netip.ParseAddr(end)
	if err != nil || !endAddr.Is6() {
		return fmt.Errorf("end must be an IPv6 address")
	}
	if startAddr.Compare(endAddr) > 0 {
		return fmt.Errorf("start must be less than or equal to end")
	}
	return nil
}

func validateDHCPv4Option(option api.DHCPv4OptionSpec) error {
	if option.Code == 0 && option.Name == "" {
		return fmt.Errorf("code or name is required")
	}
	if option.Code != 0 && option.Name != "" {
		return fmt.Errorf("code and name are mutually exclusive")
	}
	if option.Value == "" {
		return fmt.Errorf("value is required")
	}
	if strings.ContainsAny(option.Name, " \t\n,") || strings.ContainsAny(option.Value, "\n\r") {
		return fmt.Errorf("contains invalid whitespace or newline")
	}
	return nil
}

func interfaceRef(res api.Resource) (string, error) {
	switch res.Kind {
	case "IPv4StaticAddress":
		spec, err := res.IPv4StaticAddressSpec()
		return spec.Interface, err
	case "DHCPv4Address":
		spec, err := res.DHCPv4AddressSpec()
		return spec.Interface, err
	case "DHCPv4Lease":
		spec, err := res.DHCPv4LeaseSpec()
		return spec.Interface, err
	case "IPv4StaticRoute":
		spec, err := res.IPv4StaticRouteSpec()
		return spec.Interface, err
	case "IPv6StaticRoute":
		spec, err := res.IPv6StaticRouteSpec()
		return spec.Interface, err
	case "DHCPv4Server", "DHCPv6Server":
		return "", nil
	case "DHCPv4Scope":
		spec, err := res.DHCPv4ScopeSpec()
		return spec.Interface, err
	case "DHCPv6Address":
		spec, err := res.DHCPv6AddressSpec()
		return spec.Interface, err
	case "IPv6RAAddress":
		spec, err := res.IPv6RAAddressSpec()
		return spec.Interface, err
	case "DHCPv6PrefixDelegation":
		spec, err := res.DHCPv6PrefixDelegationSpec()
		return spec.Interface, err
	case "IPv6DelegatedAddress":
		spec, err := res.IPv6DelegatedAddressSpec()
		return spec.Interface, err
	case "DHCPv6Scope":
		return "", nil
	case "DSLiteTunnel":
		spec, err := res.DSLiteTunnelSpec()
		return spec.Interface, err
	case "PPPoEInterface":
		spec, err := res.PPPoEInterfaceSpec()
		return spec.Interface, err
	case "PPPoESession":
		spec, err := res.PPPoESessionSpec()
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

func defaultPackageManager(osName string) string {
	switch osName {
	case "ubuntu", "debian":
		return "apt"
	case "fedora", "rhel", "rocky", "almalinux":
		return "dnf"
	case "nixos":
		return "nix"
	case "freebsd":
		return "pkg"
	default:
		return ""
	}
}

func refName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func dnsSourceExists(sources []api.DNSResolverSourceSpec, name string) bool {
	for i, source := range sources {
		sourceName := source.Name
		if sourceName == "" {
			sourceName = fmt.Sprintf("source-%d", i)
		}
		if sourceName == name {
			return true
		}
	}
	return false
}

func isStatusExpression(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") && strings.Contains(value, ".status.")
}

func parseRetentionDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

func stringInSlice(value string, values []string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
