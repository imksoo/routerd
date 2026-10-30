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

// Use the production transaction and the same journal as the durable bus.
type ruleTestStore struct{ *routerstate.SQLiteStore }

func (s ruleTestStore) CommitRule(ctx context.Context, old, next routerstate.EventRuleCheckpoint, out *daemonapi.DaemonEvent, tip bool) (routerstate.EventRuleCheckpoint, []daemonapi.DaemonEvent, error) {
	return s.CommitEventRule(ctx, old, next, out, tip, nil, nil)
}

func TestAllOf(t *testing.T) {
	assertInputs(t, api.EventRulePatternSpec{Operator: OperatorAllOf, Topics: []string{"routerd.a", "routerd.b"}}, []string{"routerd.a", "routerd.b"}, []int{0, 1})
}
func TestAnyOf(t *testing.T) {
	assertInputs(t, api.EventRulePatternSpec{Operator: OperatorAnyOf, Topics: []string{"routerd.a", "routerd.b"}}, []string{"routerd.b"}, []int{1})
}
func TestSequence(t *testing.T) {
	assertInputs(t, api.EventRulePatternSpec{Operator: OperatorSequence, Topics: []string{"routerd.a", "routerd.b"}}, []string{"routerd.a", "routerd.b"}, []int{0, 1})
}
func assertInputs(t *testing.T, pattern api.EventRulePatternSpec, topics []string, counts []int) {
	t.Helper()
	c, b := testController(t, pattern)
	for i, topic := range topics {
		mustReconcile(t, c, testEvent(topic))
		if got := len(b.Recent("routerd.out")); got != counts[i] {
			t.Fatalf("after %s outputs=%d want=%d", topic, got, counts[i])
		}
	}
}
func TestMalformedRuleDoesNotStopOtherRules(t *testing.T) {
	c, b := testController(t, api.EventRulePatternSpec{Operator: OperatorAnyOf, Topic: "routerd.a"})
	resources := append([]api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "malformed"}, Spec: api.InterfaceSpec{}}}, c.Router.Spec.Resources...)
	candidate := &Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: resources}}, Bus: b, Store: c.Store}
	if err := candidate.Prepare(t.Context()); err == nil {
		t.Fatal("malformed candidate admitted")
	}
	mustReconcile(t, c, testEvent("routerd.a"))
	if got := len(b.Recent("routerd.out")); got != 1 {
		t.Fatalf("selected valid rule outputs=%d", got)
	}
}
func TestWindow(t *testing.T) {
	rule, cp := preparedTestRule(t, api.EventRulePatternSpec{Operator: OperatorWindow, Topic: "routerd.a", Window: "60s", Threshold: 3})
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		out, err := evaluate(rule, &cp, testEventAt("routerd.a", now.Add(time.Duration(i)*time.Second)))
		if err != nil || (out != nil) != (i == 2) {
			t.Fatalf("window event%d: output=%v err=%v", i, out, err)
		}
	}
	if cp.FireCount != 1 {
		t.Fatalf("window fires=%d", cp.FireCount)
	}
}
func TestThrottle(t *testing.T) {
	rule, cp := preparedTestRule(t, api.EventRulePatternSpec{Operator: OperatorThrottle, Topic: "routerd.a", Interval: "1s"})
	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	for i, delta := range []time.Duration{0, 100 * time.Millisecond, 2 * time.Second} {
		out, err := evaluate(rule, &cp, testEventAt("routerd.a", now.Add(delta)))
		if err != nil || (out != nil) != (i != 1) {
			t.Fatalf("throttle event%d: output=%v err=%v", i, out, err)
		}
	}
	if cp.FireCount != 2 {
		t.Fatalf("throttle fires=%d", cp.FireCount)
	}
}
func TestAbsence(t *testing.T) {
	c, b := testController(t, api.EventRulePatternSpec{Operator: OperatorAbsence, Trigger: "routerd.trigger", Expected: "routerd.expected", Duration: "20ms"})
	mustReconcile(t, c, testEvent("routerd.trigger"))
	waitForRecent(t, b, "routerd.out", 1)
	assertTimerRetired(t, c)
}
func TestDebounce(t *testing.T) {
	c, b := testController(t, api.EventRulePatternSpec{Operator: OperatorDebounce, Topic: "routerd.a", Quiet: "20ms"})
	mustReconcile(t, c, testEvent("routerd.a"))
	mustReconcile(t, c, testEvent("routerd.a"))
	waitForRecent(t, b, "routerd.out", 1)
	assertTimerRetired(t, c)
}
func TestCount(t *testing.T) {
	c, b := testController(t, api.EventRulePatternSpec{Operator: OperatorCount, Topic: "routerd.a", Threshold: 2})
	mustReconcile(t, c, testEvent("routerd.a"))
	mustReconcile(t, c, testEvent("routerd.a"))
	if got := b.Recent("routerd.out"); len(got) != 1 || got[0].Attributes["count"] != "2" {
		t.Fatalf("events=%#v", got)
	}
}
func TestCorrelationStateIsBounded(t *testing.T) {
	rule, cp := preparedTestRule(t, api.EventRulePatternSpec{Operator: OperatorThrottle, Topic: "routerd.a", CorrelateBy: "attributes.interface", Interval: "1h"})
	for i := 0; i < maxRuleCorrelationKeys+1; i++ {
		event := testEventAt("routerd.a", time.Unix(int64(i+1), 0).UTC())
		event.Attributes["interface"] = "if" + strconv.Itoa(i)
		if _, err := evaluate(rule, &cp, event); err != nil {
			t.Fatal(err)
		}
	}
	if len(cp.Correlations) != maxRuleCorrelationKeys || cp.FireCount != maxRuleCorrelationKeys+1 {
		t.Fatalf("correlations=%d fires=%d", len(cp.Correlations), cp.FireCount)
	}
	if _, found := cp.Correlations["if0"]; found {
		t.Fatal("oldest correlation not evicted")
	}
	if _, found := cp.Correlations["if"+strconv.Itoa(maxRuleCorrelationKeys)]; !found {
		t.Fatal("newest correlation evicted")
	}
}

