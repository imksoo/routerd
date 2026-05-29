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
	case "CloudAddressClaim":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.CloudAddressClaimSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.ProviderRef) == "" {
			return true, fmt.Errorf("%s spec.providerRef is required", res.ID())
		}
		if err := validateCloudClaimAddress(spec.Address); err != nil {
			return true, fmt.Errorf("%s spec.address: %w", res.ID(), err)
		}
		switch strings.TrimSpace(spec.CloudAttachment.Type) {
		case "secondary-private-ip":
		case "":
			return true, fmt.Errorf("%s spec.cloudAttachment.type is required", res.ID())
		default:
			return true, fmt.Errorf("%s spec.cloudAttachment.type must be secondary-private-ip", res.ID())
		}
		switch strings.TrimSpace(spec.Delivery.Mode) {
		case "route":
		default:
			return true, fmt.Errorf("%s spec.delivery.mode must be route", res.ID())
		}
		if target := strings.TrimSpace(spec.Delivery.TargetAddress); target != "" {
			if _, err := netip.ParseAddr(target); err != nil {
				return true, fmt.Errorf("%s spec.delivery.targetAddress must be an IP address", res.ID())
			}
		}
	default:
		return false, nil
	}
	return true, nil
}

func validateCloudClaimAddress(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("is required")
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return nil
	}
	if _, err := netip.ParsePrefix(value); err == nil {
		return nil
	}
	return fmt.Errorf("must be an IP address or CIDR")
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
