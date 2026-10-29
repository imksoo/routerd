// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/state"
)

func TestLiveProviderOperationCannotBeStolenByAnotherJournalConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	other, err := state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	started, release := make(chan struct{}), make(chan struct{})
	engine := newEngine(t, store, func(context.Context, api.PluginSpec, ExecuteActionRequest) (ExecuteActionResult, RunOutcome, error) {
		close(started)
		<-release
		return succeededResult(), RunOutcome{}, nil
	}, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, engine, "live-operation")
	if err := engine.Approve(id, "operator"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- engine.Execute(context.Background(), id, allowPolicy()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider boundary was not reached")
	}
	contender := newEngine(t, other, (&fakeRunner{result: succeededResult()}).run, []api.Resource{executorPlugin("aws")})
	if recovered, err := other.RecoverInterruptedActionOperations(context.Background(), time.Now().Add(24*time.Hour)); err != nil || recovered != 0 {
		t.Fatalf("live owner was classified by age: count=%d err=%v", recovered, err)
	}
	if err := contender.Execute(context.Background(), id, allowPolicy()); !errors.Is(err, state.ErrActionOperationBusy) {
		t.Fatalf("concurrent execute was not fenced: %v", err)
	}
	if err := contender.Rollback(context.Background(), id, allowPolicy()); !errors.Is(err, state.ErrActionOperationBusy) {
		t.Fatalf("concurrent undo was not fenced: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if row, found, err := other.GetActionByID(id); err != nil || !found || row.Status != state.ActionSucceeded {
		t.Fatalf("actual owner did not complete durably: %+v %t %v", row, found, err)
	}
}

func TestProviderInvocationUncertaintyNeverAutomaticallyReexecutes(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{err: errors.New("reply lost after provider operation")}
	engine := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, engine, "unknown")
	if err := engine.Approve(id, "operator"); err != nil {
		t.Fatal(err)
	}
	if err := engine.Execute(context.Background(), id, allowPolicy()); err == nil {
		t.Fatal("lost result reported success")
	}
	if row, _, err := store.GetActionByID(id); err != nil || row.Status != state.ActionOutcomeUnknown {
		t.Fatalf("uncertain invocation classified as definitive failure: %+v %v", row, err)
	}
	if _, err := engine.ImportFromDynamicParts(); err != nil {
		t.Fatal(err)
	}
	if err := engine.Execute(context.Background(), id, allowPolicy()); err == nil || runner.calls != 1 {
		t.Fatalf("unknown effect was repeated: calls=%d err=%v", runner.calls, err)
	}
}

func TestRollbackPersistsClaimAndPreservesForwardFactsAcrossFailureAndRetry(t *testing.T) {
	store := mustStore(t)
	runner := &fakeRunner{result: succeededResult()}
	runner.result.Status.Observed = map[string]string{"priorSourceDestCheck": "true"}
	engine := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, engine, "undo")
	if err := engine.Approve(id, "operator"); err != nil {
		t.Fatal(err)
	}
	if err := engine.Execute(context.Background(), id, allowPolicy()); err != nil {
		t.Fatal(err)
	}
	forward, _, _ := store.GetActionByID(id)
	engine.run = func(_ context.Context, _ api.PluginSpec, request ExecuteActionRequest) (ExecuteActionResult, RunOutcome, error) {
		row, found, err := store.GetActionByID(id)
		if err != nil || !found || row.Status != state.ActionRollingBack || request.Spec.Parameters["priorSourceDestCheck"] != "true" {
			t.Fatalf("undo ran without durable claim/original facts: %+v %t %v request=%+v", row, found, err, request)
		}
		return ExecuteActionResult{Status: ExecuteActionResultStatus{Status: ResultFailed, Error: "definitive refusal"}}, RunOutcome{}, nil
	}
	if err := engine.Rollback(context.Background(), id, allowPolicy()); err == nil {
		t.Fatal("failed undo reported success")
	}
	failed, _, _ := store.GetActionByID(id)
	if failed.Status != state.ActionRollbackFailed || failed.Observed["priorSourceDestCheck"] != "true" || !failed.ExecutedAt.Equal(forward.ExecutedAt) {
		t.Fatalf("undo failure erased forward facts: %+v", failed)
	}
	engine.run = (&fakeRunner{result: succeededResult()}).run
	if err := engine.Rollback(context.Background(), id, allowPolicy()); err != nil {
		t.Fatal(err)
	}
	if row, _, _ := store.GetActionByID(id); row.Status != state.ActionRolledBack || row.Observed["priorSourceDestCheck"] != "true" {
		t.Fatalf("definitive undo retry result = %+v", row)
	}
}

