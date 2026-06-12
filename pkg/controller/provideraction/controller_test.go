// SPDX-License-Identifier: BSD-3-Clause

package provideractioncontroller

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
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

func TestControllerPublishesProviderCaptureChangedAfterSucceededAssign(t *testing.T) {
	store := controllerStore(t)
	seedPart(t, store, []dynamicconfig.ActionPlan{controllerPlan("k1", "10.0.0.5/32")})
	runner := &fakeRunner{}
	eventBus := bus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, unsubscribe := eventBus.Subscribe(ctx, bus.Subscription{Topics: []string{enginepkg.ProviderCaptureChangedEvent}}, 1)
	defer unsubscribe()
	controller := Controller{
		Router: controllerRouter(controllerPolicy(false, 5)),
		Bus:    eventBus,
		Store:  store,
		Runner: runner.run,
		Now:    fixedNow,
	}
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	select {
	case event := <-events:
		if event.Type != enginepkg.ProviderCaptureChangedEvent {
			t.Fatalf("event type = %q, want provider capture changed", event.Type)
		}
		if event.Attributes["action"] != "assign-secondary-ip" || event.Attributes["providerRef"] != "aws-prod" {
			t.Fatalf("event attributes = %#v", event.Attributes)
		}
	case <-time.After(time.Second):
		t.Fatal("provider capture changed event was not published")
	}
}

func TestControllerImportsStaticRemoteAddressClaimProviderAction(t *testing.T) {
	store := controllerStore(t)
	runner := &fakeRunner{}
	controller := Controller{
		Router: controllerRouterWithStaticClaim(controllerPolicy(false, 5)),
		Store:  store,
		Runner: runner.run,
		Now:    fixedNow,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	key := "remote-address-claim:app:aws:eni-1:assign-secondary-ip:10.0.0.5/32"
	rec := actionByKey(t, store, key)
	if rec.Status != routerstate.ActionSucceeded {
		t.Fatalf("status = %q, want succeeded", rec.Status)
	}
	if rec.Source != "RemoteAddressClaim" {
		t.Fatalf("source = %q, want RemoteAddressClaim", rec.Source)
	}
	target := map[string]string{}
	if err := json.Unmarshal([]byte(rec.TargetJSON), &target); err != nil {
		t.Fatalf("target json: %v", err)
	}
	if target["providerRef"] != "aws-prod" || target["nicRef"] != "eni-1" || target["address"] != "10.0.0.5/32" || target["region"] != "ap-northeast-1" {
		t.Fatalf("target = %#v", target)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
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

func TestControllerRequeuesAndExecutesStaleRunningAction(t *testing.T) {
	store := controllerStore(t)
	plan := controllerPlan("stale-running", "10.0.0.5/32")
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
		Status:         routerstate.ActionPending,
	}); err != nil {
		t.Fatalf("ImportAction: %v", err)
	}
	rec := actionByKey(t, store, "stale-running")
	if err := store.ApproveAction(rec.ID, "policy:auto-approve", fixedNow().Add(-4*time.Minute)); err != nil {
		t.Fatalf("ApproveAction: %v", err)
	}
	claimed, err := store.BeginActionExecution(rec.ID, fixedNow().Add(-3*time.Minute))
	if err != nil {
		t.Fatalf("BeginActionExecution: %v", err)
	}
	if !claimed {
		t.Fatal("stale action was not claimed")
	}

	runner := &fakeRunner{}
	controller := Controller{Router: controllerRouter(controllerPolicy(false, 5)), Store: store, Runner: runner.run, Now: fixedNow}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	rec = actionByKey(t, store, "stale-running")
	if rec.Status != routerstate.ActionSucceeded {
		t.Fatalf("status = %q, want succeeded", rec.Status)
	}
	if rec.ResultMessage != "ok" {
		t.Fatalf("resultMessage = %q, want ok", rec.ResultMessage)
	}
}

func TestControllerRequeuesAndExecutesFailedDesiredAction(t *testing.T) {
	store := controllerStore(t)
	plan := controllerPlan("failed-still-desired", "10.0.0.5/32")
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
		Status:         routerstate.ActionPending,
	}); err != nil {
		t.Fatalf("ImportAction: %v", err)
	}
	rec := actionByKey(t, store, "failed-still-desired")
	if err := store.ApproveAction(rec.ID, "policy:auto-approve", fixedNow().Add(-2*time.Minute)); err != nil {
		t.Fatalf("ApproveAction: %v", err)
	}
	claimed, err := store.BeginActionExecution(rec.ID, fixedNow().Add(-time.Minute))
	if err != nil {
		t.Fatalf("BeginActionExecution: %v", err)
	}
	if !claimed {
		t.Fatal("failed action was not claimed")
	}
	if err := store.MarkActionResult(rec.ID, routerstate.ActionFailed, "assign failed", "UnauthorizedOperation", nil, fixedNow().Add(-time.Minute)); err != nil {
		t.Fatalf("MarkActionResult: %v", err)
	}
	seedPart(t, store, []dynamicconfig.ActionPlan{plan})

	runner := &fakeRunner{}
	controller := Controller{Router: controllerRouter(controllerPolicy(false, 5)), Store: store, Runner: runner.run, Now: fixedNow}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	rec = actionByKey(t, store, "failed-still-desired")
	if rec.Status != routerstate.ActionSucceeded {
		t.Fatalf("status = %q, want succeeded", rec.Status)
	}
	if rec.Error != "" {
		t.Fatalf("error was not cleared: %q", rec.Error)
	}
}

func TestControllerSkipsStaleMobilityPathBeforeExecute(t *testing.T) {
	store := controllerStore(t)
	plan := controllerPlan("k-stale", "10.0.0.5/32")
	plan.Parameters = map[string]string{
		"mobilityPathSig": "prefix=10.0.0.5/32;nextHops=old",
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

func controllerRouterWithStaticClaim(policy api.ProviderActionPolicySpec) *api.Router {
	router := controllerRouter(policy)
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
			Metadata: api.ObjectMeta{Name: "aws-prod"},
			Spec: api.CloudProviderProfileSpec{
				Provider: "aws",
				Region:   "ap-northeast-1",
				Auth:     api.ProviderAuth{Mode: "external-command", Command: "aws"},
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
			Metadata: api.ObjectMeta{Name: "app"},
			Spec: api.RemoteAddressClaimSpec{
				Address:   "10.0.0.5/32",
				OwnerSide: "onprem",
				Capture: api.AddressCapture{
					Type:               "provider-secondary-ip",
					ProviderRef:        "aws-prod",
					NICRef:             "eni-1",
					ConfigureOSAddress: true,
					Interface:          "eth0",
				},
				Delivery: api.AddressDelivery{Mode: "route", PeerRef: "onprem", TunnelInterface: "ipip0"},
			},
		},
	)
	return router
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
