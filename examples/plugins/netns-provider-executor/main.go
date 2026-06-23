// SPDX-License-Identifier: BSD-3-Clause

// Command netns-provider-executor is a local integration-test provider executor.
// It implements the same execute.providerAction JSON contract as the real cloud
// executors, but mutates only the current Linux network namespace.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

type typeMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

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
	Context        json.RawMessage   `json:"context,omitempty"`
}

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

	actionAssignSecondaryIP    = "assign-secondary-ip"
	actionUnassignSecondaryIP  = "unassign-secondary-ip"
	actionEnsureFwdEnabled     = "ensure-forwarding-enabled"
	actionEnsureFwdDisabled    = "ensure-forwarding-disabled"
	defaultCommandTimeout      = 10 * time.Second
	captureStrategySecondaryIP = "secondary-ip"
)

type runner func(context.Context, string, ...string) (string, error)

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout, execRunner); err != nil {
		fmt.Fprintf(os.Stderr, "netns-provider-executor: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, in io.Reader, out io.Writer, runCmd runner) error {
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
	res := dispatch(ctx, req.Spec, runCmd)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func dispatch(ctx context.Context, spec executeActionRequestSpec, runCmd runner) executeActionResult {
	if strings.TrimSpace(spec.Provider) != "netns" {
		return failed(fmt.Sprintf("unsupported provider %q", spec.Provider), nil)
	}
	switch spec.Mode {
	case modeDryRun, modeExecute:
	default:
		return failed(fmt.Sprintf("invalid mode %q", spec.Mode), nil)
	}
	switch spec.Action {
	case actionAssignSecondaryIP:
		return assignSecondary(ctx, spec, runCmd)
	case actionUnassignSecondaryIP:
		return unassignSecondary(ctx, spec, runCmd)
	case actionEnsureFwdEnabled:
		return ensureForwarding(ctx, spec, true, runCmd)
	case actionEnsureFwdDisabled:
		return ensureForwarding(ctx, spec, false, runCmd)
	default:
		return failed(fmt.Sprintf("unsupported action %q", spec.Action), nil)
	}
}

func assignSecondary(ctx context.Context, spec executeActionRequestSpec, runCmd runner) executeActionResult {
	iface, address, err := requireInterfaceAddress(spec)
	if err != nil {
		return failed("assign-secondary-ip: missing target field", err)
	}
	if strategy := strings.TrimSpace(spec.Target["captureStrategy"]); strategy != "" && strategy != captureStrategySecondaryIP {
		return failed(fmt.Sprintf("captureStrategy %q is not supported by netns secondary executor", strategy), nil)
	}
	res := succeeded(fmt.Sprintf("would add %s to %s", address, iface), true)
	if spec.Mode == modeDryRun {
		return res
	}
	if _, err := runCmd(ctx, "ip", "addr", "replace", address, "dev", iface); err != nil {
		return failed("assign-secondary-ip execute: ip addr replace failed", err)
	}
	res.Status.Message = fmt.Sprintf("added %s to %s", address, iface)
	res.Status.Observed = map[string]string{"assignedAddress": address, "interface": iface}
	return res
}

func unassignSecondary(ctx context.Context, spec executeActionRequestSpec, runCmd runner) executeActionResult {
	iface, address, err := requireInterfaceAddress(spec)
	if err != nil {
		return failed("unassign-secondary-ip: missing target field", err)
	}
	res := succeeded(fmt.Sprintf("would delete %s from %s", address, iface), false)
	if spec.Mode == modeDryRun {
		return res
	}
	if _, err := runCmd(ctx, "ip", "addr", "del", address, "dev", iface); err != nil {
		if strings.Contains(err.Error(), "Cannot assign requested address") {
			res.Status.Message = fmt.Sprintf("%s was already absent from %s", address, iface)
			return res
		}
		return failed("unassign-secondary-ip execute: ip addr del failed", err)
	}
	res.Status.Message = fmt.Sprintf("deleted %s from %s", address, iface)
	res.Status.Observed = map[string]string{"removedAddress": address, "interface": iface}
	return res
}

func ensureForwarding(ctx context.Context, spec executeActionRequestSpec, enabled bool, runCmd runner) executeActionResult {
	want := "0"
	if enabled {
		want = "1"
	}
	res := succeeded("would set net.ipv4.ip_forward="+want, false)
	if spec.Mode == modeDryRun {
		return res
	}
	if _, err := runCmd(ctx, "sysctl", "-w", "net.ipv4.ip_forward="+want); err != nil {
		return failed("ensure-forwarding execute: sysctl failed", err)
	}
	res.Status.Message = "set net.ipv4.ip_forward=" + want
	return res
}

func requireInterfaceAddress(spec executeActionRequestSpec) (string, string, error) {
	iface := strings.TrimSpace(firstNonEmpty(spec.Target["interface"], spec.Target["nicRef"]))
	if iface == "" {
		return "", "", fmt.Errorf("target.interface or target.nicRef is required")
	}
	address := canonicalAddress(spec.Target["address"])
	if address == "" {
		return "", "", fmt.Errorf("target.address is required")
	}
	return iface, address, nil
}

func canonicalAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if ip, ipnet, err := net.ParseCIDR(raw); err == nil {
		ones, bits := ipnet.Mask.Size()
		if bits == 32 && ones == 32 {
			return ip.String() + "/32"
		}
		return ip.String() + fmt.Sprintf("/%d", ones)
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String() + "/32"
	}
	return raw
}

func succeeded(message string, undo bool) executeActionResult {
	return executeActionResult{
		typeMeta: typeMeta{APIVersion: resultAPIVersion, Kind: resultKind},
		Status: executeActionResultStatus{
			Status:        statusSucceeded,
			Message:       message,
			UndoAvailable: undo,
		},
	}
}

func failed(message string, err error) executeActionResult {
	res := executeActionResult{typeMeta: typeMeta{APIVersion: resultAPIVersion, Kind: resultKind}}
	res.Status.Status = statusFailed
	res.Status.Message = message
	if err != nil {
		res.Status.Error = err.Error()
	}
	return res
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func execRunner(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if err != nil {
		return output.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, output.String())
	}
	return output.String(), nil
}
