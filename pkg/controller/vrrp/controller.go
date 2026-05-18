// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/render"
	"routerd/pkg/resourcequery"
	routerstate "routerd/pkg/state"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type CommandFunc func(context.Context, string, ...string) ([]byte, error)

type Controller struct {
	Router          *api.Router
	Bus             *bus.Bus
	Store           Store
	DryRun          bool
	ConfigPath      string
	Systemctl       string
	KeepalivedCheck string
	IP              string
	Command         CommandFunc
	Logger          *slog.Logger
	trackState      map[string]trackDecision
}

func (c *Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	aliases := interfaceAliases(c.Router)
	cleanupChanged, err := c.cleanupStaleStaticAddresses(ctx, aliases)
	if err != nil {
		return err
	}
	if !hasVirtualIPv4(c.Router) {
		return nil
	}
	priorities, tracks := c.effectivePriorities()
	staticChanged, err := c.applyStaticAddresses(ctx, aliases)
	if err != nil {
		return err
	}
	data, err := render.KeepalivedConfigWithOptions(c.Router, aliases, render.KeepalivedOptions{PriorityByResource: priorities})
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return c.saveStatuses("Applied", "", cleanupChanged || staticChanged, tracks, nil, nil)
	}
	path := firstNonEmpty(c.ConfigPath, "/etc/keepalived/keepalived.conf")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	changed := true
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		changed = false
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if changed {
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
	}
	if !c.DryRun {
		if checker := strings.TrimSpace(c.KeepalivedCheck); checker != "" {
			if out, err := c.run(ctx, checker, "--config-test", "--use-file", path); err != nil {
				return c.saveError(path, changed || staticChanged, tracks, "KeepalivedConfigInvalid", fmt.Errorf("%s: %w: %s", checker, err, strings.TrimSpace(string(out))))
			}
		}
		systemctl := firstNonEmpty(c.Systemctl, "systemctl")
		if out, err := c.run(ctx, systemctl, "reload-or-restart", "keepalived.service"); err != nil {
			return c.saveError(path, changed || staticChanged, tracks, "KeepalivedRestartFailed", fmt.Errorf("%s reload-or-restart keepalived.service: %w: %s", systemctl, err, strings.TrimSpace(string(out))))
		}
	}
	roles := c.observeVRRPRoles(ctx, aliases)
	return c.saveStatuses("Applied", path, changed || cleanupChanged || staticChanged, tracks, roles, nil)
}

func (c *Controller) saveError(path string, changed bool, tracks map[string]trackSummary, reason string, err error) error {
	saveErr := c.saveStatuses("Error", path, changed, tracks, nil, map[string]any{"reason": reason, "error": err.Error()})
	if saveErr != nil {
		return saveErr
	}
	return err
}

func (c *Controller) saveStatuses(phase, path string, changed bool, tracks map[string]trackSummary, roles map[string]string, extra map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	aliases := interfaceAliases(c.Router)
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VirtualIPv4Address" {
			continue
		}
		spec, err := resource.VirtualIPv4AddressSpec()
		if err != nil {
			return err
		}
		address := spec.Address
		if resolved, err := render.VirtualIPv4Address(c.Router, spec); err == nil {
			address = resolved
		}
		status := map[string]any{
			"phase":      phase,
			"backend":    virtualIPv4Backend(spec),
			"address":    address,
			"interface":  spec.Interface,
			"ifname":     aliases[spec.Interface],
			"configPath": path,
			"changed":    changed,
			"dryRun":     c.DryRun,
			"observedAt": now,
		}
		if spec.Mode == "vrrp" {
			track := tracks[resource.Metadata.Name]
			status["virtualRouterID"] = spec.VRRP.VirtualRouterID
			status["priority"] = track.EffectivePriority
			status["basePriority"] = track.BasePriority
			status["preempt"] = spec.VRRP.Preempt != nil && *spec.VRRP.Preempt
			status["track"] = track.Entries
			status["role"] = firstNonEmpty(roles[resource.Metadata.Name], "unknown")
		} else {
			status["desiredAddress"] = address
			if !c.DryRun {
				if phase == "Applied" {
					status["appliedAddress"] = address
				} else if previous := statusString(c.Store.ObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", resource.Metadata.Name), "appliedAddress"); previous != "" {
					status["appliedAddress"] = previous
				}
			}
		}
		for key, value := range extra {
			status[key] = value
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

type staticVIP struct {
	IfName  string
	Address string
}

func (c *Controller) cleanupStaleStaticAddresses(ctx context.Context, aliases map[string]string) (bool, error) {
	lister, ok := c.Store.(routerstate.ObjectStatusLister)
	if !ok {
		return false, nil
	}
	desired := map[string]staticVIP{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VirtualIPv4Address" {
			continue
		}
		spec, err := resource.VirtualIPv4AddressSpec()
		if err != nil || (strings.TrimSpace(spec.Mode) != "" && spec.Mode != "static") {
			continue
		}
		address, err := render.VirtualIPv4Address(c.Router, spec)
		if err != nil {
			continue
		}
		desired[resource.Metadata.Name] = staticVIP{IfName: aliases[spec.Interface], Address: address}
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return false, err
	}
	changed := false
	for _, item := range statuses {
		if item.APIVersion != api.NetAPIVersion || item.Kind != "VirtualIPv4Address" || strings.TrimSpace(statusString(item.Status, "backend")) != "iproute2" {
			continue
		}
		previous := staticVIP{IfName: statusString(item.Status, "ifname"), Address: statusString(item.Status, "appliedAddress")}
		if previous.Address == "" && statusString(item.Status, "phase") != "Removed" {
			previous.Address = statusString(item.Status, "address")
		}
		if previous.IfName == "" || previous.Address == "" {
			continue
		}
		if current, ok := desired[item.Name]; ok && current.IfName == previous.IfName && current.Address == previous.Address {
			continue
		}
		changed = true
		if !c.DryRun {
			ip := firstNonEmpty(c.IP, "ip")
			if out, err := c.run(ctx, ip, "addr", "del", previous.Address, "dev", previous.IfName); err != nil {
				return changed, fmt.Errorf("%s addr del %s dev %s: %w: %s", ip, previous.Address, previous.IfName, err, strings.TrimSpace(string(out)))
			}
		}
		if !c.DryRun {
			status := map[string]any{
				"phase":          "Removed",
				"backend":        "iproute2",
				"address":        previous.Address,
				"appliedAddress": "",
				"ifname":         previous.IfName,
				"changed":        true,
				"dryRun":         c.DryRun,
				"observedAt":     time.Now().UTC().Format(time.RFC3339Nano),
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", item.Name, status); err != nil {
				return changed, err
			}
		}
	}
	return changed, nil
}

