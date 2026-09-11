// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventconsumer"
	"github.com/imksoo/routerd/pkg/state"
)

// The callback executes the real action engine against the real SQLite journal.
// Only the provider executor is replaced: this test never starts a plugin or
// performs a cloud operation. Reopening SQLite demonstrates durable deduplication
// across a cursor-write failure, rather than an in-memory processed-ID set.
func TestFaultCursorFailureReplaysCallbackWithoutRepeatingSucceededAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.LoadOrInitializeEventConsumerCursor("fault-action"); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{result: succeededResult()}
	engine := newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	id := importOne(t, store, engine, "cursor-failure")
	if err := engine.Approve(id, "test-operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordBusEvent(context.Background(), daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "fixture"}, "routerd.action.test", daemonapi.SeverityInfo)); err != nil {
		t.Fatal(err)
	}
	faultDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer faultDB.Close()
	if _, err := faultDB.Exec(`CREATE TRIGGER fail_action_cursor BEFORE UPDATE ON event_consumer_cursors WHEN NEW.consumer = 'fault-action' BEGIN SELECT RAISE(FAIL, 'injected cursor failure'); END`); err != nil {
		t.Fatal(err)
	}
	callbacks := 0
	var cursors []string
	process := func(ctx context.Context, event daemonapi.DaemonEvent) error {
		callbacks++
		cursors = append(cursors, event.Cursor)
		return engine.Execute(ctx, id, ModeExecute, allowPolicy())
	}
	if err := eventconsumer.Drain(t.Context(), store, "fault-action", process); err == nil {
		t.Fatal("cursor failure was swallowed")
	}
	if callbacks != 1 || runner.calls != 1 {
		t.Fatalf("before restart callbacks=%d executor calls=%d", callbacks, runner.calls)
	}
	if cursor, err := store.LoadOrInitializeEventConsumerCursor("fault-action"); err != nil || cursor != 0 {
		t.Fatalf("failed cursor write advanced cursor=%d error=%v", cursor, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := faultDB.Exec("DROP TRIGGER fail_action_cursor"); err != nil {
		t.Fatal(err)
	}
	store, err = state.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	engine = newEngine(t, store, runner.run, []api.Resource{executorPlugin("aws")})
	if err := eventconsumer.Drain(t.Context(), store, "fault-action", process); err != nil {
		t.Fatal(err)
	}
	if callbacks != 2 || runner.calls != 1 {
		t.Fatalf("replay callbacks=%d executor calls=%d, want 2 and 1", callbacks, runner.calls)
	}
	if len(cursors) != 2 || cursors[0] != cursors[1] {
		t.Fatalf("replayed event identity changed: %v", cursors)
	}
	row, found, err := store.GetActionByID(id)
	if err != nil || !found || row.Status != state.ActionSucceeded {
		t.Fatalf("durable action result=%+v found=%t error=%v", row, found, err)
	}
	if cursor, err := store.LoadOrInitializeEventConsumerCursor("fault-action"); err != nil || cursor <= 0 {
		t.Fatalf("retry did not advance cursor=%d error=%v", cursor, err)
	}
}
