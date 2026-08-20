// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

type PlacementDecision struct {
	Group                 string
	Active                bool
	ActiveNode            string
	Reason                string
	Seize                 bool
	LivenessObserved      bool
	SelfCommunity         string
	SelfMarker            string
	SelfMarkerPresent     bool
	ActiveCommunity       string
	ActiveMarker          string
	ActiveMarkerPresent   bool
	ActiveIdentityNodeRef string
	SeizeHoldDown         bool
	SeizeHoldDownKey      string
	SeizeHoldDownSince    time.Time
	SeizeHoldDownUntil    time.Time
	StartupSettleUntil    time.Time
}

// PlacementObservations is the typed, controller-collected input to the
// shared placement state machine. It deliberately contains facts only: the
// pure core derives the current PlacementDecision from these observations.
// Both Mobility and Discovery construct this shape, which keeps their startup
// fencing, holder selection, and retention behavior identical.
type PlacementObservations struct {
	// StartedAt is supplied by the process lifecycle owner. Keeping it in the
	// snapshot makes placement deterministic and prevents package-load time
	// from becoming a hidden second state source.
	StartedAt time.Time
	// SettleWindow is normally the default below. It remains an observation so
	// tests and an explicitly configured lifecycle can use the same core.
	SettleWindow            time.Duration
	LivenessMarkers         map[string]string
	LivenessMarkersObserved bool
	PrefixCommunities       map[string][]string
	RIBObserved             bool
	ProviderObserved        bool
	SelfHolds               bool
	Previous                placementPreviousState
}

// EvaluatePlacementFromObservations is the single placement entry point for
// controller snapshots. Both Mobility and Discovery pass the same typed
// observation set through this function, so startup, holder, and hold-down
// behavior cannot drift between controllers.
func EvaluatePlacementFromObservations(self memberPlanInfo, members map[string]memberPlanInfo, observations PlacementObservations, now time.Time) (PlacementDecision, placementStartupReadiness) {
	startedAt, settleWindow := observations.startupTiming(now)
	startup := placementStartupReadinessForMember(self, observations.LivenessMarkersObserved || observations.RIBObserved, observations.ProviderObserved)
	observedHolder := bgpObservedGroupHolder(self, members, observations.LivenessMarkers, observations.PrefixCommunities)
	placement := evaluateBGPCapturePlacement(self, members, observations.LivenessMarkers, observations.LivenessMarkersObserved, observedHolder)
	placement = applyBGPCaptureSeizeHoldDown(observations.Previous, placement, now)
	placement = fencePlacementForStartupWithReadiness(placement, observedHolder, now, startedAt, settleWindow, startup)
	placement = applyHolderRetention(placement, observations.SelfHolds, higherPriorityHolderActive(self, members, observedHolder), now, startedAt, settleWindow)
	placement.StartupSettleUntil = startedAt.Add(settleWindow)
	return placement, startup
}

type memberPlanInfo struct {
	NodeRef                  string
	Site                     string
	Role                     string
	Capture                  api.MobilityMemberCapture
	CaptureSourceAddress     string
	CaptureSourceAddressFrom api.StatusValueSourceSpec
	OwnershipDiscovery       api.MobilityOwnershipDiscovery
	StaticOwnedAddresses     []string
	PlacementGroup           string
	PlacementPriority        int
	MaintenanceDrain         bool
	MaxSecondaryIPs          int
}

func plannerMembers(members []api.ResolvedMobilityPoolMember) map[string]memberPlanInfo {
	out := map[string]memberPlanInfo{}
	for _, member := range members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		capture := member.Capture
		discovery := member.OwnershipDiscovery
		out[nodeRef] = memberPlanInfo{
			NodeRef:                  nodeRef,
			Site:                     strings.TrimSpace(member.Site),
			Role:                     strings.TrimSpace(member.Role),
			Capture:                  capture,
			CaptureSourceAddress:     strings.TrimSpace(member.Capture.SourceAddress),
			CaptureSourceAddressFrom: member.Capture.SourceAddressFrom,
			OwnershipDiscovery:       discovery,
			StaticOwnedAddresses:     member.StaticOwnedAddresses,
			PlacementGroup:           strings.TrimSpace(member.Placement.Group),
			// NormalizeMobilityPool is the only priority-defaulting boundary.
			// Planner input is always normalized, so a second fallback here would
			// create a subtly different placement policy.
			PlacementPriority: member.Placement.Priority,
			MaintenanceDrain:  member.Maintenance.Drain,
			MaxSecondaryIPs:   member.MaxSecondaryIPs,
		}
	}
	return out
}

const defaultPlacementSettleWindow = 120 * time.Second

