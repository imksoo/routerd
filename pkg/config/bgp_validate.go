// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"routerd/pkg/api"
)

func validateBGPRouterInstances(router *api.Router, vrfs map[string]bool) error {
	asnOwners := map[uint32]string{}
	instanceOwners := map[string]string{}
	var listens []bgpListenClaim
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "BGPRouter" {
			continue
		}
		spec, err := res.BGPRouterSpec()
		if err != nil {
			return err
		}
		if existing := asnOwners[spec.ASN]; existing != "" {
			return fmt.Errorf("%s spec.asn %d conflicts with %s; one routerd-managed FRR instance cannot reuse an ASN", res.ID(), spec.ASN, existing)
		}
		asnOwners[spec.ASN] = res.ID()
		vrfName := bgpVRFRefName(spec.VRF)
		if vrfName != "" && !vrfs[vrfName] {
			return fmt.Errorf("%s spec.vrf references missing VRF %q", res.ID(), spec.VRF)
		}
		instanceKey := defaultString(vrfName, "default")
		if existing := instanceOwners[instanceKey]; existing != "" {
			return fmt.Errorf("%s spec.vrf conflicts with %s; BGP VRF instance %q is already managed", res.ID(), existing, instanceKey)
		}
		instanceOwners[instanceKey] = res.ID()
		port := spec.Listen.Port
		if port == 0 {
			port = 179
		}
		claim := bgpListenClaim{Owner: res.ID(), Address: strings.TrimSpace(spec.Listen.Address), Port: port}
		for _, existing := range listens {
			if bgpListenClaimsConflict(existing, claim) {
				return fmt.Errorf("%s spec.listen conflicts with %s on %s", res.ID(), existing.Owner, bgpListenLabel(claim.Address, claim.Port))
			}
		}
		listens = append(listens, claim)
	}
	return nil
}

type bgpListenClaim struct {
	Owner   string
	Address string
	Port    int
}

func validateBGPRouterPolicy(resourceID string, spec api.BGPRouterSpec) error {
	imports, err := validateBGPPrefixList(resourceID, "spec.importPolicy.allowedPrefixes", spec.ImportPolicy.AllowedPrefixes)
	if err != nil {
		return err
	}
	connected, err := validateBGPPrefixList(resourceID, "spec.redistribute.connected.allowedPrefixes", spec.Redistribute.Connected.AllowedPrefixes)
	if err != nil {
		return err
	}
	static, err := validateBGPPrefixList(resourceID, "spec.redistribute.static.allowedPrefixes", spec.Redistribute.Static.AllowedPrefixes)
	if err != nil {
		return err
	}
	if err := validateNoBGPPrefixOverlap(resourceID, "spec.importPolicy.allowedPrefixes", imports, "spec.redistribute.connected.allowedPrefixes", connected); err != nil {
		return err
	}
	if err := validateNoBGPPrefixOverlap(resourceID, "spec.importPolicy.allowedPrefixes", imports, "spec.redistribute.static.allowedPrefixes", static); err != nil {
		return err
	}
	if err := validateNoBGPPrefixOverlap(resourceID, "spec.redistribute.connected.allowedPrefixes", connected, "spec.redistribute.static.allowedPrefixes", static); err != nil {
		return err
	}
	return validateBGPCommunities(resourceID, "spec.communities", spec.Communities)
}

func bgpVRFRefName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if kind, name, ok := strings.Cut(value, "/"); ok && kind == "VRF" {
		return strings.TrimSpace(name)
	}
	return value
}

func bgpListenClaimsConflict(a, b bgpListenClaim) bool {
	if a.Port != b.Port {
		return false
	}
	return a.Address == "" || b.Address == "" || a.Address == b.Address
}

func bgpListenLabel(address string, port int) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return "all addresses tcp/" + strconv.Itoa(port)
	}
	return address + " tcp/" + strconv.Itoa(port)
}

func validateBGPPrefixList(resourceID, path string, values []string) ([]netip.Prefix, error) {
	seen := map[string]int{}
	out := make([]netip.Prefix, 0, len(values))
	for i, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil || !prefix.Addr().Is4() {
			return nil, fmt.Errorf("%s %s[%d] must be an IPv4 prefix", resourceID, path, i)
		}
		prefix = prefix.Masked()
		key := prefix.String()
		if first, ok := seen[key]; ok {
			return nil, fmt.Errorf("%s %s[%d] duplicates %s[%d] %q", resourceID, path, i, path, first, key)
		}
		seen[key] = i
		out = append(out, prefix)
	}
	return out, nil
}

func validateNoBGPPrefixOverlap(resourceID, aPath string, a []netip.Prefix, bPath string, b []netip.Prefix) error {
	for _, left := range a {
		for _, right := range b {
			if bgpPrefixesOverlap(left, right) {
				return fmt.Errorf("%s %s %q overlaps %s %q", resourceID, aPath, left.String(), bPath, right.String())
			}
		}
	}
	return nil
}

func bgpPrefixesOverlap(a, b netip.Prefix) bool {
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func validateBGPCommunities(resourceID, path string, spec api.BGPCommunitiesSpec) error {
	switch strings.TrimSpace(spec.Send) {
	case "", "standard", "extended", "both":
	default:
		return fmt.Errorf("%s %s.send must be standard, extended, or both", resourceID, path)
	}
	for _, item := range []struct {
		field  string
		values []string
	}{
		{field: "accept", values: spec.Accept},
		{field: "set.in", values: spec.Set.In},
		{field: "set.out", values: spec.Set.Out},
	} {
		if err := validateBGPCommunityList(resourceID, path+"."+item.field, item.values); err != nil {
			return err
		}
	}
	return nil
}

func validateBGPCommunityList(resourceID, path string, values []string) error {
	seen := map[string]int{}
	for i, value := range values {
		value = strings.TrimSpace(value)
		if !validBGPCommunity(value) {
			return fmt.Errorf("%s %s[%d] must be asn:value or one of no-export, no-advertise, internet", resourceID, path, i)
		}
		if first, ok := seen[value]; ok {
			return fmt.Errorf("%s %s[%d] duplicates %s[%d] %q", resourceID, path, i, path, first, value)
		}
		seen[value] = i
	}
	return nil
}

func validBGPCommunity(value string) bool {
	switch value {
	case "no-export", "no-advertise", "internet":
		return true
	}
	left, right, ok := strings.Cut(value, ":")
	if !ok || left == "" || right == "" {
		return false
	}
	asn, err := strconv.ParseUint(left, 10, 32)
	if err != nil || asn == 0 {
		return false
	}
	community, err := strconv.ParseUint(right, 10, 16)
	return err == nil && community <= 65535
}
