// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/plugin"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	providerDiscoverySource      = "provider-discovery"
	defaultDiscoveryScanInterval = 60 * time.Second
	minDiscoveryScanInterval     = 30 * time.Second
)

type DiscoveryStore interface {
	RecordFederationEvent(routerstate.EventRecord) error
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
	ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type DiscoveryController struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  DiscoveryStore
	Runner providerinventory.Runner
	Now    func() time.Time
}

func (c DiscoveryController) HandleEvent(ctx context.Context, _ daemonapi.DaemonEvent) error {
	return c.Reconcile(ctx)
}

func (c DiscoveryController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := c.now()
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			c.saveDiscoveryStatus(res.Metadata.Name, map[string]any{"discoveryPhase": "Degraded", "discoveryReason": err.Error()})
			continue
		}
		if !mobilityBGPMode(spec) {
			continue
		}
		if err := c.reconcilePoolDiscovery(ctx, res.Metadata.Name, spec, now); err != nil {
			c.saveDiscoveryStatus(res.Metadata.Name, map[string]any{
				"discoveryPhase":  "Degraded",
				"discoveryReason": err.Error(),
			})
		}
	}
	return nil
}

func (c DiscoveryController) reconcilePoolDiscovery(ctx context.Context, poolName string, spec api.MobilityPoolSpec, now time.Time) error {
	selfNode, err := routerSelfNode(c.Router, spec.GroupRef)
	if err != nil {
		return err
	}
	members := plannerMembers(spec.Members)
	self, ok := members[selfNode]
	if !ok {
		return fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, poolName)
	}
	discovery := self.OwnershipDiscovery
	if strings.TrimSpace(discovery.Mode) != "provider-private-ip" {
		return nil
	}
	interval := discoveryScanInterval(discovery)
	if !c.scanDue(poolName, interval, now) {
		return nil
	}
	if self.Role != "cloud" || self.Capture.Type != "provider-secondary-ip" {
		return fmt.Errorf("ownershipDiscovery requires cloud provider-secondary-ip member %q", self.NodeRef)
	}
	placement := evaluatePlacement(self, members)
	if !placement.Active {
		c.saveDiscoveryStatus(poolName, map[string]any{
			"discoveryPhase":      "Standby",
			"discoveryReason":     placement.Reason,
			"discoveryLastScanAt": now.Format(time.RFC3339Nano),
		})
		return nil
	}
	profileRef := strings.TrimSpace(discovery.ProviderRef)
	if profileRef == "" {
		profileRef = strings.TrimSpace(self.Capture.ProviderRef)
	}
	profile, ok := cloudProviderProfiles(c.Router)[profileRef]
	if !ok {
		return fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q ownershipDiscovery", profileRef, poolName, self.NodeRef)
	}
	pluginSpec, pluginName, err := c.resolveInventoryPlugin(profile.Provider, discovery)
	if err != nil {
		return err
	}
	pluginContext, err := plugin.BuildPluginContext(pluginSpec.Context.Resources, c.Router.Spec.Resources)
	if err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return fmt.Errorf("parse pool prefix: %w", err)
	}
	prefix = prefix.Masked()
	req := providerinventory.NewObservePrivateIPsRequest(providerinventory.ObservePrivateIPsRequestSpec{
		Provider:    strings.TrimSpace(profile.Provider),
		ProviderRef: profileRef,
		SelfNode:    self.NodeRef,
		Pool:        poolName,
		Prefix:      prefix.String(),
		SelfNICRef:  strings.TrimSpace(self.Capture.NICRef),
		SubnetRef:   strings.TrimSpace(discovery.SubnetRef),
		Target:      copyStringMap(self.CaptureTarget),
		Selector:    providerinventory.InventorySelector{Tags: copyStringMap(discovery.Selector.Tags)},
		Context:     pluginContext,
	})
	result, _, err := c.runner()(ctx, pluginSpec, req)
	if err != nil {
		return fmt.Errorf("run provider inventory plugin %q: %w", pluginName, err)
	}
	switch result.Status.Status {
	case providerinventory.ResultSucceeded:
	case providerinventory.ResultSkipped:
		c.saveDiscoveryStatus(poolName, map[string]any{
			"discoveryPhase":      "Skipped",
			"discoveryReason":     result.Status.Message,
			"discoveryLastScanAt": now.Format(time.RFC3339Nano),
		})
		return nil
	case providerinventory.ResultFailed:
		return fmt.Errorf("provider inventory plugin %q failed: %s", pluginName, firstNonEmpty(result.Status.Error, result.Status.Message))
	default:
		return fmt.Errorf("provider inventory plugin %q returned invalid status %q", pluginName, result.Status.Status)
	}
	excludedNICs := mobilityRouterNICRefs(spec.Members)
	selfInventory := resolvedDiscoverySelfInventory(self, discovery, result.Status.Self)
	if selfInventory.NICRef != "" {
		excludedNICs[selfInventory.NICRef] = true
	}
	selfPrivateIPs := discoverySelfPrivateIPSet(selfInventory.PrivateIPs, prefix)
	staticOwners := staticOwnedOwnerNodesByAddress(spec)
	trapAddresses, err := discoveryCurrentTrapAddresses(c.Store, poolName, selfNode, prefix, now)
	if err != nil {
		return err
	}
	ttl := discoveryLeaseTTL(discovery, spec)
	counters := discoveryExclusionCounters{}
	for _, rec := range sortedPrivateIPs(result.Status.IPs) {
		address, ok := normalizeDiscoveredAddress(rec.Address, prefix)
		if !ok {
			counters.Scope++
			continue
		}
		if selfPrivateIPs[address] {
			counters.SelfPrivateIP++
			continue
		}
		if strings.TrimSpace(rec.NICRef) != "" && excludedNICs[strings.TrimSpace(rec.NICRef)] {
			counters.RouterNIC++
			continue
		}
		if ownerNode := strings.TrimSpace(staticOwners[address]); ownerNode != "" && ownerNode != self.NodeRef {
			counters.StaticOwned++
			continue
		}
		if trapAddresses[address] {
			counters.TrapAction++
			continue
		}
		if !discoveryPrimaryAllowed(discovery.Scope) && rec.Primary {
			counters.Primary++
			continue
		}
		if !discoveryScopeAllowsAddress(discovery.Scope, address) {
			counters.Scope++
			continue
		}
		if !discoverySelectorMatches(discovery.Selector, rec.Tags) {
			counters.Selector++
			continue
		}
		ev := providerDiscoveryObservedEvent(poolName, spec.GroupRef, self.NodeRef, address, profile.Provider, profileRef, rec, now, ttl)
		if err := c.Store.RecordFederationEvent(ev); err != nil {
			return err
		}
		counters.Observed++
	}
	c.saveDiscoveryStatus(poolName, map[string]any{
		"discoveryPhase":             "Observed",
		"discoveryReason":            "",
		"discoveryProvider":          profile.Provider,
		"discoveryProviderRef":       profileRef,
		"discoveryPlugin":            pluginName,
		"discoverySelfNICRef":        selfInventory.NICRef,
		"discoverySelfSubnetRef":     selfInventory.SubnetRef,
		"discoverySelfPrivateIPs":    append([]string(nil), selfInventory.PrivateIPs...),
		"discoveryObserved":          counters.Observed,
		"discoveryExcluded":          counters.Excluded(),
		"discoveryExcludedPrimary":   counters.Primary,
		"discoveryExcludedRouterNIC": counters.RouterNIC,
		"discoveryExcludedSelfIP":    counters.SelfPrivateIP,
		"discoveryExcludedStatic":    counters.StaticOwned,
		"discoveryExcludedRemote":    counters.RemoteOwner,
		"discoveryExcludedTrap":      counters.TrapAction,
		"discoveryExcludedScope":     counters.Scope,
		"discoveryExcludedSelector":  counters.Selector,
		"discoveryLastScanAt":        now.Format(time.RFC3339Nano),
		"discoveryNextScanAt":        now.Add(interval).Format(time.RFC3339Nano),
	})
	return nil
}

