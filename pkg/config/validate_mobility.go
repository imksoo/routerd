// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/platform"
)

// validateMobilityResource performs local field validation for CloudEdge
// Mobility Control Plane Kinds. The only operator-authored Kind is MobilityPool;
// derived BGP paths and provider trap actions are runtime state and never appear
// as config Kinds. It returns handled=true for Kinds it owns so the caller's Kind
// switch accepts them.
func validateMobilityResource(res api.Resource, _ platform.OS) (bool, error) {
	switch res.Kind {
	case "MobilityPool":
		if res.APIVersion != api.MobilityAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.MobilityAPIVersion)
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			return true, err
		}
		prefix := strings.TrimSpace(spec.Prefix)
		if prefix == "" {
			return true, fmt.Errorf("%s spec.prefix is required", res.ID())
		}
		parsedPrefix, err := netip.ParsePrefix(prefix)
		if err != nil {
			return true, fmt.Errorf("%s spec.prefix must be a CIDR: %w", res.ID(), err)
		}
		if !parsedPrefix.Addr().Is4() {
			return true, fmt.Errorf("%s spec.prefix must be an IPv4 CIDR", res.ID())
		}
		if strings.TrimSpace(spec.GroupRef) == "" {
			return true, fmt.Errorf("%s spec.groupRef is required", res.ID())
		}
		switch strings.TrimSpace(spec.Mode) {
		case "", "selective-address":
		default:
			return true, fmt.Errorf("%s spec.mode %q is not supported; only selective-address", res.ID(), spec.Mode)
		}
		if len(spec.Members) == 0 && len(spec.MembersFrom) == 0 {
			return true, fmt.Errorf("%s spec.members or spec.membersFrom requires at least one member source", res.ID())
		}
		for i, source := range spec.MembersFrom {
			if err := validateMobilityMembersFrom(res.ID(), i, source); err != nil {
				return true, err
			}
		}
		normalized, _, err := mobilityconfig.NormalizeMobilityPool(spec, "")
		if err != nil {
			return true, fmt.Errorf("%s %w", res.ID(), err)
		}
		spec = normalized
		switch strings.TrimSpace(spec.DeliveryPolicy.Mode) {
		case "", "bgp":
		default:
			return true, fmt.Errorf("%s spec.deliveryPolicy.mode %q is not supported; only bgp", res.ID(), spec.DeliveryPolicy.Mode)
		}
		nodeRefs := map[string]bool{}
		memberRoles := map[string]string{}
		staticOwners := map[string]string{}
		placementGroups := map[string]mobilityPlacementGroup{}
		placementCompleteness := map[string]mobilityPlacementCompleteness{}
		for i, member := range spec.Members {
			nodeRef := strings.TrimSpace(member.NodeRef)
			if nodeRef == "" {
				return true, fmt.Errorf("%s spec.members[%d].nodeRef is required", res.ID(), i)
			}
			if strings.TrimSpace(member.Site) == "" {
				return true, fmt.Errorf("%s spec.members[%d].site is required", res.ID(), i)
			}
			switch strings.TrimSpace(member.Role) {
			case "onprem", "cloud":
			default:
				return true, fmt.Errorf("%s spec.members[%d].role must be onprem or cloud", res.ID(), i)
			}
			if nodeRefs[nodeRef] {
				return true, fmt.Errorf("%s spec.members nodeRef %q is duplicated", res.ID(), nodeRef)
			}
			nodeRefs[nodeRef] = true
			memberRoles[nodeRef] = strings.TrimSpace(member.Role)
			if err := validateMobilityStaticOwnedAddresses(res, i, member, parsedPrefix.Masked(), staticOwners); err != nil {
				return true, err
			}
			if err := validateMobilityMemberCapture(res, i, spec, member); err != nil {
				return true, err
			}
			if err := validateMobilityMemberPlacement(res, i, member, placementGroups); err != nil {
				return true, err
			}
			trackMobilityPlacementCompleteness(placementCompleteness, i, member)
			if err := validateMobilityOwnershipDiscovery(res, i, spec, member); err != nil {
				return true, err
			}
		}
		if err := validateMobilityPlacementCompleteness(res, placementCompleteness); err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.CapturePolicy.Mode) {
		case "", "all-non-owner-sites":
		default:
			return true, fmt.Errorf("%s spec.capturePolicy.mode %q is not supported; only all-non-owner-sites", res.ID(), spec.CapturePolicy.Mode)
		}
		if err := validateMobilityIPOwnershipPolicy(res, spec, nodeRefs); err != nil {
			return true, err
		}
		if err := validateMobilityStaticHandovers(res, spec, parsedPrefix.Masked(), nodeRefs, memberRoles, staticOwners); err != nil {
			return true, err
		}
		switch strings.TrimSpace(spec.Authority.Mode) {
		case "", "static":
			authNode := strings.TrimSpace(spec.Authority.NodeRef)
			if authNode != "" && !nodeRefs[authNode] && len(spec.MembersFrom) == 0 {
				return true, fmt.Errorf("%s spec.authority.nodeRef %q must be one of the member nodeRefs", res.ID(), authNode)
			}
		default:
			return true, fmt.Errorf("%s spec.authority.mode %q is not supported; only static", res.ID(), spec.Authority.Mode)
		}
		return true, nil
	case "MobilityMemberSet":
		if res.APIVersion != api.MobilityAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.MobilityAPIVersion)
		}
		spec, err := res.MobilityMemberSetSpec()
		if err != nil {
			return true, err
		}
		if err := validateMobilityMemberSet(res, spec); err != nil {
			return true, err
		}
		return true, nil
	case "SAMPeerGroup":
		if res.APIVersion != api.MobilityAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.MobilityAPIVersion)
		}
		spec, err := res.SAMPeerGroupSpec()
		if err != nil {
			return true, err
		}
		if err := validateSAMPeerGroup(res, spec); err != nil {
			return true, err
		}
		return true, nil
	case "SAMTransportProfile":
		if res.APIVersion != api.MobilityAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.MobilityAPIVersion)
		}
		spec, err := res.SAMTransportProfileSpec()
		if err != nil {
			return true, err
		}
		if err := validateSAMTransportProfile(res, spec); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

