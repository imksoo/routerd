// SPDX-License-Identifier: BSD-3-Clause

// Package mobility projects CloudEdge MobilityPool federation events into
// AddressLease runtime state. It does not render or apply dataplane claims.
package mobility

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	ObservedEventType  = "routerd.client.ipv4.observed"
	ExpiredEventType   = "routerd.client.ipv4.expired"
	HeartbeatEventType = federation.MobilityMemberHeartbeatType
	staticOwnedType    = "routerd.mobility.static-owned"
	staticHandoverType = "routerd.mobility.static-handover"

	DefaultLeaseTTL      = 5 * time.Minute
	DefaultHoldDuration  = 30 * time.Second
	statusPhaseProjected = "Projected"
)

const (
	bgpMobilityLocalPrefBase uint32 = 200

	bgpMobilityCommunityOwner          = "64512:100"
	bgpMobilityCommunityRoleOnPrem     = "64512:101"
	bgpMobilityCommunityRoleCloud      = "64512:102"
	bgpMobilityCommunitySourceObserved = "64512:110"
	bgpMobilityCommunitySourceStatic   = "64512:111"
	bgpMobilityCommunitySourceHandover = "64512:112"
	bgpMobilityCommunityFailover       = "64512:120"
)

type Store interface {
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	RecordFederationEvent(routerstate.EventRecord) error
	UpsertAddressLease(routerstate.AddressLeaseRecord) error
	ListAddressLeases(pool string, includeExpired bool, now time.Time) ([]routerstate.AddressLeaseRecord, error)
	ReconcileMobilityCaptureEpochs([]routerstate.MobilityCaptureEpochRecord) ([]routerstate.MobilityCaptureEpochRecord, error)
	GetMobilityCaptureEpoch(key string) (routerstate.MobilityCaptureEpochRecord, bool, error)
	ReconcileMobilityOwnershipEpochs([]routerstate.MobilityOwnershipEpochRecord) ([]routerstate.MobilityOwnershipEpochRecord, error)
	ListMobilityOwnershipEpochs(pool string) ([]routerstate.MobilityOwnershipEpochRecord, error)
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
	ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
	UpsertMobilityDeprovisionMarker(routerstate.MobilityDeprovisionMarkerRecord) error
	ListMobilityDeprovisionMarkers(source string) ([]routerstate.MobilityDeprovisionMarkerRecord, error)
	DeleteMobilityDeprovisionMarker(key string) error
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type BGPPathClient interface {
	ListPaths(ctx context.Context, source string) ([]bgpdaemon.AppliedPath, error)
	UpsertPath(ctx context.Context, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedPath, error)
	DeletePath(ctx context.Context, path bgpdaemon.AppliedPath) error
}

type Controller struct {
	Router   *api.Router
	Bus      *bus.Bus
	Store    Store
	BGPPaths BGPPathClient
	Now      func() time.Time
}

func (c Controller) HandleEvent(ctx context.Context, _ daemonapi.DaemonEvent) error {
	return c.Reconcile(ctx)
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := c.now()
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		if err := c.emitHeartbeat(res, now); err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"phase":  "Degraded",
				"reason": err.Error(),
			})
			continue
		}
		if err := c.reconcilePool(res, now); err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"phase":  "Degraded",
				"reason": err.Error(),
			})
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"plannerPhase":  "Degraded",
				"plannerReason": err.Error(),
				"plannedAt":     now.Format(time.RFC3339Nano),
			})
			continue
		}
		if mobilityBGPMode(spec) {
			if err := c.reconcileBGPDelivery(ctx, res, spec, now); err != nil {
				_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
					"plannerPhase":  "Degraded",
					"plannerReason": err.Error(),
					"plannedAt":     now.Format(time.RFC3339Nano),
				})
			}
			continue
		}
		if err := c.reconcileBGPDeliveryDisabled(ctx, res, spec, now); err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"plannerPhase":  "Degraded",
				"plannerReason": err.Error(),
				"plannedAt":     now.Format(time.RFC3339Nano),
			})
			continue
		}
		if err := c.reconcilePlan(res, now); err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"plannerPhase":  "Degraded",
				"plannerReason": err.Error(),
				"plannedAt":     now.Format(time.RFC3339Nano),
			})
		}
	}
	return nil
}

func (c Controller) reconcileBGPDelivery(ctx context.Context, res api.Resource, spec api.MobilityPoolSpec, now time.Time) error {
	if c.BGPPaths == nil {
		return fmt.Errorf("MobilityPool/%s deliveryPolicy.mode=bgp requires routerd-bgp control client", res.Metadata.Name)
	}
	selfNode, err := c.selfNode(spec.GroupRef)
	if err != nil {
		return err
	}
	if _, ok := plannerMembers(spec.Members)[selfNode]; !ok {
		return fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, res.Metadata.Name)
	}
	spec, selfCaptureResolved, selfCaptureReason := c.specWithDiscoveredSelfCapture(res.Metadata.Name, selfNode, spec)
	source := DynamicSource(res.Metadata.Name, selfNode)
	leases, err := c.Store.ListAddressLeases(res.Metadata.Name, false, now)
	if err != nil {
		return fmt.Errorf("list address leases: %w", err)
	}
	liveness, err := c.ownershipLiveness(res.Metadata.Name, spec, now)
	if err != nil {
		return err
	}
	ownershipEpochs, err := c.reconcileBGPOwnership(res.Metadata.Name, spec, leases, liveness, now)
	if err != nil {
		return err
	}
	desired := bgpOwnedPaths(res.Metadata.Name, source, selfNode, spec, leases, ownershipEpochs, now)
	actionJournal, err := c.Store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return fmt.Errorf("list action journal: %w", err)
	}
	markers, err := c.Store.ListMobilityDeprovisionMarkers(source)
	if err != nil {
		return fmt.Errorf("list BGP mobility deprovision markers: %w", err)
	}
	previousActionPlans, err := c.previousGeneratedActionPlans(res.Metadata.Name, selfNode)
	if err != nil {
		return err
	}
	installedNextHops, bgpRIBObserved := c.bgpInstalledNextHops()
	desiredTrapAddresses, err := bgpTrapAddresses(res.Metadata.Name, selfNode, spec, ownershipEpochs, installedNextHops, bgpRIBObserved, previousActionPlans, liveness)
	if err != nil {
		return err
	}
	captureEpochDesired, err := desiredBGPCaptureEpochs(res.Metadata.Name, spec, ownershipEpochs, liveness)
	if err != nil {
		return err
	}
	captureEpochs, err := c.Store.ReconcileMobilityCaptureEpochs(captureEpochDesired)
	if err != nil {
		return fmt.Errorf("reconcile BGP mobility capture epochs: %w", err)
	}
	markers, err = c.completeDeprovisionMarkers(markers, actionJournal)
	if err != nil {
		return err
	}
	markers, err = c.dropStaleDeprovisionMarkers(markers, captureEpochsByKey(captureEpochs))
	if err != nil {
		return err
	}
	var actionPlans []dynamicconfig.ActionPlan
	if len(desiredTrapAddresses) > 0 && !selfCaptureResolved {
		actionPlans = nil
	} else {
		actionPlans, err = bgpProviderActionPlans(res.Metadata.Name, selfNode, spec, desiredTrapAddresses, previousActionPlans, markers, cloudProviderProfiles(c.Router), captureEpochs, ownershipEpochs, actionJournal, now)
		if err != nil {
			return err
		}
	}
	if err := c.persistDeprovisionMarkers(source, actionPlans); err != nil {
		return err
	}
	current, err := c.BGPPaths.ListPaths(ctx, source)
	if err != nil {
		return fmt.Errorf("list BGP mobility paths: %w", err)
	}
	currentByPrefix := map[string]bgpdaemon.AppliedPath{}
	for _, path := range current {
		currentByPrefix[path.Prefix] = path
	}
	for _, path := range desired {
		if _, err := c.BGPPaths.UpsertPath(ctx, path); err != nil {
			return fmt.Errorf("upsert BGP mobility path %s: %w", path.Prefix, err)
		}
		delete(currentByPrefix, path.Prefix)
	}
	for _, path := range currentByPrefix {
		if err := c.BGPPaths.DeletePath(ctx, path); err != nil {
			return fmt.Errorf("delete stale BGP mobility path %s: %w", path.Prefix, err)
		}
	}
	if err := c.upsertBGPPlan(res.Metadata.Name, spec, selfNode, actionPlans, now); err != nil {
		return err
	}
	status := map[string]any{
		"plannerPhase":       "BGPPlanned",
		"plannerReason":      "deliveryPolicy.mode=bgp",
		"selfNode":           selfNode,
		"dynamicSource":      source,
		"deliveryMode":       "bgp",
		"bgpPathSource":      source,
		"generatedBGPPaths":  len(desired),
		"bgpRIBObserved":     bgpRIBObserved,
		"generatedBGPTraps":  len(desiredTrapAddresses),
		"generatedClaims":    0,
		"generatedActions":   len(actionPlans),
		"plannedAt":          now.Format(time.RFC3339Nano),
		"operatorIntent":     "MobilityPool",
		"derivedConfigKinds": []string{"BGPPath"},
	}
	if len(desiredTrapAddresses) > 0 && !selfCaptureResolved {
		status["plannerPhase"] = "Degraded"
		status["plannerReason"] = selfCaptureReason
		status["providerActionPhase"] = "Blocked"
	}
	if ipOwnershipPolicyCentralized(spec.IPOwnershipPolicy) {
		status["ownershipPolicy"] = "centralized"
		status["ownershipCount"] = len(ownershipEpochs)
		status["ownershipMap"] = ownershipStatusMap(ownershipEpochs)
		if spec.IPOwnershipPolicy.AutoFailover {
			status["autoFailover"] = true
			status["streamMaxObservedAt"] = liveness.StreamMaxObservedAt.Format(time.RFC3339Nano)
			status["staleMembers"] = staleMembersStatus(liveness.StaleNodes)
		}
	}
	return c.savePlannerStatus(res.Metadata.Name, status)
}

