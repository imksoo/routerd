// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/ha"
	state "github.com/imksoo/routerd/pkg/state"
)

func runtimeApplyTestRouter(path, name string) *api.Router {
	return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "RouterdCluster"}, Metadata: api.ObjectMeta{Name: "ownership-review"}, Spec: api.RouterdClusterSpec{Identity: "router-a", Peers: []string{"router-a", "router-b"}, LeasePath: path, LeaseTTL: "1m"}}}}}
}
func runtimeApplyTestRunner(t *testing.T, router *api.Router) (*Runner, *sync.RWMutex, func()) {
	t.Helper()
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	gate := &sync.RWMutex{}
	runner := &Runner{Router: router, Bus: bus.NewWithStore(store), Store: store, Opts: Options{MutationGate: gate, EnabledControllers: []string{"log-retention"}}}
	ctx, stop := context.WithCancel(t.Context())
	if err := runner.Start(ctx); err != nil {
		stop()
		store.Close()
		t.Fatal(err)
	}
	var closeOnce sync.Once
	close := func() {
		closeOnce.Do(func() {
			stop()
			wait, done := context.WithTimeout(context.Background(), 5*time.Second)
			defer done()
			if err := runner.Wait(wait); err != nil && !errors.Is(err, context.Canceled) && !(errors.Is(err, ErrRuntimeMutationStopped) && runner.RuntimeMutationError() != nil) {
				t.Error(err)
			}
			store.Close()
		})
	}
	return runner, gate, close
}
func runtimeApplyCompetingLease(t *testing.T, path string) ha.Decision {
	t.Helper()
	decision, err := ha.Acquire(t.Context(), ha.Config{Name: "ownership-review", Identity: "router-a", Peers: []string{"router-a", "router-b"}, LeasePath: path, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Lease != nil {
		t.Cleanup(func() { decision.Lease.Close() })
	}
	return decision
}
func TestRuntimeApplyActualLeaderApplyRetainsItsSoleHALease(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-place", true: "reload"}[reload], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lease.json")
			old := runtimeApplyTestRouter(path, "old")
			runner, gate, close := runtimeApplyTestRunner(t, old)
			defer close()
			next := runtimeApplyTestRouter(path, "next")
			canonical := filepath.Join(t.TempDir(), "router.yaml")
			if err := os.WriteFile(canonical, []byte("old\n"), 0600); err != nil {
				t.Fatal(err)
			}
			gate.Lock()
			outcome, err := runner.ApplyRuntimeWithOutcome(t.Context(), next, reload, func() error {
				decision, err := ha.Acquire(t.Context(), ha.Config{Name: "ownership-review", Identity: "router-a", Peers: []string{"router-a", "router-b"}, LeasePath: path, TTL: time.Minute})
				if err != nil {
					return err
				}
				if decision.Lease != nil {
					decision.Lease.Close()
					return errors.New("selected HA owner released its lease before commit")
				}
				if decision.Leader {
					return errors.New("selected HA owner lost its lease before commit")
				}
				written, err := config.AtomicWriteFileWithOutcome(canonical, []byte("next\n"))
				if err == nil && !written.DurabilityConfirmed {
					return errors.New("durability absent")
				}
				return err
			})
			gate.Unlock()
			if err != nil || outcome.Standby || !outcome.Active.Known || !outcome.Active.Available || outcome.Active.Router != next {
				t.Fatalf("actual selected leader failed apply: %+v %v", outcome, err)
			}
			if data, err := os.ReadFile(canonical); err != nil || string(data) != "next\n" {
				t.Fatalf("canonical=%q %v", data, err)
			}
			if decision := runtimeApplyCompetingLease(t, path); decision.Leader {
				t.Fatal("successful apply lost the sole lease")
			}
			close()
			if decision := runtimeApplyCompetingLease(t, path); !decision.Leader {
				t.Fatalf("actual Runner exit retained HA lease: %+v", decision)
			}
		})
	}
}
func TestRuntimeApplyRejectedCommitReleasesOnlyNewHALease(t *testing.T) {
	oldPath, nextPath := filepath.Join(t.TempDir(), "old.lease"), filepath.Join(t.TempDir(), "next.lease")
	old := runtimeApplyTestRouter(oldPath, "old")
	runner, gate, close := runtimeApplyTestRunner(t, old)
	defer close()
	next := runtimeApplyTestRouter(nextPath, "next")
	failure := errors.New("canonical durability rejected")
	gate.Lock()
	outcome, err := runner.ApplyRuntimeWithOutcome(t.Context(), next, true, func() error { return failure })
	gate.Unlock()
	if !errors.Is(err, failure) || outcome.Active.Router != old || !outcome.Active.Known || !outcome.Active.Available {
		t.Fatalf("rejected commit damaged active owner: %+v %v", outcome, err)
	}
	if decision := runtimeApplyCompetingLease(t, oldPath); decision.Leader {
		t.Fatal("rejected commit released the selected old lease")
	}
	if decision := runtimeApplyCompetingLease(t, nextPath); !decision.Leader {
		t.Fatalf("rejected commit leaked the acquired candidate lease: %+v", decision)
	}
}
func TestRuntimeApplyActualStandbyNeverCallsCanonicalCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	holder := runtimeApplyCompetingLease(t, path)
	if !holder.Leader {
		t.Fatal("fixture did not hold lease")
	}
	old := runtimeApplyTestRouter(path, "old")
	runner, gate, close := runtimeApplyTestRunner(t, old)
	defer close()
	next := runtimeApplyTestRouter(path, "next")
	gate.Lock()
	outcome, err := runner.ApplyRuntimeWithOutcome(t.Context(), next, false, func() error { t.Error("actual standby called canonical commit"); return nil })
	gate.Unlock()
	if err != nil || !outcome.Standby || runner.ActiveRuntimeSnapshot().Router != old {
		t.Fatalf("standby changed active generation: %+v %v", outcome, err)
	}
}