func validateMobilityMemberSet(res api.Resource, spec api.MobilityMemberSetSpec) error {
	if len(spec.Members) == 0 {
		return fmt.Errorf("%s spec.members requires at least one member", res.ID())
	}
	seen := map[string]bool{}
	for i, member := range spec.Members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if nodeRef == "" {
			return fmt.Errorf("%s spec.members[%d].nodeRef is required", res.ID(), i)
		}
		if strings.TrimSpace(member.Site) == "" {
			return fmt.Errorf("%s spec.members[%d].site is required", res.ID(), i)
		}
		switch strings.TrimSpace(member.Role) {
		case "onprem", "cloud":
		default:
			return fmt.Errorf("%s spec.members[%d].role must be onprem or cloud", res.ID(), i)
		}
		if seen[nodeRef] {
			return fmt.Errorf("%s spec.members nodeRef %q is duplicated", res.ID(), nodeRef)
		}
		seen[nodeRef] = true
		if member.Placement.Priority < 0 {
			return fmt.Errorf("%s spec.members[%d].placement.priority must be >= 0", res.ID(), i)
		}
	}
	return nil
}

func validateSAMPeerGroup(res api.Resource, spec api.SAMPeerGroupSpec) error {
	if len(spec.Peers) == 0 {
		return fmt.Errorf("%s spec.peers requires at least one peer", res.ID())
	}
	seenPeers := map[string]bool{}
	for i, peer := range spec.Peers {
		nodeRef := strings.TrimSpace(peer.NodeRef)
		if nodeRef == "" {
			return fmt.Errorf("%s spec.peers[%d].nodeRef is required", res.ID(), i)
		}
		if seenPeers[nodeRef] {
			return fmt.Errorf("%s spec.peers nodeRef %q is duplicated", res.ID(), nodeRef)
		}
		seenPeers[nodeRef] = true
		if err := validateTunnelEndpointOrSource(res.ID(), fmt.Sprintf("peers[%d].remoteEndpoint", i), peer.RemoteEndpoint, peer.RemoteEndpointFrom); err != nil {
			return err
		}
		if err := validateSAMTransportPeerOverrideShape(res.ID(), i, peer.Override); err != nil {
			return err
		}
	}
	return nil
}

