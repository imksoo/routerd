// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestCaptureAssignmentGenerationRetainsActivePlanAndChangesAfterWithdrawal(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	pool, decision, placement := captureAssignmentFixture()

	firstPlans := []dynamicconfig.ActionPlan{captureAssignmentPlanFixture(decision.Address)}
	stampBGPAssignmentFenceActionPlans(firstPlans, pool, map[string]ownershipDecision{decision.Address: decision}, placement, newProviderActionHistoryWithRevision(nil, nil, ""), now)
	firstGeneration := captureAssignmentsFromActionPlans(firstPlans)[decision.Address].Generation
	if firstGeneration == "" {
		t.Fatal("initial assignment generation is empty")
	}
	upsertCaptureAssignmentPart(t, store, pool, firstPlans, "active-assignment", now, now.Add(time.Hour))

	activePlans, revision, err := previousGeneratedActionPlans(store, pool.Name, pool.Self.NodeRef, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("previous active plans: %v", err)
	}
	if len(activePlans) != 1 || revision == "" {
		t.Fatalf("active plans=%#v revision=%q, want one active plan and revision", activePlans, revision)
	}
	retainedPlans := []dynamicconfig.ActionPlan{captureAssignmentPlanFixture(decision.Address)}
	stampBGPAssignmentFenceActionPlans(retainedPlans, pool, map[string]ownershipDecision{decision.Address: decision}, placement, newProviderActionHistoryWithRevision(activePlans, nil, revision), now.Add(time.Minute))
	if got := captureAssignmentsFromActionPlans(retainedPlans)[decision.Address].Generation; got != firstGeneration {
		t.Fatalf("active plan generation = %q, want retained %q", got, firstGeneration)
	}

	// The no-action part is a durable withdrawal even when the provider action
	// engine has not imported a journal row. Reintroduction must therefore get
	// a new fence generation rather than reviving the old pending operation.
	withdrawnAt := now.Add(2 * time.Minute)
	upsertCaptureAssignmentPart(t, store, pool, nil, "withdrawn", withdrawnAt, withdrawnAt.Add(time.Hour))
	withdrawnPlans, withdrawnRevision, err := previousGeneratedActionPlans(store, pool.Name, pool.Self.NodeRef, withdrawnAt)
	if err != nil {
		t.Fatalf("previous withdrawn plans: %v", err)
	}
	if len(withdrawnPlans) != 0 || withdrawnRevision == "" {
		t.Fatalf("withdrawn plans=%#v revision=%q, want no active assignment plan with revision", withdrawnPlans, withdrawnRevision)
	}
	reintroducedPlans := []dynamicconfig.ActionPlan{captureAssignmentPlanFixture(decision.Address)}
	stampBGPAssignmentFenceActionPlans(reintroducedPlans, pool, map[string]ownershipDecision{decision.Address: decision}, placement, newProviderActionHistoryWithRevision(withdrawnPlans, nil, withdrawnRevision), withdrawnAt)
	if got := captureAssignmentsFromActionPlans(reintroducedPlans)[decision.Address].Generation; got == firstGeneration || got == "" {
		t.Fatalf("reintroduced generation = %q, want a new generation after withdrawal from %q", got, firstGeneration)
	}
}

