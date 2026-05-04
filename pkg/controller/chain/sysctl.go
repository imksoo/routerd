package chain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/sysctlprofile"
)

type outputCommandFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

type SysctlController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store   Store
	Command outputCommandFunc
}

func (c SysctlController) Reconcile(ctx context.Context) error {
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "Sysctl":
			spec, err := resource.SysctlSpec()
			if err != nil {
				return err
			}
			result, err := c.applyOne(ctx, resource.Metadata.Name, "Sysctl", sysctlSetting{Key: spec.Key, Value: spec.Value, Runtime: spec.Runtime, Persistent: spec.Persistent, Optional: spec.Optional})
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
	return nil
}

type sysctlSetting struct {
	Key        string
	Value      string
	Runtime    *bool
	Persistent bool
	Optional   bool
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
	current := strings.TrimSpace(string(currentOut))
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
	changed := currentErr != nil || current != spec.Value
	if changed {
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
		current = spec.Value
	}
	persistentPath := ""
	if spec.Persistent {
		persistentPath = filepath.Join("/etc/sysctl.d", "90-routerd-"+safeSysctlName(name)+".conf")
		data := []byte(fmt.Sprintf("# Managed by routerd. Do not edit by hand.\n%s = %s\n", spec.Key, spec.Value))
		if err := os.WriteFile(persistentPath, data, 0644); err != nil {
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
	if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, map[string]any{
		"phase":        "Applied",
		"key":          spec.Key,
		"value":        spec.Value,
		"currentValue": current,
		"changed":      changed,
		"runtime":      true,
		"persistent":   spec.Persistent,
		"optional":     spec.Optional,
		"path":         persistentPath,
		"updatedAt":    time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return sysctlApplyResult{}, err
	}
	return sysctlApplyResult{changed: changed}, nil
}

func (c SysctlController) publishApplied(ctx context.Context, kind, name, key, value string) error {
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.sysctl.applied", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: kind, Name: name}
	event.Attributes = map[string]string{"key": key, "value": value}
	return c.Bus.Publish(ctx, event)
}

func sysctlProfileSettings(spec api.SysctlProfileSpec) ([]sysctlSetting, error) {
	entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
	if err != nil {
		return nil, err
	}
	settings := make([]sysctlSetting, 0, len(entries))
	for _, entry := range entries {
		settings = append(settings, sysctlSetting{Key: entry.Key, Value: entry.Value, Runtime: spec.Runtime, Persistent: spec.Persistent, Optional: entry.Optional})
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
