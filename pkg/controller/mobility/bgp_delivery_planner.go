// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type bgpDeliveryPlannerInput struct {
	PoolName             string
	Source               string
	Self                 memberPlanInfo
	Members              map[string]memberPlanInfo
	Spec                 api.MobilityPoolSpec
	Decisions            []ownershipDecision
	Placement            PlacementDecision
	InstalledNextHops    map[string][]string
	CaptureNextHops      map[string][]string
	RIBObserved          bool
	PreviousPlans        []dynamicconfig.ActionPlan
	Profiles             map[string]api.CloudProviderProfileSpec
	ActionJournal        []routerstate.ActionExecutionRecord
	ObservedSelfIPs      map[string]bool
	ObservedSelfCaptures map[string]bool
	ObservedSelfIPsOK    bool
	ObservedSelfAt       time.Time
	ForwardingObserved   bool
	ForwardingEnabled    bool
	ForwardingObservedAt time.Time
	ObservedStaleSince   map[string]time.Time
	SuppressDeprovision  bool
	LivenessMarkers      map[string]string
	Now                  time.Time
}

type bgpDeliveryPlannerResult struct {
	Paths                 []bgpdaemon.AppliedPath
	ActionPlans           []dynamicconfig.ActionPlan
	CaptureCandidates     map[string]bgpTrapCandidate
	Placement             PlacementDecision
	Distribution          *captureDistribution
}

func planBGPMobilityDelivery(in bgpDeliveryPlannerInput) (bgpDeliveryPlannerResult, error) {
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(in.Spec.Prefix))
	if err != nil {
		return bgpDeliveryPlannerResult{}, fmt.Errorf("parse pool prefix: %w", err)
	}
	poolPrefix = poolPrefix.Masked()
	now := in.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	decisions := decisionsByAddress(in.Decisions)
	failedActions := interpretProviderCaptureAssignFailures(in.ActionJournal, in.ObservedSelfCaptures).Active
	paths := planBGPAdvertisements(in.Source, in.Self, in.Decisions, in.Placement)
	captureNextHops := in.CaptureNextHops
	if len(captureNextHops) == 0 {
		captureNextHops = in.InstalledNextHops
	}
	candidates, dist := planCaptureCandidatesWithDistribution(in.Self, in.Members, decisions, in.Placement, captureNextHops, in.RIBObserved, in.PreviousPlans, in.ObservedSelfCaptures, failedActions, in.LivenessMarkers, poolPrefix, now)
	actionPlans, err := planCaptureActionPlans(in, candidates)
	if err != nil {
		return bgpDeliveryPlannerResult{}, err
	}
	return bgpDeliveryPlannerResult{
		Paths:                 paths,
		ActionPlans:           actionPlans,
		CaptureCandidates:     candidates,
		Placement:             in.Placement,
		Distribution:          dist,
	}, nil
}

func planBGPAdvertisements(source string, self memberPlanInfo, decisions []ownershipDecision, placement PlacementDecision) []bgpdaemon.AppliedPath {
	var out []bgpdaemon.AppliedPath
	for _, decision := range decisions {
		if !decisionAdvertisesFromSelf(decision, self) {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(decision.Address))
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			continue
		}
		out = append(out, bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: source,
			Prefix: prefix.Masked().String(),
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
			Attrs:  bgpMobilityPathAttrs(self, bgpDecisionSourceType(decision), placement.Active),
		}))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Prefix < out[j].Prefix
	})
	return out
}

func decisionAdvertisesFromSelf(decision ownershipDecision, self memberPlanInfo) bool {
	if strings.TrimSpace(decision.AdvertiseOwnerNode) != strings.TrimSpace(self.NodeRef) {
		return false
	}
	switch decision.Class {
	case ownershipClassStaticOwned, ownershipClassStaticHandover, ownershipClassLocalHomeOwned:
		return true
	default:
		return false
	}
}

