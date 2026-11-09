// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func statusEventTestBuilder(event daemonapi.DaemonEvent) StatusEventBuilder {
	return func(_, _ map[string]any) (bool, *daemonapi.DaemonEvent) { copy := event; return true, &copy }
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
