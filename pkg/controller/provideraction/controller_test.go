// SPDX-License-Identifier: BSD-3-Clause

package provideractioncontroller

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	enginepkg "github.com/imksoo/routerd/pkg/provideraction"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type fakeRunner struct {
	calls int
	last  enginepkg.ExecuteActionRequest
}

func (f *fakeRunner) run(ctx context.Context, spec api.PluginSpec, req enginepkg.ExecuteActionRequest) (enginepkg.ExecuteActionResult, enginepkg.RunOutcome, error) {
	f.calls++
	f.last = req
	return enginepkg.ExecuteActionResult{
		TypeMeta: enginepkg.TypeMeta{APIVersion: enginepkg.ProtocolAPIVersion, Kind: enginepkg.KindExecuteActionResult},
		Status:   enginepkg.ExecuteActionResultStatus{Status: enginepkg.ResultSucceeded, Message: "ok"},
	}, enginepkg.RunOutcome{}, nil
}

func TestControllerAutoApprovesAndExecutesWhenPolicyAllows(t *testing.T) {
	store := controllerStore(t)
	seedPart(t, store, []dynamicconfig.ActionPlan{controllerPlan("k1", "10.0.0.5/32")})
	runner := &fakeRunner{}
	controller := Controller{
		Router: controllerRouter(controllerPolicy(false, 5)),
		Store:  store,
		Runner: runner.run,
		Now:    fixedNow,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if runner.last.Spec.Mode != enginepkg.ModeExecute {
		t.Fatalf("mode = %q, want execute", runner.last.Spec.Mode)
	}
	rec := actionByKey(t, store, "k1")
	if rec.Status != routerstate.ActionSucceeded {
		t.Fatalf("status = %q, want succeeded", rec.Status)
	}
	if rec.ApprovedBy != "policy:auto-approve" {
		t.Fatalf("approvedBy = %q, want policy:auto-approve", rec.ApprovedBy)
	}
}

func TestControllerDoesNotAutoExecuteWhenPolicyRequiresManualApproval(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy api.ProviderActionPolicySpec
	}{
		{name: "approval-required", policy: controllerPolicy(true, 5)},
		{name: "dry-run-only", policy: mutatePolicy(controllerPolicy(false, 5), func(p *api.ProviderActionPolicySpec) { p.DryRunOnly = boolPtr(true) })},
		{name: "disabled", policy: mutatePolicy(controllerPolicy(false, 5), func(p *api.ProviderActionPolicySpec) { p.Enabled = false })},
		{name: "zero-max", policy: controllerPolicy(false, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := controllerStore(t)
			seedPart(t, store, []dynamicconfig.ActionPlan{controllerPlan("k1", "10.0.0.5/32")})
			runner := &fakeRunner{}
			controller := Controller{Router: controllerRouter(tc.policy), Store: store, Runner: runner.run, Now: fixedNow}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if runner.calls != 0 {
				t.Fatalf("runner calls = %d, want 0", runner.calls)
			}
			rec := actionByKey(t, store, "k1")
			if rec.Status != routerstate.ActionPending {
				t.Fatalf("status = %q, want pending", rec.Status)
			}
		})
	}
}

func TestControllerHonorsMaxActionsPerRun(t *testing.T) {
	store := controllerStore(t)
	seedPart(t, store, []dynamicconfig.ActionPlan{
		controllerPlan("k1", "10.0.0.5/32"),
		controllerPlan("k2", "10.0.0.6/32"),
	})
	runner := &fakeRunner{}
	controller := Controller{Router: controllerRouter(controllerPolicy(false, 1)), Store: store, Runner: runner.run, Now: fixedNow}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if rec := actionByKey(t, store, "k1"); rec.Status != routerstate.ActionSucceeded {
		t.Fatalf("k1 status = %q, want succeeded", rec.Status)
	}
	if rec := actionByKey(t, store, "k2"); rec.Status != routerstate.ActionPending {
		t.Fatalf("k2 status = %q, want pending", rec.Status)
	}
}

