// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/mobilityfib"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
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

type ownershipResolverInput struct {
	PoolName            string
	SelfNode            string
	Spec                api.MobilityPoolSpec
	Events              []routerstate.EventRecord
	Status              map[string]any
	ActionJournal       []routerstate.ActionExecutionRecord
	PreviousPlans       []dynamicconfig.ActionPlan
	InstalledNextHops   map[string][]string
	BGPHomeOwnerNodes   map[string]string
	BGPReturnRoutes     map[string]bool
	BGPLiveNodes        map[string]bool
	BGPLivenessObserved bool
	Now                 time.Time
}

type ownershipDecision struct {
	Address            string
	Class              string
	HomeOwnerNode      string
	HomeProviderRef    string
	HomeSubnetRef      string
	HomeNICRef         string
	HomeResourceRef    string
	HomeResourceType   string
	LocalNodeRef       string
	LocalProviderRef   string
	LocalSubnetRef     string
	LocalNICRef        string
	LocalResourceRef   string
	LocalResourceType  string
	LocalSource        string
	LocalSourceType    string
	CaptureHolderNode  string
	CaptureProviderRef string
	CaptureTargetRef   string
	CaptureStrategy    string
	CaptureState       string
	CaptureSucceeded   bool
	AdvertiseOwnerNode string
	AdvertiseReason    string
	SuppressionReason  string
	ConflictReason     string
	ConflictOwners     []providerInventoryOwnerFact
	Fresh              bool
	Source             string
}

