// SPDX-License-Identifier: BSD-3-Clause
package mobility

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

// statusRowMaps is the store-bound conversion for typed status rows.  The
// controller carries the concrete rows until this point; callers do not hand
// map-shaped status back into planning.
func statusRowMaps[T any](rows []T) []map[string]any {
	if len(rows) == 0 {
		return []map[string]any{}
	}
	return decodeStatusValue[[]map[string]any](rows)
}

func evaluateBGPCapturePlacement(self memberPlanInfo, members map[string]memberPlanInfo, livenessMarkers map[string]string, livenessMarkersObserved bool, observedHolderNode string) PlacementDecision {
	placement := evaluatePlacementWithIncumbent(self, members, observedHolderNode)
	placement.LivenessObserved = livenessMarkersObserved
	selfCommunity, selfMarker, selfMarkerPresent := livenessMarkerForNode(livenessMarkers, self.NodeRef)
	placement.SelfCommunity = selfCommunity
	placement.SelfMarker = selfMarker
	placement.SelfMarkerPresent = selfMarkerPresent
	if placement.Active || strings.TrimSpace(placement.ActiveNode) == "" {
		return placement
	}
	if !livenessMarkersObserved {
		return placement
	}
	if !selfMarkerPresent {
		return placement
	}
	active, ok := lookupMemberByNodeRef(members, placement.ActiveNode)
	if !ok {
		placement.Reason = fmt.Sprintf("placement group %q active node %q is not resolvable for BGP liveness identity", placement.Group, placement.ActiveNode)
		return placement
	}
	if strings.TrimSpace(active.NodeRef) == "" {
		return placement
	}
	activeCommunity, activeMarker, activeMarkerPresent := livenessMarkerForNode(livenessMarkers, active.NodeRef)
	placement.ActiveIdentityNodeRef = active.NodeRef
	placement.ActiveCommunity = activeCommunity
	placement.ActiveMarker = activeMarker
	placement.ActiveMarkerPresent = activeMarkerPresent
	if activeCommunity == "" || activeMarkerPresent {
		return placement
	}
	return PlacementDecision{
		Group:                 placement.Group,
		Active:                true,
		ActiveNode:            self.NodeRef,
		Reason:                fmt.Sprintf("placement group %q configured active %q has no live BGP identity path", placement.Group, active.NodeRef),
		Seize:                 true,
		LivenessObserved:      placement.LivenessObserved,
		SelfCommunity:         placement.SelfCommunity,
		SelfMarker:            placement.SelfMarker,
		SelfMarkerPresent:     placement.SelfMarkerPresent,
		ActiveIdentityNodeRef: active.NodeRef,
		ActiveCommunity:       activeCommunity,
		ActiveMarker:          activeMarker,
		ActiveMarkerPresent:   false,
	}
}

func firstNonEmpty(values ...string) string {
	return stringutil.FirstNonBlank(values...)
}

func applyBGPCaptureSeizeHoldDown(previous placementPreviousState, placement PlacementDecision, now time.Time) PlacementDecision {
	now = now.UTC()
	key := bgpSeizeHoldDownKey(placement)
	if !placement.Seize || key == "" {
		return placement
	}
	since := now
	if previous.SeizeHoldDownKey == key && !previous.SeizeHoldDownSince.IsZero() {
		since = previous.SeizeHoldDownSince.UTC()
	}
	until := since.Add(bgpSeizeLivenessMissingHold)
	placement.SeizeHoldDownKey = key
	placement.SeizeHoldDownSince = since
	placement.SeizeHoldDownUntil = until
	if !now.Before(until) {
		return placement
	}
	placement.SeizeHoldDown = true
	placement.Seize = false
	placement.Active = false
	if active := strings.TrimSpace(placement.ActiveIdentityNodeRef); active != "" {
		placement.ActiveNode = active
	}
	placement.Reason = strings.TrimSpace(stringutil.FirstNonBlank(placement.Reason, "active BGP liveness marker is absent")) +
		"; waiting for seize hold-down until " + until.Format(time.RFC3339Nano)
	return placement
}

func bgpSeizeHoldDownKey(placement PlacementDecision) string {
	if !placement.Seize {
		return ""
	}
	parts := []string{
		strings.TrimSpace(placement.Group),
		strings.TrimSpace(placement.ActiveIdentityNodeRef),
		strings.TrimSpace(placement.ActiveCommunity),
		strings.TrimSpace(placement.SelfCommunity),
	}
	if parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return ""
	}
	return strings.Join(parts, "\x00")
}

