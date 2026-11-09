// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/conntracktuning"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/hostdeps"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/logstore"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/sysctlprofile"
)

type outputCommandFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

type SysctlController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store   Store
	BaseDir string
	DryRun  bool
}

func (c SysctlController) Reconcile(ctx context.Context) error {
	desired := map[string]bool{}
	var errs []error
	for _, resource := range hostdeps.SysctlResources(c.Router) {
		desired[resource.Kind+"/"+resource.Metadata.Name] = true
		switch resource.Kind {
		case "Sysctl":
			spec, err := resource.SysctlSpec()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			result, err := c.applyOne(ctx, resource.Metadata.Name, "Sysctl", sysctlSetting{Key: spec.Key, Value: spec.Value, ExpectedValue: spec.ExpectedValue, Compare: spec.Compare, Runtime: spec.Runtime, Persistent: spec.Persistent, Optional: spec.Optional})
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if result.changed && !c.DryRun && c.Bus != nil {
				if err := c.publishApplied(ctx, "Sysctl", resource.Metadata.Name, spec.Key, spec.Value); err != nil {
					errs = append(errs, err)
				}
			}
		case "SysctlProfile":
			spec, err := resource.SysctlProfileSpec()
			if err != nil {
				errs = append(errs, err)
				continue
			}
			settings, err := sysctlProfileSettings(spec)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			var applied, skipped, changed, failed []string
			requiredSkipped := false
			for _, setting := range settings {
				result, err := c.applyOne(ctx, resource.Metadata.Name+"-"+sysctlprofile.SafeName(setting.Key), "SysctlProfile", setting)
				if err != nil {
					errs = append(errs, err)
					failed = append(failed, setting.Key)
					continue
				}
				if result.skipped {
					skipped = append(skipped, setting.Key)
					requiredSkipped = requiredSkipped || !result.optionalAbsent
					continue
				}
				applied = append(applied, setting.Key)
				if result.changed {
					changed = append(changed, setting.Key)
				}
			}
			phase := "Applied"
			if requiredSkipped || len(failed) > 0 {
				phase = "Degraded"
			}
			if c.DryRun && len(changed) > 0 && phase == "Applied" {
				phase = "Rendered"
			}
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SysctlProfile", resource.Metadata.Name, map[string]any{
				"phase":      phase,
				"profile":    spec.Profile,
				"applied":    applied,
				"changed":    changed,
				"skipped":    skipped,
				"failed":     failed,
				"runtime":    api.BoolDefault(spec.Runtime, true),
				"persistent": spec.Persistent,
				"dryRun":     c.DryRun,
				"updatedAt":  time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				errs = append(errs, err)
				continue
			}
			if len(changed) > 0 && !c.DryRun && c.Bus != nil {
				if err := c.publishApplied(ctx, "SysctlProfile", resource.Metadata.Name, "profile", spec.Profile); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	if c.Router != nil && c.Router.Spec.Apply.AutoTuneConntrack {
		if err := c.applyConntrackTuning(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if err := c.cleanupRemovedSAMProxyARP(ctx, desired); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

type sysctlSetting struct {
	Key           string
	Value         string
	ExpectedValue string
	Compare       string
	Runtime       *bool
	Persistent    bool
	Optional      bool
}

type sysctlApplyResult struct {
	changed        bool
	skipped        bool
	optionalAbsent bool
}

func (c SysctlController) applyOne(ctx context.Context, name, kind string, spec sysctlSetting) (sysctlApplyResult, error) {
	runtime := api.BoolDefault(spec.Runtime, true)
	if !runtime && !spec.Persistent {
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, map[string]any{
			"phase":      "Skipped",
			"reason":     "RuntimeDisabled",
			"key":        spec.Key,
			"value":      spec.Value,
			"updatedAt":  time.Now().UTC().Format(time.RFC3339Nano),
			"persistent": spec.Persistent,
			"dryRun":     c.DryRun,
		}); err != nil {
			return sysctlApplyResult{}, err
		}
		return sysctlApplyResult{skipped: true}, nil
	}
	var currentOut []byte
	var currentErr error
	current := ""
	expected := normalizeSysctlRuntimeValue(firstNonEmpty(spec.ExpectedValue, spec.Value))
	runtimeChanged := false
	if runtime {
		currentOut, currentErr = runOutputCommandContext(ctx, "sysctl", "-n", spec.Key)
		current = normalizeSysctlRuntimeValue(string(currentOut))
		if currentErr != nil && spec.Optional {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, map[string]any{
				"phase":     "Skipped",
				"reason":    "ReadFailedOptional",
				"key":       spec.Key,
				"value":     spec.Value,
				"error":     currentErr.Error(),
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return sysctlApplyResult{}, err
			}
			var exitErr *exec.ExitError
			_, statErr := os.Stat("/proc/sys/" + strings.ReplaceAll(spec.Key, ".", "/"))
			_, procErr := os.Stat("/proc/sys")
			return sysctlApplyResult{skipped: true, optionalAbsent: errors.As(currentErr, &exitErr) && errors.Is(statErr, os.ErrNotExist) && procErr == nil}, nil
		}
		matches, compareErr := sysctlprofile.ValueMatches(current, expected, spec.Compare)
		runtimeChanged = currentErr != nil || compareErr != nil || !matches
		if runtimeChanged && !c.DryRun {
			if out, err := runOutputCommandContext(ctx, "sysctl", "-w", spec.Key+"="+spec.Value); err != nil {
				status := map[string]any{
					"phase":        "Error",
					"reason":       "ApplyFailed",
					"key":          spec.Key,
					"value":        spec.Value,
					"currentValue": current,
					"error":        strings.TrimSpace(string(out)),
					"updatedAt":    time.Now().UTC().Format(time.RFC3339Nano),
				}
				if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, status); saveErr != nil {
					return sysctlApplyResult{}, saveErr
				}
				return sysctlApplyResult{}, fmt.Errorf("apply sysctl %s: %w", spec.Key, err)
			}
			current = expected
		}
	}
	persistentPath := ""
	persistentChanged := false
	if spec.Persistent {
		baseDir := c.BaseDir
		if baseDir == "" {
			baseDir = "/etc/sysctl.d"
		}
		persistentPath = sysctlprofile.PersistentPath(baseDir, name)
		data := sysctlprofile.PersistentContents(spec.Key, spec.Value)
		var err error
		persistentChanged, err = writeFileIfChanged(persistentPath, data, 0644, c.DryRun)
		if err != nil {
			status := map[string]any{
				"phase":        "Error",
				"reason":       "PersistFailed",
				"key":          spec.Key,
				"value":        spec.Value,
				"currentValue": current,
				"persistent":   true,
				"path":         persistentPath,
				"error":        err.Error(),
				"updatedAt":    time.Now().UTC().Format(time.RFC3339Nano),
			}
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, status); saveErr != nil {
				return sysctlApplyResult{}, saveErr
			}
			return sysctlApplyResult{}, fmt.Errorf("persist sysctl %s: %w", spec.Key, err)
		}
	}
	changed := runtimeChanged || persistentChanged
	phase := "Applied"
	if c.DryRun && changed {
		phase = "Rendered"
	}
	status := map[string]any{
		"phase":         phase,
		"key":           spec.Key,
		"value":         spec.Value,
		"expectedValue": expected,
		"compare":       firstNonEmpty(spec.Compare, "exact"),
		"currentValue":  current,
		"changed":       changed,
		"runtime":       runtime,
		"persistent":    spec.Persistent,
		"optional":      spec.Optional,
		"path":          persistentPath,
		"dryRun":        c.DryRun,
		"updatedAt":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if runtimeChanged && currentErr == nil {
		status["previousValue"] = normalizeSysctlRuntimeValue(string(currentOut))
	}
	if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, status); err != nil {
		return sysctlApplyResult{}, err
	}
	return sysctlApplyResult{changed: changed}, nil
}

func (c SysctlController) cleanupRemovedSAMProxyARP(ctx context.Context, desired map[string]bool) error {
	if c.DryRun || c.Store == nil {
		return nil
	}
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return nil
	}
	deleter, ok := c.Store.(interface {
		DeleteObject(apiVersion, kind, name string) error
	})
	if !ok {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	desiredStatusIDs := map[string]bool{}
	for key := range desired {
		name := strings.TrimPrefix(key, "Sysctl/")
		if name != key && name != "" {
			desiredStatusIDs[lifecycle.ResourceKey(api.SystemAPIVersion, "Sysctl", name)] = true
		}
	}
	plan := lifecycle.PlanResourceTeardownGC(desiredStatusIDs, statuses)
	for _, action := range plan.Actions {
		if action.Type != lifecycle.GCActionTeardownResource {
			continue
		}
		status := action.Status
		if status.APIVersion != api.SystemAPIVersion || status.Kind != "Sysctl" {
			continue
		}
		if !strings.HasPrefix(status.Name, "sam-proxy-arp-") {
			continue
		}
		key := strings.TrimSpace(fmt.Sprint(status.Status["key"]))
		if err := deleter.DeleteObject(api.SystemAPIVersion, "Sysctl", status.Name); err != nil {
			return err
		}
		if c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.sysctl.removed", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "Sysctl", Name: status.Name}
			event.Attributes = map[string]string{"key": key}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c SysctlController) publishApplied(ctx context.Context, kind, name, key, value string) error {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.sysctl.applied", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: kind, Name: name}
	event.Attributes = map[string]string{"key": key, "value": value}
	return c.Bus.Publish(ctx, event)
}

func (c SysctlController) applyConntrackTuning(ctx context.Context) error {
	path := conntrackTuningFirewallLogPath(c.Router)
	if path == "" {
		if c.Store != nil {
			return c.Store.SaveObjectStatus(api.SystemAPIVersion, "ConntrackTuning", "default", map[string]any{
				"phase":     "Skipped",
				"reason":    "FirewallLogUnavailable",
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
		return nil
	}
	log, err := logstore.OpenFirewallLogReadOnly(path)
	if err != nil {
		if c.Store != nil {
			_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, "ConntrackTuning", "default", map[string]any{
				"phase":     "Degraded",
				"reason":    "FirewallLogOpenFailed",
				"error":     err.Error(),
				"path":      path,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			})
		}
		return nil
	}
	defer log.Close()
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)
	firewallLogs, err := log.List(ctx, logstore.FirewallLogFilter{Since: since, Limit: 1000})
	if err != nil {
		return err
	}
	dpiFlows, err := log.ListDPIFlows(ctx, logstore.DPIFlowFilter{Since: since, Limit: 5000})
	if err != nil {
		return err
	}
	expiredFlows, err := log.ListExpiredFlows(ctx, logstore.ExpiredFlowFilter{Since: since, Limit: 5000})
	if err != nil {
		return err
	}
	summary := conntracktuning.Analyze(conntracktuning.Inputs{DPIFlows: dpiFlows, FirewallLogs: firewallLogs, ExpiredFlows: expiredFlows, Now: now, Window: 24 * time.Hour, AutoApply: true})
	var applied, changed []string
	for _, row := range summary.Suggestions {
		if row.SysctlKey == "" || row.RecommendedSeconds <= 0 {
			continue
		}
		name := "conntrack-tuning-" + safeSysctlName(row.Protocol+"-"+row.Application)
		value := strconv.Itoa(row.RecommendedSeconds)
		result, err := c.applyOne(ctx, name, "ConntrackTuning", sysctlSetting{Key: row.SysctlKey, Value: value, Optional: true})
		if err != nil {
			return err
		}
		if result.skipped {
			continue
		}
		applied = append(applied, row.SysctlKey+"="+value)
		if result.changed {
			changed = append(changed, row.SysctlKey+"="+value)
		}
	}
	if c.Store != nil {
		phase := "Applied"
		if c.DryRun && len(changed) > 0 {
			phase = "Rendered"
		}
		return c.Store.SaveObjectStatus(api.SystemAPIVersion, "ConntrackTuning", "default", map[string]any{
			"phase":       phase,
			"applyMode":   summary.ApplyMode,
			"autoApply":   summary.AutoApply,
			"suggestions": len(summary.Suggestions),
			"applied":     applied,
			"changed":     changed,
			"dryRun":      c.DryRun,
			"updatedAt":   now.Format(time.RFC3339Nano),
		})
	}
	return nil
}

func conntrackTuningFirewallLogPath(router *api.Router) string {
	if router == nil {
		return ""
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.FirewallAPIVersion || resource.Kind != "FirewallEventLog" {
			continue
		}
		spec, err := resource.FirewallEventLogSpec()
		if err != nil || !spec.Enabled {
			continue
		}
		if strings.TrimSpace(spec.Path) != "" {
			return strings.TrimSpace(spec.Path)
		}
	}
	return ""
}

func sysctlProfileSettings(spec api.SysctlProfileSpec) ([]sysctlSetting, error) {
	entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
	if err != nil {
		return nil, err
	}
	settings := make([]sysctlSetting, 0, len(entries))
	for _, entry := range entries {
		settings = append(settings, sysctlSetting{Key: entry.Key, Value: entry.Value, Compare: entry.Compare, Runtime: spec.Runtime, Persistent: spec.Persistent, Optional: entry.Optional})
	}
	return settings, nil
}

func runOutputCommandContext(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

var unsafeSysctlName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func safeSysctlName(name string) string { return sysctlprofile.SafeName(name) }

func normalizeSysctlRuntimeValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
