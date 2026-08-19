// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/mobilityfib"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

const (
	ownershipClassLocalHomeOwned   = "LocalHomeOwned"
	ownershipClassLocalRouterSelf  = "LocalRouterSelf"
	ownershipClassRemoteHomeOwned  = "RemoteHomeOwned"
	ownershipClassConfirmedCapture = "ConfirmedCapture"
	ownershipClassStaleCapture     = "StaleCapture"
	ownershipClassStaticOwned      = "StaticOwned"
	ownershipClassStaticHandover   = "StaticHandover"
	ownershipClassUnknown          = "Unknown"

	captureStateNone      = "None"
	captureStateConfirmed = "Confirmed"
	captureStateStale     = "Stale"
)

// AddressDecision is the single per-/32 ownership and capture result emitted
// by the PoolPlan core.  The unexported alias below keeps package-local helper
// names stable while making the plan boundary explicit to other packages.
type AddressDecision struct {
	Address string
	Class   string
	// CaptureDisposition is the final local/provider capture decision for
	// this address. It is filled by the pure delivery planner after it has
	// considered placement, BGP observations, the previous plan, and provider
	// safety gates. Downstream effectors must project this value rather than
	// re-evaluating those inputs.
	CaptureDisposition dynamicconfig.CaptureDisposition
	CaptureReason      string
	// CapturePathSig, CaptureLastSeenAt, and CaptureSeize are selected at the
	// same decision boundary as CaptureDisposition. They are provider-action
	// fence inputs, not a second capture-candidate representation.
	CapturePathSig    string
	CaptureLastSeenAt time.Time
	CaptureSeize      bool
	// CaptureReleasePathSig is the fence selected together with a Release
	// disposition. It is intentionally part of the decision, rather than
	// being reconstructed by the provider effector from status or the RIB.
	CaptureReleasePathSig string
	CaptureReleaseSince   time.Time
	HomeOwnerNode         string
	HomeProviderRef       string
	HomeNICRef            string
	HomeResourceRef       string
	LocalNodeRef          string
	LocalProviderRef      string
	LocalNICRef           string
	LocalResourceRef      string
	LocalSource           string
	CaptureHolderNode     string
	CaptureProviderRef    string
	CaptureTargetRef      string
	CaptureStrategy       string
	CaptureState          string
	CaptureSucceeded      bool
	AdvertiseOwnerNode    string
	AdvertiseReason       string
	SuppressionReason     string
	ConflictReason        string
	ConflictWinnerNode    string
	ConflictResolution    string
	Source                string
}

type ownershipDecision = AddressDecision

// ownershipRuleContext is the complete immutable fact set for a single /32
// decision. Rules only read it and write their supplied decision.
type ownershipRuleContext struct {
	PoolRuntimeSnapshot
	self                 memberPlanInfo
	localStaticOwners    map[string]string
	providerHomeFactSets map[string][]providerInventoryOwnerFact
	remoteHomeFacts      map[string]providerInventoryOwnerFact
	remoteHomeConflicts  map[string][]providerInventoryOwnerFact
	eventOwned           map[string]resolverEventOwnedAddress
	selfIPs              map[string]bool
	capturedIPs          map[string]bool
	selfIPsObserved      bool
	confirmedCaptures    map[string]resolverCaptureState
	staleCaptures        map[string]resolverCaptureState
	handoverTargets      map[string]string
}

type ownershipRuleOutcome uint8

const (
	ownershipRuleContinue ownershipRuleOutcome = iota
	ownershipRuleResolved
	ownershipRuleDiscard
)

type ownershipRule func(ownershipRuleContext, string, *ownershipDecision) ownershipRuleOutcome

// ownershipRules is the ownership precedence order. The first matching rule
// decides the address; effectors only project the resulting decision.
var ownershipRules = []ownershipRule{
	duplicateProviderHomeOwnerRule,
	staticOwnerRule,
	staticHandoverRule,
	selfCapturedSecondaryRule,
	routeTableRouterSelfRule,
	providerLocalHomeRule,
	localOwnershipEventRule,
	remoteProviderHomeRule,
	confirmedCaptureRule,
	selfPrivateIPRule,
	bgpHomeOwnerRule,
	staleCaptureRule,
	unknownOwnershipRule,
}

