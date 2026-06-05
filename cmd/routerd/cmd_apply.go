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
	nixosapply "github.com/imksoo/routerd/pkg/apply/nixos"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/eventlog"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/inventory"
	"github.com/imksoo/routerd/pkg/netconfigbackend"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
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
	_, err = runApplyOnce(router, opts, stdout, logger)
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

func routerWithIPv6PDClientOptions(router *api.Router, opts applyOptions, osName string, nixOS bool) (*api.Router, []string, error) {
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
			spec.Client = api.EffectiveIPv6PDClient(osName, nixOS, profile, spec.Client)
		}
		if !api.ValidIPv6PDClient(spec.Client) {
			return nil, nil, fmt.Errorf("%s spec.client is invalid: %q", res.ID(), spec.Client)
		}
		if !api.ValidIPv6PDProfile(spec.Profile) {
			return nil, nil, fmt.Errorf("%s spec.profile is invalid: %q", res.ID(), spec.Profile)
		}
		out.Spec.Resources[i].Spec = spec
		ctx := api.IPv6PDClientContext{OS: strings.ToLower(osName), NixOS: nixOS, Client: spec.Client, Profile: profile}
		for _, item := range api.MatchKnownIPv6PDNGCombinations(ctx) {
			warnings = append(warnings, fmt.Sprintf("%s uses known problematic DHCPv6PrefixDelegation combination os=%s nixos=%t client=%s profile=%s: %s See %s. Continuing because known problematic combinations are warnings, not validation errors.", res.ID(), strings.ToLower(osName), nixOS, spec.Client, profile, item.Reason, item.DocLink))
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
	if path := strings.TrimSpace(opts.ConfigPath); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
	}
	data, _ := yaml.Marshal(router)
	return string(data)
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
	Backup(path string) error
}

