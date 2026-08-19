// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

// mobilityPoolStatusInput holds the completed typed plan, its snapshot, and
// effect facts needed only for status projection. It is constructed after
// effect gating and never feeds status data back into the planning core.
type mobilityPoolStatusInput struct {
	Runtime             PoolRuntimeSnapshot
	Plan                PoolPlan
	Resolved            mobilityMembersResolution
	ActionPlans         []dynamicconfig.ActionPlan
	OnPremPending       bool
	OnPremPendingReason string
	Transitions         bgpCaptureTransitionState
}

// MobilityPoolStatus is the typed, in-memory status output for a PoolPlan.
// It retains every status calculation as typed data until Serialize is called
// at the object-store boundary. Phase transitions below deliberately use
// fields rather than inspecting a partially built status map.
type MobilityPoolStatus struct {
	input mobilityPoolStatusInput

	Phase  string
	Reason string

	ProviderAction      providerActionStatusProjection
	ProviderObservation providerObservationStatus
}

type providerActionStatusProjection struct {
	Phase           string
	Error           string
	PendingCount    int
	FailedAddresses []string
}

func providerActionFailureStatus(failed []providerActionPlanFailure) ([]string, string) {
	addresses := make([]string, 0, len(failed))
	var lastError string
	var lastFailedAt time.Time
	for _, failure := range failed {
		if failure.Address != "" {
			addresses = append(addresses, failure.Address)
		}
		if failure.FailedAt.After(lastFailedAt) {
			lastFailedAt = failure.FailedAt
			lastError = failure.Error
		}
	}
	sort.Strings(addresses)
	if len(addresses) == 0 {
		addresses = nil
	}
	return addresses, lastError
}

// writeBGPSeizeHoldDownStatus serializes the planner-owned durable placement
// continuation. Discovery may observe placement but never writes this state.
func writeBGPSeizeHoldDownStatus(status map[string]any, placement PlacementDecision) {
	key := strings.TrimSpace(placement.SeizeHoldDownKey)
	status["bgpSeizeHoldDownActive"] = placement.SeizeHoldDown && key != ""
	if key == "" {
		status["bgpSeizeHoldDownKey"] = ""
		status["bgpSeizeHoldDownSince"] = ""
		status["bgpSeizeHoldDownUntil"] = ""
		return
	}
	status["bgpSeizeHoldDownKey"] = placement.SeizeHoldDownKey
	status["bgpSeizeHoldDownSince"] = placement.SeizeHoldDownSince.Format(time.RFC3339Nano)
	status["bgpSeizeHoldDownUntil"] = placement.SeizeHoldDownUntil.Format(time.RFC3339Nano)
}

func buildMobilityPoolStatus(input mobilityPoolStatusInput) MobilityPoolStatus {
	runtime := input.Runtime
	providerAction, providerObservation := projectProviderPlanStatus(
		input.ActionPlans,
		runtime.Provider.ActionHistory,
		runtime.Ownership.SelfCapturedIPs,
		runtime.Ownership.SelfInventoryKnown,
		runtime.Ownership.DiscoveryLastScanAt,
		runtime.Ownership.ForwardingKnown,
		runtime.Ownership.ForwardingOn,
		runtime.Ownership.DiscoveryLastScanAt,
	)
	projection := MobilityPoolStatus{
		input:               input,
		ProviderAction:      providerAction,
		ProviderObservation: providerObservation,
	}
	projection.applyPhaseTransitions(input)
	return projection
}

