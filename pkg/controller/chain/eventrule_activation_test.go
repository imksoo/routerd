// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/ha"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestAuxiliaryPublishersWaitForSelectedGenerationAndRetainBootstrapInput(t *testing.T) {
	for _, cancelCandidate := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "canceled candidate"}[cancelCandidate], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			inspection, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer inspection.Close()
			if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "input", map[string]any{"phase": "Ready"}); err != nil {
				t.Fatal(err)
			}
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "activation"}, Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "rule"}, Spec: api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: "any_of", Topic: "routerd.test.bootstrap"}, Emit: api.EventRuleEmitSpec{Topic: "routerd.test.output"}}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DerivedEvent"}, Metadata: api.ObjectMeta{Name: "derived"}, Spec: api.DerivedEventSpec{Topic: "routerd.test.derived", Inputs: []api.ReadyWhenSpec{{Field: "Interface/input.status.phase", Equals: "Ready"}}, EmitInitial: true}},
			}}}
			events := bus.NewWithStore(store)
			runner := &Runner{Router: router, Store: store, Bus: events, Opts: Options{EnabledControllers: []string{"event-rule", "derived-event"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
			entered, release := make(chan struct{}), make(chan struct{})
			runner.generationBuilder = func(ctx context.Context, logger *slog.Logger, observed eventedStore, start bool, decision ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
				controllers, status, lifetimes, err := runner.frameworkControllers(ctx, logger, observed, start, decision)
				if err != nil {
					return nil, status, lifetimes, err
				}
				controllers = append(controllers, framework.FuncController{ControllerName: "bootstrap barrier", ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
					if err := events.Publish(ctx, daemonapi.NewEvent(daemonapi.DaemonRef{Name: "bootstrap"}, "routerd.test.bootstrap", daemonapi.SeverityInfo)); err != nil {
						return err
					}
					close(entered)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}})
				return controllers, status, lifetimes, nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- runner.Start(ctx) }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("bootstrap barrier not entered")
			}
			// Give an erroneously started consumer its actual immediate drain /
			// initial Derived evaluation while bootstrap remains blocked.
			quiet := time.NewTimer(100 * time.Millisecond)
			<-quiet.C
			// The candidate contains a matching durable input and an initially
			// asserted DerivedEvent, but neither owner may publish before selection.
			if got := runner.ActiveRuntimeSnapshot(); got.Available {
				t.Fatalf("candidate published as active: %+v", got)
			}
			var cursors int
			if err := inspection.QueryRow(`SELECT count(*) FROM event_consumer_cursors WHERE consumer='eventrule/engine'`).Scan(&cursors); err != nil || cursors != 0 {
				t.Fatalf("candidate created durable consumption state: cursors=%d error=%v", cursors, err)
			}
			for _, topic := range []string{"routerd.test.output", "routerd.test.derived.asserted"} {
				if got := events.Recent(topic); len(got) != 0 {
					t.Fatalf("candidate emitted %s: %+v", topic, got)
				}
			}
			if cancelCandidate {
				cancel()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("canceled candidate error=%v", err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("candidate did not stop")
				}
				for _, topic := range []string{"routerd.test.output", "routerd.test.derived.asserted"} {
					if got := events.Recent(topic); len(got) != 0 {
						t.Fatalf("canceled candidate emitted %s", topic)
					}
				}
				return
			}
			close(release)
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("selected generation not ready")
			}
			for _, topic := range []string{"routerd.test.output", "routerd.test.derived.asserted"} {
				deadline := time.Now().Add(3 * time.Second)
				for len(events.Recent(topic)) == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if len(events.Recent(topic)) != 1 {
					t.Fatalf("selected owner lost bootstrap/current input for %s", topic)
				}
			}
			cancel()
			select {
			case <-runner.runtimeDone:
			case <-time.After(3 * time.Second):
				t.Fatal("selected generation did not join")
			}
		})
	}
}

func TestEventRuleActivationFailureReturnsBeforeReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	faultDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer faultDB.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "rule"},
		Spec: api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: "any_of", Topic: "routerd.test.input"}, Emit: api.EventRuleEmitSpec{Topic: "routerd.test.output"}},
	}}}}
	events := bus.NewWithStore(store)
	runner := &Runner{Router: router, Store: store, Bus: events, Opts: Options{EnabledControllers: []string{"event-rule"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	runner.generationBuilder = func(ctx context.Context, logger *slog.Logger, observed eventedStore, start bool, decision ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
		controllers, status, lifetimes, err := runner.frameworkControllers(ctx, logger, observed, start, decision)
		if err != nil {
			return nil, status, lifetimes, err
		}
		controllers = append(controllers, framework.FuncController{ControllerName: "inject activation failure", ReconcileFunc: func(context.Context, daemonapi.DaemonEvent) error {
			_, err := faultDB.Exec(`CREATE TRIGGER fail_eventrule_activation BEFORE INSERT ON event_consumer_cursors WHEN NEW.consumer='eventrule/engine' BEGIN SELECT RAISE(FAIL,'eventrule activation failed'); END`)
			return err
		}})
		return controllers, status, lifetimes, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runner.Start(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrRuntimeMutationStopped) {
			t.Fatalf("Start acknowledged a failed activation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start waited forever after its activation owner exited")
	}
	if runner.ActiveRuntimeSnapshot().Available || runner.RuntimeMutationError() == nil {
		t.Fatal("failed activation left runtime available")
	}
	if len(events.Recent("routerd.test.output")) != 0 {
		t.Fatal("failed activation published an output")
	}
}
