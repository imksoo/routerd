// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/internal/stringutil"
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
	Arping                    string
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
	if result.VMACRepairError != nil {
		extra["vmacRepairError"] = result.VMACRepairError.Error()
	}
	if err := c.saveStatuses("Applied", result.Path, result.Changed || cleanupChanged || staticChanged, tracks, result.Roles, staticIsolated, extra); err != nil {
		return err
	}
	return result.VMACRepairError
}

func (c *Controller) stopVirtualAddressBackend(ctx context.Context) error {
	if c.DryRun {
		return nil
	}
	path := stringutil.FirstNonEmpty(c.ConfigPath, "/etc/keepalived/keepalived.conf")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	systemctl := stringutil.FirstNonEmpty(c.Systemctl, "systemctl")
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
			role := stringutil.FirstNonEmpty(roles[resource.Metadata.Name], "unknown")
			previous := c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name)
			desiredVMACs := len(spec.VRRP.AdditionalFailoverVMACs)
			if spec.VRRP.FailoverVMAC != nil {
				desiredVMACs++
			}
			status["virtualRouterID"] = spec.VRRP.VirtualRouterID
			status["priority"] = track.EffectivePriority
			status["basePriority"] = track.BasePriority
			status["preempt"] = spec.VRRP.Preempt != nil && *spec.VRRP.Preempt
			status["track"] = track.Entries
			status["role"] = role
			status["failoverVMACs"] = desiredVMACs
			carryBackendActionStatus(status, previous, extra)
			if statusvalue.Field(previous, "role") == role && statusvalue.Field(previous, "lastRoleTransitionAt") != "" {
				status["lastRoleTransitionAt"] = statusvalue.Field(previous, "lastRoleTransitionAt")
			} else {
				status["lastRoleTransitionAt"] = now
			}
		} else {
			status["desiredAddress"] = address
			if !c.DryRun {
				if phase == "Applied" {
					status["appliedAddress"] = address
				} else if previous := statusvalue.Field(c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name), "appliedAddress"); previous != "" {
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
		if value := statusvalue.Field(previous, key); value != "" {
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
		backend := strings.TrimSpace(statusvalue.Field(item.Status, "backend"))
		whenFalse := statusvalue.Field(item.Status, "phase") == "Pending" && statusvalue.Field(item.Status, "reason") == "WhenFalse"
		if item.APIVersion != api.NetAPIVersion || !isVirtualAddressKind(item.Kind) || ((backend != "iproute2" && backend != "ifconfig") && !whenFalse) {
			continue
		}
		if backend == "" && whenFalse {
			backend = c.staticVirtualAddressBackend()
		}
		previous := staticVIP{IfName: statusvalue.Field(item.Status, "ifname"), Address: statusvalue.Field(item.Status, "appliedAddress")}
		if previous.IfName == "" && whenFalse {
			previous.IfName = aliases[statusvalue.Field(item.Status, "interface")]
		}
		if previous.Address == "" && statusvalue.Field(item.Status, "phase") != "Removed" && (!whenFalse || item.Status["staticAddressRemoved"] != true) {
			previous.Address = statusvalue.Field(item.Status, "address")
		}
		if previous.IfName == "" || previous.Address == "" {
			continue
		}
		if current, ok := desired[item.Kind+"\x00"+item.Name]; ok && current.IfName == previous.IfName && current.Address == previous.Address {
			continue
		}
		changed = true
		if !c.DryRun {
			if err := c.removeStaticAddress(ctx, previous.IfName, previous.Address); err != nil && !(whenFalse && staticAddressAlreadyAbsent(err)) {
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
			if whenFalse {
				status["phase"] = "Pending"
				status["reason"] = "WhenFalse"
				status["staticAddressRemoved"] = true
			}
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, item.Kind, item.Name, status); err != nil {
				return changed, err
			}
		}
	}
	return changed, nil
}

func (c *Controller) staticVirtualAddressBackend() string {
	if c.currentOS() == platform.OSFreeBSD {
		return "ifconfig"
	}
	return "iproute2"
}

func staticAddressAlreadyAbsent(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "cannot assign requested address") ||
		strings.Contains(message, "can't assign requested address") ||
		strings.Contains(message, "address not found") ||
		strings.Contains(message, "does not exist")
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
		announce := false
		_, isIPv4 := staticIPv4Host(address)
		if spec.GratuitousARP && isIPv4 && c.currentOS() == platform.OSLinux {
			addressPresent, err := c.staticIPv4AddressPresent(ctx, ifname, address)
			if err != nil {
				return changed, isolated, c.saveError("", changed, nil, "StaticVIPObserveFailed", err)
			}
			previous := c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name)
			announce = !addressPresent || statusvalue.Field(previous, "reason") == "StaticVIPGratuitousARPFailed"
		}
		if err := c.replaceStaticAddress(ctx, ifname, address); err != nil {
			return changed, isolated, c.saveError("", changed, nil, "StaticVIPApplyFailed", err)
		}
		if announce {
			if err := c.announceStaticIPv4Address(ctx, ifname, address); err != nil {
				return changed, isolated, c.saveError("", changed, nil, "StaticVIPGratuitousARPFailed", err)
			}
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
	if previous := statusvalue.Field(c.Store.ObjectStatus(api.NetAPIVersion, resource.Kind, resource.Metadata.Name), "appliedAddress"); previous != "" {
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
	Interface     string
	Address       string
	Hostname      string
	Mode          string
	VRRP          virtualVRRPSpec
	Track         []api.ResourceTrackSpec
	AddressFrom   api.StatusValueSourceSpec
	Family        string
	GratuitousARP bool
}

type virtualVRRPSpec struct {
	VirtualRouterID         int
	Priority                int
	Preempt                 *bool
	PreemptDelay            string
	Peers                   []string
	AdvertInterval          string
	Authentication          string
	AuthenticationFrom      api.SecretValueSourceSpec
	FailoverVMAC            *api.VirtualAddressVRRPFailoverVMACSpec
	AdditionalFailoverVMACs []api.VirtualAddressVRRPFailoverVMACSpec
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
			Interface:     spec.Interface,
			Address:       spec.Address,
			Hostname:      spec.Hostname,
			Mode:          spec.Mode,
			VRRP:          vrrpSpec(spec.VRRP),
			Track:         spec.Track,
			AddressFrom:   spec.AddressFrom,
			Family:        spec.Family,
			GratuitousARP: spec.GratuitousARP,
		}, true, nil
	default:
		return virtualAddressSpec{}, false, nil
	}
}