func bgpDecisionSourceType(decision ownershipDecision) string {
	switch strings.TrimSpace(decision.Source) {
	case staticOwnedType:
		return staticOwnedType
	case staticHandoverType:
		return staticHandoverType
	default:
		return providerDiscoverySource
	}
}

func planCaptureCandidatesWithDistribution(self memberPlanInfo, members map[string]memberPlanInfo, decisions map[string]ownershipDecision, placement PlacementDecision, installedNextHops map[string][]string, ribObserved bool, previousPlans []dynamicconfig.ActionPlan, observedSelfIPs map[string]bool, failedActions map[string]routerstate.ActionExecutionRecord, livenessMarkers map[string]string, poolPrefix netip.Prefix, now time.Time) (map[string]bgpTrapCandidate, *captureDistribution) {
	out := map[string]bgpTrapCandidate{}
	if self.Capture.Type != "provider-secondary-ip" {
		return out, nil
	}
	for address, decision := range decisions {
		if desiredCaptureObservedOnSelf(decision, self, members, placement, observedSelfIPs) {
			out[address] = bgpTrapCandidate{ProtectOnly: true}
		}
	}
	group := strings.TrimSpace(self.PlacementGroup)
	distributed := distributedCaptureEnabled(members, group)
	if !distributed && !placement.Active {
		return out, nil
	}
	var selfAssigned map[string]bool
	var dist *captureDistribution
	if distributed {
		distributedPlacement := PlacementDecision{Group: group, Active: true, ActiveNode: self.NodeRef}
		eligibleAddresses := collectEligibleCaptureAddresses(self, members, decisions, distributedPlacement, installedNextHops, previousPlans, failedActions, poolPrefix)
		liveNodes := distributedLiveNodes(self, members, livenessMarkers)
		nodes := distributedCaptureNodes(members, group, liveNodes)
		d := distributeCaptures(eligibleAddresses, nodes)
		dist = &d
		selfAssigned = map[string]bool{}
		for addr, node := range d.Assignments {
			if node == self.NodeRef {
				selfAssigned[addr] = true
			}
		}
	}
	installedAddresses := map[string]bool{}
	for rawPrefix, nextHops := range installedNextHops {
		if len(cleanStrings(nextHops)) == 0 {
			continue
		}
		if address, ok := normalizeBGPTrapPrefix(rawPrefix, poolPrefix); ok {
			installedAddresses[address] = true
		}
	}
	for rawPrefix, nextHops := range installedNextHops {
		cleanNextHops := cleanStrings(nextHops)
		if len(cleanNextHops) == 0 {
			continue
		}
		address, ok := normalizeBGPTrapPrefix(rawPrefix, poolPrefix)
		if !ok {
			continue
		}
		decision, ok := decisions[address]
		if !ok {
			continue
		}
		if decision.Class == ownershipClassConfirmedCapture {
			if providerCaptureObservedOnSelf(decision, self, observedSelfIPs) {
				out[address] = bgpTrapCandidate{ProtectOnly: true}
			}
			continue
		}
		if !decisionEligibleForCapture(decision, self, members, placement) {
			if _, failed := failedActions[address]; !failed {
				continue
			}
		}
		if !routeTableCaptureAllowed(decision, self) {
			continue
		}
		if selfAssigned != nil && !selfAssigned[address] {
			continue
		}
		out[address] = bgpTrapCandidate{PathSig: bgpTrapPathSig(address, cleanNextHops), LastSeenAt: now.UTC(), Seize: placement.Seize}
	}
	for address, candidate := range previousBGPTrapCandidateAddresses(previousPlans, poolPrefix) {
		decision, ok := decisions[address]
		if !ok {
			continue
		}
		if !decisionEligibleForCapture(decision, self, members, placement) {
			_, failed := failedActions[address]
			if !failed && (ribObserved || !decisionIsCaptureNotDesiredStale(decision)) {
				if ribObserved && decisionIsCaptureNotDesiredStale(decision) && !decision.CaptureSucceeded && !installedAddresses[address] && bgpTrapCandidateWithinMissingHold(candidate, now) {
					out[address] = candidate
				}
				continue
			}
		}
		if decision.Class == ownershipClassConfirmedCapture {
			if providerCaptureObservedOnSelf(decision, self, observedSelfIPs) {
				out[address] = bgpTrapCandidate{ProtectOnly: true}
			}
			continue
		}
		if !routeTableCaptureAllowed(decision, self) {
			continue
		}
		if _, desired := out[address]; desired {
			continue
		}
		if selfAssigned != nil && !selfAssigned[address] {
			continue
		}
		if !ribObserved || bgpTrapCandidateWithinMissingHold(candidate, now) {
			if candidate.LastSeenAt.IsZero() {
				candidate.LastSeenAt = now.UTC()
			}
			candidate.Seize = placement.Seize
			out[address] = candidate
		}
	}
	return out, dist
}

