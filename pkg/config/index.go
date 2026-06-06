// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strconv"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

type RouterIndex struct {
	Seen                       map[string]bool
	BaseInterfaces             map[string]bool
	Interfaces                 map[string]bool
	WireGuardInterfaces        map[string]bool
	TunnelInterfaces           map[string]bool
	DHCPv4ServerSpecs          map[string]api.DHCPv4ServerSpec
	DirectDHCPv4Servers        map[string]bool
	DHCPv4Reservations         map[string]bool
	IPv6RAs                    map[string]bool
	PrefixDelegations          map[string]bool
	DelegatedAddresses         map[string]bool
	DelegatedAddressInterfaces map[string]string
	SelfAddressPolicies        map[string]bool
	DSLiteTunnels              map[string]bool
	OverlayPeers               map[string]api.OverlayPeerSpec
	VirtualAddresses           map[string]api.VirtualAddressSpec
	AddressMobilityDomains     map[string]api.AddressMobilityDomainSpec
	CloudProviderProfiles      map[string]api.CloudProviderProfileSpec
	HealthChecks               map[string]bool
	BGPRouters                 map[string]bool
	BGPRouterSpecs             map[string]api.BGPRouterSpec
	BFDSpecs                   map[string]api.BFDSpec
	VRFs                       map[string]bool
	Zones                      map[string]bool
	DNSZones                   map[string]bool
	DNSResolvers               map[string]api.DNSResolverSpec
	DNSForwarders              map[string]api.DNSForwarderSpec
	DNSUpstreams               map[string]api.DNSUpstreamSpec
	IPAddressSets              map[string]bool
	Telemetries                map[string]bool
	LogSinks                   map[string]bool
	LogRetentions              map[string]bool
	FirewallRules              map[string]bool
	UDPListenPorts             map[int]string
	StaticByInterfaceAddress   map[string]string
	StaticIPv4ByName           map[string]staticIPv4IndexEntry
	VRRPByInterfaceVRID        map[string]string
	ProtectedInterfaces        map[string]bool
	DHCPv6AddressByInterface   map[string]dhcpv6AddressIndexEntry
	ExternalPDByInterface      map[string]externalPDIndexEntry
}

type staticIPv4IndexEntry struct {
	id      string
	iface   string
	address string
}

type dhcpv6AddressIndexEntry struct {
	id     string
	client string
}

type externalPDIndexEntry struct {
	id     string
	client string
}

func newRouterIndex(router *api.Router) *RouterIndex {
	idx := &RouterIndex{
		Seen:                       map[string]bool{},
		BaseInterfaces:             map[string]bool{},
		Interfaces:                 map[string]bool{},
		WireGuardInterfaces:        map[string]bool{},
		TunnelInterfaces:           map[string]bool{},
		DHCPv4ServerSpecs:          map[string]api.DHCPv4ServerSpec{},
		DirectDHCPv4Servers:        map[string]bool{},
		DHCPv4Reservations:         map[string]bool{},
		IPv6RAs:                    map[string]bool{},
		PrefixDelegations:          map[string]bool{},
		DelegatedAddresses:         map[string]bool{},
		DelegatedAddressInterfaces: map[string]string{},
		SelfAddressPolicies:        map[string]bool{},
		DSLiteTunnels:              map[string]bool{},
		OverlayPeers:               map[string]api.OverlayPeerSpec{},
		VirtualAddresses:           map[string]api.VirtualAddressSpec{},
		AddressMobilityDomains:     map[string]api.AddressMobilityDomainSpec{},
		CloudProviderProfiles:      map[string]api.CloudProviderProfileSpec{},
		HealthChecks:               map[string]bool{},
		BGPRouters:                 map[string]bool{},
		BGPRouterSpecs:             map[string]api.BGPRouterSpec{},
		BFDSpecs:                   map[string]api.BFDSpec{},
		VRFs:                       map[string]bool{},
		Zones:                      map[string]bool{},
		DNSZones:                   map[string]bool{},
		DNSResolvers:               map[string]api.DNSResolverSpec{},
		DNSForwarders:              map[string]api.DNSForwarderSpec{},
		DNSUpstreams:               map[string]api.DNSUpstreamSpec{},
		IPAddressSets:              map[string]bool{},
		Telemetries:                map[string]bool{},
		LogSinks:                   map[string]bool{},
		LogRetentions:              map[string]bool{},
		FirewallRules:              map[string]bool{},
		UDPListenPorts:             map[int]string{},
		StaticByInterfaceAddress:   map[string]string{},
		StaticIPv4ByName:           map[string]staticIPv4IndexEntry{},
		VRRPByInterfaceVRID:        map[string]string{},
		ProtectedInterfaces:        map[string]bool{},
		DHCPv6AddressByInterface:   map[string]dhcpv6AddressIndexEntry{},
		ExternalPDByInterface:      map[string]externalPDIndexEntry{},
	}

	for _, name := range router.Spec.Apply.ProtectedInterfaces {
		idx.ProtectedInterfaces[name] = true
	}
	return idx
}

