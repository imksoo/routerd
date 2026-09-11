// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/ha"
)

// This regression uses the original public API, so its RED result establishes
// the accepted-request bug independently of the new outcome API.
func TestFaultRuntimeAcceptedCancellationPropagatesBeforeAck(t *testing.T) {
	old := lifecycleTestRouter("old", "old")
	next := lifecycleTestRouter("new", "new")
	gate := &sync.RWMutex{}
	runner := &Runner{Router: old, Bus: bus.New(), Store: mapStore{}, Opts: Options{MutationGate: gate, EnabledControllers: []string{"test"}}}
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	runner.generationBuilder = func(ctx context.Context, _ *slog.Logger, store eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		if store.Router == next {
			close(entered)
			select {
			case <-ctx.Done():
				close(canceled)
			case <-release:
				return nil, DaemonStatusController{}, nil
			}
			<-release
			return nil, DaemonStatusController{}, ctx.Err()
		}
		return nil, DaemonStatusController{}, nil
	}
	serveCtx, stop := context.WithCancel(context.Background())
	defer stop()
	store := eventedStore{Store: runner.Store, Bus: runner.Bus, Router: old}
	first, err := runner.prepareControllerGeneration(serveCtx, slog.Default(), store)
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- runner.runControllerGenerations(serveCtx, slog.Default(), store, first) }()
	callCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callDone := make(chan error, 1)
	gate.Lock()
	go func() { defer gate.Unlock(); callDone <- runner.ReloadRuntime(callCtx, next) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("reload was not accepted")
	}
	cancel()
	select {
	case <-canceled:
	case err := <-callDone:
		t.Fatalf("accepted reload returned before builder cancellation/ack: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("builder never received request cancellation")
	}
	if gate.TryLock() {
		gate.Unlock()
		t.Fatal("mutation gate released before builder ack")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-callDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reload error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reload did not acknowledge cancellation")
	}
	stop()
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop")
	}
}

func TestFaultRuntimeReloadConfirmedOutcomes(t *testing.T) {
	requestedFailure := errors.New("requested preparation failed")
	restoreFailure := errors.New("restore preparation failed")
	for _, tc := range []struct {
		name                       string
		initial, requested         string
		failRequested, failRestore bool
	}{
		{name: "requested generation active", initial: "old", requested: "new"},
		{name: "requested preparation fails and old restored", initial: "old", requested: "new", failRequested: true},
		{name: "rollback preparation fails and new fallback active", initial: "new", requested: "old", failRequested: true},
		{name: "requested and previous preparation both fail", initial: "old", requested: "new", failRequested: true, failRestore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial, requested := lifecycleTestRouter(tc.initial, tc.initial), lifecycleTestRouter(tc.requested, tc.requested)
			runner := &Runner{Router: initial, Bus: bus.New(), Store: mapStore{}, Opts: Options{EnabledControllers: []string{"test"}}}
			var injected bool
			runner.generationBuilder = func(_ context.Context, _ *slog.Logger, store eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
				if injected && store.Router == requested && tc.failRequested {
					return nil, DaemonStatusController{}, requestedFailure
				}
				if injected && store.Router == initial && tc.failRestore {
					return nil, DaemonStatusController{}, restoreFailure
				}
				return nil, DaemonStatusController{}, nil
			}
			stop, done := startFaultRuntime(t, runner)
			defer stop()
			before := runner.ActiveRuntimeSnapshot()
			if !before.Known || !before.Available || before.Router != initial || before.Epoch == 0 {
				t.Fatalf("initial active = %+v", before)
			}
			injected = true // Only the idle supervisor's next build reads this, after channel handoff.
			outcome, err := runner.ReloadRuntimeWithOutcome(context.Background(), requested)
			if !outcome.Accepted || outcome.OperationID == 0 {
				t.Fatalf("outcome = %+v", outcome)
			}
			if errors.Is(err, requestedFailure) != tc.failRequested || errors.Is(err, restoreFailure) != tc.failRestore {
				t.Fatalf("reload error = %v", err)
			}
			if tc.failRestore {
				if !outcome.Active.Known || outcome.Active.Available || outcome.Active.Router != nil {
					t.Fatalf("no generation incorrectly reported active: %+v", outcome)
				}
				if runner.RuntimeMutationError() == nil {
					t.Fatal("failed supervisor still admits mutation")
				}
			} else {
				want := requested
				if tc.failRequested {
					want = initial
				}
				if !outcome.Active.Known || !outcome.Active.Available || outcome.Active.Router != want || outcome.Active.Epoch <= before.Epoch || outcome.Restored != tc.failRequested {
					t.Fatalf("confirmed active = %+v, want router %s", outcome, want.Metadata.Name)
				}
			}
			if snapshot := runner.ActiveRuntimeSnapshot(); snapshot != outcome.Active {
				t.Fatalf("snapshot = %+v, response = %+v", snapshot, outcome.Active)
			}
			stop()
			waitFaultRuntimeStopped(t, done)
		})
	}
}

