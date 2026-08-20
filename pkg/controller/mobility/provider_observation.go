// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type providerObservationStatus struct {
	PendingAddresses []string
	PendingCount     int
}

func updatePendingProviderUnassignObservations(pendingAddresses map[string]bool, history ProviderActionHistory, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time) {
	if len(history.previousCaptureKeys) == 0 {
		return
	}
	for key, tr := range history.captureTransitions {
		if !history.previousCaptureKeys[key] || tr.assign || !tr.succeeded {
			continue
		}
		address := normalizeAddressString(tr.plan.Target["address"])
		if address == "" {
			continue
		}
		if observedSelfCapturesOK && !observedSelfCaptures[address] && providerObservationFresh(observedSelfAt, tr.at) {
			delete(pendingAddresses, address)
			continue
		}
		pendingAddresses[address] = true
	}
}

func actionRecordCompletedAt(rec providerActionJournalRecord) time.Time {
	if !rec.executedAt.IsZero() {
		return rec.executedAt.UTC()
	}
	if !rec.updatedAt.IsZero() {
		return rec.updatedAt.UTC()
	}
	return time.Time{}
}

// actionPlanTarget keeps status projection on the current plan when present,
// while preserving the journal target for historical plans that lack it.
func actionPlanTarget(plan dynamicconfig.ActionPlan, rec providerActionJournalRecord) map[string]string {
	if len(plan.Target) != 0 {
		return plan.Target
	}
	return rec.target
}

func providerObservationFresh(observedAt, requiredAfter time.Time) bool {
	if requiredAfter.IsZero() {
		return true
	}
	if observedAt.IsZero() {
		return false
	}
	return !observedAt.UTC().Before(requiredAfter.UTC())
}

type providerActionPlanFailure struct {
	Address  string
	Error    string
	FailedAt time.Time
}

// projectProviderPlanStatus is the one current-plan/history interpretation
// for action lifecycle, provider capture observation, and forwarding
// observation. Each output has intentionally different success conditions,
// but they share the same latest record for each current plan.
func projectProviderPlanStatus(plans []dynamicconfig.ActionPlan, history ProviderActionHistory, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time, forwardingObserved, forwardingEnabled bool, forwardingObservedAt time.Time) (providerActionStatusProjection, providerObservationStatus) {
	latest := history.latestByKey
	pendingAddresses := map[string]bool{}
	pendingTargets := map[string]bool{}
	actionStatus := providerActionStatusProjection{Phase: "OK"}
	var failures []providerActionPlanFailure
	for _, plan := range plans {
		key := strings.TrimSpace(plan.IdempotencyKey)
		if key == "" {
			continue
		}
		rec, found := latest[key]
		status := strings.TrimSpace(rec.status)
		if status != routerstate.ActionSucceeded && status != routerstate.ActionSkipped {
			actionStatus.PendingCount++
		}
		if found && status == routerstate.ActionFailed {
			target := actionPlanTarget(plan, rec)
			address := ""
			assign := isProviderCaptureAssignAction(plan.Action)
			if assign || isProviderCaptureUnassignAction(plan.Action) {
				address = normalizeAddressString(target["address"])
			}
			if !assign || address == "" || !observedSelfCaptures[address] || !providerObservationFresh(observedSelfAt, actionRecordCompletedAt(rec)) {
				failedAt := rec.executedAt
				if failedAt.IsZero() {
					failedAt = rec.updatedAt
				}
				failures = append(failures, providerActionPlanFailure{
					Address:  address,
					Error:    strings.TrimSpace(firstNonEmpty(rec.errorMessage, rec.resultMessage)),
					FailedAt: failedAt,
				})
			}
		}
		if !found || status != routerstate.ActionSucceeded {
			continue
		}
		target := actionPlanTarget(plan, rec)
		if isProviderCaptureAssignAction(plan.Action) {
			address := normalizeAddressString(target["address"])
			if address != "" {
				if observedSelfCapturesOK && observedSelfCaptures[address] && providerObservationFresh(observedSelfAt, actionRecordCompletedAt(rec)) {
					delete(pendingAddresses, address)
				} else {
					pendingAddresses[address] = true
				}
			}
		}
		if strings.TrimSpace(plan.Action) == "ensure-forwarding-enabled" {
			targetRef := strings.TrimSpace(firstNonEmpty(target["nicRef"], target["resourceRef"], target["routeTableRef"], target["providerRef"]))
			if targetRef != "" {
				if forwardingObserved && forwardingEnabled && providerObservationFresh(forwardingObservedAt, actionRecordCompletedAt(rec)) {
					delete(pendingTargets, targetRef)
				} else {
					pendingTargets[targetRef] = true
				}
			}
		}
	}
	updatePendingProviderUnassignObservations(pendingAddresses, history, observedSelfCaptures, observedSelfCapturesOK, observedSelfAt)
	observation := providerObservationStatus{
		PendingCount: len(pendingAddresses) + len(pendingTargets),
	}
	if len(pendingAddresses) != 0 {
		observation.PendingAddresses = mapsort.Keys(pendingAddresses)
	}
	if len(failures) > 0 {
		actionStatus.Phase = "Failed"
		actionStatus.FailedAddresses, actionStatus.Error = providerActionFailureStatus(failures)
	}
	return actionStatus, observation
}

func decisionHomeProviderRefMatches(decision ownershipDecision, providerRef string) bool {
	providerRef = strings.TrimSpace(providerRef)
	if providerRef == "" {
		return false
	}
	for _, candidate := range []string{decision.HomeProviderRef, decision.LocalProviderRef} {
		if strings.TrimSpace(candidate) == providerRef {
			return true
		}
	}
	return false
}
