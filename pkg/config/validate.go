// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/dnsresolver"
	"routerd/pkg/healthcheck"
	"routerd/pkg/platform"
)

func Validate(router *api.Router) error {
	return ValidateForOS(router, platform.CurrentOS())
}

func ValidateForOS(router *api.Router, targetOS platform.OS) error {
	if router == nil {
		return fmt.Errorf("router is nil")
	}
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
	dhcp4Reservations := map[string]bool{}
	dhcp6Servers := map[string]bool{}
	dhcp6ServerSpecs := map[string]api.DHCPv6ServerSpec{}
	dhcp6Scopes := map[string]bool{}
	ipv6RAs := map[string]bool{}
	prefixDelegations := map[string]bool{}
	delegatedAddresses := map[string]bool{}
	delegatedAddressInterfaces := map[string]string{}
	selfAddressPolicies := map[string]bool{}
	dsliteTunnels := map[string]bool{}
	routeSets := map[string]bool{}
	healthChecks := map[string]bool{}
	bgpRouters := map[string]bool{}
	vrfs := map[string]bool{}
	zones := map[string]bool{}
	ipAddressSets := map[string]bool{}
	udpListenPorts := map[int]string{}
	staticByInterfaceAddress := map[string]string{}
	staticIPv4ByName := map[string]struct {
		id      string
		iface   string
		address string
	}{}
	vrrpByInterfaceVRID := map[string]string{}
	protectedInterfaces := map[string]bool{}
	for _, name := range router.Spec.Apply.ProtectedInterfaces {
		protectedInterfaces[name] = true
	}
	dhcp6AddressByInterface := map[string]struct {
		id     string
		client string
	}{}
	externalPDByInterface := map[string]struct {
		id     string
		client string
	}{}
	for _, res := range router.Spec.Resources {
		if err := validateResource(res, targetOS); err != nil {
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
			spec, err := res.WireGuardInterfaceSpec()
			if err != nil {
				return err
			}
			if spec.ListenPort != 0 {
				if existing := udpListenPorts[spec.ListenPort]; existing != "" {
					return fmt.Errorf("%s spec.listenPort %d conflicts with %s", res.ID(), spec.ListenPort, existing)
				}
				udpListenPorts[spec.ListenPort] = res.ID()
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "TailscaleNode" {
			spec, err := res.TailscaleNodeSpec()
			if err != nil {
				return err
			}
			if defaultString(spec.State, "present") != "absent" {
				if existing := udpListenPorts[41641]; existing != "" {
					return fmt.Errorf("%s reserves Tailscale UDP port 41641 which conflicts with %s", res.ID(), existing)
				}
				udpListenPorts[41641] = res.ID()
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VRF" {
			interfaces[res.Metadata.Name] = true
			vrfs[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VXLANTunnel" {
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && (res.Kind == "PPPoEInterface" || res.Kind == "PPPoESession") {
			interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.SystemAPIVersion && res.Kind == "NetworkAdoption" {
			spec, err := res.NetworkAdoptionSpec()
			if err != nil {
				return err
			}
			if defaultString(spec.State, "present") != "absent" && spec.Interface != "" && protectedInterfaces[spec.Interface] {
				return fmt.Errorf("%s must not adopt protected interface %q; remove it from spec.reconcile.protectedInterfaces first", res.ID(), spec.Interface)
			}
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
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6RouterAdvertisement" {
			ipv6RAs[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv4Scope" {
			spec, err := res.DHCPv4ScopeSpec()
			if err != nil {
				return err
			}
			dhcp4Scopes[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv4Reservation" {
			dhcp4Reservations[res.Metadata.Name] = true
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
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPAddressSet" {
			ipAddressSets[res.Metadata.Name] = true
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
			staticIPv4ByName[res.Metadata.Name] = struct {
				id      string
				iface   string
				address string
			}{id: res.ID(), iface: spec.Interface, address: prefix.Masked().String()}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VirtualIPv4Address" {
			spec, err := res.VirtualIPv4AddressSpec()
			if err != nil {
				return err
			}
			if defaultString(spec.Mode, "static") == "vrrp" {
				key := res.Kind + "|" + spec.Interface + "|" + strconv.Itoa(spec.VRRP.VirtualRouterID)
				if existing := vrrpByInterfaceVRID[key]; existing != "" {
					return fmt.Errorf("%s spec.vrrp.virtualRouterID conflicts with %s on interface %q", res.ID(), existing, spec.Interface)
				}
				vrrpByInterfaceVRID[key] = res.ID()
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VirtualIPv6Address" {
			spec, err := res.VirtualIPv6AddressSpec()
			if err != nil {
				return err
			}
			if defaultString(spec.Mode, "static") == "vrrp" {
				key := res.Kind + "|" + spec.Interface + "|" + strconv.Itoa(spec.VRRP.VirtualRouterID)
				if existing := vrrpByInterfaceVRID[key]; existing != "" {
					return fmt.Errorf("%s spec.vrrp.virtualRouterID conflicts with %s on interface %q", res.ID(), existing, spec.Interface)
				}
				vrrpByInterfaceVRID[key] = res.ID()
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "BGPRouter" {
			bgpRouters[res.Metadata.Name] = true
		}
		if res.APIVersion == api.FirewallAPIVersion && res.Kind == "FirewallZone" {
			zones[res.Metadata.Name] = true
		}
	}
	if err := validateListenPortCollisions(router); err != nil {
		return err
	}
	if err := validateBGPRouterInstances(router, vrfs); err != nil {
		return err
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "VirtualIPv4Address" {
			continue
		}
		spec, err := res.VirtualIPv4AddressSpec()
		if err != nil {
			return err
		}
		if address := strings.TrimSpace(spec.Address); address != "" {
			if prefix, err := netip.ParsePrefix(address); err == nil {
				if existing := staticByInterfaceAddress[spec.Interface+"|"+prefix.Masked().String()]; existing != "" {
					return fmt.Errorf("%s spec.address conflicts with IPv4StaticAddress %s on interface %q", res.ID(), existing, spec.Interface)
				}
			}
		}
		if kind, name, ok := strings.Cut(strings.TrimSpace(spec.AddressFrom.Resource), "/"); ok && kind == "IPv4StaticAddress" {
			if source, ok := staticIPv4ByName[name]; ok && source.iface == spec.Interface {
				return fmt.Errorf("%s spec.addressFrom conflicts with %s on interface %q; do not manage %s as both IPv4StaticAddress and VirtualIPv4Address", res.ID(), source.id, spec.Interface, source.address)
			}
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
		case "IPv4StaticAddress", "VirtualIPv4Address", "VirtualIPv6Address", "DHCPv4Lease", "IPv4StaticRoute", "IPv6StaticRoute", "DHCPv4Scope", "DHCPv6Address", "IPv6RAAddress", "DHCPv6PrefixDelegation", "IPv6DelegatedAddress", "DSLiteTunnel", "PPPoEInterface", "PPPoESession":
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
		if res.Kind == "ClusterNetworkRoute" {
			spec, err := res.ClusterNetworkRouteSpec()
			if err != nil {
				return err
			}
			for i, via := range spec.Via {
				name := strings.TrimSpace(via.Interface)
				if name == "" {
					return fmt.Errorf("%s spec.via[%d].interface is required", res.ID(), i)
				}
				if !interfaces[name] {
					return fmt.Errorf("%s spec.via[%d].interface references missing Interface %q", res.ID(), i, name)
				}
			}
		}
		if res.Kind == "BGPPeer" {
			spec, err := res.BGPPeerSpec()
			if err != nil {
				return err
			}
			kind, name, _ := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
			if kind != "BGPRouter" || !bgpRouters[name] {
				return fmt.Errorf("%s spec.routerRef references missing BGPRouter %q", res.ID(), spec.RouterRef)
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
			if spec.IPv6RA.Enabled && !dhcp6Scopes[spec.IPv6RA.Scope] && !dhcp6Servers[spec.IPv6RA.Scope] && !ipv6RAs[spec.IPv6RA.Scope] {
				return fmt.Errorf("%s spec.ipv6RA.scope references missing DHCPv6Scope, DHCPv6Server, or IPv6RouterAdvertisement %q", res.ID(), spec.IPv6RA.Scope)
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
			if err := validateHealthCheckDerivedFwMark(router, res, spec); err != nil {
				return err
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
				case "Interface", "PPPoEInterface", "WireGuardInterface":
					if !interfaces[refName] && !dsliteTunnels[refName] {
						return fmt.Errorf("%s spec.interfaces[%d] references missing Interface, PPPoEInterface, WireGuardInterface, or DSLiteTunnel %q", res.ID(), i, refName)
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
		if res.Kind == "ClientPolicy" {
			spec, err := res.ClientPolicySpec()
			if err != nil {
				return err
			}
			for i, name := range spec.Interfaces {
				refKind, refName := splitFirewallInterfaceRef(name)
				if refKind != "Interface" {
					return fmt.Errorf("%s spec.interfaces[%d] must reference Interface, got %q", res.ID(), i, name)
				}
				if !interfaces[refName] {
					return fmt.Errorf("%s spec.interfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
			for i, entry := range spec.Classification {
				if entry.IPv4Reservation != "" && !dhcp4Reservations[entry.IPv4Reservation] {
					return fmt.Errorf("%s spec.classification[%d].ipv4Reservation references missing DHCPv4Reservation %q", res.ID(), i, entry.IPv4Reservation)
				}
			}
		}
		if res.Kind == "PortForward" {
			spec, err := res.PortForwardSpec()
			if err != nil {
				return err
			}
			if err := validateIngressInterfaceRefs(res.ID(), spec.Listen, spec.Hairpin, interfaces); err != nil {
				return err
			}
		}
		if res.Kind == "IngressService" {
			spec, err := res.IngressServiceSpec()
			if err != nil {
				return err
			}
			if err := validateIngressInterfaceRefs(res.ID(), spec.Listen, spec.Hairpin, interfaces); err != nil {
				return err
			}
		}
		if res.Kind == "LocalServiceRedirect" {
			spec, err := res.LocalServiceRedirectSpec()
			if err != nil {
				return err
			}
			if !interfaces[spec.Interface] {
				return fmt.Errorf("%s spec.interface references missing Interface %q", res.ID(), spec.Interface)
			}
			for i, rule := range spec.Rules {
				kind, name := splitResourceRef(rule.DestinationSetRef)
				if kind != "IPAddressSet" {
					return fmt.Errorf("%s spec.rules[%d].destinationSetRef must reference IPAddressSet, got %q", res.ID(), i, rule.DestinationSetRef)
				}
				if !ipAddressSets[name] {
					return fmt.Errorf("%s spec.rules[%d].destinationSetRef references missing IPAddressSet %q", res.ID(), i, rule.DestinationSetRef)
				}
			}
		}
		if res.Kind == "NAT44Rule" {
			spec, err := res.NAT44RuleSpec()
			if err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs, ipAddressSets); err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs, ipAddressSets); err != nil {
				return err
			}
		}
		if res.Kind == "IPv4PolicyRoute" {
			spec, err := res.IPv4PolicyRouteSpec()
			if err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs, ipAddressSets); err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs, ipAddressSets); err != nil {
				return err
			}
		}
		if res.Kind == "IPv4PolicyRouteSet" {
			spec, err := res.IPv4PolicyRouteSetSpec()
			if err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs, ipAddressSets); err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs, ipAddressSets); err != nil {
				return err
			}
		}
		if res.Kind == "FirewallRule" {
			spec, err := res.FirewallRuleSpec()
			if err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs, ipAddressSets); err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs, ipAddressSets); err != nil {
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

func validateResource(res api.Resource, targetOS platform.OS) error {
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
	case "Telemetry":
		if res.APIVersion != api.ObservabilityAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.ObservabilityAPIVersion)
		}
		spec, err := res.TelemetrySpec()
		if err != nil {
			return err
		}
		if strings.TrimSpace(spec.OTLP.Endpoint) == "" {
			return fmt.Errorf("%s spec.otlp.endpoint is required", res.ID())
		}
		for _, signal := range spec.Signals {
			switch signal {
			case "logs", "metrics", "traces":
			default:
				return fmt.Errorf("%s spec.signals must contain only logs, metrics, or traces", res.ID())
			}
		}
	case "ObservabilityPipeline":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.ObservabilityPipelineSpec()
		if err != nil {
			return err
		}
		if strings.TrimSpace(spec.OTLP.Endpoint) != "" {
			if _, err := url.ParseRequestURI(strings.TrimSpace(spec.OTLP.Endpoint)); err != nil {
				return fmt.Errorf("%s spec.otlp.endpoint is invalid: %w", res.ID(), err)
			}
		}
		for _, signal := range spec.Signals {
			switch signal {
			case "logs", "metrics", "traces":
			default:
				return fmt.Errorf("%s spec.signals must contain only logs, metrics, or traces", res.ID())
			}
		}
		if spec.Sampling.Rate < 0 || spec.Sampling.Rate > 1 {
			return fmt.Errorf("%s spec.sampling.rate must be between 0 and 1", res.ID())
		}
		for i, sink := range spec.Logs.Sinks {
			if err := validateObservabilitySink(res.ID(), i, sink); err != nil {
				return err
			}
		}
	case "RouterdCluster":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.RouterdClusterSpec()
		if err != nil {
			return err
		}
		if len(compactStrings(spec.Peers)) < 2 {
			return fmt.Errorf("%s spec.peers must contain at least 2 peers", res.ID())
		}
		ttl := 30 * time.Second
		if strings.TrimSpace(spec.LeaseTTL) != "" {
			var err error
			ttl, err = time.ParseDuration(spec.LeaseTTL)
			if err != nil {
				return fmt.Errorf("%s spec.leaseTTL is invalid: %w", res.ID(), err)
			}
		}
		if ttl < 5*time.Second || ttl > 10*time.Minute {
			return fmt.Errorf("%s spec.leaseTTL must be between 5s and 10m", res.ID())
		}
		if strings.TrimSpace(spec.LeasePath) == "" {
			return fmt.Errorf("%s spec.leasePath is required", res.ID())
		}
		if strings.ContainsAny(spec.LeasePath, "\n\r") || !strings.HasPrefix(strings.TrimSpace(spec.LeasePath), "/") {
			return fmt.Errorf("%s spec.leasePath must be an absolute path", res.ID())
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
	case "KernelModule":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.KernelModuleSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.State, "present") {
		case "present":
		default:
			return fmt.Errorf("%s spec.state must be present", res.ID())
		}
		if len(spec.Modules) == 0 {
			return fmt.Errorf("%s spec.modules is required", res.ID())
		}
		seen := map[string]bool{}
		for i, module := range spec.Modules {
			module = strings.TrimSpace(module)
			if module == "" {
				return fmt.Errorf("%s spec.modules[%d] is required", res.ID(), i)
			}
			if strings.ContainsAny(module, "/ \t\n") {
				return fmt.Errorf("%s spec.modules[%d] must be a module name, not a path or command", res.ID(), i)
			}
			if seen[module] {
				return fmt.Errorf("%s spec.modules[%d] duplicates %q", res.ID(), i, module)
			}
			seen[module] = true
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
			case "ubuntu", "debian", "alpine", "fedora", "rhel", "rocky", "almalinux", "nixos", "freebsd":
			default:
				return fmt.Errorf("%s spec.packages[%d].os must be ubuntu, debian, alpine, fedora, rhel, rocky, almalinux, nixos, or freebsd", res.ID(), i)
			}
			manager := defaultString(set.Manager, defaultPackageManager(set.OS))
			switch manager {
			case "apt", "apk", "dnf", "nix", "pkg":
			default:
				return fmt.Errorf("%s spec.packages[%d].manager must be apt, apk, dnf, nix, or pkg", res.ID(), i)
			}
			if expected := defaultPackageManager(set.OS); expected != "" && manager != expected {
				return fmt.Errorf("%s spec.packages[%d].manager must be %s for os %s", res.ID(), i, expected, set.OS)
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
		switch spec.Type {
		case "", "simple", "oneshot":
		default:
			return fmt.Errorf("%s spec.type must be simple or oneshot", res.ID())
		}
		for i, arg := range spec.ExecStart {
			if strings.TrimSpace(arg) == "" {
				return fmt.Errorf("%s spec.execStart[%d] must not be empty", res.ID(), i)
			}
			if strings.ContainsAny(arg, "\x00\n\r") {
				return fmt.Errorf("%s spec.execStart[%d] contains invalid characters", res.ID(), i)
			}
		}
		for i, path := range spec.EnvironmentFiles {
			if err := validateSystemdEnvironmentFilePath(path); err != nil {
				return fmt.Errorf("%s spec.environmentFiles[%d] %w", res.ID(), i, err)
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
		for i, group := range spec.SupplementaryGroups {
			if strings.TrimSpace(group) == "" {
				return fmt.Errorf("%s spec.supplementaryGroups[%d] must not be empty", res.ID(), i)
			}
			if strings.ContainsAny(group, " \t\x00\n\r") {
				return fmt.Errorf("%s spec.supplementaryGroups[%d] contains invalid characters", res.ID(), i)
			}
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
		case "systemd-timesyncd", "chrony", "ntpd":
		default:
			return fmt.Errorf("%s spec.provider must be systemd-timesyncd, chrony, or ntpd", res.ID())
		}
		switch defaultString(spec.Source, "static") {
		case "static", "auto", "dhcp", "dhcpv6":
		default:
			return fmt.Errorf("%s spec.source must be static, auto, dhcp, or dhcpv6", res.ID())
		}
		if defaultString(spec.Source, "static") == "static" && spec.Managed && len(spec.Servers) == 0 {
			return fmt.Errorf("%s spec.servers is required when spec.source is static", res.ID())
		}
		if spec.Managed && len(spec.Servers) == 0 && len(spec.ServerFrom) == 0 && len(spec.FallbackServers) == 0 {
			return fmt.Errorf("%s spec.servers, spec.serverFrom, or spec.fallbackServers is required when managed is true", res.ID())
		}
		for i, server := range append(append([]string{}, spec.Servers...), spec.FallbackServers...) {
			if strings.TrimSpace(server) == "" {
				return fmt.Errorf("%s NTP server entry %d must not be empty", res.ID(), i)
			}
			if strings.ContainsAny(server, " \t\n\r") {
				return fmt.Errorf("%s NTP server entry %d must be a single hostname or IP address", res.ID(), i)
			}
		}
		for i, source := range spec.ServerFrom {
			if strings.TrimSpace(source.Resource) == "" {
				return fmt.Errorf("%s spec.serverFrom[%d].resource is required", res.ID(), i)
			}
			if strings.TrimSpace(source.Field) == "" {
				return fmt.Errorf("%s spec.serverFrom[%d].field is required", res.ID(), i)
			}
		}
	case "NTPServer":
		if res.APIVersion != api.SystemAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.SystemAPIVersion)
		}
		spec, err := res.NTPServerSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.Provider, "chrony") {
		case "chrony", "ntpd":
		default:
			return fmt.Errorf("%s spec.provider must be chrony or ntpd", res.ID())
		}
		switch defaultString(spec.Source, "static") {
		case "static", "auto", "dhcp", "dhcpv6":
		default:
			return fmt.Errorf("%s spec.source must be static, auto, dhcp, or dhcpv6", res.ID())
		}
		if defaultString(spec.Source, "static") == "static" && spec.Managed && len(spec.Servers) == 0 {
			return fmt.Errorf("%s spec.servers is required when spec.source is static", res.ID())
		}
		if spec.Managed && len(spec.Servers) == 0 && len(spec.ServerFrom) == 0 && len(spec.FallbackServers) == 0 {
			return fmt.Errorf("%s spec.servers, spec.serverFrom, or spec.fallbackServers is required when managed is true", res.ID())
		}
		for i, server := range append(append([]string{}, spec.Servers...), spec.FallbackServers...) {
			if strings.TrimSpace(server) == "" {
				return fmt.Errorf("%s NTP server entry %d must not be empty", res.ID(), i)
			}
			if strings.ContainsAny(server, " \t\n\r") {
				return fmt.Errorf("%s NTP server entry %d must be a single hostname or IP address", res.ID(), i)
			}
		}
		for i, cidr := range spec.AllowCIDRs {
			if strings.TrimSpace(cidr) == "" {
				return fmt.Errorf("%s spec.allowCIDRs[%d] must not be empty", res.ID(), i)
			}
		}
		for i, source := range append(append([]api.StatusValueSourceSpec{}, spec.ServerFrom...), spec.ListenAddressFrom...) {
			if strings.TrimSpace(source.Resource) == "" {
				return fmt.Errorf("%s source reference %d resource is required", res.ID(), i)
			}
			if strings.TrimSpace(source.Field) == "" {
				return fmt.Errorf("%s source reference %d field is required", res.ID(), i)
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
		if spec.ListenAddressFrom.Resource != "" && spec.ListenAddressFrom.Field == "" {
			return fmt.Errorf("%s spec.listenAddressFrom.field is required", res.ID())
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
		if strings.ContainsAny(spec.PrivateKeyFile, "\n\r") {
			return fmt.Errorf("%s spec.privateKeyFile is invalid", res.ID())
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
		if strings.ContainsAny(spec.PresharedKeyFile, "\n\r") {
			return fmt.Errorf("%s spec.presharedKeyFile is invalid", res.ID())
		}
	case "TailscaleNode":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.TailscaleNodeSpec()
		if err != nil {
			return err
		}
		switch defaultString(spec.State, "present") {
		case "present", "absent":
		default:
			return fmt.Errorf("%s spec.state must be present or absent", res.ID())
		}
		if spec.AuthKey != "" && (spec.AuthKeyEnv != "" || spec.AuthKeyFile != "") {
			return fmt.Errorf("%s spec.authKey is mutually exclusive with spec.authKeyEnv and spec.authKeyFile", res.ID())
		}
		for field, value := range map[string]string{
			"hostname":    spec.Hostname,
			"loginServer": spec.LoginServer,
			"authKeyEnv":  spec.AuthKeyEnv,
			"authKeyFile": spec.AuthKeyFile,
			"operator":    spec.Operator,
			"binaryPath":  spec.BinaryPath,
		} {
			if strings.ContainsAny(value, "\x00\n\r") {
				return fmt.Errorf("%s spec.%s contains invalid characters", res.ID(), field)
			}
		}
		if spec.AuthKeyEnv != "" && !validEnvironmentName(spec.AuthKeyEnv) {
			return fmt.Errorf("%s spec.authKeyEnv must be an environment variable name", res.ID())
		}
		if spec.AuthKeyFile != "" {
			if err := validateSystemdEnvironmentFilePath(spec.AuthKeyFile); err != nil {
				return fmt.Errorf("%s spec.authKeyFile %w", res.ID(), err)
			}
		}
		for i, route := range spec.AdvertiseRoutes {
			if _, err := netip.ParsePrefix(route); err != nil {
				return fmt.Errorf("%s spec.advertiseRoutes[%d] must be an IP prefix", res.ID(), i)
			}
		}
		for i, tag := range spec.AdvertiseTags {
			if strings.TrimSpace(tag) == "" || strings.ContainsAny(tag, " \t\n\r\x00") {
				return fmt.Errorf("%s spec.advertiseTags[%d] must be a Tailscale tag", res.ID(), i)
			}
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
	case "VirtualIPv4Address":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.VirtualIPv4AddressSpec()
		if err != nil {
			return err
		}
		if strings.TrimSpace(spec.Interface) == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if strings.TrimSpace(spec.Address) == "" && strings.TrimSpace(spec.AddressFrom.Resource) == "" {
			return fmt.Errorf("%s spec.address or spec.addressFrom is required", res.ID())
		}
		if strings.TrimSpace(spec.Address) != "" && strings.TrimSpace(spec.AddressFrom.Resource) != "" {
			return fmt.Errorf("%s spec.address and spec.addressFrom are mutually exclusive", res.ID())
		}
		if strings.TrimSpace(spec.Address) != "" {
			prefix, err := netip.ParsePrefix(spec.Address)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.address must be an IPv4 prefix", res.ID())
			}
			if prefix.Bits() != 32 {
				return fmt.Errorf("%s spec.address must be an IPv4 /32 prefix", res.ID())
			}
		}
		if err := validateIngressAddressSource(res.ID(), "spec.addressFrom", spec.AddressFrom); err != nil {
			return err
		}
		if strings.TrimSpace(spec.Hostname) != "" {
			if err := validateFQDN(spec.Hostname); err != nil {
				return fmt.Errorf("%s spec.hostname is invalid: %w", res.ID(), err)
			}
		}
		switch defaultString(spec.Mode, "static") {
		case "static":
		case "vrrp":
			if spec.VRRP.VirtualRouterID < 1 || spec.VRRP.VirtualRouterID > 255 {
				return fmt.Errorf("%s spec.vrrp.virtualRouterID must be within 1-255", res.ID())
			}
			if targetOS != platform.OSFreeBSD && len(spec.VRRP.Peers) == 0 {
				return fmt.Errorf("%s spec.vrrp.peers is required for unicast VRRP", res.ID())
			}
			if spec.VRRP.Priority != 0 && (spec.VRRP.Priority < 1 || spec.VRRP.Priority > 254) {
				return fmt.Errorf("%s spec.vrrp.priority must be within 1-254", res.ID())
			}
			if spec.VRRP.AdvertInterval != "" {
				if _, err := time.ParseDuration(spec.VRRP.AdvertInterval); err != nil {
					return fmt.Errorf("%s spec.vrrp.advertInterval is invalid: %w", res.ID(), err)
				}
			}
			if spec.VRRP.PreemptDelay != "" {
				if spec.VRRP.Preempt == nil || !*spec.VRRP.Preempt {
					return fmt.Errorf("%s spec.vrrp.preemptDelay requires spec.vrrp.preempt=true", res.ID())
				}
				if _, err := time.ParseDuration(spec.VRRP.PreemptDelay); err != nil {
					return fmt.Errorf("%s spec.vrrp.preemptDelay is invalid: %w", res.ID(), err)
				}
			}
			for i, peer := range spec.VRRP.Peers {
				if err := validateAddressOrHostname(peer); err != nil {
					return fmt.Errorf("%s spec.vrrp.peers[%d] must be a single peer address or hostname", res.ID(), i)
				}
			}
			if err := validateSecretValueSource(res.ID(), "spec.vrrp.authentication", spec.VRRP.Authentication, "spec.vrrp.authenticationFrom", spec.VRRP.AuthenticationFrom); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s spec.mode must be static or vrrp", res.ID())
		}
		for i, track := range spec.Track {
			if err := validateSourceResourceRef(track.Resource); err != nil {
				return fmt.Errorf("%s spec.track[%d].resource %w", res.ID(), i, err)
			}
			if track.UnhealthyPenalty < 0 || track.UnhealthyPenalty > 254 {
				return fmt.Errorf("%s spec.track[%d].unhealthyPenalty must be within 0-254", res.ID(), i)
			}
			if track.ConfirmConsecutiveUnhealthy < 0 || track.ConfirmConsecutiveUnhealthy > 255 {
				return fmt.Errorf("%s spec.track[%d].confirmConsecutiveUnhealthy must be non-negative and within 1-255 when set", res.ID(), i)
			}
			if track.ConfirmConsecutiveHealthy < 0 || track.ConfirmConsecutiveHealthy > 255 {
				return fmt.Errorf("%s spec.track[%d].confirmConsecutiveHealthy must be non-negative and within 1-255 when set", res.ID(), i)
			}
		}
	case "VirtualIPv6Address":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.VirtualIPv6AddressSpec()
		if err != nil {
			return err
		}
		if strings.TrimSpace(spec.Interface) == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if strings.TrimSpace(spec.Address) == "" && strings.TrimSpace(spec.AddressFrom.Resource) == "" {
			return fmt.Errorf("%s spec.address or spec.addressFrom is required", res.ID())
		}
		if strings.TrimSpace(spec.Address) != "" && strings.TrimSpace(spec.AddressFrom.Resource) != "" {
			return fmt.Errorf("%s spec.address and spec.addressFrom are mutually exclusive", res.ID())
		}
		if strings.TrimSpace(spec.Address) != "" {
			prefix, err := netip.ParsePrefix(spec.Address)
			if err != nil || !prefix.Addr().Is6() {
				return fmt.Errorf("%s spec.address must be an IPv6 prefix", res.ID())
			}
			if prefix.Bits() != 128 {
				return fmt.Errorf("%s spec.address must be an IPv6 /128 prefix", res.ID())
			}
		}
		if err := validateIngressAddressSource(res.ID(), "spec.addressFrom", spec.AddressFrom); err != nil {
			return err
		}
		if strings.TrimSpace(spec.Hostname) != "" {
			if err := validateFQDN(spec.Hostname); err != nil {
				return fmt.Errorf("%s spec.hostname is invalid: %w", res.ID(), err)
			}
		}
		switch defaultString(spec.Mode, "static") {
		case "static":
		case "vrrp":
			if spec.VRRP.VirtualRouterID < 1 || spec.VRRP.VirtualRouterID > 255 {
				return fmt.Errorf("%s spec.vrrp.virtualRouterID must be within 1-255", res.ID())
			}
			if targetOS != platform.OSFreeBSD && len(spec.VRRP.Peers) == 0 {
				return fmt.Errorf("%s spec.vrrp.peers is required for unicast VRRP", res.ID())
			}
			if spec.VRRP.Priority != 0 && (spec.VRRP.Priority < 1 || spec.VRRP.Priority > 254) {
				return fmt.Errorf("%s spec.vrrp.priority must be within 1-254", res.ID())
			}
			if spec.VRRP.AdvertInterval != "" {
				if _, err := time.ParseDuration(spec.VRRP.AdvertInterval); err != nil {
					return fmt.Errorf("%s spec.vrrp.advertInterval is invalid: %w", res.ID(), err)
				}
			}
			if spec.VRRP.PreemptDelay != "" {
				if spec.VRRP.Preempt == nil || !*spec.VRRP.Preempt {
					return fmt.Errorf("%s spec.vrrp.preemptDelay requires spec.vrrp.preempt=true", res.ID())
				}
				if _, err := time.ParseDuration(spec.VRRP.PreemptDelay); err != nil {
					return fmt.Errorf("%s spec.vrrp.preemptDelay is invalid: %w", res.ID(), err)
				}
			}
			for i, peer := range spec.VRRP.Peers {
				if err := validateAddressOrHostname(peer); err != nil {
					return fmt.Errorf("%s spec.vrrp.peers[%d] must be a single peer address or hostname", res.ID(), i)
				}
			}
			if err := validateSecretValueSource(res.ID(), "spec.vrrp.authentication", spec.VRRP.Authentication, "spec.vrrp.authenticationFrom", spec.VRRP.AuthenticationFrom); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s spec.mode must be static or vrrp", res.ID())
		}
		for i, track := range spec.Track {
			if err := validateSourceResourceRef(track.Resource); err != nil {
				return fmt.Errorf("%s spec.track[%d].resource %w", res.ID(), i, err)
			}
			if track.UnhealthyPenalty < 0 || track.UnhealthyPenalty > 254 {
				return fmt.Errorf("%s spec.track[%d].unhealthyPenalty must be within 0-254", res.ID(), i)
			}
			if track.ConfirmConsecutiveUnhealthy < 0 || track.ConfirmConsecutiveUnhealthy > 255 {
				return fmt.Errorf("%s spec.track[%d].confirmConsecutiveUnhealthy must be non-negative and within 1-255 when set", res.ID(), i)
			}
			if track.ConfirmConsecutiveHealthy < 0 || track.ConfirmConsecutiveHealthy > 255 {
				return fmt.Errorf("%s spec.track[%d].confirmConsecutiveHealthy must be non-negative and within 1-255 when set", res.ID(), i)
			}
		}
	case "BGPRouter":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.BGPRouterSpec()
		if err != nil {
			return err
		}
		if spec.ASN == 0 {
			return fmt.Errorf("%s spec.asn is required", res.ID())
		}
		addr, err := netip.ParseAddr(spec.RouterID)
		if err != nil || !addr.Is4() {
			return fmt.Errorf("%s spec.routerID must be an IPv4 address", res.ID())
		}
		if spec.Listen.Port != 0 && (spec.Listen.Port < 1 || spec.Listen.Port > 65535) {
			return fmt.Errorf("%s spec.listen.port must be within 1-65535", res.ID())
		}
		if strings.TrimSpace(spec.Listen.Address) != "" {
			if _, err := netip.ParseAddr(strings.TrimSpace(spec.Listen.Address)); err != nil {
				return fmt.Errorf("%s spec.listen.address must be an IP address", res.ID())
			}
		}
		if err := validateBGPTimers(res.ID(), "spec.timers", spec.Timers); err != nil {
			return err
		}
		if err := validateBGPGracefulRestart(res.ID(), spec.GracefulRestart); err != nil {
			return err
		}
		if err := validateBGPWatcher(res.ID(), spec.Watcher); err != nil {
			return err
		}
		switch defaultString(spec.Backend, "frr") {
		case "frr":
		default:
			return fmt.Errorf("%s spec.backend must be frr", res.ID())
		}
		if err := validateBGPRouterPolicy(res.ID(), spec); err != nil {
			return err
		}
	case "BGPPeer":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.BGPPeerSpec()
		if err != nil {
			return err
		}
		kind, name, ok := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if !ok || kind != "BGPRouter" || strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s spec.routerRef must reference BGPRouter/<name>", res.ID())
		}
		if spec.PeerASN == 0 {
			return fmt.Errorf("%s spec.peerASN is required", res.ID())
		}
		if len(spec.Peers) == 0 {
			return fmt.Errorf("%s spec.peers is required", res.ID())
		}
		seenPeers := map[string]bool{}
		for i, peer := range spec.Peers {
			peer = strings.TrimSpace(peer)
			if peer == "" || strings.ContainsAny(peer, " \t\n\r") {
				return fmt.Errorf("%s spec.peers[%d] must be a single peer address or hostname", res.ID(), i)
			}
			if seenPeers[peer] {
				return fmt.Errorf("%s spec.peers[%d] duplicates %q", res.ID(), i, peer)
			}
			seenPeers[peer] = true
		}
		if err := validateBGPTimers(res.ID(), "spec.timers", spec.Timers); err != nil {
			return err
		}
		if err := validateBGPCommunities(res.ID(), "spec.communities", spec.Communities); err != nil {
			return err
		}
		if _, err := validateBGPPrefixList(res.ID(), "spec.exportPolicy.allowedPrefixes", spec.ExportPolicy.AllowedPrefixes); err != nil {
			return err
		}
		if err := validateBGPBFD(res.ID(), spec.BFD); err != nil {
			return err
		}
		if err := validateSecretValueSource(res.ID(), "spec.password", spec.Password, "spec.passwordFrom", spec.PasswordFrom); err != nil {
			return err
		}
	case "DHCPv6Address", "IPv6RAAddress":
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
	case "ClusterNetworkRoute":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.ClusterNetworkRouteSpec()
		if err != nil {
			return err
		}
		if err := validateClusterNetworkRoute(res.ID(), spec); err != nil {
			return err
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
		if len(spec.DNSSL) > 0 && len(spec.DNSSLFrom) > 0 {
			return fmt.Errorf("%s spec.dnssl and spec.dnsslFrom cannot both be set", res.ID())
		}
		for i, domain := range spec.DNSSL {
			if err := validateDomainValue(domain); err != nil {
				return fmt.Errorf("%s spec.dnssl[%d]: %w", res.ID(), i, err)
			}
		}
		for i, source := range spec.DNSSLFrom {
			if err := validateDNSZoneDomainSource(source); err != nil {
				return fmt.Errorf("%s spec.dnsslFrom[%d]: %w", res.ID(), i, err)
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
		rendersLANService := spec.Interface != "" || spec.Mode != "" || spec.AddressPool.Start != "" || spec.AddressPool.End != "" || len(spec.DNSServers) > 0 || len(spec.SNTPServers) > 0 || len(spec.DomainSearch) > 0 || len(spec.DomainSearchFrom) > 0
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
		if len(spec.DomainSearch) > 0 && len(spec.DomainSearchFrom) > 0 {
			return fmt.Errorf("%s spec.domainSearch and spec.domainSearchFrom cannot both be set", res.ID())
		}
		for i, domain := range spec.DomainSearch {
			if err := validateDomainValue(domain); err != nil {
				return fmt.Errorf("%s spec.domainSearch[%d]: %w", res.ID(), i, err)
			}
		}
		for i, source := range spec.DomainSearchFrom {
			if err := validateDNSZoneDomainSource(source); err != nil {
				return fmt.Errorf("%s spec.domainSearchFrom[%d]: %w", res.ID(), i, err)
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
			if spec.GatewayFrom.Resource != "" && spec.GatewayFrom.Field == "" {
				return fmt.Errorf("%s spec.gatewayFrom.field is required", res.ID())
			}
			for _, server := range append(append([]string{}, spec.DNSServers...), spec.NTPServers...) {
				addr, err := netip.ParseAddr(server)
				if err != nil || !addr.Is4() {
					return fmt.Errorf("%s dnsServers/ntpServers entries must be IPv4 addresses", res.ID())
				}
			}
			for i, source := range spec.DNSServerFrom {
				if source.Resource == "" || source.Field == "" {
					return fmt.Errorf("%s spec.dnsServerFrom[%d].resource and field are required", res.ID(), i)
				}
			}
			for i, source := range spec.NTPServerFrom {
				if source.Resource == "" || source.Field == "" {
					return fmt.Errorf("%s spec.ntpServerFrom[%d].resource and field are required", res.ID(), i)
				}
			}
			if spec.Domain != "" && spec.DomainFrom.Resource != "" {
				return fmt.Errorf("%s spec.domain and spec.domainFrom cannot both be set", res.ID())
			}
			if spec.Domain != "" {
				if err := validateDomainValue(spec.Domain); err != nil {
					return fmt.Errorf("%s spec.domain: %w", res.ID(), err)
				}
			}
			if spec.DomainFrom.Resource != "" || spec.DomainFrom.Field != "" {
				if err := validateDNSZoneDomainSource(spec.DomainFrom); err != nil {
					return fmt.Errorf("%s spec.domainFrom: %w", res.ID(), err)
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
		if spec.LocalAddressFrom.Resource != "" && spec.LocalAddressFrom.Field == "" {
			return fmt.Errorf("%s spec.localAddressFrom.field is required", res.ID())
		}
		if spec.LocalAddressFrom.Resource != "" {
			if err := validateSourceResourceRef(spec.LocalAddressFrom.Resource); err != nil {
				return fmt.Errorf("%s spec.localAddressFrom.resource: %w", res.ID(), err)
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
		if spec.FwMark < 0 {
			return fmt.Errorf("%s spec.fwmark must be non-negative", res.ID())
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
		if spec.SourceAddressFrom.Resource != "" && spec.SourceAddressFrom.Field == "" {
			return fmt.Errorf("%s spec.sourceAddressFrom.field is required", res.ID())
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
			if candidate.DeviceFrom.Resource != "" && candidate.DeviceFrom.Field == "" {
				return fmt.Errorf("%s spec.candidates[%d].deviceFrom.field is required", res.ID(), i)
			}
			if candidate.GatewayFrom.Resource != "" && candidate.GatewayFrom.Field == "" {
				return fmt.Errorf("%s spec.candidates[%d].gatewayFrom.field is required", res.ID(), i)
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
				if candidate.Gateway != "" || candidate.GatewayFrom.Resource != "" {
					return fmt.Errorf("%s spec.candidates[%d].gateway and gatewayFrom are only valid when gatewaySource is static, dhcpv4, or dhcpv6", res.ID(), i)
				}
			case "static":
				if (candidate.Gateway == "") == (candidate.GatewayFrom.Resource == "") {
					return fmt.Errorf("%s spec.candidates[%d] must set exactly one of gateway or gatewayFrom when gatewaySource is static", res.ID(), i)
				}
				if candidate.Gateway != "" {
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
				}
			case "dhcpv4":
				if defaultString(spec.Family, "ipv4") != "ipv4" {
					return fmt.Errorf("%s spec.candidates[%d].gatewaySource dhcpv4 requires family ipv4", res.ID(), i)
				}
				if candidate.Gateway != "" || candidate.GatewayFrom.Resource == "" {
					return fmt.Errorf("%s spec.candidates[%d] must set gatewayFrom and must not set gateway when gatewaySource is dhcpv4", res.ID(), i)
				}
			case "dhcpv6":
				if defaultString(spec.Family, "ipv4") != "ipv6" {
					return fmt.Errorf("%s spec.candidates[%d].gatewaySource dhcpv6 requires family ipv6", res.ID(), i)
				}
				if candidate.Gateway != "" || candidate.GatewayFrom.Resource == "" {
					return fmt.Errorf("%s spec.candidates[%d] must set gatewayFrom and must not set gateway when gatewaySource is dhcpv6", res.ID(), i)
				}
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
		for _, cidr := range spec.DestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.destinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs); err != nil {
			return err
		}
		for _, cidr := range spec.ExcludeDestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.excludeDestinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs); err != nil {
			return err
		}
		if spec.Type == "snat" {
			if spec.SNATAddress == "" && spec.SNATAddressFrom.Resource == "" {
				return fmt.Errorf("%s spec.snatAddress or spec.snatAddressFrom is required when type is snat", res.ID())
			}
			if spec.SNATAddress != "" && spec.SNATAddressFrom.Resource != "" {
				return fmt.Errorf("%s spec.snatAddress and spec.snatAddressFrom are mutually exclusive", res.ID())
			}
			if spec.SNATAddressFrom.Resource != "" && spec.SNATAddressFrom.Field == "" {
				return fmt.Errorf("%s spec.snatAddressFrom.field is required", res.ID())
			}
			addr, err := netip.ParseAddr(spec.SNATAddress)
			if spec.SNATAddress != "" && (err != nil || !addr.Is4()) {
				return fmt.Errorf("%s spec.snatAddress must be an IPv4 address when type is snat", res.ID())
			}
		}
		if spec.Type == "masquerade" && (spec.SNATAddress != "" || spec.SNATAddressFrom.Resource != "") {
			return fmt.Errorf("%s spec.snatAddress and spec.snatAddressFrom are only valid when type is snat", res.ID())
		}
	case "PortForward":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.PortForwardSpec()
		if err != nil {
			return err
		}
		if err := validateIngressListen(res.ID(), "spec.listen", spec.Listen); err != nil {
			return err
		}
		if err := validateIngressTarget(res.ID(), "spec.target", spec.Target.Address, spec.Target.AddressFrom, spec.Target.Port, false); err != nil {
			return err
		}
		if err := validateIngressHairpin(res.ID(), "spec.hairpin", spec.Listen, spec.Hairpin); err != nil {
			return err
		}
	case "IngressService":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.IngressServiceSpec()
		if err != nil {
			return err
		}
		if err := validateIngressListen(res.ID(), "spec.listen", spec.Listen); err != nil {
			return err
		}
		if err := validateIngressHairpin(res.ID(), "spec.hairpin", spec.Listen, spec.Hairpin); err != nil {
			return err
		}
		if strings.TrimSpace(spec.Hostname) != "" {
			if err := validateFQDN(spec.Hostname); err != nil {
				return fmt.Errorf("%s spec.hostname is invalid: %w", res.ID(), err)
			}
		}
		if len(spec.Backends) == 0 {
			return fmt.Errorf("%s spec.backends is required", res.ID())
		}
		if spec.HealthCheck.Protocol != "" {
			switch spec.HealthCheck.Protocol {
			case "tcp", "http", "https":
			default:
				return fmt.Errorf("%s spec.healthCheck.protocol must be tcp, http, or https", res.ID())
			}
		}
		for field, value := range map[string]string{"interval": spec.HealthCheck.Interval, "timeout": spec.HealthCheck.Timeout} {
			if value == "" {
				continue
			}
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s spec.healthCheck.%s is invalid: %w", res.ID(), field, err)
			}
		}
		if spec.HealthCheck.Path != "" && !strings.HasPrefix(spec.HealthCheck.Path, "/") {
			return fmt.Errorf("%s spec.healthCheck.path must be an absolute HTTP path", res.ID())
		}
		if strings.ContainsAny(spec.HealthCheck.Host, " \t\x00\n\r") {
			return fmt.Errorf("%s spec.healthCheck.host contains invalid characters", res.ID())
		}
		for i, code := range spec.HealthCheck.ExpectedStatus {
			if code < 100 || code > 599 {
				return fmt.Errorf("%s spec.healthCheck.expectedStatus[%d] must be within 100-599", res.ID(), i)
			}
		}
		if spec.HealthCheck.HealthyThreshold < 0 {
			return fmt.Errorf("%s spec.healthCheck.healthyThreshold must be non-negative and at least 1 when set", res.ID())
		}
		if spec.HealthCheck.UnhealthyThreshold < 0 {
			return fmt.Errorf("%s spec.healthCheck.unhealthyThreshold must be non-negative and at least 1 when set", res.ID())
		}
		if spec.Policy.Selection != "" {
			switch spec.Policy.Selection {
			case "failover", "sourceHash", "random":
			default:
				return fmt.Errorf("%s spec.policy.selection must be failover, sourceHash, or random", res.ID())
			}
		}
		if spec.Policy.OnNoHealthyBackends != "" {
			switch spec.Policy.OnNoHealthyBackends {
			case "drop", "reject":
			default:
				return fmt.Errorf("%s spec.policy.onNoHealthyBackends must be drop or reject", res.ID())
			}
		}
		for i, backend := range spec.Backends {
			if err := validateIngressTarget(res.ID(), fmt.Sprintf("spec.backends[%d]", i), backend.Address, backend.AddressFrom, backend.Port, true); err != nil {
				return err
			}
			if backend.Weight < 0 {
				return fmt.Errorf("%s spec.backends[%d].weight must be non-negative", res.ID(), i)
			}
		}
	case "IPAddressSet":
		if res.APIVersion != api.NetAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.NetAPIVersion)
		}
		spec, err := res.IPAddressSetSpec()
		if err != nil {
			return err
		}
		if len(spec.Addresses) == 0 && len(spec.Names) == 0 {
			return fmt.Errorf("%s spec.addresses or spec.names is required", res.ID())
		}
		seenAddresses := map[string]bool{}
		for i, value := range spec.Addresses {
			addr, err := netip.ParseAddr(value)
			if err != nil {
				return fmt.Errorf("%s spec.addresses[%d] must be an IP address", res.ID(), i)
			}
			addr = addr.Unmap()
			if seenAddresses[addr.String()] {
				return fmt.Errorf("%s spec.addresses[%d] duplicates address %q", res.ID(), i, addr.String())
			}
			seenAddresses[addr.String()] = true
		}
		seenNames := map[string]bool{}
		for i, value := range spec.Names {
			value = strings.TrimSpace(value)
			if value == "" {
				return fmt.Errorf("%s spec.names[%d] must not be empty", res.ID(), i)
			}
			if err := validateDomainValue(value); err != nil {
				return fmt.Errorf("%s spec.names[%d] is invalid: %w", res.ID(), i, err)
			}
			if seenNames[value] {
				return fmt.Errorf("%s spec.names[%d] duplicates name %q", res.ID(), i, value)
			}
			seenNames[value] = true
		}
		if spec.RefreshInterval != "" {
			if _, err := time.ParseDuration(spec.RefreshInterval); err != nil {
				return fmt.Errorf("%s spec.refreshInterval is invalid: %w", res.ID(), err)
			}
		}
	case "LocalServiceRedirect":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.LocalServiceRedirectSpec()
		if err != nil {
			return err
		}
		if strings.TrimSpace(spec.Interface) == "" {
			return fmt.Errorf("%s spec.interface is required", res.ID())
		}
		if len(spec.Rules) == 0 {
			return fmt.Errorf("%s spec.rules is required", res.ID())
		}
		for i, rule := range spec.Rules {
			if len(rule.Protocols) == 0 {
				return fmt.Errorf("%s spec.rules[%d].protocols is required", res.ID(), i)
			}
			seenProtocols := map[string]bool{}
			for j, proto := range rule.Protocols {
				switch proto {
				case "tcp", "udp":
				default:
					return fmt.Errorf("%s spec.rules[%d].protocols[%d] must be tcp or udp", res.ID(), i, j)
				}
				if seenProtocols[proto] {
					return fmt.Errorf("%s spec.rules[%d].protocols[%d] duplicates protocol %q", res.ID(), i, j, proto)
				}
				seenProtocols[proto] = true
			}
			if strings.TrimSpace(rule.DestinationSetRef) == "" {
				return fmt.Errorf("%s spec.rules[%d].destinationSetRef is required", res.ID(), i)
			}
			if rule.DestinationPort < 1 || rule.DestinationPort > 65535 {
				return fmt.Errorf("%s spec.rules[%d].destinationPort must be within 1-65535", res.ID(), i)
			}
			if rule.RedirectPort < 1 || rule.RedirectPort > 65535 {
				return fmt.Errorf("%s spec.rules[%d].redirectPort must be within 1-65535", res.ID(), i)
			}
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
		if len(spec.SourceCIDRs) == 0 && len(spec.DestinationCIDRs) == 0 && len(spec.DestinationSetRefs) == 0 {
			return fmt.Errorf("%s spec.sourceCIDRs, spec.destinationCIDRs, or spec.destinationSetRefs is required", res.ID())
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
		if err := validateIPAddressSetRefs(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs); err != nil {
			return err
		}
		for _, cidr := range spec.ExcludeDestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.excludeDestinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs); err != nil {
			return err
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
		if len(spec.SourceCIDRs) == 0 && len(spec.DestinationCIDRs) == 0 && len(spec.DestinationSetRefs) == 0 {
			return fmt.Errorf("%s spec.sourceCIDRs, spec.destinationCIDRs, or spec.destinationSetRefs is required", res.ID())
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
		if err := validateIPAddressSetRefs(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs); err != nil {
			return err
		}
		for _, cidr := range spec.ExcludeDestinationCIDRs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || !prefix.Addr().Is4() {
				return fmt.Errorf("%s spec.excludeDestinationCIDRs entries must be IPv4 prefixes", res.ID())
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs); err != nil {
			return err
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
	case "ClientPolicy":
		if res.APIVersion != api.FirewallAPIVersion {
			return fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FirewallAPIVersion)
		}
		spec, err := res.ClientPolicySpec()
		if err != nil {
			return err
		}
		switch spec.Mode {
		case "include", "exclude":
		default:
			return fmt.Errorf("%s spec.mode must be include or exclude", res.ID())
		}
		seenInterfaces := map[string]bool{}
		for i, name := range spec.Interfaces {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("%s spec.interfaces[%d] is required", res.ID(), i)
			}
			if seenInterfaces[name] {
				return fmt.Errorf("%s spec.interfaces[%d] duplicates %q", res.ID(), i, name)
			}
			seenInterfaces[name] = true
		}
		seenMACs := map[string]bool{}
		for i, value := range spec.MACs {
			mac, err := net.ParseMAC(value)
			if err != nil {
				return fmt.Errorf("%s spec.macs[%d] is invalid: %w", res.ID(), i, err)
			}
			normalizedMAC := strings.ToLower(mac.String())
			if seenMACs[normalizedMAC] {
				return fmt.Errorf("%s spec.macs[%d] duplicates %q", res.ID(), i, normalizedMAC)
			}
			seenMACs[normalizedMAC] = true
		}
		for i, entry := range spec.Classification {
			mac, err := net.ParseMAC(entry.MACAddress)
			if err != nil {
				return fmt.Errorf("%s spec.classification[%d].macAddress is invalid: %w", res.ID(), i, err)
			}
			normalizedMAC := strings.ToLower(mac.String())
			if seenMACs[normalizedMAC] {
				return fmt.Errorf("%s spec.classification[%d].macAddress duplicates %q", res.ID(), i, normalizedMAC)
			}
			seenMACs[normalizedMAC] = true
			switch entry.As {
			case "", "guest", "trusted":
			default:
				return fmt.Errorf("%s spec.classification[%d].as must be guest or trusted", res.ID(), i)
			}
			if strings.Contains(entry.IPv4Reservation, "/") {
				return fmt.Errorf("%s spec.classification[%d].ipv4Reservation must be a DHCPv4Reservation name, not Kind/name", res.ID(), i)
			}
		}
		for i, service := range spec.GuestServices {
			switch service {
			case "dns", "dhcp", "ntp", "mdns", "ssdp":
			default:
				return fmt.Errorf("%s spec.guestServices[%d] must be dns, dhcp, ntp, mdns, or ssdp", res.ID(), i)
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
				return fmt.Errorf("%s spec.isolation.%s must be allow or deny", res.ID(), key)
			}
		}
		for i, cidr := range append(append([]string{}, spec.GuestEgressDeny...), spec.GuestEgressAllow...) {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("%s guest egress CIDR[%d] is invalid: %w", res.ID(), i, err)
			}
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
		if spec.Log.CopyRange < 0 {
			return fmt.Errorf("%s spec.log.copyRange must be greater than or equal to 0", res.ID())
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
		if spec.Port != 0 && len(spec.DestinationPorts) > 0 {
			return fmt.Errorf("%s spec.port and spec.destinationPorts are mutually exclusive", res.ID())
		}
		if err := validateFirewallRulePorts(res.ID(), spec); err != nil {
			return err
		}
		if err := validateFirewallRuleICMP(res.ID(), spec); err != nil {
			return err
		}
		if err := validateFirewallRateLimit(res.ID(), spec.RateLimit); err != nil {
			return err
		}
		if spec.ConnLimit.MaxPerSource < 0 {
			return fmt.Errorf("%s spec.connLimit.maxPerSource must be greater than or equal to 0", res.ID())
		}
		for i, cidr := range spec.SourceCIDRs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("%s spec.srcCIDRs[%d] is invalid: %w", res.ID(), i, err)
			}
		}
		for i, cidr := range spec.DestinationCIDRs {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("%s spec.destinationCIDRs[%d] is invalid: %w", res.ID(), i, err)
			}
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs); err != nil {
			return err
		}
		if err := validateIPAddressSetRefs(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs); err != nil {
			return err
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
		routeType := defaultString(spec.Type, "unicast")
		switch routeType {
		case "unicast", "blackhole":
		default:
			return fmt.Errorf("%s spec.type must be unicast or blackhole", res.ID())
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
		if routeType == "blackhole" {
			if spec.Device != "" || spec.DeviceFrom.Resource != "" || spec.Gateway != "" || spec.GatewayFrom.Resource != "" {
				return fmt.Errorf("%s spec.device, spec.deviceFrom, spec.gateway, and spec.gatewayFrom are not valid when spec.type is blackhole", res.ID())
			}
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

func validateDomainValue(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.ContainsAny(value, " \t\n\r/,") {
		return fmt.Errorf("must be a single DNS domain")
	}
	return nil
}

func validateFQDN(value string) error {
	value = strings.Trim(strings.TrimSpace(value), ".")
	if value == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.ContainsAny(value, " \t\n\r/,") {
		return fmt.Errorf("must be a single DNS name")
	}
	if !strings.Contains(value, ".") {
		return fmt.Errorf("must be a fully qualified DNS name")
	}
	if len(value) > 253 {
		return fmt.Errorf("must be 253 bytes or shorter")
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("must contain non-empty labels of 63 bytes or less")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("labels must not start or end with '-'")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return fmt.Errorf("labels must contain only letters, digits, or '-'")
		}
	}
	return nil
}

func dnsHostnameCovered(hostname string, zones map[string]string) (string, bool) {
	hostname = strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), ".")
	bestName := ""
	bestLen := -1
	for name, zone := range zones {
		zone = strings.Trim(strings.ToLower(strings.TrimSpace(zone)), ".")
		if zone == "" {
			continue
		}
		if hostname == zone || strings.HasSuffix(hostname, "."+zone) {
			if len(zone) > bestLen {
				bestName = name
				bestLen = len(zone)
			}
		}
	}
	return bestName, bestName != ""
}

func validateDNSZoneDomainSource(source api.StatusValueSourceSpec) error {
	resource := strings.TrimSpace(source.Resource)
	field := strings.TrimSpace(source.Field)
	if resource == "" || field == "" {
		return fmt.Errorf("resource and field are required")
	}
	kind, name, ok := strings.Cut(resource, "/")
	if !ok || kind != "DNSZone" || name == "" {
		return fmt.Errorf("resource must reference DNSZone/<name>")
	}
	if field != "zone" {
		return fmt.Errorf("field must be zone")
	}
	return nil
}

func interfaceRef(res api.Resource) (string, error) {
	switch res.Kind {
	case "IPv4StaticAddress":
		spec, err := res.IPv4StaticAddressSpec()
		return spec.Interface, err
	case "VirtualIPv4Address":
		spec, err := res.VirtualIPv4AddressSpec()
		return spec.Interface, err
	case "VirtualIPv6Address":
		spec, err := res.VirtualIPv6AddressSpec()
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

func validateIngressListen(resourceID, path string, listen api.IngressListenSpec) error {
	if strings.TrimSpace(listen.Interface) == "" {
		return fmt.Errorf("%s %s.interface is required", resourceID, path)
	}
	if strings.TrimSpace(listen.Address) != "" && strings.TrimSpace(listen.AddressFrom.Resource) != "" {
		return fmt.Errorf("%s %s.address and %s.addressFrom are mutually exclusive", resourceID, path, path)
	}
	if strings.TrimSpace(listen.Address) != "" {
		addr, err := netip.ParseAddr(listen.Address)
		if err != nil || !addr.Is4() {
			return fmt.Errorf("%s %s.address must be an IPv4 address", resourceID, path)
		}
	}
	if err := validateIngressAddressSource(resourceID, path+".addressFrom", listen.AddressFrom); err != nil {
		return err
	}
	switch listen.Protocol {
	case "tcp", "udp":
	default:
		return fmt.Errorf("%s %s.protocol must be tcp or udp", resourceID, path)
	}
	if listen.Port < 1 || listen.Port > 65535 {
		return fmt.Errorf("%s %s.port must be within 1-65535", resourceID, path)
	}
	return nil
}

func validateIngressTarget(resourceID, path, address string, addressFrom api.StatusValueSourceSpec, port int, allowHostname bool) error {
	if strings.TrimSpace(address) == "" && strings.TrimSpace(addressFrom.Resource) == "" {
		return fmt.Errorf("%s %s.address or %s.addressFrom is required", resourceID, path, path)
	}
	if strings.TrimSpace(address) != "" && strings.TrimSpace(addressFrom.Resource) != "" {
		return fmt.Errorf("%s %s.address and %s.addressFrom are mutually exclusive", resourceID, path, path)
	}
	if strings.TrimSpace(address) != "" {
		addr, err := netip.ParseAddr(address)
		if err != nil || !addr.Is4() {
			if !allowHostname {
				return fmt.Errorf("%s %s.address must be an IPv4 address", resourceID, path)
			}
			if err := validateAddressOrHostname(address); err != nil {
				return fmt.Errorf("%s %s.address %w", resourceID, path, err)
			}
		}
	}
	if err := validateIngressAddressSource(resourceID, path+".addressFrom", addressFrom); err != nil {
		return err
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("%s %s.port must be within 1-65535", resourceID, path)
	}
	return nil
}

func validateIngressHairpin(resourceID, path string, listen api.IngressListenSpec, hairpin api.IngressHairpinSpec) error {
	mode := strings.TrimSpace(hairpin.Mode)
	switch mode {
	case "", "auto", "manual", "off":
	default:
		return fmt.Errorf("%s %s.mode must be auto, manual, or off", resourceID, path)
	}
	if mode == "off" {
		return nil
	}
	if !hairpin.Enabled && mode != "auto" {
		if len(hairpin.Interfaces) > 0 {
			return fmt.Errorf("%s %s.enabled must be true when interfaces are set", resourceID, path)
		}
		return nil
	}
	if strings.TrimSpace(listen.Address) == "" && strings.TrimSpace(listen.AddressFrom.Resource) == "" {
		return fmt.Errorf("%s %s requires spec.listen.address or spec.listen.addressFrom", resourceID, path)
	}
	if hairpin.Enabled && mode != "auto" && len(hairpin.Interfaces) == 0 {
		return fmt.Errorf("%s %s.interfaces is required when enabled is true", resourceID, path)
	}
	for i, name := range hairpin.Interfaces {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s %s.interfaces[%d] must not be empty", resourceID, path, i)
		}
	}
	return nil
}

func validateIngressInterfaceRefs(resourceID string, listen api.IngressListenSpec, hairpin api.IngressHairpinSpec, interfaces map[string]bool) error {
	if !interfaces[listen.Interface] {
		return fmt.Errorf("%s spec.listen.interface references missing Interface %q", resourceID, listen.Interface)
	}
	if !hairpin.Enabled && strings.TrimSpace(hairpin.Mode) != "auto" {
		return nil
	}
	seen := map[string]bool{}
	for i, name := range hairpin.Interfaces {
		if !interfaces[name] {
			return fmt.Errorf("%s spec.hairpin.interfaces[%d] references missing Interface %q", resourceID, i, name)
		}
		if seen[name] {
			return fmt.Errorf("%s spec.hairpin.interfaces[%d] duplicates Interface %q", resourceID, i, name)
		}
		seen[name] = true
	}
	return nil
}

func splitResourceRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if kind, name, ok := strings.Cut(ref, "/"); ok {
		return kind, name
	}
	return "IPAddressSet", ref
}

func validateIPAddressSetRefs(resourceID, path string, refs []string) error {
	seen := map[string]bool{}
	for i, ref := range refs {
		kind, name := splitResourceRef(ref)
		if kind != "IPAddressSet" || strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s %s[%d] must reference IPAddressSet", resourceID, path, i)
		}
		key := kind + "/" + name
		if seen[key] {
			return fmt.Errorf("%s %s[%d] duplicates IPAddressSet reference %q", resourceID, path, i, ref)
		}
		seen[key] = true
	}
	return nil
}

func validateIPAddressSetRefsExist(resourceID, path string, refs []string, known map[string]bool) error {
	if err := validateIPAddressSetRefs(resourceID, path, refs); err != nil {
		return err
	}
	for i, ref := range refs {
		_, name := splitResourceRef(ref)
		if !known[name] {
			return fmt.Errorf("%s %s[%d] references missing IPAddressSet %q", resourceID, path, i, ref)
		}
	}
	return nil
}

func validateIngressAddressSource(resourceID, path string, source api.StatusValueSourceSpec) error {
	if strings.TrimSpace(source.Resource) == "" {
		if strings.TrimSpace(source.Field) != "" {
			return fmt.Errorf("%s %s.resource is required when field is set", resourceID, path)
		}
		return nil
	}
	if strings.TrimSpace(source.Field) == "" {
		return fmt.Errorf("%s %s.field is required", resourceID, path)
	}
	return nil
}

func validateBGPTimers(resourceID, path string, spec api.BGPTimersSpec) error {
	keepalive, err := validateOptionalDuration(resourceID, path+".keepalive", spec.Keepalive)
	if err != nil {
		return err
	}
	holdTime, err := validateOptionalDuration(resourceID, path+".holdTime", spec.HoldTime)
	if err != nil {
		return err
	}
	if _, err := validateOptionalDuration(resourceID, path+".connectRetry", spec.ConnectRetry); err != nil {
		return err
	}
	if keepalive > 0 && holdTime > 0 && holdTime <= keepalive {
		return fmt.Errorf("%s %s.holdTime must be greater than keepalive", resourceID, path)
	}
	return nil
}

func validateBGPGracefulRestart(resourceID string, spec api.BGPGracefulRestartSpec) error {
	if _, err := validateOptionalDuration(resourceID, "spec.gracefulRestart.restartTime", spec.RestartTime); err != nil {
		return err
	}
	if _, err := validateOptionalDuration(resourceID, "spec.gracefulRestart.stalePathTime", spec.StalePathTime); err != nil {
		return err
	}
	return nil
}

func validateOptionalDuration(resourceID, path, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %s is invalid: %w", resourceID, path, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s %s must be positive", resourceID, path)
	}
	return duration, nil
}

func validateDSLiteInnerLocalAddress(value string) error {
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return fmt.Errorf("must be an IPv4 address")
	}
	if addr.IsUnspecified() || addr.IsMulticast() || addr.IsLoopback() {
		return fmt.Errorf("must be a usable unicast IPv4 address")
	}
	return nil
}

func validateSourceResourceRef(value string) error {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("must be Kind/name")
	}
	return nil
}

func validateAddressOrHostname(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\n\r") {
		return fmt.Errorf("must be a single address or hostname")
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return nil
	}
	hostname := strings.TrimSuffix(value, ".")
	if hostname == "" || len(hostname) > 253 || strings.Contains(hostname, "..") {
		return fmt.Errorf("must be a single address or hostname")
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("must be a single address or hostname")
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '-':
			default:
				return fmt.Errorf("must be a single address or hostname")
			}
		}
	}
	return nil
}

func defaultPackageManager(osName string) string {
	switch osName {
	case "ubuntu", "debian":
		return "apt"
	case "alpine":
		return "apk"
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

func validateHealthCheckDerivedFwMark(router *api.Router, res api.Resource, spec api.HealthCheckSpec) error {
	refs := healthcheck.DerivedFwMarkRefs(router, res.Metadata.Name)
	if len(refs) == 0 {
		return nil
	}
	var mark int
	var first healthcheck.FwMarkRef
	for _, ref := range refs {
		if ref.Mark == 0 {
			continue
		}
		if mark == 0 {
			mark = ref.Mark
			first = ref
			continue
		}
		if mark != ref.Mark {
			return fmt.Errorf("%s is referenced by routing targets with conflicting marks: %s/%s=0x%x and %s/%s=0x%x", res.ID(), first.Resource, first.Name, mark, ref.Resource, ref.Name, ref.Mark)
		}
	}
	if mark == 0 {
		return nil
	}
	if spec.FwMark != 0 && spec.FwMark != mark {
		return fmt.Errorf("%s spec.fwmark 0x%x conflicts with routing target mark 0x%x; omit spec.fwmark to derive it from the referenced route target", res.ID(), spec.FwMark, mark)
	}
	return nil
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

func validEnvironmentName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_' {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func validateSystemdEnvironmentFilePath(value string) error {
	path := strings.TrimPrefix(strings.TrimSpace(value), "-")
	if path == "" {
		return fmt.Errorf("must not be empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("must be an absolute path")
	}
	if strings.ContainsAny(path, "\x00\n\r") {
		return fmt.Errorf("contains invalid characters")
	}
	return nil
}

func validateFirewallRulePorts(resourceID string, spec api.FirewallRuleSpec) error {
	srcPorts := spec.SourcePorts
	dstPorts := spec.DestinationPorts
	if spec.Port != 0 {
		dstPorts = []api.FirewallPort{api.FirewallPort(strconv.Itoa(spec.Port))}
	}
	if len(srcPorts) == 0 && len(dstPorts) == 0 {
		return nil
	}
	if spec.Protocol != "tcp" && spec.Protocol != "udp" {
		return fmt.Errorf("%s sourcePorts, destinationPorts, or port require protocol tcp or udp", resourceID)
	}
	if spec.Port < 0 || spec.Port > 65535 {
		return fmt.Errorf("%s spec.port must be between 1 and 65535", resourceID)
	}
	if err := validateFirewallPortList(resourceID, "spec.sourcePorts", srcPorts); err != nil {
		return err
	}
	return validateFirewallPortList(resourceID, "spec.destinationPorts", dstPorts)
}

func validateFirewallPortList(resourceID, field string, ports []api.FirewallPort) error {
	rangeCount := 0
	for i, port := range ports {
		value := strings.TrimSpace(string(port))
		if value == "" {
			return fmt.Errorf("%s %s[%d] must not be empty", resourceID, field, i)
		}
		if strings.Contains(value, "-") {
			rangeCount++
			parts := strings.Split(value, "-")
			if len(parts) != 2 {
				return fmt.Errorf("%s %s[%d] must be a port or start-end range", resourceID, field, i)
			}
			start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return fmt.Errorf("%s %s[%d] range start is invalid: %w", resourceID, field, i, err)
			}
			end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return fmt.Errorf("%s %s[%d] range end is invalid: %w", resourceID, field, i, err)
			}
			if start < 1 || start > 65535 || end < 1 || end > 65535 || start > end {
				return fmt.Errorf("%s %s[%d] range must be within 1-65535 and start <= end", resourceID, field, i)
			}
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s %s[%d] must be a port number or start-end range: %w", resourceID, field, i, err)
		}
		if n < 1 || n > 65535 {
			return fmt.Errorf("%s %s[%d] must be between 1 and 65535", resourceID, field, i)
		}
	}
	if rangeCount > 0 && len(ports) > 1 {
		return fmt.Errorf("%s %s cannot mix a port range with multiple port entries", resourceID, field)
	}
	return nil
}

func validateFirewallRuleICMP(resourceID string, spec api.FirewallRuleSpec) error {
	icmpType := firewallRuleICMPType(spec)
	icmpv6Type := firewallRuleICMPv6Type(spec)
	if icmpType != "" {
		if spec.Protocol != "icmp" {
			return fmt.Errorf("%s spec.icmpType requires protocol icmp", resourceID)
		}
		if _, ok := firewallICMPTypes[strings.TrimSpace(icmpType)]; !ok {
			return fmt.Errorf("%s spec.icmpType %q is not supported", resourceID, icmpType)
		}
	}
	if icmpv6Type != "" {
		if spec.Protocol != "icmpv6" && spec.Protocol != "ipv6-icmp" {
			return fmt.Errorf("%s spec.icmpv6Type requires protocol icmpv6 or ipv6-icmp", resourceID)
		}
		if _, ok := firewallICMPv6Types[strings.TrimSpace(icmpv6Type)]; !ok {
			return fmt.Errorf("%s spec.icmpv6Type %q is not supported", resourceID, icmpv6Type)
		}
	}
	return nil
}

func firewallRuleICMPType(spec api.FirewallRuleSpec) string {
	if strings.TrimSpace(spec.ICMPType) != "" {
		return strings.TrimSpace(spec.ICMPType)
	}
	return strings.TrimSpace(spec.ICMPTypeKebab)
}

func firewallRuleICMPv6Type(spec api.FirewallRuleSpec) string {
	if strings.TrimSpace(spec.ICMPv6Type) != "" {
		return strings.TrimSpace(spec.ICMPv6Type)
	}
	return strings.TrimSpace(spec.ICMPv6TypeKebab)
}

func validateFirewallRateLimit(resourceID string, limit api.FirewallRateLimitSpec) error {
	rate := limit.Rate
	if rate == 0 {
		rate = limit.PacketsPerSecond
	}
	if rate < 0 {
		return fmt.Errorf("%s spec.rateLimit.rate must be greater than or equal to 0", resourceID)
	}
	if rate == 0 {
		if limit.Unit != "" || limit.Per != "" || limit.Burst != 0 || limit.Log {
			return fmt.Errorf("%s spec.rateLimit.rate is required when rateLimit is configured", resourceID)
		}
		return nil
	}
	switch limit.Unit {
	case "", "packet", "byte", "kilobyte", "megabyte":
	default:
		return fmt.Errorf("%s spec.rateLimit.unit must be packet, byte, kilobyte, or megabyte", resourceID)
	}
	switch limit.Per {
	case "", "second", "minute":
	default:
		return fmt.Errorf("%s spec.rateLimit.per must be second or minute", resourceID)
	}
	if limit.Burst < 0 {
		return fmt.Errorf("%s spec.rateLimit.burst must be greater than or equal to 0", resourceID)
	}
	return nil
}

func validateObservabilitySink(resourceID string, index int, sink api.ObservabilityPipelineLogSink) error {
	path := fmt.Sprintf("spec.logs.sinks[%d]", index)
	switch sink.Type {
	case "stdout", "syslog", "loki", "kafka":
	default:
		return fmt.Errorf("%s %s.type must be stdout, syslog, loki, or kafka", resourceID, path)
	}
	switch defaultString(sink.MinLevel, "info") {
	case "debug", "info", "warning", "error":
	default:
		return fmt.Errorf("%s %s.minLevel must be debug, info, warning, or error", resourceID, path)
	}
	switch sink.Type {
	case "loki":
		if strings.TrimSpace(sink.Loki.URL) == "" {
			return fmt.Errorf("%s %s.loki.url is required when type is loki", resourceID, path)
		}
		if _, err := url.ParseRequestURI(strings.TrimSpace(sink.Loki.URL)); err != nil {
			return fmt.Errorf("%s %s.loki.url is invalid: %w", resourceID, path, err)
		}
	case "syslog":
		switch sink.Syslog.Network {
		case "", "unix", "unixgram", "tcp", "udp":
		default:
			return fmt.Errorf("%s %s.syslog.network must be unix, unixgram, tcp, or udp", resourceID, path)
		}
	case "kafka":
		if len(compactStrings(sink.Kafka.Brokers)) == 0 || strings.TrimSpace(sink.Kafka.Topic) == "" {
			return fmt.Errorf("%s %s.kafka.brokers and kafka.topic document the intended sink and must be set", resourceID, path)
		}
	}
	return nil
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

var firewallICMPTypes = map[string]string{
	"echo-reply":              "echo-reply",
	"destination-unreachable": "destination-unreachable",
	"source-quench":           "source-quench",
	"redirect":                "redirect",
	"echo-request":            "echo-request",
	"router-advertisement":    "router-advertisement",
	"router-solicitation":     "router-solicitation",
	"time-exceeded":           "time-exceeded",
	"parameter-problem":       "parameter-problem",
	"timestamp-request":       "timestamp-request",
	"timestamp-reply":         "timestamp-reply",
}

var firewallICMPv6Types = map[string]string{
	"destination-unreachable": "destination-unreachable",
	"packet-too-big":          "packet-too-big",
	"time-exceeded":           "time-exceeded",
	"parameter-problem":       "parameter-problem",
	"echo-request":            "echo-request",
	"echo-reply":              "echo-reply",
	"router-solicit":          "nd-router-solicit",
	"router-advert":           "nd-router-advert",
	"neighbor-solicit":        "nd-neighbor-solicit",
	"neighbor-advert":         "nd-neighbor-advert",
	"nd-router-solicit":       "nd-router-solicit",
	"nd-router-advert":        "nd-router-advert",
	"nd-neighbor-solicit":     "nd-neighbor-solicit",
	"nd-neighbor-advert":      "nd-neighbor-advert",
}

func stringInSlice(value string, values []string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