func (c *Controller) observeVRRPRoles(ctx context.Context, aliases map[string]string) map[string]string {
	roles := map[string]string{}
	if c.DryRun {
		for _, resource := range c.Router.Spec.Resources {
			if resource.APIVersion == api.NetAPIVersion && resource.Kind == "VirtualIPv4Address" {
				roles[resource.Metadata.Name] = "dryrun"
			}
		}
		return roles
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VirtualIPv4Address" {
			continue
		}
		spec, err := resource.VirtualIPv4AddressSpec()
		if err != nil || spec.Mode != "vrrp" {
			continue
		}
		ifname := aliases[spec.Interface]
		address, err := render.VirtualIPv4Address(c.Router, spec)
		if err != nil || ifname == "" {
			roles[resource.Metadata.Name] = "unknown"
			continue
		}
		ip := firstNonEmpty(c.IP, "ip")
		out, err := c.run(ctx, ip, "-4", "addr", "show", "dev", ifname)
		if err != nil {
			roles[resource.Metadata.Name] = "unknown"
			continue
		}
		if ipv4AddressPresent(string(out), address) {
			roles[resource.Metadata.Name] = "master"
		} else {
			roles[resource.Metadata.Name] = "backup"
		}
	}
	return roles
}

func (c *Controller) applyStaticAddresses(ctx context.Context, aliases map[string]string) (bool, error) {
	changed := false
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VirtualIPv4Address" {
			continue
		}
		spec, err := resource.VirtualIPv4AddressSpec()
		if err != nil {
			return changed, err
		}
		if strings.TrimSpace(spec.Mode) != "" && spec.Mode != "static" {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return changed, fmt.Errorf("%s references interface with empty ifname %q", resource.ID(), spec.Interface)
		}
		address, err := render.VirtualIPv4Address(c.Router, spec)
		if err != nil {
			return changed, fmt.Errorf("%s spec.address: %w", resource.ID(), err)
		}
		changed = true
		if c.DryRun {
			continue
		}
		ip := firstNonEmpty(c.IP, "ip")
		if out, err := c.run(ctx, ip, "addr", "replace", address, "dev", ifname); err != nil {
			return changed, c.saveError("", changed, nil, "StaticVIPApplyFailed", fmt.Errorf("%s addr replace %s dev %s: %w: %s", ip, address, ifname, err, strings.TrimSpace(string(out))))
		}
	}
	return changed, nil
}

type trackSummary struct {
	BasePriority      int
	EffectivePriority int
	Entries           []map[string]any
}

type trackDecision struct {
	HealthyCount   int
	UnhealthyCount int
	Penalized      bool
}

