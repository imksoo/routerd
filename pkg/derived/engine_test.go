// SPDX-License-Identifier: BSD-3-Clause

package derived

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func openStore(t *testing.T) *routerstate.SQLiteStore {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func setInput(t *testing.T, store *routerstate.SQLiteStore, phase, peer string) {
	t.Helper()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DSLiteTunnel", "ds-lite", map[string]any{"phase": phase, "peer": peer}); err != nil {
		t.Fatal(err)
	}
}

func testController(store *routerstate.SQLiteStore, hysteresis string, emitInitial bool) *Controller {
	return &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DerivedEvent"},
			Metadata: api.ObjectMeta{Name: "dslite-active"},
			Spec: api.DerivedEventSpec{
				Topic:    "routerd.virtual.dslite",
				Inputs:   []api.ReadyWhenSpec{{Field: "${DSLiteTunnel/ds-lite.status.phase}", Equals: "Up"}},
				EmitWhen: EmitAllTrue, RetractWhen: RetractAnyFalse,
				Hysteresis: hysteresis, EmitInitial: emitInitial,
			},
		}}}},
		Bus: bus.NewWithStore(store), Store: store,
	}
}

func reconcile(t *testing.T, controller *Controller) {
	t.Helper()
	if err := controller.Reconcile(t.Context(), false); err != nil {
		t.Fatal(err)
	}
}

func checkpoint(t *testing.T, store *routerstate.SQLiteStore) routerstate.DerivedCheckpoint {
	t.Helper()
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "DerivedEvent", "dslite-active")
	if err != nil {
		t.Fatal(err)
	}
	cp, err := routerstate.DecodeDerivedCheckpoint(status)
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

func TestAssertAndRetract(t *testing.T) {
	store := openStore(t)
	setInput(t, store, "Pending", "")
	controller := testController(store, "0s", false)
	reconcile(t, controller)
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 0 {
		t.Fatalf("initial asserted events = %d", len(got))
	}
	setInput(t, store, "Up", "")
	reconcile(t, controller)
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 1 {
		t.Fatalf("asserted events = %d", len(got))
	}
	setInput(t, store, "Pending", "")
	reconcile(t, controller)
	if got := controller.Bus.Recent("routerd.virtual.dslite.retracted"); len(got) != 1 {
		t.Fatalf("retracted events = %d", len(got))
	}
}

func TestHysteresisFlipBackCancels(t *testing.T) {
	store := openStore(t)
	setInput(t, store, "Pending", "")
	controller := testController(store, "40ms", false)
	now := time.Now().UTC()
	controller.Now = func() time.Time { return now }
	reconcile(t, controller)
	setInput(t, store, "Up", "")
	reconcile(t, controller)
	if checkpoint(t, store).PendingDue.IsZero() {
		t.Fatal("no durable deadline")
	}
	setInput(t, store, "Pending", "")
	reconcile(t, controller)
	now = now.Add(time.Second)
	reconcile(t, controller)
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 0 {
		t.Fatalf("asserted events = %d", len(got))
	}
	if !checkpoint(t, store).PendingDue.IsZero() {
		t.Fatal("retained canceled deadline")
	}
}

func TestHysteresisClearsDeadlineAfterFire(t *testing.T) {
	store := openStore(t)
	setInput(t, store, "Pending", "")
	controller := testController(store, "20ms", false)
	now := time.Now().UTC()
	controller.Now = func() time.Time { return now }
	reconcile(t, controller)
	setInput(t, store, "Up", "")
	reconcile(t, controller)
	now = now.Add(time.Second)
	reconcile(t, controller)
	cp := checkpoint(t, store)
	if !cp.PendingDue.IsZero() || !cp.Asserted {
		t.Fatalf("pending state after fire: %+v", cp)
	}
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 1 {
		t.Fatalf("asserted events = %d", len(got))
	}
}

func TestEmitInitialFalse(t *testing.T) {
	store := openStore(t)
	setInput(t, store, "Up", "")
	controller := testController(store, "0s", false)
	reconcile(t, controller)
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 0 {
		t.Fatalf("initial asserted events = %d", len(got))
	}
	if !checkpoint(t, store).Asserted {
		t.Fatal("initial decision not persisted")
	}
}

func TestCanceledOwnerDoesNotPublishDelayedTransition(t *testing.T) {
	store := openStore(t)
	setInput(t, store, "Pending", "")
	controller := testController(store, "20ms", false)
	controller.Interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done, err := controller.Start(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	setInput(t, store, "Up", "")
	deadline := time.Now().Add(time.Second)
	for checkpoint(t, store).PendingDue.IsZero() {
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("did not schedule transition")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	before := checkpoint(t, store)
	time.Sleep(40 * time.Millisecond)
	if after := checkpoint(t, store); after != before {
		t.Fatalf("stopped owner changed state: %+v -> %+v", before, after)
	}
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 0 {
		t.Fatal("canceled controller published delayed assertion")
	}
}
