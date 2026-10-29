// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"fmt"
	"strings"
)

// Canonical provider action verbs an ActionPlan may propose. Proposal validation
// does not authorize execution by the separately gated provider-action engine.
const (
	ActionAssignSecondaryIP        = "assign-secondary-ip"
	ActionUnassignSecondaryIP      = "unassign-secondary-ip"
	ActionAssignRouteTableRoute    = "assign-route-table-route"
	ActionUnassignRouteTableRoute  = "unassign-route-table-route"
	ActionEnsureForwardingEnabled  = "ensure-forwarding-enabled"
	ActionEnsureForwardingDisabled = "ensure-forwarding-disabled"
)

// ActionModeDryRun is the only non-empty proposal mode. A proposal cannot select
// live execution; the operator/policy chooses it through the action engine.
const ActionModeDryRun = "dry-run"

// actionPlanExecuteRejection is the exact error text returned when an ActionPlan
// declares mode=execute. It is a const so callers/tests can match it precisely.
const actionPlanExecuteRejection = "actionPlan mode=execute is not supported; a proposal cannot authorize live execution"

var canonicalProviders = map[string]bool{
	"aws":   true,
	"azure": true,
	"oci":   true,
	"gcp":   true,
}

var canonicalActions = map[string]bool{
	ActionAssignSecondaryIP:        true,
	ActionUnassignSecondaryIP:      true,
	ActionAssignRouteTableRoute:    true,
	ActionUnassignRouteTableRoute:  true,
	ActionEnsureForwardingEnabled:  true,
	ActionEnsureForwardingDisabled: true,
}

var canonicalRiskLevels = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}

// ValidateActionPlan checks the proposal shape. It does not execute or approve
// the operation; the action engine owns the live policy and durable claim.
func ValidateActionPlan(p ActionPlan) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("actionPlan name is required")
	}
	if strings.TrimSpace(p.Provider) == "" {
		return fmt.Errorf("actionPlan %q provider is required", p.Name)
	}
	if !canonicalProviders[p.Provider] {
		return fmt.Errorf("actionPlan %q provider %q must be one of aws, azure, oci, gcp", p.Name, p.Provider)
	}
	if strings.TrimSpace(p.Action) == "" {
		return fmt.Errorf("actionPlan %q action is required", p.Name)
	}
	if !canonicalActions[p.Action] {
		return fmt.Errorf("actionPlan %q action %q must be one of assign-secondary-ip, unassign-secondary-ip, assign-route-table-route, unassign-route-table-route, ensure-forwarding-enabled, ensure-forwarding-disabled", p.Name, p.Action)
	}
	switch p.Action {
	case ActionAssignSecondaryIP, ActionUnassignSecondaryIP:
		if strings.TrimSpace(p.Target["address"]) == "" {
			return fmt.Errorf("actionPlan %q action %q requires target.address", p.Name, p.Action)
		}
		if strings.TrimSpace(p.Target["nicRef"]) == "" {
			return fmt.Errorf("actionPlan %q action %q requires target.nicRef", p.Name, p.Action)
		}
		if strings.TrimSpace(p.Target["captureStrategy"]) == "route-table" && strings.TrimSpace(p.Target["routeTableRef"]) == "" {
			return fmt.Errorf("actionPlan %q action %q with target.captureStrategy route-table requires target.routeTableRef", p.Name, p.Action)
		}
	case ActionAssignRouteTableRoute, ActionUnassignRouteTableRoute:
		if strings.TrimSpace(p.Target["address"]) == "" {
			return fmt.Errorf("actionPlan %q action %q requires target.address", p.Name, p.Action)
		}
		if strings.TrimSpace(p.Target["routeTableRef"]) == "" {
			return fmt.Errorf("actionPlan %q action %q requires target.routeTableRef", p.Name, p.Action)
		}
		if strings.TrimSpace(p.Target["nicRef"]) == "" {
			return fmt.Errorf("actionPlan %q action %q requires target.nicRef", p.Name, p.Action)
		}
	}
	switch p.Mode {
	case "", ActionModeDryRun:
		// accepted
	case "execute":
		return fmt.Errorf("%s", actionPlanExecuteRejection)
	default:
		return fmt.Errorf("actionPlan %q mode %q must be empty or dry-run", p.Name, p.Mode)
	}
	if p.RiskLevel != "" && !canonicalRiskLevels[p.RiskLevel] {
		return fmt.Errorf("actionPlan %q riskLevel %q must be one of low, medium, high", p.Name, p.RiskLevel)
	}
	if p.Undo != nil {
		if strings.TrimSpace(p.Undo.Action) == "" {
			return fmt.Errorf("actionPlan %q undo.action is required", p.Name)
		}
		if !canonicalActions[p.Undo.Action] {
			return fmt.Errorf("actionPlan %q undo.action %q must be one of assign-secondary-ip, unassign-secondary-ip, assign-route-table-route, unassign-route-table-route, ensure-forwarding-enabled, ensure-forwarding-disabled", p.Name, p.Undo.Action)
		}
	}
	return nil
}