func resolveAddressOwnership(in ownershipResolverInput) ([]ownershipDecision, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(in.Spec.Prefix))
	if err != nil {
		return nil, fmt.Errorf("parse pool prefix: %w", err)
	}
	prefix = prefix.Masked()
	now := in.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	members := plannerMembers(in.Spec.Members)
	self, ok := lookupMemberByNodeRef(members, in.SelfNode)
	if !ok {
		return nil, fmt.Errorf("self node %q is not a member of MobilityPool/%s", in.SelfNode, in.PoolName)
	}
	staticOwners := staticOwnedOwnerNodesByAddress(in.Spec)
	remoteHomeFactSets := providerInventoryHomeOwnerFactSets(in.PoolName, in.Spec, in.Events, now)
	remoteHomeFacts := selectedProviderInventoryHomeOwnerFacts(remoteHomeFactSets, members)
	remoteHomeConflicts := duplicateProviderHomeOwnerFacts(remoteHomeFactSets, members)
	localInventory := localInventoryRecordsFromStatus(in.Status, prefix)
	removeSelfResourceLocalInventory(localInventory, statusString(in.Status["discoverySelfResourceRef"]))
	discoveryOwned := statusStringSet(in.Status["discoveryOwnedAddresses"], prefix)
	selfIPs, capturedIPs, selfIPsObserved := selfInventoryAddressSetsFromStatus(in.Status, prefix)
	eventOwned := resolverEventOwnedAddresses(in.PoolName, in.SelfNode, in.Spec, in.Events, in.Status, prefix, now)
	confirmedCaptures, staleCaptures := captureStatesForSelf(self, in.PreviousPlans, in.ActionJournal, capturedIPs, selfIPsObserved)
	handoverTargets := staticHandoverTargets(in.Spec, prefix)
	universe := map[string]bool{}
	for address := range staticOwners {
		universe[address] = true
	}
	for address := range remoteHomeFacts {
		universe[address] = true
	}
	for address := range remoteHomeConflicts {
		universe[address] = true
	}
	for address := range localInventory {
		universe[address] = true
	}
	for address := range eventOwned {
		universe[address] = true
	}
	for address := range selfIPs {
		universe[address] = true
	}
	for address := range capturedIPs {
		universe[address] = true
	}
	for address := range confirmedCaptures {
		universe[address] = true
	}
	for address := range staleCaptures {
		universe[address] = true
	}
	for address := range handoverTargets {
		universe[address] = true
	}
	for raw := range in.InstalledNextHops {
		if address, ok := normalizeBGPTrapPrefix(raw, prefix); ok {
			if in.BGPReturnRoutes[address] {
				continue
			}
			universe[address] = true
		}
	}
	for raw := range in.BGPHomeOwnerNodes {
		if address, ok := normalizeBGPTrapPrefix(raw, prefix); ok {
			universe[address] = true
		}
	}
	var out []ownershipDecision
	for _, address := range mapKeysSorted(universe) {
		decision := ownershipDecision{
			Address:      address,
			Class:        ownershipClassUnknown,
			CaptureState: captureStateNone,
		}
		if facts := remoteHomeConflicts[address]; len(facts) > 1 {
			applyProviderHomeOwnerFact(&decision, facts[0])
			decision.Class = ownershipClassRemoteHomeOwned
			decision.Source = providerDiscoverySource
			decision.SuppressionReason = "provider-home-owner-conflict"
			decision.ConflictReason = "duplicate-provider-home-owners"
			decision.ConflictOwners = facts
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if capture, ok := confirmedCaptures[address]; ok {
			decision.CaptureState = captureStateConfirmed
			decision.CaptureHolderNode = capture.HolderNode
			decision.CaptureProviderRef = capture.ProviderRef
			decision.CaptureTargetRef = capture.TargetRef
			decision.CaptureStrategy = capture.Strategy
			decision.CaptureSucceeded = capture.Succeeded
		}
		if capture, ok := staleCaptures[address]; ok && decision.CaptureState == captureStateNone {
			decision.CaptureState = captureStateStale
			decision.CaptureHolderNode = capture.HolderNode
			decision.CaptureProviderRef = capture.ProviderRef
			decision.CaptureTargetRef = capture.TargetRef
			decision.CaptureStrategy = capture.Strategy
			decision.CaptureSucceeded = capture.Succeeded
		}
		if owner := strings.TrimSpace(staticOwners[address]); owner != "" {
			decision.HomeOwnerNode = owner
			decision.Source = staticOwnedType
			if owner == self.NodeRef {
				decision.Class = ownershipClassStaticOwned
				decision.AdvertiseOwnerNode = self.NodeRef
				decision.AdvertiseReason = "static-owned"
			} else {
				decision.Class = ownershipClassRemoteHomeOwned
				decision.SuppressionReason = "static-owned-by-remote"
			}
			clearDisprovedStaleCapture(&decision, self.NodeRef, capturedIPs, selfIPsObserved, address)
			out = append(out, decision)
			continue
		}
		if toNode := strings.TrimSpace(handoverTargets[address]); toNode != "" {
			decision.HomeOwnerNode = toNode
			decision.Source = staticHandoverType
			if toNode == self.NodeRef {
				decision.Class = ownershipClassStaticHandover
				decision.AdvertiseOwnerNode = self.NodeRef
				decision.AdvertiseReason = "static-handover"
			} else {
				decision.Class = ownershipClassRemoteHomeOwned
				decision.SuppressionReason = "static-handover-to-remote"
			}
			out = append(out, decision)
			continue
		}
		if capturedIPs[address] {
			remoteFact, hasRemoteFact := remoteHomeFacts[address]
			bgpOwner := strings.TrimSpace(in.BGPHomeOwnerNodes[address])
			if (!hasRemoteFact || strings.TrimSpace(remoteFact.NodeRef) == "" || strings.TrimSpace(remoteFact.NodeRef) == self.NodeRef) && (bgpOwner == "" || bgpOwner == self.NodeRef) {
				if decision.CaptureState == captureStateNone {
					decision.CaptureState = captureStateStale
					decision.CaptureHolderNode = self.NodeRef
					decision.CaptureProviderRef = strings.TrimSpace(self.Capture.ProviderRef)
					decision.CaptureTargetRef = providerCaptureRefFromCapture(self.Capture, self.CaptureTarget)
					decision.CaptureStrategy = effectiveCaptureStrategy("", captureStrategyValue(self.Capture))
				}
				decision.Class = ownershipClassStaleCapture
				decision.SuppressionReason = "self-captured-secondary"
				decision.Source = "self-inventory"
				decision.Fresh = true
				out = append(out, decision)
				continue
			}
			// Remote fresh-home facts below decide whether this is a valid
			// same-provider capture or a stale cross-home attachment.
		}
		if rec, ok := localInventory[address]; ok && localInventoryRecordIsRouterSelf(rec, self) {
			if decision.CaptureState != captureStateNone && decision.CaptureStrategy == captureStrategyRouteTable {
				decision.Class = ownershipClassStaleCapture
				decision.HomeOwnerNode = self.NodeRef
				decision.HomeProviderRef = firstNonEmpty(rec.ProviderRef, self.Capture.ProviderRef)
				decision.HomeSubnetRef = rec.SubnetRef
				decision.HomeNICRef = rec.NICRef
				decision.LocalNodeRef = self.NodeRef
				decision.LocalProviderRef = decision.HomeProviderRef
				decision.LocalSubnetRef = rec.SubnetRef
				decision.LocalNICRef = rec.NICRef
				decision.LocalResourceRef = rec.ResourceRef
				decision.LocalResourceType = rec.ResourceType
				decision.Source = "local-inventory"
				decision.SuppressionReason = "local-router-self"
				decision.Fresh = true
				out = append(out, decision)
				continue
			}
			decision.Class = ownershipClassLocalRouterSelf
			decision.HomeOwnerNode = self.NodeRef
			decision.HomeProviderRef = firstNonEmpty(rec.ProviderRef, self.Capture.ProviderRef)
			decision.HomeSubnetRef = rec.SubnetRef
			decision.HomeNICRef = rec.NICRef
			decision.LocalNodeRef = self.NodeRef
			decision.LocalProviderRef = decision.HomeProviderRef
			decision.LocalSubnetRef = rec.SubnetRef
			decision.LocalNICRef = rec.NICRef
			decision.LocalResourceRef = rec.ResourceRef
			decision.LocalResourceType = rec.ResourceType
			decision.Source = "local-inventory"
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if fact, ok := remoteHomeFacts[address]; ok && strings.TrimSpace(fact.NodeRef) == self.NodeRef && strings.TrimSpace(fact.ProviderRef) != "" {
			applyProviderHomeOwnerFact(&decision, fact)
			decision.AdvertiseOwnerNode = self.NodeRef
			decision.Source = providerDiscoverySource
			decision.Fresh = true
			decision.Class = ownershipClassLocalHomeOwned
			decision.AdvertiseReason = "provider-home-owner"
			out = append(out, decision)
			continue
		}
		if eventOwner, ok := eventOwned[address]; ok && strings.TrimSpace(eventOwner.AdvertiseOwnerNode) == self.NodeRef {
			if fact, remote := remoteHomeFacts[address]; remote && strings.TrimSpace(fact.NodeRef) != "" && strings.TrimSpace(fact.NodeRef) != self.NodeRef {
				decision.Class = ownershipClassRemoteHomeOwned
				applyProviderHomeOwnerFact(&decision, fact)
				decision.LocalNodeRef = self.NodeRef
				decision.LocalSource = ownershipEventLocalSource(eventOwner.SourceType)
				decision.LocalSourceType = eventOwner.SourceType
				decision.Source = providerDiscoverySource
				decision.SuppressionReason = "remote-home-owner"
				decision.ConflictReason = "remote-home-owner-overlaps-local-ownership-event"
				decision.Fresh = true
				out = append(out, decision)
				continue
			}
			decision.Class = ownershipClassLocalHomeOwned
			decision.HomeOwnerNode = self.NodeRef
			decision.LocalNodeRef = self.NodeRef
			decision.LocalSource = ownershipEventLocalSource(eventOwner.SourceType)
			decision.LocalSourceType = eventOwner.SourceType
			decision.AdvertiseOwnerNode = self.NodeRef
			decision.AdvertiseReason = "ownership-event"
			decision.Source = eventOwner.SourceType
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if fact, ok := remoteHomeFacts[address]; ok && strings.TrimSpace(fact.NodeRef) != "" && strings.TrimSpace(fact.NodeRef) != self.NodeRef {
			applyProviderHomeOwnerFact(&decision, fact)
			decision.Source = providerDiscoverySource
			decision.Fresh = true
			if rec, local := localInventory[address]; local {
				if remoteHomeOwnerUnavailableForLocalInventoryTakeover(self, members, in.BGPLiveNodes, in.BGPLivenessObserved, fact) {
					decision.Class = ownershipClassLocalHomeOwned
					decision.HomeOwnerNode = self.NodeRef
					decision.HomeProviderRef = firstNonEmpty(rec.ProviderRef, self.OwnershipDiscovery.ProviderRef, self.Capture.ProviderRef)
					decision.HomeSubnetRef = rec.SubnetRef
					decision.HomeNICRef = rec.NICRef
					decision.HomeResourceRef = rec.ResourceRef
					decision.HomeResourceType = rec.ResourceType
					decision.LocalNodeRef = self.NodeRef
					decision.LocalProviderRef = decision.HomeProviderRef
					decision.LocalSubnetRef = rec.SubnetRef
					decision.LocalNICRef = rec.NICRef
					decision.LocalResourceRef = rec.ResourceRef
					decision.LocalResourceType = rec.ResourceType
					decision.LocalSource = "local-inventory"
					decision.AdvertiseOwnerNode = self.NodeRef
					decision.AdvertiseReason = "local-home-inventory-takeover"
					decision.Source = "local-inventory"
					out = append(out, decision)
					continue
				}
				decision.LocalNodeRef = self.NodeRef
				decision.LocalProviderRef = firstNonEmpty(rec.ProviderRef, self.OwnershipDiscovery.ProviderRef, self.Capture.ProviderRef)
				decision.LocalSubnetRef = rec.SubnetRef
				decision.LocalNICRef = rec.NICRef
				decision.LocalResourceRef = rec.ResourceRef
				decision.LocalResourceType = rec.ResourceType
				decision.LocalSource = "local-inventory"
				if remoteHomeOwnerSharesPlacementSite(self, members, fact) {
					decision.Class = ownershipClassRemoteHomeOwned
					decision.SuppressionReason = "remote-home-owner"
					out = append(out, decision)
					continue
				}
				if !rec.Primary || localInventoryRecordIsSameSitePeerCapture(rec, self, members) {
					// A remote-home-owned address that appears in local provider
					// inventory only as a secondary IP is a distributed-capture
					// secondary (held by self or a same-site peer leaf for
					// delivery), not a competing home-ownership claim. Suppress
					// instead of raising a duplicate-owner conflict. Only a
					// primary local-inventory record (an actual client home on a
					// local NIC) overlapping a remote home owner is a real conflict.
					decision.Class = ownershipClassRemoteHomeOwned
					decision.SuppressionReason = "remote-home-owner"
					out = append(out, decision)
					continue
				}
				decision.ConflictReason = "remote-home-owner-overlaps-local-inventory"
			}
			homeProviderRef := strings.TrimSpace(fact.ProviderRef)
			selfProviderRef := strings.TrimSpace(self.Capture.ProviderRef)
			if decision.CaptureState == captureStateConfirmed && (homeProviderRef == "" || selfProviderRef == "" || homeProviderRef == selfProviderRef) {
				decision.Class = ownershipClassConfirmedCapture
				decision.AdvertiseReason = "confirmed-capture"
				decision.Source = "provider-action"
				out = append(out, decision)
				continue
			}
			if decision.CaptureState != captureStateNone || selfIPs[address] || capturedIPs[address] {
				decision.Class = ownershipClassStaleCapture
				decision.SuppressionReason = "fresh-home-owner"
			} else {
				decision.Class = ownershipClassRemoteHomeOwned
				decision.SuppressionReason = "remote-home-owner"
			}
			clearDisprovedStaleCapture(&decision, self.NodeRef, capturedIPs, selfIPsObserved, address)
			out = append(out, decision)
			continue
		}
		if decision.CaptureState == captureStateConfirmed {
			if owner := strings.TrimSpace(in.BGPHomeOwnerNodes[address]); owner != "" && owner != self.NodeRef {
				decision.HomeOwnerNode = owner
			}
			decision.Class = ownershipClassConfirmedCapture
			decision.AdvertiseReason = "confirmed-capture"
			decision.Source = "provider-action"
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if selfIPs[address] {
			decision.Class = ownershipClassLocalRouterSelf
			decision.HomeOwnerNode = self.NodeRef
			decision.HomeProviderRef = self.Capture.ProviderRef
			decision.HomeSubnetRef = statusString(in.Status["discoverySelfSubnetRef"])
			decision.HomeNICRef = self.Capture.NICRef
			decision.LocalNodeRef = self.NodeRef
			decision.LocalProviderRef = self.Capture.ProviderRef
			decision.LocalSubnetRef = decision.HomeSubnetRef
			decision.LocalNICRef = self.Capture.NICRef
			decision.LocalSource = "self-inventory"
			decision.Source = "self-inventory"
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if rec, ok := localInventory[address]; ok && discoveryOwned[address] {
			decision.Class = ownershipClassLocalHomeOwned
			decision.HomeOwnerNode = self.NodeRef
			decision.HomeProviderRef = firstNonEmpty(rec.ProviderRef, self.OwnershipDiscovery.ProviderRef, self.Capture.ProviderRef)
			decision.HomeSubnetRef = rec.SubnetRef
			decision.HomeNICRef = rec.NICRef
			decision.LocalNodeRef = self.NodeRef
			decision.LocalProviderRef = decision.HomeProviderRef
			decision.LocalSubnetRef = rec.SubnetRef
			decision.LocalNICRef = rec.NICRef
			decision.LocalResourceRef = rec.ResourceRef
			decision.LocalResourceType = rec.ResourceType
			decision.LocalSource = "local-inventory"
			decision.AdvertiseOwnerNode = self.NodeRef
			decision.AdvertiseReason = "local-home-inventory"
			decision.Source = "local-inventory"
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if owner := strings.TrimSpace(in.BGPHomeOwnerNodes[address]); owner != "" {
			if owner == self.NodeRef {
				if decision.CaptureState == captureStateStale {
					decision.Class = ownershipClassStaleCapture
					decision.SuppressionReason = "capture-not-desired"
					out = append(out, decision)
					continue
				}
			} else if decision.CaptureState == captureStateConfirmed {
				decision.HomeOwnerNode = owner
				decision.Source = "bgp-owner"
				decision.Class = ownershipClassConfirmedCapture
				decision.AdvertiseReason = "confirmed-capture"
				decision.Source = "provider-action"
			} else {
				decision.HomeOwnerNode = owner
				decision.Source = "bgp-owner"
				decision.Class = ownershipClassRemoteHomeOwned
				decision.SuppressionReason = "bgp-owner"
			}
			if decision.Class != ownershipClassUnknown {
				clearDisprovedStaleCapture(&decision, self.NodeRef, capturedIPs, selfIPsObserved, address)
				out = append(out, decision)
				continue
			}
		}
		if decision.CaptureState == captureStateStale {
			decision.Class = ownershipClassStaleCapture
			decision.SuppressionReason = "capture-not-desired"
			decision.Source = "provider-action"
			out = append(out, decision)
			continue
		}
		if in.BGPReturnRoutes[address] {
			continue
		}
		if decision.Source == "" {
			decision.Source = "bgp-rib"
		}
		out = append(out, decision)
	}
	return out, nil
}

func remoteHomeOwnerSharesPlacementSite(self memberPlanInfo, members map[string]memberPlanInfo, fact providerInventoryOwnerFact) bool {
	ownerNode := strings.TrimSpace(fact.NodeRef)
	if ownerNode == "" || ownerNode == strings.TrimSpace(self.NodeRef) {
		return false
	}
	owner, ok := lookupMemberByNodeRef(members, ownerNode)
	return ok && samePlacementSite(self, owner)
}

func remoteHomeOwnerUnavailableForLocalInventoryTakeover(self memberPlanInfo, members map[string]memberPlanInfo, liveNodes map[string]bool, livenessObserved bool, fact providerInventoryOwnerFact) bool {
	if !livenessObserved || strings.TrimSpace(self.NodeRef) == "" {
		return false
	}
	if !remoteHomeOwnerSharesPlacementSite(self, members, fact) {
		return false
	}
	ownerNode := strings.TrimSpace(fact.NodeRef)
	if liveNodes[strings.TrimSpace(self.NodeRef)] {
		return !liveNodes[ownerNode]
	}
	return false
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

func selectedProviderInventoryHomeOwnerFacts(sets map[string][]providerInventoryOwnerFact, members map[string]memberPlanInfo) map[string]providerInventoryOwnerFact {
	out := map[string]providerInventoryOwnerFact{}
	for address, facts := range sets {
		if len(facts) == 0 {
			continue
		}
		selected := facts[0]
		for _, fact := range facts[1:] {
			if providerInventoryOwnerFactPreferred(fact, selected, members) {
				selected = fact
			}
		}
		out[address] = selected
	}
	return out
}

func duplicateProviderHomeOwnerFacts(sets map[string][]providerInventoryOwnerFact, members map[string]memberPlanInfo) map[string][]providerInventoryOwnerFact {
	out := map[string][]providerInventoryOwnerFact{}
	for address, facts := range sets {
		byEndpoint := map[string]providerInventoryOwnerFact{}
		for _, fact := range facts {
			endpoint := providerInventoryOwnerFactEndpointKey(fact)
			if endpoint == "" {
				continue
			}
			current, found := byEndpoint[endpoint]
			if !found || providerInventoryOwnerFactPreferred(fact, current, members) {
				byEndpoint[endpoint] = fact
			}
		}
		if len(byEndpoint) < 2 {
			continue
		}
		rows := make([]providerInventoryOwnerFact, 0, len(byEndpoint))
		for _, fact := range byEndpoint {
			rows = append(rows, fact)
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if !rows[i].ObservedAt.Equal(rows[j].ObservedAt) {
				return rows[i].ObservedAt.After(rows[j].ObservedAt)
			}
			return rows[i].NodeRef < rows[j].NodeRef
		})
		out[address] = rows
	}
	return out
}

func providerInventoryOwnerFactEndpointKey(fact providerInventoryOwnerFact) string {
	parts := []string{
		strings.TrimSpace(fact.Provider),
		strings.TrimSpace(fact.ProviderRef),
		strings.TrimSpace(fact.SubnetRef),
		strings.TrimSpace(fact.NICRef),
		strings.TrimSpace(fact.ResourceRef),
		strings.TrimSpace(fact.ResourceType),
	}
	empty := true
	for _, part := range parts {
		if part != "" {
			empty = false
			break
		}
	}
	if empty {
		node := strings.TrimSpace(fact.NodeRef)
		if node == "" {
			return ""
		}
		parts = append(parts, node)
	}
	return strings.Join(parts, "\x00")
}

func providerInventoryOwnerFactPreferred(candidate, current providerInventoryOwnerFact, members map[string]memberPlanInfo) bool {
	candidateMember, candidateOK := lookupMemberByNodeRef(members, candidate.NodeRef)
	currentMember, currentOK := lookupMemberByNodeRef(members, current.NodeRef)
	if candidateOK && currentOK && candidateMember.PlacementPriority != currentMember.PlacementPriority {
		return candidateMember.PlacementPriority < currentMember.PlacementPriority
	}
	if candidateOK != currentOK {
		return candidateOK
	}
	candidateNode := strings.TrimSpace(candidate.NodeRef)
	currentNode := strings.TrimSpace(current.NodeRef)
	if candidateNode != currentNode {
		return candidateNode < currentNode
	}
	return providerInventoryOwnerFactGreater(candidate, current)
}

func applyProviderHomeOwnerFact(decision *ownershipDecision, fact providerInventoryOwnerFact) {
	decision.HomeOwnerNode = strings.TrimSpace(fact.NodeRef)
	decision.HomeProviderRef = strings.TrimSpace(fact.ProviderRef)
	decision.HomeSubnetRef = strings.TrimSpace(fact.SubnetRef)
	decision.HomeNICRef = strings.TrimSpace(fact.NICRef)
	decision.HomeResourceRef = strings.TrimSpace(fact.ResourceRef)
	decision.HomeResourceType = strings.TrimSpace(fact.ResourceType)
}

type resolverPrivateIPRecord struct {
	Address       string
	NICRef        string
	SubnetRef     string
	VPCRef        string
	ProviderRef   string
	ResourceRef   string
	ResourceType  string
	Primary       bool
	InstanceState string
}

func localInventoryRecordsFromStatus(status map[string]any, poolPrefix netip.Prefix) map[string]resolverPrivateIPRecord {
	out := map[string]resolverPrivateIPRecord{}
	for _, raw := range statusMapSlice(status["discoveryLocalInventory"]) {
		address, ok := normalizeDiscoveredAddress(firstNonEmpty(raw["address"], raw["ip"]), poolPrefix)
		if !ok {
			continue
		}
		out[address] = resolverPrivateIPRecord{
			Address:       address,
			NICRef:        strings.TrimSpace(raw["nicRef"]),
			SubnetRef:     strings.TrimSpace(raw["subnetRef"]),
			VPCRef:        strings.TrimSpace(raw["vpcRef"]),
			ProviderRef:   strings.TrimSpace(raw["providerRef"]),
			ResourceRef:   strings.TrimSpace(raw["resourceRef"]),
			ResourceType:  strings.TrimSpace(raw["resourceType"]),
			Primary:       strings.EqualFold(strings.TrimSpace(raw["primary"]), "true"),
			InstanceState: strings.TrimSpace(raw["instanceState"]),
		}
	}
	return out
}

func removeSelfResourceLocalInventory(records map[string]resolverPrivateIPRecord, selfResourceRef string) {
	selfResourceRef = strings.TrimSpace(selfResourceRef)
	if selfResourceRef == "" {
		return
	}
	for address, rec := range records {
		if strings.TrimSpace(rec.ResourceRef) == selfResourceRef {
			delete(records, address)
		}
	}
}

func selfInventoryAddressSetsFromStatus(status map[string]any, poolPrefix netip.Prefix) (map[string]bool, map[string]bool, bool) {
	privateIPs := map[string]bool{}
	capturedIPs := map[string]bool{}
	observed := false
	for _, key := range []string{"discoverySelfPrivateIPs", "discoverySelfCapturedAddresses"} {
		if _, ok := status[key]; ok {
			observed = true
		}
		for _, raw := range statusStringSlice(status[key]) {
			address, ok := normalizeDiscoveredAddress(raw, poolPrefix)
			if !ok {
				continue
			}
			if key == "discoverySelfCapturedAddresses" {
				capturedIPs[address] = true
			} else {
				privateIPs[address] = true
			}
		}
	}
	return privateIPs, capturedIPs, observed
}

type resolverEventOwnedAddress struct {
	AdvertiseOwnerNode string
	SourceType         string
}

func ownershipEventLocalSource(sourceType string) string {
	switch strings.TrimSpace(sourceType) {
	case OnPremSourceDHCPv4Lease, OnPremSourceARPObserver, OnPremSourceOnDemandARP, OnPremSourcePVESVNet:
		return onPremDiscoverySource
	default:
		return strings.TrimSpace(sourceType)
	}
}

func resolverEventOwnedAddresses(poolName, selfNode string, spec api.MobilityPoolSpec, events []routerstate.EventRecord, status map[string]any, poolPrefix netip.Prefix, now time.Time) map[string]resolverEventOwnedAddress {
	discoveryOwnedAddresses := statusStringSet(status["discoveryOwnedAddresses"], poolPrefix)
	discoveryOwnedObserved := statusHasAny(status, "discoveryOwnedAddresses")
	discoverySelfIPs, _, discoverySelfIPsObserved := selfInventoryAddressSetsFromStatus(status, poolPrefix)
	owned := bgpLocalOwnedAddressesFromConfigAndEvents(poolName, selfNode, spec, events, discoveryOwnedAddresses, discoveryOwnedObserved, discoverySelfIPs, discoverySelfIPsObserved, poolPrefix, now)
	out := map[string]resolverEventOwnedAddress{}
	for _, item := range owned {
		address := normalizeAddressString(item.Address)
		if address == "" {
			continue
		}
		out[address] = resolverEventOwnedAddress{AdvertiseOwnerNode: strings.TrimSpace(selfNode), SourceType: item.SourceType}
	}
	if self, ok := lookupMemberByNodeRef(plannerMembers(spec.Members), selfNode); ok && strings.TrimSpace(self.OwnershipDiscovery.Mode) == "onprem-l2" {
		for _, snapshot := range onPremObservedClientSnapshotsFromStatus(status) {
			for _, client := range snapshot.Clients {
				observation := onPremObservation{
					Action:     "observed",
					Address:    firstNonEmpty(client.Address, client.IP),
					MAC:        client.MAC,
					Interface:  snapshot.Interface,
					Network:    snapshot.Network,
					Bridge:     snapshot.Bridge,
					SourceType: firstNonEmpty(client.SourceType, snapshot.SourceType),
					ObservedAt: now,
				}
				address, ok := normalizeDiscoveredAddress(observation.Address, poolPrefix)
				if !ok || !discoveryScopeAllowsAddress(self.OwnershipDiscovery.Scope, address) {
					continue
				}
				if _, ok := matchingOnPremDiscoverySource(self, observation); !ok {
					continue
				}
				out[address] = resolverEventOwnedAddress{AdvertiseOwnerNode: strings.TrimSpace(selfNode), SourceType: observation.SourceType}
			}
		}
	}
	return out
}

func statusStringSet(value any, poolPrefix netip.Prefix) map[string]bool {
	out := map[string]bool{}
	for _, raw := range statusStringSlice(value) {
		address, ok := normalizeDiscoveredAddress(raw, poolPrefix)
		if ok {
			out[address] = true
		}
	}
	return out
}

func statusHasAny(status map[string]any, key string) bool {
	_, ok := status[key]
	return ok
}

func statusMapSlice(value any) []map[string]string {
	var out []map[string]string
	switch typed := value.(type) {
	case []map[string]string:
		return append([]map[string]string(nil), typed...)
	case []map[string]any:
		for _, item := range typed {
			out = append(out, anyMapToStringMap(item))
		}
	case []any:
		for _, item := range typed {
			switch v := item.(type) {
			case map[string]any:
				out = append(out, anyMapToStringMap(v))
			case map[string]string:
				out = append(out, v)
			}
		}
	}
	return out
}

func anyMapToStringMap(values map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out[key] = statusString(v)
	}
	return out
}

func statusString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func localInventoryRecordIsRouterSelf(rec resolverPrivateIPRecord, self memberPlanInfo) bool {
	nicRef := strings.TrimSpace(rec.NICRef)
	if nicRef == "" {
		return false
	}
	if nicRef == strings.TrimSpace(self.Capture.NICRef) {
		return true
	}
	return strings.TrimSpace(rec.ResourceType) == "router-nic"
}

func localInventoryRecordIsSameSitePeerCapture(rec resolverPrivateIPRecord, self memberPlanInfo, members map[string]memberPlanInfo) bool {
	nicRef := strings.TrimSpace(rec.NICRef)
	if nicRef == "" {
		return false
	}
	for _, member := range members {
		if strings.TrimSpace(member.NodeRef) == strings.TrimSpace(self.NodeRef) {
			continue
		}
		if member.Capture.Type != "provider-secondary-ip" {
			continue
		}
		if !samePlacementSite(self, member) {
			continue
		}
		if nicRef != strings.TrimSpace(member.Capture.NICRef) {
			continue
		}
		providerRef := strings.TrimSpace(rec.ProviderRef)
		memberProviderRef := strings.TrimSpace(member.Capture.ProviderRef)
		return providerRef == "" || memberProviderRef == "" || providerRef == memberProviderRef
	}
	return false
}

type resolverCaptureState struct {
	HolderNode  string
	ProviderRef string
	TargetRef   string
	Strategy    string
	Succeeded   bool
}

func captureStatesForSelf(self memberPlanInfo, previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, selfIPs map[string]bool, selfIPsObserved bool) (map[string]resolverCaptureState, map[string]resolverCaptureState) {
	confirmed := map[string]resolverCaptureState{}
	stale := map[string]resolverCaptureState{}
	latest := latestProviderCaptureTransitions(previousPlans, journal)
	selfProviderRef := strings.TrimSpace(self.Capture.ProviderRef)
	selfTargetRef := providerCaptureRefFromCapture(self.Capture, self.CaptureTarget)
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
			Strategy:    effectiveCaptureStrategy(tr.plan.Provider, firstNonEmpty(tr.plan.Target["captureStrategy"], captureStrategyValue(self.Capture))),
			Succeeded:   tr.succeeded,
		}
		if tr.assign && tr.succeeded && selfIPs[address] {
			confirmed[address] = state
			continue
		}
		if tr.assign {
			stale[address] = state
		}
	}
	for _, plan := range previousPlans {
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
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
			Strategy:    effectiveCaptureStrategy(plan.Provider, firstNonEmpty(plan.Target["captureStrategy"], captureStrategyValue(self.Capture))),
		}
	}
	return confirmed, stale
}

func staticHandoverTargets(spec api.MobilityPoolSpec, poolPrefix netip.Prefix) map[string]string {
	out := map[string]string{}
	for _, handover := range spec.StaticHandovers {
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

func ownershipResolverStatus(decisions []ownershipDecision) map[string]any {
	counts := map[string]int{}
	items := make([]map[string]any, 0, len(decisions))
	conflicts := []map[string]any{}
	staleClaims := []map[string]any{}
	unknownClaims := []map[string]any{}
	for _, d := range decisions {
		counts[d.Class]++
		item := map[string]any{
			"address": d.Address,
			"class":   d.Class,
			"source":  d.Source,
		}
		if d.HomeOwnerNode != "" {
			item["homeOwnerNode"] = d.HomeOwnerNode
		}
		if d.HomeProviderRef != "" {
			item["homeProviderRef"] = d.HomeProviderRef
		}
		if d.HomeSubnetRef != "" {
			item["homeSubnetRef"] = d.HomeSubnetRef
		}
		if d.HomeNICRef != "" {
			item["homeNICRef"] = d.HomeNICRef
		}
		if d.LocalNodeRef != "" {
			item["localNodeRef"] = d.LocalNodeRef
		}
		if d.LocalProviderRef != "" {
			item["localProviderRef"] = d.LocalProviderRef
		}
		if d.LocalSubnetRef != "" {
			item["localSubnetRef"] = d.LocalSubnetRef
		}
		if d.LocalNICRef != "" {
			item["localNICRef"] = d.LocalNICRef
		}
		if d.LocalResourceRef != "" {
			item["localResourceRef"] = d.LocalResourceRef
		}
		if d.LocalResourceType != "" {
			item["localResourceType"] = d.LocalResourceType
		}
		if d.LocalSource != "" {
			item["localSource"] = d.LocalSource
		}
		if d.LocalSourceType != "" {
			item["localSourceType"] = d.LocalSourceType
		}
		if d.CaptureState != "" && d.CaptureState != captureStateNone {
			item["captureState"] = d.CaptureState
		}
		if d.CaptureHolderNode != "" {
			item["captureHolderNode"] = d.CaptureHolderNode
		}
		if d.CaptureProviderRef != "" {
			item["captureProviderRef"] = d.CaptureProviderRef
		}
		if d.CaptureTargetRef != "" {
			item["captureTargetRef"] = d.CaptureTargetRef
		}
		if d.CaptureStrategy != "" {
			item["captureStrategy"] = d.CaptureStrategy
		}
		if d.AdvertiseOwnerNode != "" {
			item["advertiseOwnerNode"] = d.AdvertiseOwnerNode
		}
		if d.AdvertiseReason != "" {
			item["advertiseReason"] = d.AdvertiseReason
		}
		if d.SuppressionReason != "" {
			item["suppressionReason"] = d.SuppressionReason
		}
		if d.ConflictReason != "" {
			item["conflictReason"] = d.ConflictReason
			if len(d.ConflictOwners) > 0 {
				item["conflictOwners"] = providerInventoryOwnerFactStatusRows(d.ConflictOwners)
			}
			conflict := map[string]any{
				"address":        d.Address,
				"class":          d.Class,
				"conflictReason": d.ConflictReason,
				"homeOwnerNode":  d.HomeOwnerNode,
				"source":         d.Source,
			}
			if d.HomeProviderRef != "" {
				conflict["homeProviderRef"] = d.HomeProviderRef
			}
			if d.HomeSubnetRef != "" {
				conflict["homeSubnetRef"] = d.HomeSubnetRef
			}
			if d.HomeNICRef != "" {
				conflict["homeNICRef"] = d.HomeNICRef
			}
			if d.LocalNodeRef != "" {
				conflict["localNodeRef"] = d.LocalNodeRef
			}
			if d.LocalProviderRef != "" {
				conflict["localProviderRef"] = d.LocalProviderRef
			}
			if d.LocalSubnetRef != "" {
				conflict["localSubnetRef"] = d.LocalSubnetRef
			}
			if d.LocalNICRef != "" {
				conflict["localNICRef"] = d.LocalNICRef
			}
			if d.LocalResourceRef != "" {
				conflict["localResourceRef"] = d.LocalResourceRef
			}
			if d.LocalResourceType != "" {
				conflict["localResourceType"] = d.LocalResourceType
			}
			if d.LocalSource != "" {
				conflict["localSource"] = d.LocalSource
			}
			if d.LocalSourceType != "" {
				conflict["localSourceType"] = d.LocalSourceType
			}
			if len(d.ConflictOwners) > 0 {
				conflict["owners"] = providerInventoryOwnerFactStatusRows(d.ConflictOwners)
			}
			conflicts = append(conflicts, conflict)
		}
		switch ownershipResolverClaimState(d) {
		case "Stale":
			staleClaims = append(staleClaims, ownershipResolverDiagnosticRow(d, "stale"))
		case "Unknown":
			unknownClaims = append(unknownClaims, ownershipResolverDiagnosticRow(d, "unknown"))
		}
		if d.Fresh {
			item["fresh"] = true
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return fmt.Sprint(items[i]["address"]) < fmt.Sprint(items[j]["address"])
	})
	countMap := map[string]any{}
	for _, key := range mapStringKeysSorted(counts) {
		countMap[key] = counts[key]
	}
	sort.SliceStable(conflicts, func(i, j int) bool {
		return fmt.Sprint(conflicts[i]["address"]) < fmt.Sprint(conflicts[j]["address"])
	})
	sort.SliceStable(staleClaims, func(i, j int) bool {
		return fmt.Sprint(staleClaims[i]["address"]) < fmt.Sprint(staleClaims[j]["address"])
	})
	sort.SliceStable(unknownClaims, func(i, j int) bool {
		return fmt.Sprint(unknownClaims[i]["address"]) < fmt.Sprint(unknownClaims[j]["address"])
	})
	phase := "Resolved"
	reason := ""
	if len(conflicts) > 0 {
		phase = "Conflict"
		reason = "remote home owner overlaps local ownership evidence"
	}
	status := map[string]any{
		"ownershipResolverPhase":                  "Resolved",
		"ownershipResolverAddressCount":           len(decisions),
		"ownershipResolverClassCounts":            countMap,
		"ownershipResolverDecisions":              items,
		"ownershipResolverOwnerTable":             ownershipResolverOwnerTable(decisions),
		"ownershipResolverControlPlaneOwnerTable": ownershipResolverControlPlaneOwnerTable(decisions),
		"ownershipResolverFIBVerdicts":            ownershipResolverFIBVerdicts(decisions),
	}
	status["ownershipResolverPhase"] = phase
	status["ownershipResolverConflictCount"] = len(conflicts)
	status["ownershipResolverConflicts"] = conflicts
	status["ownershipResolverStaleCount"] = len(staleClaims)
	status["ownershipResolverStaleClaims"] = staleClaims
	status["ownershipResolverUnknownCount"] = len(unknownClaims)
	status["ownershipResolverUnknownClaims"] = unknownClaims
	status["ownershipResolverReason"] = reason
	return status
}

func ownershipResolverClaimState(d ownershipDecision) string {
	if d.ConflictReason != "" {
		return "Conflict"
	}
	if d.Class == ownershipClassUnknown {
		return "Unknown"
	}
	if d.Class == ownershipClassStaleCapture || d.CaptureState == captureStateStale {
		return "Stale"
	}
	return "OK"
}

func ownershipResolverDiagnosticRow(d ownershipDecision, reason string) map[string]any {
	row := map[string]any{
		"address": d.Address,
		"class":   d.Class,
		"state":   ownershipResolverClaimState(d),
		"source":  d.Source,
		"reason":  reason,
	}
	if d.HomeOwnerNode != "" {
		row["ownerNode"] = d.HomeOwnerNode
	}
	if d.CaptureState != "" && d.CaptureState != captureStateNone {
		row["captureState"] = d.CaptureState
	}
	if d.CaptureHolderNode != "" {
		row["captureHolderNode"] = d.CaptureHolderNode
	}
	if d.SuppressionReason != "" {
		row["suppressionReason"] = d.SuppressionReason
	}
	if d.ConflictReason != "" {
		row["conflictReason"] = d.ConflictReason
	}
	return row
}

func providerInventoryOwnerFactStatusRows(facts []providerInventoryOwnerFact) []map[string]any {
	rows := make([]map[string]any, 0, len(facts))
	for _, fact := range facts {
		row := map[string]any{
			"nodeRef": fact.NodeRef,
		}
		if fact.Provider != "" {
			row["provider"] = fact.Provider
		}
		if fact.ProviderRef != "" {
			row["providerRef"] = fact.ProviderRef
		}
		if fact.SubnetRef != "" {
			row["subnetRef"] = fact.SubnetRef
		}
		if fact.NICRef != "" {
			row["nicRef"] = fact.NICRef
		}
		if fact.ResourceRef != "" {
			row["resourceRef"] = fact.ResourceRef
		}
		if fact.ResourceType != "" {
			row["resourceType"] = fact.ResourceType
		}
		if !fact.ObservedAt.IsZero() {
			row["observedAt"] = fact.ObservedAt.UTC().Format(time.RFC3339Nano)
		}
		rows = append(rows, row)
	}
	return rows
}

func ownershipResolverFIBVerdicts(decisions []ownershipDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	for _, d := range decisions {
		action, reason := ownershipResolverFIBVerdict(d)
		row := map[string]any{
			"address": d.Address,
			"action":  action,
			"reason":  reason,
			"class":   d.Class,
		}
		if d.HomeOwnerNode != "" {
			row["ownerNode"] = d.HomeOwnerNode
		}
		if d.LocalNodeRef != "" {
			row["localNode"] = d.LocalNodeRef
		}
		if d.ConflictReason != "" {
			row["conflictReason"] = d.ConflictReason
		}
		if len(d.ConflictOwners) > 0 {
			row["conflictOwners"] = providerInventoryOwnerFactStatusRows(d.ConflictOwners)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i]["address"]) < fmt.Sprint(rows[j]["address"])
	})
	return rows
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

func ownershipResolverOwnerTable(decisions []ownershipDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	for _, d := range decisions {
		row := map[string]any{
			"address": d.Address,
			"class":   d.Class,
			"state":   ownershipResolverClaimState(d),
			"source":  d.Source,
		}
		if d.HomeOwnerNode != "" {
			row["ownerNode"] = d.HomeOwnerNode
		}
		if d.HomeProviderRef != "" {
			row["ownerProviderRef"] = d.HomeProviderRef
		}
		if d.HomeSubnetRef != "" {
			row["ownerSubnetRef"] = d.HomeSubnetRef
		}
		if d.HomeNICRef != "" {
			row["ownerNICRef"] = d.HomeNICRef
		}
		if d.LocalNodeRef != "" {
			row["localNode"] = d.LocalNodeRef
		}
		if d.LocalProviderRef != "" {
			row["localProviderRef"] = d.LocalProviderRef
		}
		if d.LocalSubnetRef != "" {
			row["localSubnetRef"] = d.LocalSubnetRef
		}
		if d.LocalNICRef != "" {
			row["localNICRef"] = d.LocalNICRef
		}
		if d.LocalResourceRef != "" {
			row["localResourceRef"] = d.LocalResourceRef
		}
		if d.LocalResourceType != "" {
			row["localResourceType"] = d.LocalResourceType
		}
		if d.LocalSource != "" {
			row["localSource"] = d.LocalSource
		}
		if d.LocalSourceType != "" {
			row["localSourceType"] = d.LocalSourceType
		}
		if d.AdvertiseOwnerNode != "" {
			row["advertiseOwnerNode"] = d.AdvertiseOwnerNode
		}
		if d.SuppressionReason != "" {
			row["suppressionReason"] = d.SuppressionReason
		}
		if d.ConflictReason != "" {
			row["conflictReason"] = d.ConflictReason
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i]["address"]) < fmt.Sprint(rows[j]["address"])
	})
	return rows
}