type discoveryExclusionCounters struct {
	Observed      int
	Primary       int
	RouterNIC     int
	SelfPrivateIP int
	StaticOwned   int
	RemoteOwner   int
	TrapAction    int
	Scope         int
	Selector      int
}

func (c discoveryExclusionCounters) Excluded() int {
	return c.Primary + c.RouterNIC + c.SelfPrivateIP + c.StaticOwned + c.RemoteOwner + c.TrapAction + c.Scope + c.Selector
}

type discoverySelfInventory struct {
	NICRef     string
	SubnetRef  string
	PrivateIPs []string
}

func resolvedDiscoverySelfInventory(self memberPlanInfo, discovery api.MobilityOwnershipDiscovery, pluginSelf *providerinventory.PrivateIPSelf) discoverySelfInventory {
	out := discoverySelfInventory{}
	if pluginSelf != nil {
		out.NICRef = strings.TrimSpace(pluginSelf.NICRef)
		out.SubnetRef = strings.TrimSpace(pluginSelf.SubnetRef)
		out.PrivateIPs = cleanStrings(pluginSelf.PrivateIPs)
	}
	if explicit := strings.TrimSpace(self.Capture.NICRef); explicit != "" {
		out.NICRef = explicit
	}
	if explicit := strings.TrimSpace(discovery.SubnetRef); explicit != "" {
		out.SubnetRef = explicit
	}
	return out
}

