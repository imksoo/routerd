// SPDX-License-Identifier: BSD-3-Clause

// Command oci-provider-executor is a REAL OCI routerd executor plugin (ADR 0007,
// Phase 5.1) advertising the capability execute.providerAction. It performs the
// CloudEdge Selective Address Mobility VNIC mutations — assigning a captured /32
// to the cloud VNIC and allowing it to forward (skipSourceDestCheck=true) —
// through the gated, journaled execution path instead of by hand.
//
// REAL EXECUTOR — it mutates OCI, but ONLY in execute mode. In dry-run mode it
// issues read-only get/list calls and mutates nothing (enforced: see
// guardedRunner). It drives the OCI CLI (`oci`) via an injectable command runner
// (the ociRunner func var, default execRunner running the real `oci` binary;
// tests inject a fake), so unit tests NEVER call real OCI.
//
// CREDENTIALS: it authenticates with the OCI instance principal that the OCI CLI
// resolves on its own. routerd core passes it NO credentials and inherits NO
// parent environment to it (see RunExecutor); the executor reads no OCI
// credentials from the request. It imports no OCI SDK — the ONLY external
// dependency is exec of the `oci` CLI binary.
//
// Reads from the request Target: nicRef (VNIC OCID), address (the captured /32),
// region (and compartmentId/region when available). A missing required field is
// a clear failed result.
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

	actionAssignSecondaryIP       = "assign-secondary-ip"
	actionUnassignSecondaryIP     = "unassign-secondary-ip"
	actionAssignRouteTableRoute   = "assign-route-table-route"
	actionUnassignRouteTableRoute = "unassign-route-table-route"
	actionEnsureFwdEnabled        = "ensure-forwarding-enabled"
	actionEnsureFwdDisabled       = "ensure-forwarding-disabled"

	captureStrategyRouteTable = "route-table"
	routeRuleDescription      = "routerd CloudEdge SAM mobility"

	defaultOCICommandTimeoutMs = 25000
)

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout, defaultRunner()); err != nil {
		fmt.Fprintf(os.Stderr, "oci-provider-executor: %v\n", err)
		os.Exit(1)
	}
}

// run reads one ExecuteActionRequest, dispatches it, and writes one
// ExecuteActionResult. runner is the injectable oci command runner.
func run(ctx context.Context, in io.Reader, out io.Writer, runner ociRunner) error {
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
		if isAuthorizationError(err) {
			res.Status.Observed = map[string]string{
				"failureClass":   "authorization",
				"permissionHint": ociPermissionHint(msg),
			}
		}
	}
	return res
}

func isAuthorizationError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if isToolchainExecutionError(msg) {
		return false
	}
	return strings.Contains(msg, "notauthorizedornotfound") ||
		strings.Contains(msg, "not authorized") ||
		strings.Contains(msg, "authorization") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "status code: 401") ||
		strings.Contains(msg, "status code: 403") ||
		strings.Contains(msg, "http 401") ||
		strings.Contains(msg, "http 403")
}

func isToolchainExecutionError(msg string) bool {
	return strings.Contains(msg, "fork/exec") ||
		strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "permission denied")
}

func ociPermissionHint(msg string) string {
	msg = strings.ToLower(msg)
	switch {
	case strings.Contains(msg, "assign-secondary-ip") && strings.Contains(msg, "vnic get"):
		return "use vnics"
	case strings.Contains(msg, "assign-secondary-ip"):
		return "manage private-ips"
	case strings.Contains(msg, "unassign-secondary-ip"):
		return "manage private-ips"
	case strings.Contains(msg, "ensure-forwarding") && strings.Contains(msg, "vnic get"):
		return "use vnics"
	case strings.Contains(msg, "ensure-forwarding"):
		return "use vnics"
	case strings.Contains(msg, "route-table") && strings.Contains(msg, "private-ip lookup"):
		return "use private-ips"
	case strings.Contains(msg, "route-table"):
		return "manage route-tables"
	default:
		return "oci-policy"
	}
}

