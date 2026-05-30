// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"path/filepath"
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
		TargetJSON:     `{"address":"10.88.60.9/32","nicRef":"eni-1"}`,
		ParametersJSON: `{"region":"ap-northeast-1"}`,
		UndoJSON:       `{"action":"unassign-secondary-ip"}`,
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
		if err := store.MarkActionResult(rec.ID, c.status, "msg", "", time.Time{}); err == nil {
			t.Fatalf("%s: marking result on pending action should error", c.key)
		}

		if err := store.ApproveAction(rec.ID, "op", time.Time{}); err != nil {
			t.Fatalf("%s approve: %v", c.key, err)
		}
		if err := store.MarkActionResult(rec.ID, c.status, "done", "boom", time.Time{}); err != nil {
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
	if err := store.MarkActionResult(rec.ID, "pending", "", "", time.Time{}); err == nil {
		t.Fatalf("invalid result status should error")
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
	if err := store.MarkActionRolledBack(rec.ID, "undo", time.Time{}); err == nil {
		t.Fatalf("rollback of non-succeeded action should error")
	}

	if err := store.ApproveAction(rec.ID, "op", time.Time{}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := store.MarkActionResult(rec.ID, ActionSucceeded, "applied", "", time.Time{}); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	if err := store.MarkActionRolledBack(rec.ID, "undo applied", time.Time{}); err != nil {
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

	// Round-trip: stored JSON fields come back intact.
	rec, ok, err := store.GetActionByIdempotencyKey("aws-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if rec.TargetJSON == "" || rec.ParametersJSON == "" || rec.UndoJSON == "" {
		t.Fatalf("JSON fields not round-tripped: %+v", rec)
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

// TestActionExecutionBackwardCompat verifies a database that predates the
// action_executions table gets it created on open (the ensure*Columns migration
// is backward compatible).
func TestActionExecutionBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Simulate an older database: drop the table, then reopen so the migration
	// re-creates it.
	if _, err := store.db.Exec(`DROP TABLE action_executions`); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	if _, err := reopened.ImportAction(sampleActionRecord("after-upgrade")); err != nil {
		t.Fatalf("import after upgrade: %v", err)
	}
	rows, err := reopened.ListActions(ActionExecutionFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after backward-compat upgrade, got %d", len(rows))
	}
}