type blockedRuleCommitStore struct {
	ruleTestStore
	entered, release chan struct{}
	once             sync.Once
	onlyOutput       bool
}

func (s *blockedRuleCommitStore) CommitRule(ctx context.Context, old, next routerstate.EventRuleCheckpoint, out *daemonapi.DaemonEvent, tip bool) (routerstate.EventRuleCheckpoint, []daemonapi.DaemonEvent, error) {
	if !s.onlyOutput && next.Cursor > old.Cursor || out != nil {
		s.once.Do(func() { close(s.entered); <-s.release })
	}
	return s.ruleTestStore.CommitRule(ctx, old, next, out, tip)
}
func TestStoredEventsRecoverMissedWakeupAndCursorPreventsRestartDuplicates(t *testing.T) {
	const eventCount = 130
	c, b := newTestController(t, api.EventRulePatternSpec{Operator: OperatorCount, Topic: "routerd.a", Threshold: eventCount})
	db := c.Store.EventJournal()
	blocked := &blockedRuleCommitStore{ruleTestStore: ruleTestStore{db}, entered: make(chan struct{}), release: make(chan struct{})}
	c.Store = blocked
	var release sync.Once
	stop := startTestController(t, c)
	t.Cleanup(func() { release.Do(func() { close(blocked.release) }) })
	if err := b.PublishDurable(t.Context(), testEvent("routerd.a")); err != nil {
		t.Fatal(err)
	}
	waitForDrain(t, blocked.entered)
	// Pause before the real transaction while durable inputs overflow the
	// bounded wake-up queue. Recovery must read every input from SQLite.
	for i := 1; i < eventCount; i++ {
		if err := b.PublishDurable(t.Context(), testEvent("routerd.a")); err != nil {
			t.Fatal(err)
		}
	}
	release.Do(func() { close(blocked.release) })
	waitForRecent(t, b, "routerd.out", 1)
	stop()
	cp, err := db.ReadEventRule(t.Context(), "rule")
	if err != nil || cp.FireCount != 1 {
		t.Fatalf("checkpoint before restart: %+v %v", cp, err)
	}
	restarted := &Controller{Router: c.Router, Bus: b, Store: ruleTestStore{db}, Poll: time.Millisecond}
	startTestController(t, restarted)
	if err := restarted.drainStoredEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := len(b.Recent("routerd.out")); got != 1 {
		t.Fatalf("restart duplicated processed inputs: outputs=%d", got)
	}
	rows, err := db.ListEvents(routerstate.EventQuery{Topic: "routerd.out", Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("durable outputs=%d err=%v", len(rows), err)
	}
}

func newTestController(t *testing.T, pattern api.EventRulePatternSpec) (*Controller, *bus.Bus) {
	t.Helper()
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	b := bus.NewWithStore(db)
	return &Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{testRuleResource(pattern)}}}, Bus: b, Store: ruleTestStore{db}, Poll: time.Millisecond}, b
}
func testController(t *testing.T, pattern api.EventRulePatternSpec) (*Controller, *bus.Bus) {
	t.Helper()
	c, b := newTestController(t, pattern)
	startTestController(t, c)
	return c, b
}
func startTestController(t *testing.T, c *Controller) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done, err := c.Start(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); waitForDrain(t, done) }) }
	t.Cleanup(stop)
	return stop
}
func testRuleResource(pattern api.EventRulePatternSpec) api.Resource {
	return api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "rule"}, Spec: api.EventRuleSpec{Pattern: pattern, Emit: api.EventRuleEmitSpec{Topic: "routerd.out", Attributes: map[string]string{"input": "${event.type}"}}}}
}
func preparedTestRule(t *testing.T, pattern api.EventRulePatternSpec) (preparedRule, routerstate.EventRuleCheckpoint) {
	t.Helper()
	resource := testRuleResource(pattern)
	spec, err := resource.EventRuleSpec()
	if err != nil {
		t.Fatal(err)
	}
	rule, err := prepareRule(resource, spec)
	if err != nil {
		t.Fatal(err)
	}
	return rule, routerstate.EventRuleCheckpoint{Version: 1, Name: "rule", Definition: rule.definition.Digest, Correlations: map[string]routerstate.EventRuleCorrelation{}}
}
func mustReconcile(t *testing.T, c *Controller, event daemonapi.DaemonEvent) {
	t.Helper()
	if err := c.Bus.PublishDurable(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := c.drainStoredEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func waitForDrain(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("owned EventRule operation did not acknowledge completion")
	}
}
func waitForRecent(t *testing.T, b *bus.Bus, topic string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(b.Recent(topic)) != want {
		if time.Now().After(deadline) {
			t.Fatalf("events=%d want=%d", len(b.Recent(topic)), want)
		}
		time.Sleep(time.Millisecond)
	}
}
func assertTimerRetired(t *testing.T, c *Controller) {
	t.Helper()
	cp, err := c.Store.EventJournal().ReadEventRule(t.Context(), "rule")
	if err != nil {
		t.Fatal(err)
	}
	if cp.FireCount != 1 {
		t.Fatalf("timer fires=%d", cp.FireCount)
	}
	for key, value := range cp.Correlations {
		if value.Timer != nil {
			t.Fatalf("committed timer %s was not retired", key)
		}
	}
}
func testEvent(topic string) daemonapi.DaemonEvent { return testEventAt(topic, time.Now().UTC()) }
func testEventAt(topic string, at time.Time) daemonapi.DaemonEvent {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test", Kind: "test", Instance: "test"}, topic, daemonapi.SeverityInfo)
	event.Time = at
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "Test", Name: "test"}
	event.Attributes = map[string]string{"interface": "wan"}
	return event
}
