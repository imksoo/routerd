package eventrule

import (
	"context"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
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
	time.Sleep(80 * time.Millisecond)
	if got := b.Recent("routerd.out"); len(got) != 1 {
		t.Fatalf("events = %d", len(got))
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
	time.Sleep(80 * time.Millisecond)
	if got := b.Recent("routerd.out"); len(got) != 1 {
		t.Fatalf("events = %d", len(got))
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