func resolveAddressOwnership(in PoolRuntimeSnapshot) ([]ownershipDecision, error) {
	pool := in.Pool
	prefix := pool.Prefix
	if !prefix.IsValid() {
		return nil, fmt.Errorf("MobilityPool/%s normalized prefix is required", pool.Name)
	}
	now := in.Now.UTC()
	if now.IsZero() {
		return nil, fmt.Errorf("ownership resolver time is required")
	}
	self, ok := lookupMemberByNodeRef(pool.Members, pool.Self.NodeRef)
	if !ok {
		return nil, fmt.Errorf("self node %q is not a member of MobilityPool/%s", pool.Self.NodeRef, pool.Name)
	}
	localStaticOwners := localStaticOwnerNodesByAddress(pool, prefix)
	providerHomeFactSets := providerInventoryHomeOwnerFactSets(pool, prefix, in.Ownership.providerRuntime, now)
	remoteHomeFacts, remoteHomeConflicts := providerInventoryHomeOwnerFacts(providerHomeFactSets)
	providerOwned := map[string]providerDiscoveryAddressFact{}
	if runtime, found := in.Ownership.providerRuntimeForNode(pool.SelfNode); found {
		providerOwned = providerDiscoveryActiveAddressFacts(runtime.RuntimeFact.Addresses, prefix, now)
	}
	selfIPs, capturedIPs, selfIPsObserved := in.Ownership.SelfPrivateIPs, in.Ownership.SelfCapturedIPs, in.Ownership.SelfInventoryKnown
	selfIPsObservedAt := in.Ownership.DiscoveryLastScanAt
	eventOwned := resolverEventOwnedAddresses(pool, in.Events, providerOwned, prefix, now)
	confirmedCaptures, staleCaptures := captureStatesForSelf(self, in.Provider.ActionHistory, capturedIPs, selfIPsObserved, selfIPsObservedAt)
	handoverTargets := staticHandoverTargets(pool.Spec.StaticHandovers, prefix)
	universe := map[string]bool{}
	addAddressKeys(universe, localStaticOwners)
	addAddressKeys(universe, remoteHomeFacts)
	addAddressKeys(universe, remoteHomeConflicts)
	addAddressKeys(universe, eventOwned)
	addAddressKeys(universe, selfIPs)
	addAddressKeys(universe, capturedIPs)
	addAddressKeys(universe, confirmedCaptures)
	addAddressKeys(universe, staleCaptures)
	addAddressKeys(universe, handoverTargets)
	for raw := range in.BGP.InstalledNextHops {
		if address, ok := normalizeBGPTrapPrefix(raw, prefix); ok && !in.BGP.ReturnRoutes[address] {
			universe[address] = true
		}
	}
	for raw := range in.BGP.HomeOwnerNodes {
		if address, ok := normalizeBGPTrapPrefix(raw, prefix); ok {
			universe[address] = true
		}
	}
	ctx := ownershipRuleContext{
		PoolRuntimeSnapshot:  in,
		self:                 self,
		localStaticOwners:    localStaticOwners,
		providerHomeFactSets: providerHomeFactSets,
		remoteHomeFacts:      remoteHomeFacts,
		remoteHomeConflicts:  remoteHomeConflicts,
		eventOwned:           eventOwned,
		selfIPs:              selfIPs,
		capturedIPs:          capturedIPs,
		selfIPsObserved:      selfIPsObserved,
		confirmedCaptures:    confirmedCaptures,
		staleCaptures:        staleCaptures,
		handoverTargets:      handoverTargets,
	}
	var out []ownershipDecision
	for _, address := range mapsort.Keys(universe) {
		decision := ownershipDecision{Address: address, Class: ownershipClassUnknown, CaptureState: captureStateNone}
		if capture, ok := confirmedCaptures[address]; ok {
			applyCaptureState(&decision, captureStateConfirmed, capture)
		} else if capture, ok := staleCaptures[address]; ok {
			applyCaptureState(&decision, captureStateStale, capture)
		}
		if applyOwnershipRules(ctx, address, &decision) == ownershipRuleResolved {
			out = append(out, decision)
		}
	}
	return out, nil
}

