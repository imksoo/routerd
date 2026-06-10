// SPDX-License-Identifier: BSD-3-Clause

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
	PoolName          string
	SelfNode          string
	Spec              api.MobilityPoolSpec
	Events            []routerstate.EventRecord
	Status            map[string]any
	ActionJournal     []routerstate.ActionExecutionRecord
	PreviousPlans     []dynamicconfig.ActionPlan
	InstalledNextHops map[string][]string
	Now               time.Time
}

type ownershipDecision struct {
	Address            string
	Class              string
	HomeOwnerNode      string
	HomeProviderRef    string
	HomeSubnetRef      string
	HomeNICRef         string
	CaptureHolderNode  string
	CaptureProviderRef string
	CaptureTargetRef   string
	CaptureStrategy    string
	CaptureState       string
	AdvertiseOwnerNode string
	AdvertiseReason    string
	SuppressionReason  string
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
	remoteHomeFacts := providerInventoryHomeOwnerFacts(in.PoolName, in.Spec, in.Events, now)
	localInventory := localInventoryRecordsFromStatus(in.Status, prefix)
	removeSelfResourceLocalInventory(localInventory, statusString(in.Status["discoverySelfResourceRef"]))
	selfIPs, capturedIPs, selfIPsObserved := selfInventoryAddressSetsFromStatus(in.Status, prefix)
	captureObservedIPs := mergeBoolMaps(selfIPs, capturedIPs)
	eventOwned := resolverEventOwnedAddresses(in.PoolName, in.SelfNode, in.Spec, in.Events, in.Status, prefix, now)
	confirmedCaptures, staleCaptures := captureStatesForSelf(self, in.PreviousPlans, in.ActionJournal, captureObservedIPs, selfIPsObserved)
	handoverTargets := staticHandoverTargets(in.Spec, prefix)
	universe := map[string]bool{}
	for address := range staticOwners {
		universe[address] = true
	}
	for address := range remoteHomeFacts {
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
		if capture, ok := confirmedCaptures[address]; ok {
			decision.CaptureState = captureStateConfirmed
			decision.CaptureHolderNode = capture.HolderNode
			decision.CaptureProviderRef = capture.ProviderRef
			decision.CaptureTargetRef = capture.TargetRef
			decision.CaptureStrategy = capture.Strategy
		}
		if capture, ok := staleCaptures[address]; ok && decision.CaptureState == captureStateNone {
			decision.CaptureState = captureStateStale
			decision.CaptureHolderNode = capture.HolderNode
			decision.CaptureProviderRef = capture.ProviderRef
			decision.CaptureTargetRef = capture.TargetRef
			decision.CaptureStrategy = capture.Strategy
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
			if !hasRemoteFact || strings.TrimSpace(remoteFact.NodeRef) == "" || strings.TrimSpace(remoteFact.NodeRef) == self.NodeRef {
				if decision.CaptureState == captureStateConfirmed {
					decision.Class = ownershipClassConfirmedCapture
					decision.AdvertiseOwnerNode = self.NodeRef
					decision.AdvertiseReason = "confirmed-capture"
					decision.Source = "provider-action"
					decision.Fresh = true
					out = append(out, decision)
					continue
				}
				if decision.CaptureState == captureStateNone {
					decision.CaptureState = captureStateStale
					decision.CaptureHolderNode = self.NodeRef
					decision.CaptureProviderRef = strings.TrimSpace(self.Capture.ProviderRef)
					decision.CaptureTargetRef = providerCaptureRefFromCapture(self.Capture, self.CaptureTarget)
					decision.CaptureStrategy = effectiveCaptureStrategy("", self.Capture.Strategy)
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
			decision.Source = "local-inventory"
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if fact, ok := remoteHomeFacts[address]; ok && strings.TrimSpace(fact.NodeRef) == self.NodeRef && strings.TrimSpace(fact.ProviderRef) != "" {
			decision.HomeOwnerNode = self.NodeRef
			decision.HomeProviderRef = fact.ProviderRef
			decision.HomeSubnetRef = fact.SubnetRef
			decision.HomeNICRef = fact.NICRef
			decision.AdvertiseOwnerNode = self.NodeRef
			decision.Source = providerDiscoverySource
			decision.Fresh = true
			decision.Class = ownershipClassLocalHomeOwned
			decision.AdvertiseReason = "provider-home-owner"
			out = append(out, decision)
			continue
		}
		if eventOwner, ok := eventOwned[address]; ok && strings.TrimSpace(eventOwner.AdvertiseOwnerNode) == self.NodeRef {
			decision.Class = ownershipClassLocalHomeOwned
			decision.HomeOwnerNode = self.NodeRef
			decision.AdvertiseOwnerNode = self.NodeRef
			decision.AdvertiseReason = "ownership-event"
			decision.Source = eventOwner.SourceType
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if fact, ok := remoteHomeFacts[address]; ok && strings.TrimSpace(fact.NodeRef) != "" && strings.TrimSpace(fact.NodeRef) != self.NodeRef {
			decision.HomeOwnerNode = fact.NodeRef
			decision.HomeProviderRef = fact.ProviderRef
			decision.HomeSubnetRef = fact.SubnetRef
			decision.HomeNICRef = fact.NICRef
			decision.Source = providerDiscoverySource
			decision.Fresh = true
			homeProviderRef := strings.TrimSpace(fact.ProviderRef)
			selfProviderRef := strings.TrimSpace(self.Capture.ProviderRef)
			if decision.CaptureState == captureStateConfirmed && (homeProviderRef == "" || selfProviderRef == "" || homeProviderRef == selfProviderRef) {
				decision.Class = ownershipClassConfirmedCapture
				decision.AdvertiseOwnerNode = self.NodeRef
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
			out = append(out, decision)
			continue
		}
		if decision.CaptureState == captureStateConfirmed {
			decision.Class = ownershipClassConfirmedCapture
			decision.AdvertiseOwnerNode = self.NodeRef
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
			decision.Source = "self-inventory"
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if rec, ok := localInventory[address]; ok {
			decision.Class = ownershipClassLocalHomeOwned
			decision.HomeOwnerNode = self.NodeRef
			decision.HomeProviderRef = firstNonEmpty(rec.ProviderRef, self.OwnershipDiscovery.ProviderRef, self.Capture.ProviderRef)
			decision.HomeSubnetRef = rec.SubnetRef
			decision.HomeNICRef = rec.NICRef
			decision.AdvertiseOwnerNode = self.NodeRef
			decision.AdvertiseReason = "local-home-inventory"
			decision.Source = "local-inventory"
			decision.Fresh = true
			out = append(out, decision)
			continue
		}
		if decision.CaptureState == captureStateStale {
			decision.Class = ownershipClassStaleCapture
			decision.SuppressionReason = "capture-not-desired"
			decision.Source = "provider-action"
			out = append(out, decision)
			continue
		}
		if decision.Source == "" {
			decision.Source = "bgp-rib"
		}
		out = append(out, decision)
	}
	return out, nil
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

func mergeBoolMaps(values ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		for key, ok := range value {
			if ok {
				out[key] = true
			}
		}
	}
	return out
}

type resolverEventOwnedAddress struct {
	AdvertiseOwnerNode string
	SourceType         string
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

type resolverCaptureState struct {
	HolderNode  string
	ProviderRef string
	TargetRef   string
	Strategy    string
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
			Strategy:    effectiveCaptureStrategy(tr.plan.Provider, firstNonEmpty(tr.plan.Target["captureStrategy"], self.Capture.Strategy)),
		}
		if tr.assign && tr.succeeded && (selfIPs[address] || !selfIPsObserved) {
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
		stale[address] = resolverCaptureState{
			HolderNode:  firstNonEmpty(plan.Parameters[captureParamHolder], self.NodeRef),
			ProviderRef: providerRef,
			TargetRef:   targetRef,
			Strategy:    effectiveCaptureStrategy(plan.Provider, firstNonEmpty(plan.Target["captureStrategy"], self.Capture.Strategy)),
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
	return map[string]any{
		"ownershipResolverPhase":        "Resolved",
		"ownershipResolverAddressCount": len(decisions),
		"ownershipResolverClassCounts":  countMap,
		"ownershipResolverDecisions":    items,
	}
}