func (s *MobilityPoolStatus) applyPhaseTransitions(input mobilityPoolStatusInput) {
	runtime := input.Runtime
	plan := input.Plan
	plannedPhase := "Ready"
	plannedReason := ""
	if plan.Placement.SeizeHoldDown && strings.TrimSpace(plan.Placement.SeizeHoldDownKey) != "" {
		plannedPhase = "Pending"
		plannedReason = "BGP capture seize hold-down is active"
	}
	if captureNeedsResolution(plan.Addresses) && runtime.Provider.CaptureResolutionError != "" {
		plannedPhase = "Degraded"
		plannedReason = runtime.Provider.CaptureResolutionError
		s.ProviderAction.Phase = "Blocked"
	}
	if s.ProviderAction.PendingCount > 0 && s.ProviderAction.Phase == "OK" {
		plannedPhase = "Pending"
		plannedReason = "provider actions are pending"
		s.ProviderAction.Phase = "Pending"
	}
	if s.ProviderObservation.PendingCount > 0 && s.ProviderAction.Phase == "OK" {
		plannedPhase = "Pending"
		plannedReason = "provider observations are pending"
	}
	if reason := ownershipResolverConflictReason(plan.Addresses); reason != "" {
		plannedPhase = "Degraded"
		plannedReason = reason
		if s.ProviderAction.Phase == "OK" {
			s.ProviderAction.Phase = "Blocked"
		}
	}
	if input.OnPremPending {
		plannedPhase = "Pending"
		plannedReason = input.OnPremPendingReason
	}

	switch s.ProviderAction.Phase {
	case "Failed":
		s.Phase = "Failed"
		s.Reason = firstNonEmpty(s.ProviderAction.Error, plannedReason, "provider action failed")
		return
	case "Blocked":
		s.Phase = "Degraded"
		s.Reason = firstNonEmpty(plannedReason, "provider actions are blocked")
		return
	case "Pending":
		s.Phase = "Pending"
		s.Reason = firstNonEmpty(plannedReason, "provider actions are pending")
		return
	}
	if s.ProviderObservation.PendingCount > 0 {
		s.Phase = "Pending"
		s.Reason = firstNonEmpty(plannedReason, "provider observations are pending")
		return
	}
	s.Phase = plannedPhase
	s.Reason = plannedReason
}

// Serialize is the sole conversion to the object-status map. Consumers of the
// planner receive typed PoolPlan output instead of these display values.
func (s MobilityPoolStatus) Serialize() map[string]any {
	input := s.input
	runtime := input.Runtime
	plan := input.Plan
	status := map[string]any{
		"phase":                               s.Phase,
		"prefix":                              runtime.Pool.Prefix.String(),
		"groupRef":                            runtime.Pool.Spec.GroupRef,
		"generatedBGPPaths":                   len(plan.BGPPaths),
		"observedBGPReturnRoutes":             mapsort.Keys(runtime.BGP.ReturnRoutes),
		"bgpRIBObserved":                      runtime.BGP.RIBObserved(),
		"membersFrom":                         statusRowMaps(input.Resolved.MembersFrom),
		"resolvedMemberCount":                 input.Resolved.ResolvedMemberCount,
		"pendingSources":                      input.Resolved.PendingSources,
		"placementGroup":                      plan.Placement.Group,
		"placementActive":                     plan.Placement.Active,
		"placementActiveNode":                 plan.Placement.ActiveNode,
		"providerActionPhase":                 s.ProviderAction.Phase,
		"providerActionError":                 s.ProviderAction.Error,
		"providerActionFailedAddresses":       s.ProviderAction.FailedAddresses,
		"providerObservationPendingAddresses": s.ProviderObservation.PendingAddresses,
		"observedSelfStaleCaptures":           observedSelfStaleCaptureTimes(plan.Addresses, runtime.Pool.SelfNode, runtime.Previous.ObservedStaleSince, runtime.Now),
	}
	// Planner and discovery status updates are partial merges. Write an empty
	// reason as well so a recovered Ready state cannot retain a prior failure or
	// pending explanation.
	status["reason"] = s.Reason
	writeBGPSeizeHoldDownStatus(status, plan.Placement)
	writeBGPCaptureTransitionState(status, input.Transitions)
	status["ownershipResolverControlPlaneOwnerTable"] = ownershipResolverControlPlaneOwnerTable(plan.Addresses)
	return status
}