func vrrpSpec(spec api.VirtualAddressVRRPSpec) virtualVRRPSpec {
	return virtualVRRPSpec{
		VirtualRouterID:         spec.VirtualRouterID,
		Priority:                spec.Priority,
		Preempt:                 spec.Preempt,
		PreemptDelay:            spec.PreemptDelay,
		Peers:                   spec.Peers,
		AdvertInterval:          spec.AdvertInterval,
		Authentication:          spec.Authentication,
		AuthenticationFrom:      spec.AuthenticationFrom,
		FailoverVMAC:            spec.FailoverVMAC,
		AdditionalFailoverVMACs: spec.AdditionalFailoverVMACs,
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

type trackedResourceState int

const (
	trackedResourceUnhealthy trackedResourceState = iota
	trackedResourceHealthy
	trackedResourceNeutral
)

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
			state := trackedPhaseState(kind, phase, fmt.Sprint(status["reason"]))
			if c.trackedResourceWhenFalse(kind, name) {
				state = trackedResourceNeutral
			}
			healthy := state == trackedResourceHealthy
			penalty := track.UnhealthyPenalty
			if penalty == 0 {
				penalty = 50
			}
			decision := c.trackStateFor(resource.Kind, resource.Metadata.Name, track, state)
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

func (c *Controller) trackedResourceWhenFalse(kind, name string) bool {
	if c.Router == nil || c.Store == nil {
		return false
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != kind || resource.Metadata.Name != name {
			continue
		}
		when := resourcequery.ResourceWhen(resource)
		return resourcequery.ResourceWhenPresent(when) && !resourcequery.ResourceWhenMatches(when, newVRRPWhenStore(c.Store))
	}
	return false
}

type vrrpWhenStore struct {
	Store
	state resourcequery.StateStore
}

func newVRRPWhenStore(store Store) vrrpWhenStore {
	state, _ := store.(resourcequery.StateStore)
	return vrrpWhenStore{Store: store, state: state}
}

func (s vrrpWhenStore) Get(name string) routerstate.Value {
	if s.state != nil {
		return s.state.Get(name)
	}
	now := s.Now()
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: now, UpdatedAt: now}
}

func (s vrrpWhenStore) Age(name string) time.Duration {
	if s.state != nil {
		return s.state.Age(name)
	}
	return 0
}

func (s vrrpWhenStore) Now() time.Time {
	if s.state != nil {
		return s.state.Now()
	}
	return time.Now().UTC()
}

func (c *Controller) trackStateFor(kind, vip string, track api.ResourceTrackSpec, state trackedResourceState) trackDecision {
	decision := c.currentTrackDecision(kind, vip, track.Resource)
	if state == trackedResourceNeutral {
		return decision
	}
	return c.confirmTrack(kind, vip, track, state == trackedResourceHealthy)
}

func (c *Controller) confirmTrack(kind, vip string, track api.ResourceTrackSpec, healthy bool) trackDecision {
	key := kind + "\x00" + vip + "\x00" + track.Resource
	decision := c.currentTrackDecision(kind, vip, track.Resource)
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

func (c *Controller) currentTrackDecision(kind, vip, trackedResource string) trackDecision {
	key := kind + "\x00" + vip + "\x00" + trackedResource
	decision, ok := c.trackState[key]
	if !ok {
		decision = c.restoreTrackDecision(kind, vip, trackedResource)
		c.trackState[key] = decision
	}
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
			Penalized:      statusvalue.BoolOrFalse(entry["penalized"]),
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

func trackedPhaseHealthy(kind, phase string) bool {
	return trackedPhaseState(kind, phase, "") == trackedResourceHealthy
}

func trackedPhaseState(kind, phase, reason string) trackedResourceState {
	if phase == "Pending" && reason == "WhenFalse" {
		return trackedResourceNeutral
	}
	switch phase {
	case "Standby", "NotApplicable", "Disabled":
		return trackedResourceNeutral
	}
	switch kind {
	case "BGPRouter", "BGPPeer":
		if phase == "Established" {
			return trackedResourceHealthy
		}
	case "IngressService":
		switch phase {
		case "Active", "Healthy", "Applied":
			return trackedResourceHealthy
		}
	default:
		switch phase {
		case "Applied", "Bound", "Healthy", "Installed", "Ready", "Running", "Up", "Established", "Active":
			return trackedResourceHealthy
		}
	}
	return trackedResourceUnhealthy
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
		ifconfig := stringutil.FirstNonEmpty(c.Ifconfig, "ifconfig")
		family := ifconfigAddressFamily(address)
		if out, err := c.run(ctx, ifconfig, ifname, family, address, "alias"); err != nil {
			return fmt.Errorf("%s %s %s %s alias: %w: %s", ifconfig, ifname, family, address, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	ip := stringutil.FirstNonEmpty(c.IP, "ip")
	if out, err := c.run(ctx, ip, "addr", "replace", address, "dev", ifname); err != nil {
		return fmt.Errorf("%s addr replace %s dev %s: %w: %s", ip, address, ifname, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Controller) staticIPv4AddressPresent(ctx context.Context, ifname, address string) (bool, error) {
	if _, ok := staticIPv4Host(address); !ok {
		return false, nil
	}
	ip := stringutil.FirstNonEmpty(c.IP, "ip")
	out, err := c.run(ctx, ip, "-4", "-o", "addr", "show", "dev", ifname)
	if err != nil {
		return false, fmt.Errorf("%s -4 -o addr show dev %s: %w: %s", ip, ifname, err, strings.TrimSpace(string(out)))
	}
	return ipv4AddressPresent(string(out), address), nil
}

func (c *Controller) announceStaticIPv4Address(ctx context.Context, ifname, address string) error {
	host, ok := staticIPv4Host(address)
	if !ok {
		return nil
	}
	arping := stringutil.FirstNonEmpty(c.Arping, "arping")
	out, err := c.run(ctx, arping, "-U", "-c", "3", "-I", ifname, host)
	if err != nil {
		return fmt.Errorf("%s -U -c 3 -I %s %s: %w: %s", arping, ifname, host, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func staticIPv4Host(address string) (string, bool) {
	value := strings.TrimSpace(address)
	if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Addr().Is4() {
		return prefix.Addr().String(), true
	}
	if parsed, err := netip.ParseAddr(value); err == nil && parsed.Is4() {
		return parsed.String(), true
	}
	return "", false
}

func (c *Controller) removeStaticAddress(ctx context.Context, ifname, address string) error {
	if c.currentOS() == platform.OSFreeBSD {
		ifconfig := stringutil.FirstNonEmpty(c.Ifconfig, "ifconfig")
		family := ifconfigAddressFamily(address)
		if out, err := c.run(ctx, ifconfig, ifname, family, address, "-alias"); err != nil {
			return fmt.Errorf("%s %s %s %s -alias: %w: %s", ifconfig, ifname, family, address, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	ip := stringutil.FirstNonEmpty(c.IP, "ip")
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

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}
