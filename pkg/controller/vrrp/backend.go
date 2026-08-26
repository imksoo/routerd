// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

type backendResult struct {
	Path                string
	Changed             bool
	Roles               map[string]string
	ServiceActive       *bool
	LastReloadAt        string
	LastRestartAt       string
	LastChangeReason    string
	VMACRepairError     error
	GracefulActivations map[string]gracefulActivationStatus
}

type gracefulActivationStatus struct {
	State         string
	VIPAdvertised bool
	StartedAt     time.Time
	Timeout       time.Duration
	WaitingFor    []string
	Reason        string
	Error         string
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
	path := stringutil.FirstNonEmpty(c.ConfigPath, "/etc/keepalived/keepalived.conf")
	changed, err := fileContentChanged(path, data)
	if err != nil {
		return backendResult{}, err
	}
	if changed && !c.DryRun {
		if err := seedGracefulElectionStates(ctx, c, aliases); err != nil {
			return backendResult{}, err
		}
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
		action, err := reloadOrRestartSystemdKeepalived(ctx, c, path)
		if err != nil {
			return backendResult{}, err
		}
		serviceActive := c.refreshKeepalivedServiceActive(ctx)
		result := backendResult{Path: path, Changed: changed, Roles: observeKeepalivedRolesAfterChange(ctx, c, aliases), ServiceActive: &serviceActive, LastChangeReason: reason}
		completeKeepalivedResult(ctx, c, aliases, &result)
		if action == "reload" {
			result.LastReloadAt = now
		} else {
			result.LastRestartAt = now
		}
		return result, nil
	}
	roles := observeKeepalivedRoles(ctx, c, aliases)
	serviceActive := c.refreshKeepalivedServiceActive(ctx)
	if !serviceActive && !c.DryRun {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		reason := "keepalived.service inactive"
		action, err := reloadOrRestartSystemdKeepalived(ctx, c, path)
		if err != nil {
			return backendResult{}, err
		}
		serviceActive = c.refreshKeepalivedServiceActive(ctx)
		result := backendResult{Path: path, Changed: changed, Roles: observeKeepalivedRolesAfterChange(ctx, c, aliases), ServiceActive: &serviceActive, LastChangeReason: reason}
		completeKeepalivedResult(ctx, c, aliases, &result)
		if action == "reload" {
			result.LastReloadAt = now
		} else {
			result.LastRestartAt = now
		}
		return result, nil
	}
	result := backendResult{Path: path, Changed: changed, Roles: roles, ServiceActive: &serviceActive}
	completeKeepalivedResult(ctx, c, aliases, &result)
	return result, nil
}

func completeKeepalivedResult(ctx context.Context, c *Controller, aliases map[string]string, result *backendResult) {
	vmacErr := syncFailoverVMACs(ctx, c, aliases, result.Roles)
	activations, activationErr := reconcileGracefulActivations(ctx, c, aliases, result.Roles)
	result.GracefulActivations = activations
	result.VMACRepairError = errors.Join(vmacErr, activationErr)
}

