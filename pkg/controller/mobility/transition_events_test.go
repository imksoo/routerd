// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRecordBGPCaptureAssignmentTransitionsEmitsMachineReadableSequence(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Store: store}
	address := "10.88.60.10/32"
	self := memberPlanInfo{
		NodeRef: "aws-rr-b",
	}
	livenessMarkers := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-rr-b"): "10.99.0.12/32",
	}
	mobilityPrefixCommunities := map[string][]string{
		address: {
			bgpMobilityCommunityOwner,
			bgpMobilityCommunityActiveHolder,
			bgpstate.MobilityNodeIdentityCommunity("aws-rr-b"),
		},
	}
	assignment := bgpCaptureAssignment{
		Address:        address,
		Phase:          "Active",
		Generation:     "group-a/7",
		DesiredHolder:  "aws-rr-b",
		PreviousHolder: "aws-rr-a",
		Reason:         "hard-failure",
		IssuedAt:       now,
		RenewedAt:      now,
		LeaseUntil:     now.Add(DefaultLeaseTTL),
	}
	plans := []dynamicconfig.ActionPlan{{
		Action: actionAssignSecondaryIP,
		Target: map[string]string{
			"address": address,
		},
		Parameters: map[string]string{
			bgpPathSigParam: "prefix=10.88.60.10/32;nextHops=10.99.0.3",
		},
	}}
	placement := PlacementDecision{
		SeizeHoldDown:      true,
		SeizeHoldDownUntil: now.Add(9 * time.Second),
	}

	state := recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, nil, map[string]bgpCaptureAssignment{address: assignment}, plans, placement, nil, nil, nil, nil, bgpCaptureTransitionState{}, now)
	events := listMobilityTransitionEvents(t, store)
	if len(events) != 1 {
		t.Fatalf("events after start = %d, want 1", len(events))
	}
	assertTransitionEvent(t, events[0], "seize-start", address, "aws-rr-a", "aws-rr-b", assignment.Generation)
	if got := events[0].Attributes["mobilityPathSig"]; got != "prefix=10.88.60.10/32;nextHops=10.99.0.3" {
		t.Fatalf("mobilityPathSig = %q", got)
	}
	if got := events[0].Attributes["holdDownRemainingSeconds.seize"]; got != "9" {
		t.Fatalf("holdDownRemainingSeconds.seize = %q, want 9", got)
	}

	state = recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, livenessMarkers, mobilityPrefixCommunities, nil, nil, state, now.Add(151*time.Second))
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 2 {
		t.Fatalf("events after dataplane complete = %d, want 2", len(events))
	}
	assertTransitionEvent(t, events[1], "seize-complete", address, "aws-rr-a", "aws-rr-b", assignment.Generation)
	assertExtractableTransitionCount(t, events, "seize-complete", 1)
	assertExtractableTransitionCount(t, events, "capture-confirmed", 0)
	completed := state.SeizeComplete
	if got := completed[address]; got != assignment.Generation {
		t.Fatalf("seize completion marker = %q, want %q", got, assignment.Generation)
	}
	state = recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, livenessMarkers, mobilityPrefixCommunities, []ownershipDecision{{
		Address:           address,
		Class:             ownershipClassConfirmedCapture,
		CaptureHolderNode: "aws-rr-b",
	}}, nil, state, now.Add(173*time.Second))
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 3 {
		t.Fatalf("events after provider confirmed = %d, want 3", len(events))
	}
	assertTransitionEvent(t, events[2], "capture-confirmed", address, "aws-rr-a", "aws-rr-b", assignment.Generation)
	assertExtractableTransitionCount(t, events, "seize-complete", 1)
	assertExtractableTransitionCount(t, events, "capture-confirmed", 1)
	completed = state.CaptureConfirmed
	if got := completed[address]; got != assignment.Generation {
		t.Fatalf("capture confirmation marker = %q, want %q", got, assignment.Generation)
	}

	state = recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, livenessMarkers, mobilityPrefixCommunities, []ownershipDecision{{
		Address:           address,
		Class:             ownershipClassConfirmedCapture,
		CaptureHolderNode: "aws-rr-b",
	}}, nil, state, now.Add(174*time.Second))
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 3 {
		t.Fatalf("events after duplicate transitions = %d, want 3", len(events))
	}
	_ = recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, nil, plans, PlacementDecision{}, nil, nil, nil, nil, state, now.Add(180*time.Second))
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 4 {
		t.Fatalf("events after yield = %d, want 4", len(events))
	}
	assertTransitionEvent(t, events[3], "yield", address, "aws-rr-b", "", assignment.Generation)
}

