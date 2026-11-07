// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestPairedStatusEventsReserveRowsAndRollbackTogether(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.eventJournalMaxRows = 3
	ref := daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DerivedEvent", Name: "condition"}
	build := func(_, _ map[string]any) (bool, []daemonapi.DaemonEvent) {
		return true, []daemonapi.DaemonEvent{
			{Type: "routerd.resource.status.changed", Resource: &ref},
			{Type: "routerd.condition.asserted", Resource: &ref},
		}
	}
	for i := 0; i < 4; i++ {
		if _, err := store.SaveObjectStatusAndEvents(ref.APIVersion, ref.Kind, ref.Name, map[string]any{"step": i}, build); err != nil {
			t.Fatal(err)
		}
		if stats := store.EventJournalStats(); stats.Rows > 3 {
			t.Fatalf("pair escaped row bound: %+v", stats)
		}
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_pair_second BEFORE INSERT ON events WHEN NEW.topic='routerd.condition.asserted' BEGIN SELECT RAISE(ABORT,'second required event failed'); END`); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListEvents(EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveObjectStatusAndEvents(ref.APIVersion, ref.Kind, ref.Name, map[string]any{"step": 9}, build); err == nil {
		t.Fatal("failed second output committed")
	}
	if got := store.ObjectStatus(ref.APIVersion, ref.Kind, ref.Name)["step"]; got != float64(3) {
		t.Fatalf("status survived failed pair: %v", got)
	}
	var lastBefore int64
	for _, event := range before {
		if event.ID > lastBefore {
			lastBefore = event.ID
		}
	}
	after, err := store.ListEvents(EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	// Pruning and both required entries share the status transaction. A failed
	// second insert must preserve prior history as well as the original status.
	if len(after) != len(before) {
		t.Fatalf("failed transaction pruned history: before=%d after=%d", len(before), len(after))
	}
	for _, event := range after {
		if event.ID > lastBefore {
			t.Fatalf("partial new history survived: %+v", event)
		}
	}
}

func TestPairedStatusEventsUseExactPayloadBudget(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.eventJournalMaxBytes = 500_000
	ref := daemonapi.ResourceRef{APIVersion: "v1", Kind: "DerivedEvent", Name: "condition"}
	// Topic is not a bounded message: its full bytes occur in both SQL type
	// and topic. A valid long topic must fit by actual size, not a fixed guess.
	event := daemonapi.DaemonEvent{Type: strings.Repeat("t", 200_000), Resource: &ref}
	build := func(_, _ map[string]any) (bool, []daemonapi.DaemonEvent) {
		return true, []daemonapi.DaemonEvent{event}
	}
	for step := 0; step < 3; step++ {
		if _, err := store.SaveObjectStatusAndEvents(ref.APIVersion, ref.Kind, ref.Name, map[string]any{"step": step}, build); err != nil {
			t.Fatal(err)
		}
		var rows, payload int64
		if err := store.db.QueryRow(`SELECT count(*), coalesce(sum(`+eventJournalPayloadSQL+`),0) FROM events`).Scan(&rows, &payload); err != nil {
			t.Fatal(err)
		}
		stats := store.EventJournalStats()
		if stats.Rows != rows || stats.PayloadBytes != payload || payload > stats.MaxPayloadBytes {
			t.Fatalf("cached budget differs from durable bytes: stats=%+v rows=%d payload=%d", stats, rows, payload)
		}
	}
}

func TestOversizedStatusEventPairPreservesPriorStateAndHistory(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.eventJournalMaxBytes = 700_000
	ref := daemonapi.ResourceRef{APIVersion: "v1", Kind: "DerivedEvent", Name: "condition"}
	event := daemonapi.DaemonEvent{Type: "small", Resource: &ref}
	build := func(_, _ map[string]any) (bool, []daemonapi.DaemonEvent) {
		return true, []daemonapi.DaemonEvent{event, event}
	}
	if _, err := store.SaveObjectStatusAndEvents(ref.APIVersion, ref.Kind, ref.Name, map[string]any{"step": 1}, build); err != nil {
		t.Fatal(err)
	}
	before := store.EventJournalStats()
	event.Type = strings.Repeat("t", 200_000) // pair >700KiB; former fixed reservation <700KiB.
	if _, err := store.SaveObjectStatusAndEvents(ref.APIVersion, ref.Kind, ref.Name, map[string]any{"step": 2}, build); err == nil {
		t.Fatal("oversized pair escaped the durable budget")
	}
	if after := store.EventJournalStats(); after.Rows != before.Rows || after.PayloadBytes != before.PayloadBytes {
		t.Fatalf("failed pair changed journal accounting: before=%+v after=%+v", before, after)
	}
	if got := store.ObjectStatus(ref.APIVersion, ref.Kind, ref.Name)["step"]; got != float64(1) {
		t.Fatalf("failed pair changed state: %v", got)
	}
	var rows int64
	if err := store.db.QueryRow(`SELECT count(*) FROM events`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != before.Rows {
		t.Fatalf("failed pair pruned earlier history: %d", rows)
	}
}

func TestStatusTransactionReadsAfterConcurrentWriterCompletes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref := daemonapi.ResourceRef{APIVersion: "v1", Kind: "DerivedEvent", Name: "condition"}
	if err := store.SaveObjectStatus(ref.APIVersion, ref.Kind, ref.Name, map[string]any{"step": 1}); err != nil {
		t.Fatal(err)
	}
	other, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := other.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE objects SET status='{"step":2}' WHERE kind='DerivedEvent'`); err != nil {
		t.Fatal(err)
	}
	entered := make(chan int, 1)
	done := make(chan error, 1)
	go func() {
		_, err := store.SaveObjectStatusAndEvents(ref.APIVersion, ref.Kind, ref.Name, map[string]any{"step": 3}, func(current, _ map[string]any) (bool, []daemonapi.DaemonEvent) {
			entered <- int(current["step"].(float64))
			return true, []daemonapi.DaemonEvent{{Type: "condition.asserted", Resource: &ref}}
		})
		done <- err
	}()
	var prematurelyRead int
	select {
	case prematurelyRead = <-entered:
	case <-time.After(40 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("status transaction did not resume after the other writer committed")
	}
	if prematurelyRead != 0 {
		t.Fatalf("transaction read stale premise while another writer was active: %d", prematurelyRead)
	}
	if got := <-entered; got != 2 {
		t.Fatalf("transaction saw step=%d after writer committed", got)
	}
}

func TestReadOnlyStateSnapshotDoesNotAcquireWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	other, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := store.db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM objects`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := other.db.ExecContext(ctx, `INSERT INTO objects(api_version,kind,name,status,created_at,modified_at) VALUES('v1','DerivedEvent','other','{}',?,?)`, formatStateTime(time.Now()), formatStateTime(time.Now())); err != nil {
		t.Fatalf("read-only snapshot blocked another writer: %v", err)
	}
}
