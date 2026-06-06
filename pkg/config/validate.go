// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
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

	idx := newRouterIndex(router)
	if err := idx.build(router, targetOS); err != nil {
		return err
	}

	if err := validateListenPortCollisions(router); err != nil {
		return err
	}
	if err := validateDNSForwarderGraph(router, idx.DNSResolvers, idx.DNSForwarders, idx.DNSUpstreams, idx.DNSZones, idx.Interfaces, idx.WireGuardInterfaces); err != nil {
		return err
	}
	if err := validateStatusReferences(router); err != nil {
		return err
	}
	if err := validateBGPRouterInstances(router, idx.VRFs); err != nil {
		return err
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "VirtualAddress" {
			continue
		}
		spec, err := res.VirtualAddressSpec()
		if err != nil {
			return err
		}
		if spec.Family != "ipv4" {
			continue
		}
		if address := strings.TrimSpace(spec.Address); address != "" {
			if prefix, err := netip.ParsePrefix(address); err == nil {
				if existing := idx.StaticByInterfaceAddress[spec.Interface+"|"+prefix.Masked().String()]; existing != "" {
					return fmt.Errorf("%s spec.address conflicts with IPv4StaticAddress %s on interface %q", res.ID(), existing, spec.Interface)
				}
			}
		}
		if kind, name, ok := strings.Cut(strings.TrimSpace(spec.AddressFrom.Resource), "/"); ok && kind == "IPv4StaticAddress" {
			if source, ok := idx.StaticIPv4ByName[name]; ok && source.iface == spec.Interface {
				return fmt.Errorf("%s spec.addressFrom conflicts with %s on interface %q; do not manage %s as both IPv4StaticAddress and VirtualAddress", res.ID(), source.id, spec.Interface, source.address)
			}
		}
	}
	for i, name := range router.Spec.Apply.ProtectedInterfaces {
		if !idx.Interfaces[name] {
			return fmt.Errorf("spec.apply.protectedInterfaces[%d] references missing Interface %q", i, name)
		}
	}
	for i, name := range router.Spec.Apply.ProtectedZones {
		if !idx.Zones[name] {
			return fmt.Errorf("spec.apply.protectedZones[%d] references missing FirewallZone %q", i, name)
		}
	}
	for iface, pd := range idx.ExternalPDByInterface {
		if dhcpv6, ok := idx.DHCPv6AddressByInterface[iface]; ok && dhcpv6.client != pd.client {
			return fmt.Errorf("%s conflicts with %s on interface %q: client=%s must own DHCPv6 on that interface", pd.id, dhcpv6.id, iface, pd.client)
		}
	}

	bfdRefs := map[string]int{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "IPv4StaticAddress", "VirtualAddress", "DHCPv4Client", "IPv4StaticRoute", "IPv6StaticRoute", "DHCPv6Address", "IPv6RAAddress", "DHCPv6PrefixDelegation", "IPv6DelegatedAddress", "DSLiteTunnel", "PPPoESession":
			name, err := interfaceRef(res)
			if err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("%s spec.interface is required", res.ID())
			}
			if !idx.Interfaces[name] {
				return fmt.Errorf("%s references missing Interface %q", res.ID(), name)
			}
			if res.Kind == "PPPoESession" && !idx.BaseInterfaces[name] {
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
				if !idx.Interfaces[name] {
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
			if kind != "BGPRouter" || !idx.BGPRouters[name] {
				return fmt.Errorf("%s spec.routerRef references missing BGPRouter %q", res.ID(), spec.RouterRef)
			}
			if spec.RouteReflectorClient {
				routerSpec := idx.BGPRouterSpecs[name]
				if routerSpec.ASN != spec.PeerASN {
					return fmt.Errorf("%s spec.routeReflectorClient requires iBGP peerASN matching %s spec.asn", res.ID(), spec.RouterRef)
				}
			}
			if strings.TrimSpace(spec.BFD) != "" {
				refKind, refName, ok := strings.Cut(strings.TrimSpace(spec.BFD), "/")
				bfdSpec, exists := idx.BFDSpecs[refName]
				if !ok || refKind != "BFD" || !exists {
					return fmt.Errorf("%s spec.bfd references missing BFD %q", res.ID(), spec.BFD)
				}
				if !bfdSpecMatchesBGPPeer(bfdSpec, res.Metadata.Name, spec.Peers) {
					return fmt.Errorf("%s spec.bfd references BFD %q whose spec.peer does not match this BGPPeer or one of its peer addresses", res.ID(), spec.BFD)
				}
				bfdRefs[refName]++
			}
		}
		if res.Kind == "SAMTransportProfile" {
			spec, err := res.SAMTransportProfileSpec()
			if err != nil {
				return err
			}
			if !idx.Interfaces[spec.UnderlayInterface] {
				return fmt.Errorf("%s spec.underlayInterface references missing Interface %q", res.ID(), spec.UnderlayInterface)
			}
			kind, name, _ := strings.Cut(strings.TrimSpace(spec.BGP.RouterRef), "/")
			if kind != "BGPRouter" || !idx.BGPRouters[name] {
				return fmt.Errorf("%s spec.bgp.routerRef references missing BGPRouter %q", res.ID(), spec.BGP.RouterRef)
			}
			for i, peer := range spec.Peers {
				if override := strings.TrimSpace(peer.Override.UnderlayInterface); override != "" && !idx.Interfaces[override] {
					return fmt.Errorf("%s spec.peers[%d].override.underlayInterface references missing Interface %q", res.ID(), i, override)
				}
			}
		}
		if res.Kind == "BFD" {
			spec, err := res.BFDSpec()
			if err != nil {
				return err
			}
			if kind, name, ok := strings.Cut(strings.TrimSpace(spec.Peer), "/"); ok {
				if kind != "BGPPeer" || !idx.Seen[api.NetAPIVersion+"/BGPPeer/"+name] {
					return fmt.Errorf("%s spec.peer references missing BGPPeer %q", res.ID(), spec.Peer)
				}
			}
			if spec.Interface != "" {
				refKind, refName := splitFirewallInterfaceRef(spec.Interface)
				if refKind != "Interface" || !idx.Interfaces[refName] {
					return fmt.Errorf("%s spec.interface references missing Interface %q", res.ID(), spec.Interface)
				}
			}
		}
		if res.Kind == "Bridge" {
			spec, err := res.BridgeSpec()
			if err != nil {
				return err
			}
			for i, member := range spec.Members {
				if !idx.Interfaces[member] {
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
			if !idx.Interfaces[spec.UnderlayInterface] {
				return fmt.Errorf("%s spec.underlayInterface references missing Interface %q", res.ID(), spec.UnderlayInterface)
			}
			if spec.Bridge != "" && !idx.Interfaces[spec.Bridge] {
				return fmt.Errorf("%s spec.bridge references missing Bridge %q", res.ID(), spec.Bridge)
			}
		}
		if res.Kind == "WireGuardPeer" {
			spec, err := res.WireGuardPeerSpec()
			if err != nil {
				return err
			}
			if !idx.WireGuardInterfaces[spec.Interface] {
				return fmt.Errorf("%s spec.interface references missing WireGuardInterface %q", res.ID(), spec.Interface)
			}
		}
		if res.Kind == "VRF" {
			spec, err := res.VRFSpec()
			if err != nil {
				return err
			}
			for i, member := range spec.Members {
				if !idx.Interfaces[member] {
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
			if !idx.Interfaces[spec.UnderlayInterface] {
				return fmt.Errorf("%s spec.underlayInterface references missing Interface %q", res.ID(), spec.UnderlayInterface)
			}
			if spec.Bridge != "" && !idx.Interfaces[spec.Bridge] {
				return fmt.Errorf("%s spec.bridge references missing Bridge %q", res.ID(), spec.Bridge)
			}
		}
		if res.Kind == "DHCPv4Reservation" {
			spec, err := res.DHCPv4ReservationSpec()
			if err != nil {
				return err
			}
			if spec.Scope != "" {
				return fmt.Errorf("%s spec.scope is not supported; use spec.server to reference DHCPv4Server", res.ID())
			}
			if spec.Server != "" {
				if _, ok := idx.DHCPv4ServerSpecs[spec.Server]; !ok {
					return fmt.Errorf("%s references missing DHCPv4Server %q", res.ID(), spec.Server)
				}
			}
		}
		if res.Kind == "NTPClient" {
			spec, err := res.NTPClientSpec()
			if err != nil {
				return err
			}
			if spec.Interface != "" && !idx.Interfaces[spec.Interface] {
				return fmt.Errorf("%s references missing Interface %q", res.ID(), spec.Interface)
			}
		}
		if res.Kind == "LogSink" {
			spec, err := res.LogSinkSpec()
			if err != nil {
				return err
			}
			if strings.TrimSpace(spec.OTLP.TelemetryRef) != "" {
				kind, name := splitKindNameRef(spec.OTLP.TelemetryRef, "Telemetry")
				if kind != "Telemetry" || !idx.Telemetries[name] {
					return fmt.Errorf("%s spec.otlp.telemetryRef references missing Telemetry %q", res.ID(), spec.OTLP.TelemetryRef)
				}
			}
		}
		if res.Kind == "LogRetention" {
			spec, err := res.LogRetentionSpec()
			if err != nil {
				return err
			}
			for i, ref := range spec.Sinks {
				kind, name := splitKindNameRef(ref, "LogSink")
				if kind != "LogSink" || !idx.LogSinks[name] {
					return fmt.Errorf("%s spec.sinks[%d] references missing LogSink %q", res.ID(), i, ref)
				}
			}
		}
		if res.Kind == "DHCPv4Server" {
			spec, err := res.DHCPv4ServerSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.ListenInterfaces {
				if !idx.Interfaces[name] {
					return fmt.Errorf("%s spec.listenInterfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
			if spec.Interface != "" && !idx.Interfaces[spec.Interface] {
				return fmt.Errorf("%s spec.interface references missing Interface %q", res.ID(), spec.Interface)
			}
			if spec.DNSInterface != "" && !idx.Interfaces[spec.DNSInterface] {
				return fmt.Errorf("%s spec.dnsInterface references missing Interface %q", res.ID(), spec.DNSInterface)
			}
		}
		if res.Kind == "DHCPv6Server" {
			spec, err := res.DHCPv6ServerSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.ListenInterfaces {
				if !idx.Interfaces[name] {
					return fmt.Errorf("%s spec.listenInterfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
			if spec.Interface != "" && !idx.Interfaces[spec.Interface] {
				return fmt.Errorf("%s spec.interface references missing Interface %q", res.ID(), spec.Interface)
			}
			if spec.DelegatedAddress != "" {
				if !idx.DelegatedAddresses[spec.DelegatedAddress] {
					return fmt.Errorf("%s references missing IPv6DelegatedAddress %q", res.ID(), spec.DelegatedAddress)
				}
				if len(spec.ListenInterfaces) > 0 && !stringInSlice(idx.DelegatedAddressInterfaces[spec.DelegatedAddress], spec.ListenInterfaces) {
					return fmt.Errorf("%s delegatedAddress interface %q must be listed in spec.listenInterfaces", res.ID(), idx.DelegatedAddressInterfaces[spec.DelegatedAddress])
				}
			}
			if spec.SelfAddressPolicy != "" && !idx.SelfAddressPolicies[spec.SelfAddressPolicy] {
				return fmt.Errorf("%s references missing SelfAddressPolicy %q", res.ID(), spec.SelfAddressPolicy)
			}
		}
		if res.Kind == "DHCPv4Reservation" {
			spec, err := res.DHCPv4ReservationSpec()
			if err != nil {
				return err
			}
			if spec.Server != "" {
				if !idx.DirectDHCPv4Servers[spec.Server] {
					return fmt.Errorf("%s spec.server references missing direct DHCPv4Server %q", res.ID(), spec.Server)
				}
			} else if spec.Scope == "" && len(idx.DirectDHCPv4Servers) != 1 {
				return fmt.Errorf("%s spec.server is required when direct DHCPv4Server count is not one", res.ID())
			}
		}
		if res.Kind == "DHCPv4Relay" {
			spec, err := res.DHCPv4RelaySpec()
			if err != nil {
				return err
			}
			for i, name := range spec.Interfaces {
				if !idx.Interfaces[name] {
					return fmt.Errorf("%s spec.idx.Interfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
		}
		if res.Kind == "IPv6DelegatedAddress" {
			spec, err := res.IPv6DelegatedAddressSpec()
			if err != nil {
				return err
			}
			if !idx.PrefixDelegations[spec.PrefixDelegation] {
				return fmt.Errorf("%s references missing DHCPv6PrefixDelegation %q", res.ID(), spec.PrefixDelegation)
			}
		}
		if res.Kind == "SelfAddressPolicy" {
			spec, err := res.SelfAddressPolicySpec()
			if err != nil {
				return err
			}
			for i, candidate := range spec.Candidates {
				if candidate.Interface != "" && !idx.Interfaces[candidate.Interface] {
					return fmt.Errorf("%s spec.candidates[%d] references missing Interface %q", res.ID(), i, candidate.Interface)
				}
				if candidate.DelegatedAddress != "" && !idx.DelegatedAddresses[candidate.DelegatedAddress] {
					return fmt.Errorf("%s spec.candidates[%d] references missing IPv6DelegatedAddress %q", res.ID(), i, candidate.DelegatedAddress)
				}
			}
		}
		if res.Kind == "DSLiteTunnel" {
			spec, err := res.DSLiteTunnelSpec()
			if err != nil {
				return err
			}
			if spec.LocalDelegatedAddress != "" && !idx.DelegatedAddresses[spec.LocalDelegatedAddress] {
				return fmt.Errorf("%s references missing local IPv6DelegatedAddress %q", res.ID(), spec.LocalDelegatedAddress)
			}
		}
		if res.Kind == "HybridRoute" {
			spec, err := res.HybridRouteSpec()
			if err != nil {
				return err
			}
			if _, ok := idx.OverlayPeers[spec.PeerRef]; !ok {
				return fmt.Errorf("%s spec.peerRef references missing OverlayPeer %q", res.ID(), spec.PeerRef)
			}
			if spec.HealthCheckRef != "" && !idx.HealthChecks[spec.HealthCheckRef] {
				return fmt.Errorf("%s spec.healthCheckRef references missing HealthCheck %q", res.ID(), spec.HealthCheckRef)
			}
		}
		if res.Kind == "OverlayPeer" {
			spec, err := res.OverlayPeerSpec()
			if err != nil {
				return err
			}
			switch spec.Underlay.Type {
			case "ipip", "gre", "fou", "gue":
				if !idx.TunnelInterfaces[spec.Underlay.Interface] {
					return fmt.Errorf("%s spec.underlay.interface references missing TunnelInterface %q", res.ID(), spec.Underlay.Interface)
				}
			}
		}
		if res.Kind == "AddressMobilityDomain" {
			spec, err := res.AddressMobilityDomainSpec()
			if err != nil {
				return err
			}
			if spec.PeerRef != "" {
				if _, ok := idx.OverlayPeers[spec.PeerRef]; !ok {
					return fmt.Errorf("%s spec.peerRef references missing OverlayPeer %q", res.ID(), spec.PeerRef)
				}
			}
		}
		if res.Kind == "RemoteAddressClaim" {
			spec, err := res.RemoteAddressClaimSpec()
			if err != nil {
				return err
			}
			domain, ok := idx.AddressMobilityDomains[spec.DomainRef]
			if !ok {
				return fmt.Errorf("%s spec.domainRef references missing AddressMobilityDomain %q", res.ID(), spec.DomainRef)
			}
			domainPrefix, err := netip.ParsePrefix(domain.Prefix)
			if err != nil {
				return fmt.Errorf("%s spec.domainRef references AddressMobilityDomain %q with invalid prefix %q", res.ID(), spec.DomainRef, domain.Prefix)
			}
			claimPrefix, err := netip.ParsePrefix(spec.Address)
			if err != nil {
				return fmt.Errorf("%s spec.address is invalid: %w", res.ID(), err)
			}
			if !domainPrefix.Masked().Contains(claimPrefix.Masked().Addr()) {
				return fmt.Errorf("%s spec.address %q is outside AddressMobilityDomain %q prefix %q", res.ID(), claimPrefix.Masked().String(), spec.DomainRef, domainPrefix.Masked().String())
			}
			if _, ok := idx.OverlayPeers[spec.Delivery.PeerRef]; !ok {
				return fmt.Errorf("%s spec.delivery.peerRef references missing OverlayPeer %q", res.ID(), spec.Delivery.PeerRef)
			}
			if spec.Capture.Type == "provider-secondary-ip" {
				if _, ok := idx.CloudProviderProfiles[spec.Capture.ProviderRef]; !ok {
					return fmt.Errorf("%s spec.capture.providerRef references missing CloudProviderProfile %q", res.ID(), spec.Capture.ProviderRef)
				}
			}
			if ref := captureActiveWhenVirtualAddressRef(spec.Capture.ActiveWhen); ref != "" {
				if _, ok := idx.VirtualAddresses[ref]; !ok {
					return fmt.Errorf("%s spec.capture.activeWhen.virtualAddressRef references missing VirtualAddress %q", res.ID(), ref)
				}
			}
		}
		if res.Kind == "MobilityPool" {
			spec, err := res.MobilityPoolSpec()
			if err != nil {
				return err
			}
			selfNode := mobilitySelfNode(router, spec.GroupRef)
			normalized, _, err := mobilityconfig.NormalizeMobilityPool(spec, selfNode)
			if err != nil {
				return fmt.Errorf("%s %w", res.ID(), err)
			}
			if err := validateMobilitySelfMemberCompleteness(res, normalized, selfNode); err != nil {
				return err
			}
			for i, member := range normalized.Members {
				if selfNode != "" && strings.TrimSpace(member.NodeRef) != selfNode {
					continue
				}
				if ref := captureActiveWhenVirtualAddressRef(member.Capture.ActiveWhen); ref != "" {
					if _, ok := idx.VirtualAddresses[ref]; !ok {
						return fmt.Errorf("%s spec.members[%d].capture.activeWhen.virtualAddressRef references missing VirtualAddress %q", res.ID(), i, ref)
					}
				}
			}
		}
		if res.Kind == "HealthCheck" {
			spec, err := res.HealthCheckSpec()
			if err != nil {
				return err
			}
			if spec.Interface != "" && !idx.Interfaces[spec.Interface] && !idx.DSLiteTunnels[spec.Interface] {
				return fmt.Errorf("%s references missing Interface, PPPoESession, or DSLiteTunnel %q", res.ID(), spec.Interface)
			}
		}
		if res.Kind == "EgressRoutePolicy" {
			spec, err := res.EgressRoutePolicySpec()
			if err != nil {
				return err
			}
			for i, candidate := range spec.Candidates {
				if candidate.Interface != "" && !idx.Interfaces[candidate.Interface] && !idx.DSLiteTunnels[candidate.Interface] {
					return fmt.Errorf("%s spec.candidates[%d] references missing Interface, PPPoESession, or DSLiteTunnel %q", res.ID(), i, candidate.Interface)
				}
				if candidate.HealthCheck != "" && !idx.HealthChecks[candidate.HealthCheck] {
					return fmt.Errorf("%s spec.candidates[%d] references missing HealthCheck %q", res.ID(), i, candidate.HealthCheck)
				}
				for j, target := range candidate.Targets {
					if target.EffectiveInterface() != "" && !idx.Interfaces[target.EffectiveInterface()] && !idx.DSLiteTunnels[target.EffectiveInterface()] {
						return fmt.Errorf("%s spec.candidates[%d].targets[%d] references missing Interface, PPPoESession, or DSLiteTunnel %q", res.ID(), i, j, target.EffectiveInterface())
					}
					if target.HealthCheck != "" && !idx.HealthChecks[target.HealthCheck] {
						return fmt.Errorf("%s spec.candidates[%d].targets[%d] references missing HealthCheck %q", res.ID(), i, j, target.HealthCheck)
					}
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
				case "Interface", "PPPoESession", "WireGuardInterface":
					if !idx.Interfaces[refName] && !idx.DSLiteTunnels[refName] {
						return fmt.Errorf("%s spec.idx.Interfaces[%d] references missing Interface, PPPoESession, WireGuardInterface, or DSLiteTunnel %q", res.ID(), i, refName)
					}
				case "DSLiteTunnel":
					if !idx.DSLiteTunnels[refName] {
						return fmt.Errorf("%s spec.idx.Interfaces[%d] references missing DSLiteTunnel %q", res.ID(), i, refName)
					}
				default:
					return fmt.Errorf("%s spec.idx.Interfaces[%d] has unsupported reference %q", res.ID(), i, name)
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
					return fmt.Errorf("%s spec.idx.Interfaces[%d] must reference Interface, got %q", res.ID(), i, name)
				}
				if !idx.Interfaces[refName] {
					return fmt.Errorf("%s spec.idx.Interfaces[%d] references missing Interface %q", res.ID(), i, name)
				}
			}
			for i, entry := range spec.Classification {
				if entry.IPv4Reservation != "" && !idx.DHCPv4Reservations[entry.IPv4Reservation] {
					return fmt.Errorf("%s spec.classification[%d].ipv4Reservation references missing DHCPv4Reservation %q", res.ID(), i, entry.IPv4Reservation)
				}
			}
		}
		if res.Kind == "PortForward" {
			spec, err := res.PortForwardSpec()
			if err != nil {
				return err
			}
			if err := validateIngressInterfaceRefs(res.ID(), spec.Listen, spec.Hairpin, idx.Interfaces); err != nil {
				return err
			}
		}
		if res.Kind == "IngressService" {
			spec, err := res.IngressServiceSpec()
			if err != nil {
				return err
			}
			if err := validateIngressInterfaceRefs(res.ID(), spec.Listen, spec.Hairpin, idx.Interfaces); err != nil {
				return err
			}
		}
		if res.Kind == "LocalServiceRedirect" {
			spec, err := res.LocalServiceRedirectSpec()
			if err != nil {
				return err
			}
			if !idx.Interfaces[spec.Interface] {
				return fmt.Errorf("%s spec.interface references missing Interface %q", res.ID(), spec.Interface)
			}
			for i, rule := range spec.Rules {
				kind, name := splitResourceRef(rule.DestinationSetRef)
				if kind != "IPAddressSet" {
					return fmt.Errorf("%s spec.rules[%d].destinationSetRef must reference IPAddressSet, got %q", res.ID(), i, rule.DestinationSetRef)
				}
				if !idx.IPAddressSets[name] {
					return fmt.Errorf("%s spec.rules[%d].destinationSetRef references missing IPAddressSet %q", res.ID(), i, rule.DestinationSetRef)
				}
			}
		}
		if res.Kind == "NAT44Rule" {
			spec, err := res.NAT44RuleSpec()
			if err != nil {
				return err
			}
			if spec.OutboundInterface != "" && !idx.Interfaces[spec.OutboundInterface] && !idx.DSLiteTunnels[spec.OutboundInterface] {
				return fmt.Errorf("%s references missing outbound Interface, PPPoESession, or DSLiteTunnel %q", res.ID(), spec.OutboundInterface)
			}
			if spec.EgressInterface != "" && !idx.Interfaces[spec.EgressInterface] && !idx.DSLiteTunnels[spec.EgressInterface] {
				return fmt.Errorf("%s references missing egress Interface, PPPoESession, or DSLiteTunnel %q", res.ID(), spec.EgressInterface)
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs, idx.IPAddressSets); err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs, idx.IPAddressSets); err != nil {
				return err
			}
		}
		if res.Kind == "EgressRoutePolicy" {
			spec, err := res.EgressRoutePolicySpec()
			if err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs, idx.IPAddressSets); err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs, idx.IPAddressSets); err != nil {
				return err
			}
		}
		if res.Kind == "FirewallRule" {
			spec, err := res.FirewallRuleSpec()
			if err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.destinationSetRefs", spec.DestinationSetRefs, idx.IPAddressSets); err != nil {
				return err
			}
			if err := validateIPAddressSetRefsExist(res.ID(), "spec.excludeDestinationSetRefs", spec.ExcludeDestinationSetRefs, idx.IPAddressSets); err != nil {
				return err
			}
			if spec.FromZone != "self" && !idx.Zones[spec.FromZone] {
				return fmt.Errorf("%s spec.fromZone references missing FirewallZone %q", res.ID(), spec.FromZone)
			}
			if spec.ToZone != "self" && !idx.Zones[spec.ToZone] {
				return fmt.Errorf("%s spec.toZone references missing FirewallZone %q", res.ID(), spec.ToZone)
			}
		}
		if res.Kind == "FirewallPolicy" {
			if _, err := res.FirewallPolicySpec(); err != nil {
				return err
			}
		}
		if res.Kind == "FirewallEventLog" {
			spec, err := res.FirewallEventLogSpec()
			if err != nil {
				return err
			}
			for i, name := range spec.FromZones {
				if name != "self" && !idx.Zones[name] {
					return fmt.Errorf("%s spec.fromZones[%d] references missing FirewallZone %q", res.ID(), i, name)
				}
			}
			for i, name := range spec.ToZones {
				if name != "self" && !idx.Zones[name] {
					return fmt.Errorf("%s spec.toZones[%d] references missing FirewallZone %q", res.ID(), i, name)
				}
			}
			for i, ref := range spec.Rules {
				kind, name := splitKindNameRef(ref, "FirewallRule")
				if kind != "FirewallRule" || !idx.FirewallRules[name] {
					return fmt.Errorf("%s spec.rules[%d] references missing FirewallRule %q", res.ID(), i, ref)
				}
			}
			for i, ref := range spec.Sinks {
				kind, name := splitKindNameRef(ref, "LogSink")
				if kind != "LogSink" || !idx.LogSinks[name] {
					return fmt.Errorf("%s spec.sinks[%d] references missing LogSink %q", res.ID(), i, ref)
				}
			}
			if strings.TrimSpace(spec.Retention) != "" {
				kind, name := splitKindNameRef(spec.Retention, "LogRetention")
				if kind != "LogRetention" || !idx.LogRetentions[name] {
					return fmt.Errorf("%s spec.retention references missing LogRetention %q", res.ID(), spec.Retention)
				}
			}
		}
	}
	for name := range idx.BFDSpecs {
		if bfdRefs[name] == 0 {
			return fmt.Errorf("%s/BFD/%s is not referenced by any BGPPeer spec.bfd", api.NetAPIVersion, name)
		}
	}
	return nil
}

func captureActiveWhenVirtualAddressRef(activeWhen api.CaptureActiveWhen) string {
	ref := strings.TrimSpace(activeWhen.VirtualAddressRef)
	return strings.TrimPrefix(ref, "VirtualAddress/")
}

func validateMobilitySelfMemberCompleteness(res api.Resource, spec api.MobilityPoolSpec, selfNode string) error {
	selfNode = strings.TrimSpace(selfNode)
	if selfNode == "" || effectiveMobilityDeliveryMode(spec) != "bgp" {
		return nil
	}
	for i, member := range spec.Members {
		if strings.TrimSpace(member.NodeRef) != selfNode {
			continue
		}
		if strings.TrimSpace(member.Role) == "cloud" && strings.TrimSpace(member.Capture.Type) == "" {
			return fmt.Errorf("%s spec.members[%d] is the local cloud member %q and must resolve provider-secondary-ip capture details from capture or profileRef", res.ID(), i, selfNode)
		}
		return nil
	}
	return nil
}

func mobilitySelfNode(router *api.Router, groupRef string) string {
	groupRef = strings.TrimSpace(groupRef)
	if router == nil || groupRef == "" {
		return ""
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FederationAPIVersion || res.Kind != "EventGroup" || res.Metadata.Name != groupRef {
			continue
		}
		spec, err := res.EventGroupSpec()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(spec.NodeName)
	}
	return ""
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

func validManagementInterfaceName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && !strings.ContainsAny(name, "/ \t\n\r\x00")
}

func normalizeOUIPrefix(value string) (string, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("must use three hex octets such as 18:ec:e7")
	}
	for i, part := range parts {
		if len(part) != 2 {
			return "", fmt.Errorf("octet %d must contain two hex digits", i)
		}
		if _, err := strconv.ParseUint(part, 16, 8); err != nil {
			return "", err
		}
		parts[i] = strings.ToLower(part)
	}
	return strings.Join(parts, ":"), nil
}

func bfdSpecMatchesBGPPeer(spec api.BFDSpec, peerName string, peerAddresses []string) bool {
	peer := strings.TrimSpace(spec.Peer)
	if kind, name, ok := strings.Cut(peer, "/"); ok {
		return kind == "BGPPeer" && name == peerName
	}
	for _, address := range peerAddresses {
		if peer == strings.TrimSpace(address) {
			return true
		}
	}
	return false
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

	validators := []func(api.Resource, platform.OS) (bool, error){
		validatePluginResource,
		validateConfigResource,
		validateSystemResource,
		validateInterfaceResource,
		validateWANResource,
		validateDNSResource,
		validateDHCPResource,
		validateRouteResource,
		validateHybridResource,
		validateEventResource,
		validateMobilityResource,
		validateFirewallResource,
	}
	for _, validate := range validators {
		handled, err := validate(res, targetOS)
		if err != nil {
			return err
		}
		if handled {
			return validateResourceWhens(res)
		}
	}
	return fmt.Errorf("unsupported resource kind %s in %s", res.Kind, res.ID())
}

func validateResourceWhens(res api.Resource) error {
	for _, item := range resourceWhens(res) {
		if err := validateResourceWhen(item.path, item.when); err != nil {
			return err
		}
	}
	return nil
}

type resourceWhenRef struct {
	path string
	when api.ResourceWhenSpec
}

func resourceWhens(res api.Resource) []resourceWhenRef {
	switch res.Kind {
	case "ObservabilityPipeline":
		spec, _ := res.ObservabilityPipelineSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "RouterdCluster":
		spec, _ := res.RouterdClusterSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "VirtualAddress":
		spec, _ := res.VirtualAddressSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "BGPRouter":
		spec, _ := res.BGPRouterSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "BGPPeer":
		spec, _ := res.BGPPeerSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "BFD":
		spec, _ := res.BFDSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "TailscaleNode":
		spec, _ := res.TailscaleNodeSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "NTPServer":
		spec, _ := res.NTPServerSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DHCPv4Client":
		spec, _ := res.DHCPv4ClientSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "ClusterNetworkRoute":
		spec, _ := res.ClusterNetworkRouteSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DHCPv4Server":
		spec, _ := res.DHCPv4ServerSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DHCPv4Reservation":
		spec, _ := res.DHCPv4ReservationSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "IPv6DelegatedAddress":
		spec, _ := res.IPv6DelegatedAddressSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DHCPv6Server":
		spec, _ := res.DHCPv6ServerSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DHCPv4ServerLeaseSync":
		spec, _ := res.DHCPv4ServerLeaseSyncSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DHCPv6ServerLeaseSync":
		spec, _ := res.DHCPv6ServerLeaseSyncSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DHCPv6PrefixDelegationLeaseSync":
		spec, _ := res.DHCPv6PrefixDelegationLeaseSyncSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DHCPv6PrefixDelegation":
		spec, _ := res.DHCPv6PrefixDelegationSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "IPv6RouterAdvertisement":
		spec, _ := res.IPv6RouterAdvertisementSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DSLiteTunnel":
		spec, _ := res.DSLiteTunnelSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DNSForwarder":
		spec, _ := res.DNSForwarderSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DNSResolver":
		spec, _ := res.DNSResolverSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "DNSUpstream":
		spec, _ := res.DNSUpstreamSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "EventGroup":
		spec, _ := res.EventGroupSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "HealthCheck":
		spec, _ := res.HealthCheckSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "NAT44Rule":
		spec, _ := res.NAT44RuleSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "NAT44SessionSync":
		spec, _ := res.NAT44SessionSyncSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "PortForward":
		spec, _ := res.PortForwardSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "IngressService":
		spec, _ := res.IngressServiceSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "IPAddressSet":
		spec, _ := res.IPAddressSetSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "LocalServiceRedirect":
		spec, _ := res.LocalServiceRedirectSpec()
		return []resourceWhenRef{{path: res.ID() + " spec.when", when: spec.When}}
	case "EgressRoutePolicy":
		spec, _ := res.EgressRoutePolicySpec()
		out := make([]resourceWhenRef, 0, len(spec.Candidates)+1)
		out = append(out, resourceWhenRef{path: res.ID() + " spec.when", when: spec.When})
		for i, candidate := range spec.Candidates {
			out = append(out, resourceWhenRef{path: fmt.Sprintf("%s spec.candidates[%d].when", res.ID(), i), when: candidate.When})
		}
		return out
	default:
		return nil
	}
}

func validateResourceWhen(path string, when api.ResourceWhenSpec) error {
	if isZeroResourceWhen(when) {
		return nil
	}
	forms := 0
	if len(when.State) > 0 {
		forms++
	}
	if len(when.All) > 0 {
		forms++
	}
	if len(when.Any) > 0 {
		forms++
	}
	if forms != 1 {
		return fmt.Errorf("%s must set exactly one of state, all, or any", path)
	}
	for name, match := range when.State {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s state keys must not be empty", path)
		}
		switch match.Status {
		case "", routerstate.StatusSet, routerstate.StatusUnset, routerstate.StatusUnknown:
		default:
			return fmt.Errorf("%s state[%q].status must be set, unset, or unknown", path, name)
		}
		if match.For != "" {
			if _, err := time.ParseDuration(match.For); err != nil {
				return fmt.Errorf("%s state[%q].for is invalid: %w", path, name, err)
			}
		}
	}
	for i, child := range when.All {
		if isZeroResourceWhen(child) {
			return fmt.Errorf("%s all[%d] must not be empty", path, i)
		}
		if err := validateResourceWhen(fmt.Sprintf("%s all[%d]", path, i), child); err != nil {
			return err
		}
	}
	for i, child := range when.Any {
		if isZeroResourceWhen(child) {
			return fmt.Errorf("%s any[%d] must not be empty", path, i)
		}
		if err := validateResourceWhen(fmt.Sprintf("%s any[%d]", path, i), child); err != nil {
			return err
		}
	}
	return nil
}

func isZeroResourceWhen(when api.ResourceWhenSpec) bool {
	return len(when.State) == 0 && len(when.All) == 0 && len(when.Any) == 0
}
