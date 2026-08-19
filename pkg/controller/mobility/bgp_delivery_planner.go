// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/sam"
)

type bgpDeliveryPlannerInput struct {
	PoolRuntimeSnapshot
	Decisions []ownershipDecision
	Placement PlacementDecision
}

func planBGPMobilityDelivery(in bgpDeliveryPlannerInput) (PoolPlan, error) {
	poolPrefix := in.Pool.Prefix
	if !poolPrefix.IsValid() {
		return PoolPlan{}, fmt.Errorf("MobilityPool/%s normalized prefix is required", in.Pool.Name)
	}
	now := in.Now.UTC()
	if now.IsZero() {
		return PoolPlan{}, fmt.Errorf("BGP mobility delivery planner time is required")
	}
	// All downstream pure planning steps receive the same snapshot time. This
	// keeps capture-release timing and provider assignment fences from taking
	// independent wall-clock samples in one plan.
	in.Now = now
	captureNextHops := in.BGP.CaptureNextHops
	if len(captureNextHops) == 0 {
		captureNextHops = in.BGP.InstalledNextHops
	}
	// Finalize each address exactly once before projecting it to BGP provider
	// effects and the local dataplane. In particular, neither projection is
	// allowed to reinterpret the RIB, provider observation, or placement.
	finalizeCaptureDispositions(&in, captureNextHops, poolPrefix, now)
	paths := planBGPAdvertisements(in.Pool.Source, in.Pool.Self, in.Decisions, in.Placement, in.CaptureGate)
	actionPlans, err := planCaptureActionPlans(in)
	if err != nil {
		return PoolPlan{}, err
	}
	captures := planLocalCaptureIntents(in)
	routes, staticAddresses := planCapturePrefixEffects(in, poolPrefix, captures)
	return PoolPlan{
		BGPPaths:        paths,
		ProviderActions: actionPlans,
		LocalDataplane: dynamicconfig.MobilityDataplanePlan{
			PoolPrefix:      poolPrefix.String(),
			Captures:        captures,
			Routes:          routes,
			StaticAddresses: staticAddresses,
		},
	}, nil
}

