// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	// OnPremARPRequestObservedEvent is a fact that a local CE emitted an ARP
	// request for an unresolved address. It is not a remote execution command:
	// each receiving leaf independently validates its local MobilityPool policy
	// before asking its own on-demand observer to probe the target.
	OnPremARPRequestObservedEvent = "routerd.mobility.arp.request.observed"
	onPremARPRequestFactSource    = "onprem-l2-arp-request"
	onPremARPRequestTTL           = 45 * time.Second
)

// ARPProbeFunc asks an already-supervised local on-demand observer to probe one
// address. The chain implementation owns socket selection and readiness.
type ARPProbeFunc func(context.Context, string, string) error

type arpProbeRequestClaim struct {
	ObservedAt time.Time
	ExpiresAt  time.Time
}

// ARPProbeRequestTracker prevents a periodic reconcile from executing the same
// federation event more than once. A refreshed stable event ID has a newer
// ObservedAt and is therefore eligible again after the observer cooldown.
type ARPProbeRequestTracker struct {
	mu     sync.Mutex
	claims map[string]arpProbeRequestClaim
}

func NewARPProbeRequestTracker() *ARPProbeRequestTracker {
	return &ARPProbeRequestTracker{claims: map[string]arpProbeRequestClaim{}}
}

func (t *ARPProbeRequestTracker) claim(key string, observedAt, expiresAt, now time.Time) bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.claims == nil {
		t.claims = map[string]arpProbeRequestClaim{}
	}
	for existingKey, existing := range t.claims {
		if !existing.ExpiresAt.IsZero() && !now.Before(existing.ExpiresAt) {
			delete(t.claims, existingKey)
		}
	}
	if existing, ok := t.claims[key]; ok && !observedAt.After(existing.ObservedAt) {
		return false
	}
	t.claims[key] = arpProbeRequestClaim{ObservedAt: observedAt, ExpiresAt: expiresAt}
	return true
}

func (t *ARPProbeRequestTracker) release(key string, observedAt time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if existing, ok := t.claims[key]; ok && existing.ObservedAt.Equal(observedAt) {
		delete(t.claims, key)
	}
	t.mu.Unlock()
}

type onPremARPRequest struct {
	Target       string
	Pool         string
	Interface    string
	Network      string
	Bridge       string
	RequesterIP  string
	RequesterMAC string
}

func onPremARPRequestFromDaemonEvent(event daemonapi.DaemonEvent) (onPremARPRequest, bool) {
	if strings.TrimSpace(event.Type) != OnPremARPRequestObservedEvent {
		return onPremARPRequest{}, false
	}
	attrs := event.Attributes
	request := onPremARPRequest{
		Target:       firstNonEmpty(attrs["target"], attrs["address"]),
		Pool:         strings.TrimSpace(attrs["pool"]),
		Interface:    firstNonEmpty(attrs["interface"], attrs["ifname"]),
		Network:      firstNonEmpty(attrs["network"], attrs["svnet"]),
		Bridge:       strings.TrimSpace(attrs["bridge"]),
		RequesterIP:  strings.TrimSpace(attrs["requesterIP"]),
		RequesterMAC: strings.TrimSpace(attrs["requesterMAC"]),
	}
	return request, request.Target != "" && request.Pool != ""
}

func (c DiscoveryController) handleOnPremARPRequestEvent(event daemonapi.DaemonEvent) error {
	request, ok := onPremARPRequestFromDaemonEvent(event)
	if !ok || c.Router == nil || c.Store == nil {
		return nil
	}
	now := controllerNow(c.Now)
	observedAt := event.Time.UTC()
	if observedAt.IsZero() {
		observedAt = now
	}
	if observedAt.After(now.Add(time.Second)) || !now.Before(observedAt.Add(onPremARPRequestTTL)) {
		return nil
	}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" || strings.TrimSpace(res.Metadata.Name) != request.Pool {
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
		if !onPremPoolCanFanoutARP(pool) {
			continue
		}
		address, ok := normalizeLeaseAddress(request.Target, pool.Prefix)
		if !ok || !discoveryScopeAllowsAddress(pool.Self.OwnershipDiscovery.Scope, address) {
			continue
		}
		_, sourceMatches := matchingOnPremDiscoverySource(pool.Self, onPremObservation{
			Address:    address,
			Interface:  request.Interface,
			Network:    request.Network,
			Bridge:     request.Bridge,
			SourceType: OnPremSourceOnDemandARP,
		})
		if !sourceMatches {
			continue
		}
		eventRecord := onPremARPRequestEvent(pool, address, request, observedAt)
		if err := c.Store.RecordFederationEvent(eventRecord); err != nil {
			return fmt.Errorf("record onprem ARP request event %q: %w", eventRecord.ID, err)
		}
		return nil
	}
	return nil
}

