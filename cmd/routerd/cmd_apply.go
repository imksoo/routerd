// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/config"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/inventory"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

func applyCommand(args []string, stdout, stderr io.Writer) (err error) {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	applyFlags := registerApplyFlags(fs, true, true)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*applyFlags.Once {
		return errors.New("apply currently requires --once")
	}
	router, err := config.Load(*applyFlags.ConfigPath)
	if err != nil {
		return err
	}
	if err := applyFlags.validateOverrides(); err != nil {
		return err
	}
	logger, err := eventlog.New(router)
	if err != nil {
		return err
	}
	defer closeLogger(logger, "apply", &err)
	logger.Emit(eventlog.LevelInfo, "apply", "routerd command started", map[string]string{
		"config": *applyFlags.ConfigPath,
		"dryRun": fmt.Sprintf("%t", *applyFlags.DryRun),
	})
	opts := applyFlags.applyOptions(*applyFlags.ConfigPath)
	opts.MgmtLockoutWriter = stderr
	_, err = runApplyChainOnce(context.Background(), router, opts, stdout, logger)
	return err
}

type applyOptions struct {
	ConfigPath           string
	StatusFile           string
	NetplanPath          string
	DnsmasqConfigPath    string
	DnsmasqServicePath   string
	NftablesPath         string
	LedgerPath           string
	StatePath            string
	BGPSocketPath        string
	BGPControlSocketPath string
	BGPStatePath         string
	OverrideClient       string
	OverrideProfile      string
	DryRun               bool
	SkipServiceManager   bool
	AllowMgmtLockout     bool
	AnnounceDryRunToCLI  bool
	MgmtLockoutWriter    io.Writer
	SkipConfigCommit     bool
	ConfigYAMLOverride   string
	Sandbox              bool
	MutationGate         *sync.RWMutex
	nativeOwner          *nativeOperationOwner
	attemptHooks         *applyAttemptHooks
}

func routerWithIPv6PDClientOptions(router *api.Router, opts applyOptions, osName string) (*api.Router, []string, error) {
	if router == nil {
		return nil, nil, errors.New("router is nil")
	}
	if !api.ValidIPv6PDClient(opts.OverrideClient) {
		return nil, nil, fmt.Errorf("unsupported DHCPv6PrefixDelegation client override %q; use routerd-dhcpv6-client", opts.OverrideClient)
	}
	if !api.ValidIPv6PDProfile(opts.OverrideProfile) {
		return nil, nil, fmt.Errorf("invalid DHCPv6PrefixDelegation profile override %q", opts.OverrideProfile)
	}

	out := *router
	out.Spec.Resources = append([]api.Resource(nil), router.Spec.Resources...)
	var warnings []string
	for i := range out.Spec.Resources {
		res := out.Spec.Resources[i]
		if res.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		spec, err := res.DHCPv6PrefixDelegationSpec()
		if err != nil {
			return nil, nil, err
		}
		if opts.OverrideProfile != "" {
			spec.Profile = opts.OverrideProfile
		}
		profile := defaultString(spec.Profile, api.IPv6PDProfileDefault)
		if opts.OverrideClient != "" {
			spec.Client = opts.OverrideClient
		} else {
			spec.Client = api.EffectiveIPv6PDClient(osName, profile, spec.Client)
		}
		if !api.ValidIPv6PDClient(spec.Client) {
			return nil, nil, fmt.Errorf("%s spec.client %q is unsupported; use routerd-dhcpv6-client or omit client", res.ID(), spec.Client)
		}
		if !api.ValidIPv6PDProfile(spec.Profile) {
			return nil, nil, fmt.Errorf("%s spec.profile is invalid: %q", res.ID(), spec.Profile)
		}
		out.Spec.Resources[i].Spec = spec
		ctx := api.IPv6PDClientContext{OS: strings.ToLower(osName), Client: spec.Client, Profile: profile}
		for _, item := range api.MatchKnownIPv6PDNGCombinations(ctx) {
			warnings = append(warnings, fmt.Sprintf("%s uses known problematic DHCPv6PrefixDelegation combination os=%s client=%s profile=%s: %s See %s. Continuing because known problematic combinations are warnings, not validation errors.", res.ID(), strings.ToLower(osName), spec.Client, profile, item.Reason, item.DocLink))
		}
	}
	if err := config.Validate(&out); err != nil {
		return nil, nil, err
	}
	return &out, warnings, nil
}