// planLocalCaptureIntents turns the already-observed BGP/provider result into
// a direct local dataplane plan. It is intentionally adjacent to the BGP and
// provider plan, rather than being reconstructed later from status.
func planLocalCaptureIntents(in bgpDeliveryPlannerInput) []dynamicconfig.LocalCaptureIntent {
	captureGateClosed := in.CaptureGate != nil && !in.CaptureGate.Active
	captureInterface := strings.TrimSpace(in.Pool.SelfCaptureInterface)
	out := make([]dynamicconfig.LocalCaptureIntent, 0, len(in.Decisions))
	captureType := strings.TrimSpace(in.Pool.Self.Capture.Type)
	for _, decision := range in.Decisions {
		disposition := decision.CaptureDisposition
		if disposition == "" || disposition == dynamicconfig.CaptureProhibited {
			continue
		}
		// Capture gates prevent acquisition, not safe teardown. A release must
		// still reach the dataplane when a CARP/VRRP role changes.
		if captureGateClosed && disposition != dynamicconfig.CaptureRelease {
			continue
		}
		switch captureType {
		case "proxy-arp":
			if disposition != dynamicconfig.CaptureDesired && disposition != dynamicconfig.CaptureProtectExisting && disposition != dynamicconfig.CaptureRelease {
				continue
			}
		case "provider-secondary-ip":
			// A secondary IP is locally captured only after its provider-side
			// assignment is observed. CaptureDesired is therefore deliberately
			// projected to the provider effector only; ProtectExisting and
			// Release are the complete local desired state.
			if disposition != dynamicconfig.CaptureProtectExisting && disposition != dynamicconfig.CaptureRelease && disposition != dynamicconfig.CaptureHold {
				continue
			}
		default:
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(decision.Address))
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			continue
		}
		address := prefix.Masked().String()
		out = append(out, dynamicconfig.LocalCaptureIntent{
			ID:               "mobility-" + safeName(in.Pool.Name) + "-" + safeName(strings.TrimSuffix(address, "/32")),
			PoolRef:          in.Pool.Name,
			Address:          address,
			Disposition:      disposition,
			CaptureType:      captureType,
			CaptureInterface: captureInterface,
			TunnelInterfaces: append([]string(nil), in.TunnelInterfaces...),
			GratuitousARP:    in.Pool.Self.Capture.GratuitousARP,
			Reason:           decision.CaptureReason,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// planCapturePrefixEffects emits the non-neighbor local effects implied by a
// proxy-ARP capture. The capture disposition has already been decided; this
// function only lowers desired/protect-existing captures into their typed
// source-address and prefix-route effects.
func planCapturePrefixEffects(in bgpDeliveryPlannerInput, pool netip.Prefix, captures []dynamicconfig.LocalCaptureIntent) ([]dynamicconfig.MobilityIPv4RouteIntent, []dynamicconfig.MobilityIPv4AddressIntent) {
	activeCapture := false
	for _, capture := range captures {
		if strings.TrimSpace(capture.CaptureType) != "proxy-arp" {
			continue
		}
		if capture.Disposition == dynamicconfig.CaptureDesired || capture.Disposition == dynamicconfig.CaptureProtectExisting {
			activeCapture = true
			break
		}
	}
	if !activeCapture {
		return nil, nil
	}

	if !pool.IsValid() || !pool.Addr().Is4() {
		return nil, nil
	}
	device := strings.TrimSpace(in.Pool.SelfCaptureInterface)
	if device == "" {
		return nil, nil
	}

	preferredSource := ""
	var staticAddresses []dynamicconfig.MobilityIPv4AddressIntent
	if source, ok := captureSourcePrefix(in.Pool.Self.CaptureSourceAddress, pool); ok {
		preferredSource = source.Addr().String()
		if strings.TrimSpace(in.Pool.Self.CaptureSourceAddressFrom.Resource) == "" {
			staticAddresses = append(staticAddresses, dynamicconfig.MobilityIPv4AddressIntent{
				ID:        "mobility-" + safeName(in.Pool.Name) + "-capture-source",
				PoolRef:   in.Pool.Name,
				Purpose:   dynamicconfig.MobilityIPv4AddressPurposeCaptureSource,
				Interface: device,
				Address:   source.String(),
			})
		}
	}

	prefixes := sam.IPv4PrefixesExcluding(pool, in.Pool.Self.Capture.ExcludeAddresses)
	routes := make([]dynamicconfig.MobilityIPv4RouteIntent, 0, len(prefixes))
	for _, prefix := range prefixes {
		id := "mobility-" + safeName(in.Pool.Name) + "-capture-prefix"
		if len(prefixes) != 1 {
			id = "mobility-" + safeName(in.Pool.Name) + "-capture-" + safeName(prefix.String())
		}
		routes = append(routes, dynamicconfig.MobilityIPv4RouteIntent{
			ID:              id,
			PoolRef:         in.Pool.Name,
			Purpose:         dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix,
			Destination:     prefix.String(),
			Device:          device,
			PreferredSource: preferredSource,
			Metric:          90,
		})
	}
	return routes, staticAddresses
}

func captureSourcePrefix(raw string, pool netip.Prefix) (netip.Prefix, bool) {
	prefix, ok := parseIPv4AddressOrPrefix(raw)
	if !ok || !pool.Addr().Is4() || !pool.Masked().Contains(prefix.Addr()) {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(prefix.Addr(), 32), true
}

func planBGPAdvertisements(source string, self memberPlanInfo, decisions []ownershipDecision, placement PlacementDecision, captureGate *sam.CaptureGateStatus) []bgpdaemon.AppliedPath {
	var out []bgpdaemon.AppliedPath
	for _, decision := range decisions {
		if captureGate != nil && !captureGate.Active {
			// A CARP BACKUP must withdraw its local host path. Keeping it with a
			// lower LocalPref still leaves an ECMP path toward a silent capture.
			continue
		}
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
			if providerHomeNeedsSeize(decision, self, members) && !placement.Seize {
				return false
			}
			return strings.TrimSpace(decision.HomeOwnerNode) != "" &&
				strings.TrimSpace(decision.HomeOwnerNode) != strings.TrimSpace(self.NodeRef)
		default:
			return true
		}
	case ownershipClassRemoteHomeOwned:
		if strings.TrimSpace(decision.AdvertiseOwnerNode) == strings.TrimSpace(self.NodeRef) {
			return false
		}
		if providerHomeNeedsSeize(decision, self, members) && !placement.Seize {
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

// providerHomeNeedsSeize is the one same-site provider-home test shared by
// direct capture eligibility, distributed capture selection, and initial
// acquisition suppression. A same-site fresh provider home must not be
// claimed until placement has explicitly entered seize; other ownership
// sources deliberately remain eligible under their own rules.
func providerHomeNeedsSeize(decision ownershipDecision, self memberPlanInfo, members map[string]memberPlanInfo) bool {
	if strings.TrimSpace(decision.Source) != providerDiscoverySource {
		return false
	}
	if decision.Class != ownershipClassRemoteHomeOwned &&
		(decision.Class != ownershipClassStaleCapture || strings.TrimSpace(decision.SuppressionReason) != "fresh-home-owner") {
		return false
	}
	owner, ok := lookupMemberByNodeRef(members, decision.HomeOwnerNode)
	return ok && samePlacementSite(self, owner)
}

func routeTableCaptureAllowed(decision ownershipDecision, self memberPlanInfo) bool {
	if providerCaptureStrategy(self.Capture) != captureStrategyRouteTable {
		return true
	}
	if decision.Class == ownershipClassLocalRouterSelf || decision.Class == ownershipClassLocalHomeOwned {
		return false
	}
	if strings.TrimSpace(decision.AdvertiseOwnerNode) == strings.TrimSpace(self.NodeRef) {
		return false
	}
	nextHop := normalizeAddressString(strings.TrimSpace(self.Capture.Target["nextHopIPAddress"]))
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

func decisionHasProviderCaptureState(decision ownershipDecision) bool {
	state := strings.TrimSpace(decision.CaptureState)
	return state != "" && state != captureStateNone
}

func planCaptureActionPlans(in bgpDeliveryPlannerInput) ([]dynamicconfig.ActionPlan, error) {
	if in.Pool.Self.Capture.Type != "provider-secondary-ip" {
		return nil, nil
	}
	plans, err := bgpProviderActionPlans(in.Pool.Name, in.Pool.Self, in.Decisions, in.Provider.ActionHistory, in.Provider.Profiles, in.Ownership.ForwardingKnown, in.Ownership.ForwardingOn, in.Ownership.DiscoveryLastScanAt, in.Provider.SuppressDeprovision, in.Now)
	if err != nil {
		return nil, err
	}
	stampBGPAssignmentFenceActionPlans(plans, in.Pool, decisionsByAddress(in.Decisions), in.Placement, in.Provider.ActionHistory, in.Now)
	stampBGPProviderTransitionFences(plans, in.Pool.Self, "", in.Provider.ActionHistory, in.Ownership.SelfCapturedIPs, in.Ownership.SelfInventoryKnown, in.Ownership.DiscoveryLastScanAt)
	return plans, nil
}

// suppressInitialSameSiteSecondaryIPCapture retains the safety boundary from
// the former candidate planner: a fresh home address on the same provider is
// not a normal secondary-IP acquisition. A real placement seize may acquire
// it only when there is no prior local capture plan to preserve. That narrow
// exception lets a peer take over after a drained holder withdraws its marker,
// without reviving a stale same-site plan while its provider transition is
// still unresolved.
func suppressInitialSameSiteSecondaryIPCapture(decision ownershipDecision, self memberPlanInfo, members map[string]memberPlanInfo, seize, hasPrevious bool) bool {
	if decision.CaptureDisposition == dynamicconfig.CaptureProtectExisting || decisionHasProviderCaptureState(decision) || decision.CaptureSucceeded {
		return false
	}

	sameProviderHome := false
	switch decision.Class {
	case ownershipClassRemoteHomeOwned:
		sameProviderHome = strings.TrimSpace(decision.HomeProviderRef) != "" &&
			strings.TrimSpace(decision.HomeProviderRef) == strings.TrimSpace(self.Capture.ProviderRef)
		if sameProviderHome {
			return !seize || hasPrevious
		}
		if seize || strings.TrimSpace(decision.Source) != providerDiscoverySource {
			return false
		}
	case ownershipClassStaleCapture:
		if strings.TrimSpace(decision.SuppressionReason) != "fresh-home-owner" ||
			strings.TrimSpace(decision.Source) != providerDiscoverySource {
			return false
		}
		sameProviderHome = decisionHomeProviderRefMatches(decision, self.Capture.ProviderRef)
		if sameProviderHome {
			return true
		}
	default:
		return false
	}

	return providerHomeNeedsSeize(decision, self, members)
}

func bgpPathSigFromObservedSelfStale(address string, staleSince time.Time) string {
	normalized := normalizeAddressString(address)
	if prefix, ok := parseIPv4AddressOrPrefix(normalized); ok {
		normalized = prefix.Masked().String()
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
