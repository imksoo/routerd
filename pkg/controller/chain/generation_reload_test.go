// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/ha"
)

func TestRuntimeGenerationReloadActivatesLifecycleChangeWithoutOverlap(t *testing.T) {
	gate := &sync.RWMutex{}
	log := &mutatorExecutionLog{}
	oldRouter := lifecycleTestRouter("router-a", "dns-old")
	newRouter := lifecycleTestRouter("router-b", "dns-new")
	runner := &Runner{
		Router: oldRouter,
		Bus:    bus.New(),
		Store:  mapStore{},
		Opts: Options{
			EnabledControllers: []string{"fake-lifecycle"},
			MutationGate:       gate,
		},
	}
	activated := make(chan string, 4)
	runner.generationBuilder = func(_ context.Context, _ *slog.Logger, _ eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		routerName := runner.Router.Metadata.Name
		dnsName := ""
		for _, resource := range runner.Router.Spec.Resources {
			if resource.Kind == "DNSResolver" {
				dnsName = resource.Metadata.Name
			}
		}
		controller := framework.FuncController{
			ControllerName: "fake-lifecycle",
			ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
				if err := log.run(ctx, routerName); err != nil {
					return err
				}
				activated <- dnsName
				return nil
			},
		}
		return []framework.Controller{controller}, DaemonStatusController{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := eventedStore{Store: runner.Store, Bus: runner.Bus, Router: oldRouter}
	first, err := runner.prepareControllerGeneration(ctx, slog.Default(), store)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- runner.runControllerGenerations(ctx, slog.Default(), store, first)
	}()
	waitForActivation(t, activated, "dns-old")

	gate.Lock()
	if err := runner.ReloadRuntime(ctx, newRouter); err != nil {
		gate.Unlock()
		t.Fatalf("ReloadRuntime: %v", err)
	}
	gate.Unlock()
	waitForActivation(t, activated, "dns-new")

	if runner.Router != newRouter {
		t.Fatal("running Runner did not switch to the new Router")
	}
	if overlap := log.firstOverlap(); overlap != nil {
		t.Fatalf("old/new mutator intervals overlap: old=%s..%s new=%s..%s",
			overlap.a.started.Format(time.RFC3339Nano),
			overlap.a.finished.Format(time.RFC3339Nano),
			overlap.b.started.Format(time.RFC3339Nano),
			overlap.b.finished.Format(time.RFC3339Nano))
	}

	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("generation supervisor error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("generation supervisor did not stop")
	}
}

func TestRuntimeReloadQueuedBetweenGenerationsPreemptsGatedPrepare(t *testing.T) {
	gate := &sync.RWMutex{}
	oldRouter := lifecycleTestRouter("router-a", "dns-old")
	newRouter := lifecycleTestRouter("router-b", "dns-new")
	runner := &Runner{
		Router: oldRouter,
		Bus:    bus.New(),
		Store:  mapStore{},
		Opts: Options{
			EnabledControllers: []string{"reload-preemption"},
			MutationGate:       gate,
		},
		reloadCh: make(chan generationReload, 1),
	}
	runner.generationBuilder = func(context.Context, *slog.Logger, eventedStore, bool, ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		return []framework.Controller{framework.FuncController{
			ControllerName: "reload-preemption",
			ReconcileFunc:  func(context.Context, daemonapi.DaemonEvent) error { return nil },
		}}, DaemonStatusController{}, nil
	}
	request := generationReload{router: newRouter, done: make(chan error, 1)}
	runner.reloadCh <- request

	gate.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	store := eventedStore{Store: runner.Store, Bus: runner.Bus, Router: oldRouter}
	supervisorDone := make(chan error, 1)
	go func() {
		supervisorDone <- runner.runControllerGenerations(ctx, slog.Default(), store, nil)
	}()
	select {
	case err := <-request.done:
		if err != nil {
			t.Fatalf("queued reload failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued reload deadlocked behind the mutation gate")
	}
	if runner.Router != newRouter {
		t.Fatal("queued reload did not replace the Router before prepare")
	}
	cancel()
	gate.Unlock()
	select {
	case <-supervisorDone:
	case <-time.After(time.Second):
		t.Fatal("generation supervisor did not stop")
	}
}

func TestStandbyPrepareRaceReloadTimesOutAndSupervisorRecovers(t *testing.T) {
	leasePath := filepath.Join(t.TempDir(), "routerd-cluster.lease")
	holder, err := ha.Acquire(context.Background(), ha.Config{
		Identity:  "router-a",
		Peers:     []string{"router-a", "router-b"},
		LeasePath: leasePath,
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Lease.Close()

	gate := &sync.RWMutex{}
	runner := newClusterAcceptanceRunner("router-b", leasePath, time.Minute, &mutatorExecutionLog{})
	runner.Opts.MutationGate = gate
	prepareStarted := make(chan ha.Decision, 1)
	runner.generationBuilder = func(_ context.Context, _ *slog.Logger, _ eventedStore, _ bool, decision ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		prepareStarted <- decision
		return []framework.Controller{framework.FuncController{
			ControllerName: "standby-prepare",
			ReconcileFunc:  func(context.Context, daemonapi.DaemonEvent) error { return nil },
		}}, DaemonStatusController{}, nil
	}
	store := eventedStore{Store: runner.Store, Bus: runner.Bus, Router: runner.Router}
	ctx, cancel := context.WithCancel(context.Background())
	supervisorDone := make(chan error, 1)

	gate.Lock()
	go func() {
		supervisorDone <- runner.runControllerGenerations(ctx, slog.Default(), store, nil)
	}()
	select {
	case decision := <-prepareStarted:
		if decision.Leader {
			t.Fatal("test did not enter a standby prepare window")
		}
	case <-time.After(time.Second):
		t.Fatal("standby prepare did not start")
	}

	reloadCtx, reloadCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer reloadCancel()
	err = runner.ReloadRuntime(reloadCtx, lifecycleTestRouter("router-b-new", "dns-new"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReloadRuntime error = %v, want deadline exceeded", err)
	}

	gate.Unlock()
	cancel()
	select {
	case <-supervisorDone:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not recover after the mutation gate was released")
	}
}

func lifecycleTestRouter(name, dnsName string) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"},
			Metadata: api.ObjectMeta{Name: dnsName},
			Spec:     map[string]any{"listen": []any{"127.0.0.1:53"}},
		}}},
	}
}

func waitForActivation(t *testing.T, activated <-chan string, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case got := <-activated:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for lifecycle activation %q", want)
		}
	}
}