func TestRecordBGPCaptureAssignmentTransitionsCompletesProviderCaptureFromSucceededAssignJournal(t *testing.T) {
	now := time.Date(2026, 7, 4, 16, 0, 0, 0, time.UTC)
	executedAt := now.Add(-7 * time.Second)
	store := testStore(t, now)
	controller := Controller{Store: store}
	address := "10.88.60.17/32"
	self := memberPlanInfo{
		NodeRef: "aws-rr-b",
		Capture: api.MobilityMemberCapture{
			Type:        "provider-secondary-ip",
			ProviderRef: "aws-provider",
			NICRef:      "eni-b",
		},
	}
	assignment := activeCaptureAssignmentForTransitionTest(address, "aws-rr-b", "aws-rr-a", now)
	plans := []dynamicconfig.ActionPlan{assignSecondaryIPPlanForTransitionTest(address, "aws-provider", "eni-b", "aws-rr-b")}
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForTransitionTest(t, 89, actionAssignSecondaryIP, address, "aws-provider", "eni-b", "aws-rr-b", assignment.Generation, executedAt),
	}

	state := recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, journal, bgpCaptureTransitionState{}, now)
	events := listMobilityTransitionEvents(t, store)
	if len(events) != 1 {
		t.Fatalf("events after provider accepted = %d, want 1", len(events))
	}
	assertTransitionEvent(t, events[0], "seize-complete", address, "aws-rr-a", "aws-rr-b", "provider-capture/89")
	if got := events[0].Attributes["issuedAt"]; got != executedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("issuedAt = %q, want journal ExecutedAt %s", got, executedAt.UTC().Format(time.RFC3339Nano))
	}
	completed := state.SeizeComplete
	if got := completed[address]; got != "provider-capture/89" {
		t.Fatalf("seize completion marker = %q, want provider-capture/89", got)
	}

	_ = recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, journal, state, now.Add(time.Second))
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 1 {
		t.Fatalf("events after duplicate provider accepted = %d, want 1", len(events))
	}
}

func TestRecordBGPCaptureAssignmentTransitionsDoesNotCompleteProviderCaptureAfterLatestUnassign(t *testing.T) {
	now := time.Date(2026, 7, 4, 16, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Store: store}
	address := "10.88.60.17/32"
	self := memberPlanInfo{
		NodeRef: "aws-rr-b",
		Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "aws-provider", NICRef: "eni-b"},
	}
	assignment := activeCaptureAssignmentForTransitionTest(address, "aws-rr-b", "aws-rr-a", now)
	plans := []dynamicconfig.ActionPlan{assignSecondaryIPPlanForTransitionTest(address, "aws-provider", "eni-b", "aws-rr-b")}
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForTransitionTest(t, 89, actionAssignSecondaryIP, address, "aws-provider", "eni-b", "aws-rr-b", assignment.Generation, now.Add(-10*time.Second)),
		providerCaptureActionRecordForTransitionTest(t, 90, actionUnassignSecondaryIP, address, "aws-provider", "eni-b", "aws-rr-b", assignment.Generation, now.Add(-time.Second)),
	}

	_ = recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, journal, bgpCaptureTransitionState{}, now)
	events := listMobilityTransitionEvents(t, store)
	if len(events) != 0 {
		t.Fatalf("events after latest unassign = %d, want 0 (%#v)", len(events), events)
	}
}

