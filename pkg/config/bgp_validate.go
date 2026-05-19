// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"routerd/pkg/api"
)

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
