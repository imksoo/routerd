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
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
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

type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	Now    func() time.Time
}

func (c Controller) HandleEvent(ctx context.Context, _ daemonapi.DaemonEvent) error {
	return c.Reconcile(ctx)
}

func (c Controller) Reconcile(_ context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := c.now()
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		if err := c.emitHeartbeat(res, now); err != nil {
			_ = c.Store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", res.Metadata.Name, map[string]any{
				"phase":  "Degraded",
				"reason": err.Error(),
			})
			continue
		}
		if err := c.reconcilePool(res, now); err != nil {
			_ = c.Store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", res.Metadata.Name, map[string]any{
				"phase":  "Degraded",
				"reason": err.Error(),
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
	return c.Store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", res.Metadata.Name, status)
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
