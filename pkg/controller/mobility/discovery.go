// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/plugin"
	provideraction "github.com/imksoo/routerd/pkg/provideraction"
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
	onPremLeaseRefreshMinBefore  = 30 * time.Second
	onPremLeaseRefreshMaxBefore  = time.Minute
)

type DiscoveryStore interface {
	RecordFederationEvent(routerstate.EventRecord) error
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
	ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type DiscoveryController struct {
	Router           *api.Router
	Bus              *bus.Bus
	Store            DiscoveryStore
	Runner           providerinventory.Runner
	ProbeARP         ARPProbeFunc
	ARPProbeRequests *ARPProbeRequestTracker
	Now              func() time.Time
	StartedAt        time.Time
}

func (c DiscoveryController) HandleEvent(ctx context.Context, event daemonapi.DaemonEvent) error {
	if event.Type == OnPremARPRequestObservedEvent {
		return c.handleOnPremARPRequestEvent(event)
	}
	if err := c.handleOnPremDiscoveryEvent(ctx, event); err != nil {
		return err
	}
	return c.reconcile(ctx, event.Type == provideraction.ProviderCaptureChangedEvent)
}

func (c DiscoveryController) Reconcile(ctx context.Context) error {
	return c.reconcile(ctx, false)
}

func (c DiscoveryController) reconcile(ctx context.Context, forceProviderScan bool) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := controllerNow(c.Now)
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			c.saveDiscoveryError(res.Metadata.Name, err)
			continue
		}
		normalized, err := resolveNormalizedMobilityPool(c.Router, spec)
		if err != nil {
			c.saveDiscoveryError(res.Metadata.Name, err)
			continue
		}
		if err := c.reconcilePoolDiscovery(ctx, res, normalized, now, forceProviderScan); err != nil {
			c.saveDiscoveryError(res.Metadata.Name, err)
		}
	}
	return nil
}