func (c *Controller) effectivePriorities() (map[string]int, map[string]trackSummary) {
	priorities := map[string]int{}
	summaries := map[string]trackSummary{}
	if c.trackState == nil {
		c.trackState = map[string]trackDecision{}
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VirtualIPv4Address" {
			continue
		}
		spec, err := resource.VirtualIPv4AddressSpec()
		if err != nil || spec.Mode != "vrrp" {
			continue
		}
		base := spec.VRRP.Priority
		if base == 0 {
			base = 100
		}
		effective := base
		var entries []map[string]any
		for _, track := range spec.Track {
			kind, name, ok := resourcequery.SplitResource(track.Resource)
			if !ok {
				continue
			}
			status := c.Store.ObjectStatus(resourcequery.APIVersionForKind(kind), kind, name)
			phase := fmt.Sprint(status["phase"])
			healthy := trackedPhaseHealthy(kind, phase)
			penalty := track.UnhealthyPenalty
			if penalty == 0 {
				penalty = 50
			}
			decision := c.confirmTrack(resource.Metadata.Name, track, healthy)
			if decision.Penalized {
				effective -= penalty
			}
			entries = append(entries, map[string]any{
				"resource":                    track.Resource,
				"phase":                       phase,
				"healthy":                     healthy,
				"penalty":                     penalty,
				"penalized":                   decision.Penalized,
				"healthyCount":                decision.HealthyCount,
				"unhealthyCount":              decision.UnhealthyCount,
				"confirmConsecutiveHealthy":   defaultInt(track.ConfirmConsecutiveHealthy, 2),
				"confirmConsecutiveUnhealthy": defaultInt(track.ConfirmConsecutiveUnhealthy, 3),
			})
		}
		if effective < 1 {
			effective = 1
		}
		priorities[resource.Metadata.Name] = effective
		summaries[resource.Metadata.Name] = trackSummary{BasePriority: base, EffectivePriority: effective, Entries: entries}
	}
	return priorities, summaries
}

func (c *Controller) confirmTrack(vip string, track api.ResourceTrackSpec, healthy bool) trackDecision {
	key := vip + "\x00" + track.Resource
	decision, ok := c.trackState[key]
	if !ok {
		decision = c.restoreTrackDecision(vip, track.Resource)
	}
	if healthy {
		decision.HealthyCount++
		decision.UnhealthyCount = 0
		if decision.Penalized && decision.HealthyCount >= defaultInt(track.ConfirmConsecutiveHealthy, 2) {
			decision.Penalized = false
		}
	} else {
		decision.UnhealthyCount++
		decision.HealthyCount = 0
		if !decision.Penalized && decision.UnhealthyCount >= defaultInt(track.ConfirmConsecutiveUnhealthy, 3) {
			decision.Penalized = true
		}
	}
	c.trackState[key] = decision
	return decision
}

func (c *Controller) restoreTrackDecision(vip, trackedResource string) trackDecision {
	if c.Store == nil {
		return trackDecision{}
	}
	status := c.Store.ObjectStatus(api.NetAPIVersion, "VirtualIPv4Address", vip)
	for _, entry := range trackEntries(status["track"]) {
		if strings.TrimSpace(fmt.Sprint(entry["resource"])) != trackedResource {
			continue
		}
		return trackDecision{
			HealthyCount:   statusInt(entry["healthyCount"]),
			UnhealthyCount: statusInt(entry["unhealthyCount"]),
			Penalized:      statusBool(entry["penalized"]),
		}
	}
	return trackDecision{}
}

func trackEntries(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if entry, ok := item.(map[string]any); ok {
				out = append(out, entry)
			}
		}
		return out
	default:
		return nil
	}
}

func statusInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func statusBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func trackedPhaseHealthy(kind, phase string) bool {
	switch kind {
	case "BGPRouter", "BGPPeer":
		return phase == "Established"
	case "IngressService":
		return phase == "Active" || phase == "Healthy" || phase == "Applied"
	default:
		switch phase {
		case "Applied", "Bound", "Healthy", "Installed", "Ready", "Running", "Up", "Established", "Active":
			return true
		default:
			return false
		}
	}
}

func virtualIPv4Backend(spec api.VirtualIPv4AddressSpec) string {
	if strings.TrimSpace(spec.Mode) == "vrrp" {
		return "keepalived"
	}
	return "iproute2"
}

func hasVirtualIPv4(router *api.Router) bool {
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "VirtualIPv4Address" {
			return true
		}
	}
	return false
}

func (c *Controller) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.Command != nil {
		return c.Command(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func ipv4AddressPresent(output, address string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == "inet" && i+1 < len(fields) && fields[i+1] == address {
				return true
			}
		}
	}
	return false
}

func statusString(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	value, ok := status[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func interfaceAliases(router *api.Router) map[string]string {
	aliases := map[string]string{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err == nil {
			aliases[resource.Metadata.Name] = spec.IfName
		}
	}
	return aliases
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
