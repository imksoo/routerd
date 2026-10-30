// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestSelectedEmptyRuleGenerationDoesNotReplayAbsentInputsOnReadd(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller, _ := testController(api.EventRulePatternSpec{Operator: OperatorAnyOf, Topic: "routerd.a"})
	events := bus.NewWithStore(store)
	controller.Bus, controller.Store, controller.Events = events, store, store
	start := func(c *Controller) func() {
		t.Helper()
		ctx, cancel := context.WithCancel(t.Context())
		done, err := c.Start(ctx)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		return func() { cancel(); <-done }
	}
	stop := start(controller)
	stop()
	empty := &Controller{Router: &api.Router{}, Bus: events, Store: store, Events: store, Poll: time.Millisecond}
	stop = start(empty)
	defer func() { stop() }()
	if err := events.Publish(t.Context(), testEvent("routerd.a")); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListEvents(routerstate.EventQuery{Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Fatalf("missing absent input: %v %v", rows, err)
	}
	through := rows[0].ID
	deadline := time.Now().Add(time.Second)
	for {
		cursor, err := store.LoadOrInitializeEventConsumerCursor("eventrule/engine")
		if err != nil {
			t.Fatal(err)
		}
		if cursor >= through {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("empty selected generation stopped preserving the shared input position")
		}
		time.Sleep(time.Millisecond)
	}
	stop()
	readded := &Controller{Router: controller.Router, Bus: events, Store: store, Events: store, Poll: time.Millisecond}
	stop = start(readded)
	if err := events.Publish(t.Context(), testEvent("routerd.a")); err != nil {
		t.Fatal(err)
	}
	rows, err = store.ListEvents(routerstate.EventQuery{Topic: "routerd.a", Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Fatalf("missing readded input: %v %v", rows, err)
	}
	through = rows[0].ID
	deadline = time.Now().Add(time.Second)
	for {
		cursor, err := store.LoadOrInitializeEventConsumerCursor("eventrule/engine")
		if err != nil {
			t.Fatal(err)
		}
		if cursor >= through && len(events.Recent("routerd.out")) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("readded rule did not consume new input %s", strconv.FormatInt(through, 10))
		}
		time.Sleep(time.Millisecond)
	}
	if got := len(events.Recent("routerd.out")); got != 1 {
		t.Fatalf("readd replayed input while rule was absent: outputs=%d", got)
	}
}
