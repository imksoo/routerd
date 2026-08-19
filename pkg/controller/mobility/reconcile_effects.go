// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

// applyPoolPlan is the imperative half of reconciliation.  It applies the
// already-decided BGP and dynamic-config outputs, records transitions, then
// serializes the status projection.  It makes no ownership or capture choice.
func (c Controller) applyPoolPlan(ctx context.Context, state poolReconcileState, plan PoolPlan) error {
	if err := validatePoolPlanEffects(state.Runtime.Pool.Name, plan); err != nil {
		return fmt.Errorf("validate MobilityPool/%s typed plan before BGP apply: %w", state.Runtime.Pool.Name, err)
	}
	actionPlans := plan.ProviderActions
	if err := c.applyBGPPaths(ctx, state.Runtime.Pool.Source, plan.BGPPaths); err != nil {
		return err
	}
	if err := c.upsertBGPPlan(ctx, state.Runtime.Pool.Name, state.Runtime.Pool.SelfNode, actionPlans, plan.LocalDataplane, plan.FIBVerdicts, state.Runtime.Now); err != nil {
		return err
	}
	previousAssignments := captureAssignmentsFromActionPlans(state.Runtime.Provider.ActionHistory.previousCaptureAssigns)
	currentAssignments := captureAssignmentsFromActionPlans(actionPlans)
	onPremPending, onPremReason := onPremL2OwnershipPending(state.Runtime.Pool.Self, plan.Placement, plan.Addresses, state.Runtime.Previous.OnPremDiscovery, state.Runtime.Now)
	transitions, err := c.recordBGPCaptureAssignmentTransitions(ctx, state, plan, captureTransitionEffects{
		Previous:    previousAssignments,
		Current:     currentAssignments,
		ActionPlans: append(actionPlans, state.Runtime.Provider.ActionHistory.previousCaptureAssigns...),
	})
	if err != nil {
		return err
	}
	status := buildMobilityPoolStatus(mobilityPoolStatusInput{
		Runtime:             state.Runtime,
		Plan:                plan,
		Resolved:            state.Resolved,
		ActionPlans:         actionPlans,
		OnPremPending:       onPremPending,
		OnPremPendingReason: onPremReason,
		Transitions:         transitions,
	})
	return c.savePlannerStatus(state.Runtime.Pool.Name, status.Serialize())
}

// validatePoolPlanEffects validates every persisted plan boundary before BGP
// mutation. The persistence helper deliberately assumes this has succeeded so
// an invalid plan cannot leave BGP applied while its typed effects are rejected.
func validatePoolPlanEffects(poolName string, plan PoolPlan) error {
	if !plan.LocalDataplane.IsEmpty() {
		if err := dynamicconfig.ValidateMobilityDataplanePlanScope(plan.LocalDataplane, poolName); err != nil {
			return fmt.Errorf("local dataplane scope: %w", err)
		}
	}
	if len(plan.FIBVerdicts) == 0 {
		return nil
	}
	if err := dynamicconfig.ValidateMobilityFIBVerdicts(plan.FIBVerdicts, poolName); err != nil {
		return fmt.Errorf("FIB verdicts: %w", err)
	}
	if plan.LocalDataplane.IsEmpty() {
		return nil
	}
	for _, verdict := range plan.FIBVerdicts {
		if verdict.Scope == nil {
			continue
		}
		if verdict.PoolRef == "" || verdict.Scope.Prefix != plan.LocalDataplane.PoolPrefix {
			return fmt.Errorf("FIB scope prefix %q does not match local dataplane poolPrefix %q", verdict.Scope.Prefix, plan.LocalDataplane.PoolPrefix)
		}
		return nil
	}
	return fmt.Errorf("missing FIB pool scope")
}

func (c Controller) applyBGPPaths(ctx context.Context, source string, desired []bgpdaemon.AppliedPath) error {
	current, err := c.BGPPaths.ListPaths(ctx, source)
	if err != nil {
		return fmt.Errorf("list BGP mobility paths: %w", err)
	}
	stale := make(map[string]bgpdaemon.AppliedPath, len(current))
	for _, path := range current {
		stale[path.Prefix] = path
	}
	for _, path := range desired {
		if _, err := c.BGPPaths.UpsertPath(ctx, path); err != nil {
			return fmt.Errorf("upsert BGP mobility path %s: %w", path.Prefix, err)
		}
		delete(stale, path.Prefix)
	}
	for _, path := range stale {
		if err := c.BGPPaths.DeletePath(ctx, path); err != nil {
			return fmt.Errorf("delete stale BGP mobility path %s: %w", path.Prefix, err)
		}
	}
	return nil
}
