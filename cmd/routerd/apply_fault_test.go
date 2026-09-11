// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/ha"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

// Only terminal generation UPDATEs fail. The real SQLite connection, status
// writes, generation INSERT, and canonical filesystem remain operational.
func faultGenerationStore(t *testing.T, path string, fail bool) *routerstate.SQLiteStore {
	t.Helper()
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if fail {
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		_, err = db.Exec(`CREATE TRIGGER fault_terminal BEFORE UPDATE OF finished_at ON generations
BEGIN SELECT RAISE(FAIL, 'injected terminal update failure'); END`)
		if err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func faultApplyFixture(t *testing.T) (*api.Router, applyOptions) {
	t.Helper()
	setMissingSAMForwardChainIPTables(t)
	dir := t.TempDir()
	opts := applyOptions{ConfigPath: filepath.Join(dir, "router.yaml"), StatePath: filepath.Join(dir, "state.db"), LedgerPath: filepath.Join(dir, "ledger.db"), StatusFile: filepath.Join(dir, "status.json"), SkipServiceManager: true, ConfigYAMLOverride: testRouterYAML("new-router")}
	// Keep fault tests on the real apply/SQLite path, but exclude unrelated
	// host controllers even when the test machine has networking privileges.
	opts.attemptHooks = &applyAttemptHooks{configureControllers: func(o *controllerchain.Options) {
		o.EnabledControllers = []string{"log-retention"}
	}}
	if err := os.WriteFile(opts.ConfigPath, []byte(testRouterYAML("old-router")), 0600); err != nil {
		t.Fatal(err)
	}
	router, err := config.LoadBytes([]byte(opts.ConfigYAMLOverride), "candidate")
	if err != nil {
		t.Fatal(err)
	}
	return router, opts
}

func TestFaultApplyTerminalFailureDoesNotReportSuccess(t *testing.T) {
	router, opts := faultApplyFixture(t)
	store := faultGenerationStore(t, opts.StatePath, true)
	var output strings.Builder
	result, err := runApplyChainOnce(context.Background(), router, opts, &output, nil)
	if err == nil {
		t.Error("terminal UPDATE failed but apply returned success")
	}
	if result == nil {
		t.Error("partial apply observation was lost")
	}
	if output.Len() != 0 {
		t.Error("success response was written before terminal UPDATE")
	}
	if _, err := os.Stat(opts.StatusFile); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("success status file exists: %v", err)
	}
	data, readErr := os.ReadFile(opts.ConfigPath)
	if readErr != nil || !strings.Contains(string(data), "new-router") {
		t.Errorf("canonical replacement = %q, %v", data, readErr)
	}
	rows, err := store.ListGenerations(10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("generation rows = %+v, %v", rows, err)
	}
	if !rows[0].FinishedAt.IsZero() {
		t.Errorf("failed terminal UPDATE recorded completion: %+v", rows[0])
	}
}

type faultPartialWriter struct {
	calls int
	err   error
}

func (w *faultPartialWriter) Write(p []byte) (int, error) { w.calls++; return len(p) / 2, w.err }

func TestFaultApplyOutputFailurePreservesCompletedGeneration(t *testing.T) {
	router, opts := faultApplyFixture(t)
	store := faultGenerationStore(t, opts.StatePath, false)
	want := errors.New("partial output failure")
	writer := &faultPartialWriter{err: want}
	result, err := runApplyChainOnce(context.Background(), router, opts, writer, nil)
	if !errors.Is(err, want) {
		t.Errorf("apply error = %v", err)
	}
	if result == nil || result.Phase != "Healthy" {
		t.Errorf("partial result = %+v", result)
	}
	rows, err := store.ListGenerations(10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("generation rows = %+v, %v", rows, err)
	}
	if rows[0].Phase != "Healthy" || rows[0].FinishedAt.IsZero() {
		t.Errorf("output failure rewrote completed generation: %+v", rows[0])
	}
	if writer.calls != 1 {
		t.Errorf("output attempts = %d", writer.calls)
	}
}

func TestFaultMutatorPostCommitFailureKeepsVisibleRuntime(t *testing.T) {
	for _, sandbox := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "sandbox"}[sandbox], func(t *testing.T) {
			next, opts := faultApplyFixture(t)
			store := faultGenerationStore(t, opts.StatePath, true)
			current, err := config.Load(opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			opts.Sandbox = sandbox
			cache := &resultCache{}
			cache.Store(&apply.Result{Phase: "Healthy", Generation: 99})
			m := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: cache, getRouter: func() *api.Router { return current }, setRouter: func(r *api.Router) { current = r }}
			response, err := m.apply(nil, controlapi.ApplyRequest{CandidateYAML: opts.ConfigYAMLOverride, Replace: true})
			if err == nil || response != nil {
				t.Errorf("partial failure response = %+v, %v", response, err)
			}
			if current.Metadata.Name != next.Metadata.Name {
				t.Errorf("visible runtime = %s, canonical is new-router", current.Metadata.Name)
			}
			if cached := cache.Load(); cached != nil && cached.Phase == "Healthy" {
				t.Errorf("stale success remains cached: %+v", cached)
			}
			rows, err := store.ListGenerations(10)
			if err != nil || len(rows) != 1 || !rows[0].FinishedAt.IsZero() {
				t.Errorf("generation rows = %+v, %v", rows, err)
			}
		})
	}
}

