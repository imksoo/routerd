// SPDX-License-Identifier: BSD-3-Clause

package eventconsumer

import (
	"context"
	"path/filepath"
	"testing"

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
