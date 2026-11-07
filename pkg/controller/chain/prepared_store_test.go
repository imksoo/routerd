// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lifecycle"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPreparedGenerationStatusUsesCurrentDeclaredOwnership(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ownedRouter := func(owner string, suppressed bool) *api.Router {
		spec := api.NTPClientSpec{Managed: suppressed, Servers: []string{"192.0.2.123"}}
		if suppressed {
			spec.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"role": {Equals: "master"}}}
		}
		return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "accepted"}, Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
			Metadata: api.ObjectMeta{Name: "clock", OwnerRefs: []api.OwnerRef{{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: owner}}},
			Spec:     spec,
		}}}}
	}
	initial := ownedRouter("old-owner", false)
	if err := config.Validate(initial); err != nil {
		t.Fatal(err)
	}
	gate := &sync.RWMutex{}
	events := bus.New()
	runner := &Runner{Router: initial, Store: store, Bus: events, Opts: Options{EnabledControllers: []string{"ntp-client"}, DryRunNTP: true, MutationGate: gate}}
	stop, done := startFaultRuntime(t, runner)
	defer func() { stop(); waitFaultRuntimeStopped(t, done) }()
	if _, err := store.Set("role", "backup", "test current role"); err != nil {
		t.Fatal(err)
	}
	for _, update := range []struct {
		owner      string
		suppressed bool
	}{{"new-owner", false}, {"suppressed-owner", true}} {
		next := ownedRouter(update.owner, update.suppressed)
		if err := config.Validate(next); err != nil {
			t.Fatal(err)
		}
		gate.Lock()
		err := runner.UpdateRuntimeRouter(next)
		gate.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		want := lifecycle.OwnerRefsStatusList(next.Spec.Resources[0].Metadata.OwnerRefs)
		reason := ""
		if update.suppressed {
			reason = "WhenFalse"
		}
		waitPreparedOwnerStatus(t, store, events, api.SystemAPIVersion, "NTPClient", "clock", want, reason)
	}
}

func TestPreparedBGPGenerationStatusUsesCurrentOwnership(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "accepted"}, Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
		Metadata: api.ObjectMeta{Name: "core", OwnerRefs: []api.OwnerRef{{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "old-owner"}}},
		Spec:     api.BGPRouterSpec{ASN: 64512, RouterID: "192.0.2.1"},
	}}}}
	if err := config.Validate(router); err != nil {
		t.Fatal(err)
	}
	gate := &sync.RWMutex{}
	events := bus.New()
	runner := &Runner{Router: router, Store: store, Bus: events, Opts: Options{EnabledControllers: []string{"bgp"}, DryRunBGP: true, BGPStatePath: filepath.Join(t.TempDir(), "applied.json"), MutationGate: gate}}
	stop, done := startFaultRuntime(t, runner)
	defer func() { stop(); waitFaultRuntimeStopped(t, done) }()
	next := router.Clone()
	next.Spec.Resources[0].Metadata.OwnerRefs[0].Name = "new-owner"
	gate.Lock()
	err = runner.UpdateRuntimeRouter(&next)
	gate.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	waitPreparedOwnerStatus(t, store, events, api.NetAPIVersion, "BGPRouter", "core", lifecycle.OwnerRefsStatusList(next.Spec.Resources[0].Metadata.OwnerRefs), "")
}

func waitPreparedOwnerStatus(t *testing.T, store *routerstate.SQLiteStore, events *bus.Bus, apiVersion, kind, name string, want []any, reason string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		// Wake the existing generation through its real subscription callback.
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test"}, "routerd.controller.bootstrap", "info")
		if err := events.Publish(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		status, err := store.ReadObjectStatus(apiVersion, kind, name)
		if err != nil {
			t.Fatal(err)
		}
		if reflect.DeepEqual(status["ownerRefs"], want) && (reason == "" || status["reason"] == reason) {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("updated callback persisted stale ownership: %+v, want %+v", status, want)
		}
	}
}