func routerConfigYAML(router *api.Router, opts applyOptions) (string, error) {
	if strings.TrimSpace(opts.ConfigYAMLOverride) != "" {
		return opts.ConfigYAMLOverride, nil
	}
	if path := strings.TrimSpace(opts.ConfigPath); path != "" {
		data, err := config.CanonicalYAMLFile(path)
		if err == nil {
			snapshot, err := config.LoadBytes(data, path)
			if err != nil {
				return "", err
			}
			if err := config.Validate(snapshot); err != nil {
				return "", fmt.Errorf("validate configuration snapshot %s: %w", path, err)
			}
			if !reflect.DeepEqual(snapshot, router) {
				return "", fmt.Errorf("configuration %s changed after it was loaded; retry the apply", path)
			}
			return string(data), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	data, err := yaml.Marshal(router)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func configGenerationHash(configYAML string, effectiveRouter *api.Router) (string, error) {
	sourceHash, err := config.NormalizedYAMLHash([]byte(configYAML))
	if err != nil {
		return "", fmt.Errorf("normalize configuration for generation: %w", err)
	}
	effective, err := json.Marshal(effectiveRouter)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append(append([]byte(sourceHash), '\n'), effective...))
	return "generation-v1-sha256:" + hex.EncodeToString(sum[:]), nil
}

func beginConfigGeneration(store *routerstate.SQLiteStore, configYAML string, effectiveRouter *api.Router) (int64, bool, error) {
	configHash, err := configGenerationHash(configYAML, effectiveRouter)
	if err != nil {
		return 0, false, err
	}
	generation, changed, err := store.BeginGenerationIfChanged(configHash, configYAML)
	if err != nil {
		return 0, false, err
	}
	return generation, changed, nil
}

func configCommitPhase(phase string) bool {
	switch strings.TrimSpace(phase) {
	case "Healthy", "Applied", "Committed":
		return true
	default:
		return false
	}
}

func recordWarningEvents(router *api.Router, store routerstate.Store, warnings []string) {
	recorder, ok := store.(routerstate.EventRecorder)
	if !ok {
		return
	}
	for _, warning := range warnings {
		_ = recorder.RecordEvent(router.APIVersion, router.Kind, router.Metadata.Name, "Warning", "ApplyWarning", warning)
		for _, res := range router.Spec.Resources {
			if strings.Contains(warning, res.ID()) {
				_ = recorder.RecordEvent(res.APIVersion, res.Kind, res.Metadata.Name, "Warning", "ApplyWarning", warning)
			}
		}
	}
}

func recordKnownNGCombinationEvents(router *api.Router, store routerstate.Store, warnings []string) {
	recorder, ok := store.(routerstate.EventRecorder)
	if !ok {
		return
	}
	for _, warning := range warnings {
		for _, res := range router.Spec.Resources {
			if strings.Contains(warning, res.ID()) {
				_ = recorder.RecordEvent(res.APIVersion, res.Kind, res.Metadata.Name, "Warning", "KnownNGCombination", warning)
			}
		}
	}
}

func recordHostInventoryState(ctx context.Context, store routerstate.Store) error {
	objectStore, ok := store.(routerstate.ObjectStatusStore)
	if !ok {
		return nil
	}
	observed, err := inventory.Collect(ctx)
	if err != nil {
		return err
	}
	status := inventoryStatusMap(observed)
	previous := objectStore.ObjectStatus(api.RouterAPIVersion, "Inventory", "host")
	if err := objectStore.SaveObjectStatus(api.RouterAPIVersion, "Inventory", "host", status); err != nil {
		return err
	}
	if recorder, ok := store.(routerstate.EventRecorder); ok && !reflect.DeepEqual(previous, status) {
		_ = recorder.RecordEvent(api.RouterAPIVersion, "Inventory", "host", "Normal", "InventoryObserved", "host inventory changed")
	}
	return nil
}

type staleObjectStatusCleanupStore interface {
	routerstate.ObjectStatusLister
	routerstate.ObjectDeleteStore
	routerstate.EventRecorder
	resourcequery.StateStore
}

type staleObjectStatusCleanupResult struct {
	Removed      []routerstate.ObjectStatus
	SnapshotPath string
	Skipped      bool
}

func cleanupUnsupportedLegacyObjectStatuses(router *api.Router, store staleObjectStatusCleanupStore, statePath string, now time.Time, logger *eventlog.Logger) (staleObjectStatusCleanupResult, error) {
	return cleanupUnsupportedLegacyObjectStatusesForOS(router, store, statePath, now, logger, platform.CurrentOS())
}

func cleanupUnsupportedLegacyObjectStatusesForOS(router *api.Router, store staleObjectStatusCleanupStore, statePath string, now time.Time, logger *eventlog.Logger, targetOS platform.OS) (staleObjectStatusCleanupResult, error) {
	if store == nil {
		return staleObjectStatusCleanupResult{}, nil
	}
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		emitStateCleanupWarning(logger, "stale state cleanup skipped: list object statuses failed", map[string]string{"error": err.Error()})
		recordStateCleanupEvent(router, store, "StaleStateCleanupSkipped", "stale state cleanup skipped: "+err.Error())
		return staleObjectStatusCleanupResult{Skipped: true}, err
	}
	desired, err := desiredObjectStatusKeysForOS(router, store, now, targetOS)
	if err != nil {
		emitStateCleanupWarning(logger, "stale state cleanup skipped: build effective resource view failed", map[string]string{"error": err.Error()})
		recordStateCleanupEvent(router, store, "StaleStateCleanupSkipped", "stale state cleanup skipped: "+err.Error())
		return staleObjectStatusCleanupResult{Skipped: true}, err
	}
	plan := lifecycle.PlanStatusGC(desired, statuses)
	removed := plan.StatusDeletes
	if len(removed) == 0 {
		return staleObjectStatusCleanupResult{}, nil
	}
	snapshotPath := staleStateCleanupSnapshotPath(statePath, now)
	if err := writeStaleStateCleanupSnapshot(snapshotPath, removed); err != nil {
		msg := fmt.Sprintf("stale state cleanup skipped: failed to write snapshot %s: %v", snapshotPath, err)
		emitStateCleanupWarning(logger, msg, map[string]string{"snapshot": snapshotPath, "error": err.Error(), "resources": staleObjectStatusIDs(removed)})
		recordStateCleanupEvent(router, store, "StaleStateCleanupSkipped", msg)
		return staleObjectStatusCleanupResult{Removed: removed, SnapshotPath: snapshotPath, Skipped: true}, nil
	}
	for _, status := range removed {
		if err := store.DeleteObject(status.APIVersion, status.Kind, status.Name); err != nil {
			msg := fmt.Sprintf("stale state cleanup stopped after snapshot %s: delete %s failed: %v", snapshotPath, objectStatusID(status), err)
			emitStateCleanupWarning(logger, msg, map[string]string{"snapshot": snapshotPath, "error": err.Error(), "resource": objectStatusID(status)})
			recordStateCleanupEvent(router, store, "StaleStateCleanupPartial", msg)
			return staleObjectStatusCleanupResult{Removed: removed, SnapshotPath: snapshotPath, Skipped: true}, err
		}
	}
	pruneStaleStateCleanupBackups(statePath, 5, logger)
	msg := fmt.Sprintf("removed %d stale resource status rows after snapshot %s", len(removed), snapshotPath)
	emitStateCleanupWarning(logger, msg, map[string]string{"snapshot": snapshotPath, "count": strconv.Itoa(len(removed)), "resources": staleObjectStatusIDs(removed)})
	recordStateCleanupEvent(router, store, "StaleStateCleanup", msg)
	return staleObjectStatusCleanupResult{Removed: removed, SnapshotPath: snapshotPath}, nil
}

func desiredObjectStatusKeysForOS(router *api.Router, store resourcequery.StateStore, now time.Time, targetOS platform.OS) (map[string]bool, error) {
	out := map[string]bool{}
	if router == nil {
		return out, nil
	}
	effective, err := resourcequery.FilterRouterByWhen(router, store)
	if err != nil {
		return nil, err
	}
	routers, err := controllerchain.BuildDynamicRouteSAMObjectStatusRouters(effective, store, now.UTC(), targetOS)
	if err != nil {
		return nil, err
	}
	for _, candidate := range routers {
		for key := range configuredResourceStatusKeys(candidate) {
			out[key] = true
		}
	}
	for _, resource := range router.Spec.Resources {
		when := resourcequery.ResourceWhen(resource)
		matches, err := resourcequery.ResourceWhenMatches(when, store)
		if err != nil {
			return nil, err
		}
		if !resourcequery.ResourceWhenPresent(when) || matches {
			continue
		}
		if strings.TrimSpace(resource.Kind) == "" || strings.TrimSpace(resource.Metadata.Name) == "" {
			continue
		}
		apiVersion := resource.APIVersion
		if apiVersion == "" {
			apiVersion = lifecycle.APIVersionForKind(resource.Kind)
		}
		if strings.TrimSpace(apiVersion) == "" {
			continue
		}
		out[apiVersion+"/"+resource.Kind+"/"+resource.Metadata.Name] = true
	}
	return out, nil
}

func configuredResourceStatusKeys(router *api.Router) map[string]bool {
	out := map[string]bool{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if strings.TrimSpace(resource.Kind) == "" || strings.TrimSpace(resource.Metadata.Name) == "" {
			continue
		}
		apiVersion := resource.APIVersion
		if apiVersion == "" {
			apiVersion = lifecycle.APIVersionForKind(resource.Kind)
		}
		if strings.TrimSpace(apiVersion) == "" {
			continue
		}
		out[apiVersion+"/"+resource.Kind+"/"+resource.Metadata.Name] = true
	}
	return out
}

func staleStateCleanupSnapshotPath(statePath string, now time.Time) string {
	base := filepath.Base(statePath)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "routerd.db"
	}
	return filepath.Join(filepath.Dir(statePath), fmt.Sprintf("%s.stale-cleanup.%s.json", base, now.UTC().Format("20060102T150405Z")))
}