func (c Controller) specWithDiscoveredSelfCapture(poolName, selfNode string, spec api.MobilityPoolSpec) (api.MobilityPoolSpec, bool, string) {
	for i := range spec.Members {
		member := &spec.Members[i]
		if strings.TrimSpace(member.NodeRef) != strings.TrimSpace(selfNode) {
			continue
		}
		if member.Capture.Type != "provider-secondary-ip" || member.OwnershipDiscovery.Mode != "provider-private-ip" {
			return spec, true, ""
		}
		if strings.TrimSpace(member.Capture.NICRef) != "" {
			return spec, true, ""
		}
		status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
		nicRef := strings.TrimSpace(fmt.Sprint(status["discoverySelfNICRef"]))
		if nicRef == "" || nicRef == "<nil>" {
			return spec, false, "provider inventory self NIC is unresolved"
		}
		member.Capture.NICRef = nicRef
		if strings.TrimSpace(member.OwnershipDiscovery.SubnetRef) == "" {
			if subnetRef := strings.TrimSpace(fmt.Sprint(status["discoverySelfSubnetRef"])); subnetRef != "" && subnetRef != "<nil>" {
				member.OwnershipDiscovery.SubnetRef = subnetRef
				if member.Capture.Target == nil {
					member.Capture.Target = map[string]string{}
				}
				if strings.TrimSpace(member.Capture.Target["subnetRef"]) == "" {
					member.Capture.Target["subnetRef"] = subnetRef
				}
			}
		}
		return spec, true, ""
	}
	return spec, true, ""
}

func (c Controller) reconcileBGPOwnership(poolName string, spec api.MobilityPoolSpec, leases []routerstate.AddressLeaseRecord, liveness OwnershipLiveness, now time.Time) ([]routerstate.MobilityOwnershipEpochRecord, error) {
	var desired []routerstate.MobilityOwnershipEpochRecord
	if ipOwnershipPolicyCentralized(spec.IPOwnershipPolicy) {
		var err error
		desired, err = desiredOwnershipEpochs(poolName, spec, leases, liveness, now)
		if err != nil {
			return nil, err
		}
	} else {
		desired = bgpLeaseOwnershipEpochs(poolName, spec, leases, now)
	}
	ownership, err := c.Store.ReconcileMobilityOwnershipEpochs(desired)
	if err != nil {
		return nil, fmt.Errorf("reconcile BGP mobility ownership epochs: %w", err)
	}
	return ownership, nil
}

func (c Controller) upsertBGPPlan(poolName string, spec api.MobilityPoolSpec, selfNode string, actionPlans []dynamicconfig.ActionPlan, now time.Time) error {
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("mobility-" + poolName + "-" + selfNode),
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "MobilityPool",
				Name:       poolName,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      DynamicSource(poolName, selfNode),
			Generation:  dynamicGeneration,
			ObservedAt:  now,
			ExpiresAt:   now.Add(durationDefault(spec.LeasePolicy.TTL, DefaultLeaseTTL)),
			Resources:   []api.Resource{},
			Directives:  []dynamicconfig.DynamicConfigDirective{},
			ActionPlans: dedupeActionPlans(actionPlans),
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := dynamicPartRecord(part)
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}

func (c Controller) reconcileBGPDeliveryDisabled(ctx context.Context, res api.Resource, spec api.MobilityPoolSpec, now time.Time) error {
	if c.BGPPaths == nil {
		return nil
	}
	selfNode, err := c.selfNode(spec.GroupRef)
	if err != nil {
		return err
	}
	source := DynamicSource(res.Metadata.Name, selfNode)
	paths, err := c.BGPPaths.ListPaths(ctx, source)
	if err != nil {
		return nil
	}
	for _, path := range paths {
		if err := c.BGPPaths.DeletePath(ctx, path); err != nil {
			continue
		}
	}
	_ = now
	return nil
}