func (c DiscoveryController) reconcilePoolDiscovery(ctx context.Context, res api.Resource, normalized resolvedMobilityPool, now time.Time, forceProviderScan bool) error {
	pool := normalized.Pool
	pool.Name = res.Metadata.Name
	pool.Source = DynamicSource(pool.Name, pool.SelfNode)
	// ARP observation is an input to ownership and BGP planning, so its daemon
	// bootstrap is emitted independently from the final PoolPlan. Write an empty
	// intent set for any non-onprem/pending shape to withdraw a prior observer
	// without making chain reopen the raw MobilityPool.
	if err := c.upsertARPObserverIntents(pool, c.arpObserverIntents(pool), now); err != nil {
		return fmt.Errorf("upsert ARP observer intents: %w", err)
	}
	if len(normalized.Resolved.PendingSources) > 0 {
		c.saveDiscoveryStatus(pool.Name, map[string]any{
			"discoveryPhase":      "Pending",
			"discoveryReason":     "membersFrom source is not resolved",
			"pendingSources":      normalized.Resolved.PendingSources,
			"membersFrom":         statusRowMaps(normalized.Resolved.MembersFrom),
			"resolvedMemberCount": normalized.Resolved.ResolvedMemberCount,
		})
		return nil
	}
	discovery := pool.Self.OwnershipDiscovery
	switch strings.TrimSpace(discovery.Mode) {
	case "", "disabled":
		return nil
	default:
		if strings.TrimSpace(discovery.Mode) != "onprem-l2" && strings.TrimSpace(discovery.Mode) != "provider-private-ip" {
			return nil
		}
	}
	facts, err := collectPoolPlanningFacts(c.Router, c.Store, pool, now)
	if err != nil {
		return err
	}
	if strings.TrimSpace(discovery.Mode) == "onprem-l2" {
		return c.reconcileOnPremL2Discovery(pool, pool.Prefix, facts.Events, facts.OnPremIntentReady, now)
	}
	if shardPrefixes := ResolveShardScope(c.Store, pool.Spec.GroupRef, pool.Name, pool.SelfNode, now); len(shardPrefixes) > 0 {
		discovery.Scope.IncludeAddresses = shardPrefixes
	}
	if pool.Self.Role != "cloud" || pool.Self.Capture.Type != "provider-secondary-ip" {
		return fmt.Errorf("ownershipDiscovery requires cloud provider-secondary-ip member %q", pool.Self.NodeRef)
	}
	placementSnapshot := poolRuntimeSnapshotFromFacts(pool, facts, c.StartedAt, now)
	placement, _ := EvaluatePlacementFromObservations(placementSnapshot.Pool.Self, placementSnapshot.Pool.Members, placementSnapshot.PlacementObservations, placementSnapshot.Now)
	interval := discoveryScanInterval(discovery)
	if !forceProviderScan && !scanDue(facts.Previous, facts.Ownership, interval, now, pool.Self.Capture.Type == "provider-secondary-ip" && strings.TrimSpace(pool.Self.Capture.NICRef) == "", placement) {
		return nil
	}
	profileRef := strings.TrimSpace(discovery.ProviderRef)
	if profileRef == "" {
		profileRef = strings.TrimSpace(pool.Self.Capture.ProviderRef)
	}
	profile, ok := cloudProviderProfiles(c.Router)[profileRef]
	if !ok {
		return fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q ownershipDiscovery", profileRef, pool.Name, pool.Self.NodeRef)
	}
	pluginSpec, pluginName, err := c.resolveInventoryPlugin(profile.Provider, discovery)
	if err != nil {
		return err
	}
	pluginContext, err := plugin.BuildPluginContext(pluginSpec.Context.Resources, c.Router.Spec.Resources)
	if err != nil {
		return err
	}
	prefix := pool.Prefix
	req := providerinventory.NewObservePrivateIPsRequest(providerinventory.ObservePrivateIPsRequestSpec{
		Provider:      strings.TrimSpace(profile.Provider),
		ProviderRef:   profileRef,
		Strategy:      providerCaptureStrategy(pool.Self.Capture),
		SelfNode:      pool.Self.NodeRef,
		Pool:          pool.Name,
		Prefix:        prefix.String(),
		SelfNICRef:    strings.TrimSpace(pool.Self.Capture.NICRef),
		SubnetRef:     strings.TrimSpace(discovery.SubnetRef),
		RouteTableRef: strings.TrimSpace(pool.Self.Capture.Target["routeTableRef"]),
		Target:        copyStringMap(pool.Self.Capture.Target),
		Selector:      providerinventory.InventorySelector{Tags: copyStringMap(discovery.Selector.Tags)},
		Context:       pluginContext,
	})
	result, _, err := c.runner()(ctx, pluginSpec, req)
	if err != nil {
		return fmt.Errorf("run provider inventory plugin %q: %w", pluginName, err)
	}
	switch result.Status.Status {
	case providerinventory.ResultSucceeded:
	case providerinventory.ResultSkipped:
		c.saveDiscoveryStatus(pool.Name, map[string]any{
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
	// The normalized pool has exactly one local overlay.  Router-NIC filtering
	// therefore derives only from that overlay; records explicitly classified
	// by inventory as router NICs remain rejected below as the provider-side
	// safety boundary.
	excludedNICs := discoveryRouterNICRefs(pool)
	selfInventory := resolvedDiscoverySelfInventory(pool, result.Status.Self)
	rawLocalInventory := filterProviderInventoryRecords(result.Status.LocalInventoryRecords(), profileRef, "", prefix)
	selfInventory = scopedDiscoverySelfInventory(selfInventory, rawLocalInventory, prefix)
	trustedSubnetRef := trustedDiscoverySelfSubnetRef(selfInventory, rawLocalInventory)
	observedCandidates := filterProviderInventoryRecords(result.Status.ObservedCandidateRecords(), profileRef, trustedSubnetRef, netip.Prefix{})
	ttl := discoveryLeaseTTL(discovery)
	previousRuntime, _ := facts.Ownership.providerRuntimeForNode(pool.SelfNode)
	if !placement.Active {
		runtime, err := providerDiscoveryRuntimeEvent(pool, selfInventory, nil, placement, now, ttl)
		if err != nil {
			return fmt.Errorf("encode standby provider discovery runtime fact: %w", err)
		}
		if err := c.Store.RecordFederationEvent(runtime); err != nil {
			return fmt.Errorf("record standby provider discovery runtime fact: %w", err)
		}
		status := map[string]any{
			"discoveryPhase":      "Standby",
			"discoveryReason":     placement.Reason,
			"discoveryObserved":   0,
			"discoveryLastScanAt": now.Format(time.RFC3339Nano),
		}
		c.saveDiscoveryStatus(pool.Name, status)
		return nil
	}
	if selfInventory.NICRef != "" {
		excludedNICs[selfInventory.NICRef] = true
	}
	selfResourceRef := strings.TrimSpace(selfInventory.ResourceRef)
	selfPrivateIPs := discoverySelfPrivateIPSet(selfInventory.PrivateIPs)
	actionHistory, err := providerActionHistoryForPool(c.Store, pool.Name, pool.SelfNode, now)
	if err != nil {
		return err
	}
	trapAddresses := discoveryCurrentTrapAddresses(actionHistory, pool, profileRef, prefix)
	observedThisScan := map[string]providerDiscoveryAddressFact{}
	invalidThisScan := scopedOutProviderCandidateAddresses(result.Status.ObservedCandidateRecords(), observedCandidates, prefix)
	observed := 0
	for _, rec := range sortedPrivateIPs(observedCandidates) {
		address, ok := normalizeLeaseAddress(rec.Address, prefix)
		if !ok {
			continue
		}
		if strings.TrimSpace(rec.ResourceType) == "router-nic" {
			invalidThisScan[address] = true
			continue
		}
		if selfPrivateIPs[address] {
			invalidThisScan[address] = true
			continue
		}
		if selfResourceRef != "" && strings.TrimSpace(rec.ResourceRef) == selfResourceRef {
			invalidThisScan[address] = true
			continue
		}
		if strings.TrimSpace(rec.NICRef) != "" && excludedNICs[strings.TrimSpace(rec.NICRef)] {
			invalidThisScan[address] = true
			continue
		}
		if trapAddresses[address] {
			invalidThisScan[address] = true
			continue
		}
		if !discoveryScopeAllowsAddress(discovery.Scope, address) {
			invalidThisScan[address] = true
			continue
		}
		if !discoverySelectorMatches(discovery.Selector, rec.Tags) {
			invalidThisScan[address] = true
			continue
		}
		instanceStopped := strings.TrimSpace(rec.InstanceState) == "stopped"
		if instanceStopped && stoppedInstancePolicy(discovery) == "release" {
			invalidThisScan[address] = true
			continue
		}
		observedThisScan[address] = providerDiscoveryAddressFactFromRecord(address, profile.Provider, profileRef, rec, now, ttl)
		observed++
	}
	for address := range observedThisScan {
		delete(invalidThisScan, address)
	}
	addresses := mergeProviderDiscoveryAddressFacts(previousRuntime.RuntimeFact.Addresses, observedThisScan, invalidThisScan, now, 2*interval)
	runtime, err := providerDiscoveryRuntimeEvent(pool, selfInventory, addresses, placement, now, ttl)
	if err != nil {
		return fmt.Errorf("encode provider discovery runtime fact: %w", err)
	}
	if err := c.Store.RecordFederationEvent(runtime); err != nil {
		return fmt.Errorf("record provider discovery runtime fact: %w", err)
	}
	status := map[string]any{
		"discoveryPhase":      "Observed",
		"discoveryReason":     "",
		"discoveryObserved":   observed,
		"discoveryLastScanAt": now.Format(time.RFC3339Nano),
	}
	c.saveDiscoveryStatus(pool.Name, status)
	return nil
}

func (c DiscoveryController) reconcileOnPremL2Discovery(pool NormalizedMobilityPool, poolPrefix netip.Prefix, events []routerstate.EventRecord, intentReady bool, now time.Time) error {
	if strings.TrimSpace(pool.Self.Role) != "onprem" || strings.TrimSpace(pool.Self.Capture.Type) != "proxy-arp" {
		return fmt.Errorf("ownershipDiscovery mode onprem-l2 requires onprem proxy-arp member %q", pool.Self.NodeRef)
	}
	if intentReady {
		armed, err := c.ensureOnPremDiscoveryArmedFact(pool, events, now)
		if err != nil {
			return fmt.Errorf("record onprem discovery armed fact: %w", err)
		}
		events = append(events, armed)
	}
	state := onPremDiscoveryStateFromFacts(pool, poolPrefix, events, intentReady, now)
	phase := strings.TrimSpace(state.Phase)
	if phase == "" {
		phase = "Armed"
	}
	reason := "onprem-l2 event sources armed"
	if !intentReady {
		reason = "onprem-l2 ARP observer intents are not active"
	}
	completedAt := time.Time{}
	if phase == "Observed" {
		reason = "onprem-l2 local clients observed"
		completedAt = now
	} else if phase == "Complete" {
		reason = "onprem-l2 empty discovery accepted by allowEmptyAfter policy"
		completedAt = now
	}
	status := map[string]any{
		"discoveryPhase":      phase,
		"discoveryReason":     reason,
		"discoveryMode":       "onprem-l2",
		"discoveryObserved":   state.ResultCount,
		"discoveryLastScanAt": now.Format(time.RFC3339Nano),
	}
	if !state.ArmedAt.IsZero() {
		status["discoveryArmedAt"] = state.ArmedAt.Format(time.RFC3339Nano)
	}
	if !completedAt.IsZero() {
		status["discoveryCompletedAt"] = completedAt.UTC().Format(time.RFC3339Nano)
	}
	if !state.FreshUntil.IsZero() {
		status["discoveryFreshUntil"] = state.FreshUntil.UTC().Format(time.RFC3339Nano)
	}
	c.saveDiscoveryStatus(pool.Name, status)
	return nil
}

func onPremDiscoveryEventMatches(pool NormalizedMobilityPool, poolPrefix netip.Prefix, event routerstate.EventRecord, now time.Time) bool {
	if event.Group != pool.Spec.GroupRef || event.Type != ObservedEventType {
		return false
	}
	if strings.TrimSpace(event.Payload["source"]) != onPremDiscoverySource ||
		strings.TrimSpace(event.Payload["pool"]) != strings.TrimSpace(pool.Name) ||
		strings.TrimSpace(event.SourceNode) != strings.TrimSpace(pool.Self.NodeRef) {
		return false
	}
	if event.ExpiresAt.IsZero() || !now.Before(event.ExpiresAt) {
		return false
	}
	observation := onPremObservation{
		Action:     "observed",
		Address:    firstNonEmpty(event.Payload["address"], event.Subject),
		MAC:        event.Payload["mac"],
		Interface:  event.Payload["interface"],
		Network:    event.Payload["network"],
		Bridge:     event.Payload["bridge"],
		SourceType: event.Payload["sourceType"],
		ObservedAt: event.ObservedAt,
	}
	address, ok := normalizeLeaseAddress(observation.Address, poolPrefix)
	if !ok || !discoveryScopeAllowsAddress(pool.Self.OwnershipDiscovery.Scope, address) {
		return false
	}
	_, ok = matchingOnPremDiscoverySource(pool.Self, observation)
	return ok
}

func statusTimeValue(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return time.Time{}, false
		}
		return typed.UTC(), true
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

type onPremObservation struct {
	Action     string
	Address    string
	MAC        string
	Interface  string
	Network    string
	Bridge     string
	SourceType string
	ObservedAt time.Time
}

func (c DiscoveryController) handleOnPremDiscoveryEvent(ctx context.Context, event daemonapi.DaemonEvent) error {
	observation, ok := onPremObservationFromDaemonEvent(event)
	if !ok || c.Router == nil || c.Store == nil {
		return nil
	}
	now := controllerNow(c.Now)
	recorded := false
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			continue
		}
		normalized, err := resolveNormalizedMobilityPool(c.Router, spec)
		if err != nil || len(normalized.Resolved.PendingSources) > 0 {
			continue
		}
		pool := normalized.Pool
		pool.Name = res.Metadata.Name
		pool.Source = DynamicSource(res.Metadata.Name, pool.SelfNode)
		if strings.TrimSpace(pool.Self.NodeRef) == "" {
			continue
		}
		if strings.TrimSpace(pool.Self.OwnershipDiscovery.Mode) != "onprem-l2" {
			continue
		}
		poolPrefix := pool.Prefix
		if !poolPrefix.IsValid() {
			continue
		}
		ok, recordErr := c.recordOnPremObservation(pool, poolPrefix, observation, now)
		if recordErr != nil {
			return recordErr
		}
		recorded = recorded || ok
	}
	if recorded && c.Bus != nil {
		changed := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "mobility-discovery", Kind: "mobility-discovery"}, OwnershipChangedEvent, daemonapi.SeverityInfo)
		changed.Time = now
		_ = c.Bus.Publish(ctx, changed)
	}
	return nil
}

