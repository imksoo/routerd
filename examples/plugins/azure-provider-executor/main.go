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
	"strconv"
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
	captureStrategyRouteTable     = "route-table"
	networkAPIVersion             = "2023-09-01"
	defaultAzCommandTimeout       = 60 * time.Second
	azCommandTimeoutEnv           = "ROUTERD_AZURE_EXECUTOR_COMMAND_TIMEOUT"
	legacyAzCommandTimeoutMsEnv   = "ROUTERD_AZURE_EXECUTOR_COMMAND_TIMEOUT_MS"
	azCommandRetryAttempts        = 3
	azCommandRetryDelay           = 250 * time.Millisecond
	seizeVerifyAttempts           = 5
	seizeVerifyDelay              = 500 * time.Millisecond
)

var sleep = time.Sleep

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
		if isAuthorizationError(err) {
			res.Status.Observed = map[string]string{
				"failureClass":   "authorization",
				"permissionHint": azurePermissionHint(msg),
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
	return strings.Contains(msg, "authorizationfailed") ||
		strings.Contains(msg, "access denied") ||
		strings.Contains(msg, "not authorized") ||
		strings.Contains(msg, "does not have authorization") ||
		strings.Contains(msg, "forbidden")
}

func isToolchainExecutionError(msg string) bool {
	return strings.Contains(msg, "fork/exec") ||
		strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "no such file or directory") ||
		strings.Contains(msg, "permission denied")
}

func azurePermissionHint(msg string) string {
	msg = strings.ToLower(msg)
	switch {
	case strings.Contains(msg, "route-table") && strings.Contains(msg, "show"):
		return "Microsoft.Network/routeTables/routes/read"
	case strings.Contains(msg, "assign-route-table-route"):
		return "Microsoft.Network/routeTables/routes/write"
	case strings.Contains(msg, "unassign-route-table-route"):
		return "Microsoft.Network/routeTables/routes/delete"
	case strings.Contains(msg, "assign-secondary-ip") && strings.Contains(msg, "show"):
		return "Microsoft.Network/networkInterfaces/read"
	case strings.Contains(msg, "assign-secondary-ip"):
		return "Microsoft.Network/networkInterfaces/write"
	case strings.Contains(msg, "unassign-secondary-ip") && strings.Contains(msg, "show"):
		return "Microsoft.Network/networkInterfaces/read"
	case strings.Contains(msg, "unassign-secondary-ip"):
		return "Microsoft.Network/networkInterfaces/write"
	case strings.Contains(msg, "ensure-forwarding") && strings.Contains(msg, "show"):
		return "Microsoft.Network/networkInterfaces/read"
	case strings.Contains(msg, "ensure-forwarding"):
		return "Microsoft.Network/networkInterfaces/write"
	default:
		return "azure-rbac"
	}
}

// nicTarget bundles the Azure NIC identification read from the request Target.
type nicTarget struct {
	nicID         string // NIC resource id (for --ids on show/update)
	resourceGroup string
	nicName       string // for ip-config create/delete (--nic-name)
	ipConfigName  string
	address       string
	displaced     displacedTarget
}

type displacedTarget struct {
	nicID         string
	resourceGroup string
	nicName       string
	ipConfigName  string
}

type routeTarget struct {
	resourceGroup    string
	routeTableName   string
	routeName        string
	address          string
	nextHopIPAddress string
}

func (t displacedTarget) complete() bool {
	return strings.TrimSpace(t.resourceGroup) != "" && strings.TrimSpace(t.nicName) != "" && strings.TrimSpace(t.ipConfigName) != ""
}

