// SPDX-License-Identifier: BSD-3-Clause

// Package dynamicconfig defines the runtime configuration fragments produced by
// trusted local sources and merged with startup configuration by routerd.
package dynamicconfig

import (
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	// ConfigAPIVersion is the API group for dynamic configuration objects.
	ConfigAPIVersion = api.ConfigAPIVersion
	// HybridAPIVersion is the API group for hybrid cloud/on-prem resources.
	HybridAPIVersion = api.HybridAPIVersion

	// DirectiveOpMask suppresses a matching startup-config resource while the
	// directive is active.
	DirectiveOpMask = "mask"
)

// DynamicConfigPart is one generated runtime configuration fragment.
//
// DynamicConfigPart objects are produced by trusted local plugins or other
// dynamic sources and are stored separately from the human-managed startup
// configuration.
type DynamicConfigPart struct {
	api.TypeMeta `yaml:",inline" json:",inline"`
	Metadata     api.ObjectMeta        `yaml:"metadata" json:"metadata"`
	Spec         DynamicConfigPartSpec `yaml:"spec" json:"spec"`
}

// DynamicConfigPartSpec describes the resources and directives observed from a
// dynamic source at one generation.
//
// ActionPlans are advisory, display-only provider operations a plugin proposed.
// routerd NEVER executes an ActionPlan and NEVER invokes a provider CLI/SDK
// from these: they are persisted purely so EventSubscription-driven plugin runs
// stay reviewable. They are NOT resources and are not merged into the effective
// config.
type DynamicConfigPartSpec struct {
	Source      string                   `yaml:"source" json:"source"`
	Generation  int64                    `yaml:"generation" json:"generation"`
	ObservedAt  time.Time                `yaml:"observedAt" json:"observedAt"`
	ExpiresAt   time.Time                `yaml:"expiresAt" json:"expiresAt"`
	Digest      string                   `yaml:"digest" json:"digest"`
	Resources   []api.Resource           `yaml:"resources" json:"resources"`
	Directives  []DynamicConfigDirective `yaml:"directives" json:"directives"`
	ActionPlans []ActionPlan             `yaml:"actionPlans,omitempty" json:"actionPlans,omitempty"`
}

// ActionPlan is a plugin-proposed provider operation recorded for dry-run and
// display only. routerd never executes an ActionPlan and never invokes a
// provider CLI/SDK; it is data the operator reviews.
//
// It is defined here (the lower-level package) rather than in pkg/plugin so
// DynamicConfigPartSpec can carry it without an import cycle (pkg/plugin imports
// pkg/dynamicconfig, not the reverse). pkg/plugin aliases these types.
type ActionPlan struct {
	Name            string            `yaml:"name" json:"name"`
	Provider        string            `yaml:"provider" json:"provider"`
	Action          string            `yaml:"action" json:"action"`
	Target          map[string]string `yaml:"target" json:"target"`
	ProviderRef     string            `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	Mode            string            `yaml:"mode,omitempty" json:"mode,omitempty"`
	Description     string            `yaml:"description,omitempty" json:"description,omitempty"`
	RiskLevel       string            `yaml:"riskLevel,omitempty" json:"riskLevel,omitempty"`
	IdempotencyKey  string            `yaml:"idempotencyKey,omitempty" json:"idempotencyKey,omitempty"`
	Parameters      map[string]string `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Preconditions   []ActionCheck     `yaml:"preconditions,omitempty" json:"preconditions,omitempty"`
	ExpectedEffects []string          `yaml:"expectedEffects,omitempty" json:"expectedEffects,omitempty"`
	Undo            *ActionUndo       `yaml:"undo,omitempty" json:"undo,omitempty"`
}

// ActionCheck is a display-only precondition a plugin attached to an ActionPlan.
// routerd does not evaluate it.
type ActionCheck struct {
	Name   string            `yaml:"name" json:"name"`
	Expect string            `yaml:"expect,omitempty" json:"expect,omitempty"`
	Detail string            `yaml:"detail,omitempty" json:"detail,omitempty"`
	Target map[string]string `yaml:"target,omitempty" json:"target,omitempty"`
}

// ActionUndo describes the inverse provider operation for display only.
type ActionUndo struct {
	Action     string            `yaml:"action" json:"action"`
	Parameters map[string]string `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// IsExpired reports whether the part's expiresAt timestamp is at or before now.
func (p *DynamicConfigPart) IsExpired(now time.Time) bool {
	if p == nil || p.Spec.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(p.Spec.ExpiresAt)
}

// DynamicConfigDirective changes how effective-config is derived without
// mutating startup-config.
type DynamicConfigDirective struct {
	Op     string          `yaml:"op" json:"op"`
	Target DirectiveTarget `yaml:"target" json:"target"`
	Reason string          `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// DirectiveTarget identifies one resource by API version, kind, and name.
type DirectiveTarget struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Name       string `yaml:"name" json:"name"`
}

// DynamicOverridePolicy grants dynamic sources permission to use directives
// against selected startup resources.
type DynamicOverridePolicy struct {
	api.TypeMeta `yaml:",inline" json:",inline"`
	Metadata     api.ObjectMeta            `yaml:"metadata" json:"metadata"`
	Spec         DynamicOverridePolicySpec `yaml:"spec" json:"spec"`
}

// DynamicOverridePolicySpec lists allowed dynamic override rules.
type DynamicOverridePolicySpec struct {
	Allow []OverrideAllowRule `yaml:"allow" json:"allow"`
}

// OverrideAllowRule allows a source to perform operations on selected targets.
type OverrideAllowRule struct {
	Source     string            `yaml:"source" json:"source"`
	Operations []string          `yaml:"operations" json:"operations"`
	Targets    []DirectiveTarget `yaml:"targets" json:"targets"`
}
