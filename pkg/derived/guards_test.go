// SPDX-License-Identifier: BSD-3-Clause

package derived

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func independentGuards(t *testing.T, controller *Controller) {
	t.Helper()
	resource := &controller.Router.Spec.Resources[0]
	spec, err := resource.DerivedEventSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.Inputs = append(spec.Inputs, api.ReadyWhenSpec{Field: "${DSLiteTunnel/ds-lite.status.peer}", Equals: "Up"})
	spec.RetractWhen = RetractAllFalse
	resource.Spec = spec
}

func TestIndependentGuardsRetainLatchAcrossMixedInputs(t *testing.T) {
	store := openStore(t)
	controller := testController(store, "0s", false)
	independentGuards(t, controller)
	for _, step := range []struct {
		phase, peer string
		asserted    bool
	}{
		{"Up", "Up", true}, {"Pending", "Up", true}, {"Pending", "Up", true},
		{"Pending", "Pending", false}, {"Up", "Pending", false}, {"Up", "Up", true},
	} {
		setInput(t, store, step.phase, step.peer)
		reconcile(t, controller)
		if cp := checkpoint(t, store); cp.Asserted != step.asserted {
			t.Fatalf("%s/%s: %+v, want asserted=%t", step.phase, step.peer, cp, step.asserted)
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
	store := openStore(t)
	setInput(t, store, "Up", "Up")
	controller := testController(store, "20ms", false)
	independentGuards(t, controller)
	now := time.Now().UTC()
	controller.Now = func() time.Time { return now }
	reconcile(t, controller)
	setInput(t, store, "Pending", "Pending")
	reconcile(t, controller)
	setInput(t, store, "Pending", "Up")
	reconcile(t, controller)
	now = now.Add(time.Second)
	reconcile(t, controller)
	if got := len(controller.Bus.Recent("routerd.virtual.dslite.retracted")); got != 0 {
		t.Fatalf("retractions=%d from mixed inputs", got)
	}
	if cp := checkpoint(t, store); !cp.Asserted || !cp.PendingDue.IsZero() {
		t.Fatalf("retained state=%+v", cp)
	}
}