// requireNICID requires the NIC resource id (used for show/update via --ids).
func requireNICID(spec executeActionRequestSpec) (nicTarget, error) {
	t := nicTarget{
		nicID:         spec.Target["nicRef"],
		resourceGroup: spec.Target["resourceGroup"],
		nicName:       spec.Target["nicName"],
		ipConfigName:  spec.Target["ipConfigName"],
		address:       bareIP(spec.Target["address"]),
		displaced: displacedTarget{
			nicID:         spec.Target["displacedNicRef"],
			resourceGroup: spec.Target["displacedResourceGroup"],
			nicName:       spec.Target["displacedNicName"],
			ipConfigName:  spec.Target["displacedIpConfigName"],
		},
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func requireRouteTarget(spec executeActionRequestSpec) (routeTarget, error) {
	t := routeTarget{
		resourceGroup:    spec.Target["resourceGroup"],
		routeTableName:   firstNonEmpty(spec.Target["routeTableName"], spec.Target["routeTableRef"]),
		routeName:        spec.Target["routeName"],
		address:          spec.Target["address"],
		nextHopIPAddress: spec.Target["nextHopIPAddress"],
	}
	if t.resourceGroup == "" {
		return routeTarget{}, fmt.Errorf("target.resourceGroup is required for route-table operations")
	}
	if t.routeTableName == "" {
		return routeTarget{}, fmt.Errorf("target.routeTableName or target.routeTableRef is required")
	}
	if t.routeName == "" {
		return routeTarget{}, fmt.Errorf("target.routeName is required")
	}
	if t.address == "" {
		return routeTarget{}, fmt.Errorf("target.address is required")
	}
	if t.nextHopIPAddress == "" {
		return routeTarget{}, fmt.Errorf("target.nextHopIPAddress is required")
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

func captureStrategy(spec executeActionRequestSpec) string {
	return strings.TrimSpace(spec.Target["captureStrategy"])
}

func assignRouteTableRoute(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireRouteTarget(spec)
	if err != nil {
		return failed("assign-route-table-route: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true

	if mode == modeDryRun {
		if _, derr := runner(ctx, "network", "route-table", "show", "--resource-group", t.resourceGroup, "--name", t.routeTableName); derr != nil {
			return failed("assign-route-table-route dry-run: route table show failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would route %s to %s in %s", t.address, t.nextHopIPAddress, t.routeTableName)
		return res
	}

	allowReassignment := stringBool(spec.Parameters["allowReassignment"])
	if allowReassignment {
		if err := updateRouteTableRoute(ctx, runner, t); err != nil {
			if !isNotFoundError(err) {
				return failed("assign-route-table-route execute: route update failed", err)
			}
			if err := createRouteTableRoute(ctx, runner, t); err != nil {
				return failed("assign-route-table-route execute: route create after missing update failed", err)
			}
		}
	} else if err := createRouteTableRoute(ctx, runner, t); err != nil {
		if !isAlreadyExistsError(err) {
			return failed("assign-route-table-route execute: route create failed", err)
		}
		existing, found, err := showRouteTableRoute(ctx, runner, t)
		if err != nil {
			return failed("assign-route-table-route execute: route show after existing create failed", err)
		}
		if !found {
			return failed("assign-route-table-route execute: route already exists but show returned missing", nil)
		}
		if !sameIP(existing.nextHopIPAddress, t.nextHopIPAddress) {
			return failed(fmt.Sprintf("assign-route-table-route execute: route already exists with next hop %s, not %s", existing.nextHopIPAddress, t.nextHopIPAddress), nil)
		}
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("routed %s to %s in %s", t.address, t.nextHopIPAddress, t.routeTableName)
	res.Status.Observed = map[string]string{"assignedRoute": t.address, "routeTableRef": t.routeTableName, "nextHopIPAddress": t.nextHopIPAddress}
	return res
}

func createRouteTableRoute(ctx context.Context, runner azRunner, t routeTarget) error {
	_, err := callAzWithRetry(ctx, runner, "network", "route-table", "route", "create",
		"--resource-group", t.resourceGroup,
		"--route-table-name", t.routeTableName,
		"--name", t.routeName,
		"--address-prefix", t.address,
		"--next-hop-type", "VirtualAppliance",
		"--next-hop-ip-address", t.nextHopIPAddress)
	return err
}

func updateRouteTableRoute(ctx context.Context, runner azRunner, t routeTarget) error {
	_, err := callAzWithRetry(ctx, runner, "network", "route-table", "route", "update",
		"--resource-group", t.resourceGroup,
		"--route-table-name", t.routeTableName,
		"--name", t.routeName,
		"--set",
		"addressPrefix="+t.address,
		"nextHopType=VirtualAppliance",
		"nextHopIpAddress="+t.nextHopIPAddress)
	return err
}

func unassignRouteTableRoute(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireRouteTarget(spec)
	if err != nil {
		return failed("unassign-route-table-route: missing target field", err)
	}
	res := newResult()

	if mode == modeDryRun {
		if _, derr := runner(ctx, "network", "route-table", "show", "--resource-group", t.resourceGroup, "--name", t.routeTableName); derr != nil {
			return failed("unassign-route-table-route dry-run: route table show failed", derr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("would delete route %s from %s", t.address, t.routeTableName)
		return res
	}

	existing, found, err := showRouteTableRoute(ctx, runner, t)
	if err != nil {
		return failed("unassign-route-table-route execute: route show failed", err)
	}
	if !found {
		res.Status.Status = statusSkipped
		res.Status.Message = fmt.Sprintf("route %s already absent from %s", t.routeName, t.routeTableName)
		return res
	}
	if !sameIP(existing.nextHopIPAddress, t.nextHopIPAddress) {
		res.Status.Status = statusSkipped
		res.Status.Message = fmt.Sprintf("route %s in %s is held by %s, not %s; leaving it intact", t.routeName, t.routeTableName, existing.nextHopIPAddress, t.nextHopIPAddress)
		return res
	}

	if _, err := callAzWithRetry(ctx, runner, "network", "route-table", "route", "delete",
		"--resource-group", t.resourceGroup,
		"--route-table-name", t.routeTableName,
		"--name", t.routeName,
		"--yes"); err != nil {
		if isNotFoundError(err) {
			res.Status.Status = statusSkipped
			res.Status.Message = fmt.Sprintf("route %s already absent from %s", t.routeName, t.routeTableName)
			return res
		}
		return failed("unassign-route-table-route execute: route delete failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("deleted route %s from %s", t.address, t.routeTableName)
	return res
}

type routeTableRoute struct {
	addressPrefix    string
	nextHopIPAddress string
}

func showRouteTableRoute(ctx context.Context, runner azRunner, t routeTarget) (routeTableRoute, bool, error) {
	out, err := callAzReadWithRetry(ctx, runner, "network", "route-table", "route", "show",
		"--resource-group", t.resourceGroup,
		"--route-table-name", t.routeTableName,
		"--name", t.routeName)
	if err != nil {
		if isNotFoundError(err) {
			return routeTableRoute{}, false, nil
		}
		return routeTableRoute{}, false, err
	}
	var parsed struct {
		AddressPrefix    string `json:"addressPrefix"`
		NextHopIPAddress string `json:"nextHopIpAddress"`
		Properties       struct {
			AddressPrefix    string `json:"addressPrefix"`
			NextHopIPAddress string `json:"nextHopIpAddress"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return routeTableRoute{}, false, fmt.Errorf("parse network route-table route show output: %w", err)
	}
	route := routeTableRoute{
		addressPrefix:    firstNonEmpty(parsed.AddressPrefix, parsed.Properties.AddressPrefix),
		nextHopIPAddress: firstNonEmpty(parsed.NextHopIPAddress, parsed.Properties.NextHopIPAddress),
	}
	if strings.TrimSpace(route.addressPrefix) == "" && strings.TrimSpace(route.nextHopIPAddress) == "" {
		return routeTableRoute{}, false, nil
	}
	return route, true, nil
}

// assignSecondaryIP attaches the captured /32 to the NIC via a new ip-config.
//   - dry-run: show the NIC (read-only), report "would assign".
//   - execute: network nic ip-config create. With allowReassignment=true it
//     performs Azure's non-atomic seize as delete-old-ip-config -> create-self.
func assignSecondaryIP(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireIPConfigTarget(spec, true)
	if err != nil {
		return failed("assign-secondary-ip: missing target field", err)
	}
	res := newResult()
	res.Status.UndoAvailable = true
	allowReassignment := stringBool(spec.Parameters["allowReassignment"])

	if mode == modeDryRun {
		if _, derr := showNIC(ctx, runner, t.nicID); derr != nil {
			return failed("assign-secondary-ip dry-run: nic show failed", derr)
		}
		res.Status.Status = statusSucceeded
		if allowReassignment {
			res.Status.Message = fmt.Sprintf("would seize/reassign %s to %s", t.address, t.nicName)
		} else {
			res.Status.Message = fmt.Sprintf("would assign %s to %s", t.address, t.nicName)
		}
		return res
	}

	if allowReassignment {
		return seizeSecondaryIP(ctx, t, runner)
	}

	if err := createIPConfig(ctx, runner, t); err != nil {
		if isAlreadyExistsError(err) {
			cfg, serr := waitForSelfAddress(ctx, runner, t)
			if serr != nil {
				return failed("assign-secondary-ip execute: verify existing self ip-config failed", serr)
			}
			res.Status.Status = statusSucceeded
			res.Status.Message = fmt.Sprintf("assigned %s to %s (already present as ip-config %s)", t.address, t.nicName, cfg.Name)
			res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": cfg.Name, "assignAlreadyPresent": "true"}
			return res
		}
		if isAddressConflictError(err) {
			return failSecondaryIPConflict(ctx, t, runner, err)
		}
		return failed("assign-secondary-ip execute: ip-config create failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("assigned %s to %s (ip-config %s)", t.address, t.nicName, t.ipConfigName)
	res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": t.ipConfigName}
	return res
}

func failSecondaryIPConflict(ctx context.Context, t nicTarget, runner azRunner, originalErr error) executeActionResult {
	res := newResult()
	res.Status.UndoAvailable = true

	holder, found, err := discoverCurrentHolder(ctx, runner, t)
	if err != nil {
		return failed("assign-secondary-ip execute: holder rediscovery after conflict failed", err)
	}
	if !found {
		return failed("assign-secondary-ip execute: ip-config create conflict but no displaced holder found", originalErr)
	}
	if holder.sameNIC(t.resourceGroup, t.nicName) {
		cfg, serr := waitForSelfAddress(ctx, runner, t)
		if serr != nil {
			return failed("assign-secondary-ip execute: verify conflict self ip-config failed", serr)
		}
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("assigned %s to %s (already present as ip-config %s)", t.address, t.nicName, cfg.Name)
		res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": cfg.Name, "assignAlreadyPresent": "true"}
		return res
	}
	res.Status.Status = statusFailed
	res.Status.Message = "assign-secondary-ip execute: address held by another target"
	res.Status.Error = originalErr.Error()
	res.Status.Observed = map[string]string{
		"failureClass":                "addressHeldByAnotherTarget",
		"addressHeldByAnotherTarget":  "true",
		"observedHolderResourceGroup": holder.resourceGroup,
		"observedHolderNIC":           holder.nicName,
		"observedHolderIPConfig":      holder.ipConfigName,
	}
	return res
}

func seizeSecondaryIP(ctx context.Context, t nicTarget, runner azRunner) executeActionResult {
	res := newResult()
	res.Status.UndoAvailable = true

	self, err := showNIC(ctx, runner, t.nicID)
	if err != nil {
		return failed("assign-secondary-ip execute: self nic show failed", err)
	}
	if cfg, ok := ipConfigForAddress(self.IPConfigurations, t.address); ok {
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("seized/reassigned %s to %s (already present as ip-config %s)", t.address, t.nicName, cfg.Name)
		res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": cfg.Name, "seizeAlreadyPresent": "true"}
		return res
	}

	holder, found, err := discoverCurrentHolder(ctx, runner, t)
	if err != nil {
		return failed("assign-secondary-ip execute: holder discovery failed", err)
	}
	if found && !holder.sameNIC(t.resourceGroup, t.nicName) {
		if err := deleteIPConfig(ctx, runner, holder); err != nil {
			return failed("assign-secondary-ip execute: displaced ip-config delete failed", err)
		}
	}

	if err := createIPConfig(ctx, runner, t); err != nil {
		if isAlreadyExistsError(err) {
			cfg, serr := waitForSelfAddress(ctx, runner, t)
			if serr != nil {
				return failed("assign-secondary-ip execute: verify existing self ip-config failed", serr)
			}
			res.Status.Status = statusSucceeded
			res.Status.Message = fmt.Sprintf("seized/reassigned %s to %s (already present as ip-config %s)", t.address, t.nicName, cfg.Name)
			res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": cfg.Name, "seizeAlreadyPresent": "true"}
			return res
		}
		if isAddressConflictError(err) {
			rediscovered, rediscoveredFound, derr := discoverCurrentHolder(ctx, runner, t)
			if derr != nil {
				return failed("assign-secondary-ip execute: holder rediscovery after conflict failed", derr)
			}
			if rediscoveredFound && !rediscovered.sameNIC(t.resourceGroup, t.nicName) {
				if derr := deleteIPConfig(ctx, runner, rediscovered); derr != nil {
					return failed("assign-secondary-ip execute: displaced ip-config delete after conflict failed", derr)
				}
				if cerr := createIPConfig(ctx, runner, t); cerr != nil {
					return failed("assign-secondary-ip execute: ip-config create after conflict failed", cerr)
				}
				holder = rediscovered
				found = true
			} else {
				return failed("assign-secondary-ip execute: ip-config create conflict but no displaced holder found", err)
			}
		} else {
			return failed("assign-secondary-ip execute: ip-config create failed", err)
		}
	}

	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("seized/reassigned %s to %s (ip-config %s)", t.address, t.nicName, t.ipConfigName)
	res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": t.ipConfigName}
	return res
}

func waitForSelfAddress(ctx context.Context, runner azRunner, t nicTarget) (ipConfig, error) {
	var lastErr error
	for attempt := 0; attempt < seizeVerifyAttempts; attempt++ {
		self, err := showNIC(ctx, runner, t.nicID)
		if err == nil {
			if cfg, ok := ipConfigForAddress(self.IPConfigurations, t.address); ok {
				return cfg, nil
			}
			lastErr = fmt.Errorf("self NIC %s missing assigned address %s", t.nicName, t.address)
		} else {
			lastErr = err
		}
		if err := sleepBeforeRetry(ctx, attempt); err != nil {
			return ipConfig{}, err
		}
	}
	return ipConfig{}, lastErr
}

func waitForDisplacedRelease(ctx context.Context, runner azRunner, h ipConfigHolder, address string) error {
	var lastErr error
	for attempt := 0; attempt < seizeVerifyAttempts; attempt++ {
		configs, err := listIPConfigs(ctx, runner, h.resourceGroup, h.nicName)
		if err == nil {
			if _, stillPresent := ipConfigForAddress(configs, address); !stillPresent {
				return nil
			}
			lastErr = fmt.Errorf("displaced NIC %s still holds address %s", h.nicName, bareIP(address))
		} else {
			lastErr = err
		}
		if err := sleepBeforeRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return lastErr
}

func sleepBeforeRetry(ctx context.Context, attempt int) error {
	if attempt >= seizeVerifyAttempts-1 {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	sleep(seizeVerifyDelay)
	return ctx.Err()
}

func createIPConfig(ctx context.Context, runner azRunner, t nicTarget) error {
	_, _, err := ensureNICAssignedAndForwarding(ctx, runner, t)
	return err
}

func ensureNICAssignedAndForwarding(ctx context.Context, runner azRunner, t nicTarget) (string, bool, error) {
	nic, err := showNIC(ctx, runner, t.nicID)
	if err != nil {
		return "", false, err
	}
	body, changed, err := buildNICAssignForwardingBody(nic, t)
	if err != nil {
		return "", false, err
	}
	if !changed {
		return string(body), false, nil
	}
	_, err = putNIC(ctx, runner, t.nicID, body)
	return string(body), true, err
}

func putNIC(ctx context.Context, runner azRunner, nicID string, body []byte) ([]byte, error) {
	return callAzWithRetry(ctx, runner, "rest",
		"--method", "put",
		"--uri", nicResourceURI(nicID),
		"--body", string(body))
}

func nicResourceURI(nicID string) string {
	if strings.Contains(nicID, "?") {
		return nicID
	}
	return strings.TrimSpace(nicID) + "?api-version=" + networkAPIVersion
}

func buildNICAssignForwardingBody(nic nicShow, t nicTarget) ([]byte, bool, error) {
	if nic.Raw == nil {
		return nil, false, fmt.Errorf("network nic show output missing raw body")
	}
	body, ipConfigs, err := nicPUTBodyAndConfigs(nic)
	if err != nil {
		return nil, false, err
	}
	setNICPUTForwarding(body, true)
	hasAddress := false
	for _, cfg := range ipConfigs {
		if sameIP(rawIPConfigAddress(cfg), t.address) {
			hasAddress = true
			break
		}
	}
	if !hasAddress {
		subnet, ok := subnetForNewIPConfig(ipConfigs)
		if !ok {
			return nil, false, fmt.Errorf("network nic show output missing subnet for new ip-config")
		}
		setNICPUTIPConfigurations(body, append(ipConfigs, map[string]any{
			"name": t.ipConfigName,
			"properties": map[string]any{
				"privateIPAddress":          t.address,
				"privateIPAllocationMethod": "Static",
				"primary":                   false,
				"subnet":                    subnet,
			},
		}))
	}

	changed := !nic.EnableIPForwarding || !hasAddress
	out, err := json.Marshal(body)
	if err != nil {
		return nil, false, fmt.Errorf("marshal network nic update body: %w", err)
	}
	return out, changed, nil
}

func nicPUTBodyAndConfigs(nic nicShow) (map[string]any, []any, error) {
	rawConfigs, err := rawIPConfigurations(nic.Raw)
	if err != nil {
		return nil, nil, err
	}
	configs := make([]any, 0, len(rawConfigs))
	for _, cfg := range rawConfigs {
		configs = append(configs, normalizeIPConfigForPUT(cfg))
	}
	body := map[string]any{}
	for _, key := range []string{"location", "tags", "identity", "extendedLocation"} {
		if v, ok := nic.Raw[key]; ok {
			body[key] = v
		}
	}
	props := map[string]any{}
	if rawProps, ok := nic.Raw["properties"].(map[string]any); ok {
		props = cloneJSONMap(rawProps)
	}
	body["properties"] = props
	setNICPUTIPConfigurations(body, configs)
	return body, configs, nil
}

func normalizeIPConfigForPUT(raw any) map[string]any {
	cfg, _ := raw.(map[string]any)
	out := map[string]any{}
	if name, ok := cfg["name"]; ok {
		out["name"] = name
	}
	props := map[string]any{}
	if rawProps, ok := cfg["properties"].(map[string]any); ok {
		props = cloneJSONMap(rawProps)
	}
	for _, key := range []string{
		"privateIPAddress",
		"privateIpAddress",
		"privateIPAddressVersion",
		"privateIPAllocationMethod",
		"primary",
		"subnet",
		"publicIPAddress",
		"applicationSecurityGroups",
		"loadBalancerBackendAddressPools",
		"loadBalancerInboundNatRules",
	} {
		if v, ok := cfg[key]; ok {
			props[key] = v
		}
	}
	out["properties"] = props
	return out
}

func setNICPUTForwarding(body map[string]any, enabled bool) {
	props := ensureNICPUTProperties(body)
	props["enableIPForwarding"] = enabled
}

func setNICPUTIPConfigurations(body map[string]any, configs []any) {
	props := ensureNICPUTProperties(body)
	props["ipConfigurations"] = configs
}

func ensureNICPUTProperties(body map[string]any) map[string]any {
	if props, ok := body["properties"].(map[string]any); ok {
		return props
	}
	props := map[string]any{}
	body["properties"] = props
	return props
}

func cloneJSONMap(in map[string]any) map[string]any {
	var out map[string]any
	b, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func rawIPConfigurations(body map[string]any) ([]any, error) {
	raw, ok := body["ipConfigurations"]
	if !ok {
		if props, propsOK := body["properties"].(map[string]any); propsOK {
			raw, ok = props["ipConfigurations"]
		}
	}
	if !ok {
		return nil, fmt.Errorf("network nic show output missing ipConfigurations")
	}
	configs, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("network nic show output has invalid ipConfigurations")
	}
	return configs, nil
}

func rawIPConfigAddress(cfg any) string {
	m, ok := cfg.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"privateIPAddress", "privateIpAddress"} {
		if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
			return bareIP(v)
		}
	}
	props, _ := m["properties"].(map[string]any)
	for _, key := range []string{"privateIPAddress", "privateIpAddress"} {
		if v, ok := props[key].(string); ok && strings.TrimSpace(v) != "" {
			return bareIP(v)
		}
	}
	return ""
}

func subnetForNewIPConfig(configs []any) (any, bool) {
	for _, cfg := range configs {
		m, ok := cfg.(map[string]any)
		if !ok {
			continue
		}
		props, _ := m["properties"].(map[string]any)
		if subnet, ok := m["subnet"]; ok {
			return subnet, true
		}
		if subnet, ok := props["subnet"]; ok {
			return subnet, true
		}
	}
	return nil, false
}

func deleteIPConfig(ctx context.Context, runner azRunner, h ipConfigHolder) error {
	err := deleteIPConfigRaw(ctx, runner, h)
	if err != nil && isNotFoundError(err) {
		return nil
	}
	return err
}

func deleteIPConfigRaw(ctx context.Context, runner azRunner, h ipConfigHolder) error {
	_, err := callAzWithRetry(ctx, runner, "network", "nic", "ip-config", "delete",
		"--resource-group", h.resourceGroup,
		"--nic-name", h.nicName,
		"--name", h.ipConfigName)
	return err
}

type ipConfigHolder struct {
	resourceGroup string
	nicName       string
	ipConfigName  string
}

func (h ipConfigHolder) sameNIC(resourceGroup, nicName string) bool {
	return strings.EqualFold(strings.TrimSpace(h.resourceGroup), strings.TrimSpace(resourceGroup)) &&
		strings.EqualFold(strings.TrimSpace(h.nicName), strings.TrimSpace(nicName))
}

func discoverCurrentHolder(ctx context.Context, runner azRunner, t nicTarget) (ipConfigHolder, bool, error) {
	if t.displaced.complete() {
		configs, err := listIPConfigs(ctx, runner, t.displaced.resourceGroup, t.displaced.nicName)
		if err != nil {
			if isNotFoundError(err) {
				return ipConfigHolder{}, false, nil
			}
			return ipConfigHolder{}, false, err
		}
		if cfg, ok := namedOrAddressConfig(configs, t.displaced.ipConfigName, t.address); ok {
			return ipConfigHolder{
				resourceGroup: t.displaced.resourceGroup,
				nicName:       t.displaced.nicName,
				ipConfigName:  cfg.Name,
			}, true, nil
		}
	}
	nics, err := listNICs(ctx, runner, t.resourceGroup)
	if err != nil {
		return ipConfigHolder{}, false, err
	}
	for _, nic := range nics {
		cfg, ok := ipConfigForAddress(nic.IPConfigurations, t.address)
		if !ok {
			continue
		}
		rg := strings.TrimSpace(nic.ResourceGroup)
		if rg == "" {
			rg = t.resourceGroup
		}
		return ipConfigHolder{resourceGroup: rg, nicName: nic.Name, ipConfigName: cfg.Name}, true, nil
	}
	return ipConfigHolder{}, false, nil
}

func namedOrAddressConfig(configs []ipConfig, name, address string) (ipConfig, bool) {
	for _, cfg := range configs {
		if strings.TrimSpace(cfg.Name) == strings.TrimSpace(name) && sameIP(cfg.PrivateIPAddress, address) {
			return cfg, true
		}
	}
	return ipConfigForAddress(configs, address)
}

func ipConfigForAddress(configs []ipConfig, address string) (ipConfig, bool) {
	address = bareIP(address)
	for _, cfg := range configs {
		if sameIP(cfg.PrivateIPAddress, address) {
			return cfg, true
		}
	}
	return ipConfig{}, false
}

func sameIP(left, right string) bool {
	return bareIP(left) == bareIP(right) && bareIP(left) != ""
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "notfound") ||
		strings.Contains(msg, "could not be found") ||
		strings.Contains(msg, "resourcenotfound")
}

func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "alreadyexists")
}

func isAddressConflictError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "privateipaddressisinuse") ||
		strings.Contains(msg, "private ip address") && strings.Contains(msg, "in use") ||
		strings.Contains(msg, "conflict") ||
		strings.Contains(msg, "already allocated")
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

	if nic.EnableIPForwarding {
		res.Status.Status = statusSucceeded
		res.Status.Message = fmt.Sprintf("ipForwarding already true on %s (prior=%s)", t.nicID, prior)
		return res
	}
	body, err := buildNICForwardingBody(nic)
	if err != nil {
		return failed("ensure-forwarding-enabled execute: build nic update body failed", err)
	}
	if _, err := putNIC(ctx, runner, t.nicID, body); err != nil {
		return failed("ensure-forwarding-enabled execute: nic put failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("set ipForwarding=true on %s (prior=%s)", t.nicID, prior)
	return res
}

func buildNICForwardingBody(nic nicShow) ([]byte, error) {
	if nic.Raw == nil {
		return nil, fmt.Errorf("network nic show output missing raw body")
	}
	body, _, err := nicPUTBodyAndConfigs(nic)
	if err != nil {
		return nil, err
	}
	setNICPUTForwarding(body, true)
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal network nic update body: %w", err)
	}
	return out, nil
}

// unassignSecondaryIP is the undo of assign-secondary-ip: delete the ip-config.
func unassignSecondaryIP(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireIPConfigTarget(spec, true)
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

	if err := deleteIPConfigRaw(ctx, runner, ipConfigHolder{
		resourceGroup: t.resourceGroup,
		nicName:       t.nicName,
		ipConfigName:  t.ipConfigName,
	}); err != nil {
		if isNotFoundError(err) {
			deleted, derr := unassignIPConfigByAddress(ctx, runner, t)
			if derr != nil {
				return failed("unassign-secondary-ip execute: nic show for fallback failed", derr)
			}
			if deleted {
				if err := waitForAddressAbsentFromNIC(ctx, runner, t); err != nil {
					return failed("unassign-secondary-ip execute: verify fallback delete failed", err)
				}
				res.Status.Status = statusSucceeded
				res.Status.Message = fmt.Sprintf("unassigned ip-config %s from %s", t.ipConfigName, t.nicName)
				return res
			}
			res.Status.Status = statusSucceeded
			res.Status.Message = fmt.Sprintf("ip-config %s already absent from %s", t.ipConfigName, t.nicName)
			return res
		}
		return failed("unassign-secondary-ip execute: ip-config delete failed", err)
	}
	if err := waitForAddressAbsentFromNIC(ctx, runner, t); err != nil {
		return failed("unassign-secondary-ip execute: verify delete failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("unassigned ip-config %s from %s", t.ipConfigName, t.nicName)
	return res
}

func waitForAddressAbsentFromNIC(ctx context.Context, runner azRunner, t nicTarget) error {
	var lastErr error
	for attempt := 0; attempt < seizeVerifyAttempts; attempt++ {
		self, err := showNIC(ctx, runner, t.nicID)
		if err == nil {
			if _, stillPresent := ipConfigForAddress(self.IPConfigurations, t.address); !stillPresent {
				return nil
			}
			lastErr = fmt.Errorf("NIC %s still holds address %s", t.nicName, bareIP(t.address))
		} else {
			lastErr = err
		}
		if err := sleepBeforeRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return lastErr
}

func unassignIPConfigByAddress(ctx context.Context, runner azRunner, t nicTarget) (bool, error) {
	self, err := showNIC(ctx, runner, t.nicID)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(strings.TrimSpace(self.Name), strings.TrimSpace(t.nicName)) {
		// If the NIC identity shifted in-place (unexpected), use the show response
		// as truth for the follow-up delete target.
		t.nicName = self.Name
	}
	cfg, ok := ipConfigForAddress(self.IPConfigurations, t.address)
	if !ok {
		return false, nil
	}
	rg := strings.TrimSpace(t.resourceGroup)
	if rg == "" && strings.TrimSpace(self.ResourceGroup) != "" {
		rg = strings.TrimSpace(self.ResourceGroup)
	}
	if err := deleteIPConfig(ctx, runner, ipConfigHolder{
		resourceGroup: rg,
		nicName:       t.nicName,
		ipConfigName:  cfg.Name,
	}); err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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

func stringBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// commandTimeout is the per-az-invocation timeout. Azure CLI calls can be slow
// when managed identity token acquisition or ARM control-plane reads are cold, so
// keep this independent from routerd's outer executor timeout.
func commandTimeout() time.Duration {
	if value := strings.TrimSpace(os.Getenv(azCommandTimeoutEnv)); value != "" {
		timeout, err := time.ParseDuration(value)
		if err == nil && timeout > 0 {
			return timeout
		}
	}
	if value := strings.TrimSpace(os.Getenv(legacyAzCommandTimeoutMsEnv)); value != "" {
		ms, err := strconv.Atoi(value)
		if err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return defaultAzCommandTimeout
}