func validateSAMTransportProfile(res api.Resource, spec api.SAMTransportProfileSpec) error {
	if strings.TrimSpace(spec.SelfNodeRef) == "" {
		return fmt.Errorf("%s spec.selfNodeRef is required", res.ID())
	}
	switch strings.TrimSpace(spec.Mode) {
	case "ipip", "gre":
	default:
		return fmt.Errorf("%s spec.mode must be ipip or gre", res.ID())
	}
	switch strings.TrimSpace(spec.Encryption) {
	case "", "none", "wireguard":
	default:
		return fmt.Errorf("%s spec.encryption must be none or wireguard", res.ID())
	}
	if strings.TrimSpace(spec.UnderlayInterface) == "" {
		return fmt.Errorf("%s spec.underlayInterface is required", res.ID())
	}
	if err := validateTunnelEndpointOrSource(res.ID(), "localEndpoint", spec.LocalEndpoint, spec.LocalEndpointFrom); err != nil {
		return err
	}
	inner, err := netip.ParsePrefix(strings.TrimSpace(spec.InnerPrefix))
	if err != nil {
		return fmt.Errorf("%s spec.innerPrefix must be an IPv4 CIDR: %w", res.ID(), err)
	}
	inner = inner.Masked()
	if !inner.Addr().Is4() {
		return fmt.Errorf("%s spec.innerPrefix must be an IPv4 CIDR", res.ID())
	}
	if inner.Bits() > 31 {
		return fmt.Errorf("%s spec.innerPrefix must leave at least one /31 edge", res.ID())
	}
	if spec.BGP.RouterRef == "" {
		return fmt.Errorf("%s spec.bgp.routerRef is required", res.ID())
	}
	if kind, name, ok := strings.Cut(strings.TrimSpace(spec.BGP.RouterRef), "/"); !ok || kind != "BGPRouter" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s spec.bgp.routerRef must reference BGPRouter/<name>", res.ID())
	}
	if spec.BGP.PeerASN == 0 {
		return fmt.Errorf("%s spec.bgp.peerASN is required", res.ID())
	}
	switch strings.TrimSpace(spec.BGP.TimersPreset) {
	case "", "default", "fast", "slow":
	default:
		return fmt.Errorf("%s spec.bgp.timersPreset must be default, fast, or slow", res.ID())
	}
	if clusterID := strings.TrimSpace(spec.BGP.RouteReflectorClusterID); clusterID != "" {
		parsed, err := netip.ParseAddr(clusterID)
		if err != nil || !parsed.Is4() {
			return fmt.Errorf("%s spec.bgp.routeReflectorClusterID must be an IPv4 address", res.ID())
		}
	}
	if len(spec.Peers) == 0 && len(spec.PeersFrom) == 0 {
		return fmt.Errorf("%s spec.peers or spec.peersFrom requires at least one peer source", res.ID())
	}
	for i, source := range spec.PeersFrom {
		if err := validateSAMTransportPeersFrom(res.ID(), i, source); err != nil {
			return err
		}
	}
	addressingMode := mobilityconfig.NormalizeSAMTransportAddressingMode(spec.AddressingMode)
	if strings.TrimSpace(spec.AddressingMode) != "" && addressingMode == "" {
		return fmt.Errorf("%s spec.addressingMode must be edge-index or pair-stable", res.ID())
	}
	seenPeers := map[string]bool{}
	usedInner := map[string]string{}
	for i, peer := range spec.Peers {
		nodeRef := strings.TrimSpace(peer.NodeRef)
		if nodeRef == "" {
			return fmt.Errorf("%s spec.peers[%d].nodeRef is required", res.ID(), i)
		}
		if nodeRef == strings.TrimSpace(spec.SelfNodeRef) {
			return fmt.Errorf("%s spec.peers[%d].nodeRef must not equal spec.selfNodeRef", res.ID(), i)
		}
		if seenPeers[nodeRef] {
			return fmt.Errorf("%s spec.peers nodeRef %q is duplicated", res.ID(), nodeRef)
		}
		seenPeers[nodeRef] = true
		if err := validateTunnelEndpointOrSource(res.ID(), fmt.Sprintf("peers[%d].remoteEndpoint", i), peer.RemoteEndpoint, peer.RemoteEndpointFrom); err != nil {
			return err
		}
		if err := validateSAMTransportPeerOverride(res.ID(), i, inner, peer.Override, usedInner); err != nil {
			return err
		}
	}
	capacity := 1 << (31 - inner.Bits())
	switch addressingMode {
	case "pair-stable":
		if len(spec.Peers) > capacity {
			return fmt.Errorf("%s spec.innerPrefix %s has %d /31 edges but spec.peers requires %d edges for pair-stable addressing", res.ID(), inner, capacity, len(spec.Peers))
		}
		seedPrefix := inner.Masked().String()
		collisions := map[int]string{}
		self := strings.TrimSpace(spec.SelfNodeRef)
		for i, peer := range spec.Peers {
			peerNode := strings.TrimSpace(peer.NodeRef)
			if strings.TrimSpace(peer.Override.LocalInner) != "" && strings.TrimSpace(peer.Override.RemoteInner) != "" {
				continue
			}
			slot := mobilityconfig.StableSAMTransportSlot(seedPrefix, self, peerNode, capacity)
			slotPrefix, err := mobilityconfig.SAMTransportSlotPrefix(inner, slot)
			if err != nil {
				return fmt.Errorf("%s spec.peers[%d].nodeRef %q slot computation failed: %w", res.ID(), i, peerNode, err)
			}
			for _, addr := range []string{slotPrefix.Addr().String(), slotPrefix.Addr().Next().String()} {
				if previous := usedInner[addr]; previous != "" {
					return fmt.Errorf("%s spec.peers[%d].nodeRef %q pair-stable slot %s conflicts with %s; change override.localInner/remoteInner or expand spec.innerPrefix", res.ID(), i, peerNode, slotPrefix, previous)
				}
			}
			if prev, ok := collisions[slot]; ok {
				return fmt.Errorf("%s spec.peers[%d].nodeRef %q collides with %s in pair-stable slot %s; use override.localInner/remoteInner or expand spec.innerPrefix", res.ID(), i, peerNode, prev, slotPrefix)
			}
			collisions[slot] = fmt.Sprintf("spec.peers[%d].nodeRef %q", i, peerNode)
		}
	default:
		if len(spec.Peers) > 1 && len(spec.TopologyNodeRefs) == 0 {
			return fmt.Errorf("%s spec.topologyNodeRefs is required when spec.peers has more than one peer", res.ID())
		}
		topology, err := normalizeSAMTransportTopology(res.ID(), spec)
		if err != nil {
			return err
		}
		edgeCount := len(topology) * (len(topology) - 1) / 2
		if edgeCount > capacity {
			return fmt.Errorf("%s spec.innerPrefix %s has %d /31 edges but topologyNodeRefs requires %d edges", res.ID(), inner, capacity, edgeCount)
		}
		for i, peer := range spec.Peers {
			nodeRef := strings.TrimSpace(peer.NodeRef)
			if !stringInSlice(nodeRef, topology) {
				return fmt.Errorf("%s spec.peers[%d].nodeRef %q must be listed in spec.topologyNodeRefs", res.ID(), i, nodeRef)
			}
		}
	}
	return nil
}

func normalizeSAMTransportTopology(resourceID string, spec api.SAMTransportProfileSpec) ([]string, error) {
	topology := append([]string(nil), spec.TopologyNodeRefs...)
	if len(topology) == 0 {
		topology = []string{spec.SelfNodeRef}
		for _, peer := range spec.Peers {
			topology = append(topology, peer.NodeRef)
		}
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(topology))
	for i, node := range topology {
		node = strings.TrimSpace(node)
		if node == "" {
			return nil, fmt.Errorf("%s spec.topologyNodeRefs[%d] must not be empty", resourceID, i)
		}
		if seen[node] {
			return nil, fmt.Errorf("%s spec.topologyNodeRefs nodeRef %q is duplicated", resourceID, node)
		}
		seen[node] = true
		out = append(out, node)
	}
	if !seen[strings.TrimSpace(spec.SelfNodeRef)] {
		return nil, fmt.Errorf("%s spec.selfNodeRef %q must be listed in spec.topologyNodeRefs", resourceID, spec.SelfNodeRef)
	}
	return out, nil
}

func validateSAMTransportPeersFrom(resourceID string, index int, source api.SAMTransportPeersSourceSpec) error {
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind != "SAMPeerGroup" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s spec.peersFrom[%d].resource must reference SAMPeerGroup/<name>", resourceID, index)
	}
	return nil
}

func validateMobilityMembersFrom(resourceID string, index int, source api.MobilityMembersSourceSpec) error {
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind != "MobilityMemberSet" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s spec.membersFrom[%d].resource must reference MobilityMemberSet/<name>", resourceID, index)
	}
	return nil
}