func applyOwnershipRules(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	for _, rule := range ownershipRules {
		if outcome := rule(ctx, address, decision); outcome != ownershipRuleContinue {
			return outcome
		}
	}
	return ownershipRuleDiscard
}

func duplicateProviderHomeOwnerRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	facts := ctx.remoteHomeConflicts[address]
	if len(facts) <= 1 {
		return ownershipRuleContinue
	}
	winner := providerInventoryConflictWinner(facts, strings.TrimSpace(ctx.BGP.HomeOwnerNodes[address]))
	applyProviderHomeOwnerFact(decision, winner)
	if local, found := providerInventoryOwnerFactForNode(ctx.providerHomeFactSets[address], ctx.self.NodeRef); found && strings.TrimSpace(winner.NodeRef) != ctx.self.NodeRef {
		applyProviderLocalEvidence(decision, ctx.self, local)
		decision.LocalSource = providerDiscoverySource
	}
	decision.Source = providerDiscoverySource
	decision.SuppressionReason = "provider-home-owner-conflict"
	decision.ConflictReason = "duplicate-provider-home-owners"
	decision.ConflictWinnerNode = strings.TrimSpace(winner.NodeRef)
	decision.ConflictResolution = providerInventoryConflictResolution(*decision, ctx.self.NodeRef, ctx.capturedIPs[address])
	if providerInventoryConflictReleasesLocalCapture(*decision, ctx.self.NodeRef) {
		decision.Class = ownershipClassStaleCapture
		decision.SuppressionReason = "provider-split-brain-loser"
	} else {
		decision.Class = ownershipClassRemoteHomeOwned
	}
	return ownershipRuleResolved
}

func staticOwnerRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	owner := strings.TrimSpace(ctx.localStaticOwners[address])
	if owner == "" {
		return ownershipRuleContinue
	}
	decision.HomeOwnerNode = owner
	decision.Source = staticOwnedType
	decision.Class = ownershipClassStaticOwned
	decision.AdvertiseOwnerNode = ctx.self.NodeRef
	decision.AdvertiseReason = "static-owned"
	clearDisprovedStaleCapture(decision, ctx.self.NodeRef, ctx.capturedIPs, ctx.selfIPsObserved, address)
	return ownershipRuleResolved
}

func staticHandoverRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	toNode := strings.TrimSpace(ctx.handoverTargets[address])
	if toNode == "" {
		return ownershipRuleContinue
	}
	decision.HomeOwnerNode = toNode
	decision.Source = staticHandoverType
	if toNode == ctx.self.NodeRef {
		decision.Class = ownershipClassStaticHandover
		decision.AdvertiseOwnerNode = ctx.self.NodeRef
		decision.AdvertiseReason = "static-handover"
	} else {
		decision.Class = ownershipClassRemoteHomeOwned
		decision.SuppressionReason = "static-handover-to-remote"
	}
	return ownershipRuleResolved
}

func selfCapturedSecondaryRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	if !ctx.capturedIPs[address] {
		return ownershipRuleContinue
	}
	remoteFact, hasRemoteFact := ctx.remoteHomeFacts[address]
	bgpOwner := strings.TrimSpace(ctx.BGP.HomeOwnerNodes[address])
	if (hasRemoteFact && strings.TrimSpace(remoteFact.NodeRef) != "" && strings.TrimSpace(remoteFact.NodeRef) != ctx.self.NodeRef) || (bgpOwner != "" && bgpOwner != ctx.self.NodeRef) {
		return ownershipRuleContinue
	}
	if decision.CaptureState == captureStateNone {
		applyCaptureState(decision, captureStateStale, resolverCaptureState{
			HolderNode:  ctx.self.NodeRef,
			ProviderRef: strings.TrimSpace(ctx.self.Capture.ProviderRef),
			TargetRef:   providerCaptureRefFromCapture(ctx.self.Capture),
			Strategy:    providerCaptureStrategy(ctx.self.Capture),
		})
	}
	decision.Class = ownershipClassStaleCapture
	decision.SuppressionReason = "self-captured-secondary"
	decision.Source = "self-inventory"
	return ownershipRuleResolved
}

func routeTableRouterSelfRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	if !ctx.selfIPs[address] || decision.CaptureState == captureStateNone || decision.CaptureStrategy != captureStrategyRouteTable {
		return ownershipRuleContinue
	}
	applySelfInventoryOwner(decision, ctx.self)
	decision.Class = ownershipClassStaleCapture
	decision.SuppressionReason = "local-router-self"
	decision.Source = "self-inventory"
	return ownershipRuleResolved
}

func providerLocalHomeRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	fact, ok := ctx.remoteHomeFacts[address]
	if !ok || strings.TrimSpace(fact.NodeRef) != ctx.self.NodeRef || strings.TrimSpace(fact.ProviderRef) == "" {
		return ownershipRuleContinue
	}
	applyProviderHomeOwnerFact(decision, fact)
	decision.AdvertiseOwnerNode = ctx.self.NodeRef
	decision.Source = providerDiscoverySource
	decision.Class = ownershipClassLocalHomeOwned
	decision.AdvertiseReason = "provider-home-owner"
	return ownershipRuleResolved
}

func localOwnershipEventRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	eventOwner, ok := ctx.eventOwned[address]
	if !ok || strings.TrimSpace(eventOwner.AdvertiseOwnerNode) != ctx.self.NodeRef {
		return ownershipRuleContinue
	}
	if fact, remote := ctx.remoteHomeFacts[address]; remote && strings.TrimSpace(fact.NodeRef) != "" && strings.TrimSpace(fact.NodeRef) != ctx.self.NodeRef {
		decision.Class = ownershipClassRemoteHomeOwned
		applyProviderHomeOwnerFact(decision, fact)
		decision.LocalNodeRef = ctx.self.NodeRef
		decision.LocalSource = ownershipEventLocalSource(eventOwner.SourceType)
		decision.Source = providerDiscoverySource
		decision.SuppressionReason = "remote-home-owner"
		decision.ConflictReason = "remote-home-owner-overlaps-local-ownership-event"
		return ownershipRuleResolved
	}
	decision.Class = ownershipClassLocalHomeOwned
	decision.HomeOwnerNode = ctx.self.NodeRef
	decision.LocalNodeRef = ctx.self.NodeRef
	decision.LocalSource = ownershipEventLocalSource(eventOwner.SourceType)
	decision.AdvertiseOwnerNode = ctx.self.NodeRef
	decision.AdvertiseReason = "ownership-event"
	decision.Source = eventOwner.SourceType
	return ownershipRuleResolved
}

func remoteProviderHomeRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	fact, ok := ctx.remoteHomeFacts[address]
	if !ok || strings.TrimSpace(fact.NodeRef) == "" || strings.TrimSpace(fact.NodeRef) == ctx.self.NodeRef {
		return ownershipRuleContinue
	}
	applyProviderHomeOwnerFact(decision, fact)
	decision.Source = providerDiscoverySource
	if local, found := providerInventoryOwnerFactForNode(ctx.providerHomeFactSets[address], ctx.self.NodeRef); found {
		decision.ConflictReason = "remote-home-owner-overlaps-local-inventory"
		applyProviderLocalEvidence(decision, ctx.self, local)
		decision.LocalSource = providerDiscoverySource
	}
	homeProviderRef := strings.TrimSpace(fact.ProviderRef)
	selfProviderRef := strings.TrimSpace(ctx.self.Capture.ProviderRef)
	if decision.CaptureState == captureStateConfirmed && (homeProviderRef == "" || selfProviderRef == "" || homeProviderRef == selfProviderRef) {
		decision.Class = ownershipClassConfirmedCapture
		decision.AdvertiseReason = "confirmed-capture"
		decision.Source = "provider-action"
		return ownershipRuleResolved
	}
	if decision.CaptureState != captureStateNone || ctx.selfIPs[address] || ctx.capturedIPs[address] {
		decision.Class = ownershipClassStaleCapture
		decision.SuppressionReason = "fresh-home-owner"
	} else {
		decision.Class = ownershipClassRemoteHomeOwned
		decision.SuppressionReason = "remote-home-owner"
	}
	clearDisprovedStaleCapture(decision, ctx.self.NodeRef, ctx.capturedIPs, ctx.selfIPsObserved, address)
	return ownershipRuleResolved
}

func confirmedCaptureRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	if decision.CaptureState != captureStateConfirmed {
		return ownershipRuleContinue
	}
	if owner := strings.TrimSpace(ctx.BGP.HomeOwnerNodes[address]); owner != "" && owner != ctx.self.NodeRef {
		decision.HomeOwnerNode = owner
	}
	decision.Class = ownershipClassConfirmedCapture
	decision.AdvertiseReason = "confirmed-capture"
	decision.Source = "provider-action"
	return ownershipRuleResolved
}

func selfPrivateIPRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	if !ctx.selfIPs[address] {
		return ownershipRuleContinue
	}
	applySelfInventoryOwner(decision, ctx.self)
	decision.Class = ownershipClassLocalRouterSelf
	decision.LocalSource = "self-inventory"
	decision.Source = "self-inventory"
	return ownershipRuleResolved
}

func bgpHomeOwnerRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	owner := strings.TrimSpace(ctx.BGP.HomeOwnerNodes[address])
	if owner == "" {
		return ownershipRuleContinue
	}
	if owner == ctx.self.NodeRef {
		if decision.CaptureState != captureStateStale {
			return ownershipRuleContinue
		}
		decision.Class = ownershipClassStaleCapture
		decision.SuppressionReason = "capture-not-desired"
		return ownershipRuleResolved
	}
	decision.HomeOwnerNode = owner
	if decision.CaptureState == captureStateConfirmed {
		decision.Class = ownershipClassConfirmedCapture
		decision.AdvertiseReason = "confirmed-capture"
		decision.Source = "provider-action"
	} else {
		decision.Class = ownershipClassRemoteHomeOwned
		decision.SuppressionReason = "bgp-owner"
		decision.Source = "bgp-owner"
	}
	clearDisprovedStaleCapture(decision, ctx.self.NodeRef, ctx.capturedIPs, ctx.selfIPsObserved, address)
	return ownershipRuleResolved
}

func staleCaptureRule(_ ownershipRuleContext, _ string, decision *ownershipDecision) ownershipRuleOutcome {
	if decision.CaptureState != captureStateStale {
		return ownershipRuleContinue
	}
	decision.Class = ownershipClassStaleCapture
	decision.SuppressionReason = "capture-not-desired"
	decision.Source = "provider-action"
	return ownershipRuleResolved
}

func unknownOwnershipRule(ctx ownershipRuleContext, address string, decision *ownershipDecision) ownershipRuleOutcome {
	if ctx.BGP.ReturnRoutes[address] {
		return ownershipRuleDiscard
	}
	if decision.Source == "" {
		decision.Source = "bgp-rib"
	}
	return ownershipRuleResolved
}

func addAddressKeys[V any](universe map[string]bool, values map[string]V) {
	for address := range values {
		universe[address] = true
	}
}

func applyCaptureState(decision *ownershipDecision, state string, capture resolverCaptureState) {
	decision.CaptureState = state
	decision.CaptureHolderNode = capture.HolderNode
	decision.CaptureProviderRef = capture.ProviderRef
	decision.CaptureTargetRef = capture.TargetRef
	decision.CaptureStrategy = capture.Strategy
	decision.CaptureSucceeded = capture.Succeeded
}

