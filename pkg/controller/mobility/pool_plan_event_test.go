// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type poolPlanEventStore struct {
	parts     map[string][]routerstate.DynamicConfigPartRecord
	upsertErr error
}

func (s *poolPlanEventStore) ListFederationEvents(string, bool, int64) ([]routerstate.EventRecord, error) {
	return nil, nil
}

func (s *poolPlanEventStore) RecordFederationEvent(routerstate.EventRecord) error { return nil }

func (s *poolPlanEventStore) UpsertDynamicConfigPart(next routerstate.DynamicConfigPartRecord) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
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

func (s *poolPlanEventStore) GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error) {
	return append([]routerstate.DynamicConfigPartRecord(nil), s.parts[source]...), nil
}

func (s *poolPlanEventStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	var out []routerstate.DynamicConfigPartRecord
	for _, parts := range s.parts {
		out = append(out, parts...)
	}
	return out, nil
}

func (s *poolPlanEventStore) ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error) {
	return nil, nil
}

func (s *poolPlanEventStore) SaveObjectStatus(string, string, string, map[string]any) error {
	return nil
}

func (s *poolPlanEventStore) ObjectStatus(string, string, string) map[string]any {
	return map[string]any{}
}

func TestControllerUpsertBGPPlanPublishesChangedTypedPlanAfterPersist(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := &poolPlanEventStore{}
	eventsBus := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := eventsBus.Subscribe(ctx, bus.Subscription{Topics: []string{PoolPlanChangedEvent}}, 4)
	defer unsubscribe()
	controller := Controller{Store: store, Bus: eventsBus}

	first := poolPlanEventIntent("cloudedge/10.77.60.9", "10.77.60.9/32")
	if err := controller.upsertBGPPlan(ctx, "cloudedge", "router-a", nil, poolPlanEventDataplane(first), nil, now); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	event := requirePoolPlanChangedEvent(t, events)
	source := DynamicSource("cloudedge", "router-a")
	parts, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil || len(parts) != 1 {
		t.Fatalf("persisted parts = %#v, err = %v", parts, err)
	}
	if event.Resource == nil || event.Resource.APIVersion != api.MobilityAPIVersion || event.Resource.Kind != "MobilityPool" || event.Resource.Name != "cloudedge" {
		t.Fatalf("event resource = %#v", event.Resource)
	}
	if event.Attributes["source"] != source || event.Attributes["digest"] != parts[0].Digest {
		t.Fatalf("event attributes = %#v, persisted part = %#v", event.Attributes, parts[0])
	}

	// Refreshing the same desired plan only extends its lease; it is not a
	// dataplane change and must not make SAM reconcile again.
	if err := controller.upsertBGPPlan(ctx, "cloudedge", "router-a", nil, poolPlanEventDataplane(first), nil, now.Add(time.Second)); err != nil {
		t.Fatalf("unchanged upsert: %v", err)
	}
	requireNoPoolPlanChangedEvent(t, events)

	second := poolPlanEventIntent("cloudedge/10.77.60.10", "10.77.60.10/32")
	if err := controller.upsertBGPPlan(ctx, "cloudedge", "router-a", nil, poolPlanEventDataplane(second), nil, now.Add(2*time.Second)); err != nil {
		t.Fatalf("changed upsert: %v", err)
	}
	event = requirePoolPlanChangedEvent(t, events)
	if event.Attributes["digest"] == parts[0].Digest {
		t.Fatalf("changed plan kept prior digest %q", event.Attributes["digest"])
	}
}

func TestControllerUpsertBGPPlanDoesNotPublishBeforeFailedPersist(t *testing.T) {
	store := &poolPlanEventStore{upsertErr: errors.New("write failed")}
	eventsBus := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := eventsBus.Subscribe(ctx, bus.Subscription{Topics: []string{PoolPlanChangedEvent}}, 1)
	defer unsubscribe()
	controller := Controller{Store: store, Bus: eventsBus}

	err := controller.upsertBGPPlan(ctx, "cloudedge", "router-a", nil, poolPlanEventDataplane(poolPlanEventIntent("cloudedge/10.77.60.9", "10.77.60.9/32")), nil, time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("upsert succeeded, want error")
	}
	requireNoPoolPlanChangedEvent(t, events)
}

func poolPlanEventIntent(id, address string) dynamicconfig.LocalCaptureIntent {
	return dynamicconfig.LocalCaptureIntent{
		ID:               id,
		PoolRef:          "cloudedge",
		Address:          address,
		Disposition:      dynamicconfig.CaptureDesired,
		CaptureType:      "proxy-arp",
		CaptureInterface: "ens3",
	}
}

func poolPlanEventDataplane(captures ...dynamicconfig.LocalCaptureIntent) dynamicconfig.MobilityDataplanePlan {
	return dynamicconfig.MobilityDataplanePlan{PoolPrefix: "10.77.60.0/24", Captures: captures}
}

func requirePoolPlanChangedEvent(t *testing.T, events <-chan daemonapi.DaemonEvent) daemonapi.DaemonEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	default:
		t.Fatal("missing typed MobilityPool plan-changed event")
		return daemonapi.DaemonEvent{}
	}
}

func requireNoPoolPlanChangedEvent(t *testing.T, events <-chan daemonapi.DaemonEvent) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected typed MobilityPool plan-changed event: %#v", event)
	default:
	}
}
