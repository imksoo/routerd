// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/sam"
)

// finalizeCaptureDispositions is the sole capture-decision boundary for a
// PoolPlan. It converts the candidate calculation into a durable, per-address
// disposition before either the provider or local-dataplane effector sees it.
// The two effectors must only project this result.
func finalizeCaptureDispositions(in *bgpDeliveryPlannerInput, captureNextHops map[string][]string, poolPrefix netip.Prefix, now time.Time) {
	for i := range in.Decisions {
		in.Decisions[i].CaptureDisposition = dynamicconfig.CaptureProhibited
		in.Decisions[i].CaptureReason = "capture is not desired"
		in.Decisions[i].CapturePathSig = ""
		in.Decisions[i].CaptureLastSeenAt = time.Time{}
		in.Decisions[i].CaptureSeize = false
		in.Decisions[i].CaptureReleasePathSig = ""
		in.Decisions[i].CaptureReleaseSince = time.Time{}
	}

	if strings.TrimSpace(in.Pool.Self.Capture.Type) == "proxy-arp" {
		finalizeProxyARPCaptureDispositions(in, poolPrefix)
		return
	}
	if strings.TrimSpace(in.Pool.Self.Capture.Type) != "provider-secondary-ip" {
		return
	}

	facts := captureDispositionFactsFor(in, captureNextHops, poolPrefix)
	releaseStandby := standbyShouldReleaseCapture(in.Pool.Self, in.Placement)

	// Keep a confirmed/eligible self capture safe while the control plane
	// decides whether it needs to retain or move it. This is intentionally
	// evaluated before candidate selection, because a later observed BGP path
	// may promote the same address from ProtectExisting to Desired.
	for i := range facts {
		fact := &facts[i]
		if !fact.captureObservedOnSelf || releaseStandby {
			continue
		}
		if fact.decision.Class == ownershipClassConfirmedCapture || fact.eligible {
			fact.set(dynamicconfig.CaptureProtectExisting, "provider capture observed")
		}
	}
	if in.Placement.SeizeHoldDown {
		for i := range facts {
			fact := &facts[i]
			if fact.observedOnSelf && fact.decision.CaptureDisposition == dynamicconfig.CaptureProhibited {
				fact.set(dynamicconfig.CaptureHold, "capture release is fenced while placement is unsettled")
			}
		}
		return
	}

	group := strings.TrimSpace(in.Pool.Self.PlacementGroup)
	distributed := distributedCaptureEnabled(in.Pool.Members, group)
	if !distributed && !in.Placement.Active {
		for i := range facts {
			fact := &facts[i]
			if fact.decision.CaptureDisposition != dynamicconfig.CaptureProhibited {
				continue
			}
			if previousCaptureShouldRelease(in, fact, releaseStandby) {
				fact.release("previous provider capture is no longer desired", now)
			} else if !fact.observedOnSelf {
				continue
			} else if releaseStandby {
				fact.release("provider capture is no longer desired", now)
			} else if staleSince, ok := observedStaleCaptureReleaseSince(in, fact, now); ok {
				fact.release("observed stale provider capture is safe to release", staleSince)
			} else {
				fact.set(dynamicconfig.CaptureHold, "capture release is fenced while placement is unsettled")
			}
		}
		return
	}

	assignedToSelf := map[string]bool(nil)
	if distributed {
		eligibleAddresses := make([]string, 0, len(facts))
		for _, fact := range facts {
			if fact.decision.Class == ownershipClassConfirmedCapture || !fact.distributionEligible || !fact.routeAllowed || (!fact.installed && !fact.hasPrevious) {
				continue
			}
			eligibleAddresses = append(eligibleAddresses, fact.address)
		}
		liveNodes := distributedLiveNodes(in.Pool.Self, in.Pool.Members, in.BGP.LivenessMarkers)
		assignments := distributeCaptures(eligibleAddresses, distributedCaptureNodes(in.Pool.Members, group, liveNodes))
		assignedToSelf = make(map[string]bool, len(assignments))
		for address, node := range assignments {
			assignedToSelf[address] = node == in.Pool.Self.NodeRef
		}
	}

	for i := range facts {
		fact := &facts[i]
		if fact.decision.Class != ownershipClassConfirmedCapture && fact.installed && fact.eligible && fact.routeAllowed && (assignedToSelf == nil || assignedToSelf[fact.address]) {
			pathSig := bgpTrapPathSig(fact.address, fact.nextHops)
			seize := captureCandidateSeize(in, fact.address)
			if !seize && fact.hasPrevious && fact.previous.Seize && fact.previous.PathSig == pathSig {
				// A successful provider assignment can take longer than inventory
				// observation. Keep its canonical fenced action while the exact
				// same BGP path remains desired, instead of creating a second,
				// lower-risk assignment solely because the journal is newer.
				seize = true
			}
			if !suppressInitialSameSiteSecondaryIPCapture(*fact.decision, in.Pool.Self, in.Pool.Members, seize, fact.hasPrevious) {
				fact.desire("installed BGP path", pathSig, now, seize)
			}
		}
		if fact.decision.CaptureDisposition != dynamicconfig.CaptureProhibited || !fact.hasPrevious || (assignedToSelf != nil && !assignedToSelf[fact.address]) {
			continue
		}
		if !fact.eligible {
			if in.BGP.RIBObserved() && decisionIsCaptureNotDesiredStale(*fact.decision) && !fact.decision.CaptureSucceeded && !fact.installed && previousCaptureCandidateWithinMissingHold(fact.previous, now) {
				seize := captureCandidateSeize(in, fact.address)
				if !seize {
					seize = fact.previous.Seize
				}
				fact.desire("previous BGP capture candidate retained", fact.previous.PathSig, fact.previous.LastSeenAt, seize)
			}
			continue
		}
		if fact.decision.Class == ownershipClassConfirmedCapture || !fact.routeAllowed || (in.BGP.RIBObserved() && !previousCaptureCandidateWithinMissingHold(fact.previous, now)) {
			continue
		}
		lastSeenAt := fact.previous.LastSeenAt
		if lastSeenAt.IsZero() {
			lastSeenAt = now.UTC()
		}
		seize := captureCandidateSeize(in, fact.address)
		if !seize {
			seize = fact.previous.Seize
		}
		if !suppressInitialSameSiteSecondaryIPCapture(*fact.decision, in.Pool.Self, in.Pool.Members, seize, fact.hasPrevious) {
			fact.desire("previous BGP capture candidate retained", fact.previous.PathSig, lastSeenAt, seize)
		}
	}

	finalizePreviousCaptureReleaseDispositions(in, facts, releaseStandby, now)
	finalizeObservedStaleCaptureDispositions(in, facts, now)
	for i := range facts {
		fact := &facts[i]
		if fact.observedOnSelf && fact.decision.CaptureDisposition == dynamicconfig.CaptureProhibited {
			// An observation alone is not authority to deprovision a provider
			// address. Keep it explicit in the plan until placement/ownership or
			// the stale-capture hold has produced a Release decision.
			fact.set(dynamicconfig.CaptureHold, "provider capture awaits a safe release decision")
		}
	}
}

