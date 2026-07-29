// SPDX-License-Identifier: BSD-3-Clause

package eventconsumer

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDrainAdvancesCursorOnlyAfterSuccessfulProcessing(t *testing.T) {
	store := &fakeStore{initialized: true, events: []routerstate.StoredEvent{
		{ID: 1, Topic: "routerd.first"},
		{ID: 2, Topic: "routerd.second"},
	}}
	wantErr := errors.New("consumer failed")
	err := Drain(context.Background(), store, "test", func(_ context.Context, event daemonapi.DaemonEvent) error {
		if event.Type == "routerd.second" {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Drain error = %v, want %v", err, wantErr)
	}
	if store.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", store.cursor)
	}
}

func TestDrainRestoresCursorAndDoesNotReplay(t *testing.T) {
	store := &fakeStore{
		cursor:      1,
		initialized: true,
		events: []routerstate.StoredEvent{
			{ID: 1, Topic: "routerd.first"},
			{ID: 2, Topic: "routerd.second"},
		},
	}
	var processed []string
	if err := Drain(context.Background(), store, "test", func(_ context.Context, event daemonapi.DaemonEvent) error {
		processed = append(processed, event.Type)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(processed) != 1 || processed[0] != "routerd.second" {
		t.Fatalf("processed = %v, want [routerd.second]", processed)
	}
	if store.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", store.cursor)
	}
}

func TestDrainInitializesCursorAtLatestEvent(t *testing.T) {
	store := &fakeStore{events: []routerstate.StoredEvent{
		{ID: 1, Topic: "routerd.old"},
		{ID: 2, Topic: "routerd.latest"},
	}}
	var processed []string
	if err := Drain(context.Background(), store, "new-consumer", func(_ context.Context, event daemonapi.DaemonEvent) error {
		processed = append(processed, event.Type)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(processed) != 0 {
		t.Fatalf("initial drain replayed history: %v", processed)
	}
	if store.cursor != 2 {
		t.Fatalf("initial cursor = %d, want MAX(id)=2", store.cursor)
	}
}

func TestSQLiteInitialCursorUsesCurrentMaxID(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	eventBus := bus.NewWithStore(store)
	for _, topic := range []string{"routerd.old", "routerd.latest"} {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test", Kind: "test"}, topic, daemonapi.SeverityInfo)
		if err := eventBus.Publish(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	processed := 0
	if err := Drain(context.Background(), store, "new-consumer", func(context.Context, daemonapi.DaemonEvent) error {
		processed++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if processed != 0 {
		t.Fatalf("initial drain replayed %d historical events", processed)
	}
	cursor, err := store.LoadOrInitializeEventConsumerCursor("new-consumer")
	if err != nil {
		t.Fatal(err)
	}
	if cursor != 2 {
		t.Fatalf("initial cursor = %d, want MAX(id)=2", cursor)
	}
}

func TestBackoffSuppressesRetriesAndCapsDelay(t *testing.T) {
	now := time.Unix(100, 0)
	backoff := Backoff{
		Initial: time.Second,
		Max:     4 * time.Second,
		Now:     func() time.Time { return now },
	}
	for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second} {
		if !backoff.Ready() {
			t.Fatal("backoff was not ready before failure")
		}
		backoff.Failure()
		if backoff.Ready() {
			t.Fatal("backoff allowed an immediate retry")
		}
		now = now.Add(delay)
		if !backoff.Ready() {
			t.Fatalf("backoff was not ready after %s", delay)
		}
	}
	backoff.Success()
	if !backoff.Ready() {
		t.Fatal("successful drain did not reset backoff")
	}
}

type fakeStore struct {
	cursor      int64
	initialized bool
	events      []routerstate.StoredEvent
}

func (s *fakeStore) LoadOrInitializeEventConsumerCursor(string) (int64, error) {
	if !s.initialized {
		for _, event := range s.events {
			if event.ID > s.cursor {
				s.cursor = event.ID
			}
		}
		s.initialized = true
	}
	return s.cursor, nil
}

func (s *fakeStore) SaveEventConsumerCursor(_ string, cursor int64) error {
	s.cursor = cursor
	return nil
}

func (s *fakeStore) ListEvents(query routerstate.EventQuery) ([]routerstate.StoredEvent, error) {
	var events []routerstate.StoredEvent
	for _, event := range s.events {
		if event.ID > query.SinceID {
			events = append(events, event)
		}
	}
	return events, nil
}