func lookupMemberByNodeRef(members map[string]memberPlanInfo, nodeRef string) (memberPlanInfo, bool) {
	nodeRef = strings.TrimSpace(nodeRef)
	if nodeRef == "" {
		return memberPlanInfo{}, false
	}
	member, ok := members[nodeRef]
	return member, ok
}

func livenessMarkerForNode(markers map[string]string, nodeRef string) (string, string, bool) {
	community := bgpstate.MobilityNodeIdentityCommunity(strings.TrimSpace(nodeRef))
	if community == "" {
		return "", "", false
	}
	marker := strings.TrimSpace(markers[community])
	return community, marker, marker != ""
}

func bgpMobilityPathAttrs(member memberPlanInfo, sourceType string, active bool) bgpdaemon.AppliedPathAttrs {
	communities := []string{bgpMobilityCommunityOwner}
	if nodeCommunity := bgpstate.MobilityNodeIdentityCommunity(strings.TrimSpace(member.NodeRef)); nodeCommunity != "" {
		communities = append(communities, nodeCommunity)
	}
	switch member.Role {
	case "onprem":
		communities = append(communities, bgpMobilityCommunityRoleOnPrem)
	case "cloud":
		communities = append(communities, bgpMobilityCommunityRoleCloud)
	}
	switch strings.TrimSpace(sourceType) {
	case staticOwnedType:
		communities = append(communities, bgpMobilityCommunitySourceStatic)
	case staticHandoverType:
		communities = append(communities, bgpMobilityCommunitySourceHandover)
	default:
		communities = append(communities, bgpMobilityCommunitySourceObserved)
	}
	localPref := bgpMobilityLocalPrefBase
	if active {
		localPref++
		communities = append(communities, bgpMobilityCommunityActiveHolder)
	}
	attrs := bgpdaemon.AppliedPathAttrs{LocalPref: localPref, Communities: communities}
	if member.PlacementPriority > 0 {
		attrs.MED = uint32(member.PlacementPriority)
	}
	return attrs
}

func bgpMobilityReturnRoutePathAttrs(member memberPlanInfo) bgpdaemon.AppliedPathAttrs {
	communities := []string{bgpMobilityCommunitySourceReturn}
	if nodeCommunity := bgpstate.MobilityNodeIdentityCommunity(strings.TrimSpace(member.NodeRef)); nodeCommunity != "" {
		communities = append(communities, nodeCommunity)
	}
	switch member.Role {
	case "onprem":
		communities = append(communities, bgpMobilityCommunityRoleOnPrem)
	case "cloud":
		communities = append(communities, bgpMobilityCommunityRoleCloud)
	}
	return bgpdaemon.AppliedPathAttrs{LocalPref: bgpMobilityLocalPrefBase, Communities: communities}
}

func staticHandoversByFrom(handovers []api.MobilityStaticHandover, prefix netip.Prefix) map[string]api.MobilityStaticHandover {
	out := map[string]api.MobilityStaticHandover{}
	for _, handover := range handovers {
		address, ok := normalizeLeaseAddress(handover.Address, prefix)
		if !ok {
			continue
		}
		fromNode := strings.TrimSpace(handover.FromNodeRef)
		if fromNode == "" {
			continue
		}
		out[staticHandoverKey(address, fromNode)] = handover
	}
	return out
}

func staticHandoverKey(address, fromNode string) string {
	return strings.TrimSpace(address) + "|" + strings.TrimSpace(fromNode)
}

func normalizeLeaseAddress(raw string, pool netip.Prefix) (string, bool) {
	prefix, ok := parseIPv4AddressOrPrefix(raw)
	if !ok || prefix.Bits() != 32 || !pool.Contains(prefix.Addr()) {
		return "", false
	}
	return netip.PrefixFrom(prefix.Addr(), 32).String(), true
}

// parseIPv4AddressOrPrefix accepts either an IPv4 address or CIDR. Prefix
// host bits are deliberately retained: a capture source of 192.0.2.7/24 is
// the source address 192.0.2.7, while callers that need a network apply
// Masked explicitly.
func parseIPv4AddressOrPrefix(raw string) (netip.Prefix, bool) {
	raw = strings.TrimSpace(raw)
	if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Addr().Is4() {
		return prefix, true
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil || !addr.Is4() {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(addr, 32), true
}

func durationDefault(raw string, fallback time.Duration) time.Duration {
	if parsed, err := time.ParseDuration(strings.TrimSpace(raw)); err == nil {
		return parsed
	}
	return fallback
}