func (o PlacementObservations) startupTiming(now time.Time) (time.Time, time.Duration) {
	settleWindow := o.SettleWindow
	if settleWindow <= 0 {
		settleWindow = defaultPlacementSettleWindow
	}
	startedAt := o.StartedAt.UTC()
	if startedAt.IsZero() {
		// Direct callers without a lifecycle owner are deliberately treated as
		// already settled, including the longer readiness fallback window.
		// Production Runner wiring always supplies StartedAt; this fallback keeps
		// the pure core deterministic for isolated callers.
		startedAt = now.UTC().Add(-placementStartupReadinessFallbackWindow(settleWindow))
	}
	return startedAt, settleWindow
}

func placementObservationsFromFacts(startedAt time.Time, bgp BGPSnapshot, providerObserved, selfHolds bool, previous placementPreviousState) PlacementObservations {
	return PlacementObservations{
		StartedAt:               startedAt,
		LivenessMarkers:         bgp.LivenessMarkers,
		LivenessMarkersObserved: bgp.LivenessObserved,
		PrefixCommunities:       bgp.PrefixCommunities,
		RIBObserved:             bgp.RIBObserved(),
		ProviderObserved:        providerObserved,
		SelfHolds:               selfHolds,
		Previous:                previous,
	}
}

const placementStartupReadinessFallbackMultiplier = 3

type placementStartupReadiness struct {
	Known            bool
	BGPObserved      bool
	ProviderRequired bool
	ProviderObserved bool
}

func (r placementStartupReadiness) Ready() bool {
	return r.BGPObserved && (!r.ProviderRequired || r.ProviderObserved)
}

func placementStartupFenceDefersActive(active bool, incumbent string, sinceStart, settle time.Duration, readiness placementStartupReadiness) bool {
	if !active || strings.TrimSpace(incumbent) != "" {
		return false
	}
	if !readiness.Known {
		return sinceStart < settle
	}
	if readiness.Ready() {
		return false
	}
	return sinceStart < placementStartupReadinessFallbackWindow(settle)
}

func placementStartupReadinessFallbackWindow(settle time.Duration) time.Duration {
	if settle <= 0 {
		return 0
	}
	return settle * placementStartupReadinessFallbackMultiplier
}

func placementStartupReadinessForMember(self memberPlanInfo, bgpObserved, providerObserved bool) placementStartupReadiness {
	return placementStartupReadiness{
		Known:            true,
		BGPObserved:      bgpObserved,
		ProviderRequired: placementStartupProviderObservationRequired(self),
		ProviderObserved: providerObserved,
	}
}

func placementStartupProviderObservationRequired(self memberPlanInfo) bool {
	if strings.TrimSpace(self.Capture.Type) != "provider-secondary-ip" {
		return false
	}
	if strings.TrimSpace(self.Capture.ProviderRef) == "" {
		return false
	}
	return strings.TrimSpace(self.OwnershipDiscovery.Mode) == "provider-private-ip"
}

// applyHolderRetention keeps a node active while it still physically holds its
// group's captures, so the live holder never yields to the deterministic tie-break
// winner or to a transient peer observation (ADR 0016: yield only on losing your
// own holdership, never because a peer was observed). It applies only after the
// startup settle window so the selfHolds signal (the node's fresh provider
// self-capture observation) is trustworthy rather than the returning node's stale
// "I used to hold" memory.
// higherPriorityHolderActive reports that the observed active holder beacon belongs
// to a strictly higher-priority peer (lower priority number). When true the local
// holder must yield rather than retain, so a returning higher-priority node performs
// the configured priority restore instead of deadlocking against retention.
func higherPriorityHolderActive(self memberPlanInfo, members map[string]memberPlanInfo, observedHolder string) bool {
	holder := strings.TrimSpace(observedHolder)
	if holder == "" {
		return false
	}
	member, ok := lookupMemberByNodeRef(members, holder)
	if !ok || member.NodeRef == self.NodeRef {
		return false
	}
	return member.PlacementPriority < self.PlacementPriority
}

func applyHolderRetention(placement PlacementDecision, selfHolds bool, yieldToHigherPriority bool, now, startedAt time.Time, settleWindow time.Duration) PlacementDecision {
	if placement.Active || !selfHolds || yieldToHigherPriority {
		return placement
	}
	if now.Sub(startedAt) < settleWindow {
		return placement
	}
	placement.Active = true
	placement.Seize = false
	placement.Reason = fmt.Sprintf("holder retention: keeping active while self holds placement group %q captures", placement.Group)
	return placement
}

func fencePlacementForStartupWithReadiness(placement PlacementDecision, incumbent string, now, startedAt time.Time, settleWindow time.Duration, readiness placementStartupReadiness) PlacementDecision {
	if strings.TrimSpace(placement.Group) == "" {
		return placement
	}
	if !placementStartupFenceDefersActive(placement.Active, incumbent, now.Sub(startedAt), settleWindow, readiness) {
		return placement
	}
	placement.Active = false
	placement.Seize = false
	if readiness.Known {
		placement.Reason = fmt.Sprintf("startup readiness: deferring active assertion in placement group %q until BGP/provider observations converge", placement.Group)
	} else {
		placement.Reason = fmt.Sprintf("startup settle: deferring active assertion in placement group %q until peer-holder state converges", placement.Group)
	}
	return placement
}

