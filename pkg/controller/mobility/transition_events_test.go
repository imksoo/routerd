// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

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
		Seq:            7,
		ClaimEpoch:     "group-a/7",
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

	nextStatus := map[string]any{}
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, nil, map[string]bgpCaptureAssignment{address: assignment}, plans, placement, nil, nil, nil, nil, nil, nextStatus, now); err != nil {
		t.Fatalf("record start transition: %v", err)
	}
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

	seizeCompleteStatus := map[string]any{}
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, livenessMarkers, mobilityPrefixCommunities, nil, nil, nextStatus, seizeCompleteStatus, now.Add(151*time.Second)); err != nil {
		t.Fatalf("record dataplane complete transition: %v", err)
	}
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 2 {
		t.Fatalf("events after dataplane complete = %d, want 2", len(events))
	}
	assertTransitionEvent(t, events[1], "seize-complete", address, "aws-rr-a", "aws-rr-b", assignment.Generation)
	assertExtractableTransitionCount(t, events, "seize-complete", 1)
	assertExtractableTransitionCount(t, events, "capture-confirmed", 0)
	completed := bgpCaptureTransitionCompletedByKindFromStatus(seizeCompleteStatus)
	if got := completed[bgpCaptureTransitionCompletedField][address]; got != assignment.Generation {
		t.Fatalf("seize completion marker = %q, want %q", got, assignment.Generation)
	}
	captureStatus := map[string]any{}
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, livenessMarkers, mobilityPrefixCommunities, []ownershipDecision{{
		Address:           address,
		Class:             ownershipClassConfirmedCapture,
		CaptureHolderNode: "aws-rr-b",
		CaptureStrategy:   captureStrategySecondaryIP,
	}}, nil, seizeCompleteStatus, captureStatus, now.Add(173*time.Second)); err != nil {
		t.Fatalf("record provider confirmed transition: %v", err)
	}
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 3 {
		t.Fatalf("events after provider confirmed = %d, want 3", len(events))
	}
	assertTransitionEvent(t, events[2], "capture-confirmed", address, "aws-rr-a", "aws-rr-b", assignment.Generation)
	if got := events[2].Attributes["captureStrategy"]; got != captureStrategySecondaryIP {
		t.Fatalf("captureStrategy = %q, want %q", got, captureStrategySecondaryIP)
	}
	assertExtractableTransitionCount(t, events, "seize-complete", 1)
	assertExtractableTransitionCount(t, events, "capture-confirmed", 1)
	completed = bgpCaptureTransitionCompletedByKindFromStatus(captureStatus)
	if got := completed[bgpCaptureConfirmedField][address]; got != assignment.Generation {
		t.Fatalf("capture confirmation marker = %q, want %q", got, assignment.Generation)
	}

	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, livenessMarkers, mobilityPrefixCommunities, []ownershipDecision{{
		Address:           address,
		Class:             ownershipClassConfirmedCapture,
		CaptureHolderNode: "aws-rr-b",
		CaptureStrategy:   captureStrategySecondaryIP,
	}}, nil, captureStatus, map[string]any{}, now.Add(174*time.Second)); err != nil {
		t.Fatalf("record duplicate transitions: %v", err)
	}
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 3 {
		t.Fatalf("events after duplicate transitions = %d, want 3", len(events))
	}
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, nil, plans, PlacementDecision{}, nil, nil, nil, nil, captureStatus, map[string]any{}, now.Add(180*time.Second)); err != nil {
		t.Fatalf("record yield transition: %v", err)
	}
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
		Capture: api.AddressCapture{
			Type:        "provider-secondary-ip",
			ProviderRef: "aws-provider",
			NICRef:      "eni-b",
		},
		CaptureTarget: map[string]string{"nicRef": "eni-b"},
	}
	assignment := activeCaptureAssignmentForTransitionTest(address, "aws-rr-b", "aws-rr-a", now)
	plans := []dynamicconfig.ActionPlan{assignSecondaryIPPlanForTransitionTest(address, "aws-provider", "eni-b", "aws-rr-b")}
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForTransitionTest(t, 89, actionAssignSecondaryIP, address, "aws-provider", "eni-b", "aws-rr-b", executedAt),
	}

	nextStatus := map[string]any{}
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, journal, map[string]any{}, nextStatus, now); err != nil {
		t.Fatalf("record provider accepted transition: %v", err)
	}
	events := listMobilityTransitionEvents(t, store)
	if len(events) != 1 {
		t.Fatalf("events after provider accepted = %d, want 1", len(events))
	}
	assertTransitionEvent(t, events[0], "seize-complete", address, "aws-rr-a", "aws-rr-b", "provider-capture/89")
	if got := events[0].Attributes["issuedAt"]; got != executedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("issuedAt = %q, want journal ExecutedAt %s", got, executedAt.UTC().Format(time.RFC3339Nano))
	}
	completed := bgpCaptureTransitionCompletedByKindFromStatus(nextStatus)
	if got := completed[bgpCaptureTransitionCompletedField][address]; got != "provider-capture/89" {
		t.Fatalf("seize completion marker = %q, want provider-capture/89", got)
	}

	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, journal, nextStatus, map[string]any{}, now.Add(time.Second)); err != nil {
		t.Fatalf("record duplicate provider accepted transition: %v", err)
	}
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
		NodeRef:       "aws-rr-b",
		Capture:       api.AddressCapture{Type: "provider-secondary-ip", ProviderRef: "aws-provider", NICRef: "eni-b"},
		CaptureTarget: map[string]string{"nicRef": "eni-b"},
	}
	assignment := activeCaptureAssignmentForTransitionTest(address, "aws-rr-b", "aws-rr-a", now)
	plans := []dynamicconfig.ActionPlan{assignSecondaryIPPlanForTransitionTest(address, "aws-provider", "eni-b", "aws-rr-b")}
	journal := []routerstate.ActionExecutionRecord{
		providerCaptureActionRecordForTransitionTest(t, 89, actionAssignSecondaryIP, address, "aws-provider", "eni-b", "aws-rr-b", now.Add(-10*time.Second)),
		providerCaptureActionRecordForTransitionTest(t, 90, actionUnassignSecondaryIP, address, "aws-provider", "eni-b", "aws-rr-b", now.Add(-time.Second)),
	}

	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, journal, map[string]any{}, map[string]any{}, now); err != nil {
		t.Fatalf("record provider accepted transition: %v", err)
	}
	events := listMobilityTransitionEvents(t, store)
	if len(events) != 0 {
		t.Fatalf("events after latest unassign = %d, want 0 (%#v)", len(events), events)
	}
}

