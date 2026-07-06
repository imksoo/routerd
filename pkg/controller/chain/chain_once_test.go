// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
)

type onceObserver struct {
	mu    sync.Mutex
	calls []onceObserverCall
}

type onceObserverCall struct {
	name    string
	trigger string
	err     error
}

func (o *onceObserver) ControllerStarted(string, time.Duration) {}

func (o *onceObserver) ControllerReconciled(name, trigger string, _, _ time.Duration, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, onceObserverCall{name: name, trigger: trigger, err: err})
}

func TestRunnerReconcileOnceUsesFilteredControllerList(t *testing.T) {
	observer := &onceObserver{}
	runner := &Runner{
		Router: &api.Router{
			TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
			Metadata: api.ObjectMeta{
				Name: "once-router",
			},
		},
		Bus:   bus.New(),
		Store: mapStore{},
		Opts: Options{
			EnabledControllers: []string{"log-retention"},
			ControllerObserver: observer,
		},
	}
	if err := runner.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}

	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.calls) != 1 {
		t.Fatalf("observer calls = %d, want 1: %+v", len(observer.calls), observer.calls)
	}
	if got := observer.calls[0]; got.name != "log-retention" || got.trigger != "once" || got.err != nil {
		t.Fatalf("observer call = %+v", got)
	}
}