// syncFailoverVMACs makes the WAN L2 identity match the one authoritative
// VRRP role even if keepalived retains MASTER across a configuration reload.
// The keepalived notify hooks provide the fast transition path; this sync is
// the idempotent reconciliation path and runs before role status is published.
func syncFailoverVMACs(ctx context.Context, c *Controller, aliases map[string]string, roles map[string]string) error {
	if c.DryRun {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil || !ok || spec.Mode != "vrrp" {
			if err != nil {
				return err
			}
			continue
		}
		vmacs := append([]api.VirtualAddressVRRPFailoverVMACSpec(nil), spec.VRRP.AdditionalFailoverVMACs...)
		if spec.VRRP.FailoverVMAC != nil {
			vmacs = append([]api.VirtualAddressVRRPFailoverVMACSpec{*spec.VRRP.FailoverVMAC}, vmacs...)
		}
		if len(vmacs) == 0 && spec.VRRP.GracefulActivation == nil {
			continue
		}
		action := "deactivate"
		if roles[resource.Metadata.Name] == "master" {
			action = "activate"
		}
		args := []string{action}
		if spec.VRRP.GracefulActivation != nil {
			address, renderErr := renderVirtualAddress(c.Router, spec)
			if renderErr != nil {
				return renderErr
			}
			args = append(args, "--resource", resource.Metadata.Name, "--deferred-address", address, "--deferred-interface", aliases[spec.Interface], "--reconcile")
		}
		if len(vmacs) == 1 && strings.TrimSpace(vmacs[0].LinkLocalAddress) == "" {
			vmac := vmacs[0]
			parent := aliases[vmac.ParentInterface]
			if parent == "" {
				return fmt.Errorf("%s spec.vrrp.failoverVMAC references interface with empty ifname %q", resource.ID(), vmac.ParentInterface)
			}
			args = append(args, "--parent", parent, "--interface", vmac.Interface, "--mac", vmac.MACAddress)
		} else {
			for _, vmac := range vmacs {
				parent := aliases[vmac.ParentInterface]
				if parent == "" {
					return fmt.Errorf("%s spec.vrrp.failoverVMAC references interface with empty ifname %q", resource.ID(), vmac.ParentInterface)
				}
				args = append(args, "--vmac", parent+","+vmac.Interface+","+vmac.MACAddress+","+vmac.LinkLocalAddress+","+fmt.Sprintf("%t", vmac.WithdrawRouterAdvertisement))
			}
		}
		if out, err := c.run(ctx, "/usr/local/sbin/routerd-vrrp-vmac", args...); err != nil {
			return fmt.Errorf("sync failover VMAC for %s: %w: %s", resource.ID(), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func reconcileGracefulActivations(ctx context.Context, c *Controller, aliases map[string]string, roles map[string]string) (map[string]gracefulActivationStatus, error) {
	statuses := map[string]gracefulActivationStatus{}
	var resultErr error
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil || !ok || spec.Mode != "vrrp" || spec.VRRP.GracefulActivation == nil {
			if err != nil {
				resultErr = errors.Join(resultErr, err)
			}
			continue
		}
		gate := spec.VRRP.GracefulActivation
		timeout := 45 * time.Second
		if strings.TrimSpace(gate.Timeout) != "" {
			parsed, parseErr := time.ParseDuration(gate.Timeout)
			if parseErr != nil || parsed <= 0 {
				err := fmt.Errorf("%s spec.vrrp.gracefulActivation.timeout must be a positive duration", resource.ID())
				statuses[resource.Metadata.Name] = gracefulActivationStatus{State: "Failed", Timeout: timeout, Reason: "InvalidTimeout", Error: err.Error()}
				resultErr = errors.Join(resultErr, err)
				continue
			}
			timeout = parsed
		}
		status := gracefulActivationStatus{State: "Standby", Timeout: timeout}
		if c.DryRun {
			status.State = "DryRun"
			statuses[resource.Metadata.Name] = status
			continue
		}
		ifname := aliases[spec.Interface]
		address, renderErr := renderVirtualAddress(c.Router, spec)
		if renderErr != nil || ifname == "" {
			if renderErr == nil {
				renderErr = fmt.Errorf("%s references interface with empty ifname %q", resource.ID(), spec.Interface)
			}
			status.State, status.Reason, status.Error = "Failed", "AddressUnavailable", renderErr.Error()
			statuses[resource.Metadata.Name] = status
			resultErr = errors.Join(resultErr, renderErr)
			continue
		}
		present, observeErr := c.staticIPv4AddressPresent(ctx, ifname, address)
		if observeErr != nil {
			status.State, status.Reason, status.Error = "Failed", "VIPObserveFailed", observeErr.Error()
			statuses[resource.Metadata.Name] = status
			resultErr = errors.Join(resultErr, observeErr)
			continue
		}
		if roles[resource.Metadata.Name] != "master" {
			if present {
				if removeErr := c.removeGracefulVIP(ctx, ifname, address); removeErr != nil {
					status.State, status.Reason, status.Error = "Failed", "VIPWithdrawFailed", removeErr.Error()
					resultErr = errors.Join(resultErr, removeErr)
				}
			}
			statuses[resource.Metadata.Name] = status
			continue
		}
		previous := c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name)
		startedAt := gracefulActivationStartedAt(previous, controllerNow(c))
		status.StartedAt = startedAt
		ready := resourcequery.ResourceWhenPresent(gate.ReadyWhen) && resourcequery.ResourceWhenMatches(gate.ReadyWhen, newVRRPWhenStore(c.Store))
		if !ready {
			if present {
				if removeErr := c.removeGracefulVIP(ctx, ifname, address); removeErr != nil {
					status.State, status.Reason, status.Error = "Failed", "VIPWithdrawFailed", removeErr.Error()
					statuses[resource.Metadata.Name] = status
					resultErr = errors.Join(resultErr, removeErr)
					continue
				}
			}
			status.State = "Preparing"
			status.WaitingFor = gracefulActivationWaitingFor(gate.ReadyWhen, newVRRPWhenStore(c.Store))
			if controllerNow(c).Sub(startedAt) >= timeout {
				status.State = "Failed"
				status.Reason = "ReadinessTimeout"
			}
			statuses[resource.Metadata.Name] = status
			continue
		}
		if !present {
			if applyErr := c.replaceStaticAddress(ctx, ifname, address); applyErr != nil {
				status.State, status.Reason, status.Error = "Failed", "VIPApplyFailed", applyErr.Error()
				statuses[resource.Metadata.Name] = status
				resultErr = errors.Join(resultErr, applyErr)
				continue
			}
			if announceErr := c.announceStaticIPv4Address(ctx, ifname, address); announceErr != nil {
				status.State, status.Reason, status.Error = "Failed", "VIPGratuitousARPFailed", announceErr.Error()
				statuses[resource.Metadata.Name] = status
				resultErr = errors.Join(resultErr, announceErr)
				continue
			}
		}
		status.State = "Ready"
		status.VIPAdvertised = true
		statuses[resource.Metadata.Name] = status
	}
	return statuses, resultErr
}

func (c *Controller) removeGracefulVIP(ctx context.Context, ifname, address string) error {
	ip := stringutil.FirstNonEmpty(c.IP, "ip")
	if out, err := c.run(ctx, ip, "addr", "del", address, "dev", ifname); err != nil {
		return fmt.Errorf("%s addr del %s dev %s: %w: %s", ip, address, ifname, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func gracefulActivationStartedAt(previous map[string]any, now time.Time) time.Time {
	if statusvalue.Field(previous, "role") == "master" {
		if parsed, err := time.Parse(time.RFC3339Nano, statusvalue.Field(previous, "activationStartedAt")); err == nil {
			return parsed
		}
	}
	return now
}

func gracefulActivationWaitingFor(when api.ResourceWhenSpec, store resourcequery.StateStore) []string {
	waiting := map[string]bool{}
	var walk func(api.ResourceWhenSpec)
	walk = func(current api.ResourceWhenSpec) {
		for ref, match := range current.State {
			if !resourcequery.StateMatch(store, ref, match) {
				waiting[ref] = true
			}
		}
		for _, child := range current.All {
			walk(child)
		}
		for _, child := range current.Any {
			walk(child)
		}
	}
	walk(when)
	out := make([]string, 0, len(waiting))
	for ref := range waiting {
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func controllerNow(c *Controller) time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func gracefulElectionStatePath(c *Controller, resource string) string {
	dir := strings.TrimSpace(c.VRRPRuntimeDir)
	if dir == "" {
		dir = "/run/routerd"
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(resource)))
	return fmt.Sprintf("%s/vrrp-election-%x.role", dir, sum[:8])
}

func gracefulElectionRole(c *Controller, resource string) string {
	readFile := os.ReadFile
	if c.ReadFile != nil {
		readFile = c.ReadFile
	}
	data, err := readFile(gracefulElectionStatePath(c, resource))
	if err != nil {
		return ""
	}
	role := strings.TrimSpace(string(data))
	if role == "master" || role == "backup" {
		return role
	}
	return ""
}

func seedGracefulElectionStates(ctx context.Context, c *Controller, aliases map[string]string) error {
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil || !ok || spec.Mode != "vrrp" || spec.VRRP.GracefulActivation == nil {
			if err != nil {
				return err
			}
			continue
		}
		if gracefulElectionRole(c, resource.Metadata.Name) != "" {
			continue
		}
		address, err := renderVirtualAddress(c.Router, spec)
		if err != nil {
			return err
		}
		ifname := aliases[spec.Interface]
		present, err := c.staticIPv4AddressPresent(ctx, ifname, address)
		if err != nil {
			return err
		}
		role := "backup"
		if present {
			role = "master"
		}
		path := gracefulElectionStatePath(c, resource.Metadata.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := writeFile(path, []byte(role+"\n"), 0644); err != nil {
			return err
		}
	}
	return nil
}

func reloadOrRestartSystemdKeepalived(ctx context.Context, c *Controller, path string) (string, error) {
	systemctl := stringutil.FirstNonEmpty(c.Systemctl, "systemctl")
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
	// Kernel VIP ownership is the authoritative role evidence.  Do not let a
	// cached systemd result hide an already-owned VIP.
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil || !ok {
			continue
		}
		if err != nil || spec.Mode != "vrrp" {
			continue
		}
		if spec.VRRP.GracefulActivation != nil {
			role := gracefulElectionRole(c, resource.Metadata.Name)
			if role == "master" || role == "backup" {
				if !c.refreshKeepalivedServiceActive(ctx) {
					role = "inactive"
				}
				roles[resource.Metadata.Name] = role
				continue
			}
		}
		ifname := aliases[spec.Interface]
		address, err := renderVirtualAddress(c.Router, spec)
		if err != nil || ifname == "" {
			roles[resource.Metadata.Name] = "unknown"
			continue
		}
		ip := stringutil.FirstNonEmpty(c.IP, "ip")
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
		if role != "master" && !c.refreshKeepalivedServiceActive(ctx) {
			role = "inactive"
		}
		roles[resource.Metadata.Name] = role
	}
	return roles
}

func (c *Controller) keepalivedServiceActive(ctx context.Context) bool {
	ttl := c.KeepalivedActiveTTL
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	if ttl > 0 && !c.keepalivedActiveCheckedAt.IsZero() && time.Since(c.keepalivedActiveCheckedAt) <= ttl {
		return c.keepalivedActiveCached
	}
	return c.refreshKeepalivedServiceActive(ctx)
}

func (c *Controller) refreshKeepalivedServiceActive(ctx context.Context) bool {
	systemctl := stringutil.FirstNonEmpty(c.Systemctl, "systemctl")
	_, err := c.run(ctx, systemctl, "is-active", "--quiet", "keepalived.service")
	active := err == nil
	c.keepalivedActiveCached = active
	c.keepalivedActiveCheckedAt = time.Now()
	return active
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
		kldload := stringutil.FirstNonEmpty(c.Kldload, "kldload")
		_, _ = c.run(ctx, kldload, "carp")
		sysctl := stringutil.FirstNonEmpty(c.Sysctl, "sysctl")
		wantedPreempt := config.PreemptSysctlValue()
		currentPreempt, currentErr := c.run(ctx, sysctl, "-n", "net.inet.carp.preempt")
		if currentErr != nil || strings.TrimSpace(string(currentPreempt)) != wantedPreempt {
			if out, err := c.run(ctx, sysctl, "net.inet.carp.preempt="+wantedPreempt); err != nil {
				return backendResult{}, c.saveError("", changed, nil, "CARPPreemptSysctlFailed", fmt.Errorf("%s net.inet.carp.preempt=%s: %w: %s", sysctl, wantedPreempt, err, strings.TrimSpace(string(out))))
			}
			changed = true
		}
		ifconfig := stringutil.FirstNonEmpty(c.Ifconfig, "ifconfig")
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
	ifconfig := stringutil.FirstNonEmpty(c.Ifconfig, "ifconfig")
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
