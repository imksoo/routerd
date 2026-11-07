// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"github.com/imksoo/routerd/pkg/api"
	"reflect"
	"testing"
	"time"
)

func sampleActionRecord(key string) ActionExecutionRecord {
	return ActionExecutionRecord{
		IdempotencyKey: key,
		Source:         "cloudedge/claim-10.88.60.9",
		Provider:       "aws",
		ProviderRef:    "aws-prod",
		Action:         "assign-secondary-ip",
		Target:         map[string]string{"address": "10.88.60.9/32", "nicRef": "eni-1"},
		Parameters:     map[string]string{"region": "ap-northeast-1"},
		Undo:           &api.ProviderActionUndo{Action: "unassign-secondary-ip"},
		RiskLevel:      "medium",
	}
}

func TestImportActionDedup(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	inserted, err := store.ImportAction(sampleActionRecord("key-1"))
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if !inserted {
		t.Fatalf("first import should report inserted=true")
	}

	// Same idempotency key → no duplicate row, inserted=false.
	inserted, err = store.ImportAction(sampleActionRecord("key-1"))
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if inserted {
		t.Fatalf("second import of same key should report inserted=false")
	}

	all, err := store.ListActions(ActionExecutionFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 row after dedup, got %d", len(all))
	}
	if all[0].Status != ActionPending {
		t.Fatalf("imported action should be pending, got %q", all[0].Status)
	}
}

