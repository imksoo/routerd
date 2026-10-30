// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventrule"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestEventRuleAtomicResultUsesSelectedDeclarationOwnership(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	events := bus.NewWithStore(db)
	for index, owner := range []string{"old-owner", "current-owner"} {
		resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "rule", OwnerRefs: []api.OwnerRef{{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: owner}}}, Spec: api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: "count", Topic: "routerd.owner.input", Threshold: 1}, Emit: api.EventRuleEmitSpec{Topic: "routerd.owner.output"}}}
		router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
		actor := eventrule.Controller{Router: router, Bus: events, Store: eventRuleStore{eventedStore{Store: db, Bus: events, Router: router}}, Poll: time.Millisecond}
		ctx, cancel := context.WithCancel(t.Context())
		done, err := actor.Start(ctx)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if err := events.Publish(ctx, daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "test"}, "routerd.owner.input", daemonapi.SeverityInfo)); err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for len(events.Recent("routerd.owner.output")) < index+1 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		cancel()
		<-done
		status, err := db.ReadObjectStatus(api.NetAPIVersion, "EventRule", "rule")
		if err != nil {
			t.Fatal(err)
		}
		refs, ok := status["ownerRefs"].([]any)
		if !ok || len(refs) != 1 || refs[0].(map[string]any)["name"] != owner {
			t.Fatalf("atomic result used another declaration's metadata: %+v", status)
		}
		cp, err := db.ReadEventRule(t.Context(), "rule")
		if err != nil || cp.FireCount != index+1 {
			t.Fatalf("ownership refresh lost same-definition recovery state: %+v %v", cp, err)
		}
	}
	other, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "mismatch"}, Spec: api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: "count", Topic: "routerd.owner.input"}, Emit: api.EventRuleEmitSpec{Topic: "routerd.owner.output"}}}
	actor := eventrule.Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, Bus: events, Store: eventRuleStore{eventedStore{Store: other}}}
	if err := actor.Prepare(t.Context()); err == nil {
		t.Fatal("atomic results and bus accepted different journals")
	}
}
