// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
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

func TestAllOf(t *testing.T) {
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorAllOf, Topics: []string{"routerd.a", "routerd.b"}})
	mustReconcile(t, controller, testEvent("routerd.a"))
	if got := b.Recent("routerd.out"); len(got) != 0 {
		t.Fatalf("events after first = %d", len(got))
	}
	mustReconcile(t, controller, testEvent("routerd.b"))
	if got := b.Recent("routerd.out"); len(got) != 1 {
		t.Fatalf("events = %d", len(got))
	}
}

func TestAnyOf(t *testing.T) {
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorAnyOf, Topics: []string{"routerd.a", "routerd.b"}})
	mustReconcile(t, controller, testEvent("routerd.b"))
	if got := b.Recent("routerd.out"); len(got) != 1 {
		t.Fatalf("events = %d", len(got))
	}
}

func TestMalformedRuleDoesNotStopOtherRules(t *testing.T) {
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorAnyOf, Topic: "routerd.a"})
	controller.Router.Spec.Resources = append([]api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"},
		Metadata: api.ObjectMeta{Name: "malformed"},
		Spec:     "not-an-event-rule-spec",
	}}, controller.Router.Spec.Resources...)
	if err := controller.Reconcile(context.Background(), testEvent("routerd.a")); err != nil {
		t.Fatalf("Reconcile returned malformed rule error: %v", err)
	}
	if got := len(b.Recent("routerd.out")); got != 1 {
		t.Fatalf("valid rule outputs = %d, want 1", got)
	}
}

func TestSequence(t *testing.T) {
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorSequence, Topics: []string{"routerd.a", "routerd.b"}})
	mustReconcile(t, controller, testEvent("routerd.a"))
	mustReconcile(t, controller, testEvent("routerd.b"))
	if got := b.Recent("routerd.out"); len(got) != 1 {
		t.Fatalf("events = %d", len(got))
	}
}

func TestWindow(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorWindow, Topic: "routerd.a", Window: "60s", Threshold: 3})
	for i := 0; i < 3; i++ {
		mustReconcile(t, controller, testEventAt("routerd.a", now.Add(time.Duration(i)*time.Second)))
	}
	if got := b.Recent("routerd.out"); len(got) != 1 {
		t.Fatalf("events = %d", len(got))
	}
}

func TestAbsence(t *testing.T) {
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorAbsence, Trigger: "routerd.trigger", Expected: "routerd.expected", Duration: "20ms"})
	mustReconcile(t, controller, testEvent("routerd.trigger"))
	waitForRecent(t, b, "routerd.out", 1)
	if got := timerCount(controller, "absence"); got != 0 {
		t.Fatalf("absence timers = %d", got)
	}
	controller.StopTimers()
}

func TestThrottle(t *testing.T) {
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorThrottle, Topic: "routerd.a", Interval: "1s"})
	mustReconcile(t, controller, testEventAt("routerd.a", now))
	mustReconcile(t, controller, testEventAt("routerd.a", now.Add(100*time.Millisecond)))
	mustReconcile(t, controller, testEventAt("routerd.a", now.Add(2*time.Second)))
	if got := b.Recent("routerd.out"); len(got) != 2 {
		t.Fatalf("events = %d", len(got))
	}
}

func TestDebounce(t *testing.T) {
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorDebounce, Topic: "routerd.a", Quiet: "20ms"})
	mustReconcile(t, controller, testEvent("routerd.a"))
	mustReconcile(t, controller, testEvent("routerd.a"))
	waitForRecent(t, b, "routerd.out", 1)
	if got := timerCount(controller, "debounce"); got != 0 {
		t.Fatalf("debounce timers = %d", got)
	}
	controller.StopTimers()
}

func TestCount(t *testing.T) {
	controller, b := testController(api.EventRulePatternSpec{Operator: OperatorCount, Topic: "routerd.a", Threshold: 2})
	mustReconcile(t, controller, testEvent("routerd.a"))
	mustReconcile(t, controller, testEvent("routerd.a"))
	if got := b.Recent("routerd.out"); len(got) != 1 || got[0].Attributes["count"] != "2" {
		t.Fatalf("events = %#v", got)
	}
}

