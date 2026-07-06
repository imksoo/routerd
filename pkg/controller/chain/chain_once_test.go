// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
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

func TestFilterScheduledReconcileControllersSkipsServeLoopControllers(t *testing.T) {
	controllers := []framework.Controller{
		framework.FuncController{ControllerName: "link"},
		framework.FuncController{ControllerName: "ipv4-route"},
		framework.FuncController{ControllerName: "firewall"},
		framework.FuncController{ControllerName: "nat44-session-sync"},
		framework.FuncController{ControllerName: "dhcp-lease-sync"},
		framework.FuncController{ControllerName: "sysctl"},
		framework.FuncController{ControllerName: "log-retention"},
		framework.FuncController{ControllerName: "future-controller"},
	}

	got := filterScheduledReconcileControllers(controllers)
	if len(got) != 1 {
		t.Fatalf("scheduled controllers = %d, want 1", len(got))
	}
	if got[0].Name() != "future-controller" {
		t.Fatalf("scheduled controller = %q, want future-controller", got[0].Name())
	}
}
