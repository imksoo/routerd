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
			if err := validateMobilityMemberCapture(res, i, member); err != nil {
				return true, err
			}
		}
		switch strings.TrimSpace(spec.CapturePolicy.Mode) {
		case "", "all-non-owner-sites":
		default:
			return true, fmt.Errorf("%s spec.capturePolicy.mode %q is not supported; only all-non-owner-sites", res.ID(), spec.CapturePolicy.Mode)
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
		case "", "route":
		default:
			return true, fmt.Errorf("%s spec.deliveryPolicy.mode %q is not supported; only route", res.ID(), spec.DeliveryPolicy.Mode)
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
