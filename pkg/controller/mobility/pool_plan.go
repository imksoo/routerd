// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/controller/mobilityfib"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// NormalizedMobilityPool is the once-normalized identity/topology input to the
// functional core; observations and durable state live in PoolRuntimeSnapshot.
type NormalizedMobilityPool struct {
	Name   string
	Source string
	Spec   api.MobilityPoolSpec
	// Prefix is the masked pool prefix decoded once at the normalization
	// boundary.  The functional core must not reparse Spec.Prefix.
	Prefix               netip.Prefix
	SelfNode             string
	Self                 memberPlanInfo
	Members              map[string]memberPlanInfo
	SelfCaptureInterface string
}

// ProviderSnapshot is the provider observation bundle; provider mutation stays separate.
type ProviderSnapshot struct {
	Profiles            map[string]api.CloudProviderProfileSpec
	ActionHistory       ProviderActionHistory
	SuppressDeprovision bool
	// CaptureResolutionError is the typed precondition emitted by snapshot
	// collection when a discovery-derived provider capture target is not yet
	// known. An empty value means capture actions may be planned normally.
	CaptureResolutionError string
}

// PoolRuntimeSnapshot is the complete typed input to the pure planner.
type PoolRuntimeSnapshot struct {
	Pool      NormalizedMobilityPool
	Events    []routerstate.EventRecord
	Ownership OwnershipFacts
	// BGP is decoded once by the controller shell.
	BGP                   BGPSnapshot
	PlacementObservations PlacementObservations
	Previous              PreviousPoolState
	Provider              ProviderSnapshot
	LivenessMarkerPrefix  string
	TunnelInterfaces      []string
	CaptureGate           *sam.CaptureGateStatus
	Now                   time.Time
}

// PreviousPoolState is only the durable continuation state needed after restart.
type PreviousPoolState struct {
	Placement          placementPreviousState
	ObservedStaleSince map[string]time.Time
	OnPremDiscovery    onPremDiscoveryState
	Transitions        bgpCaptureTransitionState
}

// poolDiscoveryObservation is the typed discovery fact carried by the snapshot.
type poolDiscoveryObservation struct {
	// providerRuntime preserves leases needed for missing-hold continuation.
	providerRuntime     []providerDiscoveryRuntimeRecord
	SelfPrivateIPs      map[string]bool
	SelfCapturedIPs     map[string]bool
	SelfInventoryKnown  bool
	SelfPrimaryKnown    bool
	DiscoveryLastScanAt time.Time
	ForwardingKnown     bool
	ForwardingOn        bool
	SelfNICRef          string
	SelfSubnetRef       string
	Placement           discoveryPlacementObservation
}

// OwnershipFacts shares the one decoded provider observation across planning.
type OwnershipFacts = poolDiscoveryObservation

// discoveryPlacementObservation is the durable fact used to schedule the next scan.
type discoveryPlacementObservation struct {
	LivenessObserved    bool
	ActiveNode          string
	Active              bool
	Seize               bool
	SelfMarkerPresent   bool
	ActiveMarkerPresent bool
	SelfMarker          string
	ActiveMarker        string
}

// onPremDiscoveryState is the typed durable on-prem capture gate.
type onPremDiscoveryState struct {
	Phase       string
	ResultCount int64
	FreshUntil  time.Time
	ArmedAt     time.Time
}

// bgpCaptureTransitionState keeps idempotent transition markers typed.
type bgpCaptureTransitionState struct {
	SeizeComplete    map[string]string
	CaptureConfirmed map[string]string
}

// placementPreviousState is the minimal durable placement continuation.
type placementPreviousState struct {
	SeizeHoldDownActive bool
	SeizeHoldDownKey    string
	SeizeHoldDownSince  time.Time
	SeizeHoldDownUntil  time.Time
}

func decodePlacementPreviousState(status map[string]any) placementPreviousState {
	seizeHoldDownActive, _ := statusvalue.StrictBool(status["bgpSeizeHoldDownActive"])
	since, _ := statusTimeValue(status["bgpSeizeHoldDownSince"])
	until, _ := statusTimeValue(status["bgpSeizeHoldDownUntil"])
	return placementPreviousState{
		SeizeHoldDownActive: seizeHoldDownActive,
		SeizeHoldDownKey:    statusvalue.Text(status["bgpSeizeHoldDownKey"]),
		SeizeHoldDownSince:  since,
		SeizeHoldDownUntil:  until,
	}
}

func decodePoolPreviousState(status map[string]any) PreviousPoolState {
	placement := decodePlacementPreviousState(status)
	return PreviousPoolState{
		Placement:          placement,
		ObservedStaleSince: observedSelfStaleCaptureSinceFromStatus(status),
		Transitions:        decodeBGPCaptureTransitionState(status),
	}
}

// PoolPlan is the single typed result consumed by effectors.
type PoolPlan struct {
	Placement       PlacementDecision
	Addresses       []AddressDecision
	BGPPaths        []bgpdaemon.AppliedPath
	ProviderActions []dynamicconfig.ActionPlan
	LocalDataplane  dynamicconfig.MobilityDataplanePlan
	FIBVerdicts     []dynamicconfig.FIBVerdict
}

