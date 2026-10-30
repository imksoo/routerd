// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventlog"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

func TestEventConsumerChangesReloadActualGeneration(t *testing.T) {
	for _, kind := range []string{"EventRule", "DerivedEvent", "Plugin"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			statePath, configPath := filepath.Join(dir, "state.db"), filepath.Join(dir, "router.yaml")
			store, err := routerstate.OpenSQLite(statePath)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: "selection"}}
			worker := "derived-event"
			if kind == "EventRule" {
				worker = "event-rule"
				resource.Spec = api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: "debounce", Topic: "routerd.test.trigger", Quiet: "300ms", AllowMissingCorrelation: true}, Emit: api.EventRuleEmitSpec{Topic: "routerd.test.old"}}
			} else if kind == "DerivedEvent" {
				resource.Spec = api.DerivedEventSpec{Topic: "routerd.test.old", Inputs: []api.ReadyWhenSpec{{Field: "ready", Equals: "ready"}}, EmitInitial: true}
			} else {
				resource.APIVersion, worker = api.PluginAPIVersion, "event-subscription"
				resource.Spec = api.PluginSpec{Executable: "/bin/true"}
			}
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "events"}, Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
			if err := config.Validate(router); err != nil {
				t.Fatal(err)
			}
			data, err := yaml.Marshal(router)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			events := bus.NewWithStore(store)
			gate := &sync.RWMutex{}
			runner := &controllerchain.Runner{Router: router, Store: store, Bus: events, Opts: controllerchain.Options{EnabledControllers: []string{worker}, MutationGate: gate}}
			ctx, cancel := context.WithCancel(context.Background())
			if err := runner.Start(ctx); err != nil {
				cancel()
				t.Fatal(err)
			}
			defer func() {
				cancel()
				waitEventGeneration(t, func() bool { active := runner.ActiveRuntimeSnapshot(); return active.Known && !active.Available })
			}()
			before := runner.ActiveRuntimeSnapshot()
			if kind == "EventRule" {
				trigger := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test"}, "routerd.test.trigger", daemonapi.SeverityInfo)
				cursor, err := store.RecordBusEvent(ctx, trigger)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := strconv.ParseInt(cursor, 10, 64)
				trigger.Cursor = cursor
				if err := events.PublishRecorded(ctx, trigger); err != nil {
					t.Fatal(err)
				}
				waitEventGeneration(t, func() bool {
					current, err := store.ReadEventRule(ctx, "selection")
					return err == nil && current.Cursor >= id
				})
			} else if kind == "DerivedEvent" {
				waitEventGeneration(t, func() bool { return len(events.Recent("routerd.test.old.asserted")) != 0 })
			}
			next := router.Clone()
			if kind == "EventRule" {
				spec, _ := next.Spec.Resources[0].EventRuleSpec()
				spec.Pattern.Quiet, spec.Emit.Topic = "10ms", "routerd.test.new"
				next.Spec.Resources[0].Spec = spec
			} else if kind == "DerivedEvent" {
				spec, _ := next.Spec.Resources[0].DerivedEventSpec()
				spec.Topic = "routerd.test.new"
				next.Spec.Resources[0].Spec = spec
			} else {
				spec, _ := next.Spec.Resources[0].PluginSpec()
				spec.Executable = "/bin/false"
				next.Spec.Resources[0].Spec = spec
			}
			candidate, err := yaml.Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			mutator := serveConfigMutator{configPath: configPath, statePath: statePath, baseOpts: applyOptions{ConfigPath: configPath, StatePath: statePath, LedgerPath: filepath.Join(dir, "ledger.db"), Sandbox: true, SkipServiceManager: true, MutationGate: gate}, cache: &resultCache{}, logger: &eventlog.Logger{}, getRouter: func() *api.Router { return runner.ActiveRuntimeSnapshot().Router }, applyRuntimeWithOutcome: runner.ApplyRuntimeWithOutcome, activeRuntime: runner.ActiveRuntimeSnapshot, runtimeMutationError: runner.RuntimeMutationError}
			if _, err := mutator.apply(nil, controlapi.ApplyRequest{CandidateYAML: string(candidate), Replace: true}); err != nil {
				t.Fatal(err)
			}
			if after := runner.ActiveRuntimeSnapshot(); after.Epoch <= before.Epoch || !after.Available {
				t.Fatalf("consumer change did not replace the generation: before=%+v after=%+v", before, after)
			}
			if kind == "EventRule" {
				if err := events.Publish(ctx, daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test"}, "routerd.test.trigger", daemonapi.SeverityInfo)); err != nil {
					t.Fatal(err)
				}
				waitEventGeneration(t, func() bool { return len(events.Recent("routerd.test.new")) != 0 })
				// The old debounce was admitted before replacement; its owning
				// generation must have canceled and acknowledged the timer.
				timer := time.NewTimer(350 * time.Millisecond)
				<-timer.C
				if old := events.Recent("routerd.test.old"); len(old) != 0 {
					t.Fatalf("retired generation emitted old rule: %+v", old)
				}
			} else if kind == "DerivedEvent" {
				waitEventGeneration(t, func() bool { return len(events.Recent("routerd.test.new.asserted")) != 0 })
			} else {
				spec, err := runner.ActiveRuntimeSnapshot().Router.Spec.Resources[0].PluginSpec()
				if err != nil || spec.Executable != "/bin/false" {
					t.Fatalf("new generation retained old plugin manifest: %+v %v", spec, err)
				}
			}
		})
	}
}

func waitEventGeneration(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("event consumer generation did not reach expected observation")
		}
	}
}
