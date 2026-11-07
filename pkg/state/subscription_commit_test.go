// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSubscriptionDesiredPartAndBatchDedupCommitAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	const subscription = "EventSubscription/test"
	for _, id := range []string{"one", "two"} {
		if err := store.UpsertSubscriptionRunStart(subscription, id, "group", "plugin"); err != nil {
			t.Fatal(err)
		}
	}
	part := DynamicConfigPartRecord{Source: subscription + "/batch", Generation: 1, Digest: "new", Status: "active", ResourcesJSON: "[]", ObservedAt: time.Now()}
	// A real SQLite failure after the part and the first acknowledgement were
	// written must roll both back, rather than exposing a partially applied batch.
	if _, err := store.db.Exec(`CREATE TRIGGER fail_second_subscription_ack BEFORE UPDATE ON event_subscription_runs WHEN NEW.event_id='two' AND NEW.status='succeeded' BEGIN SELECT RAISE(ABORT,'fixture write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitSubscriptionResult(context.Background(), subscription, []string{"one", "two"}, part); err == nil {
		t.Fatal("partial batch reported success")
	}
	records, err := store.GetDynamicConfigPartsBySource(part.Source)
	if err != nil || len(records) != 0 {
		t.Fatalf("uncommitted part visible: %+v err=%v", records, err)
	}
	for _, id := range []string{"one", "two"} {
		status, attempts, found, err := store.SubscriptionRunStatus(subscription, id)
		if err != nil || !found || status != "pending" || attempts != 1 {
			t.Fatalf("uncommitted dedup %s=%s/%d found=%v err=%v", id, status, attempts, found, err)
		}
	}
	if _, err := store.db.Exec(`DROP TRIGGER fail_second_subscription_ack`); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitSubscriptionResult(context.Background(), subscription, []string{"one", "two"}, part); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err = store.GetDynamicConfigPartsBySource(part.Source)
	if err != nil || len(records) != 1 || records[0].Digest != "new" {
		t.Fatalf("committed part lost on restart: %+v err=%v", records, err)
	}
	for _, id := range []string{"one", "two"} {
		status, attempts, found, err := store.SubscriptionRunStatus(subscription, id)
		if err != nil || !found || status != "succeeded" || attempts != 1 {
			t.Fatalf("committed dedup %s=%s/%d found=%v err=%v", id, status, attempts, found, err)
		}
	}
	// A delayed failure callback cannot revoke an already committed success.
	if err := store.MarkSubscriptionRunResult(subscription, "one", "failed", "", 0, "late failure"); err == nil {
		t.Fatal("late failure overwrote terminal dedup")
	}
}

func TestSubscriptionCommitRejectsMissingInvocationWithoutPublishingPart(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	part := DynamicConfigPartRecord{Source: "EventSubscription/test/batch", Generation: 1, Status: "active", ResourcesJSON: "[]"}
	if err := store.CommitSubscriptionResult(context.Background(), "EventSubscription/test", []string{"missing"}, part); err == nil {
		t.Fatal("missing invocation acknowledged")
	}
	records, err := store.GetDynamicConfigPartsBySource(part.Source)
	if err != nil || len(records) != 0 {
		t.Fatalf("part published without invocation: %+v err=%v", records, err)
	}
}
