// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func validateHybridResource(res api.Resource, _ platform.OS) (bool, error) {
	switch res.Kind {
	case "OverlayPeer":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.OverlayPeerSpec()
		if err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.Role) {
		case "onprem", "cloud":
		default:
			return true, fmt.Errorf("%s spec.role must be onprem or cloud", res.ID())
		}
		if strings.TrimSpace(spec.NodeID) == "" {
			return true, fmt.Errorf("%s spec.nodeID is required", res.ID())
		}
		switch strings.TrimSpace(spec.Underlay.Type) {
		case "wireguard", "tailscale", "ipsec", "route":
		default:
			return true, fmt.Errorf("%s spec.underlay.type must be wireguard, tailscale, ipsec, or route", res.ID())
		}
		if spec.Underlay.Type == "wireguard" && strings.TrimSpace(spec.Underlay.Interface) == "" {
			return true, fmt.Errorf("%s spec.underlay.interface is required when spec.underlay.type is wireguard", res.ID())
		}
		if address := strings.TrimSpace(spec.Underlay.Address); address != "" {
			if _, err := netip.ParseAddr(address); err != nil {
				return true, fmt.Errorf("%s spec.underlay.address must be an IP address", res.ID())
			}
		}
	case "HybridRoute":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.HybridRouteSpec()
		if err != nil {
			return true, err
		}
		if len(spec.DestinationCIDRs) == 0 {
			return true, fmt.Errorf("%s spec.destinationCIDRs is required", res.ID())
		}
		for i, destination := range spec.DestinationCIDRs {
			if err := validateHybridDestinationCIDR(destination); err != nil {
				return true, fmt.Errorf("%s spec.destinationCIDRs[%d]: %w", res.ID(), i, err)
			}
		}
		if strings.TrimSpace(spec.PeerRef) == "" {
			return true, fmt.Errorf("%s spec.peerRef is required", res.ID())
		}
		switch table := strings.TrimSpace(spec.Install.Table); table {
		case "", "main":
		default:
			return true, fmt.Errorf("%s spec.install.table must be empty or main; HybridRoute MVP lowers to IPv4Route, which installs only in the main table", res.ID())
		}
		if spec.Install.Metric < 0 {
			return true, fmt.Errorf("%s spec.install.metric must be >= 0", res.ID())
		}
	case "AddressMobilityDomain":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.AddressMobilityDomainSpec()
		if err != nil {
			return true, err
		}
		if err := validateAddressMobilityDomainPrefix(spec.Prefix); err != nil {
			return true, fmt.Errorf("%s spec.prefix: %w", res.ID(), err)
		}
		switch strings.TrimSpace(spec.Mode) {
		case "selective-address":
		case "full-l2":
			return true, fmt.Errorf("%s spec.mode full L2 extension is not supported; routerd implements Selective Address Mobility", res.ID())
		default:
			return true, fmt.Errorf("%s spec.mode full L2 extension is not supported; routerd implements Selective Address Mobility", res.ID())
		}
	case "CloudProviderProfile":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.CloudProviderProfileSpec()
		if err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.Provider) {
		case "azure", "aws", "oci", "gcp":
		default:
			return true, fmt.Errorf("%s spec.provider must be azure, aws, oci, or gcp", res.ID())
		}
		if len(spec.Capabilities) == 0 {
			return true, fmt.Errorf("%s spec.capabilities is required", res.ID())
		}
		for i, capability := range spec.Capabilities {
			if strings.TrimSpace(capability) == "" {
				return true, fmt.Errorf("%s spec.capabilities[%d] must not be empty", res.ID(), i)
			}
		}
		switch strings.TrimSpace(spec.Auth.Mode) {
		case "external-command":
			if strings.TrimSpace(spec.Auth.Command) == "" {
				return true, fmt.Errorf("%s spec.auth.command is required when spec.auth.mode is external-command", res.ID())
			}
		default:
			return true, fmt.Errorf("%s spec.auth.mode must be external-command", res.ID())
		}
	case "RemoteAddressClaim":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.RemoteAddressClaimSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.DomainRef) == "" {
			return true, fmt.Errorf("%s spec.domainRef is required", res.ID())
		}
		if err := validateRemoteClaimAddress(spec.Address); err != nil {
			return true, fmt.Errorf("%s spec.address: %w", res.ID(), err)
		}
		switch strings.TrimSpace(spec.OwnerSide) {
		case "cloud", "onprem":
		default:
			return true, fmt.Errorf("%s spec.ownerSide must be cloud or onprem", res.ID())
		}
		switch strings.TrimSpace(spec.Capture.Type) {
		case "provider-secondary-ip":
			if strings.TrimSpace(spec.Capture.ProviderRef) == "" {
				return true, fmt.Errorf("%s spec.capture.providerRef is required when spec.capture.type is provider-secondary-ip", res.ID())
			}
			if strings.TrimSpace(spec.Capture.ProviderMode) == "" {
				return true, fmt.Errorf("%s spec.capture.providerMode is required when spec.capture.type is provider-secondary-ip", res.ID())
			}
			if strings.TrimSpace(spec.Capture.NICRef) == "" {
				return true, fmt.Errorf("%s spec.capture.nicRef is required when spec.capture.type is provider-secondary-ip", res.ID())
			}
		case "proxy-arp":
			if strings.TrimSpace(spec.Capture.Interface) == "" {
				return true, fmt.Errorf("%s spec.capture.interface is required when spec.capture.type is proxy-arp", res.ID())
			}
		case "":
			return true, fmt.Errorf("%s spec.capture.type is required", res.ID())
		case "static-host-route", "garp":
			return true, fmt.Errorf("%s spec.capture.type %q is reserved/not implemented in MVP", res.ID(), strings.TrimSpace(spec.Capture.Type))
		default:
			return true, fmt.Errorf("%s spec.capture.type %q is reserved/not implemented in MVP", res.ID(), strings.TrimSpace(spec.Capture.Type))
		}
		switch strings.TrimSpace(spec.Delivery.Mode) {
		case "route":
		default:
			return true, fmt.Errorf("%s spec.delivery.mode must be route", res.ID())
		}
		if strings.TrimSpace(spec.Delivery.PeerRef) == "" {
			return true, fmt.Errorf("%s spec.delivery.peerRef is required", res.ID())
		}
	default:
		return false, nil
	}
	return true, nil
}

func validateAddressMobilityDomainPrefix(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("is required")
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return fmt.Errorf("must be a valid IPv4 CIDR: %w", err)
	}
	if !prefix.Addr().Is4() {
		return fmt.Errorf("must be an IPv4 CIDR")
	}
	return nil
}

func validateRemoteClaimAddress(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("is required")
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return fmt.Errorf("must be a valid IPv4 /32 CIDR: %w", err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return fmt.Errorf("must be an IPv4 /32 CIDR")
	}
	return nil
}

func validateHybridDestinationCIDR(value string) error {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "default") {
		return fmt.Errorf("default routes are not allowed")
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return fmt.Errorf("must be a valid CIDR: %w", err)
	}
	masked := prefix.Masked()
	if (masked.Addr().Is4() && masked.Bits() == 0) || (masked.Addr().Is6() && masked.Bits() == 0) {
		return fmt.Errorf("default routes are not allowed")
	}
	if !masked.Addr().Is4() {
		return fmt.Errorf("must be an IPv4 CIDR; HybridRoute MVP lowers to IPv4Route")
	}
	return nil
}
