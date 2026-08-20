// SPDX-License-Identifier: BSD-3-Clause
// Package mobility derives BGP /32 mobility paths and provider trap action
// plans from MobilityPool intent and federation observed facts.
package mobility

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

const (
	mobilityHolderTransitionTopic      = "routerd.mobility.holder.transition"
	bgpCaptureTransitionCompletedKey   = "bgpCaptureTransitionCompleted"
	bgpCaptureTransitionCompletedField = "seizeComplete"
	bgpCaptureConfirmedField           = "captureConfirmed"
)

// captureTransitionEffects are the already-applied provider-action projection
// of one PoolPlan. They stay separate from the plan because a closed capture
// gate can suppress publication without changing the pure decision.
type captureTransitionEffects struct {
	Previous    map[string]bgpCaptureAssignment
	Current     map[string]bgpCaptureAssignment
	ActionPlans []dynamicconfig.ActionPlan
}

// recordBGPCaptureAssignmentTransitions persists externally observable holder
// transitions after the pool plan has been applied. It receives the same typed
// reconcile state and plan as the other effectors, so it neither reconstructs
// facts from status nor receives a parallel list of planning inputs.
func (c Controller) recordBGPCaptureAssignmentTransitions(ctx context.Context, state poolReconcileState, plan PoolPlan, effects captureTransitionEffects) (bgpCaptureTransitionState, error) {
	runtime := state.Runtime
	previous := effects.Previous
	current := effects.Current
	plans := effects.ActionPlans
	placement := plan.Placement
	now := runtime.Now.UTC()
	if now.IsZero() {
		return bgpCaptureTransitionState{}, fmt.Errorf("MobilityPool/%s transition snapshot time is required", runtime.Pool.Name)
	}
	recorder, _ := c.Store.(mobilityEventRecorder)
	pathSigs := bgpPathSigsByAddress(plans)
	transitions := cloneBGPCaptureTransitionState(runtime.Previous.Transitions)
	seizeComplete := transitions.SeizeComplete
	captureConfirmed := transitions.CaptureConfirmed
	self := runtime.Pool.Self
	selfNode := strings.TrimSpace(self.NodeRef)
	for _, address := range mapsort.Keys(current) {
		next := current[address]
		if strings.TrimSpace(next.Generation) == "" {
			continue
		}
		prev := previous[address]
		if bgpCaptureAssignmentTransitionUnchanged(prev, next) {
			continue
		}
		kind := holderTransitionStartKind(prev, next, runtime.Previous.Placement.SeizeHoldDownActive)
		if recorder != nil {
			if _, err := recorder.RecordBusEvent(ctx, mobilityHolderTransitionEvent(runtime.Pool.Name, selfNode, now, next, kind, pathSigs[address], placement)); err != nil {
				return bgpCaptureTransitionState{}, err
			}
		}
	}
	for address := range bgpObservedSelfHolderAddresses(self, runtime.BGP.LivenessMarkers, runtime.BGP.PrefixCommunities) {
		assignment, ok := current[address]
		if !ok || assignment.Phase != "Active" || strings.TrimSpace(assignment.Generation) == "" {
			continue
		}
		if seizeComplete[address] == assignment.Generation {
			continue
		}
		if recorder != nil {
			if _, err := recorder.RecordBusEvent(ctx, mobilityHolderTransitionEvent(runtime.Pool.Name, selfNode, now, assignment, "seize-complete", pathSigs[address], placement)); err != nil {
				return bgpCaptureTransitionState{}, err
			}
		}
		seizeComplete[address] = assignment.Generation
	}
	latestProviderCapture := runtime.Provider.ActionHistory.captureTransitions
	for _, address := range mapsort.Keys(current) {
		assignment, ok := providerCaptureSeizeCompleteAssignment(self, current[address], latestProviderCapture, now)
		if !ok {
			continue
		}
		if seizeComplete[address] == assignment.Generation {
			continue
		}
		if recorder != nil {
			if _, err := recorder.RecordBusEvent(ctx, mobilityHolderTransitionEvent(runtime.Pool.Name, selfNode, now, assignment, "seize-complete", pathSigs[address], placement)); err != nil {
				return bgpCaptureTransitionState{}, err
			}
		}
		seizeComplete[address] = assignment.Generation
	}
	confirmedThisReconcile := map[string]bool{}
	for _, decision := range plan.Addresses {
		address := normalizeAddressString(decision.Address)
		if address == "" || decision.Class != ownershipClassConfirmedCapture || strings.TrimSpace(decision.CaptureHolderNode) != strings.TrimSpace(selfNode) {
			continue
		}
		assignment, ok := captureConfirmedAssignmentForDecision(current, decision, latestProviderCapture, now)
		if !ok {
			continue
		}
		confirmedThisReconcile[address] = true
		if captureConfirmed[address] == assignment.Generation {
			continue
		}
		if recorder != nil {
			if _, err := recorder.RecordBusEvent(ctx, mobilityHolderTransitionEvent(runtime.Pool.Name, selfNode, now, assignment, "capture-confirmed", pathSigs[address], placement)); err != nil {
				return bgpCaptureTransitionState{}, err
			}
		}
		captureConfirmed[address] = assignment.Generation
	}
	for address := range seizeComplete {
		if _, ok := current[address]; !ok {
			delete(seizeComplete, address)
		}
	}
	for address := range captureConfirmed {
		if _, ok := current[address]; ok {
			continue
		}
		if !confirmedThisReconcile[address] {
			delete(captureConfirmed, address)
		}
	}
	for _, address := range mapsort.Keys(previous) {
		if _, ok := current[address]; ok {
			continue
		}
		prev := previous[address]
		if strings.TrimSpace(prev.Generation) == "" {
			continue
		}
		released := bgpCaptureAssignment{
			Address:        prev.Address,
			Phase:          "Released",
			Generation:     prev.Generation,
			DesiredHolder:  "",
			PreviousHolder: prev.DesiredHolder,
			Reason:         firstNonEmpty(prev.Reason, "holder-yield"),
			IssuedAt:       prev.IssuedAt,
			RenewedAt:      now,
			LeaseUntil:     prev.LeaseUntil,
		}
		if recorder != nil {
			if _, err := recorder.RecordBusEvent(ctx, mobilityHolderTransitionEvent(runtime.Pool.Name, selfNode, now, released, "yield", pathSigs[address], placement)); err != nil {
				return bgpCaptureTransitionState{}, err
			}
		}
	}
	return transitions, nil
}

