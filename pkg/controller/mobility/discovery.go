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
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/plugin"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	providerDiscoverySource      = "provider-discovery"
	onPremDiscoverySource        = "onprem-l2-discovery"
	OnPremSourceDHCPv4Lease      = "dhcpv4-lease"
	OnPremSourceARPObserver      = "arp-observer"
	OnPremSourceOnDemandARP      = "on-demand-arp"
	OnPremSourcePVESVNet         = "pve-svnet"
	OnPremARPObservedEvent       = "routerd.mobility.arp.observed"
	OnPremARPProbeHitEvent       = "routerd.mobility.arp.probe.hit"
	OnPremPVESVNetObservedEvent  = "routerd.mobility.pve-svnet.observed"
	OwnershipChangedEvent        = "routerd.mobility.ownership.changed"
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

func (c DiscoveryController) HandleEvent(ctx context.Context, event daemonapi.DaemonEvent) error {
	if err := c.handleOnPremDiscoveryEvent(ctx, event); err != nil {
		return err
	}
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
	spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
	if err != nil {
		return fmt.Errorf("normalize MobilityPool/%s discovery: %w", poolName, err)
	}
	members := plannerMembers(spec.Members)
	self, ok := lookupMemberByNodeRef(members, selfNode)
	if !ok {
		return fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, poolName)
	}
	selfNode = self.NodeRef
	discovery := self.OwnershipDiscovery
	switch strings.TrimSpace(discovery.Mode) {
	case "", "disabled":
		return nil
	case "onprem-l2":
		return c.reconcileOnPremL2Discovery(poolName, self, discovery, now)
	case "provider-private-ip":
	default:
		return nil
	}
	if self.Role != "cloud" || self.Capture.Type != "provider-secondary-ip" {
		return fmt.Errorf("ownershipDiscovery requires cloud provider-secondary-ip member %q", self.NodeRef)
	}
	interval := discoveryScanInterval(discovery)
	if !c.scanDue(poolName, interval, now, true) {
		return nil
	}
	livenessMarkers, livenessMarkersObserved := bgpLivenessMarkersFromStatus(c.Router, c.Store)
	placement := evaluateBGPCapturePlacement(self, members, livenessMarkers, livenessMarkersObserved)
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
	if !placement.Active {
		if err := c.expireStaleProviderDiscoveryEvents(poolName, spec, self.NodeRef, prefix, nil, now, discoveryLeaseTTL(discovery, spec), 0); err != nil {
			return err
		}
		status := mergeAnyMaps(discoverySelfInventoryStatus(selfInventory), map[string]any{
			"discoveryPhase":          "Standby",
			"discoveryReason":         placement.Reason,
			"discoveryProvider":       profile.Provider,
			"discoveryProviderRef":    profileRef,
			"discoveryPlugin":         pluginName,
			"discoveryObserved":       0,
			"discoveryOwnedAddresses": []string{},
			"discoveryLastScanAt":     now.Format(time.RFC3339Nano),
			"discoveryNextScanAt":     now.Add(interval).Format(time.RFC3339Nano),
		})
		c.saveDiscoveryStatus(poolName, status)
		return nil
	}
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
	observedThisScan := map[string]bool{}
	retainedThisScan := map[string]bool{}
	counters := discoveryExclusionCounters{}
	for _, rec := range sortedPrivateIPs(result.Status.IPs) {
		address, ok := normalizeDiscoveredAddress(rec.Address, prefix)
		if !ok {
			counters.Scope++
			continue
		}
		retainedThisScan[address] = true
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
		observedThisScan[address] = true
		counters.Observed++
	}
	for address := range observedThisScan {
		retainedThisScan[address] = true
	}
	if err := c.expireStaleProviderDiscoveryEvents(poolName, spec, self.NodeRef, prefix, retainedThisScan, now, ttl, ttl); err != nil {
		return err
	}
	status := mergeAnyMaps(discoverySelfInventoryStatus(selfInventory), map[string]any{
		"discoveryPhase":             "Observed",
		"discoveryReason":            "",
		"discoveryProvider":          profile.Provider,
		"discoveryProviderRef":       profileRef,
		"discoveryPlugin":            pluginName,
		"discoveryObserved":          counters.Observed,
		"discoveryOwnedAddresses":    mapStringKeysSorted(observedThisScan),
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
	c.saveDiscoveryStatus(poolName, status)
	return nil
}

func (c DiscoveryController) reconcileOnPremL2Discovery(poolName string, self memberPlanInfo, discovery api.MobilityOwnershipDiscovery, now time.Time) error {
	if strings.TrimSpace(self.Role) != "onprem" || strings.TrimSpace(self.Capture.Type) != "proxy-arp" {
		return fmt.Errorf("ownershipDiscovery mode onprem-l2 requires onprem proxy-arp member %q", self.NodeRef)
	}
	sources := onPremDiscoverySources(discovery)
	statusSources := make([]map[string]string, 0, len(sources))
	for _, source := range sources {
		item := map[string]string{"type": strings.TrimSpace(source.Type)}
		if value := strings.TrimSpace(source.Resource); value != "" {
			item["resource"] = value
		}
		if value := strings.TrimSpace(firstNonEmpty(source.Interface, self.Capture.Interface)); value != "" {
			item["interface"] = value
		}
		if value := strings.TrimSpace(source.Network); value != "" {
			item["network"] = value
		}
		if value := strings.TrimSpace(source.Bridge); value != "" {
			item["bridge"] = value
		}
		statusSources = append(statusSources, item)
	}
	c.saveDiscoveryStatus(poolName, map[string]any{
		"discoveryPhase":       "Ready",
		"discoveryReason":      "onprem-l2 event sources armed",
		"discoveryMode":        "onprem-l2",
		"discoverySources":     statusSources,
		"discoveryObserved":    0,
		"discoveryLastScanAt":  now.Format(time.RFC3339Nano),
		"discoveryNextScanAt":  "",
		"discoverySourceCount": len(statusSources),
	})
	return nil
}

type onPremObservation struct {
	Action     string
	Address    string
	MAC        string
	Interface  string
	Network    string
	Bridge     string
	SourceType string
}

func (c DiscoveryController) handleOnPremDiscoveryEvent(ctx context.Context, event daemonapi.DaemonEvent) error {
	observation, ok := onPremObservationFromDaemonEvent(event)
	if !ok || c.Router == nil || c.Store == nil {
		return nil
	}
	now := c.now()
	selfByGroup := map[string]string{}
	recorded := false
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil || !mobilityBGPMode(spec) {
			continue
		}
		selfNode, ok := selfByGroup[strings.TrimSpace(spec.GroupRef)]
		if !ok {
			selfNode, err = routerSelfNode(c.Router, spec.GroupRef)
			if err != nil {
				continue
			}
			selfByGroup[strings.TrimSpace(spec.GroupRef)] = selfNode
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
		if err != nil {
			continue
		}
		members := plannerMembers(spec.Members)
		self, ok := lookupMemberByNodeRef(members, selfNode)
		if !ok || strings.TrimSpace(self.OwnershipDiscovery.Mode) != "onprem-l2" {
			continue
		}
		poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
		if err != nil {
			continue
		}
		poolPrefix = poolPrefix.Masked()
		address, ok := normalizeDiscoveredAddress(observation.Address, poolPrefix)
		if !ok || !discoveryScopeAllowsAddress(self.OwnershipDiscovery.Scope, address) {
			continue
		}
		if ownerNode := strings.TrimSpace(staticOwnedOwnerNodesByAddress(spec)[address]); ownerNode != "" && ownerNode != self.NodeRef {
			continue
		}
		source, ok := matchingOnPremDiscoverySource(self, observation)
		if !ok {
			continue
		}
		ttl := onPremDiscoveryLeaseTTL(self.OwnershipDiscovery, source, spec)
		var ev routerstate.EventRecord
		if observation.Action == "expired" {
			ev = onPremDiscoveryExpiredEvent(res.Metadata.Name, spec.GroupRef, self.NodeRef, address, observation, now, ttl)
		} else {
			ev = onPremDiscoveryObservedEvent(res.Metadata.Name, spec.GroupRef, self.NodeRef, address, observation, now, ttl)
		}
		if err := c.Store.RecordFederationEvent(ev); err != nil {
			return fmt.Errorf("record onprem ownership event %q: %w", ev.ID, err)
		}
		recorded = true
	}
	if recorded && c.Bus != nil {
		changed := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "mobility-discovery", Kind: "mobility-discovery"}, OwnershipChangedEvent, daemonapi.SeverityInfo)
		changed.Time = now
		_ = c.Bus.Publish(ctx, changed)
	}
	return nil
}