func TestCorrelationStateIsBounded(t *testing.T) {
	controller, _ := testController(api.EventRulePatternSpec{Operator: OperatorThrottle, Topic: "routerd.a", CorrelateBy: "attributes.interface", Interval: "1h"})
	for i := 0; i < maxRuleCorrelationKeys+1; i++ {
		event := testEventAt("routerd.a", time.Unix(int64(i), 0).UTC())
		event.Attributes["interface"] = "if" + strconv.Itoa(i)
		mustReconcile(t, controller, event)
	}
	controller.mu.Lock()
	state := controller.state["rule"]
	gotLastSeen := len(state.lastSeen)
	gotLastEmit := len(state.lastEmit)
	controller.mu.Unlock()
	if gotLastSeen != maxRuleCorrelationKeys || gotLastEmit != maxRuleCorrelationKeys {
		t.Fatalf("state sizes lastSeen=%d lastEmit=%d", gotLastSeen, gotLastEmit)
	}
}

func TestStoredEventsRecoverMissedWakeupAndCursorPreventsRestartDuplicates(t *testing.T) {
	const eventCount = 129

	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	b := bus.NewWithStore(store)
	blockedEvents := &blockingEventStore{
		SQLiteStore: store,
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	controller := &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"},
			Metadata: api.ObjectMeta{Name: "rule"},
			Spec: api.EventRuleSpec{
				Pattern: api.EventRulePatternSpec{Operator: OperatorCount, Topic: "routerd.a", Threshold: eventCount},
				Emit:    api.EventRuleEmitSpec{Topic: "routerd.out"},
			},
		}}}},
		Bus:    b,
		Store:  store,
		Events: blockedEvents,
		Poll:   20 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	controller.Start(ctx)
	select {
	case <-blockedEvents.entered:
	case <-time.After(time.Second):
		t.Fatal("stored event drain did not start")
	}
	// The drain is blocked while more events than the 128-entry bus
	// subscription buffer are published. Wake-ups are therefore dropped, but
	// every event remains recoverable from the store.
	for i := 0; i < eventCount; i++ {
		if err := b.Publish(context.Background(), testEvent("routerd.a")); err != nil {
			t.Fatal(err)
		}
	}
	close(blockedEvents.release)
	waitForRecent(t, b, "routerd.out", 1)
	cancel()
	controller.StopTimers()

	restarted := &Controller{
		Router: controller.Router,
		Bus:    b,
		Store:  store,
		Events: store,
		Poll:   20 * time.Millisecond,
	}
	restartCtx, restartCancel := context.WithCancel(context.Background())
	defer restartCancel()
	restarted.Start(restartCtx)
	time.Sleep(100 * time.Millisecond)
	if got := len(b.Recent("routerd.out")); got != 1 {
		t.Fatalf("restart replayed processed events: outputs = %d, want 1", got)
	}
}

type blockingEventStore struct {
	*routerstate.SQLiteStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockingEventStore) ListEvents(query routerstate.EventQuery) ([]routerstate.StoredEvent, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return s.SQLiteStore.ListEvents(query)
}

func testController(pattern api.EventRulePatternSpec) (*Controller, *bus.Bus) {
	b := bus.New()
	controller := &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"},
				Metadata: api.ObjectMeta{Name: "rule"},
				Spec: api.EventRuleSpec{
					Pattern: pattern,
					Emit:    api.EventRuleEmitSpec{Topic: "routerd.out", Attributes: map[string]string{"input": "${event.type}"}},
				},
			},
		}}},
		Bus:   b,
		Store: mapStore{},
	}
	return controller, b
}

func mustReconcile(t *testing.T, controller *Controller, event daemonapi.DaemonEvent) {
	t.Helper()
	if err := controller.Reconcile(context.Background(), event); err != nil {
		t.Fatal(err)
	}
}

func waitForRecent(t *testing.T, b *bus.Bus, topic string, want int) {
	t.Helper()
	// These tests assert eventual timer/store recovery, not sub-second
	// throughput. SQLite cursor persistence can take several seconds under
	// concurrent package tests, so keep a bounded but scheduler-tolerant wait.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := len(b.Recent(topic)); got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("events = %d", len(b.Recent(topic)))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func timerCount(controller *Controller, kind string) int {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	state := controller.state["rule"]
	if state == nil {
		return 0
	}
	switch kind {
	case "absence":
		return len(state.absence)
	case "debounce":
		return len(state.debounce)
	default:
		return 0
	}
}

func testEvent(topic string) daemonapi.DaemonEvent {
	return testEventAt(topic, time.Now().UTC())
}

func testEventAt(topic string, at time.Time) daemonapi.DaemonEvent {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test", Kind: "test", Instance: "test"}, topic, daemonapi.SeverityInfo)
	event.Time = at
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "Test", Name: "test"}
	event.Attributes = map[string]string{"interface": "wan"}
	return event
}
