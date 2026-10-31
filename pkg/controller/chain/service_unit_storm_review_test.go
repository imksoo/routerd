// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/conntrack"
	"github.com/imksoo/routerd/pkg/controller/conntrackobserver"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type serviceStormReviewRun struct {
	trigger      string
	resourceName string
	err          error
}

type serviceStormReviewObserver struct {
	runs   chan serviceStormReviewRun
	count  atomic.Int32
	cancel context.CancelFunc
}

func (*serviceStormReviewObserver) ControllerStarted(string, time.Duration) {}
func (o *serviceStormReviewObserver) ControllerReconciled(_ string, trigger string, _, _ time.Duration, err error) {
	o.record(trigger, "", err)
}
func (o *serviceStormReviewObserver) ControllerReconciledResource(_ string, trigger, _, resourceName string, _, _ time.Duration, err error) {
	o.record(trigger, resourceName, err)
}
func (o *serviceStormReviewObserver) record(trigger, resourceName string, err error) {
	select {
	case o.runs <- serviceStormReviewRun{trigger: trigger, resourceName: resourceName, err: err}:
	default:
		o.cancel()
	}
	if o.count.Add(1) >= 32 {
		o.cancel()
	}
}

func TestReviewUnownedDNSUnitCannotReenterOwnServiceWorker(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd controller regression uses the Linux runtime")
	}
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	events := bus.NewWithStore(store)
	router := &api.Router{Metadata: api.ObjectMeta{Name: "service-storm-review"}, Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
		Metadata: api.ObjectMeta{Name: "dependent.service"},
		Spec: api.HealthCheckSpec{When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
			"ServiceUnit/external-gate.service.status.phase": {Equals: "Applied"},
		}}},
	}}}}
	state := eventedStore{Store: store, Bus: events, Router: router}
	units := filepath.Join(dir, "units")
	if err := os.Mkdir(units, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(units, "routerd-dns-resolver@.service")
	original := []byte("[Service]\nExecStart=/private/operator/resolver\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	var foreignCommands atomic.Int32
	controller := SystemdUnitController{
		Router: router, DeclaredRouter: router, Store: state,
		SystemdSystemDir: units, StateDir: filepath.Join(dir, "runtime"),
		BGPStatePath: filepath.Join(dir, "bgp", "applied.json"),
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			for _, arg := range args {
				if arg == filepath.Base(path) {
					foreignCommands.Add(1)
				}
			}
			if name == "systemctl" && len(args) > 0 && args[0] == "is-active" {
				return nil, errors.New("inactive private command fixture")
			}
			return nil, nil
		},
	}
	// Acquire the unrelated synthetic helper once before starting subscriptions,
	// so the regression specifically observes the stale-template failure cycle.
	for i := 0; i < 2; i++ {
		if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "refuse to replace unowned routerd artifact") {
			t.Fatalf("prepare actual controller: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	observer := &serviceStormReviewObserver{runs: make(chan serviceStormReviewRun, 64), cancel: cancel}
	done := make(chan error, 1)
	worker := framework.FuncController{ControllerName: "service-unit", Every: time.Second,
		Subs: serviceUnitStatusSubscriptions(router),
		PeriodicFunc: func(ctx context.Context) (bool, error) {
			return didWorkError(controller.Reconcile(ctx))
		},
	}
	go func() {
		done <- (framework.Runner{Bus: events, Observer: observer, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Run(ctx, worker)
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("worker termination: %v", err)
			}
			status, err := store.ReadObjectStatus(api.SystemAPIVersion, "ServiceUnit", filepath.Base(path))
			if err != nil || status["phase"] != "Error" {
				t.Errorf("failure status lost after worker termination: %+v %v", status, err)
			}
		case <-time.After(3 * time.Second):
			t.Error("worker did not terminate")
		}
	}()
	next := func() serviceStormReviewRun {
		t.Helper()
		select {
		case run := <-observer.runs:
			return run
		case <-time.After(3 * time.Second):
			t.Fatal("worker did not reconcile")
			return serviceStormReviewRun{}
		}
	}
	checkFailure := func(run serviceStormReviewRun) {
		t.Helper()
		if run.err == nil || !strings.Contains(run.err.Error(), "refuse to replace unowned routerd artifact") {
			t.Fatalf("expected retained foreign-template failure, got trigger=%s err=%v", run.trigger, run.err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(original) {
			t.Fatalf("foreign template changed: %q %v", got, err)
		}
		if foreignCommands.Load() != 0 {
			t.Fatalf("foreign unit received %d commands", foreignCommands.Load())
		}
	}
	first := next()
	checkFailure(first)
	if first.trigger != "bootstrap" {
		t.Fatalf("initial trigger=%s", first.trigger)
	}
	second := next()
	checkFailure(second)
	if second.trigger != "periodic" {
		t.Fatalf("own failure status re-entered service worker before periodic retry: trigger=%s resource=%s", second.trigger, second.resourceName)
	}
	// An explicit dependency still wakes the same worker, but an unrelated unit does not.
	if err := state.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", "unrelated.service", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", "external-gate.service", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatal(err)
	}
	matched, err := resourcequery.ResourceWhenMatches(resourcequery.ResourceWhen(router.Spec.Resources[0]), store)
	if err != nil || !matched {
		t.Fatalf("canonical dotted-name status dependency did not resolve: matched=%v err=%v", matched, err)
	}
	third := next()
	checkFailure(third)
	if third.trigger != "routerd.resource.status.changed" || third.resourceName != "external-gate.service" {
		t.Fatalf("explicit ServiceUnit dependency did not wake worker: %s", third.trigger)
	}
	select {
	case extra := <-observer.runs:
		t.Fatalf("unrelated/self status produced an extra reconciliation: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
	t.Logf("retained foreign template; triggers=%s,%s,%s; executions=%d", first.trigger, second.trigger, third.trigger, observer.count.Load())
}

func TestReviewUnchangedConntrackUnavailableDoesNotPublishAgain(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	events := bus.NewWithStore(store)
	router := &api.Router{}
	state := eventedStore{Store: store, Bus: events, Router: router}
	controller := &conntrackobserver.Controller{Router: router, Store: state, Bus: events, Paths: conntrack.Paths{
		Count: filepath.Join(dir, "missing-count"), Entries: filepath.Join(dir, "missing-entries"), Max: filepath.Join(dir, "missing-max"),
	}}
	for i := 0; i < 3; i++ {
		if err := controller.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "ConntrackObserver", "default")
	if err != nil || status["phase"] != "Unavailable" || status["reason"] != "ConntrackUnavailable" {
		t.Fatalf("missing native observation was not preserved: %+v %v", status, err)
	}
	recorded := events.Recent("routerd.resource.status.changed")
	if len(recorded) != 1 {
		t.Fatalf("unchanged Unavailable published %d status events: %+v", len(recorded), recorded)
	}
	if recorded[0].Resource == nil || recorded[0].Resource.Kind != "ConntrackObserver" {
		t.Fatalf("unexpected event: %+v", recorded[0])
	}
	t.Logf("three actual missing-file observations; status=%s; transition events=%d; changedFields=%s", status["phase"], len(recorded), recorded[0].Attributes["changedFields"])
}
