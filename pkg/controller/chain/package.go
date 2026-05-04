package chain

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
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
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Package" {
			continue
		}
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
			_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "NoMatchingOSPackageSet",
				"os":        osName,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			})
			continue
		}
		manager := firstNonEmpty(set.Manager, packageManagerForOS(set.OS))
		if manager == "nix" {
			_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
				"phase":     "Rendered",
				"reason":    "NixOSDeclarativeOnly",
				"os":        set.OS,
				"manager":   manager,
				"names":     set.Names,
				"changed":   false,
				"dryRun":    c.DryRun,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			})
			continue
		}
		if manager != "apt" && manager != "dnf" && manager != "pkg" {
			_ = c.Store.SaveObjectStatus(api.SystemAPIVersion, "Package", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "UnsupportedManagerRuntime",
				"os":        set.OS,
				"manager":   manager,
				"names":     set.Names,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			})
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
			if _, err := command(ctx, checkName, checkArgs...); err != nil {
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
	case "ubuntu", "debian", "fedora", "rhel", "rocky", "almalinux", "nixos":
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
	case "nixos":
		return "nix"
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