func collectEligibleCaptureAddresses(self memberPlanInfo, members map[string]memberPlanInfo, decisions map[string]ownershipDecision, placement PlacementDecision, installedNextHops map[string][]string, previousPlans []dynamicconfig.ActionPlan, failedActions map[string]routerstate.ActionExecutionRecord, poolPrefix netip.Prefix) []string {
	eligible := map[string]bool{}
	for rawPrefix, nextHops := range installedNextHops {
		if len(cleanStrings(nextHops)) == 0 {
			continue
		}
		address, ok := normalizeBGPTrapPrefix(rawPrefix, poolPrefix)
		if !ok {
			continue
		}
		decision, ok := decisions[address]
		if !ok {
			continue
		}
		if decision.Class == ownershipClassConfirmedCapture {
			continue
		}
		if !decisionEligibleForCapture(decision, self, members, placement) {
			if _, failed := failedActions[address]; !failed {
				continue
			}
		}
		if !routeTableCaptureAllowed(decision, self) {
			continue
		}
		eligible[address] = true
	}
	for address := range previousBGPTrapCandidateAddresses(previousPlans, poolPrefix) {
		decision, ok := decisions[address]
		if !ok {
			continue
		}
		if decision.Class == ownershipClassConfirmedCapture {
			continue
		}
		if !routeTableCaptureAllowed(decision, self) {
			continue
		}
		eligible[address] = true
	}
	out := make([]string, 0, len(eligible))
	for addr := range eligible {
		out = append(out, addr)
	}
	sort.Strings(out)
	return out
}

func desiredCaptureObservedOnSelf(decision ownershipDecision, self memberPlanInfo, members map[string]memberPlanInfo, placement PlacementDecision, observedSelfIPs map[string]bool) bool {
	if !providerCaptureObservedOnSelf(decision, self, observedSelfIPs) {
		return false
	}
	if standbyShouldReleaseCapture(self, placement) {
		return false
	}
	if decision.Class == ownershipClassConfirmedCapture {
		return true
	}
	return decisionEligibleForCapture(decision, self, members, placement)
}

func standbyShouldReleaseCapture(self memberPlanInfo, placement PlacementDecision) bool {
	return !placement.Active &&
		strings.TrimSpace(placement.ActiveNode) != "" &&
		strings.TrimSpace(placement.ActiveNode) != strings.TrimSpace(self.NodeRef) &&
		placement.ActiveMarkerPresent
}

func providerCaptureObservedOnSelf(decision ownershipDecision, self memberPlanInfo, observedSelfIPs map[string]bool) bool {
	holder := firstNonEmpty(decision.CaptureHolderNode, decision.AdvertiseOwnerNode)
	if strings.TrimSpace(holder) != strings.TrimSpace(self.NodeRef) {
		return false
	}
	return observedSelfIPs[normalizeAddressString(decision.Address)]
}

