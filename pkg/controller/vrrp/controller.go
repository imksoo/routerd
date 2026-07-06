// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type CommandFunc func(context.Context, string, ...string) ([]byte, error)

type Controller struct {
	Router                    *api.Router
	Bus                       *bus.Bus
	Store                     Store
	DryRun                    bool
	ConfigPath                string
	Systemctl                 string
	KeepalivedCheck           string
	IP                        string
	Ifconfig                  string
	Sysctl                    string
	Kldload                   string
	OperatingSystem           platform.OS
	Command                   CommandFunc
	Logger                    *slog.Logger
	KeepalivedActiveTTL       time.Duration
	trackState                map[string]trackDecision
	keepalivedActiveCheckedAt time.Time
	keepalivedActiveCached    bool
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
	if !hasVirtualAddress(c.Router) {
		return c.stopVirtualAddressBackend(ctx)
	}
	priorities, tracks := c.effectivePriorities()
	staticChanged, staticIsolated, err := c.applyStaticAddresses(ctx, aliases)
	if err != nil {
		return err
	}
	backend := c.vrrpBackend()
	result, err := backend.Apply(ctx, c, aliases, priorities)
	if err != nil {
		return err
	}
	extra := map[string]any{}
	if result.LastReloadAt != "" {
		extra["lastReloadAt"] = result.LastReloadAt
	}
	if result.LastRestartAt != "" {
		extra["lastRestartAt"] = result.LastRestartAt
	}
	if result.LastChangeReason != "" {
		extra["lastChangeReason"] = result.LastChangeReason
	}
	if result.ServiceActive != nil {
		extra["serviceActive"] = *result.ServiceActive
	}
	return c.saveStatuses("Applied", result.Path, result.Changed || cleanupChanged || staticChanged, tracks, result.Roles, staticIsolated, extra)
}

func (c *Controller) stopVirtualAddressBackend(ctx context.Context) error {
	if c.DryRun {
		return nil
	}
	path := firstNonEmpty(c.ConfigPath, "/etc/keepalived/keepalived.conf")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	systemctl := firstNonEmpty(c.Systemctl, "systemctl")
	if _, err := c.run(ctx, systemctl, "is-active", "--quiet", "keepalived.service"); err != nil {
		return nil
	}
	if out, err := c.run(ctx, systemctl, "stop", "keepalived.service"); err != nil {
		return fmt.Errorf("%s stop keepalived.service: %w: %s", systemctl, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Controller) saveError(path string, changed bool, tracks map[string]trackSummary, reason string, err error) error {
	saveErr := c.saveStatuses("Error", path, changed, tracks, nil, nil, map[string]any{"reason": reason, "error": err.Error()})
	if saveErr != nil {
		return saveErr
	}
	return err
}

func (c *Controller) saveStatuses(phase, path string, changed bool, tracks map[string]trackSummary, roles map[string]string, isolated map[string]bool, extra map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	aliases := interfaceAliases(c.Router)
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if isolated[resource.Metadata.Name] {
			continue
		}
		address := spec.Address
		if resolved, err := renderVirtualAddress(c.Router, spec); err == nil {
			address = resolved
		}
		status := map[string]any{
			"phase":      phase,
			"backend":    c.virtualAddressBackend(spec),
			"address":    address,
			"hostname":   strings.TrimSpace(spec.Hostname),
			"interface":  spec.Interface,
			"ifname":     aliases[spec.Interface],
			"configPath": path,
			"changed":    changed,
			"dryRun":     c.DryRun,
			"observedAt": now,
		}
		if spec.Mode == "vrrp" {
			track := tracks[resource.Metadata.Name]
			role := firstNonEmpty(roles[resource.Metadata.Name], "unknown")
			status["virtualRouterID"] = spec.VRRP.VirtualRouterID
			status["priority"] = track.EffectivePriority
			status["basePriority"] = track.BasePriority
			status["preempt"] = spec.VRRP.Preempt != nil && *spec.VRRP.Preempt
			status["track"] = track.Entries
			status["role"] = role
			previous := c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name)
			carryBackendActionStatus(status, previous, extra)
			if statusString(previous, "role") == role && statusString(previous, "lastRoleTransitionAt") != "" {
				status["lastRoleTransitionAt"] = statusString(previous, "lastRoleTransitionAt")
			} else {
				status["lastRoleTransitionAt"] = now
			}
		} else {
			status["desiredAddress"] = address
			if !c.DryRun {
				if phase == "Applied" {
					status["appliedAddress"] = address
				} else if previous := statusString(c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name), "appliedAddress"); previous != "" {
					status["appliedAddress"] = previous
				}
			}
		}
		for key, value := range extra {
			status[key] = value
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name, status); err != nil {
			return err
		}
	}
	return nil
}