// previousCaptureShouldRelease retains the old planner's cleanup contract in
// the single capture-decision core. A previous assign may have reached the
// provider before Discovery has observed it, so a drained holder must still
// issue a fenced teardown while inventory is unavailable. Once inventory has
// completed, secondary-IP teardown remains fail-closed unless the capture is
// actually observed. Route-table capture has no secondary-IP attachment and
// is safe to remove from the prior desired plan directly.
func previousCaptureShouldRelease(in *bgpDeliveryPlannerInput, fact *captureDispositionFact, releaseStandby bool) bool {
	if !fact.hasPrevious || fact.decision.CaptureDisposition != dynamicconfig.CaptureProhibited {
		return false
	}
	strategy := strings.TrimSpace(fact.decision.CaptureStrategy)
	if strategy == "" {
		strategy = providerCaptureStrategy(in.Pool.Self.Capture)
	}
	if strategy != captureStrategySecondaryIP {
		return true
	}
	if !in.Pool.Self.MaintenanceDrain && !releaseStandby {
		return false
	}
	return !in.Ownership.SelfInventoryKnown || fact.observedOnSelf
}

// finalizePreviousCaptureReleaseDispositions handles the active and
// distributed cases after candidate selection. It deliberately runs before
// stale-observation cleanup: an explicit prior desired assignment is stronger
// evidence than an uncorrelated inventory row, while the disposition remains
// the sole signal consumed by the provider projection.
func finalizePreviousCaptureReleaseDispositions(in *bgpDeliveryPlannerInput, facts []captureDispositionFact, releaseStandby bool, now time.Time) {
	for i := range facts {
		fact := &facts[i]
		if previousCaptureShouldRelease(in, fact, releaseStandby) {
			fact.release("previous provider capture is no longer desired", now)
		}
	}
}