func decisionEligibleForCapture(decision ownershipDecision, self memberPlanInfo, members map[string]memberPlanInfo, placement PlacementDecision) bool {
	if normalizeAddressString(decision.Address) == "" {
		return false
	}
	if strings.TrimSpace(decision.ConflictReason) != "" {
		return false
	}
	switch decision.Class {
	case ownershipClassLocalRouterSelf, ownershipClassStaticOwned, ownershipClassStaticHandover:
		return false
	case ownershipClassConfirmedCapture:
		return true
	case ownershipClassLocalHomeOwned:
		return decision.Source == providerDiscoverySource && decision.AdvertiseReason == "ownership-event"
	case ownershipClassStaleCapture:
		switch strings.TrimSpace(decision.SuppressionReason) {
		case "capture-not-desired", "local-router-self", "local-home-owner", "self-captured-secondary":
			return false
		case "fresh-home-owner":
			return strings.TrimSpace(decision.HomeOwnerNode) != "" &&
				strings.TrimSpace(decision.HomeOwnerNode) != strings.TrimSpace(self.NodeRef)
		default:
			return true
		}
	case ownershipClassRemoteHomeOwned:
		if strings.TrimSpace(decision.AdvertiseOwnerNode) == strings.TrimSpace(self.NodeRef) {
			return false
		}
		if owner, ok := lookupMemberByNodeRef(members, decision.HomeOwnerNode); ok && samePlacementSite(self, owner) && !placement.Active && !placement.Seize {
			return false
		}
		return true
	}
	return false
}

func decisionIsCaptureNotDesiredStale(decision ownershipDecision) bool {
	return decision.Class == ownershipClassStaleCapture && strings.TrimSpace(decision.SuppressionReason) == "capture-not-desired"
}

func samePlacementSite(a, b memberPlanInfo) bool {
	return strings.TrimSpace(a.PlacementGroup) != "" &&
		strings.TrimSpace(a.PlacementGroup) == strings.TrimSpace(b.PlacementGroup) &&
		strings.TrimSpace(a.Site) == strings.TrimSpace(b.Site)
}

func routeTableCaptureAllowed(decision ownershipDecision, self memberPlanInfo) bool {
	if effectiveCaptureStrategy("", captureStrategyValue(self.Capture)) != captureStrategyRouteTable {
		return true
	}
	if decision.Class == ownershipClassLocalRouterSelf || decision.Class == ownershipClassLocalHomeOwned {
		return false
	}
	if strings.TrimSpace(decision.AdvertiseOwnerNode) == strings.TrimSpace(self.NodeRef) {
		return false
	}
	nextHop := normalizeAddressString(strings.TrimSpace(self.CaptureTarget["nextHopIPAddress"]))
	if nextHop == "" {
		return true
	}
	address := normalizeAddressString(decision.Address)
	if address == nextHop {
		return false
	}
	addr, err := netip.ParseAddr(nextHop)
	if err != nil || !addr.Is4() {
		return true
	}
	return address != netip.PrefixFrom(addr, 32).String()
}

func planCaptureActionPlans(in bgpDeliveryPlannerInput, candidates map[string]bgpTrapCandidate) ([]dynamicconfig.ActionPlan, error) {
	if in.Self.Capture.Type != "provider-secondary-ip" {
		return nil, nil
	}
	plans, err := bgpProviderActionPlans(in.PoolName, in.Self.NodeRef, in.Spec, candidates, in.PreviousPlans, in.Profiles, in.ActionJournal, in.ObservedSelfCaptures, in.ObservedSelfIPsOK, in.ObservedSelfAt, in.ForwardingObserved, in.ForwardingEnabled, in.ForwardingObservedAt, in.SuppressDeprovision, standbyShouldReleaseCapture(in.Self, in.Placement), in.Now)
	if err != nil {
		return nil, err
	}
	if !in.SuppressDeprovision {
		observed, err := observedSelfStaleCaptureActionPlans(in, candidates)
		if err != nil {
			return nil, err
		}
		plans = append(plans, observed...)
	}
	return dedupeActionPlans(plans), nil
}

