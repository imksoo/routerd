// SPDX-License-Identifier: BSD-3-Clause

// Command azure-provider-executor is a REAL Azure routerd executor plugin (ADR
// 0007, Phase 5.1) advertising the capability execute.providerAction. It performs
// the CloudEdge Selective Address Mobility NIC mutations — assigning a captured
// /32 to the cloud NIC's IP configuration and enabling IP forwarding on the NIC —
// through the gated, journaled execution path instead of by hand.
//
// REAL EXECUTOR — it mutates Azure, but ONLY in execute mode. In dry-run mode it
// issues read-only show/list calls and mutates nothing (enforced: see
// guardedRunner). It drives routerd's azure-routerd-helper via an injectable
// command runner (the azRunner func var, default execRunner running the shipped
// helper; tests inject a fake), so unit tests NEVER call real Azure.
//
// CREDENTIALS: azure-routerd-helper authenticates with Azure managed identity.
// routerd core passes it NO credentials and inherits NO parent environment to it
// (see RunExecutor); the executor reads no Azure credentials from the request.
// It imports no Azure SDK — provider SDK calls are isolated in
// azure-routerd-helper.
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

	actionAssignSecondaryIP       = "assign-secondary-ip"
	actionUnassignSecondaryIP     = "unassign-secondary-ip"
	actionAssignRouteTableRoute   = "assign-route-table-route"
	actionUnassignRouteTableRoute = "unassign-route-table-route"
	actionEnsureFwdEnabled        = "ensure-forwarding-enabled"
	actionEnsureFwdDisabled       = "ensure-forwarding-disabled"
	actionPreflight               = "preflight"
	captureStrategyRouteTable     = "route-table"
	defaultAzCommandTimeoutMs     = 120000
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
	subscriptionID string
	nicID          string // NIC resource id (for --ids on show/update)
	resourceGroup  string
	nicName        string // for ip-config create/delete (--nic-name)
	ipConfigName   string
	address        string
	displaced      displacedTarget
}

type displacedTarget struct {
	subscriptionID string
	nicID          string
	resourceGroup  string
	nicName        string
	ipConfigName   string
}

type routeTarget struct {
	subscriptionID   string
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
		subscriptionID: firstNonEmpty(spec.Target["subscriptionID"], spec.Target["subscriptionId"]),
		nicID:          spec.Target["nicRef"],
		resourceGroup:  spec.Target["resourceGroup"],
		nicName:        spec.Target["nicName"],
		ipConfigName:   spec.Target["ipConfigName"],
		address:        bareIP(spec.Target["address"]),
		displaced: displacedTarget{
			subscriptionID: firstNonEmpty(spec.Target["displacedSubscriptionID"], spec.Target["displacedSubscriptionId"], spec.Target["subscriptionID"], spec.Target["subscriptionId"]),
			nicID:          spec.Target["displacedNicRef"],
			resourceGroup:  spec.Target["displacedResourceGroup"],
			nicName:        spec.Target["displacedNicName"],
			ipConfigName:   spec.Target["displacedIpConfigName"],
		},
	}
	if t.nicID == "" {
		return nicTarget{}, fmt.Errorf("target.nicRef (NIC resource id) is required")
	}
	if t.subscriptionID == "" {
		t.subscriptionID = subscriptionFromARMID(t.nicID)
	}
	if t.displaced.subscriptionID == "" {
		t.displaced.subscriptionID = subscriptionFromARMID(t.displaced.nicID)
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

func subscriptionFromARMID(id string) string {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "subscriptions") {
			return strings.TrimSpace(parts[i+1])
		}
	}
	return ""
}

func routeTableRefParts(ref string) (subscriptionID, resourceGroup, routeTableName string) {
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		switch {
		case strings.EqualFold(parts[i], "subscriptions"):
			subscriptionID = strings.TrimSpace(parts[i+1])
		case strings.EqualFold(parts[i], "resourceGroups"):
			resourceGroup = strings.TrimSpace(parts[i+1])
		case strings.EqualFold(parts[i], "routeTables"):
			routeTableName = strings.TrimSpace(parts[i+1])
		}
	}
	return subscriptionID, resourceGroup, routeTableName
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
	if t.subscriptionID == "" {
		return nicTarget{}, fmt.Errorf("target.subscriptionId is required for Azure ip-config operations")
	}
	return t, nil
}

