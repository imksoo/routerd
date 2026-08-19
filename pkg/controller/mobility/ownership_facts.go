// SPDX-License-Identifier: BSD-3-Clause
// Package mobility derives BGP /32 mobility paths and provider trap action
// plans from MobilityPool intent and federation observed facts.
package mobility

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// resolverEventOwnedAddress is the normalized local ownership fact used by
// the resolver.  The address itself is the map key, so callers cannot lose
// its normalized identity while converting a BGP ownership fact into a
// resolver input.
type resolverEventOwnedAddress struct {
	AdvertiseOwnerNode string
	SourceType         string
}

// providerInventoryOwnerFact is the normalized provider observation used by
// the ownership resolver. It deliberately carries provider identity separately
// from the MobilityPool status projection.
type providerInventoryOwnerFact struct {
	Address     string
	NodeRef     string
	Provider    string
	ProviderRef string
	SubnetRef   string
	NICRef      string
	ResourceRef string
	ObservedAt  time.Time
}

func resolverEventOwnedAddresses(pool NormalizedMobilityPool, events []routerstate.EventRecord, providerOwnedAddresses map[string]providerDiscoveryAddressFact, poolPrefix netip.Prefix, now time.Time) map[string]resolverEventOwnedAddress {
	spec := pool.Spec
	self := pool.Self
	selfNode := strings.TrimSpace(self.NodeRef)
	members := pool.Members
	owned := map[string]resolverEventOwnedAddress{}
	latest := map[string]routerstate.EventRecord{}
	latestByAddressSource := map[string]map[string]routerstate.EventRecord{}
	for address, ownerNode := range localStaticOwnerNodesByAddress(pool, poolPrefix) {
		owned[address] = resolverEventOwnedAddress{AdvertiseOwnerNode: ownerNode, SourceType: staticOwnedType}
	}
	for address, fact := range providerOwnedAddresses {
		if strings.TrimSpace(fact.InstanceState) == "stopped" && stoppedInstancePolicy(self.OwnershipDiscovery) == "hold" {
			continue
		}
		if address, ok := normalizeLeaseAddress(address, poolPrefix); ok {
			owned[address] = resolverEventOwnedAddress{AdvertiseOwnerNode: selfNode, SourceType: providerDiscoverySource}
		}
	}
	for _, ev := range events {
		if ev.Group != spec.GroupRef || ev.Type != ObservedEventType && ev.Type != ExpiredEventType {
			continue
		}
		if bgpOwnershipEventSourceType(ev) == providerDiscoverySource {
			continue
		}
		address, ok := normalizeLeaseAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), poolPrefix)
		if !ok {
			continue
		}
		current, found := latest[address]
		candidate := eventWithFallbackObservedAt(ev, now)
		if !found || eventRecordGreater(candidate, current) {
			latest[address] = candidate
		}
		sourceKey := bgpOwnershipEventSourceKey(candidate)
		if latestByAddressSource[address] == nil {
			latestByAddressSource[address] = map[string]routerstate.EventRecord{}
		}
		currentBySource, foundBySource := latestByAddressSource[address][sourceKey]
		if !foundBySource || eventRecordGreater(candidate, currentBySource) {
			latestByAddressSource[address][sourceKey] = candidate
		}
	}
	for address, bySource := range latestByAddressSource {
		for _, ev := range bySource {
			sourceType := bgpOwnershipEventSourceType(ev)
			if sourceType == staticHandoverType && (ev.Type == ExpiredEventType || (!ev.ExpiresAt.IsZero() && !now.Before(ev.ExpiresAt))) {
				delete(owned, address)
			}
		}
	}
	for address, bySource := range latestByAddressSource {
		for _, ev := range bySource {
			if ev.Type == ExpiredEventType || (!ev.ExpiresAt.IsZero() && !now.Before(ev.ExpiresAt)) {
				continue
			}
			sourceType := bgpOwnershipEventSourceType(ev)
			if bgpMemberAdvertisesOwnedAddress(self, members[strings.TrimSpace(ev.SourceNode)]) {
				if strings.TrimSpace(ev.Payload["instanceState"]) == "stopped" && stoppedInstancePolicy(self.OwnershipDiscovery) == "hold" {
					continue
				}
				owned[address] = resolverEventOwnedAddress{AdvertiseOwnerNode: selfNode, SourceType: sourceType}
			}
		}
	}
	for _, handover := range spec.StaticHandovers {
		if !bgpMemberAdvertisesOwnedAddress(self, members[strings.TrimSpace(handover.ToNodeRef)]) {
			continue
		}
		address, ok := normalizeLeaseAddress(handover.Address, poolPrefix)
		if !ok {
			continue
		}
		release := latest[address]
		if release.Type == ExpiredEventType && strings.TrimSpace(release.SourceNode) == strings.TrimSpace(handover.FromNodeRef) {
			owned[address] = resolverEventOwnedAddress{AdvertiseOwnerNode: selfNode, SourceType: staticHandoverType}
		}
	}
	return owned
}

