// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"testing"
	"time"
)

func TestManualPluginPublicationCannotOverwriteCompletedConcurrentRun(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	other, err := OpenSQLite(store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	now := time.Now().UTC()
	newer := DynamicConfigPartRecord{Source: "Plugin/example", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(time.Minute), Digest: "new-result", ResourcesJSON: "[]", Status: "active"}
	if err := store.PublishPluginResult(context.Background(), 0, newer); err != nil {
		t.Fatal(err)
	}
	stale := newer
	stale.Digest = "slow-old-result"
	if err := other.PublishPluginResult(context.Background(), 0, stale); err == nil {
		t.Fatal("old invocation replaced the published result")
	}
	rows, err := store.GetDynamicConfigPartsBySource(newer.Source)
	if err != nil || len(rows) != 1 || rows[0].Digest != "new-result" {
		t.Fatalf("current result lost: %#v %v", rows, err)
	}
	newer.Generation = 2
	if err := other.PublishPluginResult(context.Background(), 1, newer); err != nil {
		t.Fatalf("subsequent current invocation failed: %v", err)
	}
}

func TestPluginPublicationWaitCannotReplaceLiveResultWithExpiredOutput(t *testing.T) {
	store := mustOpenStore(t)
	defer store.Close()
	now := time.Now().UTC()
	previous := DynamicConfigPartRecord{Source: "Plugin/wait", Generation: 1, ObservedAt: now, ExpiresAt: now.Add(time.Minute), Digest: "valid", ResourcesJSON: "[]", Status: "active"}
	if err := store.PublishPluginResult(t.Context(), 0, previous); err != nil {
		t.Fatal(err)
	}
	expired := previous
	expired.Generation, expired.Digest, expired.ExpiresAt = 2, "expired", now.Add(20*time.Millisecond)
	store.mu.Lock()
	finished := make(chan error, 1)
	go func() { finished <- store.PublishPluginResult(t.Context(), 1, expired) }()
	timer := time.NewTimer(max(time.Until(expired.ExpiresAt), 0) + 10*time.Millisecond)
	<-timer.C
	store.mu.Unlock()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("publication after its ownership wait accepted an expired observation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not acknowledge the released wait")
	}
	records, err := store.GetDynamicConfigPartsBySource(previous.Source)
	if err != nil || len(records) != 1 || records[0].Generation != previous.Generation || records[0].Digest != previous.Digest || !records[0].ExpiresAt.Equal(previous.ExpiresAt) {
		t.Fatalf("expired publication changed the prior generation: %+v err=%v", records, err)
	}
}
