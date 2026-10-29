// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/ha"
	"gopkg.in/yaml.v3"
)

const missingBridgeYAML = `
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata: {name: test-member}
      spec: {ifname: rd-member-test, managed: false, owner: external}
    - apiVersion: net.routerd.net/v1alpha1
      kind: Bridge
      metadata: {name: unobserved}
      spec: {ifname: rd-phase-test, members: [test-member]}
`

func TestMutationDriftFailurePreservesCanonicalRuntimeAndTerminalEvidence(t *testing.T) {
	for _, operation := range []string{"apply", "delete"} {
		t.Run(operation, func(t *testing.T) {
			_, opts := faultApplyFixture(t)
			// This is an actual failed observation, with host controllers disabled
			// by faultApplyFixture. No command in this fixture mutates networking.
			binDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(binDir, "ip"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			before := dnsRuntimeYAML("127.0.0.1")
			candidate := dnsRuntimeYAML("127.0.0.2") + missingBridgeYAML
			if operation == "delete" {
				before += missingBridgeYAML + `
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata: {name: appliance}
      spec: {hostname: appliance.example}
`
			}
			if err := os.WriteFile(opts.ConfigPath, []byte(before), 0o600); err != nil {
				t.Fatal(err)
			}
			old, err := config.Load(opts.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			current := old
			active := controllerchain.RuntimeSnapshot{Router: old, Known: true, Available: true, Epoch: 1}
			reloads := 0
			cache := &resultCache{}
			m := serveConfigMutator{
				configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: cache,
				getRouter: func() *api.Router { return current },
				setRouter: func(router *api.Router) {
					if router != old {
						t.Error("uncommitted candidate was published")
					}
					current = router
				},
				activeRuntime: func() controllerchain.RuntimeSnapshot { return active },
				reloadWithOutcome: func(_ context.Context, router *api.Router) (controllerchain.RuntimeReloadOutcome, error) {
					reloads++
					active = controllerchain.RuntimeSnapshot{Router: router, Known: true, Available: true, Epoch: active.Epoch + 1}
					return controllerchain.RuntimeReloadOutcome{Active: active, Accepted: true}, nil
				},
			}
			var body []byte
			if operation == "apply" {
				body, err = json.Marshal(controlapi.ApplyRequest{CandidateYAML: candidate, Replace: true})
			} else {
				body, err = json.Marshal(controlapi.DeleteRequest{Target: "Hostname/appliance"})
			}
			if err != nil {
				t.Fatal(err)
			}
			h := controlapi.Handler{Apply: m.apply, Delete: m.delete}
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, controlapi.Prefix+"/"+operation, strings.NewReader(string(body))))
			if response.Code < 400 || !strings.Contains(response.Body.String(), "phase=Drifted") || strings.Contains(response.Body.String(), `"deleted"`) {
				t.Fatalf("failed mutation reported success: HTTP %d %s", response.Code, response.Body.String())
			}
			canonical, err := os.ReadFile(opts.ConfigPath)
			if err != nil || string(canonical) != before || current != old || active.Router != old {
				t.Fatalf("failed mutation changed canonical or active runtime: config=%s error=%v current=%p active=%p old=%p", canonical, err, current, active.Router, old)
			}
			wantReloads := 0
			if operation == "apply" {
				wantReloads = 2
			}
			if reloads != wantReloads {
				t.Fatalf("reload/restore count=%d, want %d", reloads, wantReloads)
			}
			attempt, _, ok := cache.LastAttempt()
			if !ok || attempt.Result == nil || attempt.Result.Phase != "Drifted" || !attempt.TerminalSucceeded || attempt.Canonical == canonicalReplaced || cache.Load().Phase != "Error" {
				t.Fatalf("failed mutation lost its evidence: %+v", attempt)
			}
			store := faultGenerationStore(t, opts.StatePath, false)
			rows, err := store.ListGenerations(10)
			if err != nil || len(rows) != 1 || rows[0].Phase != "Drifted" || rows[0].FinishedAt.IsZero() {
				t.Fatalf("failed observation terminal history=%+v error=%v", rows, err)
			}
		})
	}
}