func TestRecordBGPCaptureAssignmentTransitionsDoesNotCompleteProviderCaptureForStaleAssignmentGeneration(t *testing.T) {
	now := time.Date(2026, 7, 4, 16, 7, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Store: store}
	address := "10.88.60.17/32"
	self := memberPlanInfo{
		NodeRef: "aws-rr-b",
		Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "aws-provider", NICRef: "eni-b"},
	}
	assignment := activeCaptureAssignmentForTransitionTest(address, "aws-rr-b", "aws-rr-a", now)
	plans := []dynamicconfig.ActionPlan{assignSecondaryIPPlanForTransitionTest(address, "aws-provider", "eni-b", "aws-rr-b")}
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForTransitionTest(t, 89, actionAssignSecondaryIP, address, "aws-provider", "eni-b", "aws-rr-b", "stale-generation", now.Add(-time.Second)),
	}

	_ = recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, journal, bgpCaptureTransitionState{}, now)
	events := listMobilityTransitionEvents(t, store)
	if len(events) != 0 {
		t.Fatalf("events for stale assignment generation = %d, want 0 (%#v)", len(events), events)
	}
}

func TestRecordBGPCaptureAssignmentTransitionsDoesNotCompleteProviderCaptureWithoutJournalFact(t *testing.T) {
	now := time.Date(2026, 7, 4, 16, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Store: store}
	address := "10.88.60.17/32"
	self := memberPlanInfo{
		NodeRef: "aws-rr-b",
		Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "aws-provider", NICRef: "eni-b"},
	}
	assignment := activeCaptureAssignmentForTransitionTest(address, "aws-rr-b", "aws-rr-a", now)
	plans := []dynamicconfig.ActionPlan{assignSecondaryIPPlanForTransitionTest(address, "aws-provider", "eni-b", "aws-rr-b")}

	_ = recordBGPCaptureAssignmentTransitionsForTest(t, controller, "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, nil, bgpCaptureTransitionState{}, now)
	events := listMobilityTransitionEvents(t, store)
	if len(events) != 0 {
		t.Fatalf("events without journal fact = %d, want 0 (%#v)", len(events), events)
	}
}

func recordBGPCaptureAssignmentTransitionsForTest(t *testing.T, controller Controller, poolName string, self memberPlanInfo, previous, current map[string]bgpCaptureAssignment, plans []dynamicconfig.ActionPlan, placement PlacementDecision, livenessMarkers map[string]string, mobilityPrefixCommunities map[string][]string, decisions []ownershipDecision, actionJournal []routerstate.ActionExecutionRecord, previousState bgpCaptureTransitionState, now time.Time) bgpCaptureTransitionState {
	t.Helper()
	history := newProviderActionHistoryWithRevision(nil, actionJournal, "")
	state := poolReconcileState{Runtime: PoolRuntimeSnapshot{
		Pool:     NormalizedMobilityPool{Name: poolName, Self: self},
		BGP:      BGPSnapshot{LivenessMarkers: livenessMarkers, PrefixCommunities: mobilityPrefixCommunities},
		Provider: ProviderSnapshot{ActionHistory: history},
		Previous: PreviousPoolState{Transitions: previousState},
		Now:      now,
	}}
	next, err := controller.recordBGPCaptureAssignmentTransitions(
		context.Background(),
		state,
		PoolPlan{Placement: placement, Addresses: decisions},
		captureTransitionEffects{Previous: previous, Current: current, ActionPlans: plans},
	)
	if err != nil {
		t.Fatalf("record BGP capture assignment transitions: %v", err)
	}
	return next
}

func listMobilityTransitionEvents(t *testing.T, store *routerstate.SQLiteStore) []routerstate.StoredEvent {
	t.Helper()
	events, err := store.ListEvents(routerstate.EventQuery{Topic: mobilityHolderTransitionTopic, Limit: 20})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	return events
}

func assertTransitionEvent(t *testing.T, event routerstate.StoredEvent, kind, address, fromNode, toNode, generation string) {
	t.Helper()
	if event.Topic != mobilityHolderTransitionTopic {
		t.Fatalf("topic = %q, want %q", event.Topic, mobilityHolderTransitionTopic)
	}
	attrs := event.Attributes
	for key, want := range map[string]string{
		"transitionKind":       kind,
		"address":              address,
		"fromNode":             fromNode,
		"toNode":               toNode,
		"assignmentGeneration": generation,
	} {
		if got := attrs[key]; got != want {
			t.Fatalf("%s = %q, want %q (attrs=%#v)", key, got, want, attrs)
		}
	}
	if attrs["timestamp"] == "" {
		t.Fatalf("timestamp is empty: %#v", attrs)
	}
}

