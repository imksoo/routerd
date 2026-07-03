// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"sort"
	"testing"
	"time"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRecordBGPCaptureAssignmentTransitionsEmitsMachineReadableSequence(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	cleaner := &fakeConntrackCleaner{}
	controller := Controller{Store: store, ConntrackCleaner: cleaner}
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
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, nil, map[string]bgpCaptureAssignment{address: assignment}, plans, placement, nil, nil, nil, nil, false, nextStatus, now); err != nil {
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
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, livenessMarkers, mobilityPrefixCommunities, nil, nextStatus, true, seizeCompleteStatus, now.Add(151*time.Second)); err != nil {
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
	if len(cleaner.addresses) != 1 || cleaner.addresses[0] != address {
		t.Fatalf("conntrack cleanup addresses = %#v, want %s", cleaner.addresses, address)
	}

	captureStatus := map[string]any{}
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, plans, PlacementDecision{}, livenessMarkers, mobilityPrefixCommunities, []ownershipDecision{{
		Address:           address,
		Class:             ownershipClassConfirmedCapture,
		CaptureHolderNode: "aws-rr-b",
		CaptureStrategy:   captureStrategySecondaryIP,
	}}, seizeCompleteStatus, true, captureStatus, now.Add(173*time.Second)); err != nil {
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
	}}, captureStatus, true, map[string]any{}, now.Add(174*time.Second)); err != nil {
		t.Fatalf("record duplicate transitions: %v", err)
	}
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 3 {
		t.Fatalf("events after duplicate transitions = %d, want 3", len(events))
	}
	if len(cleaner.addresses) != 1 {
		t.Fatalf("conntrack cleanup duplicated: %#v", cleaner.addresses)
	}

	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, nil, plans, PlacementDecision{}, nil, nil, nil, captureStatus, true, map[string]any{}, now.Add(180*time.Second)); err != nil {
		t.Fatalf("record yield transition: %v", err)
	}
	events = listMobilityTransitionEvents(t, store)
	if len(events) != 4 {
		t.Fatalf("events after yield = %d, want 4", len(events))
	}
	assertTransitionEvent(t, events[3], "yield", address, "aws-rr-b", "", assignment.Generation)
}

func TestRecordBGPCaptureAssignmentTransitionsSkipsConntrackCleanupWhenDisabled(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 20, 0, 0, time.UTC)
	store := testStore(t, now)
	cleaner := &fakeConntrackCleaner{}
	controller := Controller{Store: store, ConntrackCleaner: cleaner}
	address := "10.88.60.20/32"
	self := memberPlanInfo{NodeRef: "aws-rr-b"}
	assignment := bgpCaptureAssignment{Address: address, Phase: "Active", Generation: "group-a/8", DesiredHolder: "aws-rr-b"}
	livenessMarkers := map[string]string{bgpstate.MobilityNodeIdentityCommunity("aws-rr-b"): "10.99.0.12/32"}
	communities := map[string][]string{address: {bgpMobilityCommunityActiveHolder, bgpstate.MobilityNodeIdentityCommunity("aws-rr-b")}}

	status := map[string]any{}
	if err := controller.recordBGPCaptureAssignmentTransitions(context.Background(), "cloudedge", self, map[string]bgpCaptureAssignment{address: assignment}, map[string]bgpCaptureAssignment{address: assignment}, nil, PlacementDecision{}, livenessMarkers, communities, nil, nil, false, status, now); err != nil {
		t.Fatalf("record transition: %v", err)
	}
	if len(cleaner.addresses) != 0 {
		t.Fatalf("conntrack cleanup addresses = %#v, want none", cleaner.addresses)
	}
}

type fakeConntrackCleaner struct {
	addresses []string
	warnings  []string
}

func (f *fakeConntrackCleaner) CleanupAddress(ctx context.Context, address string) []string {
	f.addresses = append(f.addresses, address)
	return append([]string(nil), f.warnings...)
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
