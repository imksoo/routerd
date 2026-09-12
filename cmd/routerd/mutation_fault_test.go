// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFaultMutationRestoresOnlyBeforeCanonicalReplacement(t *testing.T) {
	for _, stage := range []string{"rename", "restore-fallback", "restore-unavailable", "directory-sync", "finish-generation", "sandbox-plan"} {
		t.Run(stage, func(t *testing.T) {
			_, opts := faultApplyFixture(t)
			opts.Sandbox = true
			opts.ConfigYAMLOverride = dnsRuntimeYAML("127.0.0.2")
			if err := os.WriteFile(opts.ConfigPath, []byte(dnsRuntimeYAML("127.0.0.1")), 0600); err != nil {
				t.Fatal(err)
			}
			old, err := config.Load(opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			current := old
			active := controllerchain.RuntimeSnapshot{Router: old, Known: true, Available: true, Epoch: 1}
			fault := errors.New("injected apply failure with secret payload")
			restoreFault := errors.New("injected restore failure")
			beforeRename := stage == "rename" || strings.HasPrefix(stage, "restore-")
			opts.attemptHooks.writeCanonical = func(path string, data []byte) (config.AtomicWriteOutcome, error) {
				if beforeRename {
					return config.AtomicWriteOutcome{}, fault
				}
				out, err := config.AtomicWriteFileWithOutcome(path, data)
				if err == nil && stage == "directory-sync" {
					out.DurabilityConfirmed = false
					err = fault
				}
				return out, err
			}
			finishCalls := 0
			opts.attemptHooks.finishGeneration = func(store *routerstate.SQLiteStore, g int64, p string, w []string) error {
				finishCalls++
				if stage == "finish-generation" {
					return fault
				}
				return store.FinishGeneration(g, p, w)
			}
			if stage == "sandbox-plan" {
				opts.OverrideClient = "invalid-client"
			}
			gate := &sync.RWMutex{}
			opts.MutationGate = gate
			cache := &resultCache{}
			cache.Store(&apply.Result{Generation: 99, Phase: "Healthy"})
			reloads := 0
			m := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: cache, getRouter: func() *api.Router { return current }, setRouter: func(r *api.Router) { current = r }, activeRuntime: func() controllerchain.RuntimeSnapshot { return active }}
			m.reloadWithOutcome = func(_ context.Context, r *api.Router) (controllerchain.RuntimeReloadOutcome, error) {
				reloads++
				if gate.TryRLock() {
					gate.RUnlock()
					t.Error("reload/restore released exclusive gate")
				}
				if reloads == 2 && stage == "restore-fallback" {
					active.Epoch++
					return controllerchain.RuntimeReloadOutcome{Active: active, Accepted: true, Restored: true, OperationID: uint64(reloads)}, restoreFault
				}
				if reloads == 2 && stage == "restore-unavailable" {
					active = controllerchain.RuntimeSnapshot{Known: true, Epoch: active.Epoch}
					return controllerchain.RuntimeReloadOutcome{Active: active, Accepted: true, OperationID: uint64(reloads)}, restoreFault
				}
				active = controllerchain.RuntimeSnapshot{Router: r, Known: true, Available: true, Epoch: active.Epoch + 1}
				return controllerchain.RuntimeReloadOutcome{Active: active, Accepted: true, OperationID: uint64(reloads)}, nil
			}
			response, err := m.apply(nil, controlapi.ApplyRequest{CandidateYAML: opts.ConfigYAMLOverride, Replace: true})
			if err == nil || response != nil {
				t.Fatalf("partial failure response=%+v, err=%v", response, err)
			}
			if stage != "sandbox-plan" && !errors.Is(err, fault) {
				t.Errorf("original error lost: %v", err)
			}
			if strings.HasPrefix(stage, "restore-") && !errors.Is(err, restoreFault) {
				t.Errorf("restore error lost: %v", err)
			}
			if strings.Contains(err.Error(), "secret payload") {
				t.Errorf("unsafe API error: %s", err)
			}
			wantReloads := 1
			if beforeRename {
				wantReloads = 2
			}
			if reloads != wantReloads {
				t.Errorf("reload calls=%d,want=%d", reloads, wantReloads)
			}
			if finishCalls != 1 {
				t.Errorf("terminal UPDATE attempts=%d,want=1", finishCalls)
			}
			canonical, err := os.ReadFile(opts.ConfigPath)
			wantAddress := "127.0.0.2"
			if beforeRename {
				wantAddress = "127.0.0.1"
			}
			if err != nil || !strings.Contains(string(canonical), wantAddress) {
				t.Errorf("canonical=%s,%v", canonical, err)
			}
			attempt, attemptErr, ok := cache.LastAttempt()
			if !ok || attemptErr == "" || attempt.Runtime.Active != active {
				t.Errorf("attempt snapshot=%+v err=%q present=%v active=%+v", attempt, attemptErr, ok, active)
			}
			store := faultGenerationStore(t, opts.StatePath, false)
			rows, rowErr := store.ListGenerations(10)
			if rowErr != nil || len(rows) != 1 || rows[0].Generation != attempt.Generation {
				t.Fatalf("attempt generation=%d rows=%+v error=%v", attempt.Generation, rows, rowErr)
			}
			wantPhase := "Errored"
			if stage == "finish-generation" {
				// An unfinished SQLite generation has NULL phase, exposed as "".
				wantPhase = ""
			} else if stage == "sandbox-plan" {
				wantPhase = "Committed"
			}
			if rows[0].Phase != wantPhase || rows[0].FinishedAt.IsZero() != (stage == "finish-generation") {
				t.Errorf("terminal row=%+v, want phase=%s", rows[0], wantPhase)
			}
			if !attempt.GenerationCreated || !attempt.TerminalAttempted || attempt.TerminalSucceeded != (stage != "finish-generation") {
				t.Errorf("terminal attempt=%+v", attempt)
			}
			wantCanonical := canonicalReplaced
			if beforeRename {
				wantCanonical = canonicalNotReplaced
			}
			if attempt.Canonical != wantCanonical || attempt.DurabilityConfirmed != (!beforeRename && stage != "directory-sync") {
				t.Errorf("canonical outcome=%+v", attempt)
			}
			if cached := cache.Load(); cached == nil || cached.Phase != "Error" {
				t.Errorf("cache=%+v", cached)
			}
			// A later scheduled observation can be healthy even though the
			// configuration transaction still has unresolved persistence/restoration.
			cache.Store(&apply.Result{Phase: "Healthy"})
			if cached := cache.Load(); cached == nil || cached.Phase != "Error" {
				t.Errorf("scheduled observation hid failed configuration attempt: %+v", cached)
			}
			if active.Available && current != active.Router {
				t.Error("published router differs from confirmed active")
			}
			if stage == "rename" && current != old {
				t.Error("successful restoration did not publish old router")
			}
			if stage != "rename" && current == old {
				t.Error("last-known/runtime incorrectly reset to old router")
			}
			if !gate.TryLock() {
				t.Fatal("completed mutation retained gate")
			}
			gate.Unlock()
		})
	}
}

