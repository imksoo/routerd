// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestRuntimeObservationUsesCurrentWhenAndExpiryWithoutWrites(t *testing.T) {
	now := time.Now().UTC()
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "current"}, Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "gated"},
		Spec: api.InterfaceSpec{IfName: "gated0", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}},
	}}}}
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &preparationObservationStore{SQLiteStore: db}
	if _, err := store.Set("ha.role", "backup", "test"); err != nil {
		t.Fatal(err)
	}
	part := dynamicconfig.NewPart("producer", "producer", nil, 1, now, now.Add(time.Minute))
	part.Spec.Resources = []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "dynamic"}, Spec: api.InterfaceSpec{IfName: "dynamic0"}}}
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListObjectStatuses()
	if err != nil {
		t.Fatal(err)
	}
	runner := &Runner{Router: router, Store: store, Bus: bus.New()}
	view, err := runner.ObserveRuntimeRouter(now, platform.CurrentOS())
	if err != nil {
		t.Fatal(err)
	}
	if view.DeclaredRouter != router || store.reads != 1 || len(view.RouteRouter.Spec.Resources) != 1 || view.RouteRouter.Spec.Resources[0].Metadata.Name != "dynamic" {
		t.Fatalf("backup observation: view=%+v reads=%d", view.RouteRouter, store.reads)
	}
	statuses, err := store.ListObjectStatuses()
	if err != nil || !reflect.DeepEqual(statuses, before) {
		t.Fatalf("backup observation published statuses: before=%+v after=%+v err=%v", before, statuses, err)
	}
	if _, err := store.Set("ha.role", "master", "test"); err != nil {
		t.Fatal(err)
	}
	before, err = store.ListObjectStatuses()
	if err != nil {
		t.Fatal(err)
	}
	view, err = runner.ObserveRuntimeRouter(now.Add(2*time.Minute), platform.CurrentOS())
	if err != nil {
		t.Fatal(err)
	}
	if store.reads != 2 || len(view.RouteRouter.Spec.Resources) != 1 || view.RouteRouter.Spec.Resources[0].Metadata.Name != "gated" {
		t.Fatalf("master observation after expiry: view=%+v reads=%d", view.RouteRouter, store.reads)
	}
	statuses, err = store.ListObjectStatuses()
	if err != nil || !reflect.DeepEqual(statuses, before) {
		t.Fatalf("read-only observation published resource status: %+v %v", statuses, err)
	}
}

func TestHTTPControllerSurvivesControllerFilterAndSchedulesBootstrapExpiry(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "current"}}
	views := make(chan RuntimeRouterObservation, 4)
	eventBus := bus.New()
	runner := &Runner{Router: router, Store: store, Bus: eventBus, Opts: Options{EnabledControllers: []string{"bgp"}, ReconcileHTTPServers: func(_ context.Context, view RuntimeRouterObservation) error {
		views <- view
		return nil
	}}}
	controllers, _, _, err := runner.frameworkControllers(t.Context(), nil, eventedStore{Store: store, Bus: eventBus}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	var controller framework.Controller
	for _, candidate := range controllers {
		if candidate.Name() == "control-http" {
			controller = candidate
		}
	}
	if controller == nil {
		t.Fatal("--controllers=bgp removed the actual HTTP configuration owner")
	}
	now := time.Now().UTC()
	part := dynamicconfig.NewPart("producer", "producer", nil, 1, now, now.Add(500*time.Millisecond))
	part.Spec.Resources = []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "WebConsole"}, Metadata: api.ObjectMeta{Name: "dynamic-console"}, Spec: api.WebConsoleSpec{ListenAddress: "127.0.0.1", Port: 8080}}}
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- (framework.Runner{Bus: eventBus}).Run(ctx, controller) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("HTTP controller did not release its actual event subscriptions")
		}
	}()
	select {
	case view := <-views:
		if countResources(view.EffectiveRouter, api.SystemAPIVersion, "WebConsole") != 1 || !view.NextChangeAt.Equal(part.Spec.ExpiresAt) {
			t.Fatalf("bootstrap view ignored dynamic server/expiry: %+v", view)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("HTTP bootstrap did not observe the live source")
	}
	select {
	case view := <-views:
		if countResources(view.EffectiveRouter, api.SystemAPIVersion, "WebConsole") != 0 {
			t.Fatal("HTTP source expiry retained the dynamic server")
		}
	case <-time.After(800 * time.Millisecond):
		t.Fatal("bootstrap-discovered HTTP expiry waited for the ordinary interval")
	}
}
