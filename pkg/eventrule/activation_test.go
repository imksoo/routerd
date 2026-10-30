// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"database/sql"
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	"testing"
	"time"
)

func TestSelectedEmptyRuleGenerationDoesNotReplayAbsentInputsOnReadd(t *testing.T) {
	c, events := newTestController(t, api.EventRulePatternSpec{Operator: OperatorAnyOf, Topic: "routerd.a"})
	db := c.Store.EventJournal()
	stop := startTestController(t, c)
	stop()
	empty := &Controller{Router: &api.Router{}, Bus: events, Store: ruleTestStore{db}, Poll: time.Millisecond}
	stop = startTestController(t, empty)
	if _, err := db.ReadEventRule(t.Context(), "rule"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty selected generation retained checkpoint/cursor: %v", err)
	}
	if err := events.PublishDurable(t.Context(), testEvent("routerd.a")); err != nil {
		t.Fatal(err)
	}
	through, err := db.EventJournalTip(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stop()
	readded := &Controller{Router: c.Router, Bus: events, Store: ruleTestStore{db}, Poll: time.Millisecond}
	startTestController(t, readded)
	cp, err := db.ReadEventRule(t.Context(), "rule")
	if err != nil || cp.Cursor != through || cp.FireCount != 0 {
		t.Fatalf("readded rule inherited retired position/state: %+v %v", cp, err)
	}
	mustReconcile(t, readded, testEvent("routerd.a"))
	if got := len(events.Recent("routerd.out")); got != 1 {
		t.Fatalf("readd replayed absent input: outputs=%d", got)
	}
	cp, err = db.ReadEventRule(t.Context(), "rule")
	if err != nil || cp.FireCount != 1 || cp.Cursor <= through {
		t.Fatalf("new input was not committed once: %+v %v", cp, err)
	}
}
