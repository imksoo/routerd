// SPDX-License-Identifier: BSD-3-Clause

// Command azure-provider-executor is a REAL Azure routerd executor plugin (ADR
// 0007, Phase 5.1) advertising the capability execute.providerAction. It performs
// the CloudEdge Selective Address Mobility NIC mutations — assigning a captured
// /32 to the cloud NIC's IP configuration and enabling IP forwarding on the NIC —
// through the gated, journaled execution path instead of by hand.
//
// REAL EXECUTOR — it mutates Azure, but ONLY in execute mode. In dry-run mode it
// issues read-only show/list calls and mutates nothing (enforced: see
// guardedRunner). It drives the Azure CLI (`az`) via an injectable command runner
// (the azRunner func var, default execRunner running the real `az` binary; tests
// inject a fake), so unit tests NEVER call real Azure.
//
// CREDENTIALS: it authenticates with the Azure managed identity that the `az` CLI
// resolves on its own. routerd core passes it NO credentials and inherits NO
// parent environment to it (see RunExecutor); the executor reads no Azure
// credentials from the request. It imports no Azure SDK — the ONLY external
// dependency is exec of the `az` CLI binary.
//
// Reads from the request Target: nicRef (NIC resource id), resourceGroup, NIC
// name, ipConfigName, address (the captured /32), region/subscriptionId when
// available. For update/show it prefers `--ids <nic resource id>`; for ip-config
// create/delete it uses `--resource-group <rg> --nic-name <name>`. A missing
// required field is a clear failed result.
//
// Capability: the accompanying plugin.yaml declares execute.providerAction.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// typeMeta is the apiVersion/kind envelope mirroring provideraction.TypeMeta.
type typeMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// executeActionRequest mirrors provideraction.ExecuteActionRequest. The plugin
// mirrors the routerd wire JSON locally (like the other example plugins) so it
// depends only on the Go standard library.
type executeActionRequest struct {
	typeMeta `json:",inline"`
	Spec     executeActionRequestSpec `json:"spec"`
}

type executeActionRequestSpec struct {
	Action      string            `json:"action"`
	Provider    string            `json:"provider"`
	ProviderRef string            `json:"providerRef,omitempty"`
	Target      map[string]string `json:"target,omitempty"`
	Parameters  map[string]string `json:"parameters,omitempty"`
	Mode        string            `json:"mode"`
	// IdempotencyKey is accepted but the executor itself relies on the journal
	// guard; decoded for completeness.
	IdempotencyKey string `json:"idempotencyKey"`
	// Context carries no secrets and the executor needs none; decoded loosely.
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

	modeDryRun  = "dry-run"
	modeExecute = "execute"

	statusSucceeded = "succeeded"
	statusFailed    = "failed"
	statusSkipped   = "skipped"

	actionAssignSecondaryIP   = "assign-secondary-ip"
	actionUnassignSecondaryIP = "unassign-secondary-ip"
	actionEnsureFwdEnabled    = "ensure-forwarding-enabled"
	actionEnsureFwdDisabled   = "ensure-forwarding-disabled"
	defaultAzCommandTimeoutMs = 25000
)

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout, defaultRunner()); err != nil {
		fmt.Fprintf(os.Stderr, "azure-provider-executor: %v\n", err)
		os.Exit(1)
	}
}

// run reads one ExecuteActionRequest, dispatches it, and writes one
// ExecuteActionResult. runner is the injectable az command runner.
func run(ctx context.Context, in io.Reader, out io.Writer, runner azRunner) error {
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

	result := dispatch(ctx, req, runner)

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return fmt.Errorf("encode ExecuteActionResult: %w", err)
	}
	return nil
}

// newResult builds an empty result envelope.
func newResult() executeActionResult {
	return executeActionResult{typeMeta: typeMeta{APIVersion: resultAPIVersion, Kind: resultKind}}
}

// failed builds a failed result with the given message + error.
func failed(msg string, err error) executeActionResult {
	res := newResult()
	res.Status.Status = statusFailed
	res.Status.Message = msg
	if err != nil {
		res.Status.Error = err.Error()
	}
	return res
}