func TestFaultServeTerminalFailurePrecedesOutput(t *testing.T) {
	router, opts := faultApplyFixture(t)
	store := faultGenerationStore(t, opts.StatePath, true)
	runner := &controllerchain.Runner{Router: router, Bus: bus.New(), Store: store, Opts: controllerchain.Options{EnabledControllers: []string{"log-retention"}}}
	var output strings.Builder
	result, err := runServeChainOnce(context.Background(), runner, router, opts, store, &output, nil)
	if err == nil {
		t.Error("serve ignored terminal UPDATE failure")
	}
	if result == nil {
		t.Error("serve lost partial observation")
	}
	if output.Len() != 0 {
		t.Error("serve wrote success before terminal UPDATE")
	}
}

func TestFaultUnchangedGenerationIsNotFinalizedAgain(t *testing.T) {
	router, opts := faultApplyFixture(t)
	store := faultGenerationStore(t, opts.StatePath, false)
	first, err := runApplyChainOnce(context.Background(), router, opts, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	faultGenerationStore(t, opts.StatePath, true)
	writer := &faultPartialWriter{err: errors.New("output failure")}
	_, err = runApplyChainOnce(context.Background(), router, opts, writer, nil)
	if !errors.Is(err, writer.err) {
		t.Fatalf("repeat error = %v", err)
	}
	rows, err := store.ListGenerations(10)
	if err != nil || len(rows) != 1 || rows[0].Generation != first.Generation || rows[0].Phase != "Healthy" {
		t.Errorf("reused generation changed: %+v, %v", rows, err)
	}
}

func TestFaultEarlyFailurePreservesBothCauses(t *testing.T) {
	router, opts := faultApplyFixture(t)
	original := errors.New("rename failed")
	terminal := errors.New("failed generation could not be recorded")
	opts.attemptHooks.writeCanonical = func(string, []byte) (config.AtomicWriteOutcome, error) { return config.AtomicWriteOutcome{}, original }
	calls := 0
	opts.attemptHooks.finishGeneration = func(_ *routerstate.SQLiteStore, _ int64, phase string, _ []string) error {
		calls++
		if phase != "Errored" {
			t.Errorf("early terminal phase=%s", phase)
		}
		return terminal
	}
	outcome, err := runApplyChainOnceWithOutcome(context.Background(), router, opts, io.Discard, nil)
	if !errors.Is(err, original) || !errors.Is(err, terminal) || calls != 1 || !outcome.TerminalAttempted || outcome.TerminalSucceeded {
		t.Fatalf("outcome=%+v error=%v calls=%d", outcome, err, calls)
	}
}

func TestFaultStandbyFinishesBeforeOutputWithoutCanonicalCommit(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "terminal-failure"}[fail], func(t *testing.T) {
			router, opts := faultApplyFixture(t)
			leasePath := filepath.Join(t.TempDir(), "lease")
			leader, err := ha.Acquire(context.Background(), ha.Config{Identity: "leader", Peers: []string{"leader", "standby"}, LeasePath: leasePath, TTL: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			defer leader.Lease.Close()
			router.Spec.Resources = []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "RouterdCluster"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.RouterdClusterSpec{Identity: "standby", Peers: []string{"leader", "standby"}, LeasePath: leasePath}}}
			data, err := yaml.Marshal(router)
			if err != nil {
				t.Fatal(err)
			}
			opts.ConfigYAMLOverride = string(data)
			store := faultGenerationStore(t, opts.StatePath, fail)
			opts.attemptHooks.configureControllers = func(*controllerchain.Options) { t.Error("Standby entered host reconcile") }
			var output strings.Builder
			outcome, err := runApplyChainOnceWithOutcome(context.Background(), router, opts, &output, nil)
			if (err != nil) != fail || outcome.Result == nil || outcome.Result.Phase != "Standby" || outcome.Canonical != canonicalNotRequested {
				t.Fatalf("outcome=%+v,err=%v", outcome, err)
			}
			if fail && output.Len() != 0 || !fail && output.Len() == 0 {
				t.Errorf("output length=%d,fail=%t", output.Len(), fail)
			}
			canonical, err := os.ReadFile(opts.ConfigPath)
			if err != nil || !strings.Contains(string(canonical), "old-router") {
				t.Errorf("Standby committed canonical=%q,%v", canonical, err)
			}
			rows, err := store.ListGenerations(10)
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows=%+v,%v", rows, err)
			}
			if fail && !rows[0].FinishedAt.IsZero() || !fail && rows[0].Phase != "Standby" {
				t.Errorf("terminal row=%+v", rows[0])
			}
		})
	}
}