func (c DiscoveryController) scanDue(poolName string, interval time.Duration, now time.Time) bool {
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	last, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status["discoveryLastScanAt"])))
	if err != nil || last.IsZero() {
		return true
	}
	return !last.Add(interval).After(now)
}

func (c DiscoveryController) resolveInventoryPlugin(provider string, discovery api.MobilityOwnershipDiscovery) (api.PluginSpec, string, error) {
	pluginRef := strings.TrimSpace(discovery.PluginRef)
	var candidates []api.Resource
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.PluginAPIVersion || res.Kind != "Plugin" {
			continue
		}
		if pluginRef != "" && res.Metadata.Name != pluginRef {
			continue
		}
		spec, err := res.PluginSpec()
		if err != nil {
			return api.PluginSpec{}, "", err
		}
		if !pluginHasCapability(spec.Capabilities, providerinventory.CapabilityObserveProviderPrivateIPs) {
			continue
		}
		if pluginRef != "" {
			return spec, res.Metadata.Name, nil
		}
		candidates = append(candidates, res)
	}
	if pluginRef != "" {
		return api.PluginSpec{}, "", fmt.Errorf("Plugin/%s with capability %q not found", pluginRef, providerinventory.CapabilityObserveProviderPrivateIPs)
	}
	wantName := strings.TrimSpace(provider) + "-inventory"
	for _, res := range candidates {
		if res.Metadata.Name == wantName {
			spec, err := res.PluginSpec()
			return spec, res.Metadata.Name, err
		}
	}
	if len(candidates) == 1 {
		spec, err := candidates[0].PluginSpec()
		return spec, candidates[0].Metadata.Name, err
	}
	if len(candidates) == 0 {
		return api.PluginSpec{}, "", fmt.Errorf("no Plugin with capability %q found for provider %q", providerinventory.CapabilityObserveProviderPrivateIPs, provider)
	}
	return api.PluginSpec{}, "", fmt.Errorf("ambiguous provider inventory plugin for provider %q: %d candidates found, none named %q", provider, len(candidates), wantName)
}

func (c DiscoveryController) runner() providerinventory.Runner {
	if c.Runner != nil {
		return c.Runner
	}
	return providerinventory.RunInventory
}

func (c DiscoveryController) saveDiscoveryStatus(poolName string, updates map[string]any) {
	if c.Store == nil {
		return
	}
	status := map[string]any{}
	for k, v := range c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName) {
		status[k] = v
	}
	for k, v := range updates {
		status[k] = v
	}
	_ = c.Store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName, status)
}

func (c DiscoveryController) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func discoveryScanInterval(discovery api.MobilityOwnershipDiscovery) time.Duration {
	interval := durationDefault(discovery.ScanInterval, defaultDiscoveryScanInterval)
	if interval < minDiscoveryScanInterval {
		return minDiscoveryScanInterval
	}
	return interval
}

func discoveryLeaseTTL(discovery api.MobilityOwnershipDiscovery, spec api.MobilityPoolSpec) time.Duration {
	return durationDefault(discovery.LeaseTTL, DefaultLeaseTTL)
}

func discoveryPrimaryAllowed(scope api.MobilityOwnershipDiscoveryScope) bool {
	if scope.IncludePrimary == nil {
		return true
	}
	return *scope.IncludePrimary
}