func bgpObservedSelfHolderAddresses(self memberPlanInfo, livenessMarkers map[string]string, mobilityPrefixCommunities map[string][]string) map[string]bool {
	out := map[string]bool{}
	community, _, present := livenessMarkerForNode(livenessMarkers, self.NodeRef)
	if !present || strings.TrimSpace(community) == "" {
		return out
	}
	community = strings.TrimSpace(community)
	for prefix, communities := range mobilityPrefixCommunities {
		address := normalizeAddressString(prefix)
		if address == "" {
			continue
		}
		hasActiveHolder := false
		hasSelfIdentity := false
		for _, item := range communities {
			switch strings.TrimSpace(item) {
			case bgpMobilityCommunityActiveHolder:
				hasActiveHolder = true
			case community:
				hasSelfIdentity = true
			}
		}
		if hasActiveHolder && hasSelfIdentity {
			out[address] = true
		}
	}
	return out
}

func captureConfirmedAssignmentForDecision(current map[string]bgpCaptureAssignment, decision ownershipDecision, latestProviderCapture map[string]providerCaptureTransition, now time.Time) (bgpCaptureAssignment, bool) {
	address := normalizeAddressString(decision.Address)
	if address == "" {
		return bgpCaptureAssignment{}, false
	}
	if assignment, ok := current[address]; ok && assignment.Phase == "Active" && strings.TrimSpace(assignment.Generation) != "" {
		return assignment, true
	}
	providerRef := strings.TrimSpace(decision.CaptureProviderRef)
	targetRef := strings.TrimSpace(decision.CaptureTargetRef)
	tr, ok := latestProviderCapture[providerCaptureTransitionKey(providerRef, targetRef, address)]
	if !ok || !tr.assign || !tr.succeeded {
		return bgpCaptureAssignment{}, false
	}
	generation, issuedAt := providerCaptureTransitionIdentity(tr, providerRef, targetRef, address, now)
	return bgpCaptureAssignment{
		Address:        address,
		Phase:          "Active",
		Generation:     generation,
		DesiredHolder:  strings.TrimSpace(decision.CaptureHolderNode),
		PreviousHolder: strings.TrimSpace(decision.HomeOwnerNode),
		Reason:         "provider-capture-confirmed",
		IssuedAt:       issuedAt,
		RenewedAt:      now.UTC(),
		LeaseUntil:     now.UTC().Add(DefaultLeaseTTL),
	}, true
}

