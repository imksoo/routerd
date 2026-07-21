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
	"time"

	"gopkg.in/yaml.v3"

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
	ConfigPath          string
	StatusFile          string
	NetplanPath         string
	DnsmasqConfigPath   string
	DnsmasqServicePath  string
	NftablesPath        string
	LedgerPath          string
	StatePath           string
	OverrideClient      string
	OverrideProfile     string
	DryRun              bool
	SkipServiceManager  bool
	AllowMgmtLockout    bool
	AnnounceDryRunToCLI bool
	MgmtLockoutWriter   io.Writer
	SkipConfigCommit    bool
	ConfigYAMLOverride  string
	Sandbox             bool
}

func effectiveApplyPolicy(router *api.Router) api.ApplyPolicySpec {
	policy := router.Spec.Apply
	if policy.Mode == "" {
		policy.Mode = "strict"
	}
	policy.ProtectedInterfaces = compactStringList(policy.ProtectedInterfaces)
	policy.ProtectedZones = compactStringList(policy.ProtectedZones)
	return policy
}

func routerWithIPv6PDClientOptions(router *api.Router, opts applyOptions, osName string) (*api.Router, []string, error) {
	if router == nil {
		return nil, nil, errors.New("router is nil")
	}
	if !api.ValidIPv6PDClient(opts.OverrideClient) {
		return nil, nil, fmt.Errorf("invalid DHCPv6PrefixDelegation client override %q", opts.OverrideClient)
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
			return nil, nil, fmt.Errorf("%s spec.client is invalid: %q", res.ID(), spec.Client)
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

func routerConfigHash(router *api.Router) string {
	data, _ := json.Marshal(router)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func routerConfigYAML(router *api.Router, opts applyOptions) string {
	if strings.TrimSpace(opts.ConfigYAMLOverride) != "" {
		return opts.ConfigYAMLOverride
	}
	if path := strings.TrimSpace(opts.ConfigPath); path != "" {
		if data, err := config.CanonicalYAMLFile(path); err == nil {
			return string(data)
		}
	}
	data, _ := yaml.Marshal(router)
	return string(data)
}

func commitConfigAfterSuccessfulApply(opts applyOptions, configYAML string, logger *eventlog.Logger) error {
	if opts.DryRun || opts.SkipConfigCommit || strings.TrimSpace(opts.ConfigPath) == "" {
		return nil
	}
	if err := config.AtomicWriteFile(opts.ConfigPath, []byte(configYAML)); err != nil {
		return fmt.Errorf("commit canonical config %s: %w", opts.ConfigPath, err)
	}
	if logger != nil {
		logger.Emit(eventlog.LevelInfo, "apply", "committed canonical router config", map[string]string{"config": opts.ConfigPath})
	}
	return nil
}

func configCommitPhase(phase string) bool {
	switch strings.TrimSpace(phase) {
	case "Healthy", "Applied":
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

func recordHostInventoryState(store routerstate.Store) error {
	objectStore, ok := store.(routerstate.ObjectStatusStore)
	if !ok {
		return nil
	}
	status := inventoryStatusMap(inventory.Collect())
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
	if store == nil {
		return staleObjectStatusCleanupResult{}, nil
	}
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		emitStateCleanupWarning(logger, "stale state cleanup skipped: list object statuses failed", map[string]string{"error": err.Error()})
		recordStateCleanupEvent(router, store, "StaleStateCleanupSkipped", "stale state cleanup skipped: "+err.Error())
		return staleObjectStatusCleanupResult{Skipped: true}, err
	}
	desired, err := desiredObjectStatusKeys(router, store, now)
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

func staleObjectStatuses(router *api.Router, statuses []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	return staleObjectStatusesForDesired(configuredResourceStatusKeys(router), statuses)
}

func staleObjectStatusesForDesired(desired map[string]bool, statuses []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	return lifecycle.PlanStatusGC(desired, statuses).StatusDeletes
}

func syntheticObjectStatus(status routerstate.ObjectStatus) bool {
	return lifecycle.SyntheticObjectStatus(status)
}

func desiredObjectStatusKeys(router *api.Router, store resourcequery.StateStore, now time.Time) (map[string]bool, error) {
	out := map[string]bool{}
	if router == nil {
		return out, nil
	}
	effective := resourcequery.FilterRouterByWhen(router, store)
	routers, err := controllerchain.BuildDynamicRouteSAMObjectStatusRouters(effective, store, now.UTC(), platform.CurrentOS())
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
		if !resourcequery.ResourceWhenPresent(when) || resourcequery.ResourceWhenMatches(when, store) {
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

func configResourceObjectStatus(status routerstate.ObjectStatus) bool {
	return lifecycle.ConfigResourceObjectStatus(status)
}

func unsupportedLegacyObjectStatuses(statuses []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	var out []routerstate.ObjectStatus
	for _, status := range statuses {
		if api.IsRemovedLegacyKind(status.Kind) {
			out = append(out, status)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return objectStatusID(out[i]) < objectStatusID(out[j])
	})
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
	var optionWarnings []string
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
	configYAML := routerConfigYAML(router, opts)
	if opts.DryRun && !opts.Sandbox {
		dryRunDir, err := os.MkdirTemp("", "routerd-apply-dryrun-artifacts-*")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(dryRunDir) }()
		opts.DnsmasqConfigPath = filepath.Join(dryRunDir, "dnsmasq.conf")
		opts.NftablesPath = filepath.Join(dryRunDir, "nat44.nft")
	}

	statePath := defaultString(opts.StatePath, defaultStatePath)
	stateStore, cleanup, err := openApplyChainStateStore(statePath, opts.DryRun)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	effectiveRouter := filterRouterByWhen(router, stateStore)
	var generation int64
	generationFinished := false
	if !opts.DryRun {
		generation, err = stateStore.BeginGeneration(routerConfigHash(router))
		if err != nil {
			return nil, err
		}
		if err := stateStore.RecordGenerationConfig(generation, configYAML); err != nil {
			return nil, err
		}
		defer func() {
			if generation != 0 && !generationFinished {
				_ = stateStore.FinishGeneration(generation, "Errored", nil)
			}
		}()
		if err := recordHostInventoryState(stateStore); err != nil {
			return nil, err
		}
	}
	_, err = recordObservedPrefixDelegationState(router, stateStore)
	if err != nil {
		return nil, err
	}
	result, err := apply.New().Plan(effectiveRouter)
	if err != nil {
		return nil, err
	}
	if generation != 0 {
		result.Generation = generation
	}
	result.Warnings = append(result.Warnings, optionWarnings...)
	appendPrefixDelegationStateWarnings(result, router, stateStore)
	if err := appendLedgerOwnedOrphans(result, effectiveRouter, opts.LedgerPath, opts.DryRun); err != nil {
		return nil, err
	}
	if !opts.DryRun {
		decision, clusterName, err := acquireApplyClusterLease(ctx, effectiveRouter, stateStore)
		if err != nil {
			return nil, err
		}
		if decision.Enabled && decision.Lease != nil {
			defer decision.Lease.Close()
		}
		if decision.Enabled && !decision.Leader {
			result.Phase = "Standby"
			result.Warnings = append(result.Warnings, fmt.Sprintf("RouterdCluster/%s lease is held by %s; apply skipped on standby", clusterName, decision.Holder))
			if err := writeResult(stdout, opts.StatusFile, result); err != nil {
				return nil, err
			}
			if generation != 0 {
				_ = stateStore.FinishGeneration(generation, result.Phase, result.Warnings)
				generationFinished = true
			}
			if logger != nil {
				logger.Emit(eventlog.LevelInfo, "apply", "routerd apply skipped on standby", map[string]string{"cluster": clusterName, "holder": decision.Holder})
			}
			return result, nil
		}
		recordWarningEvents(router, stateStore, result.Warnings)
		recordKnownNGCombinationEvents(router, stateStore, optionWarnings)
	}

	eventBus := bus.New()
	if !opts.DryRun {
		eventBus = bus.NewWithStore(stateStore)
	}
	eventBus.SetLogger(slog.Default())
	controllerOpts := applyChainControllerOptions(opts)
	runner := &controllerchain.Runner{
		Router: router,
		Bus:    eventBus,
		Store:  stateStore,
		Opts:   controllerOpts,
	}
	if err := runner.ReconcileOnce(ctx); err != nil {
		return nil, err
	}
	if !opts.DryRun {
		if _, err := applyIPsecConnections(ctx, effectiveRouter); err != nil {
			return nil, err
		}
	}
	if !opts.DryRun {
		if err := recordLastAppliedPath(effectiveRouter, stateStore, opts.ConfigPath); err != nil {
			return nil, err
		}
	}
	applyWarnings := append([]string{}, result.Warnings...)
	result, err = apply.New().Observe(effectiveRouter)
	if err != nil {
		return nil, err
	}
	if generation != 0 {
		result.Generation = generation
	}
	result.Warnings = append(result.Warnings, applyWarnings...)
	if err := appendLedgerOwnedOrphans(result, effectiveRouter, opts.LedgerPath, opts.DryRun); err != nil {
		return nil, err
	}
	if !opts.DryRun && configCommitPhase(result.Phase) {
		if err := commitConfigAfterSuccessfulApply(opts, configYAML, logger); err != nil {
			return result, err
		}
	}
	if opts.DryRun && opts.AnnounceDryRunToCLI {
		fmt.Fprintf(stdout, "dry-run apply plan for %s\n", opts.ConfigPath)
	}
	if err := writeResult(stdout, opts.StatusFile, result); err != nil {
		return nil, err
	}
	if !opts.DryRun && generation != 0 {
		_ = stateStore.FinishGeneration(generation, result.Phase, result.Warnings)
		generationFinished = true
	}
	if logger != nil {
		logger.Emit(eventlog.LevelInfo, "apply", "routerd apply chain once completed", map[string]string{
			"phase":      result.Phase,
			"generation": strconv.FormatInt(result.Generation, 10),
			"dryRun":     fmt.Sprintf("%t", opts.DryRun),
		})
	}
	return result, nil
}

func applyChainControllerOptions(opts applyOptions) controllerchain.Options {
	controllerOpts := controllerchain.Options{
		SuperviseClientDaemons: false,
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
	}
	if opts.DryRun {
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

func openApplyChainStateStore(path string, dryRun bool) (*routerstate.SQLiteStore, func(), error) {
	path = defaultString(path, defaultStatePath)
	if !dryRun {
		store, err := routerstate.OpenSQLite(path)
		if err != nil {
			return nil, nil, err
		}
		return store, func() { _ = store.Close() }, nil
	}
	dir, err := os.MkdirTemp("", "routerd-apply-dryrun-*")
	if err != nil {
		return nil, nil, err
	}
	cleanupDir := func() { _ = os.RemoveAll(dir) }
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "routerd.db"))
	if err != nil {
		cleanupDir()
		return nil, nil, err
	}
	cleanup := func() {
		_ = store.Close()
		cleanupDir()
	}
	if err := copyApplyStateSnapshot(path, store); err != nil {
		cleanup()
		return nil, nil, err
	}
	return store, cleanup, nil
}

func copyApplyStateSnapshot(path string, dst *routerstate.SQLiteStore) error {
	src, err := routerstate.LoadReadOnlyLive(path)
	if err != nil {
		return err
	}
	if closer, ok := src.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}
	for key, value := range src.Variables() {
		switch value.Status {
		case routerstate.StatusSet:
			dst.Set(key, value.Value, value.Reason)
		case routerstate.StatusUnset:
			dst.Unset(key, value.Reason)
		default:
			dst.Forget(key, value.Reason)
		}
	}
	lister, ok := src.(routerstate.ObjectStatusLister)
	if !ok {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	for _, status := range statuses {
		if err := dst.SaveObjectStatus(status.APIVersion, status.Kind, status.Name, status.Status); err != nil {
			return err
		}
	}
	return nil
}

func inventoryStatusMap(status inventory.Status) map[string]any {
	data, _ := json.Marshal(status)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

func compactStringList(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
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

func loadTransientStateStore(path string) (routerstate.Store, error) {
	store, err := routerstate.LoadReadOnly(path)
	if err != nil {
		return nil, err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}
	snapshot := routerstate.NewJSON()
	snapshot.Values = store.Variables()
	return snapshot, nil
}

func cleanupLegacyFreeBSDStateDir() ([]string, error) {
	if platformDefaults.OS != platform.OSFreeBSD {
		return nil, nil
	}
	legacy := filepath.Clean(legacyFreeBSDStateDir)
	current := filepath.Clean(platformDefaults.StateDir)
	if legacy == "" || current == "" || legacy == current {
		return nil, nil
	}
	info, err := os.Stat(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat legacy FreeBSD state dir %s: %w", legacy, err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	if err := os.MkdirAll(current, 0755); err != nil {
		return nil, fmt.Errorf("create FreeBSD state dir %s: %w", current, err)
	}
	destination := filepath.Join(current, "legacy-var-lib-routerd-"+time.Now().UTC().Format("20060102T150405Z"))
	for suffix := 0; ; suffix++ {
		candidate := destination
		if suffix > 0 {
			candidate = fmt.Sprintf("%s-%d", destination, suffix)
		}
		err := os.Rename(legacy, candidate)
		if err == nil {
			return []string{legacy + " -> " + candidate}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("move legacy FreeBSD state dir %s to %s: %w", legacy, candidate, err)
		}
	}
}
