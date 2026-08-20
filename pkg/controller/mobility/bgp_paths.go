// SPDX-License-Identifier: BSD-3-Clause
package mobility

import (
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

// planBGPLivenessMarkerPath is a pure plan helper.  The shell resolves the
// marker prefix once and passes it in with the rest of the BGP observation.
func planBGPLivenessMarkerPath(source, selfNode, prefix string) (bgpdaemon.AppliedPath, bool) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return bgpdaemon.AppliedPath{}, false
	}
	nodeCommunity := bgpstate.MobilityNodeIdentityCommunity(strings.TrimSpace(selfNode))
	if nodeCommunity == "" {
		return bgpdaemon.AppliedPath{}, false
	}
	return bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
		Source: source,
		Prefix: prefix,
		Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
		Attrs: bgpdaemon.AppliedPathAttrs{
			LocalPref:   50,
			Communities: []string{bgpstate.MobilityCommunityNodeLiveness, nodeCommunity},
		},
	}), true
}

// planBGPReturnRoutePaths derives return paths from the typed discovery
// observation already carried in PoolRuntimeSnapshot.  It deliberately does
// not read MobilityPool status or reconstruct desired paths in the effect
// layer.
func planBGPReturnRoutePaths(source string, self memberPlanInfo, selfIPs, captured map[string]bool, primaryObserved bool) []bgpdaemon.AppliedPath {
	if !primaryObserved || len(selfIPs) == 0 {
		return nil
	}
	var out []bgpdaemon.AppliedPath
	addresses := make([]string, 0, len(selfIPs))
	for address := range selfIPs {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	for _, address := range addresses {
		if captured[address] {
			continue
		}
		prefix, err := netip.ParsePrefix(address)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			continue
		}
		out = append(out, bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: source,
			Prefix: prefix.Masked().String(),
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
			Attrs:  bgpMobilityReturnRoutePathAttrs(self),
		}))
	}
	return out
}

func (c Controller) selfLivenessMarkerPrefix(groupRef string) (string, bool) {
	if c.Router == nil {
		return "", false
	}
	if spec, found, err := api.LookupEventGroup(c.Router, groupRef); err == nil && found {
		listenAddress := strings.TrimSpace(spec.Listen.Address)
		if listenAddress != "" {
			addr, err := netip.ParseAddr(listenAddress)
			if err != nil || !addr.Is4() {
				return "", false
			}
			return netip.PrefixFrom(addr, 32).String(), true
		}
	}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "BGPRouter" {
			continue
		}
		spec, err := res.BGPRouterSpec()
		if err != nil {
			continue
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(spec.RouterID))
		if err == nil && addr.Is4() {
			return netip.PrefixFrom(addr, 32).String(), true
		}
	}
	return "", false
}