func providerCaptureSeizeCompleteAssignment(self memberPlanInfo, assignment bgpCaptureAssignment, latestTransitions map[string]providerCaptureTransition, now time.Time) (bgpCaptureAssignment, bool) {
	address := normalizeAddressString(assignment.Address)
	if address == "" || assignment.Phase != "Active" || strings.TrimSpace(assignment.Generation) == "" {
		return bgpCaptureAssignment{}, false
	}
	if strings.TrimSpace(assignment.DesiredHolder) != strings.TrimSpace(self.NodeRef) {
		return bgpCaptureAssignment{}, false
	}
	if strings.TrimSpace(self.Capture.Type) != "provider-secondary-ip" {
		return bgpCaptureAssignment{}, false
	}
	providerRef := strings.TrimSpace(self.Capture.ProviderRef)
	targetRef := providerCaptureRefFromCapture(self.Capture)
	tr, ok := latestTransitions[providerCaptureTransitionKey(providerRef, targetRef, address)]
	if !ok || !tr.assign || !tr.succeeded {
		return bgpCaptureAssignment{}, false
	}
	if !providerCaptureTransitionMatchesAssignment(tr, assignment) {
		return bgpCaptureAssignment{}, false
	}
	if holder := strings.TrimSpace(tr.plan.Parameters[captureParamHolder]); holder != "" && holder != strings.TrimSpace(self.NodeRef) {
		return bgpCaptureAssignment{}, false
	}
	generation, issuedAt := providerCaptureTransitionIdentity(tr, providerRef, targetRef, address, now)
	assignment.Generation = generation
	assignment.Reason = "provider-capture-accepted"
	assignment.IssuedAt = issuedAt
	assignment.RenewedAt = now.UTC()
	return assignment, true
}

func providerCaptureTransitionMatchesAssignment(tr providerCaptureTransition, assignment bgpCaptureAssignment) bool {
	generation := strings.TrimSpace(tr.plan.Parameters[captureAssignmentGenerationParam])
	if generation == "" {
		return true
	}
	return generation == strings.TrimSpace(assignment.Generation)
}

func providerCaptureTransitionIdentity(tr providerCaptureTransition, providerRef, targetRef, address string, now time.Time) (string, time.Time) {
	issuedAt := tr.at.UTC()
	if issuedAt.IsZero() {
		issuedAt = now.UTC()
	}
	generation := ""
	if tr.id > 0 {
		generation = fmt.Sprintf("provider-capture/%d", tr.id)
	}
	if generation == "" {
		generation = strings.Join([]string{"provider-capture", safeName(providerRef), safeName(targetRef), safeName(address)}, "/")
	}
	return generation, issuedAt
}

func bgpCaptureAssignmentTransitionUnchanged(previous, current bgpCaptureAssignment) bool {
	return previous.Generation == current.Generation &&
		previous.Phase == current.Phase &&
		previous.DesiredHolder == current.DesiredHolder &&
		previous.PreviousHolder == current.PreviousHolder
}

func mobilityHolderTransitionEvent(poolName, selfNode string, now time.Time, assignment bgpCaptureAssignment, transitionKind, pathSig string, placement PlacementDecision) daemonapi.DaemonEvent {
	address := normalizeAddressString(assignment.Address)
	fromNode := strings.TrimSpace(assignment.PreviousHolder)
	toNode := strings.TrimSpace(assignment.DesiredHolder)
	reason := firstNonEmpty(assignment.Reason, transitionKind)
	seizeRemaining := int64(0)
	if placement.SeizeHoldDown && !placement.SeizeHoldDownUntil.IsZero() {
		seizeRemaining = nonNegativeCeilSeconds(placement.SeizeHoldDownUntil.Sub(now))
	}
	startupSettleRemaining := nonNegativeCeilSeconds(placement.StartupSettleUntil.Sub(now))
	attributes := map[string]string{
		"timestamp":                              now.UTC().Format(time.RFC3339Nano),
		"address":                                address,
		"transitionKind":                         transitionKind,
		"transitionReason":                       reason,
		"fromNode":                               fromNode,
		"toNode":                                 toNode,
		"mobilityPathSig":                        strings.TrimSpace(pathSig),
		"assignmentGeneration":                   assignment.Generation,
		"holdDownRemainingSeconds.seize":         strconv.FormatInt(seizeRemaining, 10),
		"holdDownRemainingSeconds.startupSettle": strconv.FormatInt(startupSettleRemaining, 10),
	}
	if !assignment.IssuedAt.IsZero() {
		attributes["issuedAt"] = assignment.IssuedAt.UTC().Format(time.RFC3339Nano)
	}
	if !assignment.RenewedAt.IsZero() {
		attributes["renewedAt"] = assignment.RenewedAt.UTC().Format(time.RFC3339Nano)
	}
	if !assignment.LeaseUntil.IsZero() {
		attributes["leaseUntil"] = assignment.LeaseUntil.UTC().Format(time.RFC3339Nano)
	}
	message := fmt.Sprintf("%s %s holder %s -> %s", transitionKind, address, firstNonEmpty(fromNode, "<none>"), firstNonEmpty(toNode, "<none>"))
	return daemonapi.DaemonEvent{
		Type:     mobilityHolderTransitionTopic,
		Severity: "Normal",
		Reason:   "HolderTransition",
		Message:  message,
		Daemon: daemonapi.DaemonRef{
			Kind:     "controller",
			Name:     "mobility",
			Instance: strings.TrimSpace(selfNode),
		},
		Resource: &daemonapi.ResourceRef{
			APIVersion: api.MobilityAPIVersion,
			Kind:       "MobilityPool",
			Name:       strings.TrimSpace(poolName),
		},
		Attributes: attributes,
	}
}

