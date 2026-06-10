// SPDX-License-Identifier: BSD-3-Clause

// Command aws-provider-executor is a REAL AWS routerd executor plugin (ADR 0007,
// Phase 5.1) advertising the capability execute.providerAction. It performs the
// CloudEdge Selective Address Mobility ENI mutations — assigning a captured /32
// to the cloud ENI and disabling its source/dest check — through the gated,
// journaled execution path instead of by hand.
//
// REAL EXECUTOR — it mutates AWS, but ONLY in execute mode. In dry-run mode it
// issues read-only describe-* calls and mutates nothing (enforced: see
// guardedRunner). It drives the AWS CLI (`aws`) via an injectable command runner
// (the awsRunner func var, default execRunner running the real `aws` binary;
// tests inject a fake), so unit tests NEVER call real AWS.
//
// CREDENTIALS: it authenticates with the EC2 instance IAM role (instance
// profile) that the AWS CLI resolves on its own. routerd core passes it NO
// credentials and inherits NO parent environment to it (see RunExecutor); the
// executor reads no AWS credentials from the request. It imports no AWS SDK — the
// ONLY external dependency is exec of the `aws` CLI binary.
//
// Reads from the request Target: nicRef (ENI id), address (the captured /32),
// region. A missing required field is a clear failed result.
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
	// IdempotencyKey is accepted but the executor itself relies on AWS API
	// idempotency / the journal guard; decoded for completeness.
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

	actionAssignSecondaryIP       = "assign-secondary-ip"
	actionUnassignSecondaryIP     = "unassign-secondary-ip"
	actionAssignRouteTableRoute   = "assign-route-table-route"
	actionUnassignRouteTableRoute = "unassign-route-table-route"
	actionEnsureFwdEnabled        = "ensure-forwarding-enabled"
	actionEnsureFwdDisabled       = "ensure-forwarding-disabled"
	defaultAWSCommandTimeoutMs    = 25000
)

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout, defaultRunner()); err != nil {
		fmt.Fprintf(os.Stderr, "aws-provider-executor: %v\n", err)
		os.Exit(1)
	}
}