func validateSAMTransportPeerOverrideShape(resourceID string, index int, override api.SAMTransportPeerOverrideSpec) error {
	localSet := strings.TrimSpace(override.LocalInner) != ""
	remoteSet := strings.TrimSpace(override.RemoteInner) != ""
	if localSet != remoteSet {
		return fmt.Errorf("%s spec.peers[%d].override.localInner and remoteInner must be set together", resourceID, index)
	}
	if !localSet {
		return nil
	}
	local, err := netip.ParsePrefix(strings.TrimSpace(override.LocalInner))
	if err != nil || !local.Addr().Is4() || local.Bits() != 31 {
		return fmt.Errorf("%s spec.peers[%d].override.localInner must be an IPv4 /31 prefix", resourceID, index)
	}
	remoteValue := strings.TrimSpace(override.RemoteInner)
	if strings.Contains(remoteValue, "/") {
		remotePrefix, err := netip.ParsePrefix(remoteValue)
		if err != nil || !remotePrefix.Addr().Is4() || remotePrefix.Bits() != 32 {
			return fmt.Errorf("%s spec.peers[%d].override.remoteInner must be an IPv4 address or /32", resourceID, index)
		}
		remoteValue = remotePrefix.Addr().String()
	}
	remote, err := netip.ParseAddr(remoteValue)
	if err != nil || !remote.Is4() {
		return fmt.Errorf("%s spec.peers[%d].override.remoteInner must be an IPv4 address", resourceID, index)
	}
	local = local.Masked()
	if !local.Contains(remote) || remote == local.Addr() {
		return fmt.Errorf("%s spec.peers[%d].override.remoteInner must be the other address in override.localInner", resourceID, index)
	}
	return nil
}

func validateSAMTransportPeerOverride(resourceID string, index int, inner netip.Prefix, override api.SAMTransportPeerOverrideSpec, used map[string]string) error {
	localSet := strings.TrimSpace(override.LocalInner) != ""
	remoteSet := strings.TrimSpace(override.RemoteInner) != ""
	if localSet != remoteSet {
		return fmt.Errorf("%s spec.peers[%d].override.localInner and remoteInner must be set together", resourceID, index)
	}
	if !localSet {
		return nil
	}
	local, err := netip.ParsePrefix(strings.TrimSpace(override.LocalInner))
	if err != nil {
		return fmt.Errorf("%s spec.peers[%d].override.localInner must be an IPv4 /31 prefix: %w", resourceID, index, err)
	}
	local = local.Masked()
	if !local.Addr().Is4() || local.Bits() != 31 || !inner.Contains(local.Addr()) {
		return fmt.Errorf("%s spec.peers[%d].override.localInner must be an IPv4 /31 inside spec.innerPrefix", resourceID, index)
	}
	remoteValue := strings.TrimSpace(override.RemoteInner)
	if strings.Contains(remoteValue, "/") {
		remotePrefix, err := netip.ParsePrefix(remoteValue)
		if err != nil || !remotePrefix.Addr().Is4() || remotePrefix.Bits() != 32 {
			return fmt.Errorf("%s spec.peers[%d].override.remoteInner must be an IPv4 address or /32", resourceID, index)
		}
		remoteValue = remotePrefix.Addr().String()
	}
	remote, err := netip.ParseAddr(remoteValue)
	if err != nil || !remote.Is4() {
		return fmt.Errorf("%s spec.peers[%d].override.remoteInner must be an IPv4 address", resourceID, index)
	}
	if !local.Contains(remote) || remote == local.Addr() {
		return fmt.Errorf("%s spec.peers[%d].override.remoteInner must be the other address in override.localInner", resourceID, index)
	}
	for _, addr := range []string{local.Addr().String(), remote.String()} {
		if previous := used[addr]; previous != "" {
			return fmt.Errorf("%s spec.peers[%d].override inner address %s conflicts with %s", resourceID, index, addr, previous)
		}
		used[addr] = fmt.Sprintf("spec.peers[%d].override", index)
	}
	return nil
}

func validateMobilityOwnershipDiscovery(res api.Resource, index int, spec api.MobilityPoolSpec, member api.MobilityPoolMember) error {
	discovery := member.OwnershipDiscovery
	discoverySet := strings.TrimSpace(discovery.Mode) != "" ||
		strings.TrimSpace(discovery.ProviderRef) != "" ||
		strings.TrimSpace(discovery.PluginRef) != "" ||
		strings.TrimSpace(discovery.SubnetRef) != "" ||
		strings.TrimSpace(discovery.SubnetRefFrom) != "" ||
		strings.TrimSpace(discovery.ScanInterval) != "" ||
		strings.TrimSpace(discovery.LeaseTTL) != "" ||
		len(discovery.Sources) > 0 ||
		len(discovery.Scope.IncludeAddresses) > 0 ||
		len(discovery.Scope.ExcludeAddresses) > 0 ||
		len(discovery.Selector.Tags) > 0
	if !discoverySet {
		return nil
	}
	switch strings.TrimSpace(discovery.Mode) {
	case "", "disabled":
		return nil
	case "provider-private-ip":
		return validateMobilityProviderOwnershipDiscovery(res, index, spec, member)
	case "onprem-l2":
		return validateMobilityOnPremOwnershipDiscovery(res, index, spec, member)
	default:
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.mode %q is not supported; use provider-private-ip or onprem-l2", res.ID(), index, discovery.Mode)
	}
}