func applyProviderLocalEvidence(decision *ownershipDecision, self memberPlanInfo, fact providerInventoryOwnerFact) {
	decision.LocalNodeRef = self.NodeRef
	decision.LocalProviderRef = strings.TrimSpace(fact.ProviderRef)
	decision.LocalNICRef = strings.TrimSpace(fact.NICRef)
	decision.LocalResourceRef = strings.TrimSpace(fact.ResourceRef)
}

func applySelfInventoryOwner(decision *ownershipDecision, self memberPlanInfo) {
	decision.HomeOwnerNode = self.NodeRef
	decision.HomeProviderRef = self.Capture.ProviderRef
	decision.HomeNICRef = self.Capture.NICRef
	decision.LocalNodeRef = self.NodeRef
	decision.LocalProviderRef = self.Capture.ProviderRef
	decision.LocalNICRef = self.Capture.NICRef
}

func clearDisprovedStaleCapture(decision *ownershipDecision, selfNode string, capturedIPs map[string]bool, selfIPsObserved bool, address string) {
	if decision == nil || decision.CaptureState != captureStateStale || !selfIPsObserved || capturedIPs[normalizeAddressString(address)] {
		return
	}
	if strings.TrimSpace(decision.CaptureHolderNode) != strings.TrimSpace(selfNode) {
		return
	}
	decision.CaptureState = captureStateNone
	decision.CaptureHolderNode = ""
	decision.CaptureProviderRef = ""
	decision.CaptureTargetRef = ""
	decision.CaptureStrategy = ""
	decision.CaptureSucceeded = false
}

func providerInventoryHomeOwnerFacts(sets map[string][]providerInventoryOwnerFact) (map[string]providerInventoryOwnerFact, map[string][]providerInventoryOwnerFact) {
	selected := map[string]providerInventoryOwnerFact{}
	conflicts := map[string][]providerInventoryOwnerFact{}
	for address, facts := range sets {
		if len(facts) == 0 {
			continue
		}
		selected[address] = facts[0]
		byOwner := map[string]providerInventoryOwnerFact{}
		for _, fact := range facts {
			identity := providerInventoryOwnerIdentity(fact)
			if identity == "" {
				continue
			}
			current, found := byOwner[identity]
			if !found || providerInventoryOwnerFactGreater(fact, current) {
				byOwner[identity] = fact
			}
		}
		if len(byOwner) < 2 {
			continue
		}
		rows := make([]providerInventoryOwnerFact, 0, len(byOwner))
		for _, fact := range byOwner {
			rows = append(rows, fact)
		}
		sort.SliceStable(rows, func(i, j int) bool {
			return providerInventoryOwnerFactGreater(rows[i], rows[j])
		})
		conflicts[address] = rows
	}
	return selected, conflicts
}

func providerInventoryConflictWinner(facts []providerInventoryOwnerFact, bgpOwner string) providerInventoryOwnerFact {
	bgpOwner = strings.TrimSpace(bgpOwner)
	if bgpOwner != "" {
		for _, fact := range facts {
			if strings.TrimSpace(fact.NodeRef) == bgpOwner {
				return fact
			}
		}
	}
	if len(facts) == 0 {
		return providerInventoryOwnerFact{}
	}
	winner := facts[0]
	for _, fact := range facts[1:] {
		if providerInventoryConflictWinnerLess(fact, winner) {
			winner = fact
		}
	}
	return winner
}

