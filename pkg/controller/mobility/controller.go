// SPDX-License-Identifier: BSD-3-Clause
// Package mobility derives BGP /32 mobility paths and provider trap action
// plans from MobilityPool intent and federation observed facts.
package mobility

import (
	"context"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	ObservedEventType  = "routerd.client.ipv4.observed"
	ExpiredEventType   = "routerd.client.ipv4.expired"
	staticOwnedType    = "routerd.mobility.static-owned"
	staticHandoverType = "routerd.mobility.static-handover"

	DefaultLeaseTTL = 5 * time.Minute
)

const (
	bgpMobilityLocalPrefBase uint32 = 200

	bgpMobilityCommunityOwner          = "64512:100"
	bgpMobilityCommunityRoleOnPrem     = "64512:101"
	bgpMobilityCommunityRoleCloud      = "64512:102"
	bgpMobilityCommunitySourceObserved = "64512:110"
	bgpMobilityCommunitySourceStatic   = "64512:111"
	bgpMobilityCommunitySourceHandover = "64512:112"
	bgpMobilityCommunitySourceReturn   = bgpstate.MobilityCommunityReturnRoute
	// bgpMobilityCommunityActiveHolder is advertised only by the active capture
	// holder (placement.Active) on its owner /32. It is the holder-beacon: peers
	// treat a node as the group's holder only when its owner /32 carries this
	// community, so a standby's lower-preference make-before-break advertisement and
	// a cold-start advertisement (neither active) are not mistaken for holdership.
	bgpMobilityCommunityActiveHolder = bgpstate.MobilityCommunityActiveHolder

	bgpPathSigParam             = "mobilityPathSig"
	bgpTrapLastSeenAtParam      = "mobilityTrapLastSeenAt"
	bgpTrapRIBMissingHold       = 2 * time.Minute
	bgpSeizeLivenessMissingHold = 30 * time.Second
	bgpProviderMissingRetryHold = 30 * time.Second
)

type Store interface {
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	RecordFederationEvent(routerstate.EventRecord) error
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
	GetDynamicConfigPartsBySource(source string) ([]routerstate.DynamicConfigPartRecord, error)
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
	ListActions(routerstate.ActionExecutionFilter) ([]routerstate.ActionExecutionRecord, error)
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type objectStatusMerger interface {
	MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error
}

type mobilityEventRecorder interface {
	RecordBusEvent(context.Context, daemonapi.DaemonEvent) (string, error)
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
	// StartedAt is supplied by the Runner so the shared placement core receives
	// one explicit process startup epoch rather than consulting package state.
	StartedAt time.Time
	// SuppressProviderDeprovision keeps graceful-stop handoff make-before-break:
	// withdraw liveness and lower local BGP preference first, but do not ask the
	// local provider to unassign until the caller has observed peer takeover.
	SuppressProviderDeprovision bool
	// ForceSelfDrain is an ephemeral handoff fact, not a MobilityPool config
	// mutation. It drains every resolved local placement member when no scoped
	// pool set is supplied.
	ForceSelfDrain bool
	// ForceSelfDrainPools narrows an ephemeral drain to named MobilityPools. A
	// graceful restart uses this to avoid changing unrelated or singleton pools
	// while handing off an owned /32 from another pool.
	ForceSelfDrainPools map[string]bool
	// ReconcilePools narrows an ephemeral reconcile to named MobilityPools.
	// Sources for other pools are retained unchanged rather than treated as
	// stale. It is used together with ForceSelfDrainPools during graceful stop.
	ReconcilePools map[string]bool
}

func (c Controller) forceSelfDrainPool(name string) bool {
	if len(c.ForceSelfDrainPools) > 0 {
		return c.ForceSelfDrainPools[name]
	}
	return c.ForceSelfDrain
}

func (c Controller) reconcilesPool(name string) bool {
	return len(c.ReconcilePools) == 0 || c.ReconcilePools[name]
}

func (c Controller) HandleEvent(ctx context.Context, _ daemonapi.DaemonEvent) error {
	return c.Reconcile(ctx)
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := controllerNow(c.Now)
	desiredSources := map[string]bool{}
	retainPoolSources := map[string]bool{}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		if !c.reconcilesPool(res.Metadata.Name) {
			// A scoped graceful handoff must not infer that unrelated plan sources
			// are stale merely because it intentionally skipped their reconcile.
			retainPoolSources[res.Metadata.Name] = true
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"phase":  "Degraded",
				"reason": err.Error(),
			})
			continue
		}
		result, err := c.reconcileBGPDelivery(ctx, res, spec, now)
		if result.Pending {
			// membersFrom fail-static deliberately preserves the previously
			// generated source for this Pool until topology resolution returns.
			retainPoolSources[res.Metadata.Name] = true
		}
		if result.Source != "" {
			desiredSources[result.Source] = true
		}
		if err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"phase":  "Degraded",
				"reason": err.Error(),
			})
		}
	}
	return c.deprovisionStaleMobilitySources(ctx, desiredSources, retainPoolSources, now)
}