func onPremObservationFromDaemonEvent(event daemonapi.DaemonEvent) (onPremObservation, bool) {
	attrs := event.Attributes
	address := firstNonEmpty(attrs["address"], attrs["ip"], attrs["clientIP"], attrs["clientAddress"], event.Message)
	out := onPremObservation{
		Action:     "observed",
		Address:    address,
		MAC:        firstNonEmpty(attrs["mac"], attrs["clientMAC"], attrs["lladdr"]),
		Interface:  firstNonEmpty(attrs["interface"], attrs["ifname"], attrs["device"]),
		Network:    firstNonEmpty(attrs["network"], attrs["svnet"]),
		Bridge:     attrs["bridge"],
		SourceType: firstNonEmpty(attrs["sourceType"], attrs["source"]),
	}
	switch strings.TrimSpace(event.Type) {
	case "routerd.dhcp.lease.add", "routerd.dhcp.lease.old":
		out.SourceType = firstNonEmpty(out.SourceType, OnPremSourceDHCPv4Lease)
	case "routerd.dhcp.lease.del":
		out.Action = "expired"
		out.SourceType = firstNonEmpty(out.SourceType, OnPremSourceDHCPv4Lease)
	case OnPremARPObservedEvent, "routerd.arp.observed":
		out.SourceType = firstNonEmpty(out.SourceType, OnPremSourceARPObserver)
	case OnPremARPProbeHitEvent, "routerd.arp.probe.hit":
		out.SourceType = firstNonEmpty(out.SourceType, OnPremSourceOnDemandARP)
	case OnPremPVESVNetObservedEvent:
		out.SourceType = firstNonEmpty(out.SourceType, OnPremSourcePVESVNet)
	default:
		return onPremObservation{}, false
	}
	if strings.TrimSpace(out.Address) == "" || strings.TrimSpace(out.SourceType) == "" {
		return onPremObservation{}, false
	}
	return out, true
}