func TestUnresolvedProviderOperationFencesDifferentKeyAndRollback(t *testing.T) {
	for _, status := range []string{state.ActionOutcomeUnknown, state.ActionRollbackUnknown, state.ActionRunning, state.ActionRollingBack} {
		t.Run(status, func(t *testing.T) {
			store := mustStore(t)
			runner := &fakeRunner{result: succeededResult()}
			engine := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
			live := importOne(t, store, engine, "new-generation")
			if err := engine.Approve(live, "operator"); err != nil {
				t.Fatal(err)
			}
			_, err := store.ImportAction(state.ActionExecutionRecord{IdempotencyKey: "prior-success", Provider: "aws", Action: "assign-secondary-ip", Undo: &api.ProviderActionUndo{Action: "unassign-secondary-ip"}, Status: state.ActionPending})
			if err != nil {
				t.Fatal(err)
			}
			undo, found, err := store.GetActionByIdempotencyKey("prior-success")
			if err != nil || !found {
				t.Fatalf("undo fixture missing: %v %v", found, err)
			}
			undoID := undo.ID
			if err := store.ApproveAction(undoID, "operator", time.Now()); err != nil {
				t.Fatal(err)
			}
			if claimed, err := store.BeginActionExecution(undoID, time.Now()); err != nil || !claimed {
				t.Fatalf("claim undo fixture: %v %v", claimed, err)
			}
			if err := store.MarkActionResult(undoID, state.ActionSucceeded, "", "", nil, time.Now()); err != nil {
				t.Fatal(err)
			}
			_, err = store.ImportAction(state.ActionExecutionRecord{IdempotencyKey: "old-unresolved-generation", Provider: "aws", Action: "assign-secondary-ip", Status: state.ActionPending, Undo: &api.ProviderActionUndo{Action: "unassign-secondary-ip"}})
			if err != nil {
				t.Fatal(err)
			}
			unknown, found, err := store.GetActionByIdempotencyKey("old-unresolved-generation")
			if err != nil || !found {
				t.Fatalf("uncertain fixture missing: %v %v", found, err)
			}
			uncertain := unknown.ID
			if err := store.ApproveAction(uncertain, "operator", time.Now()); err != nil {
				t.Fatal(err)
			}
			if claimed, err := store.BeginActionExecution(uncertain, time.Now()); err != nil || !claimed {
				t.Fatalf("claim uncertain fixture: %v %v", claimed, err)
			}
			switch status {
			case state.ActionOutcomeUnknown:
				err = store.MarkActionResult(uncertain, status, "", "reply lost", nil, time.Now())
			case state.ActionRollingBack, state.ActionRollbackUnknown:
				if err = store.MarkActionResult(uncertain, state.ActionSucceeded, "", "", nil, time.Now()); err != nil {
					t.Fatal(err)
				}
				if claimed, claimErr := store.BeginActionRollback(uncertain, time.Now()); claimErr != nil || !claimed {
					t.Fatalf("claim uncertain undo: %v %v", claimed, claimErr)
				}
				if status == state.ActionRollbackUnknown {
					err = store.MarkActionRollbackResult(uncertain, status, "", "undo reply lost", time.Now())
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.Execute(context.Background(), live, allowPolicy()); err == nil {
				t.Fatal("new key bypassed unresolved provider operation")
			}
			policy := allowPolicy()
			policy.AllowUndo = true
			if err := engine.Rollback(context.Background(), undoID, policy); err == nil {
				t.Fatal("different undo bypassed unresolved provider operation")
			}
			if runner.calls != 0 {
				t.Fatalf("unresolved request overlapped %d new operations", runner.calls)
			}
			if row, _, err := store.GetActionByID(live); err != nil || row.Status != state.ActionApproved {
				t.Fatalf("refusal mutated pending generation: %+v %v", row, err)
			}
		})
	}
}