func (c DiscoveryController) recordOnPremObservation(pool NormalizedMobilityPool, poolPrefix netip.Prefix, observation onPremObservation, now time.Time) (bool, error) {
	self := pool.Self
	address, ok := normalizeLeaseAddress(observation.Address, poolPrefix)
	if !ok || !discoveryScopeAllowsAddress(self.OwnershipDiscovery.Scope, address) {
		return false, nil
	}
	source, ok := matchingOnPremDiscoverySource(self, observation)
	if !ok {
		return false, nil
	}
	ttl := onPremDiscoveryLeaseTTL(self.OwnershipDiscovery, source)
	eventTime := now
	if !observation.ObservedAt.IsZero() {
		eventTime = observation.ObservedAt.UTC()
	}
	var ev routerstate.EventRecord
	if observation.Action == "expired" {
		ev = onPremDiscoveryExpiredEvent(pool.Name, pool.Spec.GroupRef, self.NodeRef, address, observation, eventTime, ttl)
	} else {
		previous, owned, err := c.latestOnPremObservationOwnership(pool, address, poolPrefix, now)
		if err != nil {
			return false, err
		}
		if owned {
			if !onPremObservationRefreshDue(previous, now, ttl) {
				return false, nil
			}
			ev = onPremDiscoveryObservedEvent(pool.Name, pool.Spec.GroupRef, self.NodeRef, address, observation, eventTime, ttl)
			if err := c.Store.RecordFederationEvent(ev); err != nil {
				return false, fmt.Errorf("refresh onprem ownership event %q: %w", ev.ID, err)
			}
			return false, nil
		}
		ev = onPremDiscoveryObservedEvent(pool.Name, pool.Spec.GroupRef, self.NodeRef, address, observation, eventTime, ttl)
	}
	if err := c.Store.RecordFederationEvent(ev); err != nil {
		return false, fmt.Errorf("record onprem ownership event %q: %w", ev.ID, err)
	}
	return true, nil
}