func observedSelfStaleCaptureActionPlans(in bgpDeliveryPlannerInput, candidates map[string]bgpTrapCandidate) ([]dynamicconfig.ActionPlan, error) {
	if !in.RIBObserved {
		return nil, nil
	}
	desired := map[string]bool{}
	for raw := range candidates {
		address := normalizeAddressString(raw)
		if address != "" {
			desired[address] = true
		}
	}
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(in.Spec.Prefix))
	if err != nil {
		return nil, fmt.Errorf("parse pool prefix: %w", err)
	}
	poolPrefix = poolPrefix.Masked()
	recentTrapCandidates := previousBGPTrapCandidateAddresses(in.PreviousPlans, poolPrefix)
	var staleAddresses []string
	staleSinceByAddress := map[string]time.Time{}
	releaseStandby := standbyShouldReleaseCapture(in.Self, in.Placement)
	for _, decision := range in.Decisions {
		address := normalizeAddressString(decision.Address)
		if address == "" || desired[address] {
			continue
		}
		if releaseStandby && in.ObservedSelfCaptures[address] {
			staleAddresses = append(staleAddresses, address)
			staleSinceByAddress[address] = in.Now.UTC()
			continue
		}
		if decision.Class != ownershipClassStaleCapture || strings.TrimSpace(decision.SuppressionReason) != "self-captured-secondary" {
			continue
		}
		if strings.TrimSpace(decision.CaptureHolderNode) != "" && strings.TrimSpace(decision.CaptureHolderNode) != strings.TrimSpace(in.Self.NodeRef) {
			continue
		}
		staleSince, ok := in.ObservedStaleSince[address]
		if !ok || staleSince.IsZero() || in.Now.UTC().Sub(staleSince.UTC()) < bgpTrapRIBMissingHold {
			continue
		}
		if candidate, ok := recentTrapCandidates[address]; ok && bgpTrapCandidateWithinMissingHold(candidate, in.Now) {
			continue
		}
		staleAddresses = append(staleAddresses, address)
		staleSinceByAddress[address] = staleSince
	}
	if len(staleAddresses) == 0 {
		return nil, nil
	}
	profile, ok := in.Profiles[strings.TrimSpace(in.Self.Capture.ProviderRef)]
	if !ok {
		return nil, fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q", in.Self.Capture.ProviderRef, in.PoolName, in.Self.NodeRef)
	}
	var plans []dynamicconfig.ActionPlan
	for _, address := range staleAddresses {
		unassign, err := providerUnassignActionPlan(in.PoolName, profile, in.Self.Capture, in.Self.CaptureTarget, address, in.Now)
		if err != nil {
			return nil, err
		}
		unassign = stampSingleBGPPathFence(unassign, address, bgpPathSigFromObservedSelfStale(address, staleSinceByAddress[address]), in.Self.NodeRef)
		plans = append(plans, unassign)
	}
	return plans, nil
}

func bgpPathSigFromObservedSelfStale(address string, staleSince time.Time) string {
	normalized := normalizeAddressString(address)
	if prefix, err := netip.ParsePrefix(normalized); err == nil && prefix.Addr().Is4() {
		normalized = prefix.Masked().String()
	} else if addr, err := netip.ParseAddr(normalized); err == nil && addr.Is4() {
		normalized = netip.PrefixFrom(addr, 32).String()
	}
	generation := staleSince.UTC().Format(time.RFC3339Nano)
	return "deprovision:" + normalized + ":observed-self-stale:since=" + generation
}

func decisionsByAddress(decisions []ownershipDecision) map[string]ownershipDecision {
	out := map[string]ownershipDecision{}
	for _, decision := range decisions {
		address := normalizeAddressString(decision.Address)
		if address == "" {
			continue
		}
		out[address] = decision
	}
	return out
}