func bgpOwnershipEventSourceType(ev routerstate.EventRecord) string {
	sourceType := strings.TrimSpace(ev.Payload["sourceType"])
	if sourceType == "" {
		sourceType = bgpMobilitySourceFromEvent(ev)
	}
	return sourceType
}

func bgpOwnershipEventSourceKey(ev routerstate.EventRecord) string {
	sourceType := bgpOwnershipEventSourceType(ev)
	if sourceType == "" {
		sourceType = "observed"
	}
	source := strings.TrimSpace(ev.Payload["source"])
	if source == "" {
		source = "event"
	}
	return strings.Join([]string{source, sourceType, strings.TrimSpace(ev.SourceNode)}, "\x00")
}

func providerInventoryHomeOwnerFactSets(pool NormalizedMobilityPool, poolPrefix netip.Prefix, runtime []providerDiscoveryRuntimeRecord, now time.Time) map[string][]providerInventoryOwnerFact {
	routerNICs := discoveryRouterNICRefs(pool)
	out := map[string][]providerInventoryOwnerFact{}
	for _, record := range runtime {
		for _, addressFact := range record.RuntimeFact.Addresses {
			if !providerDiscoveryAddressFactActive(addressFact, now) {
				continue
			}
			address, ok := normalizeLeaseAddress(addressFact.Address, poolPrefix)
			if !ok {
				continue
			}
			nicRef := strings.TrimSpace(addressFact.NICRef)
			if nicRef != "" && routerNICs[nicRef] || strings.TrimSpace(addressFact.ResourceType) == "router-nic" {
				continue
			}
			out[address] = append(out[address], providerInventoryOwnerFact{
				Address:     address,
				NodeRef:     strings.TrimSpace(record.NodeRef),
				Provider:    strings.TrimSpace(addressFact.Provider),
				ProviderRef: strings.TrimSpace(addressFact.ProviderRef),
				SubnetRef:   strings.TrimSpace(addressFact.SubnetRef),
				NICRef:      nicRef,
				ResourceRef: strings.TrimSpace(addressFact.ResourceRef),
				ObservedAt:  addressFact.ObservedAt.UTC(),
			})
		}
	}
	for address := range out {
		sort.SliceStable(out[address], func(i, j int) bool {
			return providerInventoryOwnerFactGreater(out[address][i], out[address][j])
		})
	}
	return out
}

func providerInventoryOwnerFactGreater(candidate, current providerInventoryOwnerFact) bool {
	return candidate.ObservedAt.After(current.ObservedAt) ||
		candidate.ObservedAt.Equal(current.ObservedAt) && candidate.NodeRef < current.NodeRef
}

func providerInventoryOwnerFactForNode(facts []providerInventoryOwnerFact, nodeRef string) (providerInventoryOwnerFact, bool) {
	nodeRef = strings.TrimSpace(nodeRef)
	for _, fact := range facts {
		if strings.TrimSpace(fact.NodeRef) == nodeRef {
			return fact, true
		}
	}
	return providerInventoryOwnerFact{}, false
}

func bgpMemberAdvertisesOwnedAddress(self, owner memberPlanInfo) bool {
	if strings.TrimSpace(self.NodeRef) == "" || strings.TrimSpace(owner.NodeRef) == "" {
		return false
	}
	if strings.TrimSpace(owner.NodeRef) == strings.TrimSpace(self.NodeRef) {
		return true
	}
	if strings.TrimSpace(self.PlacementGroup) == "" {
		return false
	}
	return strings.TrimSpace(self.PlacementGroup) == strings.TrimSpace(owner.PlacementGroup) &&
		strings.TrimSpace(self.Site) == strings.TrimSpace(owner.Site)
}

func eventRecordGreater(candidate, current routerstate.EventRecord) bool {
	candidateAt := candidate.ObservedAt.UTC()
	currentAt := current.ObservedAt.UTC()
	if candidateAt.After(currentAt) {
		return true
	}
	if candidateAt.Before(currentAt) {
		return false
	}
	return strings.TrimSpace(candidate.ID) > strings.TrimSpace(current.ID)
}

func eventWithFallbackObservedAt(event routerstate.EventRecord, fallback time.Time) routerstate.EventRecord {
	if event.ObservedAt.IsZero() {
		event.ObservedAt = fallback.UTC()
	}
	return event
}

func bgpMobilitySourceFromEvent(ev routerstate.EventRecord) string {
	switch strings.TrimSpace(ev.Type) {
	case staticOwnedType, staticHandoverType:
		return strings.TrimSpace(ev.Type)
	}
	switch strings.TrimSpace(ev.Payload["source"]) {
	case providerDiscoverySource:
		return providerDiscoverySource
	}
	return ""
}