// nicTarget bundles the Azure NIC identification read from the request Target.
type nicTarget struct {
	nicID         string // NIC resource id (for --ids on show/update)
	resourceGroup string
	nicName       string // for ip-config create/delete (--nic-name)
	ipConfigName  string
	address       string
}

// requireNICID requires the NIC resource id (used for show/update via --ids).
func requireNICID(spec executeActionRequestSpec) (nicTarget, error) {
	t := nicTarget{
		nicID:         spec.Target["nicRef"],
		resourceGroup: spec.Target["resourceGroup"],
		nicName:       spec.Target["nicName"],
		ipConfigName:  spec.Target["ipConfigName"],
		address:       bareIP(spec.Target["address"]),
	}
	if t.nicID == "" {
		return nicTarget{}, fmt.Errorf("target.nicRef (NIC resource id) is required")
	}
	return t, nil
}

// bareIP converts routerd's canonical host CIDR form ("10.88.60.9/32") into the
// provider API form ("10.88.60.9"). Invalid values are left unchanged so the
// provider CLI returns its native validation error.
func bareIP(address string) string {
	address = strings.TrimSpace(address)
	if ip, _, err := net.ParseCIDR(address); err == nil {
		return ip.String()
	}
	if ip := net.ParseIP(address); ip != nil {
		return ip.String()
	}
	return address
}

// requireIPConfigTarget requires the fields az ip-config create/delete need:
// resourceGroup + nicName + ipConfigName (and, for create, address).
func requireIPConfigTarget(spec executeActionRequestSpec, needAddress bool) (nicTarget, error) {
	t, err := requireNICID(spec)
	if err != nil {
		return nicTarget{}, err
	}
	if t.resourceGroup == "" {
		return nicTarget{}, fmt.Errorf("target.resourceGroup is required for ip-config operations")
	}
	if t.nicName == "" {
		return nicTarget{}, fmt.Errorf("target.nicName is required for ip-config operations")
	}
	if t.ipConfigName == "" {
		return nicTarget{}, fmt.Errorf("target.ipConfigName is required for ip-config operations")
	}
	if needAddress && t.address == "" {
		return nicTarget{}, fmt.Errorf("target.address is required")
	}
	return t, nil
}

// dispatch routes by (Action, Mode). It NEVER mutates in dry-run mode: dry-run
// paths use only show/list verbs through the guardedRunner, and the guard rejects
// any non-read-only verb so a coding mistake cannot mutate during a preview.
func dispatch(ctx context.Context, req executeActionRequest, runner azRunner) executeActionResult {
	spec := req.Spec
	mode := spec.Mode
	if mode != modeDryRun && mode != modeExecute {
		return failed(fmt.Sprintf("invalid mode %q (want dry-run or execute)", mode), nil)
	}
	if mode == modeDryRun {
		// Dry-run hard guard: only show/list verbs may be issued.
		runner = guardedRunner(runner)
	}

	switch spec.Action {
	case actionAssignSecondaryIP:
		return assignSecondaryIP(ctx, spec, mode, runner)
	case actionEnsureFwdEnabled:
		return ensureForwardingEnabled(ctx, spec, mode, runner)
	case actionUnassignSecondaryIP:
		return unassignSecondaryIP(ctx, spec, mode, runner)
	case actionEnsureFwdDisabled:
		return ensureForwardingDisabled(ctx, spec, mode, runner)
	default:
		return failed(fmt.Sprintf("unsupported action %q", spec.Action), nil)
	}
}

