// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type recoveryStore struct {
	*routerstate.SQLiteStore
	loseOutputReply bool
}

func (s *recoveryStore) CommitRule(ctx context.Context, old, next routerstate.EventRuleCheckpoint, output *daemonapi.DaemonEvent, atTip bool) (routerstate.EventRuleCheckpoint, []daemonapi.DaemonEvent, error) {
	cp, events, err := s.SQLiteStore.CommitEventRule(ctx, old, next, output, atTip, nil, nil)
	if err == nil && output != nil && s.loseOutputReply {
		s.loseOutputReply = false
		return cp, nil, errors.New("lost reply after actual SQLite commit")
	}
	return cp, events, err
}

func recoveryActor(t *testing.T, db *routerstate.SQLiteStore, pattern api.EventRulePatternSpec) (*Controller, *bus.Bus, *recoveryStore) {
	t.Helper()
	store := &recoveryStore{SQLiteStore: db}
	events := bus.NewWithStore(db)
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EventRule"}, Metadata: api.ObjectMeta{Name: "rule"}, Spec: api.EventRuleSpec{Pattern: pattern, Emit: api.EventRuleEmitSpec{Topic: "routerd.recovery.output"}}}
	c := &Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, Bus: events, Store: store, Poll: time.Millisecond}
	if err := c.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	var definitions []routerstate.EventRuleDefinition
	for _, rule := range c.rules {
		definitions = append(definitions, rule.definition)
	}
	if err := db.InitializeEventRules(t.Context(), definitions, c.initialCursor); err != nil {
		t.Fatal(err)
	}
	return c, events, store
}