// bgpObservedGroupHolder returns the live placement-group peer that is currently the
// active capture holder according to the fresh BGP RIB, or "" if no peer is. The
// active holder advertises the group's owner /32 at the active (higher) preference,
// so it is the best-path advertiser; mobilityPrefixCommunities carries the best-path
// communities, and the holder is identified by its node-identity community there.
// This is the holder-beacon: it is independent of the provider plugin (BGP is always
// present) and of a standby's lower-preference make-before-break advertisement
// (which never wins best path).
//
// It deliberately does NOT guard on self's own community: holder retention keeps the
// real holder active regardless, and a node defers only to a peer it actually sees on
// a best path. BGP best-path tie-break keeps exactly one advertiser best even when
// both briefly advertise at the active preference, so this cannot deadlock.
func bgpObservedGroupHolder(self memberPlanInfo, members map[string]memberPlanInfo, livenessMarkers map[string]string, mobilityPrefixCommunities map[string][]string) string {
	group := strings.TrimSpace(self.PlacementGroup)
	if group == "" {
		return ""
	}
	communityToPeer := map[string]string{}
	for _, member := range members {
		if strings.TrimSpace(member.PlacementGroup) != group || member.NodeRef == self.NodeRef {
			continue
		}
		if community, _, present := livenessMarkerForNode(livenessMarkers, member.NodeRef); present && strings.TrimSpace(community) != "" {
			communityToPeer[strings.TrimSpace(community)] = member.NodeRef
		}
	}
	if len(communityToPeer) == 0 {
		return ""
	}
	matched := ""
	for _, communities := range mobilityPrefixCommunities {
		holderPeer := ""
		hasActiveHolder := false
		for _, community := range communities {
			community = strings.TrimSpace(community)
			if community == bgpMobilityCommunityActiveHolder {
				hasActiveHolder = true
				continue
			}
			if node, ok := communityToPeer[community]; ok {
				if holderPeer == "" || node < holderPeer {
					holderPeer = node
				}
			}
		}
		// Only an owner /32 advertised at the active preference (carrying the
		// active-holder beacon) marks its advertiser as the group holder; a standby's
		// lower-preference advertisement and cold-start advertisements are ignored.
		if !hasActiveHolder || holderPeer == "" {
			continue
		}
		if matched == "" || holderPeer < matched {
			matched = holderPeer
		}
	}
	return matched
}

// evaluatePlacementWithIncumbent selects the active member for self's placement
// group. Members are ordered by ascending priority, then by NodeRef as a stable
// deterministic tie-break.
//
// On an equal-priority tie the current capture holder (incumbentHolder, observed
// from provider inventory) is preferred over the NodeRef tie-break so a returning
// peer does not preempt a live holder and trigger an avoidable capture handoff.
// A strictly higher-priority member (lower priority number) still reclaims, because
// the incumbent override only applies when the incumbent shares the top priority.
// An empty incumbentHolder reproduces the deterministic priority/NodeRef ordering,
// which also bootstraps the group before any holder has been observed.
func evaluatePlacementWithIncumbent(self memberPlanInfo, members map[string]memberPlanInfo, incumbentHolder string) PlacementDecision {
	group := strings.TrimSpace(self.PlacementGroup)
	if group == "" {
		return PlacementDecision{Active: true, ActiveNode: self.NodeRef}
	}
	candidates := make([]memberPlanInfo, 0, len(members))
	for _, member := range members {
		if strings.TrimSpace(member.PlacementGroup) != group || member.MaintenanceDrain {
			continue
		}
		candidates = append(candidates, member)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].PlacementPriority == candidates[j].PlacementPriority {
			return candidates[i].NodeRef < candidates[j].NodeRef
		}
		return candidates[i].PlacementPriority < candidates[j].PlacementPriority
	})
	if len(candidates) == 0 {
		return PlacementDecision{
			Group:  group,
			Active: false,
			Reason: fmt.Sprintf("placement group %q has no non-drained members", group),
		}
	}
	activeNode := candidates[0].NodeRef
	// No-preempt on equal priority: keep the live incumbent holder active instead
	// of the NodeRef tie-break winner when both share the top priority.
	if incumbent := strings.TrimSpace(incumbentHolder); incumbent != "" {
		if member, ok := lookupMemberByNodeRef(members, incumbent); ok &&
			member.NodeRef != activeNode &&
			strings.TrimSpace(member.PlacementGroup) == group &&
			!member.MaintenanceDrain &&
			member.PlacementPriority == candidates[0].PlacementPriority {
			activeNode = member.NodeRef
		}
	}
	if activeNode == self.NodeRef {
		return PlacementDecision{Group: group, Active: true, ActiveNode: activeNode}
	}
	return PlacementDecision{
		Group:      group,
		Active:     false,
		ActiveNode: activeNode,
		Reason:     fmt.Sprintf("placement group %q active node is %q", group, activeNode),
	}
}
