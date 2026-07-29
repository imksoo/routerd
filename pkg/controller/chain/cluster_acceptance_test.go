// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
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

func TestRouterdClusterABLeaseAcceptance(t *testing.T) {
	const leaseTTL = 500 * time.Millisecond

	leasePath := filepath.Join(t.TempDir(), "routerd-cluster.lease")
	log := &mutatorExecutionLog{}
	runnerA := newClusterAcceptanceRunner("router-a", leasePath, leaseTTL, log)
	runnerB := newClusterAcceptanceRunner("router-b", leasePath, leaseTTL, log)
	storeA := eventedStore{Store: runnerA.Store, Bus: runnerA.Bus, Router: runnerA.Router}
	storeB := eventedStore{Store: runnerB.Store, Bus: runnerB.Bus, Router: runnerB.Router}
	logger := slog.New(slog.DiscardHandler)

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()

	generationA, err := runnerA.prepareControllerGeneration(ctxA, logger, storeA)
	if err != nil {
		t.Fatalf("prepare A generation: %v", err)
	}
	if !generationA.decision.Leader {
		t.Fatalf("A decision = %+v, want leader", generationA.decision)
	}
	waitForCondition(t, 2*time.Second, "A apply-equivalent reconcile", func() bool {
		return log.count("router-a") >= 1
	})

	generationB, err := runnerB.prepareControllerGeneration(ctxB, logger, storeB)
	if err != nil {
		t.Fatalf("prepare B generation: %v", err)
	}
	if generationB.decision.Leader {
		t.Fatalf("B decision = %+v, want standby", generationB.decision)
	}

	doneA := make(chan error, 1)
	go func() {
		doneA <- runnerA.runControllerGenerations(ctxA, logger, storeA, generationA)
	}()
	doneB := make(chan error, 1)
	go func() {
		doneB <- runnerB.runControllerGenerations(ctxB, logger, storeB, generationB)
	}()

	assertConditionFor(t, 2*leaseTTL, "B remains standby for two lease TTLs", func() bool {
		return log.count("router-b") == 0
	})

	cancelA()
	generationA.cancel()
	select {
	case err := <-doneA:
		if err != context.Canceled {
			t.Fatalf("A generation supervisor error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("A generation supervisor did not stop")
	}
	aCountAtLoss := log.count("router-a")

	failoverDeadline := leaseTTL + clusterRetryInterval(runnerB.Router) + 2*time.Second
	waitForCondition(t, failoverDeadline, "B promotion and mutation", func() bool {
		return log.count("router-b") >= 1
	})

	assertConditionFor(t, clusterRetryInterval(runnerA.Router), "A remains fenced after lease loss", func() bool {
		return log.count("router-a") == aCountAtLoss
	})

	cancelB()
	select {
	case err := <-doneB:
		if err != context.Canceled {
			t.Fatalf("B generation supervisor error = %v, want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("B generation supervisor did not stop")
	}

	if overlap := log.firstOverlap(); overlap != nil {
		t.Fatalf("A/B mutator intervals overlap: A=%s..%s B=%s..%s",
			overlap.a.started.Format(time.RFC3339Nano),
			overlap.a.finished.Format(time.RFC3339Nano),
			overlap.b.started.Format(time.RFC3339Nano),
			overlap.b.finished.Format(time.RFC3339Nano))
	}
}

func newClusterAcceptanceRunner(identity, leasePath string, ttl time.Duration, log *mutatorExecutionLog) *Runner {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: identity},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "RouterdCluster"},
			Metadata: api.ObjectMeta{Name: "acceptance"},
			Spec: api.RouterdClusterSpec{
				Peers:     []string{"router-a", "router-b"},
				LeaseTTL:  ttl.String(),
				LeasePath: leasePath,
				Identity:  identity,
			},
		}}},
	}
	runner := &Runner{
		Router: router,
		Bus:    bus.New(),
		Store:  mapStore{},
		Opts: Options{
			EnabledControllers: []string{"fake-mutator-" + identity},
		},
	}
	runner.generationBuilder = func(_ context.Context, _ *slog.Logger, _ eventedStore, _ bool, decision ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		controller := framework.FuncController{
			ControllerName: "fake-mutator-" + identity,
			ReconcileFunc: func(ctx context.Context, _ daemonapi.DaemonEvent) error {
				if !decision.Leader {
					return nil
				}
				return log.run(ctx, identity)
			},
		}
		return []framework.Controller{controller}, DaemonStatusController{}, nil
	}
	return runner
}

type mutatorExecution struct {
	identity string
	started  time.Time
	finished time.Time
}

type mutatorExecutionLog struct {
	mu         sync.Mutex
	executions []mutatorExecution
}

func (l *mutatorExecutionLog) run(ctx context.Context, identity string) error {
	execution := mutatorExecution{identity: identity, started: time.Now()}
	select {
	case <-time.After(25 * time.Millisecond):
	case <-ctx.Done():
	}
	execution.finished = time.Now()
	l.mu.Lock()
	l.executions = append(l.executions, execution)
	l.mu.Unlock()
	return ctx.Err()
}

func (l *mutatorExecutionLog) count(identity string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, execution := range l.executions {
		if execution.identity == identity {
			count++
		}
	}
	return count
}

type mutatorOverlap struct {
	a mutatorExecution
	b mutatorExecution
}

func (l *mutatorExecutionLog) firstOverlap() *mutatorOverlap {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, a := range l.executions {
		if a.identity != "router-a" {
			continue
		}
		for _, b := range l.executions {
			if b.identity != "router-b" {
				continue
			}
			if a.started.Before(b.finished) && b.started.Before(a.finished) {
				return &mutatorOverlap{a: a, b: b}
			}
		}
	}
	return nil
}

func waitForCondition(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertConditionFor(t *testing.T, duration time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if !condition() {
			t.Fatalf("condition failed while asserting %s", description)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