func (c DiscoveryController) latestOnPremObservationOwnership(pool NormalizedMobilityPool, address string, poolPrefix netip.Prefix, now time.Time) (routerstate.EventRecord, bool, error) {
	events, err := c.Store.ListFederationEvents(pool.Spec.GroupRef, false, now.Unix())
	if err != nil {
		return routerstate.EventRecord{}, false, fmt.Errorf("list onprem ownership federation events: %w", err)
	}
	var latest routerstate.EventRecord
	found := false
	for _, ev := range events {
		if ev.Group != pool.Spec.GroupRef {
			continue
		}
		if ev.Type != ObservedEventType && ev.Type != ExpiredEventType {
			continue
		}
		if strings.TrimSpace(ev.Payload["source"]) != onPremDiscoverySource {
			continue
		}
		if strings.TrimSpace(ev.Payload["pool"]) != strings.TrimSpace(pool.Name) {
			continue
		}
		candidateAddress, ok := normalizeLeaseAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), poolPrefix)
		if !ok || candidateAddress != address {
			continue
		}
		candidate := eventWithFallbackObservedAt(ev, now)
		if !found || eventRecordGreater(candidate, latest) {
			latest = candidate
			found = true
		}
	}
	if !found || latest.Type != ObservedEventType {
		return routerstate.EventRecord{}, false, nil
	}
	if strings.TrimSpace(latest.SourceNode) != strings.TrimSpace(pool.Self.NodeRef) {
		return routerstate.EventRecord{}, false, nil
	}
	if !latest.ExpiresAt.IsZero() && !now.Before(latest.ExpiresAt) {
		return routerstate.EventRecord{}, false, nil
	}
	return latest, true, nil
}

