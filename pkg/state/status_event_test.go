// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func statusEventTestBuilder(event daemonapi.DaemonEvent) StatusEventBuilder {
	return func(_, _ map[string]any) (bool, *daemonapi.DaemonEvent) { copy := event; return true, &copy }
}

func TestFaultStatusEventTransactionRollbackAndLostLocalDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	const apiVersion = "net.routerd.net/v1alpha1"
	if err := store.SaveObjectStatus(apiVersion, "Interface", "uplink", map[string]any{"phase": "Pending", "owner": "retained"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadOrInitializeEventConsumerCursor("fixture"); err != nil {
		t.Fatal(err)
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "routerd"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: apiVersion, Kind: "Interface", Name: "uplink"}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_history BEFORE INSERT ON events BEGIN SELECT RAISE(FAIL, 'injected journal failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MergeObjectStatusAndEvent(apiVersion, "Interface", "uplink", map[string]any{"phase": "Ready"}, statusEventTestBuilder(event)); err == nil {
		t.Fatal("expected event insert error")
	}
	if got := store.ObjectStatus(apiVersion, "Interface", "uplink"); got["phase"] != "Pending" || got["owner"] != "retained" {
		t.Fatalf("failed history changed status: %#v", got)
	}
	if _, err := store.db.Exec("DROP TRIGGER fail_history"); err != nil {
		t.Fatal(err)
	}
	committed, err := store.MergeObjectStatusAndEvent(apiVersion, "Interface", "uplink", map[string]any{"phase": "Ready"}, statusEventTestBuilder(event))
	if err != nil || committed == nil || committed.Cursor == "" {
		t.Fatalf("commit event=%+v error=%v", committed, err)
	}
	// Simulate process exit after commit and before any bus delivery.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(apiVersion, "Interface", "uplink"); got["phase"] != "Ready" || got["owner"] != "retained" {
		t.Fatalf("committed merge lost fields: %#v", got)
	}
	if consumerCursor, err := store.LoadOrInitializeEventConsumerCursor("fixture"); err != nil || consumerCursor != 0 {
		t.Fatalf("lost local delivery advanced consumer: %d %v", consumerCursor, err)
	}
	events, err := store.ListEvents(EventQuery{Ascending: true, SinceID: 0})
	if err != nil || len(events) != 1 {
		t.Fatalf("history lost across reopen: events=%v error=%v", events, err)
	}
	if got := strconv.FormatInt(events[0].ID, 10); got != committed.Cursor {
		t.Fatalf("event identity changed: got=%s want=%s", got, committed.Cursor)
	}
	// An identical merge cannot enqueue the same transition twice.
	if duplicate, err := store.MergeObjectStatusAndEvent(apiVersion, "Interface", "uplink", map[string]any{"phase": "Ready"}, statusEventTestBuilder(event)); err != nil || duplicate != nil {
		t.Fatalf("identical merge event=%+v error=%v", duplicate, err)
	}
}

func TestFaultStatusEventRefreshesGenerationWithoutRepeatedHistory(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := store.BeginGeneration("first")
	if err != nil {
		t.Fatal(err)
	}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "routerd"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "Interface", Name: "uplink"}
	status := map[string]any{"phase": "Ready"}
	if _, err := store.SaveObjectStatusAndEvent(event.Resource.APIVersion, event.Resource.Kind, event.Resource.Name, status, statusEventTestBuilder(event)); err != nil {
		t.Fatal(err)
	}
	second, err := store.BeginGeneration("second")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("fixture did not advance generation")
	}
	if _, err := store.SaveObjectStatusAndEvent(event.Resource.APIVersion, event.Resource.Kind, event.Resource.Name, status, statusEventTestBuilder(event)); err != nil {
		t.Fatal(err)
	}
	var observed int64
	if err := store.db.QueryRow("SELECT observed_generation FROM objects WHERE kind='Interface' AND name='uplink'").Scan(&observed); err != nil {
		t.Fatal(err)
	}
	if observed != second {
		t.Fatalf("same status retained old observed_generation=%d, want %d", observed, second)
	}
	events, err := store.ListEvents(EventQuery{Topic: event.Type})
	if err != nil || len(events) != 1 {
		t.Fatalf("generation refresh repeated transition: %d %v", len(events), err)
	}
}