func TestFaultRuntimeCancellationBeforeAdmissionDoesNotBuild(t *testing.T) {
	runner := &Runner{Router: lifecycleTestRouter("old", "old")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome, err := runner.ReloadRuntimeWithOutcome(ctx, lifecycleTestRouter("new", "new"))
	if !errors.Is(err, context.Canceled) || outcome.Accepted || outcome.Active.Known {
		t.Fatalf("unaccepted cancellation = %+v, %v", outcome, err)
	}
	select {
	case <-runner.runtimeReloadChannel():
		t.Fatal("canceled request was enqueued")
	default:
	}
}

func TestFaultRuntimeNonCooperativeBuilderFencesUntilAck(t *testing.T) {
	old, next := lifecycleTestRouter("old", "old"), lifecycleTestRouter("new", "new")
	gate := &sync.RWMutex{}
	runner := &Runner{Router: old, Bus: bus.New(), Store: mapStore{}, Opts: Options{MutationGate: gate, EnabledControllers: []string{"test"}}, reloadCancelGrace: time.Millisecond}
	entered, release, fenced := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	runner.generationBuilder = func(_ context.Context, _ *slog.Logger, store eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		if store.Router == next {
			close(entered)
			<-release
		}
		return nil, DaemonStatusController{}, nil // Deliberately ignores cancellation.
	}
	stop, done := startFaultRuntime(t, runner)
	defer stop()
	runner.CancelServe = func() { close(fenced); stop() }
	callCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callDone := make(chan RuntimeReloadOutcome, 1)
	errDone := make(chan error, 1)
	gate.Lock()
	go func() {
		defer gate.Unlock()
		outcome, err := runner.ReloadRuntimeWithOutcome(callCtx, next)
		callDone <- outcome
		errDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("builder did not start")
	}
	cancel()
	select {
	case <-fenced:
	case <-time.After(2 * time.Second):
		t.Fatal("noncooperative builder did not fence serve")
	}
	if gate.TryLock() {
		gate.Unlock()
		t.Fatal("gate released while builder still running")
	}
	if snapshot := runner.ActiveRuntimeSnapshot(); snapshot.Known || snapshot.Available || snapshot.Router != nil {
		t.Fatalf("unfinished builder active snapshot = %+v", snapshot)
	}
	if runner.RuntimeMutationError() == nil {
		t.Fatal("fenced runtime admits mutation")
	}
	second, err := runner.ReloadRuntimeWithOutcome(context.Background(), old)
	if err == nil || second.Accepted {
		t.Fatalf("second mutation = %+v, %v", second, err)
	}
	select {
	case <-callDone:
		t.Fatal("accepted caller returned before ack")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case outcome := <-callDone:
		if err := <-errDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("late reload error = %v", err)
		}
		if !outcome.Accepted || !outcome.Active.Known || outcome.Active.Available {
			t.Fatalf("late success must not activate canceled operation: %+v", outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late builder response was not acknowledged")
	}
	waitFaultRuntimeStopped(t, done)
}

func TestFaultRuntimeSuccessfulGenerationOutlivesRequest(t *testing.T) {
	old, next := lifecycleTestRouter("old", "old"), lifecycleTestRouter("new", "new")
	runner := &Runner{Router: old, Bus: bus.New(), Store: mapStore{}, Opts: Options{EnabledControllers: []string{"test"}}}
	contexts := make(chan context.Context, 2)
	runner.generationBuilder = func(ctx context.Context, _ *slog.Logger, store eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		if store.Router == next {
			contexts <- ctx
		}
		return nil, DaemonStatusController{}, nil
	}
	stop, done := startFaultRuntime(t, runner)
	defer stop()
	requestCtx, cancel := context.WithCancel(context.Background())
	outcome, err := runner.ReloadRuntimeWithOutcome(requestCtx, next)
	if err != nil {
		t.Fatal(err)
	}
	generationCtx := <-contexts
	cancel()
	if generationCtx.Err() != nil || runner.ActiveRuntimeSnapshot() != outcome.Active {
		t.Fatal("request cancellation stopped confirmed generation")
	}
	stop()
	waitFaultRuntimeStopped(t, done)
	if generationCtx.Err() == nil {
		t.Fatal("serve shutdown did not stop generation context")
	}
}

func TestFaultRuntimeServeStopDuringReloadAcknowledgesFailure(t *testing.T) {
	old, next := lifecycleTestRouter("old", "old"), lifecycleTestRouter("new", "new")
	runner := &Runner{Router: old, Bus: bus.New(), Store: mapStore{}, Opts: Options{EnabledControllers: []string{"test"}}}
	entered := make(chan struct{})
	runner.generationBuilder = func(ctx context.Context, _ *slog.Logger, store eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		if store.Router == next {
			close(entered)
			<-ctx.Done()
		}
		return nil, DaemonStatusController{}, nil
	}
	stop, done := startFaultRuntime(t, runner)
	defer stop()
	ack := make(chan RuntimeReloadOutcome, 1)
	errs := make(chan error, 1)
	go func() {
		outcome, err := runner.ReloadRuntimeWithOutcome(context.Background(), next)
		ack <- outcome
		errs <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("builder never entered")
	}
	stop()
	select {
	case outcome := <-ack:
		if err := <-errs; err == nil {
			t.Fatal("serve shutdown returned reload success")
		}
		if !outcome.Accepted || !outcome.Active.Known || outcome.Active.Available {
			t.Fatalf("shutdown outcome = %+v", outcome)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown leaked reload ack")
	}
	waitFaultRuntimeStopped(t, done)
	if _, err := runner.ReloadRuntimeWithOutcome(context.Background(), old); err == nil {
		t.Fatal("stopped supervisor accepted reload")
	}
}

func TestFaultRuntimeSnapshotUpdatesKeepEpochAndRejectUnavailable(t *testing.T) {
	old, next := lifecycleTestRouter("old", "old"), lifecycleTestRouter("new", "old")
	runner := &Runner{Router: old, Bus: bus.New(), Store: mapStore{}, Opts: Options{EnabledControllers: []string{"test"}}}
	runner.generationBuilder = func(context.Context, *slog.Logger, eventedStore, bool, ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		return nil, DaemonStatusController{}, nil
	}
	stop, done := startFaultRuntime(t, runner)
	before := runner.ActiveRuntimeSnapshot()
	if err := runner.UpdateRuntimeRouter(next); err != nil {
		t.Fatal(err)
	}
	if got := runner.ActiveRuntimeSnapshot(); !got.Known || !got.Available || got.Router != next || got.Epoch != before.Epoch {
		t.Fatalf("same generation update = %+v", got)
	}
	stop()
	waitFaultRuntimeStopped(t, done)
	if err := runner.UpdateRuntimeRouter(old); err == nil {
		t.Fatal("stopped runtime accepted pointer update")
	}
	if got := runner.ActiveRuntimeSnapshot(); !got.Known || got.Available || got.Router != nil {
		t.Fatalf("stopped snapshot = %+v", got)
	}
}

func TestFaultRuntimeOldMutationStopsBeforeNewGeneration(t *testing.T) {
	old, next := lifecycleTestRouter("old", "old"), lifecycleTestRouter("new", "new")
	runner := &Runner{Router: old, Bus: bus.New(), Store: mapStore{}, Opts: Options{EnabledControllers: []string{"test"}}}
	entered, canceled, exited, release, newPrepared := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enteredOnce, canceledOnce, exitedOnce, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	runner.generationBuilder = func(_ context.Context, _ *slog.Logger, store eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, error) {
		if store.Router == next {
			select {
			case <-exited:
			default:
				return nil, DaemonStatusController{}, errors.New("new generation prepared before old mutation exited")
			}
			close(newPrepared)
			return nil, DaemonStatusController{}, nil
		}
		return []framework.Controller{framework.FuncController{
			ControllerName: "test",
			Every:          time.Hour,
			NextAfter:      func() time.Duration { return time.Millisecond },
			ReconcileFunc:  func(context.Context, daemonapi.DaemonEvent) error { return nil },
			PeriodicFunc: func(ctx context.Context) (bool, error) {
				enteredOnce.Do(func() { close(entered) })
				<-ctx.Done()
				canceledOnce.Do(func() { close(canceled) })
				<-release
				exitedOnce.Do(func() { close(exited) })
				return false, ctx.Err()
			},
		}}, DaemonStatusController{}, nil
	}
	stop, done := startFaultRuntime(t, runner)
	defer stop()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("old mutating interval did not start")
	}
	ack := make(chan error, 1)
	go func() { _, err := runner.ReloadRuntimeWithOutcome(context.Background(), next); ack <- err }()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("old loop cancellation was not requested")
	}
	if snapshot := runner.ActiveRuntimeSnapshot(); snapshot.Known || snapshot.Available || snapshot.Router != nil {
		t.Fatalf("stopping old mutation must not remain a confirmed active generation: %+v", snapshot)
	}
	select {
	case <-newPrepared:
		t.Fatal("new preparation overlaps blocked old mutation")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-ack:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reload did not acknowledge old loop exit")
	}
	select {
	case <-newPrepared:
	default:
		t.Fatal("new generation was never prepared")
	}
	stop()
	waitFaultRuntimeStopped(t, done)
}

func startFaultRuntime(t *testing.T, runner *Runner) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	store := eventedStore{Store: runner.Store, Bus: runner.Bus, Router: runner.Router}
	first, err := runner.prepareControllerGeneration(ctx, slog.Default(), store)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	ready := runner.runtimeReadyChannel()
	done := make(chan error, 1)
	go func() { done <- runner.runControllerGenerations(ctx, slog.Default(), store, first) }()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		stop()
		t.Fatal("runtime supervisor did not become ready")
	}
	return stop, done
}

func waitFaultRuntimeStopped(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime supervisor did not stop")
	}
}