func discoveryScopeAllowsAddress(scope api.MobilityOwnershipDiscoveryScope, address string) bool {
	addr, err := netip.ParsePrefix(strings.TrimSpace(address))
	if err != nil {
		return false
	}
	if len(scope.IncludeAddresses) > 0 {
		matched := false
		for _, raw := range scope.IncludeAddresses {
			prefix, ok := parseDiscoveryScopePrefix(raw)
			if ok && prefix.Contains(addr.Addr()) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, raw := range scope.ExcludeAddresses {
		prefix, ok := parseDiscoveryScopePrefix(raw)
		if ok && prefix.Contains(addr.Addr()) {
			return false
		}
	}
	return true
}

func parseDiscoveryScopePrefix(raw string) (netip.Prefix, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return netip.Prefix{}, false
	}
	if !strings.Contains(value, "/") {
		addr, err := netip.ParseAddr(value)
		if err != nil || !addr.Is4() {
			return netip.Prefix{}, false
		}
		return netip.PrefixFrom(addr, 32), true
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() {
		return netip.Prefix{}, false
	}
	return prefix.Masked(), true
}

func normalizeDiscoveredAddress(value string, poolPrefix netip.Prefix) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if strings.Contains(value, "/") {
		return normalizeLeaseAddress(value, poolPrefix)
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() || !poolPrefix.Contains(addr) {
		return "", false
	}
	return netip.PrefixFrom(addr, 32).String(), true
}

func mobilityRouterNICRefs(members []api.MobilityPoolMember) map[string]bool {
	out := map[string]bool{}
	for _, member := range members {
		nic := strings.TrimSpace(member.Capture.NICRef)
		if member.Capture.Type == "provider-secondary-ip" && nic != "" {
			out[nic] = true
		}
	}
	return out
}

func discoverySelfPrivateIPSet(values []string, poolPrefix netip.Prefix) map[string]bool {
	out := map[string]bool{}
	for _, raw := range values {
		address, ok := normalizeDiscoveredAddress(raw, poolPrefix)
		if ok {
			out[address] = true
		}
	}
	return out
}

func discoveryCurrentTrapAddresses(store DiscoveryStore, poolName, selfNode string, poolPrefix netip.Prefix, now time.Time) (map[string]bool, error) {
	out := map[string]bool{}
	source := DynamicSource(poolName, selfNode)
	parts, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return nil, fmt.Errorf("get discovery dynamic config part %s: %w", source, err)
	}
	for _, part := range parts {
		if part.EffectiveStatus(now) != "active" {
			continue
		}
		for _, plan := range decodeDiscoveryActionPlans(part.ActionPlansJSON) {
			if plan.Action != "assign-secondary-ip" {
				continue
			}
			address, ok := normalizeDiscoveredAddress(plan.Target["address"], poolPrefix)
			if ok {
				out[address] = true
			}
		}
	}
	actions, err := store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return nil, fmt.Errorf("list discovery action journal: %w", err)
	}
	for _, action := range actions {
		if action.Action != "assign-secondary-ip" || !discoveryActionStatusCurrent(action.Status) {
			continue
		}
		var target map[string]string
		if err := json.Unmarshal([]byte(action.TargetJSON), &target); err != nil {
			continue
		}
		address, ok := normalizeDiscoveredAddress(target["address"], poolPrefix)
		if ok {
			out[address] = true
		}
	}
	return out, nil
}

func decodeDiscoveryActionPlans(raw string) []dynamicconfig.ActionPlan {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var plans []dynamicconfig.ActionPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		return nil
	}
	return plans
}

func discoveryActionStatusCurrent(status string) bool {
	switch strings.TrimSpace(status) {
	case routerstate.ActionPending, routerstate.ActionApproved, routerstate.ActionRunning:
		return true
	default:
		return false
	}
}

func discoverySelectorMatches(selector api.MobilityOwnershipDiscoverySelector, tags map[string]string) bool {
	for key, want := range selector.Tags {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if strings.TrimSpace(tags[key]) != strings.TrimSpace(want) {
			return false
		}
	}
	return true
}

func providerDiscoveryObservedEvent(poolName, group, nodeRef, address, provider, providerRef string, rec providerinventory.PrivateIPRecord, now time.Time, ttl time.Duration) routerstate.EventRecord {
	observedAt := now.UTC()
	payload := map[string]string{
		"address":     address,
		"pool":        poolName,
		"source":      providerDiscoverySource,
		"provider":    strings.TrimSpace(provider),
		"providerRef": strings.TrimSpace(providerRef),
	}
	if value := strings.TrimSpace(rec.NICRef); value != "" {
		payload["nicRef"] = value
	}
	if value := strings.TrimSpace(rec.SubnetRef); value != "" {
		payload["subnetRef"] = value
	}
	if rec.Primary {
		payload["primary"] = "true"
	}
	return routerstate.EventRecord{
		ID:         providerDiscoveryEventID(poolName, nodeRef, address, observedAt),
		Group:      strings.TrimSpace(group),
		SourceNode: strings.TrimSpace(nodeRef),
		Type:       ObservedEventType,
		Subject:    address,
		DedupeKey:  providerDiscoveryDedupeKey(poolName, nodeRef, address),
		Payload:    payload,
		ObservedAt: observedAt,
		ExpiresAt:  observedAt.Add(ttl),
		RecordedAt: observedAt,
	}
}

func providerDiscoveryEventID(poolName, nodeRef, address string, observedAt time.Time) string {
	return providerDiscoveryDedupeKey(poolName, nodeRef, address) + ":" + strconv.FormatInt(observedAt.UTC().UnixNano(), 10)
}

func providerDiscoveryDedupeKey(poolName, nodeRef, address string) string {
	return strings.Join([]string{"mobility", providerDiscoverySource, strings.TrimSpace(poolName), strings.TrimSpace(nodeRef), strings.ReplaceAll(strings.TrimSpace(address), "/", "_")}, ":")
}

func pluginHasCapability(capabilities []string, want string) bool {
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == want {
			return true
		}
	}
	return false
}

func sortedPrivateIPs(records []providerinventory.PrivateIPRecord) []providerinventory.PrivateIPRecord {
	out := append([]providerinventory.PrivateIPRecord(nil), records...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Address == out[j].Address {
			return out[i].NICRef < out[j].NICRef
		}
		return out[i].Address < out[j].Address
	})
	return out
}