func TestPreviousGeneratedActionPlansExcludesExpiredAssignment(t *testing.T) {
	now := time.Date(2026, 8, 18, 13, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	pool, decision, placement := captureAssignmentFixture()
	plans := []dynamicconfig.ActionPlan{captureAssignmentPlanFixture(decision.Address)}
	stampBGPAssignmentFenceActionPlans(plans, pool, map[string]ownershipDecision{decision.Address: decision}, placement, newProviderActionHistoryWithRevision(nil, nil, ""), now)
	previousGeneration := captureAssignmentsFromActionPlans(plans)[decision.Address].Generation
	upsertCaptureAssignmentPart(t, store, pool, plans, "expired-assignment", now, now.Add(time.Minute))

	expiredAt := now.Add(2 * time.Minute)
	previous, revision, err := previousGeneratedActionPlans(store, pool.Name, pool.Self.NodeRef, expiredAt)
	if err != nil {
		t.Fatalf("previous expired plans: %v", err)
	}
	if len(previous) != 0 || revision == "" {
		t.Fatalf("expired plans=%#v revision=%q, want no reusable expired plan", previous, revision)
	}
	freshPlans := []dynamicconfig.ActionPlan{captureAssignmentPlanFixture(decision.Address)}
	stampBGPAssignmentFenceActionPlans(freshPlans, pool, map[string]ownershipDecision{decision.Address: decision}, placement, newProviderActionHistoryWithRevision(previous, nil, revision), expiredAt)
	if got := captureAssignmentsFromActionPlans(freshPlans)[decision.Address].Generation; got == previousGeneration || got == "" {
		t.Fatalf("expired plan generation = %q, want a fresh generation after %q", got, previousGeneration)
	}
}

func TestCaptureAssignmentGenerationRetainsConfirmedHolderConvergenceOnly(t *testing.T) {
	now := time.Date(2026, 8, 18, 14, 0, 0, 0, time.UTC)
	pool, previousDecision, placement := captureAssignmentFixture()
	firstPlans := []dynamicconfig.ActionPlan{captureAssignmentPlanFixture(previousDecision.Address)}
	stampBGPAssignmentFenceActionPlans(firstPlans, pool, map[string]ownershipDecision{previousDecision.Address: previousDecision}, placement, newProviderActionHistoryWithRevision(nil, nil, ""), now)
	first := firstPlans[0]
	firstGeneration := captureAssignmentsFromActionPlans(firstPlans)[previousDecision.Address].Generation

	journal := []routerstate.ActionExecutionRecord{captureAssignmentSucceededRecord(t, first, now.Add(time.Second))}
	converged := previousDecision
	converged.CaptureHolderNode = pool.Self.NodeRef
	converged.CaptureDisposition = dynamicconfig.CaptureDesired
	retained := []dynamicconfig.ActionPlan{captureAssignmentPlanFixture(previousDecision.Address)}
	stampBGPAssignmentFenceActionPlans(retained, pool, map[string]ownershipDecision{previousDecision.Address: converged}, placement, newProviderActionHistoryWithRevision(firstPlans, journal, "active"), now.Add(2*time.Second))
	if got := captureAssignmentsFromActionPlans(retained)[previousDecision.Address].Generation; got != firstGeneration {
		t.Fatalf("confirmed holder convergence generation = %q, want retained %q", got, firstGeneration)
	}

	// Without a successful current assign transition, a holder change remains a
	// fence boundary even when the current planner input otherwise converges.
	unconfirmed := []dynamicconfig.ActionPlan{captureAssignmentPlanFixture(previousDecision.Address)}
	stampBGPAssignmentFenceActionPlans(unconfirmed, pool, map[string]ownershipDecision{previousDecision.Address: converged}, placement, newProviderActionHistoryWithRevision(firstPlans, nil, "active"), now.Add(2*time.Second))
	if got := captureAssignmentsFromActionPlans(unconfirmed)[previousDecision.Address].Generation; got == firstGeneration || got == "" {
		t.Fatalf("unconfirmed holder convergence generation = %q, want a new fence after %q", got, firstGeneration)
	}
}

func captureAssignmentSucceededRecord(t *testing.T, plan dynamicconfig.ActionPlan, at time.Time) routerstate.ActionExecutionRecord {
	t.Helper()
	target, err := json.Marshal(plan.Target)
	if err != nil {
		t.Fatalf("marshal capture assignment target: %v", err)
	}
	parameters, err := json.Marshal(plan.Parameters)
	if err != nil {
		t.Fatalf("marshal capture assignment parameters: %v", err)
	}
	return routerstate.ActionExecutionRecord{
		ID:             1,
		IdempotencyKey: plan.IdempotencyKey,
		Provider:       plan.Provider,
		ProviderRef:    plan.ProviderRef,
		Action:         plan.Action,
		TargetJSON:     string(target),
		ParametersJSON: string(parameters),
		Status:         routerstate.ActionSucceeded,
		ExecutedAt:     at,
		UpdatedAt:      at,
	}
}

func captureAssignmentFixture() (NormalizedMobilityPool, ownershipDecision, PlacementDecision) {
	const address = "10.88.60.10/32"
	self := memberPlanInfo{NodeRef: "aws-router-b"}
	pool := NormalizedMobilityPool{
		Name:    "cloudedge",
		Self:    self,
		Members: map[string]memberPlanInfo{self.NodeRef: self, "aws-router-a": {NodeRef: "aws-router-a"}},
	}
	return pool, ownershipDecision{Address: address, CaptureHolderNode: "aws-router-a"}, PlacementDecision{
		Group:                 "aws-edge",
		Active:                true,
		ActiveNode:            self.NodeRef,
		ActiveIdentityNodeRef: "aws-router-a",
		Seize:                 true,
	}
}

func captureAssignmentPlanFixture(address string) dynamicconfig.ActionPlan {
	return dynamicconfig.ActionPlan{
		Provider:        "aws",
		ProviderRef:     "aws-provider",
		Action:          actionAssignSecondaryIP,
		Target:          map[string]string{"address": address, "providerRef": "aws-provider", "nicRef": "eni-b", "captureStrategy": captureStrategySecondaryIP},
		Parameters:      map[string]string{captureParamHolder: "aws-router-b", bgpPathSigParam: "prefix=" + address + ";nextHops=10.99.0.3", "allowReassignment": "true"},
		IdempotencyKey:  "capture-assignment-" + safeName(address),
		ExpectedEffects: []string{"fixture"},
	}
}

func upsertCaptureAssignmentPart(t *testing.T, store interface {
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
}, pool NormalizedMobilityPool, plans []dynamicconfig.ActionPlan, digest string, observedAt, expiresAt time.Time) {
	t.Helper()
	raw, err := json.Marshal(plans)
	if err != nil {
		t.Fatalf("marshal assignment plans: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord{
		Source:          DynamicSource(pool.Name, pool.Self.NodeRef),
		Generation:      dynamicGeneration,
		ObservedAt:      observedAt,
		ExpiresAt:       expiresAt,
		Digest:          digest,
		ActionPlansJSON: string(raw),
		Status:          "active",
	}); err != nil {
		t.Fatalf("upsert assignment part: %v", err)
	}
}
