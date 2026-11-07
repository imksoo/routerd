// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOperatorResolutionPreservesUndoFactsAndCannotStealLiveGuard(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	if _, err := store.ImportAction(sampleActionRecord("resolve-undo")); err != nil {
		t.Fatal(err)
	}
	rec, _, err := store.GetActionByIdempotencyKey("resolve-undo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.ApproveAction(rec.ID, "approval", now); err != nil {
		t.Fatal(err)
	}
	claimApprovedAction(t, store, rec.ID)
	if err := store.MarkActionResult(rec.ID, ActionSucceeded, "assigned", "", map[string]string{"priorSourceDestCheck": "true"}, now); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.BeginActionRollback(rec.ID, now); err != nil || !claimed {
		t.Fatalf("rollback claim %v %v", claimed, err)
	}
	if err := store.MarkActionRollbackResult(rec.ID, ActionRollbackUnknown, "lost reply", "connection closed", now); err != nil {
		t.Fatal(err)
	}
	guard, err := store.TryLockActionOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveActionOutcome(context.Background(), rec.ID, ActionRolledBack, "operator", "checked provider", now); !errors.Is(err, ErrActionOperationBusy) {
		t.Fatalf("live operation fence bypassed: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveActionOutcome(context.Background(), rec.ID, ActionSucceeded, "operator", "checked provider", now); err == nil {
		t.Fatal("rollback resolved as forward success")
	}
	if err := store.ResolveActionOutcome(context.Background(), rec.ID, ActionRolledBack, "operator", "", now); err == nil {
		t.Fatal("unverified resolution accepted")
	}
	if err := store.ResolveActionOutcome(context.Background(), rec.ID, ActionRolledBack, "operator", "checked provider", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	resolved, _, err := store.GetActionByID(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != ActionRolledBack || resolved.Observed["priorSourceDestCheck"] != "true" || !strings.Contains(resolved.ResultMessage, "checked provider") || !strings.Contains(resolved.ResultMessage, "connection closed") {
		t.Fatalf("resolution lost evidence: %#v", resolved)
	}
	if err := store.ResolveActionOutcome(context.Background(), rec.ID, ActionRollbackFailed, "operator", "overwrite", now); err == nil {
		t.Fatal("terminal result was resolved twice")
	}
}
