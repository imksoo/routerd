// SPDX-License-Identifier: BSD-3-Clause

package eventconsumer

import (
	"context"
	"errors"
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDrainAdvancesCursorOnlyAfterSuccessfulProcessing(t *testing.T) {
	store := &fakeStore{events: []routerstate.StoredEvent{
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
		cursor: 1,
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

type fakeStore struct {
	cursor int64
	events []routerstate.StoredEvent
}

func (s *fakeStore) EventConsumerCursor(string) (int64, error) {
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
