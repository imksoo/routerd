// SPDX-License-Identifier: BSD-3-Clause

package eventsubscription

import (
	"context"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestProcessBatchPublishesDurableDynamicPartChangeOnlyForChangedOutput(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	store := &eventSubscriptionTestStore{}
	eventsBus := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := eventsBus.Subscribe(ctx, bus.Subscription{Topics: []string{dynamicconfig.PartChangedEvent}}, 2)
	defer unsubscribe()
	controller := &Controller{
		Router: &api.Router{},
		Store:  store,
		Bus:    eventsBus,
		Now:    func() time.Time { return now },
		PluginRunner: func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error) {
			return eventSubscriptionTestResult(now), routerplugin.RunOutcome{}, nil
		},
	}
	resource := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventSubscription"},
		Metadata: api.ObjectMeta{Name: "cloud-routes"},
	}
	spec := api.EventSubscriptionSpec{Trigger: api.EventSubscriptionTrigger{PluginRef: "cloud-plugin"}}
	batch := []routerstate.EventRecord{{ID: "event-1", Group: "edge", Type: "route.changed"}}
	if err := controller.processBatch(ctx, resource, spec, api.PluginSpec{}, batch, now); err != nil {
		t.Fatalf("first processBatch: %v", err)
	}
	first := requireDynamicPartChangedEvent(t, events)
	if first.Resource == nil || first.Resource.Kind != "EventSubscription" || first.Resource.Name != "cloud-routes" {
		t.Fatalf("event resource = %#v", first.Resource)
	}
	if first.Attributes["source"] == "" || first.Attributes["digest"] == "" {
		t.Fatalf("event attributes = %#v", first.Attributes)
	}

	// Replaying the same event is normally skipped by the durable run row.
	// Clear only the fake's run row here to exercise the upsert path itself:
	// unchanged plugin output may renew its lease but must not wake every
	// consumer a second time.
	delete(store.runs, "EventSubscription/cloud-routes/event-1")
	if err := controller.processBatch(ctx, resource, spec, api.PluginSpec{}, batch, now.Add(time.Second)); err != nil {
		t.Fatalf("unchanged processBatch: %v", err)
	}
	requireNoDynamicPartChangedEvent(t, events)
}

func eventSubscriptionTestResult(now time.Time) routerplugin.PluginResult {
	return routerplugin.PluginResult{
		TypeMeta: api.TypeMeta{APIVersion: routerplugin.PluginAPIVersion, Kind: "PluginResult"},
		Status: routerplugin.PluginResultStatus{
			ObservedAt: now,
			TTL:        "5m",
			Resources: []api.Resource{{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
				Metadata: api.ObjectMeta{Name: "learned-route"},
				Spec:     api.IPv4RouteSpec{Destination: "198.51.100.0/24", Gateway: "192.0.2.1"},
			}},
		},
	}
}

func requireDynamicPartChangedEvent(t *testing.T, events <-chan daemonapi.DaemonEvent) daemonapi.DaemonEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	default:
		t.Fatal("missing DynamicConfigPart change event")
		return daemonapi.DaemonEvent{}
	}
}

func requireNoDynamicPartChangedEvent(t *testing.T, events <-chan daemonapi.DaemonEvent) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected DynamicConfigPart change event: %#v", event)
	default:
	}
}

type eventSubscriptionTestRun struct {
	status   string
	attempts int
}

type eventSubscriptionTestStore struct {
	statuses map[string]map[string]any
	runs     map[string]eventSubscriptionTestRun
	parts    map[string][]routerstate.DynamicConfigPartRecord
	nextRun  int64
}

func (s *eventSubscriptionTestStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.statuses == nil {
		s.statuses = map[string]map[string]any{}
	}
	s.statuses[apiVersion+"/"+kind+"/"+name] = copyEventSubscriptionStatus(status)
	return nil
}

func (s *eventSubscriptionTestStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return copyEventSubscriptionStatus(s.statuses[apiVersion+"/"+kind+"/"+name])
}

func (s *eventSubscriptionTestStore) ListFederationEvents(string, bool, int64) ([]routerstate.EventRecord, error) {
	return nil, nil
}

func (s *eventSubscriptionTestStore) SubscriptionRunStatus(subscription, eventID string) (string, int, bool, error) {
	run, found := s.runs[subscription+"/"+eventID]
	return run.status, run.attempts, found, nil
}

func (s *eventSubscriptionTestStore) UpsertSubscriptionRunStart(subscription, eventID, _ string, _ string) error {
	if s.runs == nil {
		s.runs = map[string]eventSubscriptionTestRun{}
	}
	key := subscription + "/" + eventID
	run := s.runs[key]
	run.status = "pending"
	run.attempts++
	s.runs[key] = run
	return nil
}

func (s *eventSubscriptionTestStore) MarkSubscriptionRunResult(subscription, eventID, status, _ string, _ int64, _ string) error {
	key := subscription + "/" + eventID
	run := s.runs[key]
	run.status = status
	s.runs[key] = run
	return nil
}

func (s *eventSubscriptionTestStore) UpsertDynamicConfigPart(next routerstate.DynamicConfigPartRecord) error {
	if s.parts == nil {
		s.parts = map[string][]routerstate.DynamicConfigPartRecord{}
	}
	parts := s.parts[next.Source]
	for i := range parts {
		if parts[i].Generation == next.Generation {
			parts[i] = next
			s.parts[next.Source] = parts
			return nil
		}
	}
	s.parts[next.Source] = append(parts, next)
	return nil
}

func (s *eventSubscriptionTestStore) GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error) {
	return append([]routerstate.DynamicConfigPartRecord(nil), s.parts[source]...), nil
}

func (s *eventSubscriptionTestStore) RecordPluginRun(routerstate.PluginRunRecord) (int64, error) {
	s.nextRun++
	return s.nextRun, nil
}

func (s *eventSubscriptionTestStore) CompletePluginRun(int64, time.Time, *int, string, string, string, string) error {
	return nil
}

func copyEventSubscriptionStatus(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