func (c Controller) emitHeartbeat(res api.Resource, now time.Time) error {
	spec, err := res.MobilityPoolSpec()
	if err != nil {
		return err
	}
	if !ipOwnershipAutoFailover(spec.IPOwnershipPolicy) {
		return nil
	}
	selfNode, err := c.selfNode(spec.GroupRef)
	if err != nil {
		return err
	}
	self, ok := plannerMembers(spec.Members)[selfNode]
	if !ok || self.Role != "cloud" || self.Capture.Type != "provider-secondary-ip" {
		return nil
	}
	interval := durationDefault(spec.IPOwnershipPolicy.HeartbeatInterval, 0)
	if interval <= 0 {
		return fmt.Errorf("MobilityPool/%s ipOwnershipPolicy.heartbeatInterval is required when autoFailover is true", res.Metadata.Name)
	}
	events, err := c.Store.ListFederationEvents(spec.GroupRef, false, now.Unix())
	if err != nil {
		return fmt.Errorf("list federation events for heartbeat: %w", err)
	}
	var last time.Time
	for _, ev := range events {
		if ev.Type != HeartbeatEventType || strings.TrimSpace(ev.SourceNode) != selfNode || strings.TrimSpace(ev.Payload["pool"]) != res.Metadata.Name {
			continue
		}
		if ev.ObservedAt.After(last) {
			last = ev.ObservedAt.UTC()
		}
	}
	if !last.IsZero() && last.Add(interval).After(now) {
		return nil
	}
	seq := strconv.FormatInt(now.UTC().UnixNano(), 10)
	emittedAt := now.UTC().Format(time.RFC3339Nano)
	return c.Store.RecordFederationEvent(routerstate.EventRecord{
		ID:         "mobility-heartbeat:" + res.Metadata.Name + ":" + selfNode + ":" + seq,
		Group:      spec.GroupRef,
		SourceNode: selfNode,
		Type:       HeartbeatEventType,
		Subject:    res.Metadata.Name + "/" + selfNode,
		DedupeKey:  "mobility-heartbeat:" + res.Metadata.Name + ":" + selfNode,
		Payload: map[string]string{
			"pool":      res.Metadata.Name,
			"node":      selfNode,
			"emittedAt": emittedAt,
			"seq":       seq,
		},
		ObservedAt: now.UTC(),
		RecordedAt: now.UTC(),
	})
}

func (c Controller) reconcilePool(res api.Resource, now time.Time) error {
	spec, err := res.MobilityPoolSpec()
	if err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return err
	}
	prefix = prefix.Masked()
	members := mobilityMembers(spec.Members)
	ttl := durationDefault(spec.LeasePolicy.TTL, DefaultLeaseTTL)
	hold := durationDefault(spec.LeasePolicy.HoldDuration, DefaultHoldDuration)
	handoversByFrom := staticHandoversByFrom(spec.StaticHandovers, prefix)

	events, err := c.Store.ListFederationEvents(spec.GroupRef, false, now.Unix())
	if err != nil {
		return fmt.Errorf("list federation events: %w", err)
	}
	latestObserved := map[string]leaseCandidate{}
	latestExpired := map[string]leaseCandidate{}
	for _, ev := range events {
		if ev.Type != ObservedEventType && ev.Type != ExpiredEventType {
			continue
		}
		member, ok := members[strings.TrimSpace(ev.SourceNode)]
		if !ok {
			continue
		}
		address, ok := normalizeLeaseAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), prefix)
		if !ok {
			continue
		}
		if ev.Type == ObservedEventType {
			if _, moving := handoversByFrom[staticHandoverKey(address, strings.TrimSpace(ev.SourceNode))]; moving {
				continue
			}
		}
		candidate := leaseCandidate{
			Address:    address,
			OwnerNode:  strings.TrimSpace(ev.SourceNode),
			OwnerSite:  member.Site,
			OwnerRole:  member.Role,
			EventID:    ev.ID,
			Group:      ev.Group,
			Type:       ev.Type,
			DedupeKey:  ev.DedupeKey,
			ObservedAt: ev.ObservedAt.UTC(),
			ExpiresAt:  eventExpiresAt(ev, ttl, now),
		}
		if candidate.ObservedAt.IsZero() {
			candidate.ObservedAt = now
		}
		switch ev.Type {
		case ObservedEventType:
			if existing, found := latestObserved[address]; !found || candidate.Greater(existing) {
				latestObserved[address] = candidate
			}
		case ExpiredEventType:
			if existing, found := latestExpired[address]; !found || candidate.Greater(existing) {
				latestExpired[address] = candidate
			}
		}
	}

	existing, err := c.Store.ListAddressLeases(res.Metadata.Name, true, now)
	if err != nil {
		return fmt.Errorf("list address leases: %w", err)
	}
	existingByAddress := map[string]routerstate.AddressLeaseRecord{}
	addresses := map[string]bool{}
	for _, lease := range existing {
		existingByAddress[lease.Address] = lease
		addresses[lease.Address] = true
	}
	staticObserved, staticExpired, staticEvents, err := c.staticLeaseProjection(res.Metadata.Name, spec, members, prefix, handoversByFrom, existingByAddress, latestExpired, now)
	if err != nil {
		return err
	}
	for _, ev := range staticEvents {
		if err := c.Store.RecordFederationEvent(ev); err != nil {
			return fmt.Errorf("record static mobility event %q: %w", ev.ID, err)
		}
	}
	for address, candidate := range staticObserved {
		if existing, found := latestObserved[address]; !found || candidate.Greater(existing) {
			latestObserved[address] = candidate
		}
	}
	for address, candidate := range staticExpired {
		if existing, found := latestExpired[address]; !found || candidate.Greater(existing) {
			latestExpired[address] = candidate
		}
	}
	for address := range latestObserved {
		addresses[address] = true
	}
	for address := range latestExpired {
		addresses[address] = true
	}
	ordered := make([]string, 0, len(addresses))
	for address := range addresses {
		ordered = append(ordered, address)
	}
	sort.Strings(ordered)

	counts := map[string]int{}
	for _, address := range ordered {
		current, hasCurrent := existingByAddress[address]
		projected, ok := projectLease(res.Metadata.Name, current, hasCurrent, latestObserved[address], latestExpired[address], now, hold)
		if !ok {
			continue
		}
		if err := c.Store.UpsertAddressLease(projected); err != nil {
			return err
		}
		counts[projected.Status]++
	}
	status := map[string]any{
		"phase":          statusPhaseProjected,
		"groupRef":       spec.GroupRef,
		"prefix":         prefix.String(),
		"leaseCount":     counts[routerstate.AddressLeaseStatusActive] + counts[routerstate.AddressLeaseStatusHolding] + counts[routerstate.AddressLeaseStatusExpired],
		"activeLeases":   counts[routerstate.AddressLeaseStatusActive],
		"holdingLeases":  counts[routerstate.AddressLeaseStatusHolding],
		"expiredLeases":  counts[routerstate.AddressLeaseStatusExpired],
		"projectedAt":    now.Format(time.RFC3339Nano),
		"managedBy":      "routerd",
		"management":     "managed",
		"operatorIntent": "MobilityPool",
	}
	return c.savePlannerStatus(res.Metadata.Name, status)
}