func carryBackendActionStatus(status, previous map[string]any, extra map[string]any) {
	for _, key := range []string{"lastReloadAt", "lastRestartAt", "lastChangeReason"} {
		if value, ok := extra[key]; ok && fmt.Sprint(value) != "" {
			status[key] = value
			continue
		}
		if value := statusString(previous, key); value != "" {
			status[key] = value
		}
	}
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
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil || !ok {
			continue
		}
		if err != nil || (strings.TrimSpace(spec.Mode) != "" && spec.Mode != "static") {
			continue
		}
		address, err := renderVirtualAddress(c.Router, spec)
		if err != nil {
			continue
		}
		desired[resource.Kind+"\x00"+resource.Metadata.Name] = staticVIP{IfName: aliases[spec.Interface], Address: address}
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return false, err
	}
	changed := false
	for _, item := range statuses {
		backend := strings.TrimSpace(statusString(item.Status, "backend"))
		if item.APIVersion != api.NetAPIVersion || !isVirtualAddressKind(item.Kind) || (backend != "iproute2" && backend != "ifconfig") {
			continue
		}
		previous := staticVIP{IfName: statusString(item.Status, "ifname"), Address: statusString(item.Status, "appliedAddress")}
		if previous.Address == "" && statusString(item.Status, "phase") != "Removed" {
			previous.Address = statusString(item.Status, "address")
		}
		if previous.IfName == "" || previous.Address == "" {
			continue
		}
		if current, ok := desired[item.Kind+"\x00"+item.Name]; ok && current.IfName == previous.IfName && current.Address == previous.Address {
			continue
		}
		changed = true
		if !c.DryRun {
			if err := c.removeStaticAddress(ctx, previous.IfName, previous.Address); err != nil {
				return changed, err
			}
		}
		if !c.DryRun {
			status := map[string]any{
				"phase":          "Removed",
				"backend":        backend,
				"address":        previous.Address,
				"appliedAddress": "",
				"ifname":         previous.IfName,
				"changed":        true,
				"dryRun":         c.DryRun,
				"observedAt":     time.Now().UTC().Format(time.RFC3339Nano),
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, item.Kind, item.Name, status); err != nil {
				return changed, err
			}
		}
	}
	return changed, nil
}

func (c *Controller) applyStaticAddresses(ctx context.Context, aliases map[string]string) (bool, map[string]bool, error) {
	changed := false
	isolated := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil {
			return changed, isolated, err
		}
		if !ok {
			continue
		}
		if strings.TrimSpace(spec.Mode) != "" && spec.Mode != "static" {
			continue
		}
		ifname := aliases[spec.Interface]
		if ifname == "" {
			return changed, isolated, fmt.Errorf("%s references interface with empty ifname %q", resource.ID(), spec.Interface)
		}
		address, err := renderVirtualAddress(c.Router, spec)
		if err != nil {
			phase := "Error"
			reason := "AddressInvalid"
			if pending := staticVirtualAddressPendingReason(c.Router, spec); pending != "" {
				phase = "Pending"
				reason = pending
			}
			if saveErr := c.saveStaticAddressStatus(resource, spec, aliases, phase, changed, reason, err); saveErr != nil {
				return changed, isolated, saveErr
			}
			isolated[resource.Metadata.Name] = true
			continue
		}
		changed = true
		if c.DryRun {
			continue
		}
		if err := c.replaceStaticAddress(ctx, ifname, address); err != nil {
			return changed, isolated, c.saveError("", changed, nil, "StaticVIPApplyFailed", err)
		}
	}
	return changed, isolated, nil
}

