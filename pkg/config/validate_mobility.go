// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// validateMobilityResource performs local field validation for CloudEdge
// Mobility Control Plane Kinds (Step 1). The only operator-authored Kind is
// MobilityPool; AddressLease is derived runtime state and is intentionally not a
// config Kind, so it never appears here. It returns handled=true for Kinds it
// owns so the caller's Kind switch accepts them.
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
		if len(spec.Members) == 0 {
			return true, fmt.Errorf("%s spec.members requires at least one member", res.ID())
		}
		nodeRefs := map[string]bool{}
		memberRoles := map[string]string{}
		staticOwners := map[string]string{}
		placementGroups := map[string]mobilityPlacementGroup{}
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
			if err := validateMobilityMemberCapture(res, i, member); err != nil {
				return true, err
			}
			if err := validateMobilityMemberPlacement(res, i, member, placementGroups); err != nil {
				return true, err
			}
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
		if hold := strings.TrimSpace(spec.CapturePolicy.DeprovisionHoldDuration); hold != "" {
			parsed, err := time.ParseDuration(hold)
			if err != nil {
				return true, fmt.Errorf("%s spec.capturePolicy.deprovisionHoldDuration must be a Go duration: %w", res.ID(), err)
			}
			if parsed < 0 {
				return true, fmt.Errorf("%s spec.capturePolicy.deprovisionHoldDuration must be >= 0", res.ID())
			}
		}
		if ttl := strings.TrimSpace(spec.LeasePolicy.TTL); ttl != "" {
			parsed, err := time.ParseDuration(ttl)
			if err != nil {
				return true, fmt.Errorf("%s spec.leasePolicy.ttl must be a Go duration: %w", res.ID(), err)
			}
			if parsed <= 0 {
				return true, fmt.Errorf("%s spec.leasePolicy.ttl must be > 0", res.ID())
			}
		}
		if hold := strings.TrimSpace(spec.LeasePolicy.HoldDuration); hold != "" {
			parsed, err := time.ParseDuration(hold)
			if err != nil {
				return true, fmt.Errorf("%s spec.leasePolicy.holdDuration must be a Go duration: %w", res.ID(), err)
			}
			if parsed < 0 {
				return true, fmt.Errorf("%s spec.leasePolicy.holdDuration must be >= 0", res.ID())
			}
		}
		switch strings.TrimSpace(spec.DeliveryPolicy.Mode) {
		case "", "route", "bgp":
		default:
			return true, fmt.Errorf("%s spec.deliveryPolicy.mode %q is not supported; only route or bgp", res.ID(), spec.DeliveryPolicy.Mode)
		}
		switch strings.TrimSpace(spec.Authority.Mode) {
		case "", "static":
			authNode := strings.TrimSpace(spec.Authority.NodeRef)
			if authNode != "" && !nodeRefs[authNode] {
				return true, fmt.Errorf("%s spec.authority.nodeRef %q must be one of the member nodeRefs", res.ID(), authNode)
			}
		default:
			return true, fmt.Errorf("%s spec.authority.mode %q is not supported; only static", res.ID(), spec.Authority.Mode)
		}
		return true, nil
	}
	return false, nil
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
		policy.EpochLocking != nil ||
		len(policy.PreferNodes) > 0 ||
		policy.AutoFailover ||
		strings.TrimSpace(policy.HeartbeatInterval) != "" ||
		strings.TrimSpace(policy.HeartbeatTTL) != "" ||
		strings.TrimSpace(policy.PromotionHoldDuration) != ""
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
	interval, intervalSet, err := parseOptionalMobilityDuration(res.ID()+" spec.ipOwnershipPolicy.heartbeatInterval", policy.HeartbeatInterval, true)
	if err != nil {
		return err
	}
	ttl, ttlSet, err := parseOptionalMobilityDuration(res.ID()+" spec.ipOwnershipPolicy.heartbeatTTL", policy.HeartbeatTTL, true)
	if err != nil {
		return err
	}
	hold, _, err := parseOptionalMobilityDuration(res.ID()+" spec.ipOwnershipPolicy.promotionHoldDuration", policy.PromotionHoldDuration, false)
	if err != nil {
		return err
	}
	_ = hold
	if policy.AutoFailover {
		if !intervalSet {
			return fmt.Errorf("%s spec.ipOwnershipPolicy.heartbeatInterval is required when autoFailover is true", res.ID())
		}
		if !ttlSet {
			return fmt.Errorf("%s spec.ipOwnershipPolicy.heartbeatTTL is required when autoFailover is true", res.ID())
		}
	}
	if intervalSet && ttlSet && ttl < interval {
		return fmt.Errorf("%s spec.ipOwnershipPolicy.heartbeatTTL must be >= heartbeatInterval", res.ID())
	}
	return nil
}

func parseOptionalMobilityDuration(field, raw string, positive bool) (time.Duration, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be a Go duration: %w", field, err)
	}
	if positive {
		if parsed <= 0 {
			return 0, false, fmt.Errorf("%s must be > 0", field)
		}
	} else if parsed < 0 {
		return 0, false, fmt.Errorf("%s must be >= 0", field)
	}
	return parsed, true, nil
}

type mobilityPlacementGroup struct {
	site        string
	role        string
	providerRef string
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
	if strings.TrimSpace(member.Capture.Type) != "provider-secondary-ip" {
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
		if existing.providerRef != current.providerRef {
			return fmt.Errorf("%s spec.members[%d].placement.group %q must use one providerRef; got %q and %q", res.ID(), index, group, existing.providerRef, current.providerRef)
		}
	} else {
		groups[group] = current
	}
	return nil
}

func validateMobilityMemberCapture(res api.Resource, index int, member api.MobilityPoolMember) error {
	captureType := strings.TrimSpace(member.Capture.Type)
	switch strings.TrimSpace(member.Delivery.Mode) {
	case "", "route":
	default:
		return fmt.Errorf("%s spec.members[%d].delivery.mode must be empty or route", res.ID(), index)
	}
	if captureType != "" && strings.TrimSpace(member.Delivery.PeerRef) == "" {
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
		if strings.TrimSpace(member.Capture.ActiveWhen.Type) != "vrrp-master" {
			return fmt.Errorf("%s spec.members[%d].capture.activeWhen.type must be vrrp-master for role onprem proxy-arp capture", res.ID(), index)
		}
		if strings.TrimSpace(member.Capture.ActiveWhen.VirtualAddressRef) == "" {
			return fmt.Errorf("%s spec.members[%d].capture.activeWhen.virtualAddressRef is required for role onprem proxy-arp capture", res.ID(), index)
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
		if strings.TrimSpace(member.Capture.NICRef) == "" {
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