func TestFaultTerminalAttemptIsNotRetriedByDeferredCleanup(t *testing.T) {
	router, opts := faultApplyFixture(t)
	want := errors.New("terminal fault")
	calls := 0
	opts.attemptHooks.finishGeneration = func(*routerstate.SQLiteStore, int64, string, []string) error { calls++; return want }
	outcome, err := runApplyChainOnceWithOutcome(context.Background(), router, opts, io.Discard, nil)
	if !errors.Is(err, want) || calls != 1 || !outcome.TerminalAttempted || outcome.TerminalSucceeded || outcome.Canonical != canonicalReplaced {
		t.Fatalf("outcome=%+v err=%v calls=%d", outcome, err, calls)
	}
}

func TestFaultHTTPFailureIncludesSafePartialProgress(t *testing.T) {
	_, opts := faultApplyFixture(t)
	faultGenerationStore(t, opts.StatePath, true)
	current, err := config.Load(opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	m := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: &resultCache{}, getRouter: func() *api.Router { return current }, setRouter: func(r *api.Router) { current = r }}
	handler := controlapi.Handler{Apply: m.apply}
	body := `{"candidateYaml":"apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata: {name: new-router}\nspec: {resources: []}\n","replace":true}`
	request := httptest.NewRequest(http.MethodPost, controlapi.Prefix+"/apply", strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code < 400 || !strings.Contains(response.Body.String(), "stage=finish-generation") || !strings.Contains(response.Body.String(), "canonical=replaced") {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "injected terminal") {
		t.Fatal("response leaked SQLite failure payload")
	}
}

func TestFaultDeletePostCommitFailureKeepsNewConfig(t *testing.T) {
	_, opts := faultApplyFixture(t)
	old := testRouterYAML("old-router")
	old = strings.Replace(old, "resources: []", `resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata: {name: appliance}
      spec: {hostname: appliance.example}`, 1)
	if err := os.WriteFile(opts.ConfigPath, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	current, err := config.Load(opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	faultGenerationStore(t, opts.StatePath, true)
	m := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: &resultCache{}, getRouter: func() *api.Router { return current }, setRouter: func(r *api.Router) { current = r }}
	response, err := m.delete(nil, controlapi.DeleteRequest{Target: "Hostname/appliance"})
	if err == nil || response != nil || len(current.Spec.Resources) != 0 {
		t.Fatalf("delete=%+v,%v,current=%+v", response, err, current)
	}
}

func TestFaultStatusDoesNotRelabelEarlierSuccess(t *testing.T) {
	_, opts := faultApplyFixture(t)
	store := faultGenerationStore(t, opts.StatePath, false)
	if _, err := store.BeginGeneration("failed-attempt"); err != nil {
		t.Fatal(err)
	}
	previous := &apply.Result{Generation: 42, Phase: "Healthy"}
	if got := resultWithLatestGeneration(previous, store); got.Generation != 42 {
		t.Fatalf("old success relabeled: %+v", got)
	}
}

func TestFaultShortResultWriteIsAnError(t *testing.T) {
	err := writeResult(&faultPartialWriter{}, "", &apply.Result{Phase: "Healthy"})
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short writer err=%v", err)
	}
}

func TestFaultScheduledObservationRetainsConfigGeneration(t *testing.T) {
	for _, phase := range []string{"Healthy", "Committed"} {
		t.Run(phase, func(t *testing.T) {
			cache := &resultCache{}
			cache.StoreAttempt(applyAttemptOutcome{Generation: 7, Result: &apply.Result{Generation: 7, Phase: phase}}, nil)
			cache.Store(&apply.Result{Phase: "Healthy"})
			if got := cache.Load(); got.Generation != 7 || got.Phase != phase {
				t.Fatalf("scheduled observation changed configuration meaning: %+v", got)
			}
		})
	}
}

type shortAnnouncementWriter struct{ calls int }

func (w *shortAnnouncementWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		return len(p) / 2, nil
	}
	return len(p), nil
}

