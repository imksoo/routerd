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
	RIBObserved          bool
	PreviousPlans        []dynamicconfig.ActionPlan
	Profiles             map[string]api.CloudProviderProfileSpec
	ActionJournal        []routerstate.ActionExecutionRecord
	ObservedSelfIPs      map[string]bool
	ObservedSelfCaptures map[string]bool
	ObservedSelfIPsOK    bool
	ForwardingObserved   bool
	ForwardingEnabled    bool
	ForwardingObservedAt time.Time
	SuppressDeprovision  bool
	Now                  time.Time
}

type bgpDeliveryPlannerResult struct {
	Paths                 []bgpdaemon.AppliedPath
	ActionPlans           []dynamicconfig.ActionPlan
	CaptureCandidates     map[string]bgpTrapCandidate
	Placement             PlacementDecision
	ProviderCapturedPaths int
	SeizedPaths           int
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
	failedActions := latestFailedProviderActions(in.ActionJournal)
	paths, providerCaptured, seized := planBGPAdvertisements(in.Source, in.Self, in.Decisions, in.Placement, failedActions)
	candidates := planCaptureCandidates(in.Self, in.Members, decisions, in.Placement, in.InstalledNextHops, in.RIBObserved, in.PreviousPlans, in.ObservedSelfCaptures, poolPrefix, now)
	actionPlans, err := planCaptureActionPlans(in, candidates)
	if err != nil {
		return bgpDeliveryPlannerResult{}, err
	}
	return bgpDeliveryPlannerResult{
		Paths:                 paths,
		ActionPlans:           actionPlans,
		CaptureCandidates:     candidates,
		Placement:             in.Placement,
		ProviderCapturedPaths: providerCaptured,
		SeizedPaths:           seized,
	}, nil
}

func planBGPAdvertisements(source string, self memberPlanInfo, decisions []ownershipDecision, placement PlacementDecision, failedActions map[string]routerstate.ActionExecutionRecord) ([]bgpdaemon.AppliedPath, int, int) {
	var out []bgpdaemon.AppliedPath
	providerCaptured := 0
	seized := 0
	for _, decision := range decisions {
		if !decisionAdvertisesFromSelf(decision, self) {
			continue
		}
		if decision.Class == ownershipClassConfirmedCapture {
			if self.MaintenanceDrain {
				continue
			}
			if _, failed := failedActions[normalizeAddressString(decision.Address)]; failed {
				continue
			}
			providerCaptured++
			if placement.Seize {
				seized++
			}
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(decision.Address))
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			continue
		}
		active := placement.Active
		if decision.Class == ownershipClassConfirmedCapture {
			active = true
		}
		out = append(out, bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: source,
			Prefix: prefix.Masked().String(),
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
			Attrs:  bgpMobilityPathAttrs(self, bgpDecisionSourceType(decision), active),
		}))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Prefix < out[j].Prefix
	})
	return out, providerCaptured, seized
}

