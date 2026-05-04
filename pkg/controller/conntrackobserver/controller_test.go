package conntrackobserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/conntrack"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"
)

type testStore struct {
	status map[string]map[string]any
}

func TestControllerRecordsTrafficFlowLog(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	maxPath := filepath.Join(dir, "max")
	entriesPath := filepath.Join(dir, "entries")
	flowPath := filepath.Join(dir, "traffic-flows.db")
	if err := os.WriteFile(countPath, []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maxPath, []byte("10\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entriesPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	store := &testStore{}
	controller := &Controller{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "TrafficFlowLog"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec:     api.TrafficFlowLogSpec{Enabled: true, Path: flowPath, Source: "conntrack"},
		}}}},
		Bus:   bus.New(),
		Store: store,
		Paths: conntrack.Paths{Entries: entriesPath, Count: countPath, Max: maxPath},
		NAPT: func(limit int) (*observe.NAPTTable, error) {
			return &observe.NAPTTable{Entries: []observe.NAPTTableEntry{{
				Protocol: "tcp",
				Original: observe.ConntrackTuple{Source: "172.18.0.10", SourcePort: "12345", Destination: "1.1.1.1", DestinationPort: "443"},
				Reply:    observe.ConntrackTuple{Source: "1.1.1.1", SourcePort: "443", Destination: "172.18.0.10", DestinationPort: "12345"},
			}}}, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	flowLog, err := logstore.OpenTrafficFlowLog(flowPath)
	if err != nil {
		t.Fatal(err)
	}
	defer flowLog.Close()
	flows, err := flowLog.List(context.Background(), logstore.TrafficFlowFilter{Client: "172.18.0.10", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(flows) != 1 || flows[0].PeerAddress != "1.1.1.1" || flows[0].PeerPort != 443 {
		t.Fatalf("flows = %#v", flows)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "TrafficFlowLog", "default")
	if status["phase"] != "Observed" || status["activeFlows"] != 1 {
		t.Fatalf("traffic status = %#v", status)
	}
}

func (s *testStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.status == nil {
		s.status = map[string]map[string]any{}
	}
	s.status[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s *testStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if s.status == nil {
		return map[string]any{}
	}
	if status := s.status[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestControllerRecordsStatusWithoutSnapshotEvent(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	maxPath := filepath.Join(dir, "max")
	entriesPath := filepath.Join(dir, "entries")
	if err := os.WriteFile(countPath, []byte("8\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maxPath, []byte("10\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entriesPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	store := &testStore{}
	eventBus := bus.New()
	controller := &Controller{
		Bus:            eventBus,
		Store:          store,
		Paths:          conntrack.Paths{Entries: entriesPath, Count: countPath, Max: maxPath},
		ThresholdRatio: 0.9,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "ConntrackObserver", "default")
	if status["phase"] != "Observed" || status["count"] != 8 || status["max"] != 10 {
		t.Fatalf("status = %#v", status)
	}
	if events := eventBus.Recent("routerd.conntrack.snapshot"); len(events) != 0 {
		t.Fatalf("snapshot events = %#v", events)
	}
}
