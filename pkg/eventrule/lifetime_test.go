// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type blockedRuleStatusStore struct {
	*routerstate.SQLiteStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedRuleStatusStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
}

func TestRuleLifetimeWaitsForFiredTimerAndSuppressesCanceledPublication(t *testing.T) {
	for _, operator := range []string{OperatorDebounce, OperatorAbsence} {
		t.Run(operator, func(t *testing.T) {
			db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			store := &blockedRuleStatusStore{SQLiteStore: db, entered: make(chan struct{}), release: make(chan struct{})}
			controller, events := testController(api.EventRulePatternSpec{Operator: operator, Topic: "routerd.trigger", Duration: "1ms", Quiet: "1ms", AllowMissingCorrelation: true})
			controller.Store, controller.Events = store, db
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done, err := controller.Start(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := controller.Reconcile(ctx, testEvent("routerd.trigger")); err != nil {
				t.Fatal(err)
			}
			select {
			case <-store.entered:
			case <-time.After(time.Second):
				t.Fatal("timer did not reach actual status effect")
			}
			cancel()
			select {
			case <-done:
				t.Fatal("lifetime acknowledged while timer effect was still running")
			default:
			}
			close(store.release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("lifetime did not acknowledge completed timer")
			}
			if emitted := events.Recent("routerd.out"); len(emitted) != 0 {
				t.Fatalf("canceled timer published %+v", emitted)
			}
		})
	}
}
