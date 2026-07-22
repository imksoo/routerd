// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
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

const (
	freeBSDKernelModuleBaseDir  = "/boot/loader.conf.d"
	kernelModuleOwnershipHeader = "# Managed by routerd. Do not edit by hand.\n"
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
	resources := kernelModuleControllerResources(c.Router, osName)
	activePersistentFiles := map[string]bool{}
	for _, resource := range resources {
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
		if osName == "freebsd" && spec.Persistent {
			activePersistentFiles[c.kernelModulePersistencePath(resource.Metadata.Name, osName)] = true
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
	if osName == "freebsd" {
		if _, err := c.removeStaleFreeBSDKernelModuleFiles(activePersistentFiles); err != nil {
			return err
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
	if osName == "freebsd" && spec.Persistent {
		for _, module := range modules {
			if !isFreeBSDLoaderModuleIdentifier(module) {
				return false, nil, nil, fmt.Errorf("FreeBSD KernelModule %q is not a loader variable identifier", module)
			}
		}
	}
	changed := false
	var loaded, skipped []string
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
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
		path := c.kernelModulePersistencePath(name, osName)
		data := renderKernelModulePersistence(modules, osName)
		fileChanged, err := c.writeKernelModulePersistence(path, data)
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

func isFreeBSDLoaderModuleIdentifier(module string) bool {
	if module == "" || !isFreeBSDLoaderModuleInitial(module[0]) {
		return false
	}
	for _, r := range module[1:] {
		if r != '_' && r != '-' && !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func isFreeBSDLoaderModuleInitial(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func (c KernelModuleController) kernelModulePersistencePath(name, osName string) string {
	baseDir := c.BaseDir
	if baseDir == "" {
		if osName == "freebsd" {
			baseDir = freeBSDKernelModuleBaseDir
		} else {
			baseDir = "/etc/modules-load.d"
		}
	}
	return filepath.Join(baseDir, "90-routerd-"+safeSysctlName(name)+".conf")
}

func renderKernelModulePersistence(modules []string, osName string) []byte {
	if osName == "freebsd" {
		var out strings.Builder
		out.WriteString(kernelModuleOwnershipHeader)
		for _, module := range modules {
			fmt.Fprintf(&out, "%s_load=\"YES\"\n", module)
		}
		return []byte(out.String())
	}
	return []byte(kernelModuleOwnershipHeader + strings.Join(modules, "\n") + "\n")
}

func (c KernelModuleController) writeKernelModulePersistence(path string, data []byte) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("refuse non-regular routerd KernelModule persistence file %s", path)
		}
		current, readErr := os.ReadFile(path)
		if readErr != nil {
			return false, readErr
		}
		if bytes.Equal(current, data) {
			return false, nil
		}
		if !bytes.HasPrefix(current, []byte(kernelModuleOwnershipHeader)) {
			return false, fmt.Errorf("refuse to replace non-routerd KernelModule persistence file %s", path)
		}
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if c.DryRun {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".routerd-kernelmodule-*")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0644); err != nil {
		temporary.Close()
		return false, err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return false, err
	}
	if err := temporary.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return false, err
	}
	return true, nil
}

func (c KernelModuleController) removeStaleFreeBSDKernelModuleFiles(active map[string]bool) (bool, error) {
	baseDir := c.BaseDir
	if baseDir == "" {
		baseDir = freeBSDKernelModuleBaseDir
	}
	entries, err := os.ReadDir(baseDir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	changed := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "90-routerd-") || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		path := filepath.Join(baseDir, entry.Name())
		if active[path] {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			return changed, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return changed, err
		}
		if !bytes.HasPrefix(data, []byte(kernelModuleOwnershipHeader)) {
			continue
		}
		if c.DryRun {
			changed = true
			continue
		}
		if err := os.Remove(path); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
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