type captureDispositionFact struct {
	decision              *ownershipDecision
	address               string
	nextHops              []string
	installed             bool
	hasPrevious           bool
	previous              previousCaptureCandidate
	observedOnSelf        bool
	captureObservedOnSelf bool
	eligible              bool
	distributionEligible  bool
	routeAllowed          bool
}

func captureDispositionFactsFor(in *bgpDeliveryPlannerInput, captureNextHops map[string][]string, poolPrefix netip.Prefix) []captureDispositionFact {
	nextHopsByAddress := map[string][]string{}
	for raw, nextHops := range captureNextHops {
		address, ok := normalizeBGPTrapPrefix(raw, poolPrefix)
		if !ok {
			continue
		}
		if cleaned := cleanStrings(nextHops); len(cleaned) > 0 {
			nextHopsByAddress[address] = cleaned
		}
	}
	previous := previousCaptureCandidates(in.Provider.ActionHistory.previousCaptureAssigns, poolPrefix)
	facts := make([]captureDispositionFact, 0, len(in.Decisions))
	for index := range in.Decisions {
		decision := &in.Decisions[index]
		address := normalizeAddressString(decision.Address)
		if address == "" {
			continue
		}
		nextHops := nextHopsByAddress[address]
		prior, hasPrevious := previous[address]
		eligible := decisionEligibleForCapture(*decision, in.Pool.Self, in.Pool.Members, in.Placement)
		facts = append(facts, captureDispositionFact{
			decision:              decision,
			address:               address,
			nextHops:              nextHops,
			installed:             len(nextHops) > 0,
			hasPrevious:           hasPrevious,
			previous:              prior,
			observedOnSelf:        in.Ownership.SelfCapturedIPs[address],
			captureObservedOnSelf: providerCaptureObservedOnSelf(*decision, in.Pool.Self, in.Ownership.SelfCapturedIPs),
			eligible:              eligible,
			// A distributed assignment deliberately does not use a seize to
			// claim a same-site provider home. Reuse the same provider-home
			// evaluation that direct capture eligibility and initial acquisition
			// use instead of carrying a second candidate rule.
			distributionEligible: eligible && !providerHomeNeedsSeize(*decision, in.Pool.Self, in.Pool.Members),
			routeAllowed:         routeTableCaptureAllowed(*decision, in.Pool.Self),
		})
	}
	return facts
}

func (f *captureDispositionFact) set(disposition dynamicconfig.CaptureDisposition, reason string) {
	f.decision.CaptureDisposition = disposition
	f.decision.CaptureReason = reason
	f.decision.CapturePathSig = ""
	f.decision.CaptureLastSeenAt = time.Time{}
	f.decision.CaptureSeize = false
}

func (f *captureDispositionFact) desire(reason, pathSig string, lastSeenAt time.Time, seize bool) {
	f.set(dynamicconfig.CaptureDesired, reason)
	f.decision.CapturePathSig = pathSig
	f.decision.CaptureLastSeenAt = lastSeenAt.UTC()
	f.decision.CaptureSeize = seize
}

func (f *captureDispositionFact) release(reason string, since time.Time) {
	f.set(dynamicconfig.CaptureRelease, reason)
	f.decision.CaptureReleaseSince = since.UTC()
	f.decision.CaptureReleasePathSig = bgpPathSigFromObservedSelfStale(f.address, f.decision.CaptureReleaseSince)
}

func captureCandidateSeize(in *bgpDeliveryPlannerInput, address string) bool {
	return in.Placement.Seize || shouldAllowBGPTrapReassignment(
		in.Pool.Self,
		address,
		in.Provider.ActionHistory,
		in.Ownership.SelfCapturedIPs,
		in.Ownership.SelfInventoryKnown,
		in.Ownership.DiscoveryLastScanAt,
	)
}

