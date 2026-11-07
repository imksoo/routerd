// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestReadEventsPreservesIdentityAndReturnsReadFailures(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, version := range []string{"test/v1", "test/v2"} {
		if err := store.RecordEvent(version, "Interface", "wan", "Normal", "Observed", "observed "+version); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.ReadEvents(t.Context(), "test/v1", "Interface", "wan", 10)
	if err != nil || len(events) != 1 || events[0].Message != "observed test/v1" {
		t.Fatalf("version identity: events=%+v err=%v", events, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.ReadEvents(ctx, "test/v1", "Interface", "wan", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
	if _, err := store.db.Exec(`UPDATE events SET created_at='unreadable' WHERE api_version='test/v1'`); err != nil {
		t.Fatal(err)
	}
	if events, err := store.ReadEvents(t.Context(), "test/v1", "Interface", "wan", 10); err == nil || events != nil {
		t.Fatalf("corrupt event became successful partial history: events=%+v err=%v", events, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadEvents(t.Context(), "test/v1", "Interface", "wan", 10); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed store history = %v", err)
	}
}
