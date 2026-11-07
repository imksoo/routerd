// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/hostdeps"
)

type PackageController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store   Store
	Command outputCommandFunc
	DryRun  bool
	OSName  string
}

func (c PackageController) Reconcile(ctx context.Context) error {
	for _, resource := range packageControllerResources(c.Router) {
		spec, err := resource.PackageSpec()
		if err != nil {
			return err
		}
		osName := c.OSName
		if osName == "" {
			osName = packageOSName(runtime.GOOS)
		}
		set, ok := packageSetForOS(spec, osName)
		if !ok {
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "NoMatchingOSPackageSet",
				"os":        osName,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); saveErr != nil {
				return saveErr
			}
			continue
		}
		manager := firstNonEmpty(set.Manager, packageManagerForOS(set.OS))
		if manager != "apt" && manager != "dnf" && manager != "pkg" {
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "UnsupportedManagerRuntime",
				"os":        set.OS,
				"manager":   manager,
				"names":     set.Names,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); saveErr != nil {
				return saveErr
			}
			continue
		}
		command := c.Command
		if command == nil {
			command = runOutputCommandContext
		}
		var missing []string
		for _, name := range set.Names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			checkName, checkArgs := packageCheckCommand(manager, name)
			out, checkErr := command(ctx, checkName, checkArgs...)
			installed := checkErr == nil
			if checkErr != nil {
				var exitErr *exec.ExitError
				if errors.As(checkErr, &exitErr) && exitErr.ExitCode() == 1 {
					installed, checkErr = false, nil
				}
			} else if manager == "apt" {
				installed, checkErr = installedDPKGStatus(string(out))
			}
			if checkErr != nil {
				if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
					"phase": "Error", "reason": "ObserveFailed", "manager": manager, "package": name, "error": checkErr.Error(),
					"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
				}); saveErr != nil {
					return errors.Join(checkErr, saveErr)
				}
				return fmt.Errorf("observe package %s: %w", name, checkErr)
			}
			if !installed {
				missing = append(missing, name)
			}
		}
		changed := len(missing) > 0
		if changed && c.DryRun {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "DryRun",
				"os":        set.OS,
				"manager":   manager,
				"missing":   missing,
				"names":     set.Names,
				"dryRun":    true,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			continue
		}
		if changed {
			name, args := packageInstallCommand(manager, missing)
			if out, err := command(ctx, name, args...); err != nil {
				if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
					"phase":     "Error",
					"reason":    "InstallFailed",
					"manager":   manager,
					"missing":   missing,
					"error":     strings.TrimSpace(string(out)),
					"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
				}); saveErr != nil {
					return saveErr
				}
				return fmt.Errorf("install packages %v: %w", missing, err)
			}
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
			"phase":     "Applied",
			"os":        set.OS,
			"manager":   manager,
			"names":     set.Names,
			"changed":   changed,
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.package.installed", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "Package", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"manager": manager, "names": strings.Join(missing, ",")}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

// dpkg's selection state (including hold) does not establish whether the
// package is installed. Decode its error and actual status fields separately.
func installedDPKGStatus(output string) (bool, error) {
	fields := strings.Fields(output)
	if len(fields) != 3 {
		return false, errors.New("dpkg-query returned an unrecognized package status")
	}
	switch fields[0] {
	case "unknown", "install", "hold", "deinstall", "purge":
	default:
		return false, errors.New("dpkg-query returned an unrecognized selection state")
	}
	if fields[1] != "ok" && fields[1] != "reinstreq" {
		return false, errors.New("dpkg-query returned an unrecognized package error state")
	}
	switch fields[2] {
	case "not-installed", "config-files", "half-installed", "unpacked", "half-configured", "triggers-awaited", "triggers-pending", "installed":
		return fields[1] == "ok" && fields[2] == "installed", nil
	default:
		return false, errors.New("dpkg-query returned an unrecognized installation state")
	}
}

func packageControllerResources(router *api.Router) []api.Resource {
	return hostdeps.PackageResources(router)
}

func packageSetForCurrentOS(spec api.PackageSpec) (api.OSPackageSetSpec, bool) {
	return packageSetForOS(spec, packageOSName(runtime.GOOS))
}

func packageSetForOS(spec api.PackageSpec, current string) (api.OSPackageSetSpec, bool) {
	for _, set := range spec.Packages {
		if set.OS == current {
			return set, true
		}
	}
	return api.OSPackageSetSpec{}, false
}

func packageOSName(goos string) string {
	if goos == "freebsd" {
		return "freebsd"
	}
	if goos != "linux" {
		return goos
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "linux"
	}
	fields := parseOSReleaseID(string(data))
	switch fields {
	case "ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux":
		return fields
	default:
		return "linux"
	}
}

func parseOSReleaseID(data string) string {
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "ID" {
			continue
		}
		return strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return ""
}

func packageManagerForOS(osName string) string {
	switch osName {
	case "ubuntu", "debian":
		return "apt"
	case "fedora", "rhel", "rocky", "almalinux":
		return "dnf"
	case "freebsd":
		return "pkg"
	default:
		return ""
	}
}

func packageCheckCommand(manager, name string) (string, []string) {
	switch manager {
	case "apt":
		return "dpkg-query", []string{"-W", "-f=${Status}", name}
	case "dnf":
		return "rpm", []string{"-q", name}
	case "pkg":
		return "pkg", []string{"info", "-e", name}
	default:
		return manager, []string{name}
	}
}

func packageInstallCommand(manager string, missing []string) (string, []string) {
	switch manager {
	case "apt":
		args := append([]string{"install", "-y"}, missing...)
		if os.Geteuid() == 0 {
			return "apt-get", args
		}
		return "sudo", append([]string{"apt-get"}, args...)
	case "dnf":
		args := append([]string{"install", "-y"}, missing...)
		if os.Geteuid() == 0 {
			return "dnf", args
		}
		return "sudo", append([]string{"dnf"}, args...)
	case "pkg":
		args := append([]string{"install", "-y"}, missing...)
		if os.Geteuid() == 0 {
			return "pkg", args
		}
		return "sudo", append([]string{"pkg"}, args...)
	default:
		return manager, missing
	}
}