// assignSecondaryIP attaches the captured /32 to the NIC via a new ip-config.
//   - dry-run: show the NIC (read-only), report "would assign".
//   - execute: network nic ip-config create.
func assignSecondaryIP(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireIPConfigTarget(spec, true)
	if err != nil {
		return failed("assign-secondary-ip: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true

	if mode == modeDryRun {
		if _, derr := showNIC(ctx, runner, t.nicID); derr != nil {
			return failed("assign-secondary-ip dry-run: nic show failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would assign %s to %s", t.address, t.nicName)
		return res
	}

	if _, err := runner(ctx, "network", "nic", "ip-config", "create",
		"--resource-group", t.resourceGroup,
		"--nic-name", t.nicName,
		"--name", t.ipConfigName,
		"--private-ip-address", t.address); err != nil {
		return failed("assign-secondary-ip execute: ip-config create failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("assigned %s to %s (ip-config %s)", t.address, t.nicName, t.ipConfigName)
	res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": t.ipConfigName}
	return res
}

// ensureForwardingEnabled enables IP forwarding on the NIC.
//   - dry-run: show current enableIPForwarding, "would set ipForwarding=true".
//   - execute: FIRST show to capture prior ipForwarding into
//     Observed{priorIpForwarding}, THEN nic update --ip-forwarding true.
func ensureForwardingEnabled(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireNICID(spec)
	if err != nil {
		return failed("ensure-forwarding-enabled: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true

	nic, derr := showNIC(ctx, runner, t.nicID)
	if derr != nil {
		return failed("ensure-forwarding-enabled: nic show (capture prior) failed", derr)
	}
	prior := boolStr(nic.EnableIPForwarding)
	res.Status.Observed = map[string]string{"priorIpForwarding": prior}

	if mode == modeDryRun {
		res.Status.Status = statusSucceeded
		res.Status.Message = "would set ipForwarding=true"
		return res
	}

	if _, err := runner(ctx, "network", "nic", "update",
		"--ids", t.nicID,
		"--ip-forwarding", "true"); err != nil {
		return failed("ensure-forwarding-enabled execute: nic update failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("set ipForwarding=true on %s (prior=%s)", t.nicID, prior)
	return res
}

// unassignSecondaryIP is the undo of assign-secondary-ip: delete the ip-config.
func unassignSecondaryIP(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireIPConfigTarget(spec, false)
	if err != nil {
		return failed("unassign-secondary-ip: missing target field", err)
	}
	res := newResult()

	if mode == modeDryRun {
		// Read-only preview: confirm the NIC is showable.
		if _, derr := showNIC(ctx, runner, t.nicID); derr != nil {
			return failed("unassign-secondary-ip dry-run: nic show failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would unassign ip-config %s from %s", t.ipConfigName, t.nicName)
		return res
	}

	if _, err := runner(ctx, "network", "nic", "ip-config", "delete",
		"--resource-group", t.resourceGroup,
		"--nic-name", t.nicName,
		"--name", t.ipConfigName); err != nil {
		return failed("unassign-secondary-ip execute: ip-config delete failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("unassigned ip-config %s from %s", t.ipConfigName, t.nicName)
	return res
}

// ensureForwardingDisabled is the undo of ensure-forwarding-enabled. It applies
// the RESTORE-PRIOR rule using Parameters["priorIpForwarding"], which the
// engine.Rollback injects from the journal's recorded Observed:
//   - "true"  -> IP forwarding was ALREADY enabled before we touched it -> NO-OP,
//     status=skipped. We do NOT force it off.
//   - "false" -> it was off before -> restore by setting it false.
//
// It NEVER blind-forces: a blind disable would break a NIC that was already a
// forwarder for its own reasons.
func ensureForwardingDisabled(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireNICID(spec)
	if err != nil {
		return failed("ensure-forwarding-disabled: missing target field", err)
	}
	res := newResult()
	prior := spec.Parameters["priorIpForwarding"]
	res.Status.Observed = map[string]string{"priorIpForwarding": prior}

	switch prior {
	case "true":
		// Prior was already true (already a forwarder): nothing to restore.
		res.Status.Status = statusSkipped
		res.Status.Message = "prior ipForwarding was already true; nothing to restore"
		return res
	case "false":
		// fall through to restore (set ipForwarding false)
	default:
		return failed("ensure-forwarding-disabled: missing/invalid priorIpForwarding parameter (want true|false)", nil)
	}

	if mode == modeDryRun {
		res.Status.Status = statusSucceeded
		res.Status.Message = "would set ipForwarding=false (restore prior=false)"
		return res
	}

	if _, err := runner(ctx, "network", "nic", "update",
		"--ids", t.nicID,
		"--ip-forwarding", "false"); err != nil {
		return failed("ensure-forwarding-disabled execute: nic update failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("set ipForwarding=false on %s (restored prior=false)", t.nicID)
	return res
}

// boolStr renders a Go bool as the canonical "true"/"false" the journal stores.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// commandTimeout is the per-az-invocation timeout.
func commandTimeout() time.Duration {
	return defaultAzCommandTimeoutMs * time.Millisecond
}
