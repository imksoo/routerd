// SPDX-License-Identifier: BSD-3-Clause

package bus

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFaultPublishRecordedRetainsJournalIdentityWithoutDuplicateInsert(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	b := NewWithStore(store)
	if !b.PersistsTo(store) || New().PersistsTo(store) {
		t.Fatal("journal identity mismatch")
	}
	ch, cancel := b.Subscribe(context.Background(), Subscription{}, 1)
	defer cancel()
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "fixture"}, "routerd.fixture", daemonapi.SeverityInfo)
	if err := b.PublishRecorded(t.Context(), event); err == nil {
		t.Fatal("unrecorded event accepted")
	}
	select {
	case <-ch:
		t.Fatal("unrecorded event delivered")
	default:
	}
	event.Cursor, err = store.RecordBusEvent(t.Context(), event)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.PublishRecorded(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.Cursor != event.Cursor {
		t.Fatalf("local cursor=%s journal cursor=%s", got.Cursor, event.Cursor)
	}
	records, err := store.ListEvents(routerstate.EventQuery{})
	if err != nil || len(records) != 1 {
		t.Fatalf("recorded event was persisted twice: %d %v", len(records), err)
	}
}