func decisionAdvertisesFromSelf(decision ownershipDecision, self memberPlanInfo) bool {
	if strings.TrimSpace(decision.AdvertiseOwnerNode) != strings.TrimSpace(self.NodeRef) {
		return false
	}
	return decision.Class != ownershipClassLocalRouterSelf
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

func planCaptureCandidates(self memberPlanInfo, members map[string]memberPlanInfo, decisions map[string]ownershipDecision, placement PlacementDecision, installedNextHops map[string][]string, ribObserved bool, previousPlans []dynamicconfig.ActionPlan, observedSelfIPs map[string]bool, poolPrefix netip.Prefix, now time.Time) map[string]bgpTrapCandidate {
	out := map[string]bgpTrapCandidate{}
	if self.Capture.Type != "provider-secondary-ip" || !placement.Active {
		return out
	}
	for address, decision := range decisions {
		if confirmedCaptureObservedOnSelf(decision, self, observedSelfIPs) {
			out[address] = bgpTrapCandidate{ProtectOnly: true}
		}
	}
	selfNextHop := bgpTrapSelfNextHop(placement.SelfMarker)
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
		if !ok || !decisionEligibleForCapture(decision, self, members, placement) {
			continue
		}
		if selfNextHop != "" && !bgpTrapHasRemoteNextHop(cleanNextHops, selfNextHop) {
			continue
		}
		if decision.Class == ownershipClassConfirmedCapture {
			if confirmedCaptureObservedOnSelf(decision, self, observedSelfIPs) {
				out[address] = bgpTrapCandidate{ProtectOnly: true}
			}
			continue
		}
		if !routeTableCaptureAllowed(decision, self) {
			continue
		}
		out[address] = bgpTrapCandidate{PathSig: bgpTrapPathSig(address, cleanNextHops), LastSeenAt: now.UTC(), Seize: placement.Seize}
	}
	for address, candidate := range previousBGPTrapCandidateAddresses(previousPlans, poolPrefix) {
		decision, ok := decisions[address]
		if !ok || !decisionEligibleForCapture(decision, self, members, placement) {
			continue
		}
		if decision.Class == ownershipClassConfirmedCapture {
			if confirmedCaptureObservedOnSelf(decision, self, observedSelfIPs) {
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
		if !ribObserved || bgpTrapCandidateWithinMissingHold(candidate, now) {
			if candidate.LastSeenAt.IsZero() {
				candidate.LastSeenAt = now.UTC()
			}
			candidate.Seize = placement.Seize
			out[address] = candidate
		}
	}
	return out
}

func confirmedCaptureObservedOnSelf(decision ownershipDecision, self memberPlanInfo, observedSelfIPs map[string]bool) bool {
	if decision.Class != ownershipClassConfirmedCapture {
		return false
	}
	if strings.TrimSpace(decision.AdvertiseOwnerNode) != strings.TrimSpace(self.NodeRef) {
		return false
	}
	return observedSelfIPs[normalizeAddressString(decision.Address)]
}

func decisionEligibleForCapture(decision ownershipDecision, self memberPlanInfo, members map[string]memberPlanInfo, placement PlacementDecision) bool {
	if normalizeAddressString(decision.Address) == "" {
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
		case "fresh-home-owner", "local-router-self", "local-home-owner":
			return false
		default:
			return true
		}
	}
	if strings.TrimSpace(decision.AdvertiseOwnerNode) == strings.TrimSpace(self.NodeRef) {
		return false
	}
	if decision.Class == ownershipClassRemoteHomeOwned {
		if owner, ok := lookupMemberByNodeRef(members, decision.HomeOwnerNode); ok && samePlacementSite(self, owner) && !placement.Seize {
			return false
		}
		if decision.HomeProviderRef != "" && self.Capture.ProviderRef != "" && strings.TrimSpace(decision.HomeProviderRef) != strings.TrimSpace(self.Capture.ProviderRef) {
			return false
		}
	}
	return true
}

func samePlacementSite(a, b memberPlanInfo) bool {
	return strings.TrimSpace(a.PlacementGroup) != "" &&
		strings.TrimSpace(a.PlacementGroup) == strings.TrimSpace(b.PlacementGroup) &&
		strings.TrimSpace(a.Site) == strings.TrimSpace(b.Site)
}

func routeTableCaptureAllowed(decision ownershipDecision, self memberPlanInfo) bool {
	if effectiveCaptureStrategy("", self.Capture.Strategy) != captureStrategyRouteTable {
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
	return bgpProviderActionPlans(in.PoolName, in.Self.NodeRef, in.Spec, candidates, in.PreviousPlans, in.Profiles, in.ActionJournal, in.ObservedSelfIPs, in.ObservedSelfIPsOK, in.ForwardingObserved, in.ForwardingEnabled, in.ForwardingObservedAt, in.SuppressDeprovision, in.Now)
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