func onPremARPRequestEvent(pool NormalizedMobilityPool, address string, request onPremARPRequest, now time.Time) routerstate.EventRecord {
	now = now.UTC()
	dedupeKey := strings.Join([]string{
		"mobility", onPremARPRequestFactSource, strings.TrimSpace(pool.Name),
		strings.TrimSpace(pool.SelfNode), strings.ReplaceAll(address, "/", "_"),
	}, ":")
	payload := map[string]string{
		"address":    address,
		"pool":       strings.TrimSpace(pool.Name),
		"source":     onPremARPRequestFactSource,
		"sourceType": OnPremSourceOnDemandARP,
	}
	if request.Interface != "" {
		payload["interface"] = request.Interface
	}
	if request.Network != "" {
		payload["network"] = request.Network
	}
	if request.Bridge != "" {
		payload["bridge"] = request.Bridge
	}
	if request.RequesterIP != "" {
		payload["requesterIP"] = request.RequesterIP
	}
	if request.RequesterMAC != "" {
		payload["requesterMAC"] = request.RequesterMAC
	}
	return discoveryEvent(dedupeKey, pool.Spec.GroupRef, pool.SelfNode, address, dedupeKey, OnPremARPRequestObservedEvent, payload, now, onPremARPRequestTTL)
}

func onPremPoolCanFanoutARP(pool NormalizedMobilityPool) bool {
	if strings.TrimSpace(pool.Self.Role) != "onprem" ||
		strings.TrimSpace(pool.Self.Capture.Type) != "proxy-arp" ||
		strings.TrimSpace(pool.Self.OwnershipDiscovery.Mode) != "onprem-l2" {
		return false
	}
	for _, source := range onPremDiscoverySources(pool.Self.OwnershipDiscovery) {
		if strings.TrimSpace(source.Type) == OnPremSourceOnDemandARP {
			return true
		}
	}
	return false
}

// ReconcileARPProbeRequests consumes received, non-expired ARP-request facts.
// It deliberately skips locally-originated facts because the local observer
// already issued the first probe while recording the request.
func (c DiscoveryController) ReconcileARPProbeRequests(ctx context.Context) error {
	if c.Router == nil || c.Store == nil || c.ProbeARP == nil {
		return nil
	}
	now := controllerNow(c.Now)
	var errs []error
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
		if !onPremPoolCanFanoutARP(pool) {
			continue
		}
		events, err := c.Store.ListFederationEvents(pool.Spec.GroupRef, false, now.Unix())
		if err != nil {
			errs = append(errs, fmt.Errorf("list onprem ARP request events for %s: %w", pool.Name, err))
			continue
		}
		for _, event := range events {
			address, ok := federatedARPRequestTarget(pool, event, now)
			if !ok {
				continue
			}
			claimKey := strings.TrimSpace(pool.SelfNode) + "\x00" + event.ID
			if !c.ARPProbeRequests.claim(claimKey, event.ObservedAt, event.ExpiresAt, now) {
				continue
			}
			if err := c.ProbeARP(ctx, pool.Name, address); err != nil {
				c.ARPProbeRequests.release(claimKey, event.ObservedAt)
				errs = append(errs, fmt.Errorf("probe %s for MobilityPool/%s after event %s: %w", address, pool.Name, event.ID, err))
			}
		}
	}
	return errors.Join(errs...)
}

func federatedARPRequestTarget(pool NormalizedMobilityPool, event routerstate.EventRecord, now time.Time) (string, bool) {
	if event.Group != pool.Spec.GroupRef || event.Type != OnPremARPRequestObservedEvent ||
		strings.TrimSpace(event.Payload["source"]) != onPremARPRequestFactSource ||
		strings.TrimSpace(event.Payload["sourceType"]) != OnPremSourceOnDemandARP ||
		strings.TrimSpace(event.Payload["pool"]) != strings.TrimSpace(pool.Name) ||
		strings.TrimSpace(event.SourceNode) == "" || strings.TrimSpace(event.SourceNode) == strings.TrimSpace(pool.SelfNode) ||
		event.ExpiresAt.IsZero() || !now.Before(event.ExpiresAt) || event.ObservedAt.IsZero() || event.ObservedAt.After(now.Add(time.Second)) {
		return "", false
	}
	if _, member := pool.Members[strings.TrimSpace(event.SourceNode)]; !member {
		return "", false
	}
	address, ok := normalizeLeaseAddress(event.Payload["address"], pool.Prefix)
	if !ok || !discoveryScopeAllowsAddress(pool.Self.OwnershipDiscovery.Scope, address) {
		return "", false
	}
	subject, ok := normalizeLeaseAddress(event.Subject, pool.Prefix)
	if !ok || subject != address {
		return "", false
	}
	return address, true
}
