// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

type backendResult struct {
	Path             string
	Changed          bool
	Roles            map[string]string
	ServiceActive    *bool
	LastReloadAt     string
	LastRestartAt    string
	LastChangeReason string
}

type backend interface {
	Name() string
	Apply(ctx context.Context, c *Controller, aliases map[string]string, priorities map[string]int) (backendResult, error)
}

func (c *Controller) vrrpBackend() backend {
	if c.currentOS() == platform.OSFreeBSD {
		return carpBackend{}
	}
	return keepalivedBackend{}
}

type keepalivedBackend struct{}

func (keepalivedBackend) Name() string { return "keepalived" }

func (keepalivedBackend) Apply(ctx context.Context, c *Controller, aliases map[string]string, priorities map[string]int) (backendResult, error) {
	data, err := render.KeepalivedConfigWithOptions(c.Router, aliases, render.KeepalivedOptions{PriorityByResource: priorities})
	if err != nil {
		return backendResult{}, err
	}
	if len(data) == 0 {
		return backendResult{}, nil
	}
	path := firstNonEmpty(c.ConfigPath, "/etc/keepalived/keepalived.conf")
	changed, err := fileContentChanged(path, data)
	if err != nil {
		return backendResult{}, err
	}
	if changed && !c.DryRun {
		if err := writeFile(path, data, 0644); err != nil {
			return backendResult{}, err
		}
		if checker := strings.TrimSpace(c.KeepalivedCheck); checker != "" {
			if out, err := c.run(ctx, checker, "--config-test", "--use-file", path); err != nil {
				return backendResult{}, c.saveError(path, changed, nil, "KeepalivedConfigInvalid", fmt.Errorf("%s: %w: %s", checker, err, strings.TrimSpace(string(out))))
			}
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		reason := "keepalived.config changed"
		if c.useOpenRC() {
			action, err := reloadOrRestartOpenRCKeepalived(ctx, c, path)
			if err != nil {
				return backendResult{}, err
			}
			serviceActive := c.keepalivedServiceActive(ctx)
			result := backendResult{Path: path, Changed: changed, Roles: observeKeepalivedRolesAfterChange(ctx, c, aliases), ServiceActive: &serviceActive, LastChangeReason: reason}
			if action == "reload" {
				result.LastReloadAt = now
			} else {
				result.LastRestartAt = now
			}
			return result, nil
		} else {
			action, err := reloadOrRestartSystemdKeepalived(ctx, c, path)
			if err != nil {
				return backendResult{}, err
			}
			serviceActive := c.keepalivedServiceActive(ctx)
			result := backendResult{Path: path, Changed: changed, Roles: observeKeepalivedRolesAfterChange(ctx, c, aliases), ServiceActive: &serviceActive, LastChangeReason: reason}
			if action == "reload" {
				result.LastReloadAt = now
			} else {
				result.LastRestartAt = now
			}
			return result, nil
		}
	}
	serviceActive := c.keepalivedServiceActive(ctx)
	return backendResult{Path: path, Changed: changed, Roles: observeKeepalivedRoles(ctx, c, aliases), ServiceActive: &serviceActive}, nil
}

func reloadOrRestartOpenRCKeepalived(ctx context.Context, c *Controller, path string) (string, error) {
	rcService := firstNonEmpty(c.RCService, "rc-service")
	if _, err := c.run(ctx, rcService, "keepalived", "status"); err == nil {
		if out, err := c.run(ctx, rcService, "keepalived", "reload"); err == nil {
			return "reload", nil
		} else if c.Logger != nil {
			c.Logger.Warn("keepalived reload failed; restarting", "error", err, "output", strings.TrimSpace(string(out)))
		}
	}
	if out, err := c.run(ctx, rcService, "keepalived", "restart"); err != nil {
		return "", c.saveError(path, true, nil, "KeepalivedRestartFailed", fmt.Errorf("%s keepalived restart: %w: %s", rcService, err, strings.TrimSpace(string(out))))
	}
	return "restart", nil
}

func reloadOrRestartSystemdKeepalived(ctx context.Context, c *Controller, path string) (string, error) {
	systemctl := firstNonEmpty(c.Systemctl, "systemctl")
	if _, err := c.run(ctx, systemctl, "is-active", "--quiet", "keepalived.service"); err == nil {
		if out, err := c.run(ctx, systemctl, "reload", "keepalived.service"); err == nil {
			return "reload", nil
		} else if c.Logger != nil {
			c.Logger.Warn("keepalived reload failed; restarting", "error", err, "output", strings.TrimSpace(string(out)))
		}
	}
	if out, err := c.run(ctx, systemctl, "restart", "keepalived.service"); err != nil {
		return "", c.saveError(path, true, nil, "KeepalivedRestartFailed", fmt.Errorf("%s restart keepalived.service: %w: %s", systemctl, err, strings.TrimSpace(string(out))))
	}
	return "restart", nil
}

func (c *Controller) useOpenRC() bool {
	return c.OpenRC || platform.IsAlpineHost()
}

func observeKeepalivedRoles(ctx context.Context, c *Controller, aliases map[string]string) map[string]string {
	return observeKeepalivedRolesWithWait(ctx, c, aliases, false)
}

func observeKeepalivedRolesAfterChange(ctx context.Context, c *Controller, aliases map[string]string) map[string]string {
	return observeKeepalivedRolesWithWait(ctx, c, aliases, true)
}

func observeKeepalivedRolesWithWait(ctx context.Context, c *Controller, aliases map[string]string, wait bool) map[string]string {
	roles := dryRunRoles(c)
	if roles != nil {
		return roles
	}
	roles = map[string]string{}
	serviceActive := c.keepalivedServiceActive(ctx)
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil || !ok {
			continue
		}
		if err != nil || spec.Mode != "vrrp" {
			continue
		}
		if !serviceActive {
			roles[resource.Metadata.Name] = "inactive"
			continue
		}
		ifname := aliases[spec.Interface]
		address, err := renderVirtualAddress(c.Router, spec)
		if err != nil || ifname == "" {
			roles[resource.Metadata.Name] = "unknown"
			continue
		}
		ip := firstNonEmpty(c.IP, "ip")
		ipFamily := "-4"
		if spec.Family == "ipv6" {
			ipFamily = "-6"
		}
		attempts := 1
		if wait {
			attempts = 30
		}
		role := "backup"
		for i := 0; i < attempts; i++ {
			out, err := c.run(ctx, ip, ipFamily, "addr", "show", "dev", ifname)
			if err != nil {
				role = "unknown"
				break
			}
			if ipAddressPresent(string(out), address, spec.Family) {
				role = "master"
				break
			}
			if i+1 < attempts {
				select {
				case <-ctx.Done():
					role = "unknown"
					i = attempts
				case <-time.After(200 * time.Millisecond):
				}
			}
		}
		roles[resource.Metadata.Name] = role
	}
	return roles
}

