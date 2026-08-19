// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// ProviderActionHistory is the one typed view of the durable provider-action
// journal and the preceding provider plan for a pool reconciliation.  The
// controller shell builds it once; ownership, capture planning, status, and
// transition recording consume the same interpretation rather than each
// rescanning action records and JSON fields.
type ProviderActionHistory struct {
	// sourceRevision identifies the most recent DynamicConfigPart for this
	// pool/node, including an empty or expired one. It gives a reintroduced
	// assignment a new generation even if it was withdrawn before the provider
	// action engine imported it into the journal.
	sourceRevision      string
	latestJournalAction int64

	latestByKey map[string]providerActionJournalRecord

	// captureTransitions includes the prior assign plan as a non-terminal
	// transition, then replaces it with a later successful journal transition.
	// Prior plans have succeeded=false; consumers that require evidence of an
	// executed provider operation use that existing discriminator instead of
	// keeping a second journal-only transition map.
	captureTransitions     map[string]providerCaptureTransition
	previousCaptureKeys    map[string]bool
	previousCaptureAssigns []dynamicconfig.ActionPlan
}

// providerActionJournalRecord is the in-memory representation of the action
// journal row used by the mobility core. JSON decoding is deliberately kept at
// this durable-store boundary so status, discovery, and fence code consume the
// same typed target and parameter maps.
type providerActionJournalRecord struct {
	id             int64
	idempotencyKey string
	providerRef    string
	action         string
	target         map[string]string
	parameters     map[string]string
	status         string
	errorMessage   string
	resultMessage  string
	executedAt     time.Time
	updatedAt      time.Time
}

func providerActionJournalRecordFrom(row routerstate.ActionExecutionRecord) providerActionJournalRecord {
	return providerActionJournalRecord{
		id:             row.ID,
		idempotencyKey: row.IdempotencyKey,
		providerRef:    row.ProviderRef,
		action:         row.Action,
		target:         decodeActionRecordMap(row.TargetJSON),
		parameters:     decodeActionRecordMap(row.ParametersJSON),
		status:         row.Status,
		errorMessage:   row.Error,
		resultMessage:  row.ResultMessage,
		executedAt:     row.ExecutedAt,
		updatedAt:      row.UpdatedAt,
	}
}

func decodeActionRecordMap(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

type providerActionHistoryStore interface {
	dynamicConfigPartSourceReader
	ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
}

// providerActionHistoryForPool is the one durable prior-action read shared by
// Mobility and Discovery.  It makes the active DynamicConfigPart and action
// journal one typed input instead of letting Discovery rescan their raw JSON.
func providerActionHistoryForPool(store providerActionHistoryStore, poolName, selfNode string, now time.Time) (ProviderActionHistory, error) {
	plans, revision, err := previousGeneratedActionPlans(store, poolName, selfNode, now)
	if err != nil {
		return ProviderActionHistory{}, err
	}
	journal, err := store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return ProviderActionHistory{}, fmt.Errorf("list action journal: %w", err)
	}
	return newProviderActionHistoryWithRevision(plans, journal, revision), nil
}

func newProviderActionHistoryWithRevision(previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, sourceRevision string) ProviderActionHistory {
	history := ProviderActionHistory{
		sourceRevision:      strings.TrimSpace(sourceRevision),
		latestByKey:         map[string]providerActionJournalRecord{},
		captureTransitions:  map[string]providerCaptureTransition{},
		previousCaptureKeys: map[string]bool{},
	}
	for _, plan := range previousPlans {
		assign := isProviderCaptureAssignAction(plan.Action)
		if !assign && !isProviderCaptureUnassignAction(plan.Action) {
			continue
		}
		key := providerCaptureTransitionKey(firstNonEmpty(plan.ProviderRef, plan.Target["providerRef"]), providerCaptureRefFromTarget(plan.Target), plan.Target["address"])
		if key == "" {
			continue
		}
		history.previousCaptureKeys[key] = true
		if !assign {
			continue
		}
		history.previousCaptureAssigns = append(history.previousCaptureAssigns, plan)
		history.captureTransitions[key] = providerCaptureTransition{assign: true, plan: plan}
	}
	for _, row := range journal {
		history.addJournalRecord(row)
	}
	return history
}

func providerActionStatusPending(status string) bool {
	switch strings.TrimSpace(status) {
	case routerstate.ActionPending, routerstate.ActionApproved, routerstate.ActionRunning:
		return true
	default:
		return false
	}
}