func requireRouteTarget(spec executeActionRequestSpec) (routeTarget, error) {
	routeTableRef := firstNonEmpty(spec.Target["routeTableName"], spec.Target["routeTableRef"])
	refSubscriptionID, refResourceGroup, refRouteTableName := routeTableRefParts(routeTableRef)
	t := routeTarget{
		subscriptionID:   firstNonEmpty(spec.Target["subscriptionID"], spec.Target["subscriptionId"], refSubscriptionID),
		resourceGroup:    firstNonEmpty(spec.Target["resourceGroup"], refResourceGroup),
		routeTableName:   firstNonEmpty(spec.Target["routeTableName"], spec.Target["routeTableRef"], refRouteTableName),
		routeName:        spec.Target["routeName"],
		address:          spec.Target["address"],
		nextHopIPAddress: spec.Target["nextHopIPAddress"],
	}
	if refRouteTableName != "" {
		t.routeTableName = refRouteTableName
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
	if t.subscriptionID == "" {
		return routeTarget{}, fmt.Errorf("target.subscriptionId is required for Azure route-table operations")
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
	if spec.Action == actionPreflight {
		return preflight(ctx)
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

func preflight(ctx context.Context) executeActionResult {
	path, err := resolveAzureHelperPath()
	if err != nil {
		return failed("provider executor preflight failed: Azure helper unavailable", err)
	}
	res := newResult()
	observed := map[string]string{"dependency": "azure-routerd-helper", "path": path}
	if strings.TrimSpace(os.Getenv(azureHelperEnv)) == "" && strings.TrimSpace(os.Getenv(azCLIPathEnv)) != "" {
		observed["legacyAzCLI"] = "true"
		res.Status.Status = statusSucceeded
		res.Status.Message = "legacy az CLI path available"
		res.Status.Observed = observed
		return res
	}
	versionOut, err := runHelperPreflightCommand(ctx, path, "version")
	if err != nil {
		return failed("provider executor preflight failed: Azure helper version failed", err)
	}
	var versionBody map[string]string
	if err := json.Unmarshal(versionOut, &versionBody); err == nil {
		if versionBody["version"] != "" {
			observed["version"] = versionBody["version"]
		}
	}
	preflightOut, err := runHelperPreflightCommand(ctx, path, "preflight")
	if err != nil {
		return failed("provider executor preflight failed: Azure helper identity probe failed", err)
	}
	var preflightBody map[string]string
	if err := json.Unmarshal(preflightOut, &preflightBody); err == nil {
		for k, v := range preflightBody {
			if strings.TrimSpace(v) != "" {
				observed[k] = v
			}
		}
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = "Azure helper available"
	res.Status.Observed = observed
	return res
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
		if _, derr := runner(ctx, appendSubscription(t.subscriptionID, "network", "route-table", "show", "--resource-group", t.resourceGroup, "--name", t.routeTableName)...); derr != nil {
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
	_, err := runner(ctx, appendSubscription(t.subscriptionID, "network", "route-table", "route", "create",
		"--resource-group", t.resourceGroup,
		"--route-table-name", t.routeTableName,
		"--name", t.routeName,
		"--address-prefix", t.address,
		"--next-hop-type", "VirtualAppliance",
		"--next-hop-ip-address", t.nextHopIPAddress)...)
	return err
}

func updateRouteTableRoute(ctx context.Context, runner azRunner, t routeTarget) error {
	_, err := runner(ctx, appendSubscription(t.subscriptionID, "network", "route-table", "route", "update",
		"--resource-group", t.resourceGroup,
		"--route-table-name", t.routeTableName,
		"--name", t.routeName,
		"--set",
		"addressPrefix="+t.address,
		"nextHopType=VirtualAppliance",
		"nextHopIpAddress="+t.nextHopIPAddress)...)
	return err
}

func unassignRouteTableRoute(ctx context.Context, spec executeActionRequestSpec, mode string, runner azRunner) executeActionResult {
	t, err := requireRouteTarget(spec)
	if err != nil {
		return failed("unassign-route-table-route: missing target field", err)
	}
	res := newResult()

	if mode == modeDryRun {
		if _, derr := runner(ctx, appendSubscription(t.subscriptionID, "network", "route-table", "show", "--resource-group", t.resourceGroup, "--name", t.routeTableName)...); derr != nil {
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

	if _, err := runner(ctx, appendSubscription(t.subscriptionID, "network", "route-table", "route", "delete",
		"--resource-group", t.resourceGroup,
		"--route-table-name", t.routeTableName,
		"--name", t.routeName,
		"--yes")...); err != nil {
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
	out, err := runner(ctx, appendSubscription(t.subscriptionID, "network", "route-table", "route", "show",
		"--resource-group", t.resourceGroup,
		"--route-table-name", t.routeTableName,
		"--name", t.routeName)...)
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

	if _, err := runner(ctx, appendSubscription(t.subscriptionID, "network", "nic", "ip-config", "create",
		"--resource-group", t.resourceGroup,
		"--nic-name", t.nicName,
		"--name", t.ipConfigName,
		"--private-ip-address", t.address)...); err != nil {
		if isAlreadyExistsError(err) {
			cfg, serr := waitForSelfAddress(ctx, runner, t)
			if serr != nil {
				return failed("assign-secondary-ip execute: verify existing self ip-config failed", serr)
			}
			res.Status.Status = statusSucceeded
			res.Status.Message = fmt.Sprintf("assigned %s to %s (already present as ip-config %s)", t.address, t.nicName, cfg.Name)
			res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": cfg.Name, "alreadyPresent": "true"}
			return res
		}
		return failed("assign-secondary-ip execute: ip-config create failed", err)
	}
	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("assigned %s to %s (ip-config %s)", t.address, t.nicName, t.ipConfigName)
	res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": t.ipConfigName}
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

	cfg, err := waitForSelfAddress(ctx, runner, t)
	if err != nil {
		return failed("assign-secondary-ip execute: verify self nic failed", err)
	}
	if found && !holder.sameNIC(t.resourceGroup, t.nicName) {
		if err := waitForDisplacedRelease(ctx, runner, holder, t.address); err != nil {
			return failed("assign-secondary-ip execute: verify displaced nic failed", err)
		}
	}

	res.Status.Status = statusSucceeded
	res.Status.Message = fmt.Sprintf("seized/reassigned %s to %s (ip-config %s)", t.address, t.nicName, cfg.Name)
	res.Status.Observed = map[string]string{"assignedAddress": t.address, "ipConfigName": cfg.Name}
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
		configs, err := listIPConfigs(ctx, runner, h.subscriptionID, h.resourceGroup, h.nicName)
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
	_, err := runner(ctx, appendSubscription(t.subscriptionID, "network", "nic", "ip-config", "create",
		"--resource-group", t.resourceGroup,
		"--nic-name", t.nicName,
		"--name", t.ipConfigName,
		"--private-ip-address", t.address)...)
	return err
}

func deleteIPConfig(ctx context.Context, runner azRunner, h ipConfigHolder) error {
	_, err := runner(ctx, appendSubscription(h.subscriptionID, "network", "nic", "ip-config", "delete",
		"--resource-group", h.resourceGroup,
		"--nic-name", h.nicName,
		"--name", h.ipConfigName)...)
	if err != nil && isNotFoundError(err) {
		return nil
	}
	return err
}

type ipConfigHolder struct {
	subscriptionID string
	resourceGroup  string
	nicName        string
	ipConfigName   string
}

func (h ipConfigHolder) sameNIC(resourceGroup, nicName string) bool {
	return strings.EqualFold(strings.TrimSpace(h.resourceGroup), strings.TrimSpace(resourceGroup)) &&
		strings.EqualFold(strings.TrimSpace(h.nicName), strings.TrimSpace(nicName))
}

func discoverCurrentHolder(ctx context.Context, runner azRunner, t nicTarget) (ipConfigHolder, bool, error) {
	if t.displaced.complete() {
		configs, err := listIPConfigs(ctx, runner, t.displaced.subscriptionID, t.displaced.resourceGroup, t.displaced.nicName)
		if err != nil {
			if isNotFoundError(err) {
				return ipConfigHolder{}, false, nil
			}
			return ipConfigHolder{}, false, err
		}
		if cfg, ok := namedOrAddressConfig(configs, t.displaced.ipConfigName, t.address); ok {
			return ipConfigHolder{
				subscriptionID: t.displaced.subscriptionID,
				resourceGroup:  t.displaced.resourceGroup,
				nicName:        t.displaced.nicName,
				ipConfigName:   cfg.Name,
			}, true, nil
		}
	}
	nics, err := listNICs(ctx, runner, t.subscriptionID, t.resourceGroup)
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
		return ipConfigHolder{subscriptionID: t.subscriptionID, resourceGroup: rg, nicName: nic.Name, ipConfigName: cfg.Name}, true, nil
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

	if _, err := runner(ctx, appendSubscription(t.subscriptionID, "network", "nic", "ip-config", "delete",
		"--resource-group", t.resourceGroup,
		"--nic-name", t.nicName,
		"--name", t.ipConfigName)...); err != nil {
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

func stringBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func appendSubscription(subscriptionID string, argv ...string) []string {
	out := append([]string(nil), argv...)
	if strings.TrimSpace(subscriptionID) != "" {
		out = append(out, "--subscription", strings.TrimSpace(subscriptionID))
	}
	return out
}

// commandTimeout is the per-az-invocation timeout.
func commandTimeout() time.Duration {
	return defaultAzCommandTimeoutMs * time.Millisecond
}