func (c *Controller) keepalivedServiceActive(ctx context.Context) bool {
	if c.useOpenRC() {
		rcService := firstNonEmpty(c.RCService, "rc-service")
		_, err := c.run(ctx, rcService, "keepalived", "status")
		return err == nil
	}
	systemctl := firstNonEmpty(c.Systemctl, "systemctl")
	_, err := c.run(ctx, systemctl, "is-active", "--quiet", "keepalived.service")
	return err == nil
}

type carpBackend struct{}

func (carpBackend) Name() string { return "carp" }

func (carpBackend) Apply(ctx context.Context, c *Controller, aliases map[string]string, priorities map[string]int) (backendResult, error) {
	config, err := render.CARPConfigWithOptions(c.Router, aliases, render.CARPOptions{PriorityByResource: priorities})
	if err != nil {
		return backendResult{}, err
	}
	if len(config.Interfaces) == 0 {
		return backendResult{}, nil
	}
	changed := false
	if !c.DryRun {
		kldload := firstNonEmpty(c.Kldload, "kldload")
		_, _ = c.run(ctx, kldload, "carp")
		sysctl := firstNonEmpty(c.Sysctl, "sysctl")
		wantedPreempt := config.PreemptSysctlValue()
		currentPreempt, currentErr := c.run(ctx, sysctl, "-n", "net.inet.carp.preempt")
		if currentErr != nil || strings.TrimSpace(string(currentPreempt)) != wantedPreempt {
			if out, err := c.run(ctx, sysctl, "net.inet.carp.preempt="+wantedPreempt); err != nil {
				return backendResult{}, c.saveError("", changed, nil, "CARPPreemptSysctlFailed", fmt.Errorf("%s net.inet.carp.preempt=%s: %w: %s", sysctl, wantedPreempt, err, strings.TrimSpace(string(out))))
			}
			changed = true
		}
		ifconfig := firstNonEmpty(c.Ifconfig, "ifconfig")
		commands := config.IfconfigCommands()
		for i, iface := range config.Interfaces {
			out, err := c.run(ctx, ifconfig, iface.Interface)
			if err == nil && carpInterfaceConfigured(string(out), iface) {
				continue
			}
			args := append([]string(nil), commands[i]...)
			if out, err := c.run(ctx, ifconfig, args...); err != nil {
				return backendResult{}, c.saveError("", changed, nil, "CARPApplyFailed", fmt.Errorf("%s %s: %w: %s", ifconfig, strings.Join(args, " "), err, strings.TrimSpace(string(out))))
			}
			changed = true
		}
	}
	return backendResult{Changed: changed, Roles: observeCARPRoles(ctx, c, config)}, nil
}

