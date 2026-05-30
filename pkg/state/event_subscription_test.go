// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"testing"
)

func TestSubscriptionRunDedupRetryTransitions(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()

	const sub = "EventSubscription/claim-onprem"
	const ev = "evt-1"

	// New event: no row yet.
	if _, _, found, err := store.SubscriptionRunStatus(sub, ev); err != nil {
		t.Fatalf("status: %v", err)
	} else if found {
		t.Fatalf("expected not found for new event")
	}

	// First start: pending, attempts=1.
	if err := store.UpsertSubscriptionRunStart(sub, ev, "cloudedge", "claim-plugin"); err != nil {
		t.Fatalf("start: %v", err)
	}
	status, attempts, found, err := store.SubscriptionRunStatus(sub, ev)
	if err != nil || !found {
		t.Fatalf("status after start: found=%v err=%v", found, err)
	}
	if status != "pending" || attempts != 1 {
		t.Fatalf("after first start = %s/%d, want pending/1", status, attempts)
	}

	// Mark failed, then retry start: attempts increments to 2.
	if err := store.MarkSubscriptionRunResult(sub, ev, "failed", "", 0, "boom"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := store.UpsertSubscriptionRunStart(sub, ev, "cloudedge", "claim-plugin"); err != nil {
		t.Fatalf("retry start: %v", err)
	}
	status, attempts, _, _ = store.SubscriptionRunStatus(sub, ev)
	if status != "pending" || attempts != 2 {
		t.Fatalf("after retry = %s/%d, want pending/2", status, attempts)
	}

	// Mark succeeded.
	if err := store.MarkSubscriptionRunResult(sub, ev, "succeeded", sub+"/"+ev, 1, ""); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	status, attempts, _, _ = store.SubscriptionRunStatus(sub, ev)
	if status != "succeeded" || attempts != 2 {
		t.Fatalf("after success = %s/%d, want succeeded/2", status, attempts)
	}

	// A start on a succeeded row is a no-op: status/attempts unchanged.
	if err := store.UpsertSubscriptionRunStart(sub, ev, "cloudedge", "claim-plugin"); err != nil {
		t.Fatalf("start on succeeded: %v", err)
	}
	status, attempts, _, _ = store.SubscriptionRunStatus(sub, ev)
	if status != "succeeded" || attempts != 2 {
		t.Fatalf("start on succeeded changed row to %s/%d", status, attempts)
	}

	runs, err := store.ListSubscriptionRuns(sub)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 run row, got %d", len(runs))
	}
	if runs[0].DynamicSource != sub+"/"+ev || runs[0].DynamicGeneration != 1 {
		t.Fatalf("dynamic fields not persisted: %+v", runs[0])
	}
	if runs[0].Error != "" {
		t.Fatalf("error should be cleared on success, got %q", runs[0].Error)
	}
}
