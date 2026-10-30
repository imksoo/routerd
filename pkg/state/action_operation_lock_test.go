// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActionOperationGuardFencesLiveOwnerAcrossConnectionsAndProcess(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	oldID := importApproveAndClaimAt(t, store, "old-running", now.Add(-24*time.Hour))
	freshID := importApproveAndClaimAt(t, store, "fresh-running", now)
	guard, err := store.TryLockActionOperations(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	second, err := OpenSQLite(store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.TryLockActionOperations(t.Context()); !errors.Is(err, ErrActionOperationBusy) {
		t.Fatalf("live owner not fenced: %v", err)
	}
	if count, err := second.RecoverInterruptedActionOperations(t.Context(), now.Add(365*24*time.Hour)); err != nil || count != 0 {
		t.Fatalf("stole a slow live operation: %d %v", count, err)
	}
	for _, id := range []int64{oldID, freshID} {
		row, _, err := second.GetActionByID(id)
		if err != nil || row.Status != ActionRunning {
			t.Fatalf("changed live operation: %+v %v", row, err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestActionOperationGuardChild$", "-test.v=false")
	command.Env = append(os.Environ(), "ROUTERD_TEST_ACTION_GUARD_DB="+store.path)
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "guard busy") {
		t.Fatalf("cross-process guard: %v %s", err, output)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if count, err := second.RecoverInterruptedActionOperations(t.Context(), now); err != nil || count != 2 {
		t.Fatalf("recover interrupted rows: %d %v", count, err)
	}
	for _, id := range []int64{oldID, freshID} {
		row, _, err := second.GetActionByID(id)
		if err != nil || row.Status != ActionOutcomeUnknown {
			t.Fatalf("unknown outcome became eligible: %+v %v", row, err)
		}
		if imported, err := second.ImportAction(sampleActionRecord(row.IdempotencyKey)); err != nil || imported {
			t.Fatalf("unknown outcome requeued: %v %v", imported, err)
		}
	}
}

func TestActionOperationGuardChild(t *testing.T) {
	path := os.Getenv("ROUTERD_TEST_ACTION_GUARD_DB")
	if path == "" {
		return
	}
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	guard, err := store.TryLockActionOperations(t.Context())
	if guard != nil {
		_ = guard.Close()
	}
	if !errors.Is(err, ErrActionOperationBusy) {
		t.Fatalf("another process acquired live guard: %v", err)
	}
	fmt.Println("guard busy")
}

func TestActionRollbackClaimRecoveryPreservesForwardEvidence(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	now := time.Now().UTC()
	id := importApproveAndClaimAt(t, store, "undo-uncertain", now)
	if err := store.MarkActionResult(id, ActionSucceeded, "applied", "", map[string]string{"priorSourceDestCheck": "true"}, now); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.BeginActionRollback(id, now); err != nil || !claimed {
		t.Fatalf("claim undo: %v %v", claimed, err)
	}
	if claimed, err := store.BeginActionRollback(id, now); err != nil || claimed {
		t.Fatalf("duplicate undo claim: %v %v", claimed, err)
	}
	if count, err := store.RecoverInterruptedActionOperations(t.Context(), now); err != nil || count != 1 {
		t.Fatalf("recover undo: %d %v", count, err)
	}
	row, _, err := store.GetActionByID(id)
	if err != nil || row.Status != ActionRollbackUnknown || row.Observed["priorSourceDestCheck"] != "true" || !row.ExecutedAt.Equal(now) {
		t.Fatalf("lost forward evidence: %+v %v", row, err)
	}
	if claimed, err := store.BeginActionRollback(id, now); err != nil || claimed {
		t.Fatalf("unconfirmed undo retried: %v %v", claimed, err)
	}
	if err := store.MarkActionRollbackResult(id, ActionRolledBack, "late success", "", now); err == nil {
		t.Fatal("unclaimed result was accepted")
	}
}

func TestActionOperationGuardCancellationAndClosedStore(t *testing.T) {
	store := mustOpenStore(t)
	path := store.path
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.TryLockActionOperations(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled operation admitted: %v", err)
	}
	if _, err := os.Stat(path + ".actions.lock"); !os.IsNotExist(err) {
		t.Fatalf("canceled operation created guard: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TryLockActionOperations(t.Context()); err == nil {
		t.Fatal("closed journal admitted operation")
	}
	if _, err := os.Stat(filepath.Clean(path) + ".actions.lock"); !os.IsNotExist(err) {
		t.Fatalf("closed journal created guard: %v", err)
	}
}