func TestRuntimeApplyActualLeaderCanEditTTLWithoutSelfStandby(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	old := runtimeApplyTestRouter(path, "old")
	runner, gate, close := runtimeApplyTestRunner(t, old)
	defer close()
	next := runtimeApplyTestRouter(path, "next")
	spec, _ := next.Spec.Resources[0].RouterdClusterSpec()
	spec.LeaseTTL = "2m"
	next.Spec.Resources[0].Spec = spec
	canonical := filepath.Join(t.TempDir(), "router.yaml")
	gate.Lock()
	outcome, err := runner.ApplyRuntimeWithOutcome(t.Context(), next, true, func() error { _, err := config.AtomicWriteFileWithOutcome(canonical, []byte("TTL=2m\n")); return err })
	gate.Unlock()
	if err != nil || outcome.Standby || outcome.Active.Router != next {
		t.Fatalf("selected lease owner cannot edit TTL: %+v %v", outcome, err)
	}
	if data, err := os.ReadFile(canonical); err != nil || string(data) != "TTL=2m\n" {
		t.Fatalf("TTL edit was not committed: %q %v", data, err)
	}
}

func TestRuntimeApplyActualApplyDuringRetryGapCannotBypassCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	old, next := runtimeApplyTestRouter(path, "old"), runtimeApplyTestRouter(path, "next")
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var listener net.Listener
	var listenerMu sync.Mutex
	r := &Runner{Router: old, Store: store, Bus: bus.NewWithStore(store), Opts: Options{EnabledControllers: []string{"no-host-controller"}}, reloadCh: make(chan generationReload, 1)}
	r.Opts.ReconcileHTTPServers = func(_ context.Context, view RuntimeRouterObservation) error {
		if view.DeclaredRouter.Metadata.Name != "next" {
			return nil
		}
		listenerMu.Lock()
		defer listenerMu.Unlock()
		if listener == nil {
			var err error
			listener, err = net.Listen("tcp", "127.0.0.1:0")
			return err
		}
		return nil
	}
	defer func() {
		listenerMu.Lock()
		defer listenerMu.Unlock()
		if listener != nil {
			listener.Close()
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observed := eventedStore{Store: store, Bus: r.Bus, Router: old}
	first, err := r.prepareControllerGeneration(ctx, slog.Default(), observed)
	if err != nil {
		t.Fatal(err)
	}
	if !r.publishRuntimeGeneration(first) {
		t.Fatal("fixture did not select the actual generation")
	}
	if err := r.closeControllerGeneration(first); err != nil {
		t.Fatal(err)
	}
	r.clearRuntimeGeneration(false)
	canonical := filepath.Join(t.TempDir(), "router.yaml")
	ack := make(chan error, 1)
	go func() {
		_, err := r.ApplyRuntimeWithOutcome(ctx, next, true, func() error { _, err := config.AtomicWriteFileWithOutcome(canonical, []byte("next\n")); return err })
		ack <- err
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(r.reloadCh) == 0 {
		select {
		case <-deadline.C:
			t.Fatal("apply request did not enter actor channel")
		case <-time.After(time.Millisecond):
		}
	}
	done := make(chan error, 1)
	go func() { done <- r.runControllerGenerations(ctx, slog.Default(), observed, nil) }()
	select {
	case err := <-ack:
		listenerMu.Lock()
		bound := listener != nil
		listenerMu.Unlock()
		_, statErr := os.Stat(canonical)
		if err == nil || bound || !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("retry-gap apply bypassed canonical owner: err=%v candidateBound=%t canonicalErr=%v", err, bound, statErr)
		}
	case <-time.After(2 * time.Second):
		t.Error("retry-gap apply did not acknowledge")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("actor did not stop")
	}
}

func TestRuntimeApplyLeasePathReplacementDuringCommitFencesOldLeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	old, next := runtimeApplyTestRouter(path, "old"), runtimeApplyTestRouter(path, "next")
	runner, gate, close := runtimeApplyTestRunner(t, old)
	defer close()
	gate.Lock()
	outcome, err := runner.ApplyRuntimeWithOutcome(t.Context(), next, true, func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.Rename(path, path+".previous"); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0600)
	})
	gate.Unlock()
	if err == nil {
		t.Fatal("lease pathname replacement was ignored")
	}
	if runner.RuntimeMutationError() == nil || outcome.Active.Available {
		t.Fatalf("old leader resumed after losing its native lease pathname: %+v %v", outcome, err)
	}
}