func holderTransitionStartKind(previous, current bgpCaptureAssignment, previousPending bool) string {
	reason := strings.ToLower(strings.TrimSpace(current.Reason))
	if current.Phase == "Pending" {
		return "defer-start"
	}
	if previous.Phase == "Pending" && current.Phase != "Pending" {
		return "defer-release"
	}
	if previousPending && current.Phase == "Active" {
		return "defer-release"
	}
	if current.Phase == "Active" && strings.TrimSpace(current.PreviousHolder) != "" && strings.TrimSpace(current.PreviousHolder) != strings.TrimSpace(current.DesiredHolder) {
		if strings.Contains(reason, "graceful") || strings.Contains(reason, "drain") || strings.Contains(reason, "handover") {
			return "graceful-handover"
		}
		return "seize-start"
	}
	if strings.TrimSpace(previous.Generation) == "" && current.Phase == "Active" {
		return "seize-start"
	}
	if strings.TrimSpace(previous.DesiredHolder) != "" && strings.TrimSpace(previous.DesiredHolder) != strings.TrimSpace(current.DesiredHolder) {
		return "seize-start"
	}
	return "holder-renew"
}

func decodeBGPCaptureTransitionState(status map[string]any) bgpCaptureTransitionState {
	completed := decodeStatusValue[map[string]map[string]string](status[bgpCaptureTransitionCompletedKey])
	return bgpCaptureTransitionState{
		SeizeComplete:    normalizedBGPCaptureTransitionMarkers(completed[bgpCaptureTransitionCompletedField]),
		CaptureConfirmed: normalizedBGPCaptureTransitionMarkers(completed[bgpCaptureConfirmedField]),
	}
}

func normalizedBGPCaptureTransitionMarkers(in map[string]string) map[string]string {
	out := map[string]string{}
	for address, generation := range in {
		address = normalizeAddressString(address)
		generation = strings.TrimSpace(generation)
		if address != "" && generation != "" {
			out[address] = generation
		}
	}
	return out
}

func cloneBGPCaptureTransitionState(in bgpCaptureTransitionState) bgpCaptureTransitionState {
	return bgpCaptureTransitionState{
		SeizeComplete:    normalizedBGPCaptureTransitionMarkers(in.SeizeComplete),
		CaptureConfirmed: normalizedBGPCaptureTransitionMarkers(in.CaptureConfirmed),
	}
}

func writeBGPCaptureTransitionState(status map[string]any, state bgpCaptureTransitionState) {
	encoded := map[string]map[string]string{}
	if values := normalizedBGPCaptureTransitionMarkers(state.SeizeComplete); len(values) > 0 {
		encoded[bgpCaptureTransitionCompletedField] = values
	}
	if values := normalizedBGPCaptureTransitionMarkers(state.CaptureConfirmed); len(values) > 0 {
		encoded[bgpCaptureConfirmedField] = values
	}
	// Status writers use a partial merge so omitting this key would preserve a
	// completion marker from an older reconciliation.  An explicit empty map is
	// the durable representation of "no completed transitions" and keeps the
	// typed continuation state exact after a release.
	status[bgpCaptureTransitionCompletedKey] = encoded
}
