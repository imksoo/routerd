// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strconv"
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
		case "wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue":
		default:
			return true, fmt.Errorf("%s spec.underlay.type must be wireguard, tailscale, ipsec, route, ipip, gre, fou, or gue", res.ID())
		}
		switch spec.Underlay.Type {
		case "wireguard", "ipip", "gre", "fou", "gue":
			if strings.TrimSpace(spec.Underlay.Interface) == "" {
				return true, fmt.Errorf("%s spec.underlay.interface is required when spec.underlay.type is %s", res.ID(), spec.Underlay.Type)
			}
		}
		if spec.PathMTU.ForceFragmentIPv4 {
			switch strings.TrimSpace(spec.Underlay.Type) {
			case "wireguard", "ipip", "gre", "fou", "gue":
			default:
				return true, fmt.Errorf("%s spec.pathMTU.forceFragmentIPv4 is supported only for underlay.type wireguard, ipip, gre, fou, or gue", res.ID())
			}
		}
		if address := strings.TrimSpace(spec.Underlay.Address); address != "" {
			if _, err := netip.ParseAddr(address); err != nil {
				return true, fmt.Errorf("%s spec.underlay.address must be an IP address", res.ID())
			}
		}
	case "TunnelInterface":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.TunnelInterfaceSpec()
		if err != nil {
			return true, err
		}
		mode := strings.TrimSpace(spec.Mode)
		switch mode {
		case "ipip", "gre", "fou", "gue":
		default:
			return true, fmt.Errorf("%s spec.mode must be ipip, gre, fou, or gue", res.ID())
		}
		if err := validateTunnelEndpointOrSource(res.ID(), "local", spec.Local, spec.LocalFrom); err != nil {
			return true, err
		}
		if err := validateTunnelEndpointOrSource(res.ID(), "remote", spec.Remote, spec.RemoteFrom); err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.Address) != "" {
			if err := validateTunnelAddress(spec.Address); err != nil {
				return true, fmt.Errorf("%s spec.address: %w", res.ID(), err)
			}
		}
		if spec.MTU != 0 && (spec.MTU < 576 || spec.MTU > 9216) {
			return true, fmt.Errorf("%s spec.mtu must be within 576-9216", res.ID())
		}
		if spec.TTL != 0 && (spec.TTL < 1 || spec.TTL > 255) {
			return true, fmt.Errorf("%s spec.ttl must be within 1-255", res.ID())
		}
		if spec.Key < 0 {
			return true, fmt.Errorf("%s spec.key must be >= 0", res.ID())
		}
		if strconv.IntSize >= 64 && int64(spec.Key) > 4294967295 {
			return true, fmt.Errorf("%s spec.key must be <= 4294967295", res.ID())
		}
		if mode != "gre" && spec.Key != 0 {
			return true, fmt.Errorf("%s spec.key is only supported when spec.mode is gre", res.ID())
		}
		if mode == "fou" || mode == "gue" {
			if spec.EncapSport < 1 || spec.EncapSport > 65535 {
				return true, fmt.Errorf("%s spec.encapSport is required and must be within 1-65535 when spec.mode is %s", res.ID(), mode)
			}
			if spec.EncapDport < 1 || spec.EncapDport > 65535 {
				return true, fmt.Errorf("%s spec.encapDport is required and must be within 1-65535 when spec.mode is %s", res.ID(), mode)
			}
		} else if spec.EncapSport != 0 || spec.EncapDport != 0 {
			return true, fmt.Errorf("%s spec.encapSport/spec.encapDport are only supported when spec.mode is fou or gue", res.ID())
		}
		if !spec.TrustedUnderlay {
			return true, fmt.Errorf("%s spec.trustedUnderlay must be true; ipip/gre/fou/gue tunnels are unencrypted and unauthenticated and require a trusted underlay", res.ID())
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
	case "SAMTransportProfile":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.SAMTransportProfileSpec()
		if err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.Mode) {
		case "ipip", "gre":
		default:
			return true, fmt.Errorf("%s spec.mode must be ipip or gre", res.ID())
		}
		encryption := strings.TrimSpace(spec.Encryption)
		if encryption == "" {
			encryption = "none"
		}
		switch encryption {
		case "none", "wireguard":
		default:
			return true, fmt.Errorf("%s spec.encryption must be none or wireguard", res.ID())
		}
		if strings.TrimSpace(spec.LocalNodeID) == "" {
			return true, fmt.Errorf("%s spec.localNodeID is required", res.ID())
		}
		if err := validateSAMTransportCIDR(res.ID()+" spec.innerCIDR", spec.InnerCIDR); err != nil {
			return true, err
		}
		switch role := strings.TrimSpace(spec.PeerRole); role {
		case "", "onprem", "cloud":
		default:
			return true, fmt.Errorf("%s spec.peerRole must be onprem or cloud when set", res.ID())
		}
		if strings.TrimSpace(spec.BGP.RouterRef) != "" && spec.BGP.PeerASN == 0 {
			allPeersOverrideASN := true
			for _, peer := range spec.Peers {
				if peer.PeerASN == 0 {
					allPeersOverrideASN = false
					break
				}
			}
			if !allPeersOverrideASN {
				return true, fmt.Errorf("%s spec.bgp.peerASN is required unless every peer sets peerASN", res.ID())
			}
		}
		if encryption == "none" {
			if err := validateTunnelEndpointOrSource(res.ID(), "localEndpoint", spec.LocalEndpoint, spec.LocalEndpointFrom); err != nil {
				return true, err
			}
		} else {
			if err := validateSAMTransportCIDR(res.ID()+" spec.wireGuard.transportCIDR", spec.WireGuard.TransportCIDR); err != nil {
				return true, err
			}
			if strings.TrimSpace(spec.WireGuard.LocalAddress) != "" {
				if err := validateSAMTransportAddress(res.ID()+" spec.wireGuard.localAddress", spec.WireGuard.LocalAddress); err != nil {
					return true, err
				}
			}
		}
		if len(spec.Peers) == 0 {
			return true, fmt.Errorf("%s spec.peers is required", res.ID())
		}
		peerNames := map[string]bool{}
		for i, peer := range spec.Peers {
			label := fmt.Sprintf("%s spec.peers[%d]", res.ID(), i)
			if strings.TrimSpace(peer.Name) == "" {
				return true, fmt.Errorf("%s.name is required", label)
			}
			if peerNames[peer.Name] {
				return true, fmt.Errorf("%s.name %q is duplicated", label, peer.Name)
			}
			peerNames[peer.Name] = true
			if strings.TrimSpace(peer.NodeID) == "" {
				return true, fmt.Errorf("%s.nodeID is required", label)
			}
			role := firstNonEmptyString(strings.TrimSpace(peer.Role), strings.TrimSpace(spec.PeerRole))
			switch role {
			case "onprem", "cloud":
			default:
				return true, fmt.Errorf("%s.role is required unless spec.peerRole is set", label)
			}
			if strings.TrimSpace(peer.InnerAddress) != "" {
				if err := validateSAMTransportAddress(label+".innerAddress", peer.InnerAddress); err != nil {
					return true, err
				}
			}
			if encryption == "none" {
				if err := validateTunnelEndpointOrSource(label, "endpoint", peer.Endpoint, peer.EndpointFrom); err != nil {
					return true, err
				}
			} else {
				if strings.TrimSpace(peer.WireGuard.PublicKey) == "" {
					return true, fmt.Errorf("%s.wireGuard.publicKey is required when spec.encryption is wireguard", label)
				}
				if strings.TrimSpace(peer.WireGuard.Endpoint) == "" && strings.TrimSpace(peer.Endpoint) == "" {
					return true, fmt.Errorf("%s.wireGuard.endpoint or endpoint is required when spec.encryption is wireguard", label)
				}
				if strings.TrimSpace(peer.WireGuard.TransportAddress) != "" {
					if err := validateSAMTransportAddress(label+".wireGuard.transportAddress", peer.WireGuard.TransportAddress); err != nil {
						return true, err
					}
				}
			}
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
			if spec.Capture.ConfigureOSAddress {
				return true, fmt.Errorf("%s spec.capture.configureOSAddress=true is not implemented in the MVP", res.ID())
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
		if err := validateCaptureActiveWhen(res.ID()+" spec.capture.activeWhen", spec.Capture.ActiveWhen); err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.Delivery.Mode) {
		case "route":
		default:
			return true, fmt.Errorf("%s spec.delivery.mode must be route", res.ID())
		}
		if strings.TrimSpace(spec.Delivery.PeerRef) == "" {
			return true, fmt.Errorf("%s spec.delivery.peerRef is required", res.ID())
		}
	case "ProviderActionPolicy":
		if res.APIVersion != api.HybridAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.HybridAPIVersion)
		}
		spec, err := res.ProviderActionPolicySpec()
		if err != nil {
			return true, err
		}
		if err := validateProviderActionPolicy(res, spec); err != nil {
			return true, err
		}
	default:
		return false, nil
	}
	return true, nil
}