func onPremObservationRefreshDue(ev routerstate.EventRecord, now time.Time, ttl time.Duration) bool {
	if ev.ExpiresAt.IsZero() {
		return false
	}
	refreshBefore := onPremObservationRefreshBefore(ttl)
	if refreshBefore <= 0 {
		return true
	}
	return !now.UTC().Add(refreshBefore).Before(ev.ExpiresAt.UTC())
}

func onPremObservationRefreshBefore(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	refreshBefore := ttl / 2
	if refreshBefore > onPremLeaseRefreshMaxBefore {
		refreshBefore = onPremLeaseRefreshMaxBefore
	}
	if refreshBefore < onPremLeaseRefreshMinBefore {
		if ttl <= onPremLeaseRefreshMinBefore {
			return ttl / 2
		}
		refreshBefore = onPremLeaseRefreshMinBefore
	}
	return refreshBefore
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
	case daemonapi.EventDHCPLeaseAdded, daemonapi.EventDHCPLeaseRenewed:
		out.SourceType = firstNonEmpty(out.SourceType, OnPremSourceDHCPv4Lease)
	case daemonapi.EventDHCPLeaseRemoved:
		out.Action = "expired"
		out.SourceType = firstNonEmpty(out.SourceType, OnPremSourceDHCPv4Lease)
	case OnPremARPObservedEvent:
		out.SourceType = firstNonEmpty(out.SourceType, OnPremSourceARPObserver)
	case OnPremARPProbeHitEvent:
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
		if !onPremDiscoverySelectorMatches(sourceIface, observation.Interface) ||
			!onPremDiscoverySelectorMatches(source.Network, observation.Network) ||
			!onPremDiscoverySelectorMatches(source.Bridge, observation.Bridge) {
			continue
		}
		return source, true
	}
	return api.MobilityOwnershipDiscoverySource{}, false
}

func onPremDiscoverySelectorMatches(required, observed string) bool {
	required = strings.TrimSpace(required)
	return required == "" || required == strings.TrimSpace(observed)
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

func onPremDiscoveryLeaseTTL(discovery api.MobilityOwnershipDiscovery, source api.MobilityOwnershipDiscoverySource) time.Duration {
	if ttl := durationDefault(source.LeaseTTL, 0); ttl > 0 {
		return ttl
	}
	return discoveryLeaseTTL(discovery)
}
