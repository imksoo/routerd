// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"context"
	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"sync"
	"testing"
	"time"
)

func TestRuleLifetimeWaitsForFiredTimerAndSuppressesCanceledPublication(t *testing.T) {
	for _, operator := range []string{OperatorDebounce, OperatorAbsence} {
		t.Run(operator, func(t *testing.T) {
			c, events := newTestController(t, api.EventRulePatternSpec{Operator: operator, Topic: "routerd.trigger", Duration: "1ms", Quiet: "1ms"})
			db := c.Store.EventJournal()
			blocked := &blockedRuleCommitStore{ruleTestStore: ruleTestStore{db}, entered: make(chan struct{}), release: make(chan struct{}), onlyOutput: true}
			c.Store = blocked
			ctx, cancel := context.WithCancel(t.Context())
			done, err := c.Start(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(blocked.release) }); cancel(); waitForDrain(t, done) })
			if err := events.PublishDurable(t.Context(), testEvent("routerd.trigger")); err != nil {
				t.Fatal(err)
			}
			waitForDrain(t, blocked.entered)
			cancel()
			select {
			case <-done:
				t.Fatal("lifetime acknowledged while the real commit boundary was blocked")
			default:
			}
			release.Do(func() { close(blocked.release) })
			waitForDrain(t, done)
			rows, err := db.ListEvents(routerstate.EventQuery{Topic: "routerd.out", Limit: 10})
			if err != nil || len(rows) != 0 || len(events.Recent("routerd.out")) != 0 {
				t.Fatalf("canceled transaction published: rows=%d err=%v", len(rows), err)
			}
			cp, err := db.ReadEventRule(t.Context(), "rule")
			if err != nil || cp.FireCount != 0 || cp.Correlations["default"].Timer == nil {
				t.Fatalf("canceled transaction lost the pending timer: %+v %v", cp, err)
			}
			restarted := &Controller{Router: c.Router, Bus: events, Store: ruleTestStore{db}, Poll: time.Millisecond}
			startTestController(t, restarted)
			waitForRecent(t, events, "routerd.out", 1)
			assertTimerRetired(t, restarted)
		})
	}
}