func (idx *RouterIndex) build(router *api.Router, targetOS platform.OS) error {
	for _, res := range router.Spec.Resources {
		if err := validateResource(res, targetOS); err != nil {
			return err
		}
		if idx.Seen[res.ID()] {
			return fmt.Errorf("duplicate resource %s", res.ID())
		}
		idx.Seen[res.ID()] = true
		if res.APIVersion == api.NetAPIVersion && res.Kind == "Interface" {
			idx.BaseInterfaces[res.Metadata.Name] = true
			idx.Interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.ObservabilityAPIVersion && res.Kind == "Telemetry" {
			idx.Telemetries[res.Metadata.Name] = true
		}
		if res.APIVersion == api.SystemAPIVersion && res.Kind == "LogSink" {
			idx.LogSinks[res.Metadata.Name] = true
		}
		if res.APIVersion == api.SystemAPIVersion && res.Kind == "LogRetention" {
			idx.LogRetentions[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "Bridge" {
			idx.Interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VXLANSegment" {
			idx.Interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "WireGuardInterface" {
			idx.WireGuardInterfaces[res.Metadata.Name] = true
			idx.Interfaces[res.Metadata.Name] = true
			spec, err := res.WireGuardInterfaceSpec()
			if err != nil {
				return err
			}
			if spec.ListenPort != 0 {
				if existing := idx.UDPListenPorts[spec.ListenPort]; existing != "" {
					return fmt.Errorf("%s spec.listenPort %d conflicts with %s", res.ID(), spec.ListenPort, existing)
				}
				idx.UDPListenPorts[spec.ListenPort] = res.ID()
			}
		}
		if res.APIVersion == api.HybridAPIVersion && res.Kind == "TunnelInterface" {
			idx.TunnelInterfaces[res.Metadata.Name] = true
			idx.Interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "TailscaleNode" {
			spec, err := res.TailscaleNodeSpec()
			if err != nil {
				return err
			}
			if defaultString(spec.State, "present") != "absent" {
				if existing := idx.UDPListenPorts[41641]; existing != "" {
					return fmt.Errorf("%s reserves Tailscale UDP port 41641 which conflicts with %s", res.ID(), existing)
				}
				idx.UDPListenPorts[41641] = res.ID()
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VRF" {
			idx.Interfaces[res.Metadata.Name] = true
			idx.VRFs[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VXLANTunnel" {
			idx.Interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "PPPoESession" {
			idx.Interfaces[res.Metadata.Name] = true
		}
		if res.APIVersion == api.SystemAPIVersion && res.Kind == "NetworkAdoption" {
			spec, err := res.NetworkAdoptionSpec()
			if err != nil {
				return err
			}
			if defaultString(spec.State, "present") != "absent" && spec.Interface != "" && idx.ProtectedInterfaces[spec.Interface] {
				return fmt.Errorf("%s must not adopt protected interface %q; remove it from spec.reconcile.protectedInterfaces first", res.ID(), spec.Interface)
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv4Server" {
			spec, err := res.DHCPv4ServerSpec()
			if err != nil {
				return err
			}
			idx.DHCPv4ServerSpecs[res.Metadata.Name] = spec
			if spec.Interface != "" {
				idx.DirectDHCPv4Servers[res.Metadata.Name] = true
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6RouterAdvertisement" {
			idx.IPv6RAs[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv4Reservation" {
			idx.DHCPv4Reservations[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6PrefixDelegation" {
			idx.PrefixDelegations[res.Metadata.Name] = true
			spec, err := res.DHCPv6PrefixDelegationSpec()
			if err != nil {
				return err
			}
			if isExternalIPv6PDClient(spec.Client) {
				idx.ExternalPDByInterface[spec.Interface] = externalPDIndexEntry{id: res.ID(), client: spec.Client}
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DHCPv6Address" {
			spec, err := res.DHCPv6AddressSpec()
			if err != nil {
				return err
			}
			idx.DHCPv6AddressByInterface[spec.Interface] = dhcpv6AddressIndexEntry{id: res.ID(), client: defaultString(spec.Client, "networkd")}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPv6DelegatedAddress" {
			idx.DelegatedAddresses[res.Metadata.Name] = true
			spec, err := res.IPv6DelegatedAddressSpec()
			if err != nil {
				return err
			}
			idx.DelegatedAddressInterfaces[res.Metadata.Name] = spec.Interface
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "SelfAddressPolicy" {
			idx.SelfAddressPolicies[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DNSZone" {
			idx.DNSZones[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DNSResolver" {
			spec, err := res.DNSResolverSpec()
			if err != nil {
				return err
			}
			idx.DNSResolvers[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DNSForwarder" {
			spec, err := res.DNSForwarderSpec()
			if err != nil {
				return err
			}
			idx.DNSForwarders[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DNSUpstream" {
			spec, err := res.DNSUpstreamSpec()
			if err != nil {
				return err
			}
			idx.DNSUpstreams[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "DSLiteTunnel" {
			idx.DSLiteTunnels[res.Metadata.Name] = true
		}
		if res.APIVersion == api.HybridAPIVersion && res.Kind == "OverlayPeer" {
			spec, err := res.OverlayPeerSpec()
			if err != nil {
				return err
			}
			idx.OverlayPeers[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VirtualAddress" {
			spec, err := res.VirtualAddressSpec()
			if err != nil {
				return err
			}
			idx.VirtualAddresses[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.HybridAPIVersion && res.Kind == "AddressMobilityDomain" {
			spec, err := res.AddressMobilityDomainSpec()
			if err != nil {
				return err
			}
			idx.AddressMobilityDomains[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.HybridAPIVersion && res.Kind == "CloudProviderProfile" {
			spec, err := res.CloudProviderProfileSpec()
			if err != nil {
				return err
			}
			idx.CloudProviderProfiles[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "HealthCheck" {
			idx.HealthChecks[res.Metadata.Name] = true
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "IPAddressSet" {
			idx.IPAddressSets[res.Metadata.Name] = true
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
			if existing := idx.StaticByInterfaceAddress[key]; existing != "" {
				return fmt.Errorf("%s duplicates IPv4 static address already declared by %s", res.ID(), existing)
			}
			idx.StaticByInterfaceAddress[key] = res.ID()
			idx.StaticIPv4ByName[res.Metadata.Name] = staticIPv4IndexEntry{id: res.ID(), iface: spec.Interface, address: prefix.Masked().String()}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "VirtualAddress" {
			spec, err := res.VirtualAddressSpec()
			if err != nil {
				return err
			}
			if defaultString(spec.Mode, "static") == "vrrp" {
				key := res.Kind + "|" + spec.Family + "|" + spec.Interface + "|" + strconv.Itoa(spec.VRRP.VirtualRouterID)
				if existing := idx.VRRPByInterfaceVRID[key]; existing != "" {
					return fmt.Errorf("%s spec.vrrp.virtualRouterID conflicts with %s on interface %q", res.ID(), existing, spec.Interface)
				}
				idx.VRRPByInterfaceVRID[key] = res.ID()
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "BGPRouter" {
			idx.BGPRouters[res.Metadata.Name] = true
			if spec, err := res.BGPRouterSpec(); err == nil {
				idx.BGPRouterSpecs[res.Metadata.Name] = spec
			}
		}
		if res.APIVersion == api.NetAPIVersion && res.Kind == "BFD" {
			spec, err := res.BFDSpec()
			if err != nil {
				return err
			}
			idx.BFDSpecs[res.Metadata.Name] = spec
		}
		if res.APIVersion == api.FirewallAPIVersion && res.Kind == "FirewallZone" {
			idx.Zones[res.Metadata.Name] = true
		}
		if res.APIVersion == api.FirewallAPIVersion && res.Kind == "FirewallRule" {
			idx.FirewallRules[res.Metadata.Name] = true
		}
	}
	return nil
}
