// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type TunnelCommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type TunnelInterfaceController struct {
	Router  *api.Router
	Bus     *bus.Bus
	Store   Store
	DryRun  bool
	Command TunnelCommandRunner
	OS      platform.OS
	Logger  *slog.Logger
}

type tunnelDesired struct {
	Name    string
	Mode    string
	Local   string
	Remote  string
	MTU     int
	TTL     int
	Key     int
	Address string
}

type tunnelObserved struct {
	Exists bool
	Mode   string
	Local  string
	Remote string
	MTU    int
	TTL    int
	Key    int
	Up     bool
}

func (c TunnelInterfaceController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	if targetOS == platform.OSLinux {
		if err := c.cleanupStaleResources(ctx); err != nil {
			return err
		}
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		if targetOS != platform.OSLinux {
			if err := c.saveUnsupportedStatus(resource, targetOS); err != nil {
				return err
			}
			continue
		}
		if err := c.reconcileInterface(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func (c TunnelInterfaceController) cleanupStaleResources(ctx context.Context) error {
	lister, ok := c.Store.(routerstate.ObjectStatusLister)
	if !ok {
		return nil
	}
	deleter, ok := c.Store.(routerstate.ObjectDeleteStore)
	if !ok {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	desired := map[string]struct{}{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "TunnelInterface" {
			desired[resource.Metadata.Name] = struct{}{}
		}
	}
	for _, item := range statuses {
		if item.APIVersion != api.HybridAPIVersion || item.Kind != "TunnelInterface" {
			continue
		}
		if _, ok := desired[item.Name]; ok || !routerdManagedObjectStatus(item) {
			continue
		}
		ifname := firstNonEmpty(statusString(item.Status, "ifname"), statusString(item.Status, "interface"), item.Name)
		if ifname != "" && !c.DryRun {
			if err := c.deleteTunnelInterface(ctx, ifname); err != nil {
				return err
			}
		}
		if err := deleter.DeleteObject(item.APIVersion, item.Kind, item.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c TunnelInterfaceController) saveUnsupportedStatus(resource api.Resource, targetOS platform.OS) error {
	spec, err := resource.TunnelInterfaceSpec()
	if err != nil {
		return err
	}
	desired := tunnelDesiredFromSpec(resource.Metadata.Name, spec)
	return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, tunnelStatus(desired, c.DryRun, map[string]any{
		"phase":  "Unsupported",
		"reason": "PlatformUnsupported",
		"os":     string(targetOS),
	}))
}

func (c TunnelInterfaceController) reconcileInterface(ctx context.Context, resource api.Resource) error {
	spec, err := resource.TunnelInterfaceSpec()
	if err != nil {
		return err
	}
	desired := tunnelDesiredFromSpec(resource.Metadata.Name, spec)
	status := tunnelStatus(desired, c.DryRun, map[string]any{"phase": "Pending"})
	if c.DryRun {
		status["phase"] = "Planned"
		return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
	}
	observed, err := c.observeTunnel(ctx, desired.Name)
	if err != nil {
		status["phase"] = "Error"
		status["reason"] = "StatusFailed"
		status["error"] = err.Error()
		return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
	}
	applied := false
	created := false
	if !observed.Exists {
		if err := c.addTunnelInterface(ctx, desired); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
		created = true
	} else if observed.Mode != "" && observed.Mode != desired.Mode {
		if err := c.deleteTunnelInterface(ctx, desired.Name); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		if err := c.addTunnelInterface(ctx, desired); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
		created = true
	} else if !tunnelLinkMatches(observed, desired) {
		if err := c.changeTunnelInterface(ctx, desired); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
	}
	if desired.MTU > 0 && (observed.MTU != desired.MTU || !observed.Up || created) {
		if err := c.setTunnelLink(ctx, desired); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
	} else if !observed.Up {
		if err := c.setTunnelLinkUp(ctx, desired.Name); err != nil {
			return c.saveApplyError(resource, desired, err)
		}
		applied = true
	}
	status = tunnelStatus(desired, c.DryRun, map[string]any{"phase": "Up"})
	if !applied {
		status["reason"] = "AlreadyConfigured"
	}
	if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status); err != nil {
		return err
	}
	if applied && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.tunnel.interface.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{
			"interface": desired.Name,
			"mode":      desired.Mode,
			"dryRun":    fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c TunnelInterfaceController) saveApplyError(resource api.Resource, desired tunnelDesired, applyErr error) error {
	status := tunnelStatus(desired, c.DryRun, map[string]any{
		"phase":  "Error",
		"reason": "ApplyFailed",
		"error":  applyErr.Error(),
	})
	return c.Store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", resource.Metadata.Name, status)
}

func tunnelDesiredFromSpec(name string, spec api.TunnelInterfaceSpec) tunnelDesired {
	mtu := spec.MTU
	if mtu == 0 {
		switch strings.TrimSpace(spec.Mode) {
		case "ipip":
			mtu = 1480
		case "gre":
			mtu = 1476
		}
	}
	ttl := spec.TTL
	if ttl == 0 {
		ttl = 64
	}
	return tunnelDesired{
		Name:    strings.TrimSpace(name),
		Mode:    strings.TrimSpace(spec.Mode),
		Local:   strings.TrimSpace(spec.Local),
		Remote:  strings.TrimSpace(spec.Remote),
		MTU:     mtu,
		TTL:     ttl,
		Key:     spec.Key,
		Address: strings.TrimSpace(spec.Address),
	}
}

func tunnelStatus(desired tunnelDesired, dryRun bool, extra map[string]any) map[string]any {
	status := map[string]any{
		"interface": desired.Name,
		"ifname":    desired.Name,
		"mode":      desired.Mode,
		"local":     desired.Local,
		"remote":    desired.Remote,
		"mtu":       desired.MTU,
		"ttl":       desired.TTL,
		"dryRun":    dryRun,
	}
	if desired.Key != 0 {
		status["key"] = desired.Key
	}
	if desired.Address != "" {
		status["address"] = desired.Address
	}
	for key, value := range extra {
		status[key] = value
	}
	return status
}

func tunnelLinkMatches(observed tunnelObserved, desired tunnelDesired) bool {
	if !observed.Exists {
		return false
	}
	if observed.Mode != "" && observed.Mode != desired.Mode {
		return false
	}
	if observed.Local != "" && observed.Local != desired.Local {
		return false
	}
	if observed.Remote != "" && observed.Remote != desired.Remote {
		return false
	}
	if observed.TTL != 0 && observed.TTL != desired.TTL {
		return false
	}
	if desired.Key != 0 && observed.Key != desired.Key {
		return false
	}
	return true
}

func (c TunnelInterfaceController) observeTunnel(ctx context.Context, ifname string) (tunnelObserved, error) {
	out, err := c.run(ctx, "ip", "-d", "-o", "link", "show", "dev", ifname)
	if err != nil {
		if tunnelMissingLink(out, err) {
			return tunnelObserved{}, nil
		}
		return tunnelObserved{}, fmt.Errorf("observe tunnel interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
	}
	observed := parseTunnelLinkStatus(out)
	observed.Exists = true
	return observed, nil
}

func parseTunnelLinkStatus(out []byte) tunnelObserved {
	text := string(out)
	fields := strings.Fields(text)
	observed := tunnelObserved{Exists: strings.TrimSpace(text) != ""}
	if strings.Contains(text, "<") && strings.Contains(text, "UP") {
		observed.Up = true
	}
	for i, field := range fields {
		switch {
		case field == "mtu" && i+1 < len(fields):
			observed.MTU, _ = strconv.Atoi(fields[i+1])
		case field == "ttl" && i+1 < len(fields):
			observed.TTL, _ = strconv.Atoi(fields[i+1])
		case field == "key" && i+1 < len(fields):
			observed.Key, _ = strconv.Atoi(fields[i+1])
		case field == "local" && i+1 < len(fields):
			observed.Local = strings.Trim(fields[i+1], ",")
		case (field == "remote" || field == "peer") && i+1 < len(fields):
			observed.Remote = strings.Trim(fields[i+1], ",")
		case field == "link/ipip" || strings.HasPrefix(field, "ipip/"):
			observed.Mode = "ipip"
		case field == "link/gre" || strings.HasPrefix(field, "gre/"):
			observed.Mode = "gre"
		}
	}
	return observed
}

func (c TunnelInterfaceController) addTunnelInterface(ctx context.Context, desired tunnelDesired) error {
	_, err := c.run(ctx, "ip", tunnelAddArgs(desired)...)
	return commandError("add tunnel interface "+desired.Name, err)
}

func (c TunnelInterfaceController) changeTunnelInterface(ctx context.Context, desired tunnelDesired) error {
	_, err := c.run(ctx, "ip", tunnelChangeArgs(desired)...)
	return commandError("change tunnel interface "+desired.Name, err)
}

func (c TunnelInterfaceController) deleteTunnelInterface(ctx context.Context, ifname string) error {
	out, err := c.run(ctx, "ip", "link", "del", "dev", ifname)
	if err == nil || tunnelMissingLink(out, err) {
		return nil
	}
	return fmt.Errorf("delete tunnel interface %s: %w: %s", ifname, err, strings.TrimSpace(string(out)))
}

func (c TunnelInterfaceController) setTunnelLink(ctx context.Context, desired tunnelDesired) error {
	args := []string{"link", "set", "dev", desired.Name}
	if desired.MTU > 0 {
		args = append(args, "mtu", strconv.Itoa(desired.MTU))
	}
	args = append(args, "up")
	_, err := c.run(ctx, "ip", args...)
	return commandError("set tunnel interface "+desired.Name, err)
}

func (c TunnelInterfaceController) setTunnelLinkUp(ctx context.Context, ifname string) error {
	_, err := c.run(ctx, "ip", "link", "set", "dev", ifname, "up")
	return commandError("bring tunnel interface "+ifname+" up", err)
}

func tunnelAddArgs(desired tunnelDesired) []string {
	args := []string{"link", "add", "dev", desired.Name, "type", desired.Mode, "local", desired.Local, "remote", desired.Remote, "ttl", strconv.Itoa(desired.TTL)}
	if desired.Mode == "gre" && desired.Key != 0 {
		args = append(args, "key", strconv.Itoa(desired.Key))
	}
	return args
}

func tunnelChangeArgs(desired tunnelDesired) []string {
	args := []string{"tunnel", "change", desired.Name, "mode", desired.Mode, "local", desired.Local, "remote", desired.Remote, "ttl", strconv.Itoa(desired.TTL)}
	if desired.Mode == "gre" && desired.Key != 0 {
		args = append(args, "key", strconv.Itoa(desired.Key))
	}
	return args
}

func (c TunnelInterfaceController) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	run := c.Command
	if run == nil {
		run = defaultTunnelCommandRunner
	}
	return run(ctx, name, args...)
}

func defaultTunnelCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func commandError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

func tunnelMissingLink(out []byte, err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(string(out)) + " " + err.Error())
	for _, needle := range []string{"cannot find device", "does not exist", "not found", "no such device"} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