func TestFaultDryRunAnnouncementShortWrite(t *testing.T) {
	router, opts := faultApplyFixture(t)
	opts.DryRun = true
	opts.AnnounceDryRunToCLI = true
	w := &shortAnnouncementWriter{}
	_, err := runApplyChainOnce(context.Background(), router, opts, w, nil)
	if !errors.Is(err, io.ErrShortWrite) || w.calls != 1 {
		t.Fatalf("announcement err=%v,calls=%d", err, w.calls)
	}
}

func TestFaultRuntimeStatusRequiresConfirmedGeneration(t *testing.T) {
	for _, tc := range []struct {
		name       string
		active     controllerchain.RuntimeSnapshot
		stopped    error
		base, want string
	}{
		{name: "unknown", base: "Healthy", want: "Unknown"},
		{name: "unavailable", active: controllerchain.RuntimeSnapshot{Known: true}, base: "Healthy", want: "Error"},
		{name: "active", active: controllerchain.RuntimeSnapshot{Known: true, Available: true}, base: "Healthy", want: "Healthy"},
		{name: "failed-attempt", base: "Error", want: "Error"},
		{name: "fenced", active: controllerchain.RuntimeSnapshot{Known: true, Available: true}, stopped: controllerchain.ErrRuntimeMutationStopped, base: "Healthy", want: "Error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusPhaseForRuntime(tc.base, tc.active, tc.stopped); got != tc.want {
				t.Fatalf("status phase=%s,want=%s", got, tc.want)
			}
		})
	}
}