func providerInventoryConflictWinnerLess(a, b providerInventoryOwnerFact) bool {
	aKeys := []string{
		strings.TrimSpace(a.NodeRef),
		firstNonEmpty(a.ProviderRef, a.Provider),
		strings.TrimSpace(a.ResourceRef),
		strings.TrimSpace(a.NICRef),
		strings.TrimSpace(a.SubnetRef),
		normalizeAddressString(a.Address),
	}
	bKeys := []string{
		strings.TrimSpace(b.NodeRef),
		firstNonEmpty(b.ProviderRef, b.Provider),
		strings.TrimSpace(b.ResourceRef),
		strings.TrimSpace(b.NICRef),
		strings.TrimSpace(b.SubnetRef),
		normalizeAddressString(b.Address),
	}
	for i := range aKeys {
		if aKeys[i] == bKeys[i] {
			continue
		}
		if aKeys[i] == "" {
			return false
		}
		if bKeys[i] == "" {
			return true
		}
		return aKeys[i] < bKeys[i]
	}
	return false
}

func providerInventoryConflictResolution(decision ownershipDecision, selfNode string, selfCaptured bool) string {
	winner := strings.TrimSpace(decision.ConflictWinnerNode)
	selfNode = strings.TrimSpace(selfNode)
	if winner == "" {
		return "manual-review"
	}
	if winner == selfNode {
		return "winner-retain-local-capture"
	}
	if selfCaptured || strings.TrimSpace(decision.CaptureHolderNode) == selfNode {
		return "loser-release-local-capture"
	}
	return "loser-withhold-local-capture"
}

func providerInventoryConflictReleasesLocalCapture(decision ownershipDecision, selfNode string) bool {
	return strings.TrimSpace(decision.ConflictResolution) == "loser-release-local-capture" &&
		strings.TrimSpace(decision.ConflictWinnerNode) != "" &&
		strings.TrimSpace(decision.ConflictWinnerNode) != strings.TrimSpace(selfNode)
}

func providerInventoryOwnerIdentity(fact providerInventoryOwnerFact) string {
	providerRef := firstNonEmpty(fact.ProviderRef, fact.Provider)
	if providerRef == "" {
		return ""
	}
	resourceRef := strings.TrimSpace(fact.ResourceRef)
	if resourceRef != "" {
		return strings.Join([]string{providerRef, "resource", resourceRef}, "\x00")
	}
	nicRef := strings.TrimSpace(fact.NICRef)
	if nicRef != "" {
		return strings.Join([]string{providerRef, "nic", nicRef}, "\x00")
	}
	subnetRef := strings.TrimSpace(fact.SubnetRef)
	if subnetRef != "" {
		return strings.Join([]string{providerRef, "subnet", subnetRef, normalizeAddressString(fact.Address)}, "\x00")
	}
	return strings.Join([]string{providerRef, "address", normalizeAddressString(fact.Address)}, "\x00")
}

func applyProviderHomeOwnerFact(decision *ownershipDecision, fact providerInventoryOwnerFact) {
	decision.HomeOwnerNode = strings.TrimSpace(fact.NodeRef)
	decision.HomeProviderRef = strings.TrimSpace(fact.ProviderRef)
	decision.HomeNICRef = strings.TrimSpace(fact.NICRef)
	decision.HomeResourceRef = strings.TrimSpace(fact.ResourceRef)
}

func ownershipEventLocalSource(sourceType string) string {
	switch strings.TrimSpace(sourceType) {
	case OnPremSourceDHCPv4Lease, OnPremSourceARPObserver, OnPremSourceOnDemandARP, OnPremSourcePVESVNet:
		return onPremDiscoverySource
	default:
		return strings.TrimSpace(sourceType)
	}
}

type resolverCaptureState struct {
	HolderNode  string
	ProviderRef string
	TargetRef   string
	Strategy    string
	Succeeded   bool
}