// bgpProviderActionPlans projects already-finalized address decisions into
// provider actions. It deliberately does not inspect placement, status, or
// the BGP RIB: those facts are consumed once by finalizeCaptureDispositions.
// Releases are address-keyed and forwarding is provider/ref/NIC-keyed, so
// generated plan keys are unique. Its sole caller has already established a
// provider-secondary-ip capture type.
func bgpProviderActionPlans(poolName string, self memberPlanInfo, decisions []ownershipDecision, history ProviderActionHistory, profiles map[string]api.CloudProviderProfileSpec, forwardingObserved, forwardingEnabled bool, forwardingObservedAt time.Time, suppressDeprovision bool, now time.Time) ([]dynamicconfig.ActionPlan, error) {
	var plans []dynamicconfig.ActionPlan
	forwardingSeen := map[string]bool{}
	profileFor := func(ref string) (api.CloudProviderProfileSpec, error) {
		profile, ok := profiles[strings.TrimSpace(ref)]
		if !ok {
			return api.CloudProviderProfileSpec{}, fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q", ref, poolName, self.NodeRef)
		}
		return profile, nil
	}
	for _, decision := range decisions {
		if decision.CaptureDisposition != dynamicconfig.CaptureDesired {
			continue
		}
		address := normalizeAddressString(decision.Address)
		if address == "" {
			continue
		}
		profile, err := profileFor(self.Capture.ProviderRef)
		if err != nil {
			return nil, err
		}
		generated, err := providerActionPlans(poolName, profile, self.Capture, address, forwardingSeen, decision.CaptureSeize)
		if err != nil {
			return nil, err
		}
		stampBGPPathFenceActionPlans(generated, address, decision.CapturePathSig, self.NodeRef, decision.CaptureLastSeenAt)
		stampForwardingDriftFence(generated, forwardingObserved, forwardingEnabled, forwardingObservedAt)
		plans = append(plans, generated...)
	}
	if suppressDeprovision {
		return plans, nil
	}
	for _, decision := range decisions {
		if decision.CaptureDisposition != dynamicconfig.CaptureRelease {
			continue
		}
		address := normalizeAddressString(decision.Address)
		if address == "" {
			continue
		}
		capture := self.Capture
		pathSig := strings.TrimSpace(decision.CaptureReleasePathSig)
		if source, ok := history.releaseSourceFor(self, address); ok {
			capture = source.Capture
			if pathSig == "" {
				pathSig = source.PathSig
			}
		}
		profile, err := profileFor(firstNonEmpty(capture.ProviderRef, self.Capture.ProviderRef))
		if err != nil {
			return nil, err
		}
		unassign, err := providerCaptureActionPlan(poolName, profile, capture, address, false, false, now)
		if err != nil {
			return nil, err
		}
		unassignPlans := []dynamicconfig.ActionPlan{unassign}
		stampBGPPathFenceActionPlans(unassignPlans, address, pathSig, self.NodeRef, time.Time{})
		plans = append(plans, unassignPlans[0])
	}
	return plans, nil
}

type previousCaptureCandidate struct {
	PathSig    string
	LastSeenAt time.Time
	Seize      bool
}

func previousCaptureCandidates(previousPlans []dynamicconfig.ActionPlan, poolPrefix netip.Prefix) map[string]previousCaptureCandidate {
	seen := map[string]previousCaptureCandidate{}
	for _, plan := range previousPlans {
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
		address, ok := normalizeBGPTrapPrefix(plan.Target["address"], poolPrefix)
		if ok {
			pathSig := strings.TrimSpace(plan.Parameters[bgpPathSigParam])
			if pathSig == "" {
				pathSig = "previous:" + address
			}
			seen[address] = previousCaptureCandidate{
				PathSig:    pathSig,
				LastSeenAt: parseBGPTrapLastSeenAt(plan.Parameters[bgpTrapLastSeenAtParam]),
				Seize:      strings.EqualFold(strings.TrimSpace(plan.Parameters["allowReassignment"]), "true"),
			}
		}
	}
	return seen
}

func localStaticOwnerNodesByAddress(pool NormalizedMobilityPool, poolPrefix netip.Prefix) map[string]string {
	out := map[string]string{}
	spec := pool.Spec
	selfNode := pool.Self.NodeRef
	handoversByFrom := staticHandoversByFrom(spec.StaticHandovers, poolPrefix)
	for _, raw := range pool.Self.StaticOwnedAddresses {
		address, ok := normalizeLeaseAddress(raw, poolPrefix)
		if !ok {
			continue
		}
		if _, moving := handoversByFrom[staticHandoverKey(address, selfNode)]; moving {
			continue
		}
		out[address] = selfNode
	}
	return out
}