func TestControllerSkipsStaleOwnershipEpochBeforeExecute(t *testing.T) {
	store := controllerStore(t)
	if _, err := store.ReconcileMobilityOwnershipEpochs([]routerstate.MobilityOwnershipEpochRecord{{
		Pool: "cloudedge", Address: "10.0.0.5/32", OwnerNode: "node-a",
	}}); err != nil {
		t.Fatalf("seed ownership: %v", err)
	}
	if _, err := store.ReconcileMobilityOwnershipEpochs([]routerstate.MobilityOwnershipEpochRecord{{
		Pool: "cloudedge", Address: "10.0.0.5/32", OwnerNode: "node-b",
	}}); err != nil {
		t.Fatalf("bump ownership: %v", err)
	}
	plan := controllerPlan("k-stale", "10.0.0.5/32")
	plan.Parameters = map[string]string{
		"mobilityOwnershipPool":    "cloudedge",
		"mobilityOwnershipAddress": "10.0.0.5/32",
		"mobilityOwnershipEpoch":   "1",
		"mobilityOwnershipOwner":   "node-a",
	}
	params, err := json.Marshal(plan.Parameters)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	target, err := json.Marshal(plan.Target)
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	if _, err := store.ImportAction(routerstate.ActionExecutionRecord{
		IdempotencyKey: plan.IdempotencyKey,
		Source:         "test",
		Provider:       plan.Provider,
		ProviderRef:    plan.ProviderRef,
		Action:         plan.Action,
		TargetJSON:     string(target),
		ParametersJSON: string(params),
		Status:         routerstate.ActionPending,
	}); err != nil {
		t.Fatalf("ImportAction: %v", err)
	}
	runner := &fakeRunner{}
	controller := Controller{Router: controllerRouter(controllerPolicy(false, 5)), Store: store, Runner: runner.run, Now: fixedNow}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
	rec := actionByKey(t, store, "k-stale")
	if rec.Status != routerstate.ActionSkipped {
		t.Fatalf("status = %q, want skipped", rec.Status)
	}
}

func controllerStore(t *testing.T) *routerstate.SQLiteStore {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func seedPart(t *testing.T, store *routerstate.SQLiteStore, plans []dynamicconfig.ActionPlan) {
	t.Helper()
	raw, err := json.Marshal(plans)
	if err != nil {
		t.Fatalf("marshal plans: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:          "MobilityPool/cloudedge/node/node-a",
		Generation:      1,
		ObservedAt:      fixedNow(),
		ExpiresAt:       fixedNow().Add(time.Minute),
		Status:          "active",
		Digest:          "digest",
		ActionPlansJSON: string(raw),
	}); err != nil {
		t.Fatalf("UpsertDynamicConfigPart: %v", err)
	}
}

func controllerPlan(key, address string) dynamicconfig.ActionPlan {
	return dynamicconfig.ActionPlan{
		Name:           key,
		Provider:       "aws",
		ProviderRef:    "aws-prod",
		Action:         "assign-secondary-ip",
		Target:         map[string]string{"address": address, "nicRef": "eni-1"},
		IdempotencyKey: key,
	}
}

func controllerRouter(policy api.ProviderActionPolicySpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "ProviderActionPolicy"},
				Metadata: api.ObjectMeta{Name: "policy"},
				Spec:     policy,
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
				Metadata: api.ObjectMeta{Name: "aws-executor"},
				Spec: map[string]any{
					"executable":   "/bin/true",
					"capabilities": []any{enginepkg.CapabilityExecuteProviderAction},
				},
			},
		}},
	}
}

func controllerPolicy(requireApproval bool, maxActions int) api.ProviderActionPolicySpec {
	return api.ProviderActionPolicySpec{
		Enabled:          true,
		DryRunOnly:       boolPtr(false),
		RequireApproval:  boolPtr(requireApproval),
		AllowedProviders: []string{"aws"},
		AllowedActions:   []string{"assign-secondary-ip"},
		AllowedCIDRs:     []string{"10.0.0.0/24"},
		MaxActionsPerRun: maxActions,
	}
}

func mutatePolicy(policy api.ProviderActionPolicySpec, mutate func(*api.ProviderActionPolicySpec)) api.ProviderActionPolicySpec {
	mutate(&policy)
	return policy
}

func boolPtr(v bool) *bool { return &v }

func fixedNow() time.Time {
	return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
}

func actionByKey(t *testing.T, store *routerstate.SQLiteStore, key string) routerstate.ActionExecutionRecord {
	t.Helper()
	rec, ok, err := store.GetActionByIdempotencyKey(key)
	if err != nil || !ok {
		t.Fatalf("GetActionByIdempotencyKey(%s): ok=%v err=%v", key, ok, err)
	}
	return rec
}