func (h *ProviderActionHistory) addJournalRecord(row routerstate.ActionExecutionRecord) {
	if row.ID > h.latestJournalAction {
		h.latestJournalAction = row.ID
	}
	record := providerActionJournalRecordFrom(row)
	key := strings.TrimSpace(record.idempotencyKey)
	if key == "" {
		key = "\x00" + strconv.FormatInt(record.id, 10)
	}
	if previous, found := h.latestByKey[key]; !found || record.updatedAt.After(previous.updatedAt) || (record.updatedAt.Equal(previous.updatedAt) && record.id > previous.id) {
		h.latestByKey[key] = record
	}
	key, transition, ok := providerCaptureTransitionFromActionRecord(record)
	if !ok {
		return
	}
	// The combined transition map begins with the prior desired assign plan as
	// its lowest-precedence value, then uses the same ordering for executed
	// journal transitions.
	replaceProviderCaptureTransition(h.captureTransitions, key, transition)
}

func captureAssignmentFromActionPlan(plan dynamicconfig.ActionPlan) (bgpCaptureAssignment, bool) {
	if !isProviderCaptureAssignAction(plan.Action) {
		return bgpCaptureAssignment{}, false
	}
	address := normalizeAddressString(plan.Target["address"])
	generation := strings.TrimSpace(plan.Parameters[captureAssignmentGenerationParam])
	desiredHolder := strings.TrimSpace(plan.Parameters[captureAssignmentDesiredHolderParam])
	leaseUntil, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(plan.Parameters[captureAssignmentLeaseUntilParam]))
	if address == "" || generation == "" || desiredHolder == "" || err != nil {
		return bgpCaptureAssignment{}, false
	}
	return bgpCaptureAssignment{
		Address:        address,
		Phase:          "Active",
		Generation:     generation,
		DesiredHolder:  desiredHolder,
		PreviousHolder: strings.TrimSpace(plan.Parameters[captureAssignmentPreviousHolderParam]),
		Reason:         firstNonEmpty(strings.TrimSpace(plan.Parameters[captureAssignmentReasonParam]), "provider-action-plan"),
		LeaseUntil:     leaseUntil.UTC(),
	}, true
}

// captureAssignmentsFromActionPlans is the one typed transition projection of
// the final provider ActionPlan set. It is deliberately built only after the
// capture gate has selected plans, so unpublished plans cannot emit holder
// events or become separate planner state.
func captureAssignmentsFromActionPlans(plans []dynamicconfig.ActionPlan) map[string]bgpCaptureAssignment {
	out := map[string]bgpCaptureAssignment{}
	for _, plan := range plans {
		assignment, ok := captureAssignmentFromActionPlan(plan)
		if ok {
			out[assignment.Address] = assignment
		}
	}
	return out
}

func (h ProviderActionHistory) priorCaptureAssignment(address string) (dynamicconfig.ActionPlan, bgpCaptureAssignment, bool) {
	address = normalizeAddressString(address)
	for _, plan := range h.previousCaptureAssigns {
		assignment, ok := captureAssignmentFromActionPlan(plan)
		if ok && assignment.Address == address {
			return plan, assignment, true
		}
	}
	return dynamicconfig.ActionPlan{}, bgpCaptureAssignment{}, false
}

func (h ProviderActionHistory) nextCaptureAssignmentGeneration(poolName, address string, plan dynamicconfig.ActionPlan) string {
	scope := strings.TrimSpace(poolName)
	address = normalizeAddressString(address)
	if scope == "" || address == "" {
		return ""
	}
	seed := firstNonEmpty(strings.TrimSpace(h.sourceRevision), "initial")
	if h.latestJournalAction > 0 {
		seed += "\x00journal=" + strconv.FormatInt(h.latestJournalAction, 10)
	}
	return strings.Join([]string{scope, safeName(address), bgpPathSigHash(seed + "\x00" + captureAssignmentIntentKey(plan))}, "/")
}

