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
	if err := store.PublishManualPluginResult(context.Background(), 0, newer); err != nil {
		t.Fatal(err)
	}
	stale := newer
	stale.Digest = "slow-old-result"
	if err := other.PublishManualPluginResult(context.Background(), 0, stale); err == nil {
		t.Fatal("old invocation replaced the published result")
	}
	rows, err := store.GetDynamicConfigPartsBySource(newer.Source)
	if err != nil || len(rows) != 1 || rows[0].Digest != "new-result" {
		t.Fatalf("current result lost: %#v %v", rows, err)
	}
	newer.Generation = 2
	if err := other.PublishManualPluginResult(context.Background(), 1, newer); err != nil {
		t.Fatalf("subsequent current invocation failed: %v", err)
	}
}