// run reads one ExecuteActionRequest, dispatches it, and writes one
// ExecuteActionResult. runner is the injectable aws command runner.
func run(ctx context.Context, in io.Reader, out io.Writer, runner awsRunner) error {
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

// target extracts the required ENI/address/region from the request, erroring if
// any required field is missing. address is required for the IP actions; for the
// forwarding actions only nicRef + region are required.
func requireTarget(spec executeActionRequestSpec, needAddress bool) (eni, address, region string, err error) {
	eni = spec.Target["nicRef"]
	address = spec.Target["address"]
	region = spec.Target["region"]
	if eni == "" {
		return "", "", "", fmt.Errorf("target.nicRef (ENI id) is required")
	}
	if region == "" {
		return "", "", "", fmt.Errorf("target.region is required")
	}
	if needAddress && address == "" {
		return "", "", "", fmt.Errorf("target.address is required")
	}
	if address != "" {
		address = bareIP(address)
	}
	return eni, address, region, nil
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

// dispatch routes by (Action, Mode). It NEVER mutates in dry-run mode: dry-run
// paths use only describe-* verbs through the guardedRunner, and the guard
// rejects any non-describe verb so a coding mistake cannot mutate during a
// preview.
func dispatch(ctx context.Context, req executeActionRequest, runner awsRunner) executeActionResult {
	spec := req.Spec
	mode := spec.Mode
	if mode != modeDryRun && mode != modeExecute {
		return failed(fmt.Sprintf("invalid mode %q (want dry-run or execute)", mode), nil)
	}
	if mode == modeDryRun {
		// Dry-run hard guard: only describe-* verbs may be issued.
		runner = guardedRunner(runner)
	}

	switch spec.Action {
	case actionAssignSecondaryIP:
		return assignSecondaryIP(ctx, spec, mode, runner)
	case actionAssignRouteTableRoute:
		return assignRouteTableRoute(ctx, spec, mode, runner)
	case actionEnsureFwdEnabled:
		return ensureForwardingEnabled(ctx, spec, mode, runner)
	case actionUnassignSecondaryIP:
		return unassignSecondaryIP(ctx, spec, mode, runner)
	case actionUnassignRouteTableRoute:
		return unassignRouteTableRoute(ctx, spec, mode, runner)
	case actionEnsureFwdDisabled:
		return ensureForwardingDisabled(ctx, spec, mode, runner)
	default:
		return failed(fmt.Sprintf("unsupported action %q", spec.Action), nil)
	}
}

func requireRouteTarget(spec executeActionRequestSpec) (routeTable, eni, address, region string, err error) {
	routeTable = spec.Target["routeTableRef"]
	eni = spec.Target["nicRef"]
	address = spec.Target["address"]
	region = spec.Target["region"]
	if routeTable == "" {
		return "", "", "", "", fmt.Errorf("target.routeTableRef is required")
	}
	if eni == "" {
		return "", "", "", "", fmt.Errorf("target.nicRef (ENI id) is required")
	}
	if region == "" {
		return "", "", "", "", fmt.Errorf("target.region is required")
	}
	if address == "" {
		return "", "", "", "", fmt.Errorf("target.address is required")
	}
	return routeTable, eni, address, region, nil
}

func assignRouteTableRoute(ctx context.Context, spec executeActionRequestSpec, mode string, runner awsRunner) executeActionResult {
	routeTable, eni, address, region, err := requireRouteTarget(spec)
	if err != nil {
		return failed("assign-route-table-route: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true

	if mode == modeDryRun {
		if _, derr := runner(ctx, "ec2", "describe-route-tables", "--route-table-ids", routeTable, "--region", region); derr != nil {
			return failed("assign-route-table-route dry-run: describe route table failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would route %s to %s in %s", address, eni, routeTable)
		return res
	}

	allowReassignment := stringBool(spec.Parameters["allowReassignment"])
	if allowReassignment {
		if _, err := runner(ctx, "ec2", "replace-route",
			"--route-table-id", routeTable,
			"--destination-cidr-block", address,
			"--network-interface-id", eni,
			"--region", region); err != nil {
			if _, cerr := runner(ctx, "ec2", "create-route",
				"--route-table-id", routeTable,
				"--destination-cidr-block", address,
				"--network-interface-id", eni,
				"--region", region); cerr != nil {
				return failed("assign-route-table-route execute: replace/create route failed", fmt.Errorf("replace: %v; create: %w", err, cerr))
			}
		}
	} else if _, err := runner(ctx, "ec2", "create-route",
		"--route-table-id", routeTable,
		"--destination-cidr-block", address,
		"--network-interface-id", eni,
		"--region", region); err != nil {
		return failed("assign-route-table-route execute: create route failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("routed %s to %s in %s", address, eni, routeTable)
	res.Status.Observed = map[string]string{"assignedRoute": address, "routeTableRef": routeTable, "nextHopNICRef": eni}
	return res
}

func unassignRouteTableRoute(ctx context.Context, spec executeActionRequestSpec, mode string, runner awsRunner) executeActionResult {
	routeTable, _, address, region, err := requireRouteTarget(spec)
	if err != nil {
		return failed("unassign-route-table-route: missing target field", err)
	}
	res := newResult()

	if mode == modeDryRun {
		if _, derr := runner(ctx, "ec2", "describe-route-tables", "--route-table-ids", routeTable, "--region", region); derr != nil {
			return failed("unassign-route-table-route dry-run: describe route table failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would delete route %s from %s", address, routeTable)
		return res
	}

	if _, err := runner(ctx, "ec2", "delete-route",
		"--route-table-id", routeTable,
		"--destination-cidr-block", address,
		"--region", region); err != nil {
		if isNotFoundError(err) {
			res.Status.Status = statusSkipped
			res.Status.Message = fmt.Sprintf("route %s already absent from %s", address, routeTable)
			return res
		}
		return failed("unassign-route-table-route execute: delete route failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("deleted route %s from %s", address, routeTable)
	return res
}

// assignSecondaryIP attaches the captured /32 to the ENI.
//   - dry-run: describe the ENI, report current secondary IPs, "would assign".
//   - execute: assign-private-ip-addresses.
func assignSecondaryIP(ctx context.Context, spec executeActionRequestSpec, mode string, runner awsRunner) executeActionResult {
	eni, address, region, err := requireTarget(spec, true)
	if err != nil {
		return failed("assign-secondary-ip: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true
	allowReassignment := stringBool(spec.Parameters["allowReassignment"])

	if mode == modeDryRun {
		iface, derr := describeInterface(ctx, runner, eni, region)
		if derr != nil {
			return failed("assign-secondary-ip dry-run: describe failed", derr)
		}
		res.Status.Status = statusSucceeded
		if allowReassignment {
			res.Status.Message = fmt.Sprintf("would seize/reassign %s to %s", address, eni)
		} else {
			res.Status.Message = fmt.Sprintf("would assign %s to %s", address, eni)
		}
		res.Status.Observed = map[string]string{"currentSecondaryIps": iface.secondaryIPsCSV()}
		return res
	}

	args := []string{"ec2", "assign-private-ip-addresses",
		"--network-interface-id", eni,
		"--private-ip-addresses", address,
		"--region", region}
	if allowReassignment {
		args = append(args, "--allow-reassignment")
	}
	if _, err := runner(ctx, args...); err != nil {
		return failed("assign-secondary-ip execute: assign failed", err)
	}
	res.Status.Status = statusSucceeded
	if allowReassignment {
		res.Status.Message = fmt.Sprintf("seized/reassigned %s to %s", address, eni)
	} else {
		res.Status.Message = fmt.Sprintf("assigned %s to %s", address, eni)
	}
	res.Status.Observed = map[string]string{"assignedAddress": address}
	return res
}

// ensureForwardingEnabled disables the ENI source/dest check.
//   - dry-run: describe current SourceDestCheck, "would set SourceDestCheck=false".
//   - execute: FIRST describe to capture prior SourceDestCheck into Observed,
//     THEN modify --no-source-dest-check. The captured prior value is what the
//     undo (ensure-forwarding-disabled) reads back to restore exactly prior state.
func ensureForwardingEnabled(ctx context.Context, spec executeActionRequestSpec, mode string, runner awsRunner) executeActionResult {
	eni, _, region, err := requireTarget(spec, false)
	if err != nil {
		return failed("ensure-forwarding-enabled: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true

	iface, derr := describeInterface(ctx, runner, eni, region)
	if derr != nil {
		return failed("ensure-forwarding-enabled: describe (capture prior) failed", derr)
	}
	prior := boolStr(iface.SourceDestCheck)
	res.Status.Observed = map[string]string{"priorSourceDestCheck": prior}

	if mode == modeDryRun {
		res.Status.Status = statusSucceeded
		res.Status.Message = "would set SourceDestCheck=false"
		return res
	}

	if _, err := runner(ctx, "ec2", "modify-network-interface-attribute",
		"--network-interface-id", eni,
		"--no-source-dest-check",
		"--region", region); err != nil {
		return failed("ensure-forwarding-enabled execute: modify failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("disabled SourceDestCheck on %s (prior=%s)", eni, prior)
	return res
}

// unassignSecondaryIP is the undo of assign-secondary-ip.
func unassignSecondaryIP(ctx context.Context, spec executeActionRequestSpec, mode string, runner awsRunner) executeActionResult {
	eni, address, region, err := requireTarget(spec, true)
	if err != nil {
		return failed("unassign-secondary-ip: missing target field", err)
	}
	res := newResult()

	if mode == modeDryRun {
		// Read-only preview: confirm the ENI is describable.
		if _, derr := describeInterface(ctx, runner, eni, region); derr != nil {
			return failed("unassign-secondary-ip dry-run: describe failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would unassign %s from %s", address, eni)
		return res
	}

	if _, err := runner(ctx, "ec2", "unassign-private-ip-addresses",
		"--network-interface-id", eni,
		"--private-ip-addresses", address,
		"--region", region); err != nil {
		return failed("unassign-secondary-ip execute: unassign failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("unassigned %s from %s", address, eni)
	return res
}

// ensureForwardingDisabled is the undo of ensure-forwarding-enabled. It applies
// the RESTORE-PRIOR rule using Parameters["priorSourceDestCheck"], which the
// engine.Rollback injects from the journal's recorded Observed:
//   - "true"  -> the check was ON before we touched it -> re-enable it.
//   - "false" -> the check was ALREADY disabled (the ENI was already a forwarder)
//     -> NO-OP, status=skipped. We do NOT force the check back on.
//
// It NEVER hardcodes undo=re-enable: a blind re-enable would break an ENI that
// was already a forwarder for its own reasons.
func ensureForwardingDisabled(ctx context.Context, spec executeActionRequestSpec, mode string, runner awsRunner) executeActionResult {
	eni, _, region, err := requireTarget(spec, false)
	if err != nil {
		return failed("ensure-forwarding-disabled: missing target field", err)
	}
	res := newResult()
	prior := spec.Parameters["priorSourceDestCheck"]
	res.Status.Observed = map[string]string{"priorSourceDestCheck": prior}

	switch prior {
	case "false":
		// Prior was already false: nothing to restore.
		res.Status.Status = statusSkipped
		res.Status.Message = "prior SourceDestCheck was already false; nothing to restore"
		return res
	case "true":
		// fall through to re-enable
	default:
		return failed("ensure-forwarding-disabled: missing/invalid priorSourceDestCheck parameter (want true|false)", nil)
	}

	if mode == modeDryRun {
		res.Status.Status = statusSucceeded
		res.Status.Message = "would re-enable SourceDestCheck (restore prior=true)"
		return res
	}

	if _, err := runner(ctx, "ec2", "modify-network-interface-attribute",
		"--network-interface-id", eni,
		"--source-dest-check",
		"--region", region); err != nil {
		return failed("ensure-forwarding-disabled execute: modify failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("re-enabled SourceDestCheck on %s (restored prior=true)", eni)
	return res
}

// boolStr renders a Go bool as the canonical "true"/"false" the journal stores.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func stringBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "notfound") ||
		strings.Contains(msg, "could not be found") ||
		strings.Contains(msg, "invalidroutenotfound")
}

// commandTimeout is the per-aws-invocation timeout.
func commandTimeout() time.Duration {
	return defaultAWSCommandTimeoutMs * time.Millisecond
}