func TestFaultNoReconcileFailureDoesNotChangeRuntime(t *testing.T) {
	for _, stage := range []string{"directory-sync", "finish-generation"} {
		t.Run(stage, func(t *testing.T) {
			_, opts := faultApplyFixture(t)
			old, err := config.Load(opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			current := old
			fault := errors.New("commit failure")
			if stage == "directory-sync" {
				opts.attemptHooks.writeCanonical = func(path string, data []byte) (config.AtomicWriteOutcome, error) {
					o, e := config.AtomicWriteFileWithOutcome(path, data)
					if e != nil {
						return o, e
					}
					o.DurabilityConfirmed = false
					return o, fault
				}
			}
			if stage == "finish-generation" {
				opts.attemptHooks.finishGeneration = func(*routerstate.SQLiteStore, int64, string, []string) error { return fault }
			}
			m := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: &resultCache{}, getRouter: func() *api.Router { return current }, setRouter: func(r *api.Router) { t.Error("NoReconcile published runtime"); current = r }, reload: func(context.Context, *api.Router) error { t.Error("NoReconcile reloaded"); return nil }}
			response, err := m.apply(nil, controlapi.ApplyRequest{CandidateYAML: opts.ConfigYAMLOverride, Replace: true, NoReconcile: true})
			if !errors.Is(err, fault) || response != nil || current != old {
				t.Fatalf("response=%+v,error=%v,current=%+v", response, err, current)
			}
			attempt, _, ok := m.cache.LastAttempt()
			if !ok || attempt.Canonical != canonicalReplaced {
				t.Errorf("attempt=%+v,present=%v", attempt, ok)
			}
		})
	}
}
