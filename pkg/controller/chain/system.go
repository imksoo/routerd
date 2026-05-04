package chain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/platform"
	"routerd/pkg/render"
)

type NetworkAdoptionController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store              Store
	Command            outputCommandFunc
	DryRun             bool
	NetworkdDropinBase string
	ResolvedDropinDir  string
}

func (c NetworkAdoptionController) Reconcile(ctx context.Context) error {
	defaults, features := platform.Current()
	if c.NetworkdDropinBase == "" {
		c.NetworkdDropinBase = defaults.NetworkdDropinDir
	}
	if c.ResolvedDropinDir == "" {
		c.ResolvedDropinDir = "/etc/systemd/resolved.conf.d"
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "NetworkAdoption" {
			continue
		}
		spec, err := resource.NetworkAdoptionSpec()
		if err != nil {
			return err
		}
		if !features.HasSystemd {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NetworkAdoption", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "SystemdUnsupported",
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			continue
		}
		ifname := strings.TrimSpace(spec.IfName)
		if ifname == "" && spec.Interface != "" {
			ifname = interfaceIfName(c.Router, spec.Interface)
		}
		paths, changed, err := c.applyNetworkAdoption(ctx, resource.Metadata.Name, ifname, spec, command)
		if err != nil {
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NetworkAdoption", resource.Metadata.Name, map[string]any{
				"phase":     "Error",
				"reason":    "ApplyFailed",
				"ifname":    ifname,
				"paths":     paths,
				"error":     err.Error(),
				"dryRun":    c.DryRun,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); saveErr != nil {
				return saveErr
			}
			return err
		}
		phase := "Applied"
		if c.DryRun && changed {
			phase = "Rendered"
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "NetworkAdoption", resource.Metadata.Name, map[string]any{
			"phase":     phase,
			"ifname":    ifname,
			"paths":     paths,
			"changed":   changed,
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		if changed && !c.DryRun && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.network_adoption.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "NetworkAdoption", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"ifname": ifname, "paths": strings.Join(paths, ",")}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c NetworkAdoptionController) applyNetworkAdoption(ctx context.Context, name, ifname string, spec api.NetworkAdoptionSpec, command outputCommandFunc) ([]string, bool, error) {
	state := firstNonEmpty(spec.State, "present")
	var paths []string
	var changed bool
	networkdChanged := false
	resolvedChanged := false
	if spec.SystemdNetworkd.DisableDHCPv4 || spec.SystemdNetworkd.DisableDHCPv6 || spec.SystemdNetworkd.DisableIPv6RA {
		if ifname == "" {
			return paths, changed, fmt.Errorf("ifname is required for systemdNetworkd adoption")
		}
		path := filepath.Join(networkdDropinDir(c.NetworkdDropinBase, ifname), firstNonEmpty(spec.SystemdNetworkd.DropinName, "90-routerd-adoption.conf"))
		paths = append(paths, path)
		if state == "absent" {
			removed, err := removeFileIfExists(path, c.DryRun)
			if err != nil {
				return paths, changed, err
			}
			networkdChanged = removed
		} else {
			data := networkdAdoptionDropin(spec.SystemdNetworkd)
			fileChanged, err := writeFileIfChanged(path, data, 0644, c.DryRun)
			if err != nil {
				return paths, changed, err
			}
			networkdChanged = fileChanged
		}
		changed = changed || networkdChanged
	}
	if spec.SystemdResolved.DisableDNSStubListener {
		path := filepath.Join(c.ResolvedDropinDir, firstNonEmpty(spec.SystemdResolved.DropinName, "90-routerd-adoption.conf"))
		paths = append(paths, path)
		if state == "absent" {
			removed, err := removeFileIfExists(path, c.DryRun)
			if err != nil {
				return paths, changed, err
			}
			resolvedChanged = removed
		} else {
			data := []byte("# Managed by routerd. Do not edit by hand.\n[Resolve]\nDNSStubListener=no\n")
			fileChanged, err := writeFileIfChanged(path, data, 0644, c.DryRun)
			if err != nil {
				return paths, changed, err
			}
			resolvedChanged = fileChanged
		}
		changed = changed || resolvedChanged
	}
	if c.DryRun || !api.BoolDefault(spec.Reload, true) {
		return paths, changed, nil
	}
	if networkdChanged {
		if _, err := command(ctx, "networkctl", "reload"); err != nil {
			if _, fallbackErr := command(ctx, "systemctl", "restart", "systemd-networkd.service"); fallbackErr != nil {
				return paths, changed, fmt.Errorf("reload networkd: %w; restart fallback: %v", err, fallbackErr)
			}
		}
		if ifname != "" {
			if _, err := command(ctx, "networkctl", "reconfigure", ifname); err != nil {
				return paths, changed, fmt.Errorf("reconfigure %s: %w", ifname, err)
			}
		}
	}
	if resolvedChanged {
		if _, err := command(ctx, "systemctl", "restart", "systemd-resolved.service"); err != nil {
			return paths, changed, fmt.Errorf("restart systemd-resolved: %w", err)
		}
	}
	_ = name
	return paths, changed, nil
}

type SystemdUnitController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store            Store
	Command          outputCommandFunc
	DryRun           bool
	SystemdSystemDir string
}