func validateMobilityProviderOwnershipDiscovery(res api.Resource, index int, spec api.MobilityPoolSpec, member api.MobilityPoolMember) error {
	discovery := member.OwnershipDiscovery
	if len(discovery.Sources) > 0 {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.sources is supported only when mode is onprem-l2", res.ID(), index)
	}
	if strings.TrimSpace(member.Role) != "cloud" {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery is supported only for role cloud", res.ID(), index)
	}
	if effectiveMobilityDeliveryMode(spec) != "bgp" {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery requires spec.deliveryPolicy.mode=bgp", res.ID(), index)
	}
	if strings.TrimSpace(member.Capture.Type) != "provider-secondary-ip" {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery requires capture.type provider-secondary-ip", res.ID(), index)
	}
	providerRef := strings.TrimSpace(discovery.ProviderRef)
	if providerRef == "" {
		providerRef = strings.TrimSpace(member.Capture.ProviderRef)
	}
	if providerRef == "" {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.providerRef or capture.providerRef is required", res.ID(), index)
	}
	if interval := strings.TrimSpace(discovery.ScanInterval); interval != "" {
		parsed, err := time.ParseDuration(interval)
		if err != nil {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.scanInterval must be a Go duration: %w", res.ID(), index, err)
		}
		if parsed < 30*time.Second {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.scanInterval must be >= 30s", res.ID(), index)
		}
	}
	if ttl := strings.TrimSpace(discovery.LeaseTTL); ttl != "" {
		parsed, err := time.ParseDuration(ttl)
		if err != nil {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.leaseTTL must be a Go duration: %w", res.ID(), index, err)
		}
		if parsed <= 0 {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.leaseTTL must be > 0", res.ID(), index)
		}
	}
	if err := validateMobilityOwnershipDiscoveryScope(res, index, discovery.Scope, mustParsePrefixForValidation(spec.Prefix)); err != nil {
		return err
	}
	for key := range discovery.Selector.Tags {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.selector.tags must not contain empty keys", res.ID(), index)
		}
	}
	return nil
}

func validateMobilityOnPremOwnershipDiscovery(res api.Resource, index int, spec api.MobilityPoolSpec, member api.MobilityPoolMember) error {
	discovery := member.OwnershipDiscovery
	if strings.TrimSpace(member.Role) != "onprem" {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery mode onprem-l2 is supported only for role onprem", res.ID(), index)
	}
	if effectiveMobilityDeliveryMode(spec) != "bgp" {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery mode onprem-l2 requires spec.deliveryPolicy.mode=bgp", res.ID(), index)
	}
	if strings.TrimSpace(member.Capture.Type) != "proxy-arp" {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery mode onprem-l2 requires capture.type proxy-arp", res.ID(), index)
	}
	if strings.TrimSpace(member.Capture.Interface) == "" {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery mode onprem-l2 requires capture.interface", res.ID(), index)
	}
	if len(discovery.Sources) == 0 {
		return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.sources requires at least one source when mode is onprem-l2", res.ID(), index)
	}
	if err := validateMobilityDiscoveryDuration(res, index, "ownershipDiscovery.scanInterval", discovery.ScanInterval, 30*time.Second, false); err != nil {
		return err
	}
	if err := validateMobilityDiscoveryDuration(res, index, "ownershipDiscovery.leaseTTL", discovery.LeaseTTL, 0, true); err != nil {
		return err
	}
	if err := validateMobilityOwnershipDiscoveryScope(res, index, discovery.Scope, mustParsePrefixForValidation(spec.Prefix)); err != nil {
		return err
	}
	for key := range discovery.Selector.Tags {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.selector.tags must not contain empty keys", res.ID(), index)
		}
	}
	seen := map[string]bool{}
	for j, source := range discovery.Sources {
		sourceType := strings.TrimSpace(source.Type)
		switch sourceType {
		case "dhcpv4-lease", "arp-observer", "on-demand-arp", "pve-svnet":
		case "":
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.sources[%d].type is required", res.ID(), index, j)
		default:
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.sources[%d].type %q is not supported", res.ID(), index, j, source.Type)
		}
		key := strings.Join([]string{
			sourceType,
			strings.TrimSpace(source.Resource),
			strings.TrimSpace(mobilityFirstNonEmpty(source.Interface, member.Capture.Interface)),
			strings.TrimSpace(source.Network),
			strings.TrimSpace(source.Bridge),
		}, "\x00")
		if seen[key] {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.sources[%d] duplicates another source", res.ID(), index, j)
		}
		seen[key] = true
		if err := validateMobilityDiscoveryDuration(res, index, fmt.Sprintf("ownershipDiscovery.sources[%d].scanInterval", j), source.ScanInterval, time.Second, false); err != nil {
			return err
		}
		if err := validateMobilityDiscoveryDuration(res, index, fmt.Sprintf("ownershipDiscovery.sources[%d].probeTimeout", j), source.ProbeTimeout, 0, true); err != nil {
			return err
		}
		if err := validateMobilityDiscoveryDuration(res, index, fmt.Sprintf("ownershipDiscovery.sources[%d].leaseTTL", j), source.LeaseTTL, 0, true); err != nil {
			return err
		}
		if source.ProbeRetries < 0 || source.ProbeRetries > 20 {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.sources[%d].probeRetries must be between 0 and 20", res.ID(), index, j)
		}
		if strings.TrimSpace(sourceType) == "on-demand-arp" {
			if strings.TrimSpace(mobilityFirstNonEmpty(source.Interface, member.Capture.Interface)) == "" {
				return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.sources[%d].interface or capture.interface is required for on-demand-arp", res.ID(), index, j)
			}
		}
	}
	return nil
}

func validateMobilityDiscoveryDuration(res api.Resource, index int, path, value string, min time.Duration, positive bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("%s spec.members[%d].%s must be a Go duration: %w", res.ID(), index, path, err)
	}
	if positive && parsed <= 0 {
		return fmt.Errorf("%s spec.members[%d].%s must be > 0", res.ID(), index, path)
	}
	if min > 0 && parsed < min {
		return fmt.Errorf("%s spec.members[%d].%s must be >= %s", res.ID(), index, path, min)
	}
	return nil
}

func mobilityFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func validateMobilityOwnershipDiscoveryScope(res api.Resource, index int, scope api.MobilityOwnershipDiscoveryScope, pool netip.Prefix) error {
	for i, raw := range scope.IncludeAddresses {
		if _, err := parseMobilityDiscoveryScopePrefix(raw, pool); err != nil {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.scope.includeAddresses[%d]: %w", res.ID(), index, i, err)
		}
	}
	for i, raw := range scope.ExcludeAddresses {
		if _, err := parseMobilityDiscoveryScopePrefix(raw, pool); err != nil {
			return fmt.Errorf("%s spec.members[%d].ownershipDiscovery.scope.excludeAddresses[%d]: %w", res.ID(), index, i, err)
		}
	}
	return nil
}

func parseMobilityDiscoveryScopePrefix(raw string, pool netip.Prefix) (netip.Prefix, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return netip.Prefix{}, fmt.Errorf("must not be empty")
	}
	if !strings.Contains(value, "/") {
		addr, err := netip.ParseAddr(value)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("must be an IPv4 address or CIDR: %w", err)
		}
		if !addr.Is4() {
			return netip.Prefix{}, fmt.Errorf("must be an IPv4 address or CIDR")
		}
		prefix := netip.PrefixFrom(addr, 32)
		if !pool.Contains(addr) {
			return netip.Prefix{}, fmt.Errorf("%s is outside pool prefix %s", prefix.String(), pool.String())
		}
		return prefix, nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("must be an IPv4 address or CIDR: %w", err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("must be an IPv4 address or CIDR")
	}
	if prefix.Bits() < pool.Bits() || !pool.Contains(prefix.Addr()) {
		return netip.Prefix{}, fmt.Errorf("%s is outside pool prefix %s", prefix.String(), pool.String())
	}
	return prefix, nil
}

func mustParsePrefixForValidation(raw string) netip.Prefix {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil {
		return netip.Prefix{}
	}
	return prefix.Masked()
}

func validateMobilityStaticOwnedAddresses(res api.Resource, index int, member api.MobilityPoolMember, pool netip.Prefix, owners map[string]string) error {
	if len(member.StaticOwnedAddresses) == 0 {
		return nil
	}
	if strings.TrimSpace(member.Role) != "onprem" {
		return fmt.Errorf("%s spec.members[%d].staticOwnedAddresses is supported only for role onprem", res.ID(), index)
	}
	for j, raw := range member.StaticOwnedAddresses {
		address, err := parseMobilityStaticAddress(raw, pool)
		if err != nil {
			return fmt.Errorf("%s spec.members[%d].staticOwnedAddresses[%d]: %w", res.ID(), index, j, err)
		}
		if existing := owners[address]; existing != "" {
			return fmt.Errorf("%s spec.members[%d].staticOwnedAddresses[%d] %q duplicates staticOwnedAddresses owned by member %q", res.ID(), index, j, address, existing)
		}
		owners[address] = strings.TrimSpace(member.NodeRef)
	}
	return nil
}

func validateMobilityStaticHandovers(res api.Resource, spec api.MobilityPoolSpec, pool netip.Prefix, nodeRefs map[string]bool, memberRoles, staticOwners map[string]string) error {
	seen := map[string]bool{}
	for i, handover := range spec.StaticHandovers {
		address, err := parseMobilityStaticAddress(handover.Address, pool)
		if err != nil {
			return fmt.Errorf("%s spec.staticHandovers[%d].address: %w", res.ID(), i, err)
		}
		fromNode := strings.TrimSpace(handover.FromNodeRef)
		toNode := strings.TrimSpace(handover.ToNodeRef)
		if fromNode == "" {
			return fmt.Errorf("%s spec.staticHandovers[%d].fromNodeRef is required", res.ID(), i)
		}
		if toNode == "" {
			return fmt.Errorf("%s spec.staticHandovers[%d].toNodeRef is required", res.ID(), i)
		}
		if fromNode == toNode {
			return fmt.Errorf("%s spec.staticHandovers[%d].toNodeRef must differ from fromNodeRef", res.ID(), i)
		}
		if !nodeRefs[fromNode] {
			return fmt.Errorf("%s spec.staticHandovers[%d].fromNodeRef %q must be one of the member nodeRefs", res.ID(), i, fromNode)
		}
		if !nodeRefs[toNode] {
			return fmt.Errorf("%s spec.staticHandovers[%d].toNodeRef %q must be one of the member nodeRefs", res.ID(), i, toNode)
		}
		if memberRoles[fromNode] != "onprem" {
			return fmt.Errorf("%s spec.staticHandovers[%d].fromNodeRef %q must reference an onprem member", res.ID(), i, fromNode)
		}
		if owner := staticOwners[address]; owner != "" && owner != fromNode {
			return fmt.Errorf("%s spec.staticHandovers[%d].address %q is static-owned by member %q, not fromNodeRef %q", res.ID(), i, address, owner, fromNode)
		}
		if seen[address] {
			return fmt.Errorf("%s spec.staticHandovers[%d] duplicates handover for %s", res.ID(), i, address)
		}
		seen[address] = true
	}
	return nil
}

func parseMobilityStaticAddress(raw string, pool netip.Prefix) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("address is required")
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return "", fmt.Errorf("must be an IPv4 /32 CIDR: %w", err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return "", fmt.Errorf("must be an IPv4 /32 CIDR")
	}
	if !pool.Contains(prefix.Addr()) {
		return "", fmt.Errorf("%q must be within spec.prefix %s", prefix.String(), pool.String())
	}
	return prefix.String(), nil
}