func (c Controller) staticLeaseProjection(poolName string, spec api.MobilityPoolSpec, members map[string]memberInfo, prefix netip.Prefix, handoversByFrom map[string]api.MobilityStaticHandover, existing map[string]routerstate.AddressLeaseRecord, expiredEvents map[string]leaseCandidate, now time.Time) (map[string]leaseCandidate, map[string]leaseCandidate, []routerstate.EventRecord, error) {
	observed := map[string]leaseCandidate{}
	expired := map[string]leaseCandidate{}
	var events []routerstate.EventRecord
	selfNode, selfErr := c.selfNode(spec.GroupRef)
	if selfErr != nil && hasStaticMobilityIntent(spec) {
		return nil, nil, nil, selfErr
	}

	for _, member := range spec.Members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		info, ok := members[nodeRef]
		if !ok {
			continue
		}
		for _, raw := range member.StaticOwnedAddresses {
			address, ok := normalizeLeaseAddress(raw, prefix)
			if !ok {
				continue
			}
			if _, moving := handoversByFrom[staticHandoverKey(address, nodeRef)]; moving {
				continue
			}
			candidate := staticOwnedCandidate(poolName, spec.GroupRef, address, nodeRef, info, existing[address], now)
			observed[address] = candidate
			if ev, emit := staticObservedFederationEvent(poolName, spec.GroupRef, address, nodeRef, selfNode, selfErr, existing[address], now); emit {
				events = append(events, ev)
			}
		}
	}

	for _, handover := range spec.StaticHandovers {
		address, ok := normalizeLeaseAddress(handover.Address, prefix)
		if !ok {
			continue
		}
		fromNode := strings.TrimSpace(handover.FromNodeRef)
		fromInfo, ok := members[fromNode]
		if !ok {
			continue
		}
		current := existing[address]
		if selfNode == fromNode && current.OwnerNode == fromNode && current.Status != routerstate.AddressLeaseStatusExpired && isStaticLeaseSource(current.SourceType) {
			candidate, ok := staticExpiredCandidate(poolName, spec.GroupRef, address, fromNode, fromInfo, now)
			if ok {
				expired[address] = candidate
			}
		}
		if ev, emit := staticExpiredFederationEvent(poolName, spec.GroupRef, address, fromNode, selfNode, selfErr, current, expiredEvents[address], now); emit {
			events = append(events, ev)
		}
		release := expiredEvents[address]
		if staticHandoverReleaseObserved(handover, current, release) {
			if toInfo, ok := members[strings.TrimSpace(handover.ToNodeRef)]; ok {
				observed[address] = staticHandoverCandidate(poolName, spec.GroupRef, address, strings.TrimSpace(handover.ToNodeRef), toInfo, release, now)
			}
		}
	}

	for address, current := range existing {
		if !isStaticLeaseSource(current.SourceType) || current.Status == routerstate.AddressLeaseStatusExpired {
			continue
		}
		if _, stillOwned := observed[address]; stillOwned {
			continue
		}
		if _, moving := handoversByFrom[staticHandoverKey(address, current.OwnerNode)]; moving {
			continue
		}
		info, ok := members[strings.TrimSpace(current.OwnerNode)]
		if !ok {
			continue
		}
		candidate, ok := staticExpiredCandidate(poolName, spec.GroupRef, address, current.OwnerNode, info, now)
		if !ok {
			continue
		}
		if latest, found := expired[address]; !found || candidate.Greater(latest) {
			expired[address] = candidate
		}
		if ev, emit := staticExpiredFederationEvent(poolName, spec.GroupRef, address, current.OwnerNode, selfNode, selfErr, current, expiredEvents[address], now); emit {
			events = append(events, ev)
		}
	}
	return observed, expired, events, nil
}

func hasStaticMobilityIntent(spec api.MobilityPoolSpec) bool {
	if len(spec.StaticHandovers) > 0 {
		return true
	}
	for _, member := range spec.Members {
		if len(member.StaticOwnedAddresses) > 0 {
			return true
		}
	}
	return false
}

func mobilityBGPMode(spec api.MobilityPoolSpec) bool {
	return strings.TrimSpace(spec.DeliveryPolicy.Mode) == "bgp"
}

func bgpOwnedPaths(poolName, source, selfNode string, spec api.MobilityPoolSpec, leases []routerstate.AddressLeaseRecord, ownership []routerstate.MobilityOwnershipEpochRecord, now time.Time) []bgpdaemon.AppliedPath {
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil
	}
	poolPrefix = poolPrefix.Masked()
	members := plannerMembers(spec.Members)
	self := members[strings.TrimSpace(selfNode)]
	owners := bgpOwnershipByAddress(ownership)
	var out []bgpdaemon.AppliedPath
	for _, lease := range leases {
		if lease.Pool != poolName || lease.Status != routerstate.AddressLeaseStatusActive {
			continue
		}
		if !lease.ExpiresAt.IsZero() && !now.Before(lease.ExpiresAt) {
			continue
		}
		address, ok := normalizeLeaseAddress(lease.Address, poolPrefix)
		if !ok {
			continue
		}
		owner, ok := owners[address]
		if !ok || strings.TrimSpace(owner.OwnerNode) != strings.TrimSpace(selfNode) {
			continue
		}
		prefix, err := netip.ParsePrefix(address)
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		out = append(out, bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: source,
			Prefix: prefix.String(),
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
			Attrs:  bgpMobilityPathAttrs(self, lease, owner),
		}))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Prefix < out[j].Prefix
	})
	return out
}

func desiredBGPCaptureEpochs(poolName string, spec api.MobilityPoolSpec, ownership []routerstate.MobilityOwnershipEpochRecord, liveness OwnershipLiveness) ([]routerstate.MobilityCaptureEpochRecord, error) {
	members := plannerMembers(spec.Members)
	seen := map[string]bool{}
	var out []routerstate.MobilityCaptureEpochRecord
	for _, owner := range ownershipStatusOrder(ownership) {
		ownerMember, ok := members[strings.TrimSpace(owner.OwnerNode)]
		if !ok {
			continue
		}
		address := strings.TrimSpace(owner.Address)
		if address == "" {
			continue
		}
		for _, member := range members {
			if member.Capture.Type != "provider-secondary-ip" || member.Site == ownerMember.Site {
				continue
			}
			placement := evaluatePlacementWithLiveness(member, members, spec.IPOwnershipPolicy, liveness)
			if !placement.Active {
				continue
			}
			domain := captureDomain(member)
			key := captureEpochKey(poolName, address, domain)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, routerstate.MobilityCaptureEpochRecord{
				CaptureKey:    key,
				Pool:          poolName,
				Address:       address,
				CaptureDomain: domain,
				Holder:        member.NodeRef,
			})
		}
	}
	return out, nil
}