// captureNeedsResolution is the effect-shell gate for decisions that would
// acquire or protect a provider capture. It intentionally reads the final
// AddressDecision instead of reconstructing a separate candidate map.
func captureNeedsResolution(decisions []ownershipDecision) bool {
	for _, decision := range decisions {
		switch decision.CaptureDisposition {
		case dynamicconfig.CaptureDesired, dynamicconfig.CaptureProtectExisting:
			return true
		}
	}
	return false
}

// finalizeObservedStaleCaptureDispositions is deliberately part of the
// capture-decision core. Historically this logic lived in the provider action
// planner, which meant the provider effector re-read RIB and status facts to
// decide whether an address was safe to release.
func finalizeObservedStaleCaptureDispositions(in *bgpDeliveryPlannerInput, facts []captureDispositionFact, now time.Time) {
	if !in.BGP.RIBObserved() {
		return
	}
	for i := range facts {
		fact := &facts[i]
		if !fact.observedOnSelf || fact.decision.CaptureDisposition != dynamicconfig.CaptureProhibited {
			continue
		}
		if retainsStaleProviderCapture(*fact.decision, in.BGP.InstalledNextHops) {
			fact.set(dynamicconfig.CaptureProtectExisting, "observed provider capture remains protected")
			continue
		}
		if staleSince, ok := observedStaleCaptureReleaseSince(in, fact, now); ok {
			fact.release("observed stale provider capture is safe to release", staleSince)
		}
	}
}

func observedStaleCaptureReleaseSince(in *bgpDeliveryPlannerInput, fact *captureDispositionFact, now time.Time) (time.Time, bool) {
	staleSince, staleObserved := in.Previous.ObservedStaleSince[fact.address]
	if providerInventoryConflictReleasesLocalCapture(*fact.decision, in.Pool.Self.NodeRef) {
		return staleSince, staleObserved && !staleSince.IsZero() && now.UTC().Sub(staleSince.UTC()) >= bgpTrapRIBMissingHold
	}
	if fact.decision.Class != ownershipClassStaleCapture {
		return time.Time{}, false
	}
	if holder := strings.TrimSpace(fact.decision.CaptureHolderNode); holder != "" && holder != strings.TrimSpace(in.Pool.Self.NodeRef) {
		return time.Time{}, false
	}
	if !staleObserved || staleSince.IsZero() || now.UTC().Sub(staleSince.UTC()) < bgpTrapRIBMissingHold {
		return time.Time{}, false
	}
	if fact.hasPrevious && previousCaptureCandidateWithinMissingHold(fact.previous, now) {
		return time.Time{}, false
	}
	return staleSince, true
}

func retainsStaleProviderCapture(decision ownershipDecision, installedNextHops map[string][]string) bool {
	if strategy := strings.TrimSpace(decision.CaptureStrategy); strategy != "" && strategy != captureStrategySecondaryIP {
		return false
	}
	if decisionIsCaptureNotDesiredStale(decision) {
		return true
	}
	return decision.Class == ownershipClassStaleCapture &&
		decision.CaptureSucceeded &&
		strings.TrimSpace(decision.SuppressionReason) == "self-captured-secondary" &&
		len(cleanStrings(installedNextHops[normalizeAddressString(decision.Address)])) > 0
}

func finalizeProxyARPCaptureDispositions(in *bgpDeliveryPlannerInput, pool netip.Prefix) {
	pool = pool.Masked()
	byAddress := make(map[string]int, len(in.Decisions))
	for i := range in.Decisions {
		if address := normalizeAddressString(in.Decisions[i].Address); address != "" {
			byAddress[address] = i
		}
	}
	for raw, nextHops := range in.BGP.InstalledNextHops {
		if len(cleanStrings(nextHops)) == 0 {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || !pool.Contains(prefix.Addr()) {
			continue
		}
		address := prefix.Masked().String()
		if sam.CaptureExcludesAddress(in.Pool.Self.Capture, address) {
			continue
		}
		if index, ok := byAddress[address]; ok {
			in.Decisions[index].CaptureDisposition = dynamicconfig.CaptureDesired
			in.Decisions[index].CaptureReason = "installed BGP path"
		}
	}
}