func ReconcilePool(snapshot PoolRuntimeSnapshot) (PoolPlan, error) {
	if snapshot.Now.IsZero() {
		return PoolPlan{}, fmt.Errorf("pool runtime snapshot time is required")
	}
	pool := snapshot.Pool
	if !pool.Prefix.IsValid() {
		return PoolPlan{}, fmt.Errorf("MobilityPool/%s normalized prefix is required", pool.Name)
	}
	placement, _ := EvaluatePlacementFromObservations(pool.Self, pool.Members, snapshot.PlacementObservations, snapshot.Now)
	decisions, err := resolveAddressOwnership(snapshot)
	if err != nil {
		return PoolPlan{}, err
	}
	plan, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolRuntimeSnapshot: snapshot,
		Decisions:           decisions,
		Placement:           placement,
	})
	if err != nil {
		return PoolPlan{}, err
	}
	if captureNeedsResolution(decisions) && snapshot.Provider.CaptureResolutionError != "" {
		// A missing discovered provider target is a fact in the runtime snapshot,
		// not an effect-shell decision. Keep the BGP/local plan visible but do
		// not produce a provider mutation until the current snapshot resolves it.
		plan.ProviderActions = nil
	}
	if !pool.Self.MaintenanceDrain {
		plan.BGPPaths = append(plan.BGPPaths, planBGPReturnRoutePaths(pool.Source, pool.Self, snapshot.Ownership.SelfPrivateIPs, snapshot.Ownership.SelfCapturedIPs, snapshot.Ownership.SelfPrimaryKnown)...)
		if marker, ok := planBGPLivenessMarkerPath(pool.Source, pool.Self.NodeRef, snapshot.LivenessMarkerPrefix); ok {
			plan.BGPPaths = append(plan.BGPPaths, marker)
		}
	}
	fibVerdicts, localInventoryRoutes := planFIB(pool, decisions)
	plan.Placement = placement
	plan.Addresses = decisions
	plan.LocalDataplane.Routes = append(plan.LocalDataplane.Routes, localInventoryRoutes...)
	plan.FIBVerdicts = fibVerdicts
	return plan, nil
}

// planFIB projects each address decision to FIB and typed local inventory effects.
func planFIB(pool NormalizedMobilityPool, decisions []ownershipDecision) ([]dynamicconfig.FIBVerdict, []dynamicconfig.MobilityIPv4RouteIntent) {
	scope := fibPoolScope(pool)
	verdicts := []dynamicconfig.FIBVerdict{{PoolRef: pool.Name, Scope: scope}}
	device := ""
	if strings.TrimSpace(pool.Self.Capture.Type) == "provider-secondary-ip" {
		device = strings.TrimSpace(pool.SelfCaptureInterface)
	}
	routes := make([]dynamicconfig.MobilityIPv4RouteIntent, 0, len(decisions))
	preferredSource, multiplePreferredSources := "", false
	for _, decision := range decisions {
		action, reason := ownershipResolverFIBVerdict(decision)
		verdicts = append(verdicts, dynamicconfig.FIBVerdict{PoolRef: pool.Name, Address: decision.Address, Action: action, Class: decision.Class, OwnerNode: decision.HomeOwnerNode, Reason: reason})
		if action == mobilityfib.ActionLocalRoute && device != "" {
			routes = append(routes, localInventoryRouteIntent(pool.Name, decision.Address, device))
		}
		if action == mobilityfib.ActionLocalRoute && decision.Class == ownershipClassStaticOwned && strings.TrimSpace(decision.Address) != "" {
			if preferredSource != "" && preferredSource != decision.Address {
				multiplePreferredSources = true
			}
			preferredSource = decision.Address
		}
	}
	if !multiplePreferredSources {
		scope.PreferredSource = preferredSource
	}
	return verdicts, routes
}

func localInventoryRouteIntent(poolName, address, device string) dynamicconfig.MobilityIPv4RouteIntent {
	return dynamicconfig.MobilityIPv4RouteIntent{
		ID:          "mobility-" + safeName(poolName) + "-local-" + safeName(strings.TrimSuffix(address, "/32")),
		PoolRef:     poolName,
		Purpose:     dynamicconfig.MobilityIPv4RoutePurposeLocalInventory,
		Destination: address,
		Device:      device,
		Metric:      1,
	}
}

func fibPoolScope(pool NormalizedMobilityPool) *dynamicconfig.FIBPoolScope {
	return &dynamicconfig.FIBPoolScope{
		Prefix:                  pool.Prefix.String(),
		RemoteReturnCommunities: remoteReturnRouteCommunities(pool.Self, pool.Members),
	}
}

// remoteReturnRouteCommunities is an explicit plan selector for return paths.
// The FIB effector admits only a path carrying one of these identities; it
// must not infer remoteness from the absence of a local identity.
func remoteReturnRouteCommunities(self memberPlanInfo, members map[string]memberPlanInfo) []string {
	seen := map[string]bool{}
	for _, member := range members {
		if sameReturnRouteSite(self, member) {
			continue
		}
		community := bgpstate.MobilityNodeIdentityCommunity(strings.TrimSpace(member.NodeRef))
		if community != "" {
			seen[community] = true
		}
	}
	out := make([]string, 0, len(seen))
	for community := range seen {
		out = append(out, community)
	}
	sort.Strings(out)
	return out
}

func sameReturnRouteSite(a, b memberPlanInfo) bool {
	aNode := strings.TrimSpace(a.NodeRef)
	bNode := strings.TrimSpace(b.NodeRef)
	if aNode == "" || bNode == "" {
		return false
	}
	if aNode == bNode {
		return true
	}
	aSite := strings.TrimSpace(a.Site)
	bSite := strings.TrimSpace(b.Site)
	return aSite != "" && aSite == bSite
}