func bgpProviderActionPlans(poolName, selfNode string, spec api.MobilityPoolSpec, desiredTrapAddresses map[string]bool, previousPlans []dynamicconfig.ActionPlan, markers []routerstate.MobilityDeprovisionMarkerRecord, profiles map[string]api.CloudProviderProfileSpec, captureEpochs []routerstate.MobilityCaptureEpochRecord, ownership []routerstate.MobilityOwnershipEpochRecord, actionJournal []routerstate.ActionExecutionRecord, now time.Time) ([]dynamicconfig.ActionPlan, error) {
	members := plannerMembers(spec.Members)
	self, ok := members[strings.TrimSpace(selfNode)]
	if !ok {
		return nil, fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, poolName)
	}
	ownershipByAddress := ownershipEpochsByAddress(ownership)
	captureEpochByKey := captureEpochsByKey(captureEpochs)
	desiredAddresses := map[string]bool{}
	desiredProviderNICs := map[string]bool{}
	var plans []dynamicconfig.ActionPlan
	forwardingSeen := map[string]bool{}
	if self.Capture.Type == "provider-secondary-ip" {
		profile, ok := profiles[strings.TrimSpace(self.Capture.ProviderRef)]
		if !ok {
			return nil, fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q", self.Capture.ProviderRef, poolName, self.NodeRef)
		}
		for _, address := range mapKeysSorted(desiredTrapAddresses) {
			address = strings.TrimSpace(address)
			if address == "" {
				continue
			}
			desiredAddresses[address] = true
			if key := providerNICKey("", self.Capture.ProviderRef, self.Capture.NICRef); key != "" {
				desiredProviderNICs[key] = true
			}
			captureKey := captureEpochKey(poolName, address, captureDomain(self))
			seize := shouldStartOrMaintainBGPCaptureTakeover(captureEpochByKey[captureKey], previousPlans, actionJournal)
			generated, err := providerActionPlans(poolName, profile, self.Capture, self.CaptureTarget, address, forwardingSeen, seize)
			if err != nil {
				return nil, err
			}
			stampCaptureEpochActionPlans(generated, captureEpochByKey, captureKey)
			stampOwnershipEpochActionPlans(generated, ownershipByAddress[address])
			plans = append(plans, generated...)
		}
	}
	deprovisionPlans, err := bgpProviderDeprovisionPlans(poolName, self, previousPlans, desiredAddresses, desiredProviderNICs, profiles, captureEpochByKey, ownershipByAddress, now)
	if err != nil {
		return nil, err
	}
	plans = append(plans, deprovisionPlans...)
	markerPlans, err := actionPlansFromMarkers(markers)
	if err != nil {
		return nil, err
	}
	markerPlans = filterStaleCaptureEpochPlans(markerPlans, captureEpochByKey)
	for _, plan := range markerPlans {
		if desiredAddresses[strings.TrimSpace(plan.Target["address"])] {
			continue
		}
		plans = append(plans, plan)
	}
	return dedupeActionPlans(plans), nil
}

func (c Controller) bgpInstalledNextHops() (map[string][]string, bool) {
	out := map[string][]string{}
	observed := false
	if c.Router == nil || c.Store == nil {
		return out, observed
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := c.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		raw, ok := status["installedNextHops"]
		if !ok {
			continue
		}
		observed = true
		for prefix, nextHops := range bgpInstalledNextHopsValue(raw) {
			out[prefix] = mergeStringSet(out[prefix], nextHops)
		}
	}
	return out, observed
}

func bgpInstalledNextHopsValue(value any) map[string][]string {
	out := map[string][]string{}
	switch typed := value.(type) {
	case map[string][]string:
		for prefix, hops := range typed {
			out[strings.TrimSpace(prefix)] = cleanStrings(hops)
		}
	case map[string]any:
		for prefix, raw := range typed {
			out[strings.TrimSpace(prefix)] = statusStringSlice(raw)
		}
	}
	return out
}

func bgpTrapAddresses(poolName, selfNode string, spec api.MobilityPoolSpec, ownership []routerstate.MobilityOwnershipEpochRecord, installedNextHops map[string][]string, ribObserved bool, previousPlans []dynamicconfig.ActionPlan, liveness OwnershipLiveness) (map[string]bool, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil, fmt.Errorf("parse pool prefix: %w", err)
	}
	prefix = prefix.Masked()
	members := plannerMembers(spec.Members)
	self, ok := members[strings.TrimSpace(selfNode)]
	if !ok {
		return nil, fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, poolName)
	}
	out := map[string]bool{}
	if self.Capture.Type != "provider-secondary-ip" {
		return out, nil
	}
	placement := evaluatePlacementWithLiveness(self, members, spec.IPOwnershipPolicy, liveness)
	if !placement.Active {
		return out, nil
	}
	ownershipByAddress := bgpOwnershipByAddress(ownership)
	for rawPrefix, nextHops := range installedNextHops {
		if len(cleanStrings(nextHops)) == 0 {
			continue
		}
		address, ok := normalizeBGPTrapPrefix(rawPrefix, prefix)
		if !ok || !bgpAddressIsRemoteOwned(address, self, members, ownershipByAddress) {
			continue
		}
		out[address] = true
	}
	if !ribObserved {
		for _, address := range previousBGPTrapAddresses(self, previousPlans, prefix, members, ownershipByAddress) {
			out[address] = true
		}
	}
	return out, nil
}

func normalizeBGPTrapPrefix(value string, poolPrefix netip.Prefix) (string, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() != 32 || !poolPrefix.Contains(prefix.Addr()) {
		return "", false
	}
	return prefix.String(), true
}

func bgpAddressIsRemoteOwned(address string, self memberPlanInfo, members map[string]memberPlanInfo, ownership map[string]routerstate.MobilityOwnershipEpochRecord) bool {
	owner, ok := ownership[strings.TrimSpace(address)]
	if !ok {
		return false
	}
	ownerMember, ok := members[strings.TrimSpace(owner.OwnerNode)]
	if !ok {
		return false
	}
	return strings.TrimSpace(ownerMember.Site) != "" && strings.TrimSpace(ownerMember.Site) != strings.TrimSpace(self.Site)
}

func previousBGPTrapAddresses(self memberPlanInfo, previousPlans []dynamicconfig.ActionPlan, poolPrefix netip.Prefix, members map[string]memberPlanInfo, ownership map[string]routerstate.MobilityOwnershipEpochRecord) []string {
	seen := map[string]bool{}
	for _, plan := range previousPlans {
		if plan.Action != "assign-secondary-ip" {
			continue
		}
		capture := captureFromActionPlan(self.Capture, plan)
		if capture.Type != "provider-secondary-ip" {
			continue
		}
		if strings.TrimSpace(capture.ProviderRef) != strings.TrimSpace(self.Capture.ProviderRef) || strings.TrimSpace(capture.NICRef) != strings.TrimSpace(self.Capture.NICRef) {
			continue
		}
		address, ok := normalizeBGPTrapPrefix(plan.Target["address"], poolPrefix)
		if !ok || !bgpAddressIsRemoteOwned(address, self, members, ownership) {
			continue
		}
		seen[address] = true
	}
	return mapKeysSorted(seen)
}

func statusStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return cleanStrings(typed)
	case []any:
		var out []string
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" {
				out = append(out, value)
			}
		}
		return cleanStrings(out)
	default:
		if value := strings.TrimSpace(fmt.Sprint(value)); value != "" && value != "<nil>" {
			return []string{value}
		}
	}
	return nil
}

func mergeStringSet(base []string, extra []string) []string {
	seen := map[string]bool{}
	for _, value := range base {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = true
		}
	}
	for _, value := range extra {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = true
		}
	}
	return mapKeysSorted(seen)
}

func cleanStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = true
		}
	}
	return mapKeysSorted(seen)
}