func TestMutationPublishesChangedShapeAfterCanonicalAcknowledgement(t *testing.T) {
	_, opts := faultApplyFixture(t)
	if err := os.WriteFile(opts.ConfigPath, []byte(dnsRuntimeYAML("127.0.0.1")), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := config.Load(opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	current := old
	published := 0
	opts.attemptHooks.writeCanonical = func(path string, data []byte) (config.AtomicWriteOutcome, error) {
		if current != old {
			t.Error("candidate was published before canonical replacement")
		}
		return config.AtomicWriteFileWithOutcome(path, data)
	}
	m := serveConfigMutator{
		configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: &resultCache{},
		getRouter: func() *api.Router { return current },
		setRouter: func(router *api.Router) {
			data, err := os.ReadFile(opts.ConfigPath)
			if err != nil || !strings.Contains(string(data), "127.0.0.2") {
				t.Error("runtime publication preceded canonical acknowledgement")
			}
			current = router
			published++
		},
		reload: func(context.Context, *api.Router) error { return nil },
	}
	if _, err := m.apply(nil, controlapi.ApplyRequest{CandidateYAML: dnsRuntimeYAML("127.0.0.2"), Replace: true}); err != nil {
		t.Fatal(err)
	}
	if current == old || published != 1 {
		t.Fatalf("committed runtime publication count=%d current=%p old=%p", published, current, old)
	}
}

func TestNoReconcilePlanReadsCommittedIntentWithoutPublishingRuntime(t *testing.T) {
	_, opts := faultApplyFixture(t)
	old, err := config.Load(opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	current := old
	m := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: &resultCache{}, getRouter: func() *api.Router { return current }, setRouter: func(next *api.Router) { current = next }}
	candidate := strings.Replace(testRouterYAML("new-router"), "resources: []", `resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Hostname
      metadata: {name: pending-hostname}
      spec: {hostname: pending.example}`, 1)
	committed, err := m.apply(nil, controlapi.ApplyRequest{CandidateYAML: candidate, Replace: true, NoReconcile: true})
	if err != nil || committed == nil || committed.Result.Phase != "Committed" || current != old {
		t.Fatalf("NoReconcile changed runtime or lost commit: result=%+v error=%v", committed, err)
	}
	plan, err := m.plan(nil, controlapi.PlanRequest{})
	if err != nil || plan == nil || len(plan.Result.Resources) != 1 || !strings.HasSuffix(plan.Result.Resources[0].ID, "/Hostname/pending-hostname") {
		t.Fatalf("empty plan ignored committed canonical intent: result=%+v error=%v", plan, err)
	}
	if current != old {
		t.Fatal("plan published a runtime declaration")
	}
	store := faultGenerationStore(t, opts.StatePath, false)
	rows, err := store.ListGenerations(10)
	if err != nil || len(rows) != 1 || rows[0].Phase != "Committed" {
		t.Fatalf("plan changed configuration history: %+v %v", rows, err)
	}
}

func TestDriftedPlanRemainsReadOnly(t *testing.T) {
	_, opts := faultApplyFixture(t)
	input := dnsRuntimeYAML("127.0.0.1") + missingBridgeYAML
	router, err := config.LoadBytes([]byte(input), "candidate")
	if err != nil {
		t.Fatal(err)
	}
	opts.DryRun = true
	result, err := runApplyChainOnce(context.Background(), router, opts, io.Discard, nil)
	if err != nil || result == nil || result.Phase != "Drifted" {
		t.Fatalf("plan treated desired change as failed mutation: %+v %v", result, err)
	}
	if _, err := os.Stat(opts.StatePath); !os.IsNotExist(err) {
		t.Fatalf("plan created generation state: %v", err)
	}
}

func TestServeOnceRejectsNonconvergenceWithoutLatchingScheduledHealth(t *testing.T) {
	for _, mode := range []string{"one-shot", "scheduled", "dry-run"} {
		t.Run(mode, func(t *testing.T) {
			_, opts := faultApplyFixture(t)
			input := dnsRuntimeYAML("127.0.0.1") + missingBridgeYAML
			router, err := config.LoadBytes([]byte(input), "candidate")
			if err != nil {
				t.Fatal(err)
			}
			opts.ConfigYAMLOverride = input
			opts.DryRun = mode == "dry-run"
			store := faultGenerationStore(t, opts.StatePath, false)
			runner := &controllerchain.Runner{Router: router, Store: store, Bus: bus.New(), Opts: controllerchain.Options{EnabledControllers: []string{"log-retention"}}}
			var output strings.Builder
			result, err := runServeChainOnceWith(context.Background(), runner, router, opts, store, &output, nil, mode == "scheduled")
			if (err != nil) != (mode == "one-shot") || result == nil || result.Phase != "Drifted" {
				t.Fatalf("%s convergence result=%+v error=%v", mode, result, err)
			}
			if mode == "one-shot" && output.Len() != 0 {
				t.Fatal("one-shot wrote success output before rejecting failed convergence")
			}
			rows, rowErr := store.ListGenerations(10)
			if rowErr != nil {
				t.Fatal(rowErr)
			}
			if mode == "one-shot" {
				if len(rows) != 1 || rows[0].Phase != "Drifted" || rows[0].FinishedAt.IsZero() {
					t.Fatalf("one-shot lost terminal nonconvergence evidence: %+v", rows)
				}
			} else if len(rows) != 0 {
				t.Fatalf("%s fabricated a new config attempt: %+v", mode, rows)
			}
		})
	}
}

func TestDeletePublishesIntentAndOnlyAcknowledgedDeletion(t *testing.T) {
	for _, mode := range []string{"dry-run", "no-reconcile", "standby", "sandbox"} {
		t.Run(mode, func(t *testing.T) {
			_, opts := faultApplyFixture(t)
			router, err := config.LoadBytes([]byte(testRouterYAML("old-router")), "current")
			if err != nil {
				t.Fatal(err)
			}
			router.Spec.Resources = append(router.Spec.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Hostname"},
				Metadata: api.ObjectMeta{Name: "appliance"}, Spec: api.HostnameSpec{Hostname: "appliance.example"},
			})
			if mode == "standby" {
				leasePath := filepath.Join(t.TempDir(), "lease")
				leader, err := ha.Acquire(context.Background(), ha.Config{Identity: "leader", Peers: []string{"leader", "standby"}, LeasePath: leasePath})
				if err != nil {
					t.Fatal(err)
				}
				defer leader.Lease.Close()
				router.Spec.Resources = append(router.Spec.Resources, api.Resource{
					TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "RouterdCluster"},
					Metadata: api.ObjectMeta{Name: "local"}, Spec: api.RouterdClusterSpec{Identity: "standby", Peers: []string{"leader", "standby"}, LeasePath: leasePath},
				})
			}
			data, err := yaml.Marshal(router)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(opts.ConfigPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			old := router
			current := old
			opts.Sandbox = mode == "sandbox"
			m := serveConfigMutator{configPath: opts.ConfigPath, statePath: opts.StatePath, baseOpts: opts, cache: &resultCache{}, getRouter: func() *api.Router { return current }, setRouter: func(next *api.Router) { current = next }}
			result, err := m.delete(nil, controlapi.DeleteRequest{Target: "Hostname/appliance", DryRun: mode == "dry-run", NoReconcile: mode == "no-reconcile"})
			if err != nil || result == nil || result.Target != api.NetAPIVersion+"/Hostname/appliance" {
				t.Fatalf("delete intent/result = %+v %v", result, err)
			}
			committed := mode == "no-reconcile" || mode == "sandbox"
			if (len(result.Deleted) != 0) != committed {
				t.Fatalf("delete claimed effect before acknowledgement: %+v", result)
			}
			after, err := os.ReadFile(opts.ConfigPath)
			if err != nil || (string(after) != string(data)) != committed {
				t.Fatalf("canonical deletion acknowledgement mismatch: %s %v", after, err)
			}
			if mode == "no-reconcile" && (current != old || result.Result.Phase != "Committed") {
				t.Fatalf("NoReconcile changed active runtime: %+v", result)
			}
			if mode == "sandbox" && result.Result.Phase != "Committed" {
				t.Fatalf("sandbox claimed dataplane convergence: %+v", result)
			}
		})
	}
}
