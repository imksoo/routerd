package conntrackobserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/conntrack"
)

type testStore struct {
	status map[string]map[string]any
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