func mapKeysSorted(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func shouldStartOrMaintainBGPCaptureTakeover(current routerstate.MobilityCaptureEpochRecord, previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord) bool {
	if current.Epoch <= 1 || strings.TrimSpace(current.CaptureKey) == "" || strings.TrimSpace(current.Holder) == "" {
		return false
	}
	if captureAssignSucceeded(current, journal) {
		return false
	}
	return true
}

func bgpProviderDeprovisionPlans(poolName string, self memberPlanInfo, previousPlans []dynamicconfig.ActionPlan, desiredAddresses, desiredProviderNICs map[string]bool, profiles map[string]api.CloudProviderProfileSpec, captureEpochs map[string]routerstate.MobilityCaptureEpochRecord, ownership map[string]routerstate.MobilityOwnershipEpochRecord, now time.Time) ([]dynamicconfig.ActionPlan, error) {
	var plans []dynamicconfig.ActionPlan
	forwardingDisabled := map[string]bool{}
	for _, previous := range sortedActionPlans(previousPlans) {
		if previous.Action != "assign-secondary-ip" {
			continue
		}
		address := strings.TrimSpace(previous.Target["address"])
		if address == "" || desiredAddresses[address] {
			continue
		}
		capture := captureFromActionPlan(self.Capture, previous)
		if capture.Type != "provider-secondary-ip" {
			continue
		}
		profileRef := firstNonEmpty(previous.ProviderRef, capture.ProviderRef)
		profile, ok := profiles[strings.TrimSpace(profileRef)]
		if !ok {
			return nil, fmt.Errorf("CloudProviderProfile/%s not found for stale BGP MobilityPool/%s action %q", profileRef, poolName, previous.Name)
		}
		captureTarget := copyStringMap(previous.Target)
		unassign, err := providerUnassignActionPlan(poolName, profile, capture, captureTarget, address, now)
		if err != nil {
			return nil, err
		}
		captureKey := captureEpochKey(poolName, address, captureDomain(self))
		stampCaptureEpochActionPlanHolder(&unassign, captureEpochs, captureKey, self.NodeRef)
		stampOwnershipEpochActionPlanOwner(&unassign, ownership[address], self.NodeRef)
		plans = append(plans, unassign)

		nicKey := providerNICKey("", capture.ProviderRef, capture.NICRef)
		if nicKey == "" || desiredProviderNICs[nicKey] || forwardingDisabled[nicKey] {
			continue
		}
		disable, err := providerForwardingDisableActionPlan(poolName, profile, capture, captureTarget, address)
		if err != nil {
			return nil, err
		}
		stampCaptureEpochActionPlanHolder(&disable, captureEpochs, captureKey, self.NodeRef)
		stampOwnershipEpochActionPlanOwner(&disable, ownership[address], self.NodeRef)
		plans = append(plans, disable)
		forwardingDisabled[nicKey] = true
	}
	return plans, nil
}

func captureFromActionPlan(fallback api.AddressCapture, plan dynamicconfig.ActionPlan) api.AddressCapture {
	capture := fallback
	capture.Type = "provider-secondary-ip"
	if value := strings.TrimSpace(plan.ProviderRef); value != "" {
		capture.ProviderRef = value
	}
	if value := strings.TrimSpace(plan.Target["providerRef"]); value != "" {
		capture.ProviderRef = value
	}
	if value := strings.TrimSpace(plan.Target["nicRef"]); value != "" {
		capture.NICRef = value
	}
	return capture
}

func sortedActionPlans(plans []dynamicconfig.ActionPlan) []dynamicconfig.ActionPlan {
	out := append([]dynamicconfig.ActionPlan(nil), plans...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Action == out[j].Action {
			return out[i].IdempotencyKey < out[j].IdempotencyKey
		}
		return out[i].Action < out[j].Action
	})
	return out
}

func bgpCommunityContains(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func ownershipStatusOrder(records []routerstate.MobilityOwnershipEpochRecord) []routerstate.MobilityOwnershipEpochRecord {
	out := append([]routerstate.MobilityOwnershipEpochRecord(nil), records...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Address == out[j].Address {
			return out[i].OwnerNode < out[j].OwnerNode
		}
		return out[i].Address < out[j].Address
	})
	return out
}

func bgpLeaseOwnershipEpochs(poolName string, spec api.MobilityPoolSpec, leases []routerstate.AddressLeaseRecord, now time.Time) []routerstate.MobilityOwnershipEpochRecord {
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil
	}
	poolPrefix = poolPrefix.Masked()
	members := plannerMembers(spec.Members)
	var out []routerstate.MobilityOwnershipEpochRecord
	for _, lease := range sortedLeases(leases) {
		if lease.Pool != poolName || lease.Status != routerstate.AddressLeaseStatusActive {
			continue
		}
		if !lease.ExpiresAt.IsZero() && !now.Before(lease.ExpiresAt) {
			continue
		}
		address, ok := normalizeLeaseAddress(lease.Address, poolPrefix)
		if !ok {
			continue
		}
		if _, ok := members[strings.TrimSpace(lease.OwnerNode)]; !ok {
			continue
		}
		epoch := lease.Epoch
		if epoch < 1 {
			epoch = 1
		}
		out = append(out, routerstate.MobilityOwnershipEpochRecord{
			Pool:      poolName,
			Address:   address,
			OwnerNode: strings.TrimSpace(lease.OwnerNode),
			Epoch:     epoch,
		})
	}
	return out
}

func bgpOwnershipByAddress(records []routerstate.MobilityOwnershipEpochRecord) map[string]routerstate.MobilityOwnershipEpochRecord {
	out := map[string]routerstate.MobilityOwnershipEpochRecord{}
	for _, rec := range records {
		if address := normalizeAddressString(rec.Address); address != "" {
			out[address] = rec
		}
	}
	return out
}

func bgpMobilityPathAttrs(member memberPlanInfo, lease routerstate.AddressLeaseRecord, owner routerstate.MobilityOwnershipEpochRecord) bgpdaemon.AppliedPathAttrs {
	communities := []string{bgpMobilityCommunityOwner}
	switch member.Role {
	case "onprem":
		communities = append(communities, bgpMobilityCommunityRoleOnPrem)
	case "cloud":
		communities = append(communities, bgpMobilityCommunityRoleCloud)
	}
	switch strings.TrimSpace(lease.SourceType) {
	case staticOwnedType:
		communities = append(communities, bgpMobilityCommunitySourceStatic)
	case staticHandoverType:
		communities = append(communities, bgpMobilityCommunitySourceHandover)
	default:
		communities = append(communities, bgpMobilityCommunitySourceObserved)
	}
	if strings.TrimSpace(lease.OwnerNode) != "" && strings.TrimSpace(lease.OwnerNode) != strings.TrimSpace(owner.OwnerNode) {
		communities = append(communities, bgpMobilityCommunityFailover)
	}
	attrs := bgpdaemon.AppliedPathAttrs{
		LocalPref:   bgpMobilityLocalPref(owner.Epoch),
		Communities: communities,
	}
	if member.PlacementPriority > 0 {
		attrs.MED = uint32(member.PlacementPriority)
	}
	return attrs
}

