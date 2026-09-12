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
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/eventconsumer"
	"github.com/imksoo/routerd/pkg/eventrule"
	"github.com/imksoo/routerd/pkg/ha"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFaultStatusEventRuleDoesNotLoseTransitionOnJournalFailure(t *testing.T) {
	for _, merge := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "merge"}[merge], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, err := store.LoadOrInitializeEventConsumerCursor("eventrule/engine"); err != nil {
				t.Fatal(err)
			}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "status-count"}, Spec: api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: eventrule.OperatorCount, Topic: "routerd.resource.status.changed", Threshold: 1}, Emit: api.EventRuleEmitSpec{Topic: "routerd.test.transition"}}}}}}
			events := bus.NewWithStore(store)
			observed := eventedStore{Store: store, Bus: events, Router: router}
			faultDB, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer faultDB.Close()
			if _, err := faultDB.Exec(`CREATE TRIGGER fail_status_event BEFORE INSERT ON events WHEN NEW.topic = 'routerd.resource.status.changed' BEGIN SELECT RAISE(FAIL, 'injected status event failure'); END`); err != nil {
				t.Fatal(err)
			}
			status := map[string]any{"phase": "Bound", "gateway": "192.0.2.1"}
			save := func() error {
				if merge {
					return observed.MergeObjectStatus(api.NetAPIVersion, "DHCPv4Client", "uplink", status)
				}
				return observed.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "uplink", status)
			}
			if err := save(); err == nil {
				t.Fatal("journal failure was swallowed")
			}
			if got := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "uplink"); len(got) != 0 {
				t.Fatalf("required history failed but status committed: %#v", got)
			}
			if got := len(events.Recent("routerd.resource.status.changed")); got != 0 {
				t.Fatalf("rolled-back transition delivered locally: %d", got)
			}
			if _, err := faultDB.Exec("DROP TRIGGER fail_status_event"); err != nil {
				t.Fatal(err)
			}
			if err := save(); err != nil {
				t.Fatal(err)
			}
			persisted, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.resource.status.changed"})
			if err != nil {
				t.Fatal(err)
			}
			if len(persisted) != 1 {
				t.Fatalf("status transition journal entries=%d, want 1", len(persisted))
			}
			if got := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "uplink")["gateway"]; got != "192.0.2.1" {
				t.Fatalf("retry status=%v", got)
			}
			controller := eventrule.Controller{Router: router, Bus: events, Store: observed, Events: store}
			if err := eventconsumer.Drain(t.Context(), store, "eventrule/engine", func(ctx context.Context, event daemonapi.DaemonEvent) error {
				if event.Daemon.Kind == "routerd-eventrule" {
					return nil
				}
				return controller.Reconcile(ctx, event)
			}); err != nil {
				t.Fatal(err)
			}
			if got := len(events.Recent("routerd.test.transition")); got != 1 {
				t.Fatalf("EventRule lost committed status transition after retry: emitted=%d, want 1", got)
			}
		})
	}
}

func TestFaultStatusHistoryCapabilityIsRequiredOnlyByMatchingRules(t *testing.T) {
	for _, topic := range []string{"routerd.unrelated", "routerd.resource.**"} {
		t.Run(topic, func(t *testing.T) {
			store := mapStore{}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "rule"}, Spec: api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: eventrule.OperatorCount, Topic: topic, Threshold: 1}, Emit: api.EventRuleEmitSpec{Topic: "routerd.output"}}}}}}
			observed := eventedStore{Store: store, Bus: bus.New(), Router: router}
			err := observed.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": "Ready"})
			if topic == "routerd.unrelated" {
				if err != nil {
					t.Fatalf("unused history capability rejected lightweight store: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("matching history rule accepted a nontransactional store")
				}
				if len(store.ObjectStatus(api.NetAPIVersion, "Interface", "uplink")) != 0 {
					t.Fatal("capability failure wrote status")
				}
			}
		})
	}
}

type deliveryObserver struct{ calls chan string }

func (o deliveryObserver) ControllerStarted(string, time.Duration) {}
func (o deliveryObserver) ControllerReconciled(_ string, trigger string, _ time.Duration, _ time.Duration, err error) {
	if err != nil {
		trigger = "error:" + err.Error()
	}
	select {
	case o.calls <- trigger:
	default:
	}
}

