// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func eventRuleOutput(name string) daemonapi.DaemonEvent {
	e := daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "routerd-eventrule", Instance: name}, "routerd.rule.output", daemonapi.SeverityInfo)
	e.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "EventRule", Name: name}
	return e
}

func appendRuleInput(t *testing.T, s *SQLiteStore) int64 {
	t.Helper()
	cursor, err := s.RecordBusEvent(t.Context(), daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "test"}, "routerd.rule.input", daemonapi.SeverityInfo))
	if err != nil {
		t.Fatal(err)
	}
	id, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEventRuleCheckpointCursorStatusAndOutputCommitTogether(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	if err := store.InitializeEventRules(ctx, []EventRuleDefinition{{Name: "count", Digest: "definition"}}, 0); err != nil {
		t.Fatal(err)
	}
	cp, err := store.ReadEventRule(ctx, "count")
	if err != nil {
		t.Fatal(err)
	}
	first := appendRuleInput(t, store)
	next := cp
	next.Cursor = first
	next.Correlations = map[string]EventRuleCorrelation{"key": {Count: 1, LastSeen: time.Now().UTC()}}
	cp, events, err := store.CommitEventRule(ctx, cp, next, nil, false, nil, nil)
	if err != nil || len(events) != 0 {
		t.Fatalf("partial count commit: %+v %v", events, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	cp, err = store.ReadEventRule(ctx, "count")
	if err != nil || cp.Cursor != first || cp.Correlations["key"].Count != 1 {
		t.Fatalf("restart lost partial count/cursor: %+v %v", cp, err)
	}
	second := appendRuleInput(t, store)
	next = cp
	next.Cursor = second
	next.FireCount = 1
	next.LastFiredAt = time.Now().UTC()
	next.Correlations = map[string]EventRuleCorrelation{}
	output := eventRuleOutput("count")
	fault, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.Close()
	if _, err := fault.Exec(`CREATE TRIGGER fail_rule_output BEFORE INSERT ON events WHEN NEW.topic='routerd.rule.output' BEGIN SELECT RAISE(FAIL,'output fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CommitEventRule(ctx, cp, next, &output, false, nil, nil); err == nil {
		t.Fatal("output failure was swallowed")
	}
	current, err := store.ReadEventRule(ctx, "count")
	if err != nil || current.Revision != cp.Revision || current.Cursor != first || current.Correlations["key"].Count != 1 {
		t.Fatalf("failed effect consumed count/input: %+v %v", current, err)
	}
	public, err := store.ReadObjectStatus(api.NetAPIVersion, "EventRule", "count")
	if err != nil || public["fireCount"] != float64(0) {
		t.Fatalf("failed output published success: %+v %v", public, err)
	}
	if _, err := fault.Exec(`DROP TRIGGER fail_rule_output`); err != nil {
		t.Fatal(err)
	}
	committed, events, err := store.CommitEventRule(ctx, cp, next, &output, false, nil, nil)
	if err != nil || len(events) != 1 || events[0].Cursor == "" {
		t.Fatalf("retry result: %+v %v", events, err)
	}
	if _, _, err := store.CommitEventRule(ctx, cp, next, &output, false, nil, nil); !errors.Is(err, ErrEventRuleConflict) {
		t.Fatalf("lost-ACK replay was accepted: %v", err)
	}
	current, err = store.ReadEventRule(ctx, "count")
	if err != nil || current.Revision != committed.Revision || current.FireCount != 1 {
		t.Fatalf("lost-ACK reread: %+v %v", current, err)
	}
	outputs, err := store.ListEvents(EventQuery{Topic: output.Type})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("replayed output: %+v %v", outputs, err)
	}
}

func TestEventRuleTimerRequiresInputTipAndRetentionIsNotAbsence(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	defs := []EventRuleDefinition{{Name: "absence", Digest: "definition"}}
	if err := s.InitializeEventRules(ctx, defs, 0); err != nil {
		t.Fatal(err)
	}
	cp, err := s.ReadEventRule(ctx, "absence")
	if err != nil {
		t.Fatal(err)
	}
	input := appendRuleInput(t, s)
	next := cp
	next.FireCount = 1
	out := eventRuleOutput("absence")
	if _, _, err := s.CommitEventRule(ctx, cp, next, &out, true, nil, nil); !errors.Is(err, ErrEventRuleConflict) {
		t.Fatalf("timer skipped unread expected input: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM events WHERE id=?`, input); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EventRuleInputs(ctx, 0); !errors.Is(err, ErrEventRuleHistoryGap) {
		t.Fatalf("retention became observed absence: %v", err)
	}
	if err := s.InitializeEventRules(ctx, nil, input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadEventRule(ctx, "absence"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("removed checkpoint remained: %v", err)
	}
	var cursors int
	if err := s.db.QueryRow(`SELECT count(*) FROM event_consumer_cursors WHERE consumer=?`, EventRuleConsumer("absence")).Scan(&cursors); err != nil || cursors != 0 {
		t.Fatalf("removed cursor remained: %d %v", cursors, err)
	}
	if err := s.InitializeEventRules(ctx, defs, input); err != nil {
		t.Fatal(err)
	}
	current, err := s.ReadEventRule(ctx, "absence")
	if err != nil || current.Cursor != input || current.FireCount != 0 {
		t.Fatalf("readd revived retired state: %+v %v", current, err)
	}
	if _, _, err := s.EventRuleInputs(ctx, input); err != nil {
		t.Fatalf("new owner cannot start at retained high-water mark: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ReadEventRule(canceled, "absence"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read=%v", err)
	}
}