func (c SystemdUnitController) Reconcile(ctx context.Context) error {
	defaults, features := platform.Current()
	if c.SystemdSystemDir == "" {
		c.SystemdSystemDir = defaults.SystemdSystemDir
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "SystemdUnit" {
			continue
		}
		spec, err := resource.SystemdUnitSpec()
		if err != nil {
			return err
		}
		unitName := firstNonEmpty(spec.UnitName, resource.Metadata.Name)
		if !features.HasSystemd {
			if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", resource.Metadata.Name, map[string]any{
				"phase":     "Pending",
				"reason":    "SystemdUnsupported",
				"unitName":  unitName,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			continue
		}
		path := filepath.Join(c.SystemdSystemDir, unitName)
		changed, err := c.applySystemdUnit(ctx, resource.Metadata.Name, path, unitName, spec, command)
		if err != nil {
			if saveErr := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", resource.Metadata.Name, map[string]any{
				"phase":     "Error",
				"reason":    "ApplyFailed",
				"unitName":  unitName,
				"path":      path,
				"error":     err.Error(),
				"dryRun":    c.DryRun,
				"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}); saveErr != nil {
				return saveErr
			}
			return err
		}
		phase := "Applied"
		if c.DryRun && changed {
			phase = "Rendered"
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "SystemdUnit", resource.Metadata.Name, map[string]any{
			"phase":     phase,
			"unitName":  unitName,
			"path":      path,
			"changed":   changed,
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		if changed && !c.DryRun && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.systemd_unit.applied", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"unitName": unitName, "path": path}
			if err := c.Bus.Publish(ctx, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c SystemdUnitController) applySystemdUnit(ctx context.Context, name, path, unitName string, spec api.SystemdUnitSpec, command outputCommandFunc) (bool, error) {
	state := firstNonEmpty(spec.State, "present")
	var changed bool
	if state == "absent" {
		removed, err := removeFileIfExists(path, c.DryRun)
		if err != nil {
			return changed, err
		}
		changed = removed
		if c.DryRun {
			return changed, nil
		}
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return changed, err
		}
		_, _ = command(ctx, "systemctl", "disable", "--now", unitName)
		return changed, nil
	}
	data := render.SystemdUnit(name, spec)
	fileChanged, err := writeFileIfChanged(path, data, 0644, c.DryRun)
	if err != nil {
		return changed, err
	}
	changed = changed || fileChanged
	if c.DryRun {
		return changed, nil
	}
	if fileChanged {
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return changed, err
		}
	}
	if api.BoolDefault(spec.Enabled, true) {
		if _, err := command(ctx, "systemctl", "enable", unitName); err != nil {
			return changed, err
		}
	}
	if api.BoolDefault(spec.Started, true) {
		if _, err := command(ctx, "systemctl", "restart", unitName); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func networkdAdoptionDropin(spec api.NetworkAdoptionNetworkdSpec) []byte {
	var b strings.Builder
	b.WriteString("# Managed by routerd. Do not edit by hand.\n[Network]\n")
	if spec.DisableDHCPv4 && spec.DisableDHCPv6 {
		b.WriteString("DHCP=no\n")
	} else if spec.DisableDHCPv4 {
		b.WriteString("DHCP=ipv6\n")
	} else if spec.DisableDHCPv6 {
		b.WriteString("DHCP=ipv4\n")
	}
	if spec.DisableIPv6RA {
		b.WriteString("IPv6AcceptRA=no\n")
	}
	return []byte(b.String())
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode, dryRun bool) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if dryRun {
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return false, err
	}
	return true, nil
}

func removeFileIfExists(path string, dryRun bool) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if dryRun {
		return true, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

var unsafeNetworkdName = regexp.MustCompile(`[^A-Za-z0-9_.:-]+`)

func networkdDropinDir(base, ifname string) string {
	if base == "" {
		base = "/etc/systemd/network"
	}
	name := strings.Trim(unsafeNetworkdName.ReplaceAllString(ifname, "-"), "-.")
	if name == "" {
		name = "interface"
	}
	return filepath.Join(base, "10-netplan-"+name+".network.d")
}