type staleStateCleanupSnapshot struct {
	CreatedAt string                     `json:"createdAt"`
	Resources []routerstate.ObjectStatus `json:"resources"`
}

func writeStaleStateCleanupSnapshot(path string, removed []routerstate.ObjectStatus) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	snapshot := staleStateCleanupSnapshot{
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Resources: removed,
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func pruneStaleStateCleanupBackups(statePath string, keep int, logger *eventlog.Logger) {
	if keep <= 0 {
		return
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(statePath), filepath.Base(statePath)+".stale-cleanup.*"))
	if err != nil || len(matches) <= keep {
		return
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, path := range matches[keep:] {
		if err := os.Remove(path); err != nil {
			emitStateCleanupWarning(logger, "failed to prune stale state cleanup snapshot", map[string]string{"snapshot": path, "error": err.Error()})
		}
	}
}

func emitStateCleanupWarning(logger *eventlog.Logger, message string, attrs map[string]string) {
	if logger == nil {
		return
	}
	logger.Emit(eventlog.LevelWarning, "serve", message, attrs)
}

func recordStateCleanupEvent(router *api.Router, recorder routerstate.EventRecorder, reason, message string) {
	if recorder == nil {
		return
	}
	name := "router"
	if router != nil && strings.TrimSpace(router.Metadata.Name) != "" {
		name = strings.TrimSpace(router.Metadata.Name)
	}
	_ = recorder.RecordEvent(api.RouterAPIVersion, "Router", name, "Warning", reason, message)
}

func staleObjectStatusIDs(statuses []routerstate.ObjectStatus) string {
	ids := make([]string, 0, len(statuses))
	for _, status := range statuses {
		ids = append(ids, objectStatusID(status))
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func reportManagementPlaneFindings(w io.Writer, findings []config.ManagementPlaneFinding, allowLockout bool) {
	if w == nil || len(findings) == 0 {
		return
	}
	for _, finding := range findings {
		severity := finding.Severity
		if allowLockout && severity == config.ManagementPlaneFail {
			severity = config.ManagementPlaneWarn
		}
		fmt.Fprintf(w, "management-plane %s %s: %s", strings.ToUpper(severity), finding.Resource, finding.Message)
		if finding.Remedy != "" {
			fmt.Fprintf(w, " Remedy: %s.", finding.Remedy)
		}
		fmt.Fprintln(w)
	}
}

func managementPlaneWarnings(findings []config.ManagementPlaneFinding) []string {
	warnings := make([]string, 0, len(findings))
	for _, finding := range findings {
		warnings = append(warnings, fmt.Sprintf("management-plane %s %s: %s", finding.Severity, finding.Resource, finding.Message))
	}
	return warnings
}

func managementPlaneHasFailures(findings []config.ManagementPlaneFinding) bool {
	for _, finding := range findings {
		if finding.Severity == config.ManagementPlaneFail {
			return true
		}
	}
	return false
}

func checkManagementPlaneBeforeApply(router *api.Router, opts applyOptions) ([]string, error) {
	findings := config.CheckManagementPlane(router)
	reportManagementPlaneFindings(opts.MgmtLockoutWriter, findings, opts.AllowMgmtLockout)
	warnings := managementPlaneWarnings(findings)
	if !opts.DryRun && !opts.AllowMgmtLockout && managementPlaneHasFailures(findings) {
		return warnings, errors.New("management plane lockout risk; fix ManagementAccess findings or re-run with --allow-mgmt-lockout")
	}
	return warnings, nil
}

func objectStatusID(status routerstate.ObjectStatus) string {
	return status.APIVersion + "/" + status.Kind + "/" + status.Name
}

func runApplyChainOnce(ctx context.Context, router *api.Router, opts applyOptions, stdout io.Writer, logger *eventlog.Logger) (*apply.Result, error) {
	outcome, err := runApplyChainOnceWithOutcome(ctx, router, opts, stdout, logger)
	return outcome.Result, err
}

func runApplyChainOnceWithOutcome(ctx context.Context, router *api.Router, opts applyOptions, stdout io.Writer, logger *eventlog.Logger) (outcome applyAttemptOutcome, err error) {
	if !opts.DryRun && !opts.Sandbox && opts.nativeOwner == nil {
		owner, acquireErr := acquireNativeOperationOwner(ctx)
		if acquireErr != nil {
			return outcome, acquireErr
		}
		opts.nativeOwner = owner
		defer func() { err = errors.Join(err, owner.finish(true)) }()
	}
	outcome = applyAttemptOutcome{Stage: "prepare", Canonical: canonicalNotRequested}
	defer outcome.wrapError(&err, logger)
	_, err = runApplyChainOnceAttempt(ctx, router, opts, stdout, logger, &outcome)
	return outcome, err
}

func runApplyChainOnceAttempt(ctx context.Context, router *api.Router, opts applyOptions, stdout io.Writer, logger *eventlog.Logger, outcome *applyAttemptOutcome) (_ *apply.Result, err error) {
	var optionWarnings []string
	var configYAML string
	if !opts.DryRun {
		configYAML, err = routerConfigYAML(router, opts)
		if err != nil {
			return nil, err
		}
	}
	effectiveConfig, warnings, err := routerWithIPv6PDClientOptions(router, opts, string(platformDefaults.OS))
	if err != nil {
		return nil, err
	}
	router = effectiveConfig
	optionWarnings = append(optionWarnings, warnings...)
	optionWarnings = append(optionWarnings, config.Warnings(router)...)
	managementWarnings, err := checkManagementPlaneBeforeApply(router, opts)
	if err != nil {
		return nil, err
	}
	optionWarnings = append(optionWarnings, managementWarnings...)
	if opts.DryRun {
		outcome.Stage = "plan"
		result, err := planApply(ctx, router, opts)
		if err != nil {
			return nil, err
		}
		outcome.Result = result
		result.Warnings = append(result.Warnings, optionWarnings...)
		outcome.Stage = "output"
		if opts.AnnounceDryRunToCLI {
			announcement := fmt.Sprintf("dry-run apply plan for %s\n", opts.ConfigPath)
			if n, err := io.WriteString(stdout, announcement); err != nil {
				return result, err
			} else if n != len(announcement) {
				return result, io.ErrShortWrite
			}
		}
		if err := writeResult(stdout, opts.StatusFile, result); err != nil {
			return result, err
		}
		outcome.Stage = "complete"
		return result, nil
	}
	statePath := defaultString(opts.StatePath, defaultStatePath)
	outcome.Stage = "open-state"
	stateStore, err := routerstate.OpenSQLite(statePath)
	if err != nil {
		return nil, err
	}
	defer stateStore.Close()

	var generation int64
	generationCreated := false
	if !opts.DryRun {
		outcome.Stage = "begin-generation"
		generation, generationCreated, err = beginConfigGeneration(stateStore, configYAML, router)
		if err != nil {
			return nil, err
		}
		outcome.Generation, outcome.GenerationCreated = generation, generationCreated
		defer outcome.finishEarlyFailure(stateStore, opts, &err)
		outcome.Stage = "inventory"
		if err := recordHostInventoryState(ctx, stateStore); err != nil {
			return nil, err
		}
	}
	eventBus := bus.NewWithStore(stateStore)
	eventBus.SetLogger(slog.Default())
	outcome.Stage = "plan"
	err = recordObservedPrefixDelegationState(router, stateStore, eventBus)
	if err != nil {
		return nil, err
	}
	runner := &controllerchain.Runner{Router: router, Bus: eventBus, Store: stateStore}
	defer func() {
		if runner.RuntimeMutationError() != nil {
			opts.nativeOwner.retainUntilProcessExit()
		}
	}()
	view, err := runner.ObserveRuntimeRouter(time.Now().UTC(), platformDefaults.OS)
	if err != nil {
		return nil, err
	}
	effectiveRouter := view.RouteRouter
	result, err := apply.New(ctx).PlanEffective(effectiveRouter, view.DeclaredRouter)
	if err != nil {
		return nil, err
	}
	outcome.Result = result
	if generation != 0 {
		result.Generation = generation
	}
	result.Warnings = append(result.Warnings, optionWarnings...)
	appendPrefixDelegationStateWarnings(result, router, stateStore)
	if err := appendLedgerOwnedOrphans(ctx, result, effectiveRouter, router, opts.LedgerPath, opts.DryRun); err != nil {
		return nil, err
	}
	decision := ha.Decision{Leader: true}
	if !opts.DryRun {
		outcome.Stage = "ha-admission"
		var clusterName string
		decision, clusterName, err = acquireApplyClusterLease(ctx, effectiveRouter, stateStore)
		if err != nil {
			return nil, err
		}
		if decision.Enabled && decision.Lease != nil {
			defer decision.Lease.Close()
		}
		if decision.Enabled && !decision.Leader {
			result.Phase = "Standby"
			result.Warnings = append(result.Warnings, fmt.Sprintf("RouterdCluster/%s lease is held by %s; apply skipped on standby", clusterName, decision.Holder))
			outcome.Stage = "finish-generation"
			if err := outcome.finish(stateStore, opts, result.Phase, result.Warnings); err != nil {
				return result, err
			}
			outcome.Stage = "output"
			if err := writeResult(stdout, opts.StatusFile, result); err != nil {
				return result, err
			}
			if logger != nil {
				logger.Emit(eventlog.LevelInfo, "apply", "routerd apply skipped on standby", map[string]string{"cluster": clusterName, "holder": decision.Holder})
			}
			outcome.Stage = "complete"
			return result, nil
		}
		recordWarningEvents(router, stateStore, result.Warnings)
		recordKnownNGCombinationEvents(router, stateStore, optionWarnings)
	}

	controllerOpts := applyChainControllerOptions(opts)
	if opts.attemptHooks != nil && opts.attemptHooks.configureControllers != nil {
		opts.attemptHooks.configureControllers(&controllerOpts)
	}
	runner.Opts = controllerOpts
	runner.HADecision = &decision
	outcome.Stage = "reconcile"
	if err := runner.ReconcileOnce(ctx); err != nil {
		return nil, err
	}
	view, err = runner.ObserveRuntimeRouter(time.Now().UTC(), platformDefaults.OS)
	if err != nil {
		return nil, err
	}
	effectiveRouter = view.RouteRouter
	if !opts.DryRun {
		outcome.Stage = "record-applied-path"
		if err := recordLastAppliedPath(effectiveRouter, stateStore, opts.ConfigPath); err != nil {
			return nil, err
		}
	}
	applyWarnings := append([]string{}, result.Warnings...)
	outcome.Stage = "observe"
	result, err = apply.New(ctx).ObserveEffective(effectiveRouter, view.DeclaredRouter)
	if err != nil {
		return nil, err
	}
	outcome.Result = result
	if generation != 0 {
		result.Generation = generation
	}
	result.Warnings = append(result.Warnings, applyWarnings...)
	if err := appendLedgerOwnedOrphans(ctx, result, effectiveRouter, router, opts.LedgerPath, opts.DryRun); err != nil {
		return nil, err
	}
	if !opts.DryRun && configCommitPhase(result.Phase) {
		if err := outcome.commit(opts, configYAML); err != nil {
			return result, err
		}
	}
	outcome.Stage = "finish-generation"
	if err := outcome.finish(stateStore, opts, result.Phase, result.Warnings); err != nil {
		return result, err
	}
	if !configCommitPhase(result.Phase) {
		outcome.Stage = "observe-result"
		return result, fmt.Errorf("apply finished with phase %s; canonical config was not committed", result.Phase)
	}
	outcome.Stage = "output"

	if err := writeResult(stdout, opts.StatusFile, result); err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Emit(eventlog.LevelInfo, "apply", "routerd apply chain once completed", map[string]string{
			"phase":      result.Phase,
			"generation": strconv.FormatInt(result.Generation, 10),
			"dryRun":     fmt.Sprintf("%t", opts.DryRun),
		})
	}
	outcome.Stage = "complete"
	return result, nil
}

func applyChainControllerOptions(opts applyOptions) controllerchain.Options {
	controllerOpts := controllerchain.Options{
		SuperviseClientDaemons: false,
		SkipLegacyClientUnits:  true,
		SuperviseDNSResolvers:  false,
		DnsmasqCommand:         "dnsmasq",
		DnsmasqConfig:          defaultString(opts.DnsmasqConfigPath, defaultDnsmasqConfigPath),
		DnsmasqPID:             filepath.Join(platformDefaults.RuntimeDir, "dnsmasq.pid"),
		DnsmasqPort:            53,
		DnsmasqListen:          []string{"127.0.0.1"},
		NftablesPath:           defaultString(opts.NftablesPath, defaultNftablesPath),
		FirewallPath:           "/run/routerd/firewall.nft",
		LedgerPath:             defaultString(opts.LedgerPath, defaultLedgerPath),
		NftCommand:             "nft",
		ConntrackInterval:      30 * time.Second,
		BGPSocketPath:          opts.BGPSocketPath,
		BGPControlSocketPath:   opts.BGPControlSocketPath,
		BGPStatePath:           opts.BGPStatePath,
		MutationGate:           opts.MutationGate,
	}
	if !opts.DryRun && !opts.Sandbox {
		controllerOpts.ReconcileIPsec = applyIPsecConnections
	}
	if opts.DryRun || opts.Sandbox {
		applySandboxControllerOptions(&controllerOpts, controllerOpts.DnsmasqConfig, controllerOpts.NftablesPath)
		if !opts.Sandbox {
			controllerOpts.FirewallPath = filepath.Join(filepath.Dir(controllerOpts.NftablesPath), "firewall.nft")
		}
	}
	if opts.SkipServiceManager {
		controllerOpts.DryRunServiceUnit = true
	}
	return controllerOpts
}

func inventoryStatusMap(status inventory.Status) map[string]any {
	data, _ := json.Marshal(status)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

func acquireApplyClusterLease(ctx context.Context, router *api.Router, store any) (ha.Decision, string, error) {
	resource, spec, ok, err := applyClusterResource(router)
	if err != nil || !ok {
		return ha.Decision{Leader: true}, "", err
	}
	ttl := 30 * time.Second
	if strings.TrimSpace(spec.LeaseTTL) != "" {
		ttl, _ = time.ParseDuration(spec.LeaseTTL)
	}
	decision, err := ha.Acquire(ctx, ha.Config{
		Name:      resource.Metadata.Name,
		Identity:  spec.Identity,
		Peers:     spec.Peers,
		LeasePath: spec.LeasePath,
		TTL:       ttl,
	})
	if err != nil {
		return decision, resource.Metadata.Name, err
	}
	if statusStore, ok := store.(routerstate.ObjectStatusStore); ok {
		phase := "Standby"
		if decision.Leader {
			phase = "Leader"
		}
		_ = statusStore.SaveObjectStatus(api.SystemAPIVersion, "RouterdCluster", resource.Metadata.Name, map[string]any{
			"phase":      phase,
			"identity":   decision.Identity,
			"holder":     decision.Holder,
			"leasePath":  decision.LeasePath,
			"expiresAt":  decision.ExpiresAt.Format(time.RFC3339Nano),
			"reason":     decision.Reason,
			"observedAt": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	return decision, resource.Metadata.Name, nil
}

func applyClusterResource(router *api.Router) (api.Resource, api.RouterdClusterSpec, bool, error) {
	if router == nil {
		return api.Resource{}, api.RouterdClusterSpec{}, false, nil
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.SystemAPIVersion || resource.Kind != "RouterdCluster" {
			continue
		}
		spec, err := resource.RouterdClusterSpec()
		return resource, spec, true, err
	}
	return api.Resource{}, api.RouterdClusterSpec{}, false, nil
}
