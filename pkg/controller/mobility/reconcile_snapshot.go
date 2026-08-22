// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// poolReconcileState is the controller-shell input around one typed runtime
// snapshot. It contains observations and durable prior state only; the pure
// core receives state.Runtime and produces PoolPlan.
type poolReconcileState struct {
	Runtime  PoolRuntimeSnapshot
	Resolved mobilityMembersResolution
	Pending  bool
}

// poolPlanningFacts is the shared controller-shell observation boundary.
// Mobility and Discovery keep independent reconcile cadence, but decode the
// same event/status/BGP facts into this one typed representation.
type poolPlanningFacts struct {
	Events            []routerstate.EventRecord
	Previous          PreviousPoolState
	Ownership         OwnershipFacts
	BGP               BGPSnapshot
	OnPremIntentReady bool
}

type poolPlanningFactsStore interface {
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

func collectPoolPlanningFacts(router *api.Router, store poolPlanningFactsStore, pool NormalizedMobilityPool, now time.Time) (poolPlanningFacts, error) {
	if !pool.Prefix.IsValid() {
		return poolPlanningFacts{}, fmt.Errorf("MobilityPool/%s normalized prefix is required", pool.Name)
	}
	events, err := store.ListFederationEvents(pool.Spec.GroupRef, false, now.Unix())
	if err != nil {
		return poolPlanningFacts{}, fmt.Errorf("list federation events: %w", err)
	}
	onPremIntentReady := true
	if strings.TrimSpace(pool.Self.OwnershipDiscovery.Mode) == "onprem-l2" {
		onPremIntentReady, err = onPremDiscoveryIntentReady(store, pool, now)
		if err != nil {
			return poolPlanningFacts{}, fmt.Errorf("read onprem discovery intent: %w", err)
		}
	}
	previous, ownership := poolPreviousStateAndOwnershipFacts(store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", pool.Name), pool, events, pool.Prefix, onPremIntentReady, now)
	return poolPlanningFacts{
		Events:            events,
		Previous:          previous,
		Ownership:         ownership,
		BGP:               collectBGPSnapshot(router, store, pool),
		OnPremIntentReady: onPremIntentReady,
	}, nil
}

// poolRuntimeSnapshotFromFacts is the shared typed observation boundary for
// the Mobility and Discovery controllers. Each controller can add only the
// facts its own effects require, while placement always sees this same
// normalized PoolRuntimeSnapshot shape.
func poolRuntimeSnapshotFromFacts(pool NormalizedMobilityPool, facts poolPlanningFacts, startedAt, now time.Time) PoolRuntimeSnapshot {
	return PoolRuntimeSnapshot{
		Pool:                  pool,
		Events:                facts.Events,
		Ownership:             facts.Ownership,
		BGP:                   facts.BGP,
		PlacementObservations: placementObservationsFromFacts(startedAt, facts.BGP, facts.Ownership.SelfInventoryKnown, len(facts.Ownership.SelfCapturedIPs) > 0, facts.Previous.Placement),
		Previous:              facts.Previous,
		Now:                   now,
	}
}

func (s poolReconcileState) pendingStatus() map[string]any {
	return map[string]any{
		"phase":               "Pending",
		"reason":              "membersFrom source is not resolved",
		"pendingSources":      s.Resolved.PendingSources,
		"membersFrom":         statusRowMaps(s.Resolved.MembersFrom),
		"resolvedMemberCount": s.Resolved.ResolvedMemberCount,
	}
}

// collectPoolReconcileState is the imperative snapshot boundary. In
// particular, MobilityPool status is read and decoded exactly once here.
func (c Controller) collectPoolReconcileState(res api.Resource, spec api.MobilityPoolSpec, now time.Time) (poolReconcileState, error) {
	state := poolReconcileState{}
	normalized, err := resolveNormalizedMobilityPool(c.Router, spec)
	if err != nil {
		return state, err
	}
	state.Resolved = normalized.Resolved
	if len(normalized.Resolved.PendingSources) > 0 {
		state.Pending = true
		return state, nil
	}
	pool := normalized.Pool
	if c.forceSelfDrainPool(res.Metadata.Name) {
		pool = poolWithForcedSelfDrain(pool)
	}
	pool.Name = res.Metadata.Name
	pool.Source = DynamicSource(res.Metadata.Name, pool.SelfNode)
	facts, err := collectPoolPlanningFacts(c.Router, c.Store, pool, now)
	if err != nil {
		return state, err
	}
	pool.Self.CaptureSourceAddress = resolveCaptureSourceAddress(c.Store, pool.Self.CaptureSourceAddress, pool.Self.CaptureSourceAddressFrom, pool.Prefix)
	pool, captureResolved, captureReason := poolWithDiscoveredSelfCapture(pool, facts.Ownership)
	// Resolve the route device at the snapshot boundary. The PoolPlan persists
	// this actual ifname as a typed local route intent instead of making the
	// downstream chain controller recover it from a FIB verdict.
	selfCaptureInterface := strings.TrimSpace(api.ResolveInterfaceIfName(c.Router, pool.Self.Capture.Interface))
	captureGate := sam.EvaluateCaptureGate(pool.Self.Capture, captureGateObservation(c.Store, pool.Self.Capture))
	pool.SelfCaptureInterface = selfCaptureInterface
	releaseEvents, err := c.recordBGPStaticHandoverReleaseEvents(pool, facts.Events, now)
	if err != nil {
		return state, err
	}
	events := append(facts.Events, releaseEvents...)
	actionHistory, err := providerActionHistoryForPool(c.Store, res.Metadata.Name, pool.SelfNode, now)
	if err != nil {
		return state, err
	}
	livenessMarkerPrefix, _ := c.selfLivenessMarkerPrefix(pool.Spec.GroupRef)
	runtime := poolRuntimeSnapshotFromFacts(pool, facts, c.StartedAt, now)
	runtime.Events = events
	runtime.Provider = ProviderSnapshot{
		Profiles:            cloudProviderProfiles(c.Router),
		ActionHistory:       actionHistory,
		SuppressDeprovision: c.SuppressProviderDeprovision,
	}
	if !captureResolved {
		runtime.Provider.CaptureResolutionError = captureReason
	}
	runtime.LivenessMarkerPrefix = livenessMarkerPrefix
	runtime.TunnelInterfaces = mobilityTunnelInterfaces(c.Router)
	runtime.CaptureGate = &captureGate
	state = poolReconcileState{Resolved: normalized.Resolved, Runtime: runtime}
	return state, nil
}

// captureGateObservation is the persisted-status boundary for capture gates.
// The functional core receives only the typed VRRP fact, never a status map or
// a store handle.
func captureGateObservation(store Store, capture api.MobilityMemberCapture) sam.CaptureGateObservation {
	typ := strings.TrimSpace(capture.ActiveWhen.Type)
	ref := strings.TrimPrefix(strings.TrimSpace(capture.ActiveWhen.VirtualAddressRef), "VirtualAddress/")
	if store == nil || typ != "vrrp-master" || ref == "" {
		return sam.CaptureGateObservation{}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", ref)
	if status == nil {
		return sam.CaptureGateObservation{}
	}
	return sam.CaptureGateObservation{
		VirtualAddressStatusAvailable: true,
		VirtualAddressRole:            statusvalue.Text(status["role"]),
	}
}

func poolWithForcedSelfDrain(pool NormalizedMobilityPool) NormalizedMobilityPool {
	if strings.TrimSpace(pool.Self.PlacementGroup) == "" {
		return pool
	}
	self := pool.Self
	self.MaintenanceDrain = true
	return poolWithSelf(pool, self)
}

func poolWithSelf(pool NormalizedMobilityPool, self memberPlanInfo) NormalizedMobilityPool {
	members := make(map[string]memberPlanInfo, len(pool.Members))
	for nodeRef, member := range pool.Members {
		members[nodeRef] = member
	}
	members[self.NodeRef] = self
	pool.Self, pool.SelfNode, pool.Members = self, self.NodeRef, members
	return pool
}
