// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventrule"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type pausedStatusHistoryStore struct {
	*routerstate.SQLiteStore
	entered, release chan struct{}
}

func (s *pausedStatusHistoryStore) SaveObjectStatus(v, k, n string, status map[string]any) error {
	close(s.entered)
	<-s.release
	return s.SQLiteStore.SaveObjectStatus(v, k, n, status)
}

func (s *pausedStatusHistoryStore) SaveObjectStatusAndEvent(v, k, n string, status map[string]any, build routerstate.StatusEventBuilder) (*daemonapi.DaemonEvent, error) {
	close(s.entered)
	<-s.release
	return s.SQLiteStore.SaveObjectStatusAndEvent(v, k, n, status, build)
}
func (s *pausedStatusHistoryStore) MergeObjectStatusAndEvent(v, k, n string, status map[string]any, build routerstate.StatusEventBuilder) (*daemonapi.DaemonEvent, error) {
	close(s.entered)
	<-s.release
	return s.SQLiteStore.MergeObjectStatusAndEvent(v, k, n, status, build)
}

func TestFaultStatusEventUsesTransactionCurrentState(t *testing.T) {
	for _, scenario := range []string{"merge-latest-fields", "reject-stale-save", "same-status-stale-save"} {
		t.Run(scenario, func(t *testing.T) {
			stale := scenario != "merge-latest-fields"
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
			initial := eventedStore{Store: store}
			if err := initial.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": "Pending", "gateway": "192.0.2.1", "observedAt": now.Format(time.RFC3339Nano)}); err != nil {
				t.Fatal(err)
			}
			paused := &pausedStatusHistoryStore{SQLiteStore: store, entered: make(chan struct{}), release: make(chan struct{})}
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "rule"}, Spec: api.EventRuleSpec{Pattern: api.EventRulePatternSpec{Operator: eventrule.OperatorCount, Topic: "routerd.resource.status.changed", Threshold: 1}, Emit: api.EventRuleEmitSpec{Topic: "routerd.output"}}}}}}
			observed := eventedStore{Store: paused, Bus: bus.NewWithStore(paused), Router: router}
			updates := map[string]any{"phase": "Ready", "gateway": "192.0.2.1", "observedAt": now.Add(2 * time.Second).Format(time.RFC3339Nano)}
			if stale {
				updates["observedAt"] = now.Add(time.Second).Format(time.RFC3339Nano)
			}
			if scenario == "same-status-stale-save" {
				updates["phase"] = "Pending"
				updates["observedAt"] = now.Format(time.RFC3339Nano)
			}
			done := make(chan error, 1)
			go func() {
				if stale {
					done <- observed.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", updates)
				} else {
					done <- observed.MergeObjectStatus(api.NetAPIVersion, "Interface", "uplink", updates)
				}
			}()
			select {
			case <-paused.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("status writer did not reach transaction admission")
			}
			if err := store.MergeObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": "Intermediate", "gateway": "192.0.2.2", "concurrentOnly": "retained", "observedAt": now.Add(1500 * time.Millisecond).Format(time.RFC3339Nano)}); err != nil {
				t.Fatal(err)
			}
			close(paused.release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			events, err := store.ListEvents(routerstate.EventQuery{Topic: "routerd.resource.status.changed"})
			if err != nil {
				t.Fatal(err)
			}
			status := store.ObjectStatus(api.NetAPIVersion, "Interface", "uplink")
			if stale {
				if status["phase"] != "Intermediate" || len(events) != 0 {
					t.Fatalf("stale caller overwrote newer state: status=%#v events=%#v", status, events)
				}
				return
			}
			if status["phase"] != "Ready" || status["concurrentOnly"] != "retained" {
				t.Fatalf("merge lost latest state: %#v", status)
			}
			if len(events) != 1 || events[0].Attributes["previousPhase"] != "Intermediate" || !strings.Contains(events[0].Attributes["changedFields"].(string), "gateway") {
				t.Fatalf("event does not describe committed transition: %#v", events)
			}
		})
	}
}