func captureStatesForSelf(self memberPlanInfo, history ProviderActionHistory, selfIPs map[string]bool, selfIPsObserved bool, selfIPsObservedAt time.Time) (map[string]resolverCaptureState, map[string]resolverCaptureState) {
	confirmed := map[string]resolverCaptureState{}
	stale := map[string]resolverCaptureState{}
	latest := history.captureTransitions
	selfProviderRef := strings.TrimSpace(self.Capture.ProviderRef)
	selfTargetRef := providerCaptureRefFromCapture(self.Capture)
	for key, tr := range latest {
		parts := strings.Split(key, "\x00")
		if len(parts) != 3 {
			continue
		}
		providerRef, targetRef, address := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), normalizeAddressString(parts[2])
		if providerRef != selfProviderRef || targetRef != selfTargetRef || address == "" {
			continue
		}
		holder := firstNonEmpty(tr.plan.Parameters[captureParamHolder], self.NodeRef)
		state := resolverCaptureState{
			HolderNode:  holder,
			ProviderRef: providerRef,
			TargetRef:   targetRef,
			Strategy:    actionPlanCaptureStrategy(tr.plan),
			Succeeded:   tr.succeeded,
		}
		if tr.assign && tr.succeeded && selfIPs[address] && providerObservationFresh(selfIPsObservedAt, tr.at) {
			confirmed[address] = state
			continue
		}
		if tr.assign {
			stale[address] = state
		}
	}
	for _, plan := range history.previousCaptureAssigns {
		address := normalizeAddressString(plan.Target["address"])
		providerRef := firstNonEmpty(plan.ProviderRef, plan.Target["providerRef"])
		targetRef := providerCaptureRefFromTarget(plan.Target)
		if providerRef != selfProviderRef || targetRef != selfTargetRef || address == "" {
			continue
		}
		if _, ok := confirmed[address]; ok {
			continue
		}
		if _, ok := stale[address]; ok {
			continue
		}
		stale[address] = resolverCaptureState{
			HolderNode:  firstNonEmpty(plan.Parameters[captureParamHolder], self.NodeRef),
			ProviderRef: providerRef,
			TargetRef:   targetRef,
			Strategy:    actionPlanCaptureStrategy(plan),
		}
	}
	return confirmed, stale
}

func staticHandoverTargets(handovers []api.MobilityStaticHandover, poolPrefix netip.Prefix) map[string]string {
	out := map[string]string{}
	for _, handover := range handovers {
		address, ok := normalizeLeaseAddress(handover.Address, poolPrefix)
		if !ok {
			continue
		}
		if toNode := strings.TrimSpace(handover.ToNodeRef); toNode != "" {
			out[address] = toNode
		}
	}
	return out
}

func ownershipResolverFIBVerdict(d ownershipDecision) (string, string) {
	if strings.TrimSpace(d.ConflictReason) != "" {
		if strings.TrimSpace(d.LocalNodeRef) != "" && strings.TrimSpace(d.LocalSource) != "" {
			return mobilityfib.ActionLocalRoute, d.ConflictReason
		}
		return mobilityfib.ActionWithhold, d.ConflictReason
	}
	switch strings.TrimSpace(d.Class) {
	case ownershipClassRemoteHomeOwned, ownershipClassStaleCapture:
		if strings.TrimSpace(d.HomeOwnerNode) != "" {
			return mobilityfib.ActionDeliverRemote, firstNonEmpty(d.SuppressionReason, "remote-owner")
		}
	case ownershipClassConfirmedCapture:
		if owner := strings.TrimSpace(d.HomeOwnerNode); owner != "" && owner != strings.TrimSpace(d.LocalNodeRef) {
			return mobilityfib.ActionDeliverRemote, firstNonEmpty(d.AdvertiseReason, "confirmed-capture")
		}
	case ownershipClassLocalHomeOwned, ownershipClassLocalRouterSelf, ownershipClassStaticOwned, ownershipClassStaticHandover:
		if strings.TrimSpace(d.HomeOwnerNode) != "" || strings.TrimSpace(d.LocalNodeRef) != "" || strings.TrimSpace(d.AdvertiseOwnerNode) != "" {
			return mobilityfib.ActionLocalRoute, firstNonEmpty(d.AdvertiseReason, d.Source, "local-owner")
		}
	}
	return mobilityfib.ActionWithhold, firstNonEmpty(d.SuppressionReason, d.Source, "no-fib-owner")
}
