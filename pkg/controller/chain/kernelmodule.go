// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/hostdeps"
)

type KernelModuleController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store           Store
	Command         outputCommandFunc
	DryRun          bool
	OSName          string
	BaseDir         string
	ProcModulesPath string
}

func (c KernelModuleController) Reconcile(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	for _, resource := range kernelModuleControllerResources(c.Router) {
		spec, err := resource.KernelModuleSpec()
		if err != nil {
			return err
		}
		osName := c.OSName
		if osName == "" {
			osName = packageOSName(runtime.GOOS)
		}
		if osName == "nixos" {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "KernelModule", resource.Metadata.Name, map[string]any{
				"phase":     "Applied",
				"reason":    "NixOSDeclarativeKernelModules",
				"os":        osName,
				"modules":   compactStringList(append([]string(nil), spec.Modules...)),
				"dryRun":    c.DryRun,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			continue
		}
		if runtime.GOOS != "linux" && osName != "linux" && osName != "ubuntu" && osName != "debian" {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "KernelModule", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "UnsupportedOS",
				"os":        osName,
				"modules":   compactStringList(append([]string(nil), spec.Modules...)),
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			continue
		}
		changed, loaded, skipped, err := c.applyKernelModules(ctx, resource.Metadata.Name, spec)
		if err != nil {
			if spec.Optional {
				skipped = append(skipped, err.Error())
			} else {
				_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, "KernelModule", resource.Metadata.Name, map[string]any{
					"phase":     "Error",
					"reason":    "ApplyFailed",
					"modules":   compactStringList(append([]string(nil), spec.Modules...)),
					"error":     err.Error(),
					"dryRun":    c.DryRun,
					"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
				})
				return err
			}
		}
		phase := "Applied"
		if len(skipped) > 0 {
			phase = "Degraded"
		}
		if c.DryRun && changed {
			phase = "Rendered"
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "KernelModule", resource.Metadata.Name, map[string]any{
			"phase":      phase,
			"modules":    compactStringList(append([]string(nil), spec.Modules...)),
			"loaded":     loaded,
			"skipped":    skipped,
			"changed":    changed,
			"runtime":    api.BoolDefault(spec.Runtime, true),
			"persistent": spec.Persistent,
			"dryRun":     c.DryRun,
			"updatedAt":  time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		if changed && !c.DryRun && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.kernel_module.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "KernelModule", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"modules": strings.Join(spec.Modules, ",")}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func kernelModuleControllerResources(router *api.Router) []api.Resource {
	return hostdeps.KernelModuleResources(router)
}

func (c KernelModuleController) applyKernelModules(ctx context.Context, name string, spec api.KernelModuleSpec) (bool, []string, []string, error) {
	modules := compactStringList(append([]string(nil), spec.Modules...))
	sort.Strings(modules)
	changed := false
	var loaded, skipped []string
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	if api.BoolDefault(spec.Runtime, true) {
		loadedModules := c.loadedKernelModules()
		for _, module := range modules {
			if loadedModules[module] {
				loaded = append(loaded, module)
				continue
			}
			if c.DryRun {
				changed = true
				continue
			}
			out, err := command(ctx, "modprobe", module)
			if err != nil {
				if spec.Optional {
					skipped = append(skipped, module)
					continue
				}
				return changed, loaded, skipped, fmt.Errorf("modprobe %s: %w: %s", module, err, strings.TrimSpace(string(out)))
			}
			loaded = append(loaded, module)
			changed = true
		}
	}
	if spec.Persistent {
		baseDir := c.BaseDir
		if baseDir == "" {
			baseDir = "/etc/modules-load.d"
		}
		path := filepath.Join(baseDir, "90-routerd-"+safeSysctlName(name)+".conf")
		data := []byte("# Managed by routerd. Do not edit by hand.\n" + strings.Join(modules, "\n") + "\n")
		fileChanged, err := writeFileIfChanged(path, data, 0644, c.DryRun)
		if err != nil {
			if spec.Optional {
				skipped = append(skipped, path)
			} else {
				return changed, loaded, skipped, err
			}
		}
		changed = changed || fileChanged
	}
	return changed, loaded, skipped, nil
}

func (c KernelModuleController) loadedKernelModules() map[string]bool {
	path := c.ProcModulesPath
	if path == "" {
		path = "/proc/modules"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 {
			out[fields[0]] = true
		}
	}
	return out
}

func removeKernelModuleFile(path string, dryRun bool) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if dryRun {
		return true, nil
	}
	return true, os.Remove(path)
}

func compactStringList(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