// requireTarget extracts the required VNIC/address from the request, erroring if
// any required field is missing. address is required for the IP actions; for the
// forwarding actions only nicRef is required. region is optional (the OCI CLI
// resolves it from instance metadata when absent).
func requireTarget(spec executeActionRequestSpec, needAddress bool) (vnic, address string, err error) {
	vnic = spec.Target["nicRef"]
	address = spec.Target["address"]
	if vnic == "" {
		return "", "", fmt.Errorf("target.nicRef (VNIC OCID) is required")
	}
	if needAddress && address == "" {
		return "", "", fmt.Errorf("target.address is required")
	}
	if address != "" {
		address = bareIP(address)
	}
	return vnic, address, nil
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
// paths use only get/list verbs through the guardedRunner, and the guard rejects
// any non-read-only verb so a coding mistake cannot mutate during a preview.
func dispatch(ctx context.Context, req executeActionRequest, runner ociRunner) executeActionResult {
	spec := req.Spec
	mode := spec.Mode
	if mode != modeDryRun && mode != modeExecute {
		return failed(fmt.Sprintf("invalid mode %q (want dry-run or execute)", mode), nil)
	}
	if mode == modeDryRun {
		// Dry-run hard guard: only get/list verbs may be issued.
		runner = guardedRunner(runner)
	}

	switch spec.Action {
	case actionAssignSecondaryIP:
		if captureStrategy(spec) == captureStrategyRouteTable {
			return assignRouteTableRoute(ctx, spec, mode, runner)
		}
		return assignSecondaryIP(ctx, spec, mode, runner)
	case actionAssignRouteTableRoute:
		return assignRouteTableRoute(ctx, spec, mode, runner)
	case actionEnsureFwdEnabled:
		return ensureForwardingEnabled(ctx, spec, mode, runner)
	case actionUnassignSecondaryIP:
		if captureStrategy(spec) == captureStrategyRouteTable {
			return unassignRouteTableRoute(ctx, spec, mode, runner)
		}
		return unassignSecondaryIP(ctx, spec, mode, runner)
	case actionUnassignRouteTableRoute:
		return unassignRouteTableRoute(ctx, spec, mode, runner)
	case actionEnsureFwdDisabled:
		return ensureForwardingDisabled(ctx, spec, mode, runner)
	default:
		return failed(fmt.Sprintf("unsupported action %q", spec.Action), nil)
	}
}

// captureStrategy returns the capture strategy the planner selected for this
// action. An empty value means the default secondary-ip strategy; "route-table"
// routes the action to the VCN route-rule path instead.
func captureStrategy(spec executeActionRequestSpec) string {
	return strings.TrimSpace(spec.Target["captureStrategy"])
}

// requireRouteTarget extracts the fields the route-table path needs. OCI route
// rules point at a Private IP (the router's next hop), so unlike the secondary-ip
// path this also requires nextHopIPAddress: the router VNIC private IP whose OCID
// becomes the rule's networkEntityId. nicRef is the router VNIC the next hop
// belongs to. routeTableRef is the VCN route table OCID to write into.
func requireRouteTarget(spec executeActionRequestSpec) (rtID, vnic, address, nextHop string, err error) {
	rtID = strings.TrimSpace(spec.Target["routeTableRef"])
	vnic = strings.TrimSpace(spec.Target["nicRef"])
	address = bareIP(spec.Target["address"])
	nextHop = bareIP(spec.Target["nextHopIPAddress"])
	if rtID == "" {
		return "", "", "", "", fmt.Errorf("target.routeTableRef (route table OCID) is required")
	}
	if vnic == "" {
		return "", "", "", "", fmt.Errorf("target.nicRef (VNIC OCID) is required")
	}
	if address == "" {
		return "", "", "", "", fmt.Errorf("target.address is required")
	}
	if nextHop == "" {
		return "", "", "", "", fmt.Errorf("target.nextHopIPAddress is required for oci route-table")
	}
	return rtID, vnic, address, nextHop, nil
}

// assignRouteTableRoute steers the captured /32 to the router by adding a VCN
// route rule whose next hop is the router's private IP (networkEntityId). OCI has
// no per-rule add, so this reads the full rule set, merges in one rule, and writes
// the set back.
//   - dry-run: read-only `route-table get`, report "would route".
//   - execute: resolve the next-hop private-ip OCID, read rules, append (or
//     replace on seize) the one rule for our /32, and `route-table update`.
func assignRouteTableRoute(ctx context.Context, spec executeActionRequestSpec, mode string, runner ociRunner) executeActionResult {
	rtID, vnic, address, nextHop, err := requireRouteTarget(spec)
	if err != nil {
		return failed("assign-route-table-route: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true
	dest := address + "/32"

	if mode == modeDryRun {
		if _, derr := getRouteTableRules(ctx, runner, rtID); derr != nil {
			return failed("assign-route-table-route dry-run: route-table get failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would route %s via %s in %s", dest, nextHop, rtID)
		return res
	}

	nextHopOCID, derr := findPrivateIPOCID(ctx, runner, vnic, nextHop)
	if derr != nil {
		return failed("assign-route-table-route execute: next-hop private-ip lookup failed", derr)
	}
	rules, derr := getRouteTableRules(ctx, runner, rtID)
	if derr != nil {
		return failed("assign-route-table-route execute: route-table get failed", derr)
	}
	allowReassignment := stringBool(spec.Parameters["allowReassignment"])
	newRule := ociRouteRule{
		Destination:     dest,
		DestinationType: "CIDR_BLOCK",
		NetworkEntityID: nextHopOCID,
		Description:     routeRuleDescription,
	}
	if idx := routeRuleIndexForDest(rules, dest); idx >= 0 {
		if rules[idx].NetworkEntityID == nextHopOCID {
			res.Status.Status = statusSucceeded
			res.Status.Message = fmt.Sprintf("%s already routes to %s in %s", dest, nextHopOCID, rtID)
			res.Status.Observed = map[string]string{"assignedRoute": dest, "routeTableRef": rtID, "nextHopPrivateIP": nextHopOCID}
			return res
		}
		if !allowReassignment {
			return failed("assign-route-table-route execute: route exists for a different next hop (set allowReassignment to seize)", nil)
		}
		rules[idx] = newRule
	} else {
		rules = append(rules, newRule)
	}
	if err := updateRouteTableRules(ctx, runner, rtID, rules); err != nil {
		return failed("assign-route-table-route execute: route-table update failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("routed %s to %s in %s", dest, nextHopOCID, rtID)
	res.Status.Observed = map[string]string{"assignedRoute": dest, "routeTableRef": rtID, "nextHopPrivateIP": nextHopOCID}
	return res
}

// unassignRouteTableRoute is the undo of assignRouteTableRoute. It removes the
// /32 route rule, but ONLY when it still points at our own next-hop private IP, so
// a route a different holder has already seized is never deleted (foreign-holder
// protection). A missing rule is idempotently skipped.
func unassignRouteTableRoute(ctx context.Context, spec executeActionRequestSpec, mode string, runner ociRunner) executeActionResult {
	rtID, vnic, address, nextHop, err := requireRouteTarget(spec)
	if err != nil {
		return failed("unassign-route-table-route: missing target field", err)
	}
	res := newResult()
	dest := address + "/32"

	if mode == modeDryRun {
		if _, derr := getRouteTableRules(ctx, runner, rtID); derr != nil {
			return failed("unassign-route-table-route dry-run: route-table get failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would remove %s from %s", dest, rtID)
		return res
	}

	nextHopOCID, derr := findPrivateIPOCID(ctx, runner, vnic, nextHop)
	if derr != nil {
		return failed("unassign-route-table-route execute: next-hop private-ip lookup failed", derr)
	}
	rules, derr := getRouteTableRules(ctx, runner, rtID)
	if derr != nil {
		return failed("unassign-route-table-route execute: route-table get failed", derr)
	}
	idx := routeRuleIndexForDest(rules, dest)
	if idx < 0 {
		res.Status.Status = statusSkipped
		res.Status.Message = fmt.Sprintf("no route for %s in %s; nothing to remove", dest, rtID)
		return res
	}
	if rules[idx].NetworkEntityID != nextHopOCID {
		res.Status.Status = statusSkipped
		res.Status.Message = fmt.Sprintf("route for %s targets a different next hop (foreign holder); not removing", dest)
		return res
	}
	rules = append(rules[:idx], rules[idx+1:]...)
	if err := updateRouteTableRules(ctx, runner, rtID, rules); err != nil {
		return failed("unassign-route-table-route execute: route-table update failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("removed %s from %s (was %s)", dest, rtID, nextHopOCID)
	return res
}

// assignSecondaryIP attaches the captured /32 to the VNIC.
//   - dry-run: get the VNIC (read-only), report "would assign".
//   - execute: network private-ip create on the VNIC, or vnic assign-private-ip
//     with force reassignment when allowReassignment is set.
func assignSecondaryIP(ctx context.Context, spec executeActionRequestSpec, mode string, runner ociRunner) executeActionResult {
	vnic, address, err := requireTarget(spec, true)
	if err != nil {
		return failed("assign-secondary-ip: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true
	allowReassignment := stringBool(spec.Parameters["allowReassignment"])

	if mode == modeDryRun {
		if _, derr := getVNIC(ctx, runner, vnic); derr != nil {
			return failed("assign-secondary-ip dry-run: vnic get failed", derr)
		}
		res.Status.Status = statusSucceeded
		if allowReassignment {
			res.Status.Message = fmt.Sprintf("would seize/reassign %s to %s", address, vnic)
		} else {
			res.Status.Message = fmt.Sprintf("would assign %s to %s", address, vnic)
		}
		return res
	}

	if allowReassignment {
		ocid, derr := findPrivateIPOCID(ctx, runner, vnic, address)
		if derr == nil {
			res.Status.Status = statusSucceeded
			res.Status.Message = fmt.Sprintf("seized/reassigned %s to %s (already present as private-ip %s)", address, vnic, ocid)
			res.Status.Observed = map[string]string{"assignedAddress": address, "privateIPOCID": ocid, "seizeAlreadyPresent": "true"}
			return res
		}
		if !isPrivateIPNotFoundError(derr) {
			return failed("assign-secondary-ip execute: private-ip lookup failed", derr)
		}
	}

	args := []string{"network", "private-ip", "create",
		"--vnic-id", vnic,
		"--ip-address", address}
	if allowReassignment {
		args = []string{"network", "vnic", "assign-private-ip",
			"--vnic-id", vnic,
			"--ip-address", address,
			"--unassign-if-already-assigned"}
	}
	if _, err := runner(ctx, args...); err != nil {
		if isAlreadyAssignedError(err) {
			if ocid, derr := findPrivateIPOCID(ctx, runner, vnic, address); derr == nil {
				res.Status.Status = statusSucceeded
				res.Status.Message = fmt.Sprintf("%s already present on %s (private-ip %s)", address, vnic, ocid)
				res.Status.Observed = map[string]string{"assignedAddress": address, "privateIPOCID": ocid, "seizeAlreadyPresent": "true"}
				return res
			}
		}
		return failed("assign-secondary-ip execute: assign failed", err)
	}
	res.Status.Status = statusSucceeded
	if allowReassignment {
		res.Status.Message = fmt.Sprintf("seized/reassigned %s to %s", address, vnic)
	} else {
		res.Status.Message = fmt.Sprintf("assigned %s to %s", address, vnic)
	}
	res.Status.Observed = map[string]string{"assignedAddress": address}
	return res
}

// ensureForwardingEnabled allows the VNIC to forward by setting
// skipSourceDestCheck=true (OCI semantics: skipSourceDestCheck=true MEANS
// forwarding is allowed).
//   - dry-run: get current skipSourceDestCheck, "would set skipSourceDestCheck=true".
//   - execute: FIRST get to capture prior skipSourceDestCheck into
//     Observed{priorSkipSourceDestCheck}, THEN vnic update --skip-source-dest-check true.
func ensureForwardingEnabled(ctx context.Context, spec executeActionRequestSpec, mode string, runner ociRunner) executeActionResult {
	vnic, _, err := requireTarget(spec, false)
	if err != nil {
		return failed("ensure-forwarding-enabled: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true

	v, derr := getVNIC(ctx, runner, vnic)
	if derr != nil {
		return failed("ensure-forwarding-enabled: vnic get (capture prior) failed", derr)
	}
	prior := boolStr(v.SkipSourceDestCheck)
	res.Status.Observed = map[string]string{"priorSkipSourceDestCheck": prior}

	if mode == modeDryRun {
		res.Status.Status = statusSucceeded
		res.Status.Message = "would set skipSourceDestCheck=true"
		return res
	}

	if _, err := runner(ctx, "network", "vnic", "update",
		"--vnic-id", vnic,
		"--skip-source-dest-check", "true"); err != nil {
		return failed("ensure-forwarding-enabled execute: vnic update failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("set skipSourceDestCheck=true on %s (prior=%s)", vnic, prior)
	return res
}

// unassignSecondaryIP is the undo of assign-secondary-ip. It first finds the
// private-ip OCID for the address on the VNIC (read-only list), then deletes it.
func unassignSecondaryIP(ctx context.Context, spec executeActionRequestSpec, mode string, runner ociRunner) executeActionResult {
	vnic, address, err := requireTarget(spec, true)
	if err != nil {
		return failed("unassign-secondary-ip: missing target field", err)
	}
	res := newResult()

	if mode == modeDryRun {
		// Read-only preview: confirm the VNIC is gettable.
		if _, derr := getVNIC(ctx, runner, vnic); derr != nil {
			return failed("unassign-secondary-ip dry-run: vnic get failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would unassign %s from %s", address, vnic)
		return res
	}

	// Resolve the private-ip OCID for this address on the VNIC (read-only list).
	ocid, derr := findPrivateIPOCID(ctx, runner, vnic, address)
	if derr != nil {
		if isPrivateIPNotFoundError(derr) {
			res.Status.Status = statusSkipped
			res.Status.Message = fmt.Sprintf("%s is not present on %s; nothing to unassign", address, vnic)
			return res
		}
		return failed("unassign-secondary-ip execute: private-ip lookup failed", derr)
	}
	if _, err := runner(ctx, "network", "private-ip", "delete",
		"--private-ip-id", ocid,
		"--force"); err != nil {
		if isNotFoundError(err) {
			res.Status.Status = statusSkipped
			res.Status.Message = fmt.Sprintf("private-ip %s for %s is already absent; nothing to unassign", ocid, address)
			return res
		}
		return failed("unassign-secondary-ip execute: private-ip delete failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("unassigned %s from %s (private-ip %s)", address, vnic, ocid)
	return res
}

// ensureForwardingDisabled is the undo of ensure-forwarding-enabled. It applies
// the RESTORE-PRIOR rule using Parameters["priorSkipSourceDestCheck"], which the
// engine.Rollback injects from the journal's recorded Observed.
//
// OCI semantics: skipSourceDestCheck=true MEANS forwarding-allowed.
// ensure-forwarding-enabled sets it true; restore-prior reverts to the captured
// value:
//   - "true"  -> the VNIC was ALREADY skipping (already a forwarder) before we
//     touched it -> NO-OP, status=skipped. We do NOT force it.
//   - "false" -> it was NOT skipping before -> restore by setting it false.
//
// It NEVER blind-forces: a blind set would clobber a VNIC that was already a
// forwarder for its own reasons.
func ensureForwardingDisabled(ctx context.Context, spec executeActionRequestSpec, mode string, runner ociRunner) executeActionResult {
	vnic, _, err := requireTarget(spec, false)
	if err != nil {
		return failed("ensure-forwarding-disabled: missing target field", err)
	}
	res := newResult()
	prior := spec.Parameters["priorSkipSourceDestCheck"]
	res.Status.Observed = map[string]string{"priorSkipSourceDestCheck": prior}

	switch prior {
	case "true":
		// Prior was already true (already a forwarder): nothing to restore.
		res.Status.Status = statusSkipped
		res.Status.Message = "prior skipSourceDestCheck was already true; nothing to restore"
		return res
	case "false":
		// fall through to restore (set skipSourceDestCheck false)
	default:
		return failed("ensure-forwarding-disabled: missing/invalid priorSkipSourceDestCheck parameter (want true|false)", nil)
	}

	if mode == modeDryRun {
		res.Status.Status = statusSucceeded
		res.Status.Message = "would set skipSourceDestCheck=false (restore prior=false)"
		return res
	}

	if _, err := runner(ctx, "network", "vnic", "update",
		"--vnic-id", vnic,
		"--skip-source-dest-check", "false"); err != nil {
		return failed("ensure-forwarding-disabled execute: vnic update failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("set skipSourceDestCheck=false on %s (restored prior=false)", vnic)
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
		strings.Contains(msg, "404")
}

func isAlreadyAssignedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already assigned") ||
		strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "already in use") ||
		strings.Contains(msg, "in use") ||
		strings.Contains(msg, "conflict") ||
		strings.Contains(msg, "409")
}

// commandTimeout is the per-oci-invocation timeout.
func commandTimeout() time.Duration {
	return defaultOCICommandTimeoutMs * time.Millisecond
}