func TestFaultNotificationLossPeriodicRouteConvergence(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	savePart := func(metric int) {
		part := dynamicconfig.NewPart("routes", "route-producer", nil, 1, now, now.Add(time.Hour))
		part.Spec.Digest = "fixture"
		part.Spec.Resources = []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "dynamic"}, Spec: api.IPv4RouteSpec{Type: "blackhole", Destination: "192.0.2.0/24", Metric: metric}}}
		record, err := codec.Encode(part)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertDynamicConfigPart(record); err != nil {
			t.Fatal(err)
		}
	}
	savePart(10)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := &Runner{Router: &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "notification-test"}}, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"ipv4-route"}, DryRunRoute: true}}
	controllers, _, err := runner.frameworkControllers(t.Context(), logger, eventedStore{Store: store, Bus: runner.Bus, Router: runner.Router}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 1 {
		t.Fatalf("controllers=%d", len(controllers))
	}
	controller := controllers[0].(framework.FuncController)
	if controller.Every != 30*time.Second {
		t.Fatalf("documented maximum period changed: %s", controller.Every)
	}
	// Only the scheduling period is shortened; this is the production closure
	// that rereads SQLite and constructs the real IPv4RouteController.
	controller.Every = time.Millisecond
	observer := deliveryObserver{calls: make(chan string, 128)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// No producer notification is ever sent to this bus. Periodic recovery
	// must work even when all relevant bus wakeups are lost.
	wakeups := bus.New()
	go func() {
		done <- (framework.Runner{Bus: wakeups, Observer: observer, Logger: logger}).Run(ctx, controller)
	}()
	defer func() { cancel(); <-done }()
	wait := func(wantMetric float64, periodic bool) {
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case trigger := <-observer.calls:
				if len(trigger) >= 6 && trigger[:6] == "error:" {
					t.Fatal(trigger)
				}
				status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "dynamic")
				if status["metric"] == wantMetric && (!periodic || trigger == "periodic") {
					return
				}
			case <-deadline.C:
				t.Fatalf("latest route did not converge: %#v", store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "dynamic"))
			}
		}
	}
	wait(10, false)
	savePart(20)
	savePart(30)
	wait(30, true)
	// Duplicated old payloads contain a stale generation and metric. Their
	// content cannot replace the latest authoritative desired state.
	stale := daemonapi.DaemonEvent{Type: dynamicconfig.PartChangedEvent, Attributes: map[string]string{"source": "route-producer", "digest": "old-digest", "generation": "0", "metric": "10"}}
	for i := 0; i < 2; i++ {
		if err := wakeups.Publish(ctx, stale); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for remaining := 2; remaining > 0; {
		select {
		case trigger := <-observer.calls:
			if trigger == dynamicconfig.PartChangedEvent {
				remaining--
			}
		case <-deadline.C:
			t.Fatal("duplicate notifications were not consumed")
		}
	}
	wait(30, true)
	if got := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "dynamic")["metric"]; got != float64(30) {
		t.Fatalf("stale notification changed route metric: %v", got)
	}
}

type failedStatusEventStore struct {
	err      error
	attempts int
}

func (s *failedStatusEventStore) RecordBusEvent(context.Context, daemonapi.DaemonEvent) (string, error) {
	s.attempts++
	return "", s.err
}

func TestFaultStatusSavedBeforeNotificationFailureRemainsAuthoritative(t *testing.T) {
	for _, merge := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "merge"}[merge], func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			failure := errors.New("injected event journal failure")
			journal := &failedStatusEventStore{err: failure}
			events := bus.NewWithStore(journal)
			observed := eventedStore{Store: store, Bus: events}
			status := map[string]any{"phase": "Bound", "gateway": "192.0.2.1"}
			save := func() error {
				if merge {
					return observed.MergeObjectStatus(api.NetAPIVersion, "DHCPv4Client", "uplink", status)
				}
				return observed.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "uplink", status)
			}
			if err := save(); !errors.Is(err, failure) {
				t.Fatalf("notification error=%v", err)
			}
			if got := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "uplink")["gateway"]; got != "192.0.2.1" {
				t.Fatalf("saved status lost: %v", got)
			}
			if err := save(); err != nil {
				t.Fatalf("identical status retry=%v", err)
			}
			if journal.attempts != 1 {
				t.Fatalf("identical status unexpectedly replayed history: %d", journal.attempts)
			}
			// A notification consumer recovers by rereading this persisted status.
			// It cannot recover the missing historical transition for an audit sink.
			route := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "default"}, Spec: api.IPv4RouteSpec{Destination: "0.0.0.0/0", Device: "lan0", GatewayFrom: api.StatusValueSourceSpec{Resource: "DHCPv4Client/uplink", Field: "gateway"}}}}}}
			consumer := IPv4RouteController{Router: route, Store: store, DryRun: true}
			if err := consumer.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "default")["gateway"]; got != "192.0.2.1" {
				t.Fatalf("rescan did not consume committed status: %v", got)
			}
		})
	}
}
