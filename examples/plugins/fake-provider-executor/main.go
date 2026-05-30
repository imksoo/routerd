// SPDX-License-Identifier: BSD-3-Clause

// Command fake-provider-executor is a FAKE, provider-AGNOSTIC routerd executor
// plugin (ADR 0007, Phase 5.0) used to exercise the provider-action EXECUTION
// framework end-to-end WITHOUT touching any real cloud.
//
// FAKE — performs NO real cloud action. It imports no cloud SDK, no os/exec, and
// makes no network call. It only reads an ExecuteActionRequest JSON from stdin
// and writes an ExecuteActionResult JSON to stdout. It mutates nothing.
//
// Outcome control (so tests can drive the journal path):
//   - default                                   -> succeeded
//   - Parameters["fakeOutcome"] == "failed"     -> failed
//   - Parameters["fakeOutcome"] == "skipped"    -> skipped
//   - env FAKE_OUTCOME (failed|skipped)         -> overrides the default
//
// Mode handling:
//   - Mode == "dry-run" -> succeeded with Message "would <action>" and NO side
//     effect (it has none anyway). It NEVER mutates in either mode.
//
// UndoAvailable is reported true for the assign-secondary-ip action so the
// rollback path can be exercised.
//
// Capability: the accompanying plugin.yaml declares execute.providerAction.
// Like the other example plugins this depends only on the Go standard library
// and mirrors the routerd wire JSON locally; it imports neither pkg/provideraction
// nor pkg/plugin nor pkg/api.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// typeMeta is the apiVersion/kind envelope.
type typeMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// executeActionRequest mirrors provideraction.ExecuteActionRequest.
type executeActionRequest struct {
	typeMeta `json:",inline"`
	Spec     executeActionRequestSpec `json:"spec"`
}

type executeActionRequestSpec struct {
	Action         string            `json:"action"`
	Provider       string            `json:"provider"`
	ProviderRef    string            `json:"providerRef,omitempty"`
	Target         map[string]string `json:"target,omitempty"`
	Parameters     map[string]string `json:"parameters,omitempty"`
	Mode           string            `json:"mode"`
	IdempotencyKey string            `json:"idempotencyKey"`
	// Context is intentionally ignored by the fake (it carries no secrets and
	// the fake needs none); decoded loosely so unknown shapes never break it.
	Context json.RawMessage `json:"context,omitempty"`
}

// executeActionResult mirrors provideraction.ExecuteActionResult.
type executeActionResult struct {
	typeMeta `json:",inline"`
	Status   executeActionResultStatus `json:"status"`
}

type executeActionResultStatus struct {
	Status        string            `json:"status"`
	Message       string            `json:"message,omitempty"`
	Observed      map[string]string `json:"observed,omitempty"`
	UndoAvailable bool              `json:"undoAvailable,omitempty"`
	Error         string            `json:"error,omitempty"`
}

const (
	resultAPIVersion = "provideraction.routerd.net/v1alpha1"
	resultKind       = "ExecuteActionResult"

	modeDryRun = "dry-run"

	statusSucceeded = "succeeded"
	statusFailed    = "failed"
	statusSkipped   = "skipped"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "fake-provider-executor: %v\n", err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	var req executeActionRequest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("parse ExecuteActionRequest: %w", err)
		}
	}

	result := build(req)

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encode ExecuteActionResult: %w", err)
	}
	return nil
}

// build computes the fake result. It performs NO mutation in any mode.
func build(req executeActionRequest) executeActionResult {
	res := executeActionResult{
		typeMeta: typeMeta{APIVersion: resultAPIVersion, Kind: resultKind},
	}
	res.Status.UndoAvailable = req.Spec.Action == "assign-secondary-ip"

	outcome := desiredOutcome(req)

	switch outcome {
	case statusFailed:
		res.Status.Status = statusFailed
		res.Status.Message = fmt.Sprintf("fake %q failed (fakeOutcome=failed)", req.Spec.Action)
		res.Status.Error = "fake executor instructed to fail"
		return res
	case statusSkipped:
		res.Status.Status = statusSkipped
		res.Status.Message = fmt.Sprintf("fake %q skipped (fakeOutcome=skipped)", req.Spec.Action)
		return res
	}

	// Success path.
	res.Status.Status = statusSucceeded
	if req.Spec.Mode == modeDryRun {
		res.Status.Message = fmt.Sprintf("would %s", req.Spec.Action)
		// Dry-run: report only, no observed mutation facts.
		return res
	}
	res.Status.Message = fmt.Sprintf("faked %s", req.Spec.Action)
	// Non-secret observed facts a real executor might return. Purely synthetic.
	res.Status.Observed = map[string]string{}
	if addr := req.Spec.Target["address"]; addr != "" {
		res.Status.Observed["assignedAddress"] = addr
	}
	res.Status.Observed["nicState"] = "fake-attached"
	return res
}

// desiredOutcome resolves the requested outcome from parameters then env. An
// unrecognized value falls through to success.
func desiredOutcome(req executeActionRequest) string {
	if v := req.Spec.Parameters["fakeOutcome"]; v == statusFailed || v == statusSkipped {
		return v
	}
	if v := os.Getenv("FAKE_OUTCOME"); v == statusFailed || v == statusSkipped {
		return v
	}
	return statusSucceeded
}