func matchingOnPremDiscoverySource(self memberPlanInfo, observation onPremObservation) (api.MobilityOwnershipDiscoverySource, bool) {
	for _, source := range onPremDiscoverySources(self.OwnershipDiscovery) {
		if strings.TrimSpace(source.Type) != strings.TrimSpace(observation.SourceType) {
			continue
		}
		sourceIface := strings.TrimSpace(firstNonEmpty(source.Interface, self.Capture.Interface))
		if sourceIface != "" && strings.TrimSpace(observation.Interface) != "" && sourceIface != strings.TrimSpace(observation.Interface) {
			continue
		}
		if source.Network != "" && observation.Network != "" && strings.TrimSpace(source.Network) != strings.TrimSpace(observation.Network) {
			continue
		}
		if source.Bridge != "" && observation.Bridge != "" && strings.TrimSpace(source.Bridge) != strings.TrimSpace(observation.Bridge) {
			continue
		}
		return source, true
	}
	return api.MobilityOwnershipDiscoverySource{}, false
}

func onPremDiscoverySources(discovery api.MobilityOwnershipDiscovery) []api.MobilityOwnershipDiscoverySource {
	out := append([]api.MobilityOwnershipDiscoverySource(nil), discovery.Sources...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			return out[i].Resource < out[j].Resource
		}
		return out[i].Type < out[j].Type
	})
	return out
}

func onPremDiscoveryLeaseTTL(discovery api.MobilityOwnershipDiscovery, source api.MobilityOwnershipDiscoverySource, spec api.MobilityPoolSpec) time.Duration {
	if ttl := durationDefault(source.LeaseTTL, 0); ttl > 0 {
		return ttl
	}
	return discoveryLeaseTTL(discovery, spec)
}