func TestImportActionRequeuesFailedDesiredAction(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(sampleActionRecord("retry-key")); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, ok, err := store.GetActionByIdempotencyKey("retry-key")
	if err != nil || !ok {
		t.Fatalf("get imported action: ok=%v err=%v", ok, err)
	}
	if err := store.ApproveAction(rec.ID, "policy:auto-approve", time.Time{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	claimed, err := store.BeginActionExecution(rec.ID, time.Time{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if !claimed {
		t.Fatal("action was not claimed")
	}
	if err := store.MarkActionResult(rec.ID, ActionFailed, "assign failed", "UnauthorizedOperation", map[string]string{"attempt": "1"}, time.Time{}); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	requeued := sampleActionRecord("retry-key")
	requeued.Target = map[string]string{"address": "10.88.60.9/32", "nicRef": "eni-2"}
	inserted, err := store.ImportAction(requeued)
	if err != nil {
		t.Fatalf("reimport failed action: %v", err)
	}
	if !inserted {
		t.Fatalf("reimporting failed desired action should report inserted/updated=true")
	}

	got, ok, err := store.GetActionByIdempotencyKey("retry-key")
	if err != nil || !ok {
		t.Fatalf("get requeued action: ok=%v err=%v", ok, err)
	}
	if got.ID != rec.ID {
		t.Fatalf("requeue should preserve journal row id: got %d want %d", got.ID, rec.ID)
	}
	if got.Status != ActionPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
	if !reflect.DeepEqual(got.Target, requeued.Target) {
		t.Fatalf("target was not refreshed: %v", got.Target)
	}
	if got.ApprovedBy != "" || !got.ApprovedAt.IsZero() || !got.ExecutedAt.IsZero() || got.ResultMessage != "" || got.Error != "" || len(got.Observed) != 0 {
		t.Fatalf("terminal execution fields were not cleared: %+v", got)
	}
}

func TestImportActionDoesNotRequeueSucceededAction(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(sampleActionRecord("done-key")); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, _, _ := store.GetActionByIdempotencyKey("done-key")
	if err := store.ApproveAction(rec.ID, "policy:auto-approve", time.Time{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	claimApprovedAction(t, store, rec.ID)
	if err := store.MarkActionResult(rec.ID, ActionSucceeded, "applied", "", nil, time.Time{}); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}

	inserted, err := store.ImportAction(sampleActionRecord("done-key"))
	if err != nil {
		t.Fatalf("reimport succeeded action: %v", err)
	}
	if inserted {
		t.Fatalf("reimporting succeeded action should remain a duplicate")
	}
	got, _, _ := store.GetActionByIdempotencyKey("done-key")
	if got.Status != ActionSucceeded {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}
}

func TestImportActionRequiresFields(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(ActionExecutionRecord{Provider: "aws", Action: "assign-secondary-ip"}); err == nil {
		t.Fatalf("import without idempotencyKey should error")
	}
	if _, err := store.ImportAction(ActionExecutionRecord{IdempotencyKey: "k", Action: "assign-secondary-ip"}); err == nil {
		t.Fatalf("import without provider should error")
	}
	if _, err := store.ImportAction(ActionExecutionRecord{IdempotencyKey: "k", Provider: "aws"}); err == nil {
		t.Fatalf("import without action should error")
	}
}

func TestActionApproveTransition(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(sampleActionRecord("key-2")); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, ok, err := store.GetActionByIdempotencyKey("key-2")
	if err != nil || !ok {
		t.Fatalf("get by key: ok=%v err=%v", ok, err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := store.ApproveAction(rec.ID, "operator@example.com", now); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Approving again (now approved, not pending) must fail.
	if err := store.ApproveAction(rec.ID, "operator@example.com", now); err == nil {
		t.Fatalf("approving an already-approved action should error")
	}

	got, ok, err := store.GetActionByID(rec.ID)
	if err != nil || !ok {
		t.Fatalf("get by id: ok=%v err=%v", ok, err)
	}
	if got.Status != ActionApproved {
		t.Fatalf("expected approved, got %q", got.Status)
	}
	if got.ApprovedBy != "operator@example.com" {
		t.Fatalf("approvedBy not recorded: %q", got.ApprovedBy)
	}
	if got.ApprovedAt.IsZero() {
		t.Fatalf("approvedAt not recorded")
	}
}

func TestActionResultTransitions(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	cases := []struct {
		key    string
		status string
	}{
		{"ok-succeeded", ActionSucceeded},
		{"ok-failed", ActionFailed},
		{"ok-skipped", ActionSkipped},
	}
	for _, c := range cases {
		if _, err := store.ImportAction(sampleActionRecord(c.key)); err != nil {
			t.Fatalf("%s import: %v", c.key, err)
		}
		rec, _, err := store.GetActionByIdempotencyKey(c.key)
		if err != nil {
			t.Fatalf("%s get: %v", c.key, err)
		}

		// Cannot record result before approval.
		if err := store.MarkActionResult(rec.ID, c.status, "msg", "", nil, time.Time{}); err == nil {
			t.Fatalf("%s: marking result on pending action should error", c.key)
		}

		if err := store.ApproveAction(rec.ID, "op", time.Time{}); err != nil {
			t.Fatalf("%s approve: %v", c.key, err)
		}
		claimApprovedAction(t, store, rec.ID)
		if err := store.MarkActionResult(rec.ID, c.status, "done", "boom", nil, time.Time{}); err != nil {
			t.Fatalf("%s mark result: %v", c.key, err)
		}
		got, _, err := store.GetActionByID(rec.ID)
		if err != nil {
			t.Fatalf("%s reget: %v", c.key, err)
		}
		if got.Status != c.status {
			t.Fatalf("%s expected %q, got %q", c.key, c.status, got.Status)
		}
		if got.ExecutedAt.IsZero() {
			t.Fatalf("%s executedAt not recorded", c.key)
		}
	}

	// Invalid result status is rejected.
	if _, err := store.ImportAction(sampleActionRecord("bad-status")); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, _, _ := store.GetActionByIdempotencyKey("bad-status")
	_ = store.ApproveAction(rec.ID, "op", time.Time{})
	if err := store.MarkActionResult(rec.ID, "pending", "", "", nil, time.Time{}); err == nil {
		t.Fatalf("invalid result status should error")
	}
}

func TestBeginActionExecutionClaimsApprovedOnce(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(sampleActionRecord("running-claim")); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, _, err := store.GetActionByIdempotencyKey("running-claim")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok, err := store.BeginActionExecution(rec.ID, time.Time{}); err != nil || ok {
		t.Fatalf("pending BeginActionExecution ok=%v err=%v, want false nil", ok, err)
	}
	if err := store.ApproveAction(rec.ID, "op", time.Time{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ok, err := store.BeginActionExecution(rec.ID, time.Time{})
	if err != nil {
		t.Fatalf("BeginActionExecution: %v", err)
	}
	if !ok {
		t.Fatal("approved action was not claimed")
	}
	ok, err = store.BeginActionExecution(rec.ID, time.Time{})
	if err != nil {
		t.Fatalf("second BeginActionExecution: %v", err)
	}
	if ok {
		t.Fatal("second BeginActionExecution claimed the same action")
	}
	got, _, err := store.GetActionByID(rec.ID)
	if err != nil {
		t.Fatalf("get after claim: %v", err)
	}
	if got.Status != ActionRunning {
		t.Fatalf("status = %q, want running", got.Status)
	}
	if err := store.MarkActionResult(rec.ID, ActionSucceeded, "done", "", nil, time.Time{}); err != nil {
		t.Fatalf("MarkActionResult running: %v", err)
	}
}

func TestActionResultObservedRoundTrip(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(sampleActionRecord("obs-1")); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, _, err := store.GetActionByIdempotencyKey("obs-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := store.ApproveAction(rec.ID, "op", time.Time{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	observed := map[string]string{"priorSourceDestCheck": "true", "nicState": "in-use"}
	claimApprovedAction(t, store, rec.ID)
	if err := store.MarkActionResult(rec.ID, ActionSucceeded, "applied", "", observed, time.Time{}); err != nil {
		t.Fatalf("mark result: %v", err)
	}

	got, _, err := store.GetActionByID(rec.ID)
	if err != nil {
		t.Fatalf("reget: %v", err)
	}
	if got.Observed["priorSourceDestCheck"] != "true" || got.Observed["nicState"] != "in-use" {
		t.Fatalf("observed not round-tripped: %+v", got.Observed)
	}

	// ListActions returns Observed too.
	rows, err := store.ListActions(ActionExecutionFilter{Status: ActionSucceeded})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Observed["priorSourceDestCheck"] != "true" {
		t.Fatalf("list did not return observed: %+v", rows)
	}
}

func importApproveAndClaimAt(t *testing.T, store *SQLiteStore, key string, at time.Time) int64 {
	t.Helper()
	if _, err := store.ImportAction(sampleActionRecord(key)); err != nil {
		t.Fatalf("import %s: %v", key, err)
	}
	rec, _, err := store.GetActionByIdempotencyKey(key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	if err := store.ApproveAction(rec.ID, "op", at.Add(-time.Second)); err != nil {
		t.Fatalf("approve %s: %v", key, err)
	}
	ok, err := store.BeginActionExecution(rec.ID, at)
	if err != nil {
		t.Fatalf("begin %s: %v", key, err)
	}
	if !ok {
		t.Fatalf("begin %s did not claim", key)
	}
	return rec.ID
}

func TestActionResultEmptyObservedLeavesNull(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(sampleActionRecord("obs-empty")); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, _, _ := store.GetActionByIdempotencyKey("obs-empty")
	if err := store.ApproveAction(rec.ID, "op", time.Time{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// nil observed -> observed_json stays NULL, Observed decodes to nil/empty.
	claimApprovedAction(t, store, rec.ID)
	if err := store.MarkActionResult(rec.ID, ActionSucceeded, "applied", "", nil, time.Time{}); err != nil {
		t.Fatalf("mark result: %v", err)
	}
	got, _, _ := store.GetActionByID(rec.ID)
	if len(got.Observed) != 0 {
		t.Fatalf("expected empty Observed, got %+v", got.Observed)
	}
}

func TestActionRolledBack(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(sampleActionRecord("rb-1")); err != nil {
		t.Fatalf("import: %v", err)
	}
	rec, _, _ := store.GetActionByIdempotencyKey("rb-1")

	// Cannot roll back before it has succeeded.
	if err := store.MarkActionRollbackResult(rec.ID, ActionRolledBack, "undo", "", time.Time{}); err == nil {
		t.Fatalf("rollback of non-succeeded action should error")
	}

	if err := store.ApproveAction(rec.ID, "op", time.Time{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	claimApprovedAction(t, store, rec.ID)
	if err := store.MarkActionResult(rec.ID, ActionSucceeded, "applied", "", nil, time.Time{}); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	if claimed, err := store.BeginActionRollback(rec.ID, time.Time{}); err != nil || !claimed {
		t.Fatalf("claim undo: %v %v", claimed, err)
	}
	if err := store.MarkActionRollbackResult(rec.ID, ActionRolledBack, "undo applied", "", time.Time{}); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, _, _ := store.GetActionByID(rec.ID)
	if got.Status != ActionRolledBack {
		t.Fatalf("expected rolledBack, got %q", got.Status)
	}
	if got.ResultMessage != "undo applied" {
		t.Fatalf("rollback message not recorded: %q", got.ResultMessage)
	}
}

func TestListActionsFilterAndRoundTrip(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, err := store.ImportAction(sampleActionRecord("aws-1")); err != nil {
		t.Fatalf("import: %v", err)
	}
	azure := sampleActionRecord("azure-1")
	azure.Provider = "azure"
	if _, err := store.ImportAction(azure); err != nil {
		t.Fatalf("import: %v", err)
	}

	awsRows, err := store.ListActions(ActionExecutionFilter{Provider: "aws"})
	if err != nil {
		t.Fatalf("list aws: %v", err)
	}
	if len(awsRows) != 1 || awsRows[0].Provider != "aws" {
		t.Fatalf("provider filter failed: %+v", awsRows)
	}

	pendingRows, err := store.ListActions(ActionExecutionFilter{Status: ActionPending})
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pendingRows) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pendingRows))
	}

	// Round-trip: the SQL boundary preserves typed plan fields.
	rec, ok, err := store.GetActionByIdempotencyKey("aws-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if len(rec.Target) == 0 || len(rec.Parameters) == 0 || rec.Undo == nil {
		t.Fatalf("plan fields not round-tripped: %+v", rec)
	}
	if rec.RiskLevel != "medium" || rec.ProviderRef != "aws-prod" {
		t.Fatalf("scalar fields not round-tripped: %+v", rec)
	}
}

func TestGetActionMissing(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	if _, ok, err := store.GetActionByID(999); err != nil || ok {
		t.Fatalf("missing id should return ok=false, got ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetActionByIdempotencyKey("nope"); err != nil || ok {
		t.Fatalf("missing key should return ok=false, got ok=%v err=%v", ok, err)
	}
}

func claimApprovedAction(t *testing.T, store *SQLiteStore, id int64) {
	t.Helper()
	if claimed, err := store.BeginActionExecution(id, time.Time{}); err != nil || !claimed {
		t.Fatalf("claim approved action %d: %v %v", id, claimed, err)
	}
}
