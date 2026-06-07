// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
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
	Command outputCommandFunc
	BaseDir string
}

func (c SysctlController) Reconcile(ctx context.Context) error {
	desired := map[string]bool{}
	for _, resource := range hostdeps.SysctlResources(c.Router) {
		desired[resource.Kind+"/"+resource.Metadata.Name] = true
		switch resource.Kind {
		case "Sysctl":
			spec, err := resource.SysctlSpec()
			if err != nil {
				return err
			}
			result, err := c.applyOne(ctx, resource.Metadata.Name, "Sysctl", sysctlSetting{Key: spec.Key, Value: spec.Value, ExpectedValue: spec.ExpectedValue, Compare: spec.Compare, Runtime: spec.Runtime, Persistent: spec.Persistent, Optional: spec.Optional})
			if err != nil {
				return err
			}
			if result.changed && c.Bus != nil {
				if err := c.publishApplied(ctx, "Sysctl", resource.Metadata.Name, spec.Key, spec.Value); err != nil {
					return err
				}
			}
		case "SysctlProfile":
			spec, err := resource.SysctlProfileSpec()
			if err != nil {
				return err
			}
			settings, err := sysctlProfileSettings(spec)
			if err != nil {
				return err
			}
			var applied, skipped, changed []string
			for _, setting := range settings {
				result, err := c.applyOne(ctx, resource.Metadata.Name+"-"+safeSysctlName(setting.Key), "SysctlProfile", setting)
				if err != nil {
					return err
				}
				if result.skipped {
					skipped = append(skipped, setting.Key)
					continue
				}
				applied = append(applied, setting.Key)
				if result.changed {
					changed = append(changed, setting.Key)
				}
			}
			phase := "Applied"
			if len(skipped) > 0 {
				phase = "Degraded"
			}
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SysctlProfile", resource.Metadata.Name, map[string]any{
				"phase":      phase,
				"profile":    spec.Profile,
				"applied":    applied,
				"changed":    changed,
				"skipped":    skipped,
				"runtime":    api.BoolDefault(spec.Runtime, true),
				"persistent": spec.Persistent,
				"updatedAt":  time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			if len(changed) > 0 && c.Bus != nil {
				if err := c.publishApplied(ctx, "SysctlProfile", resource.Metadata.Name, "profile", spec.Profile); err != nil {
					return err
				}
			}
		}
	}
	if c.Router != nil && c.Router.Spec.Apply.AutoTuneConntrack {
		if err := c.applyConntrackTuning(ctx); err != nil {
			return err
		}
	}
	if err := c.cleanupRemovedSAMProxyARP(ctx, desired); err != nil {
		return err
	}
	return nil
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
	changed bool
	skipped bool
}