func (c *Controller) saveStaticAddressStatus(resource api.Resource, spec virtualAddressSpec, aliases map[string]string, phase string, changed bool, reason string, applyErr error) error {
	address := strings.TrimSpace(spec.Address)
	status := map[string]any{
		"phase":          phase,
		"backend":        c.virtualAddressBackend(spec),
		"address":        address,
		"hostname":       strings.TrimSpace(spec.Hostname),
		"interface":      spec.Interface,
		"ifname":         aliases[spec.Interface],
		"configPath":     "",
		"changed":        changed,
		"dryRun":         c.DryRun,
		"observedAt":     time.Now().UTC().Format(time.RFC3339Nano),
		"desiredAddress": address,
		"reason":         reason,
	}
	if applyErr != nil {
		status["error"] = applyErr.Error()
	}
	if previous := statusString(c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name), "appliedAddress"); previous != "" {
		status["appliedAddress"] = previous
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name, status)
}

func staticVirtualAddressPendingReason(router *api.Router, spec virtualAddressSpec) string {
	if strings.TrimSpace(spec.Address) != "" || strings.TrimSpace(spec.AddressFrom.Resource) == "" || spec.AddressFrom.Optional {
		return ""
	}
	kind, name, ok := strings.Cut(strings.TrimSpace(spec.AddressFrom.Resource), "/")
	if !ok || kind == "" || name == "" {
		return ""
	}
	field := strings.TrimSpace(spec.AddressFrom.Field)
	if field == "" {
		field = "address"
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != kind || res.Metadata.Name != name {
			continue
		}
		switch kind {
		case "IPv4StaticAddress":
			if field != "address" {
				return ""
			}
			source, err := res.IPv4StaticAddressSpec()
			if err != nil || strings.TrimSpace(source.Address) != "" {
				return ""
			}
		case "VirtualAddress":
			if field != "address" {
				return ""
			}
			source, err := res.VirtualAddressSpec()
			if err != nil || strings.TrimSpace(source.Address) != "" {
				return ""
			}
		default:
			return ""
		}
		return "AddressUnresolved: " + spec.AddressFrom.Resource
	}
	// The referenced resource is absent from the config: a real misconfiguration
	// (typo), not a bootstrap-ordering wait. Return "" so the caller reports Error.
	return ""
}

type trackSummary struct {
	BasePriority      int
	EffectivePriority int
	Entries           []map[string]any
}

type virtualAddressSpec struct {
	Interface   string
	Address     string
	Hostname    string
	Mode        string
	VRRP        virtualVRRPSpec
	Track       []api.ResourceTrackSpec
	AddressFrom api.StatusValueSourceSpec
	Family      string
}

type virtualVRRPSpec struct {
	VirtualRouterID    int
	Priority           int
	Preempt            *bool
	PreemptDelay       string
	Peers              []string
	AdvertInterval     string
	Authentication     string
	AuthenticationFrom api.SecretValueSourceSpec
}

func vrrpResourceSpec(resource api.Resource) (virtualAddressSpec, bool, error) {
	if resource.APIVersion != api.NetAPIVersion {
		return virtualAddressSpec{}, false, nil
	}
	switch resource.Kind {
	case "VirtualAddress":
		spec, err := resource.VirtualAddressSpec()
		if err != nil {
			return virtualAddressSpec{}, false, err
		}
		return virtualAddressSpec{
			Interface:   spec.Interface,
			Address:     spec.Address,
			Hostname:    spec.Hostname,
			Mode:        spec.Mode,
			VRRP:        vrrpSpec(spec.VRRP),
			Track:       spec.Track,
			AddressFrom: spec.AddressFrom,
			Family:      spec.Family,
		}, true, nil
	default:
		return virtualAddressSpec{}, false, nil
	}
}

