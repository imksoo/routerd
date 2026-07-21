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
	"github.com/imksoo/routerd/pkg/platform"
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
	osName := c.OSName
	if osName == "" {
		osName = packageOSName(runtime.GOOS)
	}
	for _, resource := range kernelModuleControllerResources(c.Router, osName) {
		spec, err := resource.KernelModuleSpec()
		if err != nil {
			return err
		}
		if !kernelModuleSupportedOS(osName) {
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
		changed, loaded, skipped, err := c.applyKernelModules(ctx, resource.Metadata.Name, spec, osName)
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

func kernelModuleControllerResources(router *api.Router, osName string) []api.Resource {
	if osName == "freebsd" {
		return hostdeps.KernelModuleResourcesForOS(router, platform.OSFreeBSD)
	}
	return hostdeps.KernelModuleResources(router)
}

func kernelModuleSupportedOS(osName string) bool {
	switch osName {
	case "linux", "ubuntu", "debian", "freebsd":
		return true
	default:
		return false
	}
}

func (c KernelModuleController) applyKernelModules(ctx context.Context, name string, spec api.KernelModuleSpec, osName string) (bool, []string, []string, error) {
	modules := compactStringList(append([]string(nil), spec.Modules...))
	sort.Strings(modules)
	changed := false
	var loaded, skipped []string
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	if osName == "freebsd" && spec.Persistent {
		return false, nil, nil, fmt.Errorf("persistent KernelModule is not supported on FreeBSD")
	}
	if api.BoolDefault(spec.Runtime, true) {
		loadedModules := c.loadedKernelModules(ctx, command, osName, modules)
		for _, module := range modules {
			if loadedModules[module] {
				loaded = append(loaded, module)
				continue
			}
			if c.DryRun {
				changed = true
				continue
			}
			commandName := "modprobe"
			if osName == "freebsd" {
				commandName = "kldload"
			}
			out, err := command(ctx, commandName, module)
			if err != nil {
				if osName == "freebsd" && freeBSDModuleAlreadyLoaded(out) {
					loaded = append(loaded, module)
					continue
				}
				if spec.Optional {
					skipped = append(skipped, module)
					continue
				}
				return changed, loaded, skipped, fmt.Errorf("%s %s: %w: %s", commandName, module, err, strings.TrimSpace(string(out)))
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

func (c KernelModuleController) loadedKernelModules(ctx context.Context, command outputCommandFunc, osName string, modules []string) map[string]bool {
	if osName == "freebsd" {
		out := map[string]bool{}
		for _, module := range modules {
			if _, err := command(ctx, "kldstat", "-q", "-m", module); err == nil {
				out[module] = true
			}
		}
		return out
	}
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

func freeBSDModuleAlreadyLoaded(out []byte) bool {
	text := strings.ToLower(string(out))
	return strings.Contains(text, "file exists") || strings.Contains(text, "already loaded")
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
