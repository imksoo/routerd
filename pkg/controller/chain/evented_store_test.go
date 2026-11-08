// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestMobilityStoreForwardsRecordBusEventThroughProductionBus(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "routerd.db")
	raw, err := routerstate.Open(statePath)
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}
	rawStore, ok := raw.(*routerstate.SQLiteStore)
	if !ok {
		t.Fatalf("state store = %T, want *SQLiteStore", raw)
	}
	t.Cleanup(func() {
		_ = rawStore.Close()
	})
	eventBus := bus.NewWithStore(rawStore)
	store := mobilityStore{
		evented: eventedStore{Store: rawStore, Bus: eventBus},
		data:    rawStore,
	}
	recorder, ok := any(store).(interface {
		RecordBusEvent(context.Context, daemonapi.DaemonEvent) (string, error)
	})
	if !ok {
		t.Fatalf("mobilityStore does not expose RecordBusEvent")
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "mobility"}, "routerd.mobility.holder.transition", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	event.Attributes = map[string]string{
		"transitionKind": "seize-complete",
		"address":        "10.88.60.10/32",
	}
	if _, err := recorder.RecordBusEvent(context.Background(), event); err != nil {
		t.Fatalf("record bus event: %v", err)
	}
	events, err := rawStore.ListEvents(routerstate.EventQuery{Topic: "routerd.mobility.holder.transition", Limit: 10})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("stored events = %d, want 1", len(events))
	}
	if got := events[0].Attributes["transitionKind"]; got != "seize-complete" {
		t.Fatalf("transitionKind = %q, want seize-complete", got)
	}
	if got := events[0].ResourceKind + "/" + events[0].ResourceName; got != "MobilityPool/cloudedge" {
		t.Fatalf("resource = %q, want MobilityPool/cloudedge", got)
	}
}
