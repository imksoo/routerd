package derived

import (
	"context"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestAssertAndRetract(t *testing.T) {
	store := mapStore{api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Pending"}}
	controller := testController(store, "0s", false)

	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 0 {
		t.Fatalf("initial asserted events = %d", len(got))
	}
	store[api.NetAPIVersion+"/DSLiteTunnel/ds-lite"]["phase"] = "Up"
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 1 {
		t.Fatalf("asserted events = %d", len(got))
	}
	store[api.NetAPIVersion+"/DSLiteTunnel/ds-lite"]["phase"] = "Pending"
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got := controller.Bus.Recent("routerd.virtual.dslite.retracted"); len(got) != 1 {
		t.Fatalf("retracted events = %d", len(got))
	}
}

func TestHysteresisFlipBackCancels(t *testing.T) {
	store := mapStore{api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Pending"}}
	controller := testController(store, "40ms", false)
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	store[api.NetAPIVersion+"/DSLiteTunnel/ds-lite"]["phase"] = "Up"
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	store[api.NetAPIVersion+"/DSLiteTunnel/ds-lite"]["phase"] = "Pending"
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 0 {
		t.Fatalf("asserted events = %d", len(got))
	}
	controller.StopTimers()
}

func TestEmitInitialFalse(t *testing.T) {
	store := mapStore{api.NetAPIVersion + "/DSLiteTunnel/ds-lite": {"phase": "Up"}}
	controller := testController(store, "0s", false)
	if err := controller.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got := controller.Bus.Recent("routerd.virtual.dslite.asserted"); len(got) != 0 {
		t.Fatalf("initial asserted events = %d", len(got))
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DerivedEvent", "dslite-active")
	if status["phase"] != PhaseAsserted || status["asserted"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func testController(store mapStore, hysteresis string, emitInitial bool) *Controller {
	b := bus.New()
	return &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DerivedEvent"},
				Metadata: api.ObjectMeta{Name: "dslite-active"},
				Spec: api.DerivedEventSpec{
					Topic:       "routerd.virtual.dslite",
					Inputs:      []api.ReadyWhenSpec{{Field: "${DSLiteTunnel/ds-lite.status.phase}", Equals: "Up"}},
					EmitWhen:    EmitAllTrue,
					RetractWhen: RetractAnyFalse,
					Hysteresis:  hysteresis,
					EmitInitial: emitInitial,
				},
			},
		}}},
		Bus:   b,
		Store: store,
	}
}
