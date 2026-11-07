// SPDX-License-Identifier: BSD-3-Clause

package eventconsumer

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestSQLiteInitialCursorUsesCurrentMaxID(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	eventBus := bus.NewWithStore(store)
	for _, topic := range []string{"routerd.old", "routerd.latest"} {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test", Kind: "test"}, topic, daemonapi.SeverityInfo)
		if err := eventBus.Publish(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	processed := 0
	if err := Drain(context.Background(), store, "new-consumer", func(context.Context, daemonapi.DaemonEvent) error {
		processed++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if processed != 0 {
		t.Fatalf("initial drain replayed %d historical events", processed)
	}
	cursor, err := store.LoadOrInitializeEventConsumerCursor("new-consumer")
	if err != nil {
		t.Fatal(err)
	}
	if cursor != 2 {
		t.Fatalf("initial cursor = %d, want MAX(id)=2", cursor)
	}
}

func TestBackoffSuppressesRetriesAndCapsDelay(t *testing.T) {
	now := time.Unix(100, 0)
	backoff := Backoff{
		Initial: time.Second,
		Max:     4 * time.Second,
		Now:     func() time.Time { return now },
	}
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second} {
		if !backoff.Ready() {
			t.Fatal("backoff was not ready before failure")
		}
		backoff.Failure()
		if backoff.Ready() {
			t.Fatal("backoff allowed an immediate retry")
		}
		now = now.Add(delay)
		if !backoff.Ready() {
			t.Fatalf("backoff was not ready after %s", delay)
		}
	}
	backoff.Success()
	if !backoff.Ready() {
		t.Fatal("successful drain did not reset backoff")
	}
}