func (c DiscoveryController) expireStaleProviderDiscoveryEvents(poolName string, spec api.MobilityPoolSpec, selfNode string, poolPrefix netip.Prefix, retained map[string]bool, now time.Time, ttl time.Duration, missingHold time.Duration) error {
	latest, err := c.latestProviderDiscoveryEvents(poolName, spec.GroupRef, selfNode, poolPrefix, now)
	if err != nil {
		return err
	}
	for _, address := range mapStringKeysSorted(latest) {
		if retained[address] {
			continue
		}
		ev := latest[address]
		if ev.Type != ObservedEventType {
			continue
		}
		if missingHold > 0 {
			observedAt := ev.ObservedAt
			if observedAt.IsZero() {
				observedAt = now
			}
			if observedAt.Add(missingHold).After(now) {
				continue
			}
		}
		expired := providerDiscoveryExpiredEvent(poolName, spec.GroupRef, selfNode, address, ev, now, ttl)
		if err := c.Store.RecordFederationEvent(expired); err != nil {
			return fmt.Errorf("record provider discovery expired event %q: %w", expired.ID, err)
		}
	}
	return nil
}

func (c DiscoveryController) latestProviderDiscoveryEvents(poolName, group, selfNode string, poolPrefix netip.Prefix, now time.Time) (map[string]routerstate.EventRecord, error) {
	events, err := c.Store.ListFederationEvents(group, false, now.Unix())
	if err != nil {
		return nil, fmt.Errorf("list provider discovery federation events: %w", err)
	}
	latest := map[string]routerstate.EventRecord{}
	for _, ev := range events {
		if ev.Group != group || strings.TrimSpace(ev.SourceNode) != strings.TrimSpace(selfNode) {
			continue
		}
		if ev.Type != ObservedEventType && ev.Type != ExpiredEventType {
			continue
		}
		if strings.TrimSpace(ev.Payload["source"]) != providerDiscoverySource {
			continue
		}
		if strings.TrimSpace(ev.Payload["pool"]) != strings.TrimSpace(poolName) {
			continue
		}
		address, ok := normalizeDiscoveredAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), poolPrefix)
		if !ok {
			continue
		}
		current, found := latest[address]
		candidate := ev
		if candidate.ObservedAt.IsZero() {
			candidate.ObservedAt = now
		}
		if !found || eventRecordGreater(candidate, current) {
			latest[address] = candidate
		}
	}
	return latest, nil
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
	NICRef            string
	SubnetRef         string
	PrivateIPs        []string
	ForwardingEnabled *bool
}

func resolvedDiscoverySelfInventory(self memberPlanInfo, discovery api.MobilityOwnershipDiscovery, pluginSelf *providerinventory.PrivateIPSelf) discoverySelfInventory {
	out := discoverySelfInventory{}
	if pluginSelf != nil {
		out.NICRef = strings.TrimSpace(pluginSelf.NICRef)
		out.SubnetRef = strings.TrimSpace(pluginSelf.SubnetRef)
		out.PrivateIPs = cleanStrings(pluginSelf.PrivateIPs)
		out.ForwardingEnabled = pluginSelf.ForwardingEnabled
	}
	if explicit := strings.TrimSpace(self.Capture.NICRef); explicit != "" {
		out.NICRef = explicit
	}
	if explicit := strings.TrimSpace(discovery.SubnetRef); explicit != "" {
		out.SubnetRef = explicit
	}
	return out
}

func discoverySelfInventoryStatus(self discoverySelfInventory) map[string]any {
	status := map[string]any{
		"discoverySelfNICRef":             self.NICRef,
		"discoverySelfSubnetRef":          self.SubnetRef,
		"discoverySelfPrivateIPs":         append([]string(nil), self.PrivateIPs...),
		"discoverySelfForwardingObserved": self.ForwardingEnabled != nil,
	}
	if self.ForwardingEnabled != nil {
		status["discoverySelfForwardingEnabled"] = *self.ForwardingEnabled
	}
	return status
}