func TestRuntimeApplyInPlaceLeaseReplacementCannotSucceed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	old, next := runtimeApplyTestRouter(path, "old"), runtimeApplyTestRouter(path, "next")
	runner, gate, close := runtimeApplyTestRunner(t, old)
	defer close()
	gate.Lock()
	outcome, err := runner.ApplyRuntimeWithOutcome(t.Context(), next, false, func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.Rename(path, path+".previous"); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0600)
	})
	gate.Unlock()
	if err == nil || runner.RuntimeMutationError() == nil || outcome.Active.Available {
		t.Fatalf("in-place apply succeeded after losing actual lease: %+v %v", outcome, err)
	}
}
func TestRuntimeApplyNewLeaseReplacementCannotActivateCandidate(t *testing.T) {
	oldPath, nextPath := filepath.Join(t.TempDir(), "old.lease"), filepath.Join(t.TempDir(), "next.lease")
	old, next := runtimeApplyTestRouter(oldPath, "old"), runtimeApplyTestRouter(nextPath, "next")
	runner, gate, close := runtimeApplyTestRunner(t, old)
	defer close()
	gate.Lock()
	outcome, err := runner.ApplyRuntimeWithOutcome(t.Context(), next, true, func() error {
		data, err := os.ReadFile(nextPath)
		if err != nil {
			return err
		}
		if err := os.Rename(nextPath, nextPath+".previous"); err != nil {
			return err
		}
		return os.WriteFile(nextPath, data, 0600)
	})
	gate.Unlock()
	if err == nil || outcome.Active.Router == next && outcome.Active.Available {
		t.Fatalf("candidate activated after its pre-acquired lease pathname was replaced: %+v %v", outcome, err)
	}
}