func validateTunnelEndpoint(value string) error {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("must be an IP address: %w", err)
	}
	if !addr.Is4() {
		return fmt.Errorf("must be an IPv4 address")
	}
	return nil
}

func validateTunnelEndpointOrSource(resourceID, field, value string, source api.StatusValueSourceSpec) error {
	hasValue := strings.TrimSpace(value) != ""
	hasSource := strings.TrimSpace(source.Resource) != ""
	switch {
	case hasValue && hasSource:
		return fmt.Errorf("%s spec.%s and spec.%sFrom are mutually exclusive", resourceID, field, field)
	case !hasValue && !hasSource:
		return fmt.Errorf("%s spec.%s or spec.%sFrom is required", resourceID, field, field)
	case hasValue:
		if err := validateTunnelEndpoint(value); err != nil {
			return fmt.Errorf("%s spec.%s: %w", resourceID, field, err)
		}
	case hasSource:
		if strings.TrimSpace(source.Field) == "" {
			return fmt.Errorf("%s spec.%sFrom.field is required", resourceID, field)
		}
	}
	return nil
}

func validateTunnelAddress(value string) error {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("must be an IPv4 prefix: %w", err)
	}
	if !prefix.Addr().Is4() {
		return fmt.Errorf("must be an IPv4 prefix")
	}
	return nil
}

