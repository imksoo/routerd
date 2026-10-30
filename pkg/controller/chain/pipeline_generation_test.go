// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/observabilitypipeline"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type generationBlockedOutput struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *generationBlockedOutput) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(data), nil
}

func TestPipelineStopTimeoutFencesReloadWithoutStartingAnotherCursorOwner(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.LoadOrInitializeEventConsumerCursor("observabilitypipeline/logs"); err != nil {
		t.Fatal(err)
	}
	events := bus.NewWithStore(store)
	old := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "old"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "logs"}, Spec: api.ObservabilityPipelineSpec{Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{Type: "stdout"}}}}}}}}
	output := &generationBlockedOutput{entered: make(chan struct{}), release: make(chan struct{})}
	var builders atomic.Int64
	var owner *observabilitypipeline.Controller
	runner := &Runner{Router: old, Store: store, Bus: events, Opts: Options{EnabledControllers: []string{"observability-pipeline"}}}
	runner.generationBuilder = func(ctx context.Context, _ *slog.Logger, configured eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
		builders.Add(1)
		pipeline := &observabilitypipeline.Controller{Router: configured.Router, Store: store, Events: store, Bus: events, Stdout: output, Poll: time.Millisecond}
		owner = pipeline
		return nil, DaemonStatusController{}, controllerLifetimes{pipeline: pipeline}, pipeline.Start(ctx)
	}
	stop, done := startFaultRuntime(t, runner)
	defer func() { stop(); waitFaultRuntimeStopped(t, done) }()
	defer func() {
		close(output.release)
		ctx, cancel := context.WithTimeout(context.Background(), RuntimeShutdownGrace)
		defer cancel()
		if err := owner.Stop(ctx); err != nil {
			t.Errorf("released pipeline did not stop: %v", err)
		}
	}()
	if err := events.Publish(context.Background(), daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "fixture"}, "routerd.pipeline.block", daemonapi.SeverityInfo)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-output.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual exporter did not reach stdout")
	}
	next := old.Clone()
	next.Metadata.Name = "next"
	request, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	outcome, err := runner.ReloadRuntimeWithOutcome(request, &next)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || !outcome.Accepted {
		t.Fatalf("blocked exporter reload = %+v %v", outcome, err)
	}
	if builders.Load() != 1 {
		t.Fatalf("started %d generations while the previous cursor owner was alive", builders.Load())
	}
	if active := runner.ActiveRuntimeSnapshot(); active.Known || active.Available || !errors.Is(runner.RuntimeMutationError(), ErrRuntimeMutationStopped) {
		t.Fatalf("failed teardown was reported as a stopped/available generation: %+v", active)
	}
}

func TestFailedPipelineStopKeepsActualClusterLockUntilWorkerExits(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.LoadOrInitializeEventConsumerCursor("observabilitypipeline/logs"); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ObservabilityPipeline"}, Metadata: api.ObjectMeta{Name: "logs"}, Spec: api.ObservabilityPipelineSpec{Logs: api.ObservabilityPipelineLogsSpec{Sinks: []api.ObservabilityPipelineLogSink{{Type: "stdout"}}}}}}}}
	output := &generationBlockedOutput{entered: make(chan struct{}), release: make(chan struct{})}
	events := bus.NewWithStore(store)
	pipeline := &observabilitypipeline.Controller{Router: router, Store: store, Events: store, Bus: events, Stdout: output, Poll: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := pipeline.Start(ctx); err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			close(output.release)
		}
		stopCtx, stop := context.WithTimeout(context.Background(), RuntimeShutdownGrace)
		defer stop()
		_ = pipeline.Stop(stopCtx)
	}()
	if err := events.Publish(context.Background(), daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "fixture"}, "routerd.pipeline.block", daemonapi.SeverityInfo)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-output.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual exporter did not block")
	}
	leasePath := filepath.Join(t.TempDir(), "lease")
	cfg := ha.Config{Identity: "old", Peers: []string{"old", "new"}, LeasePath: leasePath, TTL: time.Millisecond}
	decision, err := ha.Acquire(context.Background(), cfg)
	if err != nil || !decision.Leader {
		t.Fatalf("acquire old lease: %+v %v", decision, err)
	}
	defer decision.Lease.Close()
	generation := &controllerGeneration{cancel: cancel, decision: decision, lifetimes: controllerLifetimes{pipeline: pipeline}}
	runner := &Runner{}
	if err := runner.closeControllerGeneration(generation); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked owner stop=%v", err)
	}
	cfg.Identity = "new"
	contender, err := ha.Acquire(context.Background(), cfg)
	if err != nil || contender.Leader || contender.Reason != "locked-by-peer" {
		t.Fatalf("unconfirmed old owner released leadership: %+v err=%v", contender, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- runner.Wait(context.Background()) }()
	select {
	case err := <-exited:
		t.Fatalf("runtime acknowledged exit while old exporter retained leadership: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(output.release)
	released = true
	select {
	case err := <-exited:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("runtime lost the unconfirmed-teardown result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not acknowledge actual old-owner exit")
	}
	deadline := time.Now().Add(time.Second)
	for {
		contender, err = ha.Acquire(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if contender.Leader {
			if err := contender.Lease.Close(); err != nil {
				t.Fatal(err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("old owner exited but deferred cluster release did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}