func assertExtractableTransitionCount(t *testing.T, events []routerstate.StoredEvent, kind string, want int) {
	t.Helper()
	got := 0
	for _, event := range events {
		if event.Attributes["transitionKind"] == kind && event.Attributes["address"] != "" && event.Attributes["timestamp"] != "" {
			got++
		}
	}
	if got != want {
		t.Fatalf("extractable %s events = %d, want %d", kind, got, want)
	}
}

func activeCaptureAssignmentForTransitionTest(address, holder, previousHolder string, now time.Time) bgpCaptureAssignment {
	return bgpCaptureAssignment{
		Address:        address,
		Phase:          "Active",
		Generation:     "group-a/7",
		DesiredHolder:  holder,
		PreviousHolder: previousHolder,
		Reason:         "placement-election",
		IssuedAt:       now.Add(-time.Minute),
		RenewedAt:      now,
		LeaseUntil:     now.Add(DefaultLeaseTTL),
	}
}

func assignSecondaryIPPlanForTransitionTest(address, providerRef, targetRef, holder string) dynamicconfig.ActionPlan {
	return dynamicconfig.ActionPlan{
		Provider:       "aws",
		ProviderRef:    providerRef,
		Action:         actionAssignSecondaryIP,
		Target:         map[string]string{"address": address, "providerRef": providerRef, "nicRef": targetRef},
		Parameters:     map[string]string{captureParamHolder: holder, bgpPathSigParam: "prefix=" + address + ";nextHops=10.99.0.3"},
		IdempotencyKey: "assign-" + safeName(address),
	}
}

func providerCaptureActionRecordForTransitionTest(t *testing.T, id int64, action, address, providerRef, targetRef, holder, assignmentGeneration string, at time.Time) routerstate.ActionExecutionRecord {
	t.Helper()
	target, err := json.Marshal(map[string]string{"address": address, "providerRef": providerRef, "nicRef": targetRef})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	params, err := json.Marshal(map[string]string{captureParamHolder: holder, bgpPathSigParam: "prefix=" + address + ";nextHops=10.99.0.3", captureAssignmentGenerationParam: assignmentGeneration})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return routerstate.ActionExecutionRecord{
		ID:             id,
		IdempotencyKey: action + "-" + safeName(address),
		Provider:       "aws",
		ProviderRef:    providerRef,
		Action:         action,
		TargetJSON:     string(target),
		ParametersJSON: string(params),
		Status:         routerstate.ActionSucceeded,
		ExecutedAt:     at.UTC(),
		UpdatedAt:      at.UTC(),
	}
}

func transitionEventsByKindAddress(events []routerstate.StoredEvent, kind string) map[string]routerstate.StoredEvent {
	out := map[string]routerstate.StoredEvent{}
	for _, event := range events {
		if statusvalue.Text(event.Attributes["transitionKind"]) != kind {
			continue
		}
		address := statusvalue.Text(event.Attributes["address"])
		if address == "" {
			continue
		}
		out[address] = event
	}
	return out
}

func extractTransitionDurationsByAddress(t *testing.T, events []routerstate.StoredEvent) map[string]map[string]time.Duration {
	t.Helper()
	out := map[string]map[string]time.Duration{}
	for _, event := range events {
		kind := statusvalue.Text(event.Attributes["transitionKind"])
		address := statusvalue.Text(event.Attributes["address"])
		timestamp := statusvalue.Text(event.Attributes["timestamp"])
		issuedAt := statusvalue.Text(event.Attributes["issuedAt"])
		if kind == "" || address == "" || timestamp == "" || issuedAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			t.Fatalf("parse timestamp %q: %v", timestamp, err)
		}
		issued, err := time.Parse(time.RFC3339Nano, issuedAt)
		if err != nil {
			t.Fatalf("parse issuedAt %q: %v", issuedAt, err)
		}
		if out[kind] == nil {
			out[kind] = map[string]time.Duration{}
		}
		out[kind][address] = at.Sub(issued)
	}
	return out
}