func observeCARPRoles(ctx context.Context, c *Controller, config render.CARPConfigData) map[string]string {
	roles := dryRunRoles(c)
	if roles != nil {
		return roles
	}
	ifconfig := firstNonEmpty(c.Ifconfig, "ifconfig")
	roles = map[string]string{}
	for _, iface := range config.Interfaces {
		out, err := c.run(ctx, ifconfig, iface.Interface)
		if err != nil {
			roles[iface.Name] = "unknown"
			continue
		}
		roles[iface.Name] = carpRoleForVHID(string(out), iface.VirtualHostID)
	}
	return roles
}

func dryRunRoles(c *Controller) map[string]string {
	if !c.DryRun {
		return nil
	}
	roles := map[string]string{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && isVirtualAddressKind(resource.Kind) {
			roles[resource.Metadata.Name] = "dryrun"
		}
	}
	return roles
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode) (bool, error) {
	changed, err := fileContentChanged(path, data)
	if err != nil || !changed {
		return changed, err
	}
	return true, writeFile(path, data, mode)
}

func fileContentChanged(path string, data []byte) (bool, error) {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func carpRoleForVHID(output string, vhid int) string {
	needle := fmt.Sprintf("vhid %d", vhid)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "carp:" || fields[1] == "" || !strings.Contains(line, needle) {
			continue
		}
		switch strings.ToUpper(fields[1]) {
		case "MASTER":
			return "master"
		case "BACKUP":
			return "backup"
		case "INIT":
			return "init"
		default:
			return "unknown"
		}
	}
	return "unknown"
}

func carpInterfaceConfigured(output string, iface render.CARPInterface) bool {
	host := strings.TrimSpace(iface.Address)
	if before, _, ok := strings.Cut(host, "/"); ok {
		host = before
	}
	return strings.Contains(output, carpAddressNeedle(iface.Family, host)) &&
		strings.Contains(output, fmt.Sprintf("vhid %d", iface.VirtualHostID)) &&
		strings.Contains(output, fmt.Sprintf("advbase %d", iface.AdvBase)) &&
		strings.Contains(output, fmt.Sprintf("advskew %d", iface.AdvSkew))
}

func carpAddressNeedle(family, host string) string {
	if family == "ipv6" {
		return "inet6 " + host + " "
	}
	return "inet " + host + " "
}