func mergeAnyMaps(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func (c DiscoveryController) scanDue(poolName string, interval time.Duration, now time.Time, requireForwardingState bool) bool {
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	if requireForwardingState {
		if _, ok := status["discoverySelfForwardingObserved"]; !ok {
			return true
		}
	}
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

func providerDiscoveryExpiredEvent(poolName, group, nodeRef, address string, previous routerstate.EventRecord, now time.Time, ttl time.Duration) routerstate.EventRecord {
	observedAt := now.UTC()
	provider := strings.TrimSpace(previous.Payload["provider"])
	providerRef := strings.TrimSpace(previous.Payload["providerRef"])
	payload := map[string]string{
		"address": address,
		"pool":    poolName,
		"source":  providerDiscoverySource,
	}
	if provider != "" {
		payload["provider"] = provider
	}
	if providerRef != "" {
		payload["providerRef"] = providerRef
	}
	if value := strings.TrimSpace(previous.Payload["nicRef"]); value != "" {
		payload["nicRef"] = value
	}
	if value := strings.TrimSpace(previous.Payload["subnetRef"]); value != "" {
		payload["subnetRef"] = value
	}
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	return routerstate.EventRecord{
		ID:         providerDiscoveryDedupeKey(poolName, nodeRef, address) + ":expired:" + strconv.FormatInt(observedAt.UnixNano(), 10),
		Group:      strings.TrimSpace(group),
		SourceNode: strings.TrimSpace(nodeRef),
		Type:       ExpiredEventType,
		Subject:    address,
		DedupeKey:  providerDiscoveryDedupeKey(poolName, nodeRef, address),
		Payload:    payload,
		ObservedAt: observedAt,
		ExpiresAt:  observedAt.Add(ttl),
		RecordedAt: observedAt,
	}
}

func onPremDiscoveryObservedEvent(poolName, group, nodeRef, address string, observation onPremObservation, now time.Time, ttl time.Duration) routerstate.EventRecord {
	observedAt := now.UTC()
	payload := onPremDiscoveryPayload(poolName, address, observation)
	return routerstate.EventRecord{
		ID:         onPremDiscoveryEventID(poolName, nodeRef, address, observation.SourceType, observedAt),
		Group:      strings.TrimSpace(group),
		SourceNode: strings.TrimSpace(nodeRef),
		Type:       ObservedEventType,
		Subject:    address,
		DedupeKey:  onPremDiscoveryDedupeKey(poolName, nodeRef, address, observation.SourceType),
		Payload:    payload,
		ObservedAt: observedAt,
		ExpiresAt:  observedAt.Add(ttl),
		RecordedAt: observedAt,
	}
}

func onPremDiscoveryExpiredEvent(poolName, group, nodeRef, address string, observation onPremObservation, now time.Time, ttl time.Duration) routerstate.EventRecord {
	observedAt := now.UTC()
	payload := onPremDiscoveryPayload(poolName, address, observation)
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	return routerstate.EventRecord{
		ID:         onPremDiscoveryDedupeKey(poolName, nodeRef, address, observation.SourceType) + ":expired:" + strconv.FormatInt(observedAt.UnixNano(), 10),
		Group:      strings.TrimSpace(group),
		SourceNode: strings.TrimSpace(nodeRef),
		Type:       ExpiredEventType,
		Subject:    address,
		DedupeKey:  onPremDiscoveryDedupeKey(poolName, nodeRef, address, observation.SourceType),
		Payload:    payload,
		ObservedAt: observedAt,
		ExpiresAt:  observedAt.Add(ttl),
		RecordedAt: observedAt,
	}
}

func onPremDiscoveryPayload(poolName, address string, observation onPremObservation) map[string]string {
	payload := map[string]string{
		"address":    address,
		"pool":       strings.TrimSpace(poolName),
		"source":     onPremDiscoverySource,
		"sourceType": strings.TrimSpace(observation.SourceType),
	}
	if value := strings.TrimSpace(observation.MAC); value != "" {
		payload["mac"] = value
	}
	if value := strings.TrimSpace(observation.Interface); value != "" {
		payload["interface"] = value
	}
	if value := strings.TrimSpace(observation.Network); value != "" {
		payload["network"] = value
	}
	if value := strings.TrimSpace(observation.Bridge); value != "" {
		payload["bridge"] = value
	}
	return payload
}

func onPremDiscoveryEventID(poolName, nodeRef, address, sourceType string, observedAt time.Time) string {
	return onPremDiscoveryDedupeKey(poolName, nodeRef, address, sourceType) + ":" + strconv.FormatInt(observedAt.UTC().UnixNano(), 10)
}

func onPremDiscoveryDedupeKey(poolName, nodeRef, address, sourceType string) string {
	return strings.Join([]string{"mobility", onPremDiscoverySource, strings.TrimSpace(sourceType), strings.TrimSpace(poolName), strings.TrimSpace(nodeRef), strings.ReplaceAll(strings.TrimSpace(address), "/", "_")}, ":")
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
