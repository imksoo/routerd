// SPDX-License-Identifier: BSD-3-Clause
// Provider-action fences and journal-derived capture transition state.
package mobility

import (
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func shouldAllowBGPTrapReassignment(self memberPlanInfo, address string, history ProviderActionHistory, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time) bool {
	address = normalizeAddressString(address)
	if address == "" {
		return false
	}
	latest := history.captureTransitions
	key := providerCaptureTransitionKey(self.Capture.ProviderRef, providerCaptureRefFromCapture(self.Capture), address)
	tr, ok := latest[key]
	if !ok && observedSelfCapturesOK && !observedSelfCaptures[address] {
		return true
	}
	if !ok {
		return false
	}
	if observedSelfCapturesOK && !observedSelfCaptures[address] {
		if !tr.assign {
			return providerCaptureTransitionAllowsRecapture(tr)
		}
		return providerMissingRetryDue(tr, observedSelfAt)
	}
	return !tr.assign && providerCaptureTransitionAllowsRecapture(tr)
}

func stampBGPPathFenceActionPlans(plans []dynamicconfig.ActionPlan, address, pathSig, holder string, lastSeenAt time.Time) {
	address = normalizeAddressString(address)
	pathSig = strings.TrimSpace(pathSig)
	holder = strings.TrimSpace(holder)
	if pathSig == "" {
		pathSig = "prefix=" + address
	}
	hash := bgpPathSigHash(pathSig)
	for i := range plans {
		plan := &plans[i]
		if plan.Parameters == nil {
			plan.Parameters = map[string]string{}
		}
		plan.Parameters[bgpPathSigParam] = pathSig
		plan.Parameters[captureParamHolder] = holder
		if !lastSeenAt.IsZero() {
			plan.Parameters[bgpTrapLastSeenAtParam] = lastSeenAt.UTC().Format(time.RFC3339Nano)
		}
		if strings.TrimSpace(plan.IdempotencyKey) != "" {
			plan.IdempotencyKey = plan.IdempotencyKey + ":holder:" + safeName(holder) + ":pathsig:" + hash
		}
	}
}

// stampBGPAssignmentFenceActionPlans stamps the provider-action fence from
// the previous active DynamicConfigPart plus the action journal. It never
// consults MobilityPool status: a withdrawn plan is therefore not reusable,
// while an unchanged active plan retains its generation across reconciles.
func stampBGPAssignmentFenceActionPlans(plans []dynamicconfig.ActionPlan, pool NormalizedMobilityPool, decisions map[string]ownershipDecision, placement PlacementDecision, history ProviderActionHistory, now time.Time) {
	now = now.UTC()
	for i := range plans {
		plan := &plans[i]
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
		address := normalizeAddressString(plan.Target["address"])
		if address == "" {
			continue
		}
		decision := decisions[address]
		previousHolder := firstNonEmpty(strings.TrimSpace(decision.CaptureHolderNode), strings.TrimSpace(placement.ActiveIdentityNodeRef))
		assignment := bgpCaptureAssignment{
			Address:        address,
			Phase:          "Active",
			DesiredHolder:  strings.TrimSpace(pool.Self.NodeRef),
			PreviousHolder: previousHolder,
			Reason:         captureAssignmentReason(pool.Members, placement, previousHolder),
			IssuedAt:       now,
			RenewedAt:      now,
			LeaseUntil:     now.Add(DefaultLeaseTTL).UTC(),
		}
		if previousPlan, previous, ok := history.priorCaptureAssignment(address); ok && captureAssignmentMatchesPlan(previousPlan, previous, *plan, assignment, decision, history) {
			assignment.Generation = previous.Generation
		} else {
			assignment.Generation = history.nextCaptureAssignmentGeneration(pool.Name, address, *plan)
		}
		if plan.Parameters == nil {
			plan.Parameters = map[string]string{}
		}
		plan.Parameters[captureAssignmentGenerationParam] = assignment.Generation
		plan.Parameters[captureAssignmentDesiredHolderParam] = assignment.DesiredHolder
		plan.Parameters[captureAssignmentReasonParam] = assignment.Reason
		if assignment.PreviousHolder != "" {
			plan.Parameters[captureAssignmentPreviousHolderParam] = assignment.PreviousHolder
		}
		if !assignment.LeaseUntil.IsZero() {
			plan.Parameters[captureAssignmentLeaseUntilParam] = assignment.LeaseUntil.UTC().Format(time.RFC3339Nano)
		}
		if strings.TrimSpace(plan.IdempotencyKey) != "" && strings.TrimSpace(assignment.Generation) != "" {
			plan.IdempotencyKey += ":assigngen:" + safeName(assignment.Generation)
		}
	}
}

func captureAssignmentReason(members map[string]memberPlanInfo, placement PlacementDecision, previousHolder string) string {
	if placement.Seize {
		return firstNonEmpty(strings.TrimSpace(placement.Reason), "hard-failure")
	}
	if previous, ok := lookupMemberByNodeRef(members, previousHolder); ok && previous.MaintenanceDrain {
		return "graceful-drain"
	}
	return firstNonEmpty(strings.TrimSpace(placement.Reason), "placement-election")
}

func captureAssignmentMatchesPlan(previousPlan dynamicconfig.ActionPlan, previous bgpCaptureAssignment, plan dynamicconfig.ActionPlan, assignment bgpCaptureAssignment, decision ownershipDecision, history ProviderActionHistory) bool {
	if strings.TrimSpace(previous.Generation) == "" || previous.Phase != "Active" ||
		strings.TrimSpace(previous.DesiredHolder) != strings.TrimSpace(assignment.DesiredHolder) {
		return false
	}
	if captureAssignmentIntentKey(previousPlan) != captureAssignmentIntentKey(plan) ||
		bgpPathSigFromActionPlan(previousPlan, previous.Address) != bgpPathSigFromActionPlan(plan, assignment.Address) {
		return false
	}
	// Preserve the existing fence when the old plan omitted previousHolder;
	// provideraction treats that form as compatible with a newly-known holder.
	oldHolder := strings.TrimSpace(previous.PreviousHolder)
	if oldHolder == "" || oldHolder == strings.TrimSpace(assignment.PreviousHolder) {
		return true
	}
	return captureAssignmentConfirmedHolderConverged(previousPlan, previous, assignment, decision, history)
}

// captureAssignmentConfirmedHolderConverged preserves an active assignment's
// generation only after its own provider action has succeeded and ownership
// has converged from the former holder to this same local holder. A missing,
// failed, or later-unassigned journal transition remains a fence boundary and
// therefore cannot reuse the old generation.
func captureAssignmentConfirmedHolderConverged(previousPlan dynamicconfig.ActionPlan, previous, assignment bgpCaptureAssignment, decision ownershipDecision, history ProviderActionHistory) bool {
	desiredHolder := strings.TrimSpace(assignment.DesiredHolder)
	if desiredHolder == "" ||
		decision.CaptureDisposition != dynamicconfig.CaptureDesired ||
		strings.TrimSpace(decision.CaptureHolderNode) != desiredHolder ||
		strings.TrimSpace(assignment.PreviousHolder) != desiredHolder ||
		strings.TrimSpace(previous.PreviousHolder) == desiredHolder {
		return false
	}
	key := providerCaptureTransitionKey(previousPlan.ProviderRef, providerCaptureRefFromTarget(previousPlan.Target), previous.Address)
	transition, ok := history.captureTransitions[key]
	return ok && transition.assign && transition.succeeded &&
		strings.TrimSpace(transition.plan.IdempotencyKey) == strings.TrimSpace(previousPlan.IdempotencyKey)
}

func captureAssignmentIntentKey(plan dynamicconfig.ActionPlan) string {
	values := []string{
		"provider=" + strings.TrimSpace(plan.Provider),
		"providerRef=" + strings.TrimSpace(plan.ProviderRef),
		"action=" + strings.TrimSpace(plan.Action),
	}
	for _, key := range mapsort.Keys(plan.Target) {
		values = append(values, "target."+key+"="+strings.TrimSpace(plan.Target[key]))
	}
	for _, key := range mapsort.Keys(plan.Parameters) {
		switch key {
		case captureAssignmentGenerationParam, captureAssignmentDesiredHolderParam, captureAssignmentPreviousHolderParam, captureAssignmentLeaseUntilParam, captureAssignmentReasonParam, bgpPathSigParam, bgpTrapLastSeenAtParam:
			continue
		}
		values = append(values, "param."+key+"="+strings.TrimSpace(plan.Parameters[key]))
	}
	return strings.Join(values, "\x00")
}

func stampBGPProviderTransitionFences(plans []dynamicconfig.ActionPlan, self memberPlanInfo, onlyAddress string, history ProviderActionHistory, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time) {
	onlyAddress = normalizeAddressString(onlyAddress)
	latest := history.captureTransitions
	for i := range plans {
		plan := &plans[i]
		if !isProviderCaptureAssignAction(plan.Action) || strings.TrimSpace(plan.IdempotencyKey) == "" {
			continue
		}
		address := normalizeAddressString(plan.Target["address"])
		if address == "" || (onlyAddress != "" && address != onlyAddress) {
			continue
		}
		key := providerCaptureTransitionKey(self.Capture.ProviderRef, providerCaptureRefFromCapture(self.Capture), address)
		tr, ok := latest[key]
		if !ok || !tr.succeeded {
			continue
		}
		token := ""
		switch {
		case !tr.assign && providerCaptureTransitionAllowsRecapture(tr):
			if providerCaptureCanonicalAssignSucceeded(*plan, history, false) {
				token = fmt.Sprintf("after-unassign-%d", tr.id)
			}
		case tr.assign && observedSelfCapturesOK && !observedSelfCaptures[address] && providerMissingRetryDue(tr, observedSelfAt):
			if providerCaptureCanonicalAssignSucceeded(*plan, history, true) {
				token = fmt.Sprintf("provider-missing-%d", tr.id)
			}
		}
		if token == "" {
			continue
		}
		plan.IdempotencyKey += ":transition:" + safeName(token)
	}
}

func providerMissingRetryDue(tr providerCaptureTransition, observedSelfAt time.Time) bool {
	if tr.at.IsZero() || observedSelfAt.IsZero() {
		return false
	}
	return !observedSelfAt.Before(tr.at.Add(bgpProviderMissingRetryHold))
}

func providerCaptureCanonicalAssignSucceeded(plan dynamicconfig.ActionPlan, history ProviderActionHistory, requirePathSig bool) bool {
	if !isProviderCaptureAssignAction(plan.Action) {
		return false
	}
	canonicalKey := strings.TrimSpace(plan.IdempotencyKey)
	for _, row := range history.latestByKey {
		if row.status != routerstate.ActionSucceeded || !isProviderCaptureAssignAction(row.action) {
			continue
		}
		if canonicalKey != "" && strings.TrimSpace(row.idempotencyKey) == canonicalKey {
			return true
		}
		if providerCaptureAssignRecordMatchesPlan(row, plan, requirePathSig) {
			return true
		}
	}
	return false
}

func providerCaptureAssignRecordMatchesPlan(row providerActionJournalRecord, plan dynamicconfig.ActionPlan, requirePathSig bool) bool {
	if strings.TrimSpace(row.providerRef) != strings.TrimSpace(plan.ProviderRef) ||
		strings.TrimSpace(row.action) != strings.TrimSpace(plan.Action) {
		return false
	}
	if normalizeAddressString(row.target["address"]) != normalizeAddressString(plan.Target["address"]) {
		return false
	}
	if providerCaptureRefFromTarget(row.target) != providerCaptureRefFromTarget(plan.Target) {
		return false
	}
	keys := []string{captureParamHolder, captureAssignmentGenerationParam}
	if requirePathSig {
		keys = append(keys, bgpPathSigParam)
	}
	for _, key := range keys {
		left := strings.TrimSpace(row.parameters[key])
		right := strings.TrimSpace(plan.Parameters[key])
		if left != "" && right != "" && left != right {
			return false
		}
	}
	return true
}

func providerCaptureTransitionAllowsRecapture(tr providerCaptureTransition) bool {
	params := tr.plan.Parameters
	if strings.TrimSpace(params["deprovisionSince"]) != "" {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(params[bgpPathSigParam]), "deprovision:") {
		return false
	}
	return true
}

func stampForwardingDriftFence(plans []dynamicconfig.ActionPlan, observed, enabled bool, observedAt time.Time) {
	if !observed || enabled {
		return
	}
	token := "observed-disabled"
	if !observedAt.IsZero() {
		token += "-" + observedAt.UTC().Format("20060102T150405.000000000Z")
	}
	for i := range plans {
		plan := &plans[i]
		if plan.Action != "ensure-forwarding-enabled" || strings.TrimSpace(plan.IdempotencyKey) == "" {
			continue
		}
		plan.IdempotencyKey += ":forwarding-drift:" + safeName(token)
	}
}

type providerCaptureTransition struct {
	at        time.Time
	id        int64
	assign    bool
	succeeded bool
	plan      dynamicconfig.ActionPlan
}

func providerCaptureTransitionKey(providerRef, nicRef, address string) string {
	providerRef = strings.TrimSpace(providerRef)
	nicRef = strings.TrimSpace(nicRef)
	address = normalizeAddressString(address)
	if providerRef == "" || nicRef == "" || address == "" {
		return ""
	}
	return providerRef + "\x00" + nicRef + "\x00" + address
}

func bgpPathSigFromActionPlan(plan dynamicconfig.ActionPlan, address string) string {
	if sig := strings.TrimSpace(plan.Parameters[bgpPathSigParam]); sig != "" {
		return sig
	}
	return "deprovision:" + normalizeAddressString(address)
}