func bgpMobilityLocalPref(epoch int64) uint32 {
	if epoch < 1 {
		epoch = 1
	}
	const maxEpoch = int64(1000000)
	if epoch > maxEpoch {
		epoch = maxEpoch
	}
	return bgpMobilityLocalPrefBase + uint32(epoch)
}

func staticOwnedCandidate(poolName, group, address, nodeRef string, member memberInfo, current routerstate.AddressLeaseRecord, now time.Time) leaseCandidate {
	observedAt := now.UTC()
	if current.OwnerNode == nodeRef && current.Status == routerstate.AddressLeaseStatusActive && current.SourceType == staticOwnedType && !current.ObservedAt.IsZero() {
		observedAt = current.ObservedAt.UTC()
	}
	eventID := staticEventID(staticOwnedType, poolName, nodeRef, address, observedAt)
	return leaseCandidate{
		Address:    address,
		OwnerNode:  nodeRef,
		OwnerSite:  member.Site,
		OwnerRole:  member.Role,
		EventID:    eventID,
		Group:      group,
		Type:       staticOwnedType,
		DedupeKey:  staticDedupeKey(staticOwnedType, poolName, nodeRef, address),
		ObservedAt: observedAt,
	}
}

func staticExpiredCandidate(poolName, group, address, nodeRef string, member memberInfo, now time.Time) (leaseCandidate, bool) {
	if strings.TrimSpace(nodeRef) == "" || strings.TrimSpace(address) == "" {
		return leaseCandidate{}, false
	}
	observedAt := now.UTC()
	return leaseCandidate{
		Address:    address,
		OwnerNode:  strings.TrimSpace(nodeRef),
		OwnerSite:  member.Site,
		OwnerRole:  member.Role,
		EventID:    staticEventID(ExpiredEventType, poolName, nodeRef, address, observedAt),
		Group:      group,
		Type:       ExpiredEventType,
		DedupeKey:  staticDedupeKey(staticOwnedType, poolName, nodeRef, address),
		ObservedAt: observedAt,
		ExpiresAt:  observedAt,
	}, true
}

func staticHandoverCandidate(poolName, group, address, nodeRef string, member memberInfo, release leaseCandidate, now time.Time) leaseCandidate {
	observedAt := release.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = now.UTC()
	}
	observedAt = observedAt.Add(time.Nanosecond)
	return leaseCandidate{
		Address:    address,
		OwnerNode:  strings.TrimSpace(nodeRef),
		OwnerSite:  member.Site,
		OwnerRole:  member.Role,
		EventID:    staticEventID(staticHandoverType+":"+release.EventID, poolName, nodeRef, address, observedAt),
		Group:      group,
		Type:       staticHandoverType,
		DedupeKey:  staticDedupeKey(staticHandoverType, poolName, nodeRef, address),
		ObservedAt: observedAt,
	}
}

func staticObservedFederationEvent(poolName, group, address, ownerNode, selfNode string, selfErr error, current routerstate.AddressLeaseRecord, now time.Time) (routerstate.EventRecord, bool) {
	if selfErr != nil || strings.TrimSpace(selfNode) != strings.TrimSpace(ownerNode) {
		return routerstate.EventRecord{}, false
	}
	if current.OwnerNode == ownerNode && current.Status == routerstate.AddressLeaseStatusActive && current.SourceType == staticOwnedType {
		return routerstate.EventRecord{}, false
	}
	return staticFederationEvent(poolName, group, address, ownerNode, ObservedEventType, now), true
}

func staticExpiredFederationEvent(poolName, group, address, ownerNode, selfNode string, selfErr error, current routerstate.AddressLeaseRecord, latestExpired leaseCandidate, now time.Time) (routerstate.EventRecord, bool) {
	if selfErr != nil || strings.TrimSpace(selfNode) != strings.TrimSpace(ownerNode) {
		return routerstate.EventRecord{}, false
	}
	if current.Address != "" && (current.OwnerNode != strings.TrimSpace(ownerNode) || current.Status == routerstate.AddressLeaseStatusExpired || !isStaticLeaseSource(current.SourceType)) {
		return routerstate.EventRecord{}, false
	}
	if latestExpired.Address == address && latestExpired.OwnerNode == ownerNode && latestExpired.Greater(candidateFromLease(current)) {
		return routerstate.EventRecord{}, false
	}
	return staticFederationEvent(poolName, group, address, ownerNode, ExpiredEventType, now), true
}

func staticFederationEvent(poolName, group, address, ownerNode, eventType string, now time.Time) routerstate.EventRecord {
	observedAt := now.UTC()
	return routerstate.EventRecord{
		ID:         staticEventID(eventType, poolName, ownerNode, address, observedAt),
		Group:      group,
		SourceNode: strings.TrimSpace(ownerNode),
		Type:       eventType,
		Subject:    address,
		DedupeKey:  staticDedupeKey(staticOwnedType, poolName, ownerNode, address),
		Payload: map[string]string{
			"address": address,
			"pool":    poolName,
			"source":  staticOwnedType,
		},
		ObservedAt: observedAt,
		RecordedAt: observedAt,
	}
}

func staticHandoverReleaseObserved(handover api.MobilityStaticHandover, current routerstate.AddressLeaseRecord, release leaseCandidate) bool {
	if release.Address == "" || release.OwnerNode != strings.TrimSpace(handover.FromNodeRef) || release.Type != ExpiredEventType {
		return false
	}
	if current.OwnerNode == strings.TrimSpace(handover.FromNodeRef) && current.Status != routerstate.AddressLeaseStatusExpired {
		return release.Greater(candidateFromLease(current))
	}
	return true
}

func staticHandoversByFrom(handovers []api.MobilityStaticHandover, prefix netip.Prefix) map[string]api.MobilityStaticHandover {
	out := map[string]api.MobilityStaticHandover{}
	for _, handover := range handovers {
		address, ok := normalizeLeaseAddress(handover.Address, prefix)
		if !ok {
			continue
		}
		fromNode := strings.TrimSpace(handover.FromNodeRef)
		if fromNode == "" {
			continue
		}
		out[staticHandoverKey(address, fromNode)] = handover
	}
	return out
}

func staticHandoverKey(address, fromNode string) string {
	return strings.TrimSpace(address) + "|" + strings.TrimSpace(fromNode)
}

func isStaticLeaseSource(sourceType string) bool {
	switch strings.TrimSpace(sourceType) {
	case staticOwnedType, staticHandoverType:
		return true
	default:
		return false
	}
}

func staticDedupeKey(kind, poolName, nodeRef, address string) string {
	return strings.Join([]string{"mobility", kind, strings.TrimSpace(poolName), strings.TrimSpace(nodeRef), strings.ReplaceAll(strings.TrimSpace(address), "/", "_")}, ":")
}

func staticEventID(kind, poolName, nodeRef, address string, observedAt time.Time) string {
	return staticDedupeKey(kind, poolName, nodeRef, address) + ":" + strconv.FormatInt(observedAt.UTC().UnixNano(), 10)
}

func (c Controller) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

type memberInfo struct {
	Site string
	Role string
}