func vrrpSpec(spec api.VirtualAddressVRRPSpec) virtualVRRPSpec {
	return virtualVRRPSpec{
		VirtualRouterID:    spec.VirtualRouterID,
		Priority:           spec.Priority,
		Preempt:            spec.Preempt,
		PreemptDelay:       spec.PreemptDelay,
		Peers:              spec.Peers,
		AdvertInterval:     spec.AdvertInterval,
		Authentication:     spec.Authentication,
		AuthenticationFrom: spec.AuthenticationFrom,
	}
}

func renderVirtualAddress(router *api.Router, spec virtualAddressSpec) (string, error) {
	return render.VirtualAddress(router, api.VirtualAddressSpec{Family: spec.Family, Address: spec.Address, AddressFrom: spec.AddressFrom})
}

func isVirtualAddressKind(kind string) bool {
	return kind == "VirtualAddress"
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
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil || !ok {
			continue
		}
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
			decision := c.confirmTrack(resource.Kind, resource.Metadata.Name, track, healthy)
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

func (c *Controller) confirmTrack(kind, vip string, track api.ResourceTrackSpec, healthy bool) trackDecision {
	key := kind + "\x00" + vip + "\x00" + track.Resource
	decision, ok := c.trackState[key]
	if !ok {
		decision = c.restoreTrackDecision(kind, vip, track.Resource)
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

func (c *Controller) restoreTrackDecision(kind, vip, trackedResource string) trackDecision {
	if c.Store == nil {
		return trackDecision{}
	}
	status := c.Store.ObjectStatus(api.NetAPIVersion, kind, vip)
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

func (c *Controller) virtualAddressBackend(spec virtualAddressSpec) string {
	if strings.TrimSpace(spec.Mode) == "vrrp" {
		return c.vrrpBackend().Name()
	}
	if c.currentOS() == platform.OSFreeBSD {
		return "ifconfig"
	}
	return "iproute2"
}

func (c *Controller) currentOS() platform.OS {
	if c.OperatingSystem != "" {
		return c.OperatingSystem
	}
	return platform.CurrentOS()
}

func hasVirtualAddress(router *api.Router) bool {
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && isVirtualAddressKind(resource.Kind) {
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

func (c *Controller) replaceStaticAddress(ctx context.Context, ifname, address string) error {
	if c.currentOS() == platform.OSFreeBSD {
		ifconfig := firstNonEmpty(c.Ifconfig, "ifconfig")
		family := ifconfigAddressFamily(address)
		if out, err := c.run(ctx, ifconfig, ifname, family, address, "alias"); err != nil {
			return fmt.Errorf("%s %s %s %s alias: %w: %s", ifconfig, ifname, family, address, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	ip := firstNonEmpty(c.IP, "ip")
	if out, err := c.run(ctx, ip, "addr", "replace", address, "dev", ifname); err != nil {
		return fmt.Errorf("%s addr replace %s dev %s: %w: %s", ip, address, ifname, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Controller) removeStaticAddress(ctx context.Context, ifname, address string) error {
	if c.currentOS() == platform.OSFreeBSD {
		ifconfig := firstNonEmpty(c.Ifconfig, "ifconfig")
		family := ifconfigAddressFamily(address)
		if out, err := c.run(ctx, ifconfig, ifname, family, address, "-alias"); err != nil {
			return fmt.Errorf("%s %s %s %s -alias: %w: %s", ifconfig, ifname, family, address, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	ip := firstNonEmpty(c.IP, "ip")
	if out, err := c.run(ctx, ip, "addr", "del", address, "dev", ifname); err != nil {
		return fmt.Errorf("%s addr del %s dev %s: %w: %s", ip, address, ifname, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ifconfigAddressFamily(address string) string {
	if before, _, ok := strings.Cut(strings.TrimSpace(address), "/"); ok {
		address = before
	}
	if strings.Contains(address, ":") {
		return "inet6"
	}
	return "inet"
}

func ipv4AddressPresent(output, address string) bool {
	return ipAddressPresent(output, address, "ipv4")
}

func ipAddressPresent(output, address, family string) bool {
	token := "inet"
	if family == "ipv6" {
		token = "inet6"
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == token && i+1 < len(fields) && fields[i+1] == address {
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