func validateMobilityIPOwnershipPolicy(res api.Resource, spec api.MobilityPoolSpec, nodeRefs map[string]bool) error {
	policy := spec.IPOwnershipPolicy
	policySet := strings.TrimSpace(policy.Type) != "" ||
		len(policy.PreferNodes) > 0 ||
		policy.AutoFailover
	if !policySet {
		return nil
	}
	if strings.TrimSpace(policy.Type) != "centralized" {
		return fmt.Errorf("%s spec.ipOwnershipPolicy.type %q is not supported; only centralized", res.ID(), policy.Type)
	}
	seen := map[string]bool{}
	for i, nodeRef := range policy.PreferNodes {
		nodeRef = strings.TrimSpace(nodeRef)
		if nodeRef == "" {
			return fmt.Errorf("%s spec.ipOwnershipPolicy.preferNodes[%d] must not be empty", res.ID(), i)
		}
		if !nodeRefs[nodeRef] {
			return fmt.Errorf("%s spec.ipOwnershipPolicy.preferNodes[%d] %q must be one of the member nodeRefs", res.ID(), i, nodeRef)
		}
		if seen[nodeRef] {
			return fmt.Errorf("%s spec.ipOwnershipPolicy.preferNodes contains duplicate nodeRef %q", res.ID(), nodeRef)
		}
		seen[nodeRef] = true
	}
	return nil
}

type mobilityPlacementGroup struct {
	site        string
	role        string
	providerRef string
}

type mobilityPlacementCompleteness struct {
	site          string
	providerRef   string
	hasPlacement  bool
	missingIndex  int
	missingNode   string
	placedGroup   string
	placedNodeRef string
}

func validateMobilityMemberPlacement(res api.Resource, index int, member api.MobilityPoolMember, groups map[string]mobilityPlacementGroup) error {
	group := strings.TrimSpace(member.Placement.Group)
	if group == "" {
		if member.Placement.Priority != 0 {
			return fmt.Errorf("%s spec.members[%d].placement.priority requires placement.group", res.ID(), index)
		}
		if member.Maintenance.Drain {
			return fmt.Errorf("%s spec.members[%d].maintenance.drain requires placement.group", res.ID(), index)
		}
		return nil
	}
	if member.Placement.Priority < 0 || member.Placement.Priority > 1000000 {
		return fmt.Errorf("%s spec.members[%d].placement.priority must be between 0 and 1000000", res.ID(), index)
	}
	if strings.TrimSpace(member.Role) != "cloud" {
		return fmt.Errorf("%s spec.members[%d].placement.group is supported only for role cloud", res.ID(), index)
	}
	captureType := strings.TrimSpace(member.Capture.Type)
	if captureType != "" && captureType != "provider-secondary-ip" {
		return fmt.Errorf("%s spec.members[%d].placement.group requires provider-secondary-ip capture", res.ID(), index)
	}
	current := mobilityPlacementGroup{
		site:        strings.TrimSpace(member.Site),
		role:        strings.TrimSpace(member.Role),
		providerRef: strings.TrimSpace(member.Capture.ProviderRef),
	}
	if existing, ok := groups[group]; ok {
		if existing.site != current.site {
			return fmt.Errorf("%s spec.members[%d].placement.group %q must use one site; got %q and %q", res.ID(), index, group, existing.site, current.site)
		}
		if existing.role != current.role {
			return fmt.Errorf("%s spec.members[%d].placement.group %q must use one role; got %q and %q", res.ID(), index, group, existing.role, current.role)
		}
		if existing.providerRef != "" && current.providerRef != "" && existing.providerRef != current.providerRef {
			return fmt.Errorf("%s spec.members[%d].placement.group %q must use one providerRef; got %q and %q", res.ID(), index, group, existing.providerRef, current.providerRef)
		}
		if existing.providerRef == "" && current.providerRef != "" {
			existing.providerRef = current.providerRef
			groups[group] = existing
		}
	} else {
		groups[group] = current
	}
	return nil
}

func trackMobilityPlacementCompleteness(groups map[string]mobilityPlacementCompleteness, index int, member api.MobilityPoolMember) {
	if strings.TrimSpace(member.Role) != "cloud" || strings.TrimSpace(member.Capture.Type) != "provider-secondary-ip" {
		return
	}
	site := strings.TrimSpace(member.Site)
	providerRef := strings.TrimSpace(member.Capture.ProviderRef)
	if site == "" || providerRef == "" {
		return
	}
	key := site + "\x00" + providerRef
	current := groups[key]
	if current.site == "" {
		current.site = site
		current.providerRef = providerRef
		current.missingIndex = -1
	}
	if group := strings.TrimSpace(member.Placement.Group); group != "" {
		current.hasPlacement = true
		if current.placedGroup == "" {
			current.placedGroup = group
			current.placedNodeRef = strings.TrimSpace(member.NodeRef)
		}
	} else if current.missingIndex == -1 {
		current.missingIndex = index
		current.missingNode = strings.TrimSpace(member.NodeRef)
	}
	groups[key] = current
}

func validateMobilityPlacementCompleteness(res api.Resource, groups map[string]mobilityPlacementCompleteness) error {
	for _, group := range groups {
		if group.hasPlacement && group.missingIndex >= 0 {
			return fmt.Errorf("%s spec.members[%d].placement.group is required for provider-secondary-ip member %q because site %q/providerRef %q uses placement group %q on member %q", res.ID(), group.missingIndex, group.missingNode, group.site, group.providerRef, group.placedGroup, group.placedNodeRef)
		}
	}
	return nil
}

