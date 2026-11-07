// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"net/netip"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestUncertainProviderCaptureOperationsRemainProtectiveWithoutClaimingSuccess(t *testing.T) {
	for _, action := range []string{actionAssignSecondaryIP, actionUnassignSecondaryIP} {
		for _, status := range []string{routerstate.ActionRunning, routerstate.ActionOutcomeUnknown, routerstate.ActionRollingBack, routerstate.ActionRollbackFailed, routerstate.ActionRollbackUnknown} {
			t.Run(action+"/"+status, func(t *testing.T) {
				now := time.Now().UTC()
				store := testStore(t, now)
				record := routerstate.ActionExecutionRecord{IdempotencyKey: "capture", Provider: "aws", ProviderRef: "aws-provider", Action: action, Target: map[string]string{"address": "10.88.60.10/32", "nicRef": "eni-a", "providerRef": "aws-provider"}, Parameters: map[string]string{captureParamHolder: "self"}, Undo: &api.ProviderActionUndo{Action: actionUnassignSecondaryIP}}
				if _, err := store.ImportAction(record); err != nil {
					t.Fatal(err)
				}
				row, found, err := store.GetActionByIdempotencyKey(record.IdempotencyKey)
				if err != nil || !found {
					t.Fatalf("fixture row: %v %v", found, err)
				}
				if err := store.ApproveAction(row.ID, "operator", now); err != nil {
					t.Fatal(err)
				}
				if claimed, err := store.BeginActionExecution(row.ID, now); err != nil || !claimed {
					t.Fatalf("fixture claim: %v %v", claimed, err)
				}
				if status == routerstate.ActionOutcomeUnknown {
					if err := store.MarkActionResult(row.ID, status, "", "lost reply", nil, now); err != nil {
						t.Fatal(err)
					}
				}
				if providerActionRollbackState(status) {
					if err := store.MarkActionResult(row.ID, routerstate.ActionSucceeded, "", "", nil, now); err != nil {
						t.Fatal(err)
					}
					if claimed, err := store.BeginActionRollback(row.ID, now.Add(time.Second)); err != nil || !claimed {
						t.Fatalf("undo claim: %v %v", claimed, err)
					}
					if status != routerstate.ActionRollingBack {
						if err := store.MarkActionRollbackResult(row.ID, status, "", "undo failed or reply lost", now.Add(2*time.Second)); err != nil {
							t.Fatal(err)
						}
					}
				}
				rows, err := store.ListActions(routerstate.ActionExecutionFilter{})
				if err != nil {
					t.Fatal(err)
				}
				history := newProviderActionHistoryWithRevision(nil, rows, "")
				self := memberPlanInfo{NodeRef: "self", Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "aws-provider", NICRef: "eni-a"}}
				confirmed, protective := captureStatesForSelf(self, history, map[string]bool{"10.88.60.10/32": true}, true, now.Add(time.Hour))
				if len(confirmed) != 0 || protective["10.88.60.10/32"].Succeeded {
					t.Fatalf("uncertain operation claimed confirmed ownership: confirmed=%+v protective=%+v", confirmed, protective)
				}
				if _, found := protective["10.88.60.10/32"]; !found {
					t.Fatalf("uncertain %s did not retain capture protection", action)
				}
				pool := NormalizedMobilityPool{SelfNode: "self", Self: self}
				traps := discoveryCurrentTrapAddresses(history, pool, "aws-provider", netip.MustParsePrefix("10.88.60.0/24"))
				if !traps["10.88.60.10/32"] {
					t.Fatal("uncertain capture was rediscoverable as ordinary home ownership")
				}
				if shouldAllowBGPTrapReassignment(self, "10.88.60.10/32", history, map[string]bool{}, true, now.Add(time.Hour)) {
					t.Fatal("uncertain operation authorized recapture")
				}
			})
		}
	}
}

func TestCompletedCaptureUndoInvalidatesOldSuccessAndRequiresFreshObservationForReverseAssign(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	plan := dynamicconfig.ActionPlan{IdempotencyKey: "assign-key", ProviderRef: "aws-provider", Action: actionAssignSecondaryIP, Target: map[string]string{"address": "10.88.60.10/32", "nicRef": "eni-a", "providerRef": "aws-provider"}, Parameters: map[string]string{captureParamHolder: "self", captureAssignmentGenerationParam: "old-generation", captureAssignmentDesiredHolderParam: "self", captureAssignmentLeaseUntilParam: now.Add(time.Hour).Format(time.RFC3339Nano)}}
	row := routerstate.ActionExecutionRecord{ID: 1, IdempotencyKey: plan.IdempotencyKey, ProviderRef: plan.ProviderRef, Action: plan.Action, Target: plan.Target, Parameters: plan.Parameters, Undo: &api.ProviderActionUndo{Action: actionUnassignSecondaryIP}, Status: routerstate.ActionRolledBack, ExecutedAt: now, UpdatedAt: now.Add(time.Minute)}
	history := newProviderActionHistoryWithRevision([]dynamicconfig.ActionPlan{plan}, []routerstate.ActionExecutionRecord{row}, "revision")
	self := memberPlanInfo{NodeRef: "self", Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "aws-provider", NICRef: "eni-a"}}
	confirmed, protective := captureStatesForSelf(self, history, map[string]bool{"10.88.60.10/32": true}, true, now.Add(2*time.Minute))
	if len(confirmed) != 0 || len(protective) != 0 {
		t.Fatalf("rolled back assign retained success/protection: %+v %+v", confirmed, protective)
	}
	if _, _, found := history.priorCaptureAssignment("10.88.60.10/32"); found {
		t.Fatal("completed undo reused old assignment fence")
	}
	if _, found := history.releaseSourceFor(self, "10.88.60.10/32"); found {
		t.Fatal("completed undo was reused as a successful assign release source")
	}
	if traps := discoveryCurrentTrapAddresses(history, NormalizedMobilityPool{SelfNode: "self", Self: self}, "aws-provider", netip.MustParsePrefix("10.88.60.0/24")); len(traps) != 0 {
		t.Fatalf("completed undo remained pending discovery trap: %+v", traps)
	}
	// Undoing a successful unassign really assigns again, but observations from
	// before undo completion cannot confirm the new capture.
	row.Action = actionUnassignSecondaryIP
	row.Undo = &api.ProviderActionUndo{Action: actionAssignSecondaryIP}
	history = newProviderActionHistoryWithRevision(nil, []routerstate.ActionExecutionRecord{row}, "")
	confirmed, _ = captureStatesForSelf(self, history, map[string]bool{"10.88.60.10/32": true}, true, now.Add(30*time.Second))
	if len(confirmed) != 0 {
		t.Fatal("pre-undo inventory confirmed reverse assignment")
	}
	confirmed, _ = captureStatesForSelf(self, history, map[string]bool{"10.88.60.10/32": true}, true, row.UpdatedAt.Add(time.Second))
	if !confirmed["10.88.60.10/32"].Succeeded {
		t.Fatal("fresh observed reverse assignment not recognized")
	}
}
