// SPDX-License-Identifier: BSD-3-Clause

// Package provideraction implements the gated provider-action EXECUTION path
// (ADR 0007, Phase 5). It defines the executor wire protocol, the only place an
// executor plugin process is launched (RunExecutor), and the execution engine
// that imports planned actions into the journal, evaluates the
// ProviderActionPolicy gate, enforces approval + idempotency, and journals every
// outcome.
//
// HARD INVARIANTS (ADR 0007):
//   - execution is disabled by default (policy.Enabled defaults false);
//   - an action executes only if approved (operator OR policy auto-approve);
//   - mode=execute is rejected unless DryRunOnly is false AND the gate passes;
//   - idempotencyKey is required and an already-succeeded key is NEVER re-run;
//   - every result is journaled;
//   - provider credentials NEVER traverse routerd core — the executor runs in
//     its own process with no inherited parent environment and authenticates
//     itself with cloud-native identity;
//   - Phase 5.0 calls NO real provider CLI/SDK — this package imports no cloud
//     SDK and the only executors are fakes.
package provideraction

import (
	"github.com/imksoo/routerd/pkg/plugin"
)

const (
	// ProtocolAPIVersion is the API group for the executor wire protocol.
	ProtocolAPIVersion = "provideraction.routerd.net/v1alpha1"

	// KindExecuteActionRequest is the kind written to the executor's stdin.
	KindExecuteActionRequest = "ExecuteActionRequest"
	// KindExecuteActionResult is the kind read from the executor's stdout.
	KindExecuteActionResult = "ExecuteActionResult"

	// CapabilityExecuteProviderAction is the PluginSpec.Capabilities value an
	// executor plugin MUST declare. RunExecutor refuses any plugin lacking it.
	CapabilityExecuteProviderAction = "execute.providerAction"

	// ModeDryRun reports what would happen WITHOUT mutating.
	ModeDryRun = "dry-run"
	// ModeExecute performs the (gated, approved) mutation.
	ModeExecute = "execute"

	// ResultSucceeded / ResultFailed / ResultSkipped are the executor's reported
	// outcome states on stdout (distinct from the journal status constants in
	// pkg/state, though they map onto them).
	ResultSucceeded = "succeeded"
	ResultFailed    = "failed"
	ResultSkipped   = "skipped"
)

// TypeMeta is the apiVersion/kind envelope for the wire objects. It is defined
// locally so the protocol stays self-contained.
type TypeMeta struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
}

// ExecuteActionRequest is the JSON object routerd writes to an executor's stdin.
// It carries the approved action plan (NO secrets) plus an optional Phase-4.0
// redacted, allowlisted context. The executor authenticates itself with its own
// cloud-native identity; routerd passes it no credentials.
type ExecuteActionRequest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Spec     ExecuteActionRequestSpec `json:"spec" yaml:"spec"`
}

// ExecuteActionRequestSpec is the executor request body.
type ExecuteActionRequestSpec struct {
	// Action is the canonical action verb (e.g. assign-secondary-ip).
	Action string `json:"action" yaml:"action"`
	// Provider is the cloud provider (aws/azure/oci/...).
	Provider string `json:"provider" yaml:"provider"`
	// ProviderRef optionally names the CloudProviderProfile.
	ProviderRef string `json:"providerRef,omitempty" yaml:"providerRef,omitempty"`
	// Target is the non-secret action target (e.g. address, nicRef, region).
	Target map[string]string `json:"target,omitempty" yaml:"target,omitempty"`
	// Parameters are the non-secret action parameters.
	Parameters map[string]string `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	// Mode is "dry-run" (no mutation, report only) or "execute" (the gated
	// mutation). When Mode=="dry-run" the executor MUST NOT mutate.
	Mode string `json:"mode" yaml:"mode"`
	// IdempotencyKey is the dedup key; the executor SHOULD itself be idempotent
	// on this key so a retried request never double-applies.
	IdempotencyKey string `json:"idempotencyKey" yaml:"idempotencyKey"`
	// Context is the Phase-4.0 allowlisted, secret-redacted config the executor
	// may read. It carries NO secrets and is optional.
	Context plugin.PluginContext `json:"context,omitempty" yaml:"context,omitempty"`
}

// ExecuteActionResult is the JSON object an executor writes to stdout.
type ExecuteActionResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Status   ExecuteActionResultStatus `json:"status" yaml:"status"`
}

// ExecuteActionResultStatus is the executor result body.
type ExecuteActionResultStatus struct {
	// Status is "succeeded", "failed", or "skipped".
	Status string `json:"status" yaml:"status"`
	// Message is a human-readable summary (for dry-run, "would <action>").
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
	// Observed carries non-secret observed facts (e.g. assignedAddress,
	// nicState). It MUST NOT contain credentials.
	Observed map[string]string `json:"observed,omitempty" yaml:"observed,omitempty"`
	// UndoAvailable reports whether the executor can reverse this action.
	UndoAvailable bool `json:"undoAvailable,omitempty" yaml:"undoAvailable,omitempty"`
	// Error is an executor-side error description when Status=="failed".
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
}

// NewExecuteActionRequest builds a request envelope with the protocol
// apiVersion/kind set.
func NewExecuteActionRequest(spec ExecuteActionRequestSpec) ExecuteActionRequest {
	return ExecuteActionRequest{
		TypeMeta: TypeMeta{APIVersion: ProtocolAPIVersion, Kind: KindExecuteActionRequest},
		Spec:     spec,
	}
}

// hasCapability reports whether caps contains the executor capability.
func hasCapability(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

// validMode reports whether mode is one of the two permitted executor modes.
func validMode(mode string) bool {
	return mode == ModeDryRun || mode == ModeExecute
}