func TestRecordBGPCaptureAssignmentTransitionsDoesNotCompleteProviderCaptureWithoutJournalFact(t *testing.T) {
	now := time.Date(2026, 7, 4, 16, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Store: store}
	address := "10.88.60.17/32"
	self := memberPlanInfo{
		NodeRef:       "aws-rr-b",
		Capture:       api.AddressCapture{Type: "provider-secondary-ip", ProviderRef: "aws-provider", NICRef: "eni-b"},
		CaptureTarget: map[string]string{"nicRef": "eni-b"},
	}
	assignment := activeCaptureAssignmentForTransitionTest(address, "aws-rr-b", "aws-rr-a", now)
	plans := []dynamicconfig.ActionPlan{assignSecondaryIPPlanForTransitionTest(address, "aws-provider", "eni-b", "aws-rr-b")}

	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, nil, nil, nil, nil, map[string]any{}, map[string]any{}, now); err != nil {
		t.Fatalf("record provider accepted transition: %v", err)
	}
	events := listMobilityTransitionEvents(t, store)
	if len(events) != 0 {
		t.Fatalf("events without journal fact = %d, want 0 (%#v)", len(events), events)
	}
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
		Seq:            7,
		ClaimEpoch:     "group-a/7",
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

func providerCaptureActionRecordForTransitionTest(t *testing.T, id int64, action, address, providerRef, targetRef, holder string, at time.Time) routerstate.ActionExecutionRecord {
	t.Helper()
	target, err := json.Marshal(map[string]string{"address": address, "providerRef": providerRef, "nicRef": targetRef})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	params, err := json.Marshal(map[string]string{captureParamHolder: holder, bgpPathSigParam: "prefix=" + address + ";nextHops=10.99.0.3"})
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
		if statusString(event.Attributes["transitionKind"]) != kind {
			continue
		}
		address := statusString(event.Attributes["address"])
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
		kind := statusString(event.Attributes["transitionKind"])
		address := statusString(event.Attributes["address"])
		timestamp := statusString(event.Attributes["timestamp"])
		issuedAt := statusString(event.Attributes["issuedAt"])
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