func (c SysctlController) applyOne(ctx context.Context, name, kind string, spec sysctlSetting) (sysctlApplyResult, error) {
	if !api.BoolDefault(spec.Runtime, true) {
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, map[string]any{
			"phase":      "Skipped",
			"reason":     "RuntimeDisabled",
			"key":        spec.Key,
			"value":      spec.Value,
			"updatedAt":  time.Now().UTC().Format(time.RFC3339Nano),
			"persistent": spec.Persistent,
		}); err != nil {
			return sysctlApplyResult{}, err
		}
		return sysctlApplyResult{skipped: true}, nil
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	currentOut, currentErr := command(ctx, "sysctl", "-n", spec.Key)
	current := normalizeSysctlRuntimeValue(string(currentOut))
	expected := normalizeSysctlRuntimeValue(firstNonEmpty(spec.ExpectedValue, spec.Value))
	if currentErr != nil && spec.Optional {
		_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, map[string]any{
			"phase":     "Skipped",
			"reason":    "ReadFailedOptional",
			"key":       spec.Key,
			"value":     spec.Value,
			"error":     currentErr.Error(),
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		})
		return sysctlApplyResult{skipped: true}, nil
	}
	matches, compareErr := sysctlValueMatches(current, expected, spec.Compare)
	runtimeChanged := currentErr != nil || compareErr != nil || !matches
	if runtimeChanged {
		if out, err := command(ctx, "sysctl", "-w", spec.Key+"="+spec.Value); err != nil {
			status := map[string]any{
				"phase":        "Error",
				"reason":       "ApplyFailed",
				"key":          spec.Key,
				"value":        spec.Value,
				"currentValue": current,
				"error":        strings.TrimSpace(string(out)),
				"updatedAt":    time.Now().UTC().Format(time.RFC3339Nano),
			}
			if spec.Optional {
				status["phase"] = "Skipped"
				status["reason"] = "ApplyFailedOptional"
				_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, status)
				return sysctlApplyResult{skipped: true}, nil
			}
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, status); saveErr != nil {
				return sysctlApplyResult{}, saveErr
			}
			return sysctlApplyResult{}, fmt.Errorf("apply sysctl %s: %w", spec.Key, err)
		}
		current = expected
	}
	persistentPath := ""
	persistentChanged := false
	if spec.Persistent {
		baseDir := c.BaseDir
		if baseDir == "" {
			baseDir = "/etc/sysctl.d"
		}
		persistentPath = filepath.Join(baseDir, "90-routerd-"+safeSysctlName(name)+".conf")
		data := []byte(fmt.Sprintf("# Managed by routerd. Do not edit by hand.\n%s = %s\n", spec.Key, spec.Value))
		var err error
		persistentChanged, err = writeFileIfChanged(persistentPath, data, 0644, false)
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
			if spec.Optional {
				status["phase"] = "Skipped"
				status["reason"] = "PersistFailedOptional"
				_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, status)
				return sysctlApplyResult{skipped: true}, nil
			}
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, status); saveErr != nil {
				return sysctlApplyResult{}, saveErr
			}
			return sysctlApplyResult{}, fmt.Errorf("persist sysctl %s: %w", spec.Key, err)
		}
	}
	changed := runtimeChanged || persistentChanged
	status := map[string]any{
		"phase":         "Applied",
		"key":           spec.Key,
		"value":         spec.Value,
		"expectedValue": expected,
		"compare":       firstNonEmpty(spec.Compare, "exact"),
		"currentValue":  current,
		"changed":       changed,
		"runtime":       true,
		"persistent":    spec.Persistent,
		"optional":      spec.Optional,
		"path":          persistentPath,
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
	if c.Store == nil {
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
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	desiredStatusIDs := map[string]bool{}
	for key := range desired {
		name := strings.TrimPrefix(key, "Sysctl/")
		if name != key && name != "" {
			desiredStatusIDs[lifecycle.OwnerKey(api.SystemAPIVersion, "Sysctl", name)] = true
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
		previous := strings.TrimSpace(fmt.Sprint(status.Status["previousValue"]))
		changed, _ := statusBool(status.Status["changed"])
		if changed && key != "" && key != "<nil>" && previous != "" && previous != "<nil>" && previous != "1" {
			if out, err := command(ctx, "sysctl", "-w", key+"="+previous); err != nil {
				return fmt.Errorf("restore removed SAM proxy_arp sysctl %s: %w: %s", key, err, strings.TrimSpace(string(out)))
			}
		}
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
		return c.Store.SaveObjectStatus(api.SystemAPIVersion, "ConntrackTuning", "default", map[string]any{
			"phase":       "Applied",
			"applyMode":   summary.ApplyMode,
			"autoApply":   summary.AutoApply,
			"suggestions": len(summary.Suggestions),
			"applied":     applied,
			"changed":     changed,
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

func safeSysctlName(name string) string {
	name = strings.Trim(unsafeSysctlName.ReplaceAllString(name, "-"), "-.")
	if name == "" {
		return "sysctl"
	}
	return name
}

func normalizeSysctlRuntimeValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func sysctlValueMatches(current, expected, compare string) (bool, error) {
	switch firstNonEmpty(compare, "exact") {
	case "exact":
		return current == expected, nil
	case "atLeast":
		currentFields := strings.Fields(current)
		expectedFields := strings.Fields(expected)
		if len(currentFields) != len(expectedFields) {
			return false, fmt.Errorf("field count mismatch")
		}
		for i := range expectedFields {
			c, err := strconv.ParseInt(currentFields[i], 10, 64)
			if err != nil {
				return false, err
			}
			e, err := strconv.ParseInt(expectedFields[i], 10, 64)
			if err != nil {
				return false, err
			}
			if c < e {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported compare %q", compare)
	}
}