func publishRecoveryInput(t *testing.T, events *bus.Bus, topic string) int64 {
	t.Helper()
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "test"}, topic, daemonapi.SeverityInfo)
	if err := events.Publish(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	recent := events.Recent(topic)
	id, err := strconv.ParseInt(recent[len(recent)-1].Cursor, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPartialPatternAndThrottleStateSurviveSQLiteRestart(t *testing.T) {
	for _, operator := range []string{OperatorCount, OperatorSequence, OperatorAllOf, OperatorWindow, OperatorThrottle} {
		t.Run(operator, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			db, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { db.Close() }()
			pattern := api.EventRulePatternSpec{Operator: operator, Topic: "routerd.a", Threshold: 2, Window: "1h", Interval: "1h"}
			second := "routerd.a"
			if operator == OperatorSequence || operator == OperatorAllOf {
				pattern.Topic = ""
				pattern.Topics = []string{"routerd.a", "routerd.b"}
				second = "routerd.b"
			}
			c, events, _ := recoveryActor(t, db, pattern)
			publishRecoveryInput(t, events, "routerd.a")
			if err := c.drainStoredEvents(t.Context()); err != nil {
				t.Fatal(err)
			}
			before, err := db.ReadEventRule(t.Context(), "rule")
			if err != nil {
				t.Fatal(err)
			}
			if len(before.Correlations) != 1 {
				t.Fatalf("partial state not persisted: %+v", before)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			restarted, events, _ := recoveryActor(t, db, pattern)
			publishRecoveryInput(t, events, second)
			if err := restarted.drainStoredEvents(t.Context()); err != nil {
				t.Fatal(err)
			}
			after, err := db.ReadEventRule(t.Context(), "rule")
			if err != nil || after.FireCount != 1 {
				t.Fatalf("restart lost pattern state or repeated throttled output: %+v %v", after, err)
			}
			outputs, err := db.ListEvents(routerstate.EventQuery{Topic: "routerd.recovery.output"})
			if err != nil || len(outputs) != 1 {
				t.Fatalf("restart output count=%d error=%v", len(outputs), err)
			}
		})
	}
}

func TestActualOutputFailureAndLostReplyDoNotConsumeOrRepeatPattern(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, events, store := recoveryActor(t, db, api.EventRulePatternSpec{Operator: OperatorCount, Topic: "routerd.a", Threshold: 2})
	first := publishRecoveryInput(t, events, "routerd.a")
	if err := c.drainStoredEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	fault, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer fault.Close()
	if _, err := fault.Exec(`CREATE TRIGGER fail_recovery_output BEFORE INSERT ON events WHEN NEW.topic='routerd.recovery.output' BEGIN SELECT RAISE(FAIL,'output fault'); END`); err != nil {
		t.Fatal(err)
	}
	publishRecoveryInput(t, events, "routerd.a")
	if err := c.drainStoredEvents(t.Context()); err == nil {
		t.Fatal("output failure advanced the consumer")
	}
	cp, err := db.ReadEventRule(t.Context(), "rule")
	if err != nil || cp.Cursor != first || cp.FireCount != 0 || cp.Correlations["default"].Count != 1 {
		t.Fatalf("failed output changed durable state: %+v %v", cp, err)
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "EventRule", "rule")
	if err != nil || status["phase"] != "Error" || cp.LastError == "" {
		t.Fatalf("failed output remained a successful result: %+v %v", status, err)
	}
	if len(events.Recent("routerd.recovery.output")) != 0 {
		t.Fatal("failed output was delivered locally")
	}
	if _, err := fault.Exec(`DROP TRIGGER fail_recovery_output`); err != nil {
		t.Fatal(err)
	}
	store.loseOutputReply = true
	if err := c.drainStoredEvents(t.Context()); err == nil {
		t.Fatal("injected lost reply did not interrupt the caller")
	}
	if len(events.Recent("routerd.recovery.output")) != 0 {
		t.Fatal("unacknowledged commit was locally published")
	}
	if err := c.drainStoredEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	cp, err = db.ReadEventRule(t.Context(), "rule")
	if err != nil || cp.FireCount != 1 {
		t.Fatalf("lost ACK repeated count: %+v %v", cp, err)
	}
	status, err = db.ReadObjectStatus(api.NetAPIVersion, "EventRule", "rule")
	if err != nil || status["phase"] != PhaseActive || cp.LastError != "" {
		t.Fatalf("successful recovery retained an obsolete failure: %+v %v", status, err)
	}
	outputs, err := db.ListEvents(routerstate.EventQuery{Topic: "routerd.recovery.output"})
	if err != nil || len(outputs) != 1 {
		t.Fatalf("lost reply repeated output: %+v %v", outputs, err)
	}
}

func TestDueAbsenceDrainsOfflineExpectedInputAndRejectsHistoryGap(t *testing.T) {
	for _, pruned := range []bool{false, true} {
		t.Run(map[bool]string{false: "expected retained", true: "expected history lost"}[pruned], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			db, err := routerstate.OpenSQLite(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			pattern := api.EventRulePatternSpec{Operator: OperatorAbsence, Trigger: "routerd.trigger", Expected: "routerd.expected", Duration: "0s"}
			c, events, _ := recoveryActor(t, db, pattern)
			publishRecoveryInput(t, events, "routerd.trigger")
			// Place the expected input in the journal before the due timer can
			// be considered. A restart has only this durable history to use.
			id := publishRecoveryInput(t, events, "routerd.expected")
			if pruned {
				fault, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				defer fault.Close()
				if _, err := fault.Exec(`DELETE FROM events WHERE id=?`, id); err != nil {
					t.Fatal(err)
				}
			}
			err = c.drainStoredEvents(t.Context())
			if pruned && !errors.Is(err, routerstate.ErrEventRuleHistoryGap) || !pruned && err != nil {
				t.Fatalf("history result=%v", err)
			}
			outputs, readErr := db.ListEvents(routerstate.EventQuery{Topic: "routerd.recovery.output"})
			if readErr != nil || len(outputs) != 0 {
				t.Fatalf("absence fabricated from unread/missing input: %+v %v", outputs, readErr)
			}
			if pruned {
				status, err := db.ReadObjectStatus(api.NetAPIVersion, "EventRule", "rule")
				if err != nil || status["phase"] != "Error" || status["message"] != routerstate.ErrEventRuleHistoryGap.Error() {
					t.Fatalf("missing history retained successful public status: %+v %v", status, err)
				}
			}
		})
	}
}

func TestBoundedDrainLetsEveryRuleAdvanceBeforeFollowingTheJournalTip(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	events := bus.NewWithStore(db)
	var resources []api.Resource
	for _, name := range []string{"first", "second"} {
		resource := testRuleResource(api.EventRulePatternSpec{Operator: OperatorCount, Topic: "routerd.a", Threshold: 10000})
		resource.Metadata.Name = name
		resources = append(resources, resource)
	}
	c := Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: resources}}, Bus: events, Store: ruleTestStore{db}}
	if err := c.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	var definitions []routerstate.EventRuleDefinition
	for _, rule := range c.rules {
		definitions = append(definitions, rule.definition)
	}
	if err := db.InitializeEventRules(t.Context(), definitions, c.initialCursor); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		publishRecoveryInput(t, events, "routerd.a")
	}
	tip, err := db.EventJournalTip(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.drainStoredEvents(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second"} {
		cp, err := db.ReadEventRule(t.Context(), name)
		if err != nil || cp.Cursor == 0 || cp.Cursor >= tip || cp.Correlations["default"].Count == 0 {
			t.Fatalf("one pass monopolized the journal or starved %s: %+v %v", name, cp, err)
		}
	}
	if c.nextWake.IsZero() || c.nextWake.After(c.now()) {
		t.Fatal("bounded pending work did not request an immediate next pass")
	}
}

func TestChangedDefinitionPublishesResetCountWithoutNewInput(t *testing.T) {
	c, events := newTestController(t, api.EventRulePatternSpec{Operator: OperatorCount, Topic: "routerd.a", Threshold: 1})
	stop := startTestController(t, c)
	mustReconcile(t, c, testEvent("routerd.a"))
	stop()
	resource := testRuleResource(api.EventRulePatternSpec{Operator: OperatorCount, Topic: "routerd.a", Threshold: 2})
	replacement := &Controller{Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}, Bus: events, Store: c.Store, Poll: time.Millisecond}
	startTestController(t, replacement)
	status, err := c.Store.EventJournal().ReadObjectStatus(api.NetAPIVersion, "EventRule", "rule")
	if err != nil || status["phase"] != PhaseActive || status["fireCount"] != float64(0) {
		t.Fatalf("new definition retained the preceding public count: %+v %v", status, err)
	}
}
