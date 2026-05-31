// SPDX-License-Identifier: BSD-3-Clause

// Package mobility projects CloudEdge MobilityPool federation events into
// AddressLease runtime state. It does not render or apply dataplane claims.
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
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	ObservedEventType = "routerd.client.ipv4.observed"
	ExpiredEventType  = "routerd.client.ipv4.expired"

	DefaultLeaseTTL      = 5 * time.Minute
	DefaultHoldDuration  = 30 * time.Second
	statusPhaseProjected = "Projected"
)

type Store interface {
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	UpsertAddressLease(routerstate.AddressLeaseRecord) error
	ListAddressLeases(pool string, includeExpired bool, now time.Time) ([]routerstate.AddressLeaseRecord, error)
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
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