type staleObjectStatusCleanupResult struct {
	Removed    []routerstate.ObjectStatus
	BackupPath string
	Skipped    bool
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
	removed := unsupportedLegacyObjectStatuses(statuses)
	if len(removed) == 0 {
		return staleObjectStatusCleanupResult{}, nil
	}
	backupPath := staleStateCleanupBackupPath(statePath, now)
	if err := store.Backup(backupPath); err != nil {
		msg := fmt.Sprintf("stale state cleanup skipped: failed to create backup %s: %v", backupPath, err)
		emitStateCleanupWarning(logger, msg, map[string]string{"backup": backupPath, "error": err.Error(), "resources": staleObjectStatusIDs(removed)})
		recordStateCleanupEvent(router, store, "StaleStateCleanupSkipped", msg)
		return staleObjectStatusCleanupResult{Removed: removed, BackupPath: backupPath, Skipped: true}, nil
	}
	for _, status := range removed {
		if err := store.DeleteObject(status.APIVersion, status.Kind, status.Name); err != nil {
			msg := fmt.Sprintf("stale state cleanup stopped after backup %s: delete %s failed: %v", backupPath, objectStatusID(status), err)
			emitStateCleanupWarning(logger, msg, map[string]string{"backup": backupPath, "error": err.Error(), "resource": objectStatusID(status)})
			recordStateCleanupEvent(router, store, "StaleStateCleanupPartial", msg)
			return staleObjectStatusCleanupResult{Removed: removed, BackupPath: backupPath, Skipped: true}, err
		}
	}
	pruneStaleStateCleanupBackups(statePath, 5, logger)
	msg := fmt.Sprintf("removed %d stale unsupported resource status rows after backup %s", len(removed), backupPath)
	emitStateCleanupWarning(logger, msg, map[string]string{"backup": backupPath, "count": strconv.Itoa(len(removed)), "resources": staleObjectStatusIDs(removed)})
	recordStateCleanupEvent(router, store, "StaleStateCleanup", msg)
	return staleObjectStatusCleanupResult{Removed: removed, BackupPath: backupPath}, nil
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

func staleStateCleanupBackupPath(statePath string, now time.Time) string {
	base := filepath.Base(statePath)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "routerd.db"
	}
	return filepath.Join(filepath.Dir(statePath), fmt.Sprintf("%s.stale-cleanup.%s", base, now.UTC().Format("20060102T150405Z")))
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
			emitStateCleanupWarning(logger, "failed to prune stale state cleanup backup", map[string]string{"backup": path, "error": err.Error()})
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

func runApplyOnce(router *api.Router, opts applyOptions, stdout io.Writer, logger *eventlog.Logger) (*apply.Result, error) {
	var optionWarnings []string
	effectiveConfig, warnings, err := routerWithIPv6PDClientOptions(router, opts, string(platformDefaults.OS), isNixOSHost())
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
	stateStore, err := loadApplyStateStore(defaultString(opts.StatePath, defaultStatePath), opts.DryRun)
	if err != nil {
		return nil, err
	}
	var generation int64
	generationFinished := false
	if !opts.DryRun {
		if store, ok := stateStore.(routerstate.GenerationStore); ok {
			generation, err = store.BeginGeneration(routerConfigHash(router))
			if err != nil {
				return nil, err
			}
			if recorder, ok := stateStore.(routerstate.GenerationConfigRecorder); ok {
				if err := recorder.RecordGenerationConfig(generation, routerConfigYAML(router, opts)); err != nil {
					return nil, err
				}
			}
			defer func() {
				if generation != 0 && !generationFinished {
					_ = store.FinishGeneration(generation, "Errored", nil)
				}
			}()
		}
	}
	if !opts.DryRun {
		if err := recordHostInventoryState(stateStore); err != nil {
			return nil, err
		}
	}
	_, err = recordObservedPrefixDelegationState(router, stateStore)
	if err != nil {
		return nil, err
	}
	effectiveRouter := filterRouterByWhen(router, stateStore)
	engine := apply.New()
	result, err := engine.Plan(effectiveRouter)
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
		decision, clusterName, err := acquireApplyClusterLease(context.Background(), effectiveRouter, stateStore)
		if err != nil {
			return nil, err
		}
		if decision.Enabled && decision.Lease != nil {
			defer decision.Lease.Close()
		}
		if decision.Enabled && !decision.Leader {
			result.Phase = "Standby"
			result.Warnings = append(result.Warnings, fmt.Sprintf("RouterdCluster/%s lease is held by %s; apply skipped on standby", clusterName, decision.Holder))
			if err := stateStore.Save(defaultString(opts.StatePath, defaultStatePath)); err != nil {
				return nil, err
			}
			if store, ok := stateStore.(routerstate.GenerationStore); ok && generation != 0 {
				_ = store.FinishGeneration(generation, result.Phase, result.Warnings)
				generationFinished = true
			}
			logger.Emit(eventlog.LevelInfo, "apply", "routerd apply skipped on standby", map[string]string{"cluster": clusterName, "holder": decision.Holder})
			return result, nil
		}
	}
	if !opts.DryRun {
		recordWarningEvents(router, stateStore, result.Warnings)
		recordKnownNGCombinationEvents(router, stateStore, optionWarnings)
		if err := os.MkdirAll(filepathDir(defaultString(opts.StatePath, defaultStatePath)), 0755); err != nil {
			return nil, err
		}
		if err := stateStore.Save(defaultString(opts.StatePath, defaultStatePath)); err != nil {
			return nil, err
		}
		logger.Emit(eventlog.LevelInfo, "apply", "routerd plan completed", map[string]string{
			"phase":     result.Phase,
			"resources": fmt.Sprintf("%d", len(result.Resources)),
		})
		if platformDefaults.OS == platform.OSFreeBSD {
			next, err := runFreeBSDApplyOnce(effectiveRouter, opts, stdout, logger, engine, result, generation, stateStore)
			if store, ok := stateStore.(routerstate.GenerationStore); ok && generation != 0 && next != nil {
				_ = store.FinishGeneration(generation, next.Phase, next.Warnings)
				generationFinished = true
			}
			return next, err
		}
		policy := effectiveApplyPolicy(effectiveRouter)
		var applyErrors []string
		recordStageError := func(stage string, err error) error {
			if err == nil {
				return nil
			}
			msg := fmt.Sprintf("%s: %v", stage, err)
			result.Warnings = append(result.Warnings, msg)
			applyErrors = append(applyErrors, msg)
			logger.Emit(eventlog.LevelError, "apply", "routerd apply stage failed", map[string]string{"stage": stage, "error": err.Error()})
			if policy.Mode != "progressive" {
				return fmt.Errorf("%s: %w", stage, err)
			}
			return nil
		}

		var appliedHostPackages []string
		if err := recordStageError("packages", func() error {
			var err error
			appliedHostPackages, err = applyLinuxPackages(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var nixOSChangedFiles []string
		if isNixOSHost() {
			if err := recordStageError("nixos-rebuild", func() error {
				nixResult, err := nixosapply.Apply(context.Background(), effectiveRouter, nixosapply.Options{Mode: "switch"})
				nixOSChangedFiles = append(nixOSChangedFiles, nixResult.ChangedFiles...)
				if err == nil {
					logger.Emit(eventlog.LevelInfo, "apply", "applied NixOS module", map[string]string{
						"mode":             nixResult.Mode,
						"module":           nixResult.ModulePath,
						"generationBefore": nixResult.GenerationBefore,
						"generationAfter":  nixResult.GenerationAfter,
					})
				}
				return err
			}()); err != nil {
				return nil, err
			}
		}

		var systemdUnitChangedFiles []string
		if !opts.SkipServiceManager {
			if err := recordStageError("service-manager", func() error {
				var err error
				if platformFeatures.HasOpenRC {
					systemdUnitChangedFiles, err = applyOpenRCServiceResources(effectiveRouter)
				} else {
					systemdUnitChangedFiles, err = applySystemdUnitResources(effectiveRouter)
				}
				return err
			}()); err != nil {
				return nil, err
			}
		}

		var vrrpChangedFiles []string
		var vrrpChanged bool
		if err := recordStageError("vrrp", func() error {
			var err error
			vrrpChangedFiles, vrrpChanged, err = applyVRRPArtifactsOnce(effectiveRouter, stateStore)
			return err
		}()); err != nil {
			return nil, err
		}

		var bgpChangedFiles []string
		var bgpChanged bool
		if err := recordStageError("bgp", func() error {
			var err error
			bgpChangedFiles, bgpChanged, err = applyBGPArtifactsOnce(effectiveRouter, stateStore)
			return err
		}()); err != nil {
			return nil, err
		}

		var networkChangedFiles []string
		if err := recordStageError("network", func() error {
			if isNixOSHost() {
				logger.Emit(eventlog.LevelInfo, "apply", "skipping direct network apply on NixOS; nixos-rebuild owns activation", map[string]string{"stage": "network"})
				return nil
			}
			if !platformFeatures.HasNetplan && !platformFeatures.HasSystemdNetworkd {
				var err error
				networkChangedFiles, err = applyRuntimeLinuxNetworkResources(effectiveRouter)
				return err
			}
			netplanFiles, err := netconfigbackend.Netplan{Path: opts.NetplanPath}.Render(effectiveRouter)
			if err != nil {
				return err
			}
			var netplanData []byte
			if len(netplanFiles) > 0 {
				netplanData = netplanFiles[0].Data
			}
			networkdFiles, err := netconfigbackend.Networkd{}.Render(effectiveRouter)
			if err != nil {
				return err
			}
			networkChangedFiles, err = applyNetworkConfig(opts.NetplanPath, netplanData, networkdFiles)
			return err
		}()); err != nil {
			return nil, err
		}

		var nftablesChangedFiles []string
		if err := recordStageError("nftables", func() error {
			nftablesConfig, err := render.NftablesNAT44(effectiveRouter)
			if err != nil {
				return err
			}
			nftablesChangedFiles, err = applyNftablesConfig(opts.NftablesPath, nftablesConfig)
			return err
		}()); err != nil {
			return nil, err
		}

		var appliedIPv6DelegatedAddresses []string
		if err := recordStageError("ipv6-delegated-address", func() error {
			var err error
			appliedIPv6DelegatedAddresses, err = applyIPv6DelegatedAddressesWithState(effectiveRouter, stateStore)
			return err
		}()); err != nil {
			return nil, err
		}

		var dnsmasqChangedFiles []string
		if err := recordStageError("dnsmasq", func() error {
			dnsmasqConfig, dnsmasqWarnings, err := render.DnsmasqConfig(effectiveRouter, render.DnsmasqRuntime{
				DHCPv4DNSServersByInterface: observedDNSServersByInterface(effectiveRouter),
				DHCPv6DNSServersByInterface: observedDNSServersByInterface(effectiveRouter),
				IPv6AddressesByInterface:    observedIPv6AddressesByInterface(effectiveRouter),
				IPv6PrefixesByInterface:     observedIPv6PrefixesByInterface(effectiveRouter),
				StickyHosts:                 dhcpStickyHostsFromLog(effectiveRouter, time.Now().UTC()),
				RuntimeDir:                  platformDefaults.RuntimeDir,
				LeaseFile:                   dnsmasqLeaseFileForPlatform(),
			})
			if err != nil {
				return err
			}
			for _, w := range dnsmasqWarnings {
				result.Warnings = append(result.Warnings, w)
				logger.Emit(eventlog.LevelWarning, "apply", w, map[string]string{"stage": "dnsmasq"})
			}
			dnsmasqChangedFiles, err = applyDnsmasqConfig(opts.DnsmasqConfigPath, opts.DnsmasqServicePath, dnsmasqConfig)
			return err
		}()); err != nil {
			return nil, err
		}

		var pppoeChangedFiles []string
		if err := recordStageError("pppoe", func() error {
			var err error
			pppoeChangedFiles, err = applyPPPoEConfig(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var timesyncdChangedFiles []string
		if err := recordStageError("timesyncd", func() error {
			if !platformFeatures.HasSystemdTimesyncd {
				return nil
			}
			timesyncdConfig, err := render.TimesyncdConfig(effectiveRouter)
			if err != nil {
				return err
			}
			if isNixOSHost() {
				timesyncdConfig = nil
			}
			timesyncdChangedFiles, err = applyTimesyncdConfig(defaultTimesyncdPath, timesyncdConfig)
			return err
		}()); err != nil {
			return nil, err
		}

		var cleanedPreDSLiteOrphans []string

		var appliedTunnels []string
		if err := recordStageError("ds-lite", func() error {
			var err error
			appliedTunnels, err = applyDSLiteTunnelsWithState(effectiveRouter, stateStore)
			return err
		}()); err != nil {
			return nil, err
		}
		if err := recordStageError("ds-lite-cleanup", func() error {
			var err error
			cleanedPreDSLiteOrphans, err = cleanupStaleDSLiteTunnels(effectiveRouter)
			cleanedAliases, aliasErr := cleanupStaleDSLiteIPv4Aliases(effectiveRouter)
			cleanedPreDSLiteOrphans = append(cleanedPreDSLiteOrphans, cleanedAliases...)
			if err != nil {
				return err
			}
			return aliasErr
		}()); err != nil {
			return nil, err
		}

		var cleanedDelegatedIPv6Addresses []string

		var appliedPolicyRoutes []string
		if err := recordStageError("ipv4-policy-routes", func() error {
			var err error
			appliedPolicyRoutes, err = applyIPv4PolicyRoutes(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var appliedDefaultRoutes []string
		if err := recordStageError("ipv4-default-route-policy", func() error {
			var err error
			appliedDefaultRoutes, err = applyIPv4DefaultRoutePolicies(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var cleanedPolicyRules []string
		if platformFeatures.HasIproute2 {
			if err := recordStageError("ipv4-policy-route-cleanup", func() error {
				var err error
				cleanedPolicyRules, err = cleanupIPv4ManagedFwmarkRules(effectiveRouter)
				return err
			}()); err != nil {
				return nil, err
			}
		}

		var appliedRuntime []string
		if err := recordStageError("sysctl", func() error {
			var err error
			appliedRuntime, err = applyRuntimeSysctls(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var appliedHostnames []string
		if err := recordStageError("hostname", func() error {
			var err error
			appliedHostnames, err = applyHostnames(effectiveRouter)
			return err
		}()); err != nil {
			return nil, err
		}

		var cleanedLedgerOrphans []string
		var rememberedArtifacts int
		if len(applyErrors) == 0 {
			var err error
			cleanedLedgerOrphans, err = cleanupLedgerOwnedOrphans(effectiveRouter, opts.LedgerPath)
			if err != nil {
				return nil, err
			}
			rememberedArtifacts, err = rememberAppliedArtifacts(effectiveRouter, opts.LedgerPath, generation)
			if err != nil {
				return nil, err
			}
			if err := recordLastAppliedPath(effectiveRouter, stateStore, opts.ConfigPath); err != nil {
				return nil, err
			}
		} else {
			result.Warnings = append(result.Warnings, "skipped ledger orphan cleanup and ownership recording because apply completed with stage errors")
		}
		changedFiles := append(nixOSChangedFiles, networkChangedFiles...)
		changedFiles = append(changedFiles, systemdUnitChangedFiles...)
		changedFiles = append(changedFiles, vrrpChangedFiles...)
		changedFiles = append(changedFiles, bgpChangedFiles...)
		changedFiles = append(changedFiles, dnsmasqChangedFiles...)
		changedFiles = append(changedFiles, nftablesChangedFiles...)
		changedFiles = append(changedFiles, pppoeChangedFiles...)
		changedFiles = append(changedFiles, timesyncdChangedFiles...)
		if len(changedFiles) == 0 {
			if len(appliedRuntime) == 0 && !vrrpChanged && !bgpChanged {
				fmt.Fprintln(stdout, "network configuration already up to date")
			}
		} else {
			for _, path := range changedFiles {
				fmt.Fprintf(stdout, "wrote %s\n", path)
			}
			if len(networkChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied network configuration")
			}
			if len(dnsmasqChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied dnsmasq")
			}
			if len(nftablesChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied nftables")
			}
			if len(pppoeChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied PPPoE")
			}
			if len(timesyncdChangedFiles) > 0 {
				fmt.Fprintln(stdout, "applied NTP client")
			}
			if len(vrrpChangedFiles) > 0 {
				fmt.Fprintln(stdout, "rendered VRRP artifacts")
			}
			if bgpChanged {
				fmt.Fprintln(stdout, "rendered BGP artifacts")
			}
		}
		if len(changedFiles) == 0 && vrrpChanged {
			fmt.Fprintln(stdout, "rendered VRRP artifacts")
		}
		if len(changedFiles) == 0 && bgpChanged {
			fmt.Fprintln(stdout, "rendered BGP artifacts")
		}
		for _, key := range appliedRuntime {
			fmt.Fprintf(stdout, "applied sysctl %s\n", key)
		}
		for _, pkg := range appliedHostPackages {
			fmt.Fprintf(stdout, "applied package %s\n", pkg)
		}
		for _, hostname := range appliedHostnames {
			fmt.Fprintf(stdout, "applied hostname %s\n", hostname)
		}
		for _, address := range appliedIPv6DelegatedAddresses {
			fmt.Fprintf(stdout, "applied IPv6 delegated address %s\n", address)
		}
		for _, tunnel := range appliedTunnels {
			fmt.Fprintf(stdout, "applied DS-Lite tunnel %s\n", tunnel)
		}
		for _, address := range cleanedDelegatedIPv6Addresses {
			fmt.Fprintf(stdout, "removed stale delegated IPv6 address %s\n", address)
		}
		for _, route := range appliedDefaultRoutes {
			fmt.Fprintf(stdout, "applied IPv4 default route %s\n", route)
		}
		for _, route := range appliedPolicyRoutes {
			fmt.Fprintf(stdout, "applied IPv4 policy route %s\n", route)
		}
		for _, rule := range cleanedPolicyRules {
			fmt.Fprintf(stdout, "removed stale IPv4 policy rule %s\n", rule)
		}
		for _, artifact := range cleanedPreDSLiteOrphans {
			fmt.Fprintf(stdout, "removed orphaned owned artifact %s\n", artifact)
		}
		for _, artifact := range cleanedLedgerOrphans {
			fmt.Fprintf(stdout, "removed orphaned owned artifact %s\n", artifact)
		}
		if rememberedArtifacts > 0 {
			fmt.Fprintf(stdout, "remembered %d owned artifacts\n", rememberedArtifacts)
		}
		logger.Emit(eventlog.LevelInfo, "apply", "routerd changes applied", map[string]string{
			"changedFiles":        fmt.Sprintf("%d", len(changedFiles)),
			"runtimeSysctls":      fmt.Sprintf("%d", len(appliedRuntime)),
			"hostnames":           fmt.Sprintf("%d", len(appliedHostnames)),
			"ipv6DelegatedAddrs":  fmt.Sprintf("%d", len(appliedIPv6DelegatedAddresses)),
			"pppoeFiles":          fmt.Sprintf("%d", len(pppoeChangedFiles)),
			"ntpFiles":            fmt.Sprintf("%d", len(timesyncdChangedFiles)),
			"dsliteTunnels":       fmt.Sprintf("%d", len(appliedTunnels)),
			"ipv4DefaultRoutes":   fmt.Sprintf("%d", len(appliedDefaultRoutes)),
			"egressPolicyRoutes":  fmt.Sprintf("%d", len(appliedPolicyRoutes)),
			"ipv4PolicyRulesGone": fmt.Sprintf("%d", len(cleanedPolicyRules)),
			"bgpFiles":            fmt.Sprintf("%d", len(bgpChangedFiles)),
			"ownedOrphansGone":    fmt.Sprintf("%d", len(cleanedPreDSLiteOrphans)+len(cleanedLedgerOrphans)),
			"rememberedArtifacts": fmt.Sprintf("%d", rememberedArtifacts),
		})
		applyWarnings := append([]string{}, result.Warnings...)
		result, err = engine.Plan(effectiveRouter)
		if err != nil {
			return nil, err
		}
		if generation != 0 {
			result.Generation = generation
		}
		result.Warnings = append(result.Warnings, applyWarnings...)
		if len(applyErrors) > 0 {
			result.Phase = "Degraded"
		}
		if err := appendLedgerOwnedOrphans(result, effectiveRouter, opts.LedgerPath, false); err != nil {
			return nil, err
		}
		if err := writeResult(stdout, opts.StatusFile, result); err != nil {
			return nil, err
		}
		if store, ok := stateStore.(routerstate.GenerationStore); ok && generation != 0 {
			_ = store.FinishGeneration(generation, result.Phase, result.Warnings)
			generationFinished = true
		}
		return result, nil
	}
	if opts.AnnounceDryRunToCLI {
		fmt.Fprintf(stdout, "dry-run apply plan for %s\n", opts.ConfigPath)
	}
	if isNixOSHost() {
		nixResult, err := nixosapply.Apply(context.Background(), effectiveRouter, nixosapply.Options{Mode: "test"})
		if err != nil {
			return nil, err
		}
		for _, path := range nixResult.ChangedFiles {
			fmt.Fprintf(stdout, "wrote %s\n", path)
		}
		fmt.Fprintln(stdout, "tested NixOS module with nixos-rebuild test")
		logger.Emit(eventlog.LevelInfo, "apply", "tested NixOS module", map[string]string{
			"mode":             nixResult.Mode,
			"module":           nixResult.ModulePath,
			"generationBefore": nixResult.GenerationBefore,
			"generationAfter":  nixResult.GenerationAfter,
		})
	}
	logger.Emit(eventlog.LevelInfo, "apply", "routerd dry-run completed", map[string]string{
		"phase":     result.Phase,
		"resources": fmt.Sprintf("%d", len(result.Resources)),
	})
	if err := writeResult(stdout, opts.StatusFile, result); err != nil {
		return nil, err
	}
	if store, ok := stateStore.(routerstate.GenerationStore); ok && generation != 0 {
		_ = store.FinishGeneration(generation, result.Phase, result.Warnings)
		generationFinished = true
	}
	return result, nil
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

func loadApplyStateStore(path string, dryRun bool) (routerstate.Store, error) {
	if dryRun {
		return loadTransientStateStore(path)
	}
	return routerstate.Load(path)
}

func loadTransientStateStore(path string) (routerstate.Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return routerstate.New(), nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return routerstate.New(), nil
	} else if err != nil {
		return nil, err
	}
	var variables map[string]routerstate.Value
	if filepath.Ext(path) == ".json" {
		store, err := routerstate.LoadJSON(path)
		if err != nil {
			return nil, err
		}
		variables = store.Variables()
	} else {
		store, err := routerstate.OpenSQLiteReadOnlyImmutable(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = store.Close() }()
		variables = store.Variables()
	}
	snapshot := routerstate.NewJSON()
	snapshot.Values = variables
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