func validateMobilityMemberCapture(res api.Resource, index int, spec api.MobilityPoolSpec, member api.MobilityPoolMember) error {
	captureType := strings.TrimSpace(member.Capture.Type)
	switch strings.TrimSpace(member.Delivery.Mode) {
	case "", "route":
	default:
		return fmt.Errorf("%s spec.members[%d].delivery.mode must be empty or route", res.ID(), index)
	}
	if captureType != "" && effectiveMobilityDeliveryMode(spec) != "bgp" && strings.TrimSpace(member.Delivery.PeerRef) == "" {
		if len(member.DeliveryTo) == 0 {
			return fmt.Errorf("%s spec.members[%d].delivery.peerRef or deliveryTo is required when capture.type is set", res.ID(), index)
		}
	}
	for j, delivery := range member.DeliveryTo {
		if strings.TrimSpace(delivery.NodeRef) == "" && strings.TrimSpace(delivery.Site) == "" && strings.TrimSpace(delivery.Role) == "" {
			return fmt.Errorf("%s spec.members[%d].deliveryTo[%d] must set nodeRef, site, or role", res.ID(), index, j)
		}
		switch strings.TrimSpace(delivery.Role) {
		case "", "onprem", "cloud":
		default:
			return fmt.Errorf("%s spec.members[%d].deliveryTo[%d].role must be onprem or cloud", res.ID(), index, j)
		}
		if strings.TrimSpace(delivery.PeerRef) == "" {
			return fmt.Errorf("%s spec.members[%d].deliveryTo[%d].peerRef is required", res.ID(), index, j)
		}
		switch strings.TrimSpace(delivery.Mode) {
		case "", "route":
		default:
			return fmt.Errorf("%s spec.members[%d].deliveryTo[%d].mode must be empty or route", res.ID(), index, j)
		}
	}
	for key, value := range member.Capture.Target {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("%s spec.members[%d].capture.target contains an empty key", res.ID(), index)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s spec.members[%d].capture.target[%q] must not be empty", res.ID(), index, key)
		}
		lowerKey := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
		if strings.Contains(lowerKey, "secret") || strings.Contains(lowerKey, "token") || strings.Contains(lowerKey, "password") || strings.Contains(lowerKey, "credential") || strings.Contains(lowerKey, "private_key") || strings.Contains(lowerKey, "access_key") {
			return fmt.Errorf("%s spec.members[%d].capture.target[%q] looks secret-like; target may only carry non-secret provider identifiers", res.ID(), index, key)
		}
	}
	if err := validateCaptureActiveWhen(fmt.Sprintf("%s spec.members[%d].capture.activeWhen", res.ID(), index), member.Capture.ActiveWhen); err != nil {
		return err
	}
	if captureType == "" {
		return nil
	}
	role := strings.TrimSpace(member.Role)
	switch role {
	case "cloud":
		if captureType != "provider-secondary-ip" {
			return fmt.Errorf("%s spec.members[%d].capture.type must be provider-secondary-ip for role cloud", res.ID(), index)
		}
	case "onprem":
		if captureType != "proxy-arp" {
			return fmt.Errorf("%s spec.members[%d].capture.type must be proxy-arp for role onprem", res.ID(), index)
		}
		switch strings.TrimSpace(member.Capture.ActiveWhen.Type) {
		case "single-router":
		case "vrrp-master":
			if strings.TrimSpace(member.Capture.ActiveWhen.VirtualAddressRef) == "" {
				return fmt.Errorf("%s spec.members[%d].capture.activeWhen.virtualAddressRef is required for role onprem proxy-arp capture", res.ID(), index)
			}
		default:
			return fmt.Errorf("%s spec.members[%d].capture.activeWhen.type is required for role onprem proxy-arp capture; use single-router or vrrp-master", res.ID(), index)
		}
	}
	switch captureType {
	case "provider-secondary-ip":
		if strings.TrimSpace(member.Capture.ProviderRef) == "" {
			return fmt.Errorf("%s spec.members[%d].capture.providerRef is required when capture.type is provider-secondary-ip", res.ID(), index)
		}
		if strings.TrimSpace(member.Capture.ProviderMode) == "" {
			return fmt.Errorf("%s spec.members[%d].capture.providerMode is required when capture.type is provider-secondary-ip", res.ID(), index)
		}
		if strings.TrimSpace(member.Capture.NICRef) == "" && !mobilityProviderCaptureAllowsDiscoveredNIC(spec, member) {
			return fmt.Errorf("%s spec.members[%d].capture.nicRef is required when capture.type is provider-secondary-ip", res.ID(), index)
		}
		if member.Capture.ConfigureOSAddress {
			return fmt.Errorf("%s spec.members[%d].capture.configureOSAddress=true is not implemented in the MVP", res.ID(), index)
		}
	case "proxy-arp":
		if strings.TrimSpace(member.Capture.Interface) == "" {
			return fmt.Errorf("%s spec.members[%d].capture.interface is required when capture.type is proxy-arp", res.ID(), index)
		}
	default:
		return fmt.Errorf("%s spec.members[%d].capture.type %q is reserved/not implemented in MVP", res.ID(), index, captureType)
	}
	return nil
}

func mobilityProviderCaptureAllowsDiscoveredNIC(spec api.MobilityPoolSpec, member api.MobilityPoolMember) bool {
	return effectiveMobilityDeliveryMode(spec) == "bgp" &&
		strings.TrimSpace(member.Role) == "cloud" &&
		strings.TrimSpace(member.Capture.Type) == "provider-secondary-ip" &&
		strings.TrimSpace(member.OwnershipDiscovery.Mode) == "provider-private-ip"
}

func effectiveMobilityDeliveryMode(spec api.MobilityPoolSpec) string {
	mode := strings.TrimSpace(spec.DeliveryPolicy.Mode)
	if mode == "" {
		return "bgp"
	}
	return mode
}