func validateCaptureActiveWhen(path string, activeWhen api.CaptureActiveWhen) error {
	gateType := strings.TrimSpace(activeWhen.Type)
	ref := strings.TrimSpace(activeWhen.VirtualAddressRef)
	if gateType == "" && ref == "" {
		return nil
	}
	switch gateType {
	case "single-router":
		if ref != "" {
			return fmt.Errorf("%s.virtualAddressRef must be empty when type is single-router", path)
		}
	case "vrrp-master":
		if ref == "" {
			return fmt.Errorf("%s.virtualAddressRef is required when type is vrrp-master", path)
		}
	default:
		return fmt.Errorf("%s.type must be single-router or vrrp-master", path)
	}
	return nil
}

// canonicalProviderActionProviders is the provider allowlist a
// ProviderActionPolicy may reference (matches pkg/plugin canonicalProviders).
var canonicalProviderActionProviders = map[string]bool{
	"aws":   true,
	"azure": true,
	"oci":   true,
	"gcp":   true,
}

// canonicalProviderActionVerbs is the canonical action verb set a
// ProviderActionPolicy may allowlist (matches pkg/plugin canonicalActions).
var canonicalProviderActionVerbs = map[string]bool{
	"assign-secondary-ip":        true,
	"unassign-secondary-ip":      true,
	"ensure-forwarding-enabled":  true,
	"ensure-forwarding-disabled": true,
}

func validateProviderActionPolicy(res api.Resource, spec api.ProviderActionPolicySpec) error {
	for i, provider := range spec.AllowedProviders {
		if !canonicalProviderActionProviders[strings.TrimSpace(provider)] {
			return fmt.Errorf("%s spec.allowedProviders[%d] %q must be one of aws, azure, oci, gcp", res.ID(), i, provider)
		}
	}
	for i, action := range spec.AllowedActions {
		if !canonicalProviderActionVerbs[strings.TrimSpace(action)] {
			return fmt.Errorf("%s spec.allowedActions[%d] %q must be one of assign-secondary-ip, unassign-secondary-ip, ensure-forwarding-enabled, ensure-forwarding-disabled", res.ID(), i, action)
		}
	}
	for i, cidr := range spec.AllowedCIDRs {
		if _, err := netip.ParsePrefix(strings.TrimSpace(cidr)); err != nil {
			return fmt.Errorf("%s spec.allowedCIDRs[%d] must be a valid CIDR: %w", res.ID(), i, err)
		}
	}
	if spec.MaxActionsPerRun < 0 {
		return fmt.Errorf("%s spec.maxActionsPerRun must be >= 0", res.ID())
	}
	// ExecutionWindow is free-form and validated leniently: any non-empty string
	// is accepted in Phase 5.0 (the executor framework interprets it later).
	return nil
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

func validateSAMTransportCIDR(label, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid IPv4 CIDR: %w", label, err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() {
		return fmt.Errorf("%s must be an IPv4 CIDR", label)
	}
	if prefix.Bits() > 30 {
		return fmt.Errorf("%s must contain at least one /31 pair", label)
	}
	return nil
}

func validateSAMTransportAddress(label, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		if !prefix.Addr().Is4() {
			return fmt.Errorf("%s must be an IPv4 address or prefix", label)
		}
		return nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return fmt.Errorf("%s must be an IPv4 address or prefix: %w", label, err)
	}
	if !addr.Is4() {
		return fmt.Errorf("%s must be an IPv4 address or prefix", label)
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
