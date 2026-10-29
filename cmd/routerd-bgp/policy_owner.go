// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"reflect"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
	gobgpapi "github.com/osrg/gobgp/v4/api"
)

// policyOwnerServer is required by the daemon's live policy transaction. A
// path-only collaborator cannot acknowledge policy publication or recovery.
type policyOwnerServer interface {
	pathServer
	SetPolicies(context.Context, *gobgpapi.SetPoliciesRequest) error
	SetPolicyAssignment(context.Context, *gobgpapi.SetPolicyAssignmentRequest) error
	ResetPeer(context.Context, *gobgpapi.ResetPeerRequest) error
	AddPolicy(context.Context, *gobgpapi.AddPolicyRequest) error
	ListPolicy(context.Context, *gobgpapi.ListPolicyRequest, func(*gobgpapi.Policy)) error
	ListDefinedSet(context.Context, *gobgpapi.ListDefinedSetRequest, func(*gobgpapi.DefinedSet)) error
	ListPolicyAssignment(context.Context, *gobgpapi.ListPolicyAssignmentRequest, func(*gobgpapi.PolicyAssignment)) error
	ListPeer(context.Context, *gobgpapi.ListPeerRequest, func(*gobgpapi.Peer)) error
}

func reconcilePolicyConfig(ctx context.Context, server policyOwnerServer, statePath string, applied bgpdaemon.AppliedConfig, target bgpdaemon.PolicyIntent) (bgpdaemon.AppliedConfig, error) {
	if err := ctx.Err(); err != nil {
		return applied, err
	}
	if err := bgpdaemon.ValidatePolicyIntent(target); err != nil {
		return applied, err
	}
	target = bgpdaemon.NormalizePolicyIntent(target)
	paths := append(append([]bgpdaemon.AppliedPath(nil), applied.Paths...), applied.PendingPathAdds...)
	plan := bgpdaemon.BuildPolicyPlan(target, paths)
	matches, err := policyPlanMatches(ctx, server, plan)
	if err != nil {
		return applied, err
	}
	if matches && applied.PendingPolicy == nil {
		if reflect.DeepEqual(applied.Policy, target) {
			return applied, nil
		}
		applied.Policy = target
		return applied, bgpdaemon.WriteApplied(statePath, applied)
	}
	applied.PendingPolicy = &target
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		return applied, err
	}
	if err := ensurePolicyGuard(ctx, server); err != nil {
		return applied, err
	}
	for _, direction := range []gobgpapi.PolicyDirection{gobgpapi.PolicyDirection_POLICY_DIRECTION_IMPORT, gobgpapi.PolicyDirection_POLICY_DIRECTION_EXPORT} {
		guard := &gobgpapi.PolicyAssignment{Name: "global", Direction: direction, DefaultAction: gobgpapi.RouteAction_ROUTE_ACTION_REJECT, Policies: []*gobgpapi.Policy{{Name: bgpdaemon.PolicyGuardName}}}
		if err := server.SetPolicyAssignment(ctx, &gobgpapi.SetPolicyAssignmentRequest{Assignment: guard}); err != nil {
			return applied, err
		}
	}
	if err := server.SetPolicies(ctx, plan.SetPolicies); err != nil {
		return applied, err
	}
	if err := server.SetPolicyAssignment(ctx, &gobgpapi.SetPolicyAssignmentRequest{Assignment: plan.GlobalImportAssignment}); err != nil {
		return applied, err
	}
	if err := server.SetPolicyAssignment(ctx, &gobgpapi.SetPolicyAssignmentRequest{Assignment: plan.GlobalExportAssignment}); err != nil {
		return applied, err
	}
	var addresses []string
	if err := server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(peer *gobgpapi.Peer) {
		if address := peer.GetConf().GetNeighborAddress(); address != "" {
			addresses = append(addresses, address)
		}
	}); err != nil {
		return applied, err
	}
	for _, address := range addresses {
		if err := ctx.Err(); err != nil {
			return applied, err
		}
		if err := server.ResetPeer(ctx, &gobgpapi.ResetPeerRequest{Address: address, Soft: true, Direction: gobgpapi.ResetPeerRequest_DIRECTION_BOTH}); err != nil {
			return applied, fmt.Errorf("refresh BGP policy for %s: %w", address, err)
		}
	}
	applied.Policy, applied.PendingPolicy = target, nil
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		return applied, err
	}
	return applied, nil
}

func ensurePolicyGuard(ctx context.Context, server policyOwnerServer) error {
	var found *gobgpapi.Policy
	if err := server.ListPolicy(ctx, &gobgpapi.ListPolicyRequest{Name: bgpdaemon.PolicyGuardName}, func(policy *gobgpapi.Policy) { found = policy }); err != nil {
		return err
	}
	if found != nil {
		if !bgpdaemon.EqualPolicy(found, bgpdaemon.PolicyGuard()) {
			return fmt.Errorf("BGP policy update guard differs from its owned reject policy")
		}
		return nil
	}
	return server.AddPolicy(ctx, &gobgpapi.AddPolicyRequest{Policy: bgpdaemon.PolicyGuard()})
}

func policyPlanMatches(ctx context.Context, server policyOwnerServer, plan bgpdaemon.PolicyPlan) (bool, error) {
	for _, desired := range plan.SetPolicies.GetDefinedSets() {
		var observed *gobgpapi.DefinedSet
		if err := server.ListDefinedSet(ctx, &gobgpapi.ListDefinedSetRequest{DefinedType: desired.GetDefinedType(), Name: desired.GetName()}, func(set *gobgpapi.DefinedSet) { observed = set }); err != nil {
			return false, err
		}
		if !bgpdaemon.EqualDefinedSet(desired, observed) {
			return false, nil
		}
	}
	for _, desired := range plan.SetPolicies.GetPolicies() {
		var observed *gobgpapi.Policy
		if err := server.ListPolicy(ctx, &gobgpapi.ListPolicyRequest{Name: desired.GetName()}, func(policy *gobgpapi.Policy) { observed = policy }); err != nil {
			return false, err
		}
		if !bgpdaemon.EqualPolicy(desired, observed) {
			return false, nil
		}
	}
	for _, desired := range []*gobgpapi.PolicyAssignment{plan.GlobalImportAssignment, plan.GlobalExportAssignment} {
		var assignment *gobgpapi.PolicyAssignment
		if err := server.ListPolicyAssignment(ctx, &gobgpapi.ListPolicyAssignmentRequest{Name: "global", Direction: desired.Direction}, func(observed *gobgpapi.PolicyAssignment) { assignment = observed }); err != nil {
			return false, err
		}
		if !bgpdaemon.EqualPolicyAssignment(desired, assignment) {
			return false, nil
		}
	}
	return true, nil
}