func ownershipResolverControlPlaneOwnerTable(decisions []ownershipDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	for _, d := range decisions {
		row := map[string]any{
			"address": d.Address,
			"state":   ownershipResolverClaimState(d),
			"class":   d.Class,
			"source":  d.Source,
		}
		putNonEmpty(row, "ownerNode", d.HomeOwnerNode)
		putNonEmpty(row, "ownerProviderRef", d.HomeProviderRef)
		putNonEmpty(row, "ownerSubnetRef", d.HomeSubnetRef)
		putNonEmpty(row, "ownerNICRef", d.HomeNICRef)
		putNonEmpty(row, "ownerResourceRef", d.HomeResourceRef)
		putNonEmpty(row, "ownerResourceType", d.HomeResourceType)
		putNonEmpty(row, "localEvidenceNode", d.LocalNodeRef)
		putNonEmpty(row, "localEvidenceProviderRef", d.LocalProviderRef)
		putNonEmpty(row, "localEvidenceSubnetRef", d.LocalSubnetRef)
		putNonEmpty(row, "localEvidenceNICRef", d.LocalNICRef)
		putNonEmpty(row, "localEvidenceResourceRef", d.LocalResourceRef)
		putNonEmpty(row, "localEvidenceResourceType", d.LocalResourceType)
		putNonEmpty(row, "localEvidenceSource", d.LocalSource)
		putNonEmpty(row, "localEvidenceSourceType", d.LocalSourceType)
		putNonEmpty(row, "captureHolderNode", d.CaptureHolderNode)
		putNonEmpty(row, "captureProviderRef", d.CaptureProviderRef)
		putNonEmpty(row, "captureTargetRef", d.CaptureTargetRef)
		putNonEmpty(row, "captureStrategy", d.CaptureStrategy)
		if d.CaptureState != "" && d.CaptureState != captureStateNone {
			row["captureState"] = d.CaptureState
		}
		putNonEmpty(row, "advertiseOwnerNode", d.AdvertiseOwnerNode)
		putNonEmpty(row, "advertiseReason", d.AdvertiseReason)
		putNonEmpty(row, "suppressionReason", d.SuppressionReason)
		putNonEmpty(row, "conflictReason", d.ConflictReason)
		if len(d.ConflictOwners) > 0 {
			row["conflictOwners"] = providerInventoryOwnerFactStatusRows(d.ConflictOwners)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left := fmt.Sprint(rows[i]["address"])
		right := fmt.Sprint(rows[j]["address"])
		if left == right {
			return fmt.Sprint(rows[i]["localEvidenceNode"]) < fmt.Sprint(rows[j]["localEvidenceNode"])
		}
		return left < right
	})
	return rows
}

func putNonEmpty(row map[string]any, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		row[key] = value
	}
}