func mobilityMembers(members []api.MobilityPoolMember) map[string]memberInfo {
	out := map[string]memberInfo{}
	for _, member := range members {
		out[strings.TrimSpace(member.NodeRef)] = memberInfo{
			Site: strings.TrimSpace(member.Site),
			Role: strings.TrimSpace(member.Role),
		}
	}
	return out
}

type leaseCandidate struct {
	Address    string
	OwnerNode  string
	OwnerSite  string
	OwnerRole  string
	EventID    string
	Group      string
	Type       string
	DedupeKey  string
	ObservedAt time.Time
	ExpiresAt  time.Time
}

func (c leaseCandidate) Greater(other leaseCandidate) bool {
	if !c.ObservedAt.Equal(other.ObservedAt) {
		return c.ObservedAt.After(other.ObservedAt)
	}
	if c.EventID != other.EventID {
		return c.EventID > other.EventID
	}
	return c.OwnerNode > other.OwnerNode
}

func projectLease(pool string, current routerstate.AddressLeaseRecord, hasCurrent bool, observed leaseCandidate, expired leaseCandidate, now time.Time, hold time.Duration) (routerstate.AddressLeaseRecord, bool) {
	if !hasCurrent {
		if observed.Address == "" {
			return routerstate.AddressLeaseRecord{}, false
		}
		return leaseFromCandidate(pool, observed, 1, routerstate.AddressLeaseStatusActive, now), true
	}
	current.Pool = pool
	if current.Epoch <= 0 {
		current.Epoch = 1
	}
	if observed.Address == "" && current.CandidateEventID != "" {
		if current.CandidateExpiresAt.IsZero() || now.Before(current.CandidateExpiresAt) {
			observed = candidateFromPending(current)
		} else {
			clearCandidate(&current)
		}
	}
	if expired.Address != "" && expired.OwnerNode == current.OwnerNode && expired.Greater(candidateFromLease(current)) && (observed.Address == "" || expired.Greater(observed)) {
		current.Status = routerstate.AddressLeaseStatusExpired
		current.ExpiresAt = expired.ObservedAt
		current.SourceEventID = expired.EventID
		current.SourceGroup = expired.Group
		current.SourceType = expired.Type
		current.DedupeKey = expired.DedupeKey
		current.ConflictReason = ""
		clearCandidate(&current)
		return current, true
	}
	if observed.Address == "" {
		if !current.ExpiresAt.IsZero() && !now.Before(current.ExpiresAt) && current.Status != routerstate.AddressLeaseStatusExpired {
			current.Status = routerstate.AddressLeaseStatusExpired
			current.ConflictReason = ""
			clearCandidate(&current)
			return current, true
		}
		return current, true
	}
	if observed.OwnerNode == current.OwnerNode {
		next := leaseFromCandidate(pool, observed, current.Epoch, routerstate.AddressLeaseStatusActive, now)
		next.RecordedAt = current.RecordedAt
		return next, true
	}
	if observed.ObservedAt.Add(hold).After(now) {
		current.Status = routerstate.AddressLeaseStatusHolding
		current.CandidateOwnerNode = observed.OwnerNode
		current.CandidateOwnerSite = observed.OwnerSite
		current.CandidateOwnerRole = observed.OwnerRole
		current.CandidateEventID = observed.EventID
		current.CandidateGroup = observed.Group
		current.CandidateType = observed.Type
		current.CandidateDedupeKey = observed.DedupeKey
		current.CandidateObservedAt = observed.ObservedAt
		current.CandidateExpiresAt = observed.ExpiresAt
		current.ConflictReason = fmt.Sprintf("owner change from %s to %s held until %s", current.OwnerNode, observed.OwnerNode, observed.ObservedAt.Add(hold).UTC().Format(time.RFC3339Nano))
		return current, true
	}
	next := leaseFromCandidate(pool, observed, current.Epoch+1, routerstate.AddressLeaseStatusActive, now)
	next.RecordedAt = current.RecordedAt
	return next, true
}

func leaseFromCandidate(pool string, candidate leaseCandidate, epoch int64, status string, now time.Time) routerstate.AddressLeaseRecord {
	return routerstate.AddressLeaseRecord{
		Pool:          pool,
		Address:       candidate.Address,
		Status:        status,
		OwnerNode:     candidate.OwnerNode,
		OwnerSite:     candidate.OwnerSite,
		OwnerRole:     candidate.OwnerRole,
		Epoch:         epoch,
		ObservedAt:    candidate.ObservedAt,
		ExpiresAt:     candidate.ExpiresAt,
		SourceEventID: candidate.EventID,
		SourceGroup:   candidate.Group,
		SourceType:    candidate.Type,
		DedupeKey:     candidate.DedupeKey,
		RecordedAt:    now,
		UpdatedAt:     now,
	}
}

func candidateFromLease(lease routerstate.AddressLeaseRecord) leaseCandidate {
	return leaseCandidate{
		Address:    lease.Address,
		OwnerNode:  lease.OwnerNode,
		OwnerSite:  lease.OwnerSite,
		OwnerRole:  lease.OwnerRole,
		EventID:    lease.SourceEventID,
		Group:      lease.SourceGroup,
		Type:       lease.SourceType,
		DedupeKey:  lease.DedupeKey,
		ObservedAt: lease.ObservedAt,
		ExpiresAt:  lease.ExpiresAt,
	}
}

func candidateFromPending(lease routerstate.AddressLeaseRecord) leaseCandidate {
	return leaseCandidate{
		Address:    lease.Address,
		OwnerNode:  lease.CandidateOwnerNode,
		OwnerSite:  lease.CandidateOwnerSite,
		OwnerRole:  lease.CandidateOwnerRole,
		EventID:    lease.CandidateEventID,
		Group:      lease.CandidateGroup,
		Type:       lease.CandidateType,
		DedupeKey:  lease.CandidateDedupeKey,
		ObservedAt: lease.CandidateObservedAt,
		ExpiresAt:  lease.CandidateExpiresAt,
	}
}

func clearCandidate(lease *routerstate.AddressLeaseRecord) {
	lease.CandidateOwnerNode = ""
	lease.CandidateOwnerSite = ""
	lease.CandidateOwnerRole = ""
	lease.CandidateEventID = ""
	lease.CandidateGroup = ""
	lease.CandidateType = ""
	lease.CandidateDedupeKey = ""
	lease.CandidateObservedAt = time.Time{}
	lease.CandidateExpiresAt = time.Time{}
}

func normalizeLeaseAddress(raw string, pool netip.Prefix) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	var addr netip.Addr
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		prefix = prefix.Masked()
		if !prefix.Addr().Is4() || prefix.Bits() != 32 {
			return "", false
		}
		addr = prefix.Addr()
	} else {
		parsed, err := netip.ParseAddr(raw)
		if err != nil || !parsed.Is4() {
			return "", false
		}
		addr = parsed
	}
	if !pool.Contains(addr) {
		return "", false
	}
	return netip.PrefixFrom(addr, 32).String(), true
}

func eventExpiresAt(ev routerstate.EventRecord, ttl time.Duration, now time.Time) time.Time {
	if !ev.ExpiresAt.IsZero() {
		return ev.ExpiresAt.UTC()
	}
	observedAt := ev.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = now.UTC()
	}
	return observedAt.Add(ttl)
}

func durationDefault(raw string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
