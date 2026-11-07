// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestPreparedEventCursorIncludesBootstrapHistoryAndPreservesRecovery(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	appendEvent := func(topic string) int64 {
		t.Helper()
		cursor, err := store.RecordBusEvent(t.Context(), daemonapi.NewEvent(daemonapi.DaemonRef{Name: "test"}, topic, daemonapi.SeverityInfo))
		if err != nil {
			t.Fatal(err)
		}
		id, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	prior := appendEvent("routerd.before")
	tip, err := store.EventJournalTip(t.Context())
	if err != nil || tip != prior {
		t.Fatalf("captured tip=%d error=%v", tip, err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT count(*) FROM event_consumer_cursors`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("preparation created cursor: count=%d error=%v", count, err)
	}
	bootstrap := appendEvent("routerd.bootstrap")
	if err := store.InitializeEventRules(t.Context(), []EventRuleDefinition{{Name: "rule", Digest: "definition"}}, tip); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := store.ReadEventRule(t.Context(), "rule")
	cursor := checkpoint.Cursor
	if err != nil || cursor != prior {
		t.Fatalf("activation skipped bootstrap: cursor=%d error=%v", cursor, err)
	}
	next := checkpoint
	next.Cursor = bootstrap
	if _, _, err := store.CommitEventRule(t.Context(), checkpoint, next, nil, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeEventRules(t.Context(), []EventRuleDefinition{{Name: "rule", Digest: "definition"}}, tip); err != nil {
		t.Fatal(err)
	}
	checkpoint, err = store.ReadEventRule(t.Context(), "rule")
	cursor = checkpoint.Cursor
	if err != nil || cursor != bootstrap {
		t.Fatalf("initialization reset recovery progress: cursor=%d error=%v", cursor, err)
	}
	if _, err := store.db.Exec(`DELETE FROM events`); err != nil {
		t.Fatal(err)
	}
	tip, err = store.EventJournalTip(t.Context())
	if err != nil || tip != bootstrap {
		t.Fatalf("retention reset high water mark: tip=%d error=%v", tip, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EventJournalTip(t.Context()); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed tip read=%v", err)
	}
	if err := store.InitializeEventRules(t.Context(), nil, 0); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed initialization=%v", err)
	}
}
