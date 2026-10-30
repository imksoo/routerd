// SPDX-License-Identifier: BSD-3-Clause

package derived

import (
	"context"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestIndependentGuardsRetainLatchAcrossMixedInputs(t *testing.T) {
	store := mapStore{api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Up", "peer": "Up"}}
	controller := testController(store, "0s", false)
	resource := &controller.Router.Spec.Resources[0]
	spec, err := resource.DerivedEventSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.Inputs = append(spec.Inputs, api.ReadyWhenSpec{Field: "${DSLiteTunnel/ds-lite.status.peer}", Equals: "Up"})
	spec.RetractWhen = RetractAllFalse
	resource.Spec = spec
	for _, step := range []struct {
		phase, peer string
		asserted    bool
	}{
		{"Up", "Up", true},
		{"Pending", "Up", true},
		{"Pending", "Up", true},
		{"Pending", "Pending", false},
		{"Up", "Pending", false},
		{"Up", "Up", true},
	} {
		store[api.NetAPIVersion+"/DSLiteTunnel/ds-lite"]["phase"] = step.phase
		store[api.NetAPIVersion+"/DSLiteTunnel/ds-lite"]["peer"] = step.peer
		if err := controller.Reconcile(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		status := store.ObjectStatus(api.NetAPIVersion, "DerivedEvent", "dslite-active")
		if status["asserted"] != step.asserted {
			t.Fatalf("%s/%s: status=%#v, want asserted=%t", step.phase, step.peer, status, step.asserted)
		}
	}
	if got := len(controller.Bus.Recent("routerd.virtual.dslite.retracted")); got != 1 {
		t.Fatalf("retractions=%d, want 1", got)
	}
	if got := len(controller.Bus.Recent("routerd.virtual.dslite.asserted")); got != 1 {
		t.Fatalf("assertions=%d, want 1", got)
	}
}

func TestPendingRetractionCanceledWhenNeitherGuardMatches(t *testing.T) {
	store := mapStore{api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Up", "peer": "Up"}}
	controller := testController(store, "20ms", false)
	defer controller.StopTimers()
	resource := &controller.Router.Spec.Resources[0]
	spec, err := resource.DerivedEventSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.Inputs = append(spec.Inputs, api.ReadyWhenSpec{Field: "${DSLiteTunnel/ds-lite.status.peer}", Equals: "Up"})
	spec.RetractWhen = RetractAllFalse
	resource.Spec = spec
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	input := store[api.NetAPIVersion+"/DSLiteTunnel/ds-lite"]
	input["phase"], input["peer"] = "Pending", "Pending"
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	input["peer"] = "Up"
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if got := len(controller.Bus.Recent("routerd.virtual.dslite.retracted")); got != 0 {
		t.Fatalf("retractions=%d from mixed inputs", got)
	}
	controller.mu.Lock()
	state := controller.state["dslite-active"]
	if !state.asserted || state.pendingTransition != PendingNone || state.timer != nil {
		t.Fatalf("retained state=%+v", state)
	}
	controller.mu.Unlock()
}
