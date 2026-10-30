// SPDX-License-Identifier: BSD-3-Clause

package provideractioncontroller

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	enginepkg "github.com/imksoo/routerd/pkg/provideraction"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestControllerRechecksAssignmentLeaseAfterProviderWait(t *testing.T) {
	store := controllerStore(t)
	now := fixedNow()
	second := controllerPlan("second", "10.0.0.6/32")
	second.Parameters = map[string]string{"mobilityAssignmentGeneration": "generation-1", "mobilityAssignmentDesiredHolder": "node-a", "mobilityAssignmentLeaseUntil": now.Add(time.Second).Format(time.RFC3339Nano)}
	seedPart(t, store, []dynamicconfig.ActionPlan{controllerPlan("first", "10.0.0.5/32"), second})
	calls := 0
	controller := Controller{Router: controllerRouter(controllerPolicy(false, 2)), Store: store, Now: func() time.Time { return now }, Runner: func(context.Context, api.PluginSpec, enginepkg.ExecuteActionRequest) (enginepkg.ExecuteActionResult, enginepkg.RunOutcome, error) {
		calls++
		now = now.Add(2 * time.Second)
		return enginepkg.ExecuteActionResult{Status: enginepkg.ExecuteActionResultStatus{Status: enginepkg.ResultSucceeded}}, enginepkg.RunOutcome{}, nil
	}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d, expired second assignment executed", calls)
	}
	if record := actionByKey(t, store, "first"); !record.ExecutedAt.Equal(now) {
		t.Fatalf("result timestamp = %s, want actual completion %s", record.ExecutedAt, now)
	}
	if record := actionByKey(t, store, "second"); record.Status != routerstate.ActionSkipped || !strings.Contains(record.ResultMessage, "lease expired") {
		t.Fatalf("expired assignment result = %+v", record)
	}
}

func TestControllerReturnsProviderFailureAndContinuesBoundedBatch(t *testing.T) {
	store := controllerStore(t)
	seedPart(t, store, []dynamicconfig.ActionPlan{controllerPlan("first", "10.0.0.5/32"), controllerPlan("second", "10.0.0.6/32")})
	calls := 0
	controller := Controller{Router: controllerRouter(controllerPolicy(false, 2)), Store: store, Now: fixedNow, Runner: func(_ context.Context, _ api.PluginSpec, req enginepkg.ExecuteActionRequest) (enginepkg.ExecuteActionResult, enginepkg.RunOutcome, error) {
		calls++
		status := enginepkg.ExecuteActionResultStatus{Status: enginepkg.ResultSucceeded}
		if req.Spec.IdempotencyKey == "first" {
			status.Status, status.Error = enginepkg.ResultFailed, "provider denied mutation"
		}
		return enginepkg.ExecuteActionResult{Status: status}, enginepkg.RunOutcome{}, nil
	}}
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "provider denied mutation") {
		t.Fatalf("reconcile hid provider failure: %v", err)
	}
	if calls != 2 || actionByKey(t, store, "first").Status != routerstate.ActionFailed || actionByKey(t, store, "second").Status != routerstate.ActionSucceeded {
		t.Fatalf("batch calls=%d first=%+v second=%+v", calls, actionByKey(t, store, "first"), actionByKey(t, store, "second"))
	}
}
