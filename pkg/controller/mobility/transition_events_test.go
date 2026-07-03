// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRecordBGPCaptureAssignmentTransitionsEmitsMachineReadableSequence(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := Controller{Store: store}
	address := "10.88.60.10/32"
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
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", "aws-rr-b", nil, map[string]bgpCaptureAssignment{address: assignment}, plans, placement, nil, nil, nextStatus, now); err != nil {
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

	completeStatus := map[string]any{}
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", "aws-rr-b", map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, []ownershipDecision{{
		Address:           address,
		Class:             ownershipClassConfirmedCapture,
		CaptureHolderNode: "aws-rr-b",
	}}, nextStatus, completeStatus, now.Add(151*time.Second)); err != nil {
		t.Fatalf("record complete transition: %v", err)
	}
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 2 {
		t.Fatalf("events after complete = %d, want 2", len(events))
	}
	assertTransitionEvent(t, events[1], "seize-complete", address, "aws-rr-a", "aws-rr-b", assignment.Generation)
	completed := bgpCaptureTransitionCompletedFromStatus(completeStatus)
	if got := completed[address]; got != assignment.Generation {
		t.Fatalf("completion marker = %q, want %q", got, assignment.Generation)
	}

	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", "aws-rr-b", map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, []ownershipDecision{{
		Address:           address,
		Class:             ownershipClassConfirmedCapture,
		CaptureHolderNode: "aws-rr-b",
	}}, completeStatus, map[string]any{}, now.Add(152*time.Second)); err != nil {
		t.Fatalf("record duplicate complete transition: %v", err)
	}
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 2 {
		t.Fatalf("events after duplicate complete = %d, want 2", len(events))
	}

	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", "aws-rr-b", map[string]bgpCaptureAssignment{address: assignment}, nil, plans, PlacementDecision{}, nil, completeStatus, map[string]any{}, now.Add(180*time.Second)); err != nil {
		t.Fatalf("record yield transition: %v", err)
	}
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 3 {
		t.Fatalf("events after yield = %d, want 3", len(events))
	}
	assertTransitionEvent(t, events[2], "yield", address, "aws-rr-b", "", assignment.Generation)
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
