// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/derived"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDerivedAtomicResultUsesSelectedDeclarationAndRequiredHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	events := bus.NewWithStore(db)
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DerivedEvent"}, Metadata: api.ObjectMeta{Name: "rule", OwnerRefs: []api.OwnerRef{{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "old-owner"}}}, Spec: api.DerivedEventSpec{Topic: "routerd.owner.derived", Inputs: []api.ReadyWhenSpec{{Field: "Interface/input.status.phase", Equals: "Ready"}}, EmitInitial: true}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
	if err := db.SaveObjectStatus(api.NetAPIVersion, "Interface", "input", map[string]any{"phase": "Ready"}); err != nil {
		t.Fatal(err)
	}
	actor := derived.Controller{Router: router, Bus: events, Store: derivedStore{eventedStore{Store: db, Bus: events, Router: router}}}
	fault, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.Close()
	if _, err := fault.Exec(`CREATE TRIGGER fail_derived_notification BEFORE INSERT ON events WHEN NEW.topic='routerd.resource.status.changed' AND NEW.resource_kind='DerivedEvent' BEGIN SELECT RAISE(FAIL,'required history fault'); END`); err != nil {
		t.Fatal(err)
	}
	if err := actor.Reconcile(t.Context(), false); err == nil {
		t.Fatal("required history failure swallowed")
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "DerivedEvent", "rule")
	if err != nil || len(status) != 0 {
		t.Fatalf("failed history committed checkpoint: %+v %v", status, err)
	}
	outputs, err := db.ListEvents(routerstate.EventQuery{Topic: "routerd.owner.derived.asserted"})
	if err != nil || len(outputs) != 0 {
		t.Fatalf("failed history committed output: %+v %v", outputs, err)
	}
	if _, err := fault.Exec("DROP TRIGGER fail_derived_notification"); err != nil {
		t.Fatal(err)
	}
	if err := actor.Reconcile(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	status, err = db.ReadObjectStatus(api.NetAPIVersion, "DerivedEvent", "rule")
	if err != nil {
		t.Fatal(err)
	}
	cp, err := routerstate.DecodeDerivedCheckpoint(status)
	if err != nil || !cp.Asserted {
		t.Fatalf("committed latch: %+v %v", cp, err)
	}
	if status["ownerKey"] != api.NetAPIVersion+"/DerivedEvent/rule" {
		t.Fatalf("missing actual owner metadata: %+v", status)
	}
	// A selected declaration may change ownership without changing the predicate.
	resource.Metadata.OwnerRefs[0].Name = "current-owner"
	router = &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
	actor = derived.Controller{Router: router, Bus: events, Store: derivedStore{eventedStore{Store: db, Bus: events, Router: router}}}
	if err := actor.Reconcile(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	status, err = db.ReadObjectStatus(api.NetAPIVersion, "DerivedEvent", "rule")
	if err != nil {
		t.Fatal(err)
	}
	refs, ok := status["ownerRefs"].([]any)
	if !ok || len(refs) != 1 || refs[0].(map[string]any)["name"] != "current-owner" {
		t.Fatalf("unchanged predicate retained old declaration: %+v", status)
	}
	after, err := routerstate.DecodeDerivedCheckpoint(status)
	if err != nil || cp != after {
		t.Fatalf("metadata refresh changed latch: %+v -> %+v %v", cp, after, err)
	}
	outputs, err = db.ListEvents(routerstate.EventQuery{Topic: "routerd.owner.derived.asserted"})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("metadata refresh repeated feature output: %+v %v", outputs, err)
	}
	if len(events.Recent("routerd.owner.derived.asserted")) != 1 {
		t.Fatal("atomic feature output not delivered exactly once")
	}
}