func providerCaptureTransitionFromActionRecord(row providerActionJournalRecord) (string, providerCaptureTransition, bool) {
	if row.status != routerstate.ActionSucceeded {
		return "", providerCaptureTransition{}, false
	}
	assign := false
	switch {
	case isProviderCaptureAssignAction(row.action):
		assign = true
	case isProviderCaptureUnassignAction(row.action):
	default:
		return "", providerCaptureTransition{}, false
	}
	key := providerCaptureTransitionKey(firstNonEmpty(row.providerRef, row.target["providerRef"]), providerCaptureRefFromTarget(row.target), row.target["address"])
	if key == "" {
		return "", providerCaptureTransition{}, false
	}
	at := actionRecordCompletedAt(row)
	return key, providerCaptureTransition{
		at:        at,
		id:        row.id,
		assign:    assign,
		succeeded: true,
		plan: dynamicconfig.ActionPlan{
			IdempotencyKey: row.idempotencyKey,
			ProviderRef:    row.providerRef,
			Action:         row.action,
			Target:         copyStringMap(row.target),
			Parameters:     copyStringMap(row.parameters),
		},
	}, true
}

func replaceProviderCaptureTransition(transitions map[string]providerCaptureTransition, key string, candidate providerCaptureTransition) bool {
	previous, found := transitions[key]
	if found && !candidate.at.After(previous.at) && !(candidate.at.Equal(previous.at) && candidate.id > previous.id) {
		return false
	}
	transitions[key] = candidate
	return true
}

// providerCaptureReleaseSource is the typed provider target and BGP fence
// required to deprovision a preceding capture. It avoids reconstituting a
// journal record as a synthetic ActionPlan only for it to be parsed again.
type providerCaptureReleaseSource struct {
	Capture api.MobilityMemberCapture
	PathSig string
}

// releaseSourceFor returns the only valid source for a release. An active
// preceding plan always wins because it is the currently fenced desired state.
// If no such plan remains, the latest completed journal transition may provide
// the source only when it is still a successful local assign. A later unassign
// deliberately replaces that transition and cannot be reused as a release
// source.
func (h ProviderActionHistory) releaseSourceFor(self memberPlanInfo, address string) (providerCaptureReleaseSource, bool) {
	address = normalizeAddressString(address)
	if address == "" {
		return providerCaptureReleaseSource{}, false
	}
	var previous dynamicconfig.ActionPlan
	found := false
	for _, plan := range h.previousCaptureAssigns {
		if normalizeAddressString(plan.Target["address"]) != address {
			continue
		}
		if !found || plan.Action > previous.Action || (plan.Action == previous.Action && plan.IdempotencyKey > previous.IdempotencyKey) {
			previous, found = plan, true
		}
	}
	if found {
		return providerCaptureReleaseSourceFromPlan(self.Capture, previous, address, false), true
	}
	key := providerCaptureTransitionKey(self.Capture.ProviderRef, providerCaptureRefFromCapture(self.Capture), address)
	transition, ok := h.captureTransitions[key]
	if !ok || !transition.assign || !transition.succeeded {
		return providerCaptureReleaseSource{}, false
	}
	if holder := strings.TrimSpace(transition.plan.Parameters[captureParamHolder]); holder != "" && holder != strings.TrimSpace(self.NodeRef) {
		return providerCaptureReleaseSource{}, false
	}
	return providerCaptureReleaseSourceFromPlan(self.Capture, transition.plan, address, true), true
}

func providerCaptureReleaseSourceFromPlan(fallback api.MobilityMemberCapture, plan dynamicconfig.ActionPlan, address string, currentTarget bool) providerCaptureReleaseSource {
	capture := fallback
	capture.Type = "provider-secondary-ip"
	if len(plan.Target) != 0 {
		capture.Target = copyStringMap(plan.Target)
	}
	if currentTarget {
		if capture.Target == nil {
			capture.Target = map[string]string{}
		}
		capture.Target["address"] = address
		capture.Target["providerRef"] = strings.TrimSpace(fallback.ProviderRef)
		capture.Target["nicRef"] = strings.TrimSpace(fallback.NICRef)
		capture.Target["captureStrategy"] = providerCaptureStrategy(fallback)
	}
	if value := strings.TrimSpace(plan.ProviderRef); value != "" {
		capture.ProviderRef = value
	}
	if value := strings.TrimSpace(capture.Target["providerRef"]); value != "" {
		capture.ProviderRef = value
	}
	if value := strings.TrimSpace(capture.Target["nicRef"]); value != "" {
		capture.NICRef = value
	}
	capture.CaptureStrategy = ""
	if strings.TrimSpace(capture.Target["captureStrategy"]) == captureStrategyRouteTable {
		capture.CaptureStrategy = captureStrategyRouteTable
	}
	return providerCaptureReleaseSource{Capture: capture, PathSig: bgpPathSigFromActionPlan(plan, address)}
}
