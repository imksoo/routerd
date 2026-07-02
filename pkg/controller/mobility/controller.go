// SPDX-License-Identifier: BSD-3-Clause

// Package mobility derives BGP /32 mobility paths and provider trap action
// plans from MobilityPool intent and federation observed facts.
package mobility

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
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
	bgpMobilityCommunitySourceCapture  = "64512:113"
	bgpMobilityCommunitySourceReturn   = bgpstate.MobilityCommunityReturnRoute
	bgpMobilityCommunityFailover       = "64512:120"
	// bgpMobilityCommunityActiveHolder is advertised only by the active capture
	// holder (placement.Active) on its owner /32. It is the holder-beacon: peers
	// treat a node as the group's holder only when its owner /32 carries this
	// community, so a standby's lower-preference make-before-break advertisement and
	// a cold-start advertisement (neither active) are not mistaken for holdership.
	bgpMobilityCommunityActiveHolder = "64512:121"

	bgpPathSigParam             = "mobilityPathSig"
	bgpTrapLastSeenAtParam      = "mobilityTrapLastSeenAt"
	bgpTrapTransitionParam      = "mobilityProviderTransition"
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

type BGPPathClient interface {
	ListPaths(ctx context.Context, source string) ([]bgpdaemon.AppliedPath, error)
	UpsertPath(ctx context.Context, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedPath, error)
	DeletePath(ctx context.Context, path bgpdaemon.AppliedPath) error
}

type Controller struct {
	Router        *api.Router
	Bus           *bus.Bus
	Store         Store
	BGPPaths      BGPPathClient
	MemberSetSync *PeerGroupSyncClient
	Now           func() time.Time
	// SuppressProviderDeprovision keeps graceful-stop handoff make-before-break:
	// withdraw liveness and lower local BGP preference first, but do not ask the
	// local provider to unassign until the caller has observed peer takeover.
	SuppressProviderDeprovision bool
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
		spec, err := res.MobilityPoolSpec()
		if err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"phase":         "Degraded",
				"plannerPhase":  "Degraded",
				"plannerReason": err.Error(),
				"plannedAt":     now.Format(time.RFC3339Nano),
			})
			continue
		}
		if mobilityDeliveryMode(spec) != "bgp" {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"phase":         "Degraded",
				"plannerPhase":  "Degraded",
				"plannerReason": fmt.Sprintf("deliveryPolicy.mode=%s is no longer supported; use bgp", mobilityDeliveryMode(spec)),
				"plannedAt":     now.Format(time.RFC3339Nano),
				"deliveryMode":  mobilityDeliveryMode(spec),
			})
			continue
		}
		memberSetStatus := map[string]any{"phase": "Disabled"}
		if spec.PublishMemberSet {
			source := MobilityMemberSetDynamicSource(res.Metadata.Name)
			status, err := c.upsertMobilityMemberSetPart(res, spec, source, now)
			if err != nil {
				_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
					"phase":         "Degraded",
					"plannerPhase":  "Degraded",
					"plannerReason": err.Error(),
					"memberSet":     status,
					"plannedAt":     now.Format(time.RFC3339Nano),
				})
				continue
			}
			memberSetStatus = status
		}
		if err := c.reconcileBGPDelivery(ctx, res, spec, memberSetStatus, now); err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, map[string]any{
				"phase":         "Degraded",
				"plannerPhase":  "Degraded",
				"plannerReason": err.Error(),
				"memberSet":     memberSetStatus,
				"plannedAt":     now.Format(time.RFC3339Nano),
			})
		}
	}
	return nil
}

func (c Controller) reconcileBGPDelivery(ctx context.Context, res api.Resource, spec api.MobilityPoolSpec, memberSetStatus map[string]any, now time.Time) error {
	if c.BGPPaths == nil {
		return fmt.Errorf("MobilityPool/%s deliveryPolicy.mode=bgp requires routerd-bgp control client", res.Metadata.Name)
	}
	selfNode, err := c.selfNode(spec.GroupRef)
	if err != nil {
		return err
	}
	resolved, err := (mobilityMemberResolver{Router: c.Router, Store: c.Store, Sync: c.MemberSetSync, Now: c.Now}).resolve(ctx, spec)
	if err != nil {
		return err
	}
	spec = resolved.Spec
	if len(resolved.PendingSources) > 0 {
		return c.savePlannerStatus(res.Metadata.Name, map[string]any{
			"phase":               "Pending",
			"plannerPhase":        "Pending",
			"plannerReason":       "membersFrom source is not resolved",
			"selfNode":            selfNode,
			"deliveryMode":        "bgp",
			"pendingSources":      resolved.PendingSources,
			"membersFrom":         mobilityMembersFromStatusMaps(resolved.MembersFrom),
			"resolvedMemberCount": resolved.ResolvedMemberCount,
			"memberSet":           memberSetStatus,
			"plannedAt":           now.Format(time.RFC3339Nano),
		})
	}
	spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
	if err != nil {
		return fmt.Errorf("normalize MobilityPool/%s: %w", res.Metadata.Name, err)
	}
	members := plannerMembers(spec.Members)
	self, ok := lookupMemberByNodeRef(members, selfNode)
	if !ok {
		return fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, res.Metadata.Name)
	}
	selfNode = self.NodeRef
	spec, selfCaptureResolved, selfCaptureReason := c.specWithDiscoveredSelfCapture(res.Metadata.Name, selfNode, spec)
	members = plannerMembers(spec.Members)
	self, ok = lookupMemberByNodeRef(members, selfNode)
	if !ok {
		return fmt.Errorf("self node %q is not a member of MobilityPool/%s after self capture resolution", selfNode, res.Metadata.Name)
	}
	source := DynamicSource(res.Metadata.Name, selfNode)
	events, err := c.Store.ListFederationEvents(spec.GroupRef, false, now.Unix())
	if err != nil {
		return fmt.Errorf("list federation events: %w", err)
	}
	releaseEvents, err := c.recordBGPStaticHandoverReleaseEvents(res.Metadata.Name, selfNode, spec, events, now)
	if err != nil {
		return err
	}
	events = append(events, releaseEvents...)
	discoverySelfIPs, discoverySelfIPsObserved := c.discoverySelfPrivateIPSet(res.Metadata.Name, spec)
	discoverySelfCaptures, _ := c.discoverySelfCapturedAddressSet(res.Metadata.Name, spec)
	discoverySelfObservedAt := c.discoveryLastScanAt(res.Metadata.Name)
	livenessMarkers, livenessMarkersObserved := c.bgpLivenessMarkers()
	installedNextHops, bgpRIBObserved := c.bgpInstalledNextHops()
	captureNextHops, captureRIBObserved := c.bgpCaptureCandidateNextHops(spec)
	if captureRIBObserved {
		bgpRIBObserved = true
	}
	startupReadiness := placementStartupReadinessForMember(self, livenessMarkersObserved || bgpRIBObserved, discoverySelfIPsObserved)
	observedHolderNode := bgpObservedGroupHolder(self, members, livenessMarkers, bgpMobilityPrefixCommunitiesFromStatus(c.Router, c.Store, spec))
	ownerPlacement := c.applyBGPCaptureSeizeHoldDown(res.Metadata.Name, evaluateBGPCapturePlacement(self, members, livenessMarkers, livenessMarkersObserved, observedHolderNode), now)
	ownerPlacement = fencePlacementForStartupWithReadiness(ownerPlacement, observedHolderNode, now, startupReadiness)
	ownerPlacement = applyHolderRetention(ownerPlacement, len(discoverySelfCaptures) > 0, higherPriorityHolderActive(self, members, observedHolderNode), now)
	actionJournal, err := c.Store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return fmt.Errorf("list action journal: %w", err)
	}
	providerFailures := interpretProviderCaptureAssignFailures(actionJournal, discoverySelfCaptures, discoverySelfObservedAt)
	previousActionPlans, err := c.previousGeneratedActionPlans(res.Metadata.Name, selfNode)
	if err != nil {
		return err
	}
	bgpHomeOwnerNodes := c.bgpHomeOwnerNodes(spec)
	bgpReturnRoutes := c.bgpReturnRoutes(spec)
	forwardingObserved, forwardingEnabled, forwardingObservedAt := c.discoverySelfForwardingState(res.Metadata.Name)
	poolStatus := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", res.Metadata.Name)
	captureClaim := bgpCaptureClaimForPlacementWithStatus(res.Metadata.Name, self, members, ownerPlacement, poolStatus, now)
	previousCaptureAssignments := bgpCaptureAssignmentsFromStatus(poolStatus)
	previousCaptureAssignmentSeq := bgpCaptureAssignmentSeqFromStatus(poolStatus)
	observedStaleSince := observedSelfStaleCaptureSinceFromStatus(poolStatus)
	ownershipDecisions, ownershipErr := resolveAddressOwnership(ownershipResolverInput{
		PoolName:          res.Metadata.Name,
		SelfNode:          selfNode,
		Spec:              spec,
		Events:            events,
		Status:            poolStatus,
		ActionJournal:     actionJournal,
		PreviousPlans:     previousActionPlans,
		InstalledNextHops: installedNextHops,
		BGPHomeOwnerNodes: bgpHomeOwnerNodes,
		BGPReturnRoutes:   bgpReturnRoutes,
		Now:               now,
	})
	if ownershipErr != nil {
		return ownershipErr
	}
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName:             res.Metadata.Name,
		Source:               source,
		Self:                 self,
		Members:              members,
		Spec:                 spec,
		Decisions:            ownershipDecisions,
		Placement:            ownerPlacement,
		InstalledNextHops:    installedNextHops,
		CaptureNextHops:      captureNextHops,
		RIBObserved:          bgpRIBObserved,
		PreviousPlans:        previousActionPlans,
		Profiles:             cloudProviderProfiles(c.Router),
		ActionJournal:        actionJournal,
		ObservedSelfIPs:      discoverySelfIPs,
		ObservedSelfCaptures: discoverySelfCaptures,
		ObservedSelfIPsOK:    discoverySelfIPsObserved,
		ObservedSelfAt:       discoverySelfObservedAt,
		ForwardingObserved:   forwardingObserved,
		ForwardingEnabled:    forwardingEnabled,
		ForwardingObservedAt: forwardingObservedAt,
		CaptureClaim:         captureClaim,
		CaptureAssignments:   previousCaptureAssignments,
		CaptureAssignmentSeq: previousCaptureAssignmentSeq,
		ObservedStaleSince:   observedStaleSince,
		SuppressDeprovision:  c.SuppressProviderDeprovision,
		LivenessMarkers:      livenessMarkers,
		Now:                  now,
	})
	if err != nil {
		return err
	}
	desired := append([]bgpdaemon.AppliedPath(nil), delivery.Paths...)
	var returnRoutes []bgpdaemon.AppliedPath
	if !self.MaintenanceDrain {
		returnRoutes = c.bgpSelfReturnRoutePaths(res.Metadata.Name, source, self, spec)
		desired = append(desired, returnRoutes...)
		marker, ok := c.bgpLivenessMarkerPath(res.Metadata.Name, source, selfNode, spec.GroupRef)
		if ok {
			desired = append(desired, marker)
		}
	}
	actionPlans := delivery.ActionPlans
	if len(delivery.CaptureCandidates) > 0 && !selfCaptureResolved {
		actionPlans = nil
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
		"plannerPhase":                             "BGPPlanned",
		"plannerReason":                            "deliveryPolicy.mode=bgp",
		"selfNode":                                 selfNode,
		"dynamicSource":                            source,
		"deliveryMode":                             "bgp",
		"bgpPathSource":                            source,
		"generatedBGPPaths":                        len(desired),
		"generatedBGPReturnRoutes":                 len(returnRoutes),
		"observedBGPReturnRoutes":                  mapStringKeysSorted(bgpReturnRoutes),
		"bgpRIBObserved":                           bgpRIBObserved,
		"startupFenceReadiness":                    placementStartupReadinessStatus(startupReadiness),
		"bgpCaptureElection":                       bgpCaptureElectionStatus(delivery.Placement),
		"bgpCaptureClaim":                          bgpCaptureClaimStatus(captureClaim),
		"bgpCaptureClaimPhase":                     captureClaim.Phase,
		"bgpCaptureClaimGeneration":                captureClaim.Generation,
		"bgpCaptureClaimEpochSeq":                  captureClaim.EpochSeq,
		"bgpCaptureClaimDesiredHolder":             captureClaim.DesiredHolder,
		"bgpCaptureClaimPreviousHolder":            captureClaim.PreviousHolder,
		"bgpCaptureClaimReason":                    captureClaim.Reason,
		"bgpCaptureAssignments":                    bgpCaptureAssignmentStatusList(delivery.CaptureAssignments),
		"bgpCaptureAssignmentSeq":                  delivery.CaptureAssignmentSeq,
		"generatedBGPTraps":                        len(delivery.CaptureCandidates),
		"generatedClaims":                          0,
		"generatedActions":                         len(actionPlans),
		"membersFrom":                              mobilityMembersFromStatusMaps(resolved.MembersFrom),
		"resolvedMemberCount":                      len(spec.Members),
		"pendingSources":                           resolved.PendingSources,
		"memberSet":                                memberSetStatus,
		"selfCaptureResolved":                      selfCaptureResolved,
		"plannedAt":                                now.Format(time.RFC3339Nano),
		"operatorIntent":                           "MobilityPool",
		"derivedConfigKinds":                       []string{"BGPPath"},
		"providerActionPhase":                      "OK",
		"providerActionError":                      "",
		"providerActionFailedAddresses":            nil,
		"providerActionFailedTargets":              nil,
		"providerActionFailedDetails":              nil,
		"providerActionFailedCount":                0,
		"providerActionFailedAt":                   "",
		"providerActionSupersededFailureAddresses": nil,
		"providerActionSupersededFailureCount":     0,
		"providerActionSupersededFailureAt":        "",
		"providerActionSupersededFailureReason":    "",
		"providerObservationPhase":                 "OK",
		"providerObservationPendingAddresses":      nil,
		"providerObservationConfirmedAddresses":    nil,
		"providerObservationPendingTargets":        nil,
		"providerObservationConfirmedTargets":      nil,
		"providerObservationDetails":               nil,
		"providerObservationPendingCount":          0,
		"providerObservationConfirmedCount":        0,
		"observedSelfStaleCaptures":                observedSelfStaleCaptureStatus(ownershipDecisions, selfNode, observedStaleSince, now),
	}
	pendingActionCount := pendingProviderActionPlanCount(actionPlans, actionJournal)
	failedActions := failedProviderActionPlans(actionPlans, actionJournal, discoverySelfCaptures, discoverySelfObservedAt)
	failedActions = appendProviderCaptureAssignFailures(failedActions, providerFailures.Active)
	failedActions = filterSupersededSameProviderHomeFailures(failedActions, ownershipDecisions, self.Capture.ProviderRef)
	status["providerActionPendingCount"] = pendingActionCount
	observationStatus := providerCaptureObservationStatus(actionPlans, previousActionPlans, actionJournal, discoverySelfCaptures, discoverySelfIPsObserved, discoverySelfObservedAt, forwardingObserved, forwardingEnabled, forwardingObservedAt, ownershipDecisions)
	status["providerObservationPhase"] = observationStatus.Phase
	status["providerObservationPendingAddresses"] = observationStatus.PendingAddresses
	status["providerObservationConfirmedAddresses"] = observationStatus.ConfirmedAddresses
	status["providerObservationPendingTargets"] = observationStatus.PendingTargets
	status["providerObservationConfirmedTargets"] = observationStatus.ConfirmedTargets
	status["providerObservationDetails"] = observationStatus.Details
	status["providerObservationPendingCount"] = len(observationStatus.PendingAddresses) + len(observationStatus.PendingTargets)
	status["providerObservationConfirmedCount"] = len(observationStatus.ConfirmedAddresses) + len(observationStatus.ConfirmedTargets)
	for key, value := range bgpSeizeHoldDownStatus(delivery.Placement) {
		status[key] = value
	}
	if status["bgpCapturePending"] == true && status["plannerPhase"] == "BGPPlanned" {
		status["plannerPhase"] = "Pending"
		status["plannerReason"] = "BGP capture seize hold-down is active"
	}
	if delivery.Distribution != nil {
		status["captureDistributionMode"] = "distributed"
		status["captureDistributionNodeCounts"] = delivery.Distribution.NodeCounts
		selfCount := 0
		if c, ok := delivery.Distribution.NodeCounts[selfNode]; ok {
			selfCount = c
		}
		status["captureDistributionSelfCount"] = selfCount
		status["captureDistributionTotalAssigned"] = len(delivery.Distribution.Assignments)
	}
	if selfCaptureReason != "" {
		status["selfCaptureReason"] = selfCaptureReason
	}
	if len(delivery.CaptureCandidates) > 0 && !selfCaptureResolved {
		status["plannerPhase"] = "Degraded"
		status["plannerReason"] = selfCaptureReason
		status["providerActionPhase"] = "Blocked"
	}
	if len(failedActions) > 0 {
		status["providerActionPhase"] = "Failed"
		var failedAddrs []string
		var failedTargets []string
		var failedDetails []map[string]string
		var lastError string
		var lastFailedAt time.Time
		for _, failed := range failedActions {
			if failed.Address != "" {
				failedAddrs = append(failedAddrs, failed.Address)
			}
			target := failed.Target
			if target == "" {
				target = failed.IdempotencyKey
			}
			if target != "" {
				failedTargets = append(failedTargets, target)
			}
			detail := map[string]string{
				"action":         failed.Action,
				"address":        failed.Address,
				"target":         target,
				"provider":       failed.Provider,
				"providerRef":    failed.ProviderRef,
				"idempotencyKey": failed.IdempotencyKey,
				"error":          failed.Error,
			}
			if !failed.FailedAt.IsZero() {
				detail["failedAt"] = failed.FailedAt.Format(time.RFC3339)
			}
			failedDetails = append(failedDetails, detail)
			if failed.FailedAt.After(lastFailedAt) {
				lastFailedAt = failed.FailedAt
				lastError = failed.Error
			}
		}
		sort.Strings(failedAddrs)
		sort.Strings(failedTargets)
		sort.Slice(failedDetails, func(i, j int) bool {
			return failedDetails[i]["idempotencyKey"] < failedDetails[j]["idempotencyKey"]
		})
		status["providerActionError"] = lastError
		status["providerActionFailedAddresses"] = failedAddrs
		status["providerActionFailedTargets"] = failedTargets
		status["providerActionFailedDetails"] = failedDetails
		status["providerActionFailedCount"] = len(failedActions)
		if !lastFailedAt.IsZero() {
			status["providerActionFailedAt"] = lastFailedAt.Format(time.RFC3339)
		}
	}
	if len(providerFailures.Superseded) > 0 {
		var addrs []string
		var lastSupersededAt time.Time
		for addr, rec := range providerFailures.Superseded {
			addrs = append(addrs, addr)
			if rec.UpdatedAt.After(lastSupersededAt) {
				lastSupersededAt = rec.UpdatedAt
			}
		}
		sort.Strings(addrs)
		status["providerActionSupersededFailureAddresses"] = addrs
		status["providerActionSupersededFailureCount"] = len(providerFailures.Superseded)
		status["providerActionSupersededFailureReason"] = "observed-self-capture"
		if !lastSupersededAt.IsZero() {
			status["providerActionSupersededFailureAt"] = lastSupersededAt.Format(time.RFC3339)
		}
	}
	if pendingActionCount > 0 && status["providerActionPhase"] == "OK" {
		status["plannerPhase"] = "Pending"
		status["plannerReason"] = "provider actions are pending"
		status["providerActionPhase"] = "Pending"
	}
	if observationStatus.PendingCount() > 0 && status["providerActionPhase"] == "OK" {
		status["plannerPhase"] = "Pending"
		status["plannerReason"] = "provider observations are pending"
	}
	for key, value := range ownershipResolverStatus(ownershipDecisions) {
		status[key] = value
	}
	if status["ownershipResolverPhase"] == "Conflict" {
		reason := strings.TrimSpace(fmt.Sprint(status["ownershipResolverReason"]))
		if reason == "" {
			reason = "ownership resolver conflict"
		}
		status["plannerPhase"] = "Degraded"
		status["plannerReason"] = reason
		if status["providerActionPhase"] == "OK" {
			status["providerActionPhase"] = "Blocked"
		}
	}
	if pending, reason := onPremL2OwnershipPending(self, delivery.Placement, ownershipDecisions, poolStatus, now); pending {
		status["plannerPhase"] = "Pending"
		status["plannerReason"] = reason
		status["ownershipResolverPhase"] = "Pending"
		status["ownershipResolverReason"] = reason
	}
	status["addresses"] = samAddressStatuses(ownershipDecisions, actionPlans, actionJournal, failedActions, observationStatus)
	status["phase"] = mobilityPoolResourcePhase(status)
	return c.savePlannerStatus(res.Metadata.Name, status)
}

func mobilityPoolResourcePhase(status map[string]any) string {
	switch strings.TrimSpace(fmt.Sprint(status["providerActionPhase"])) {
	case "Failed":
		return "Failed"
	case "Blocked":
		return "Degraded"
	case "Pending":
		return "Pending"
	}
	switch strings.TrimSpace(fmt.Sprint(status["providerObservationPhase"])) {
	case "Pending":
		return "Pending"
	}
	switch strings.TrimSpace(fmt.Sprint(status["ownershipResolverPhase"])) {
	case "Conflict":
		return "Degraded"
	case "Pending":
		return "Pending"
	}
	switch strings.TrimSpace(fmt.Sprint(status["plannerPhase"])) {
	case "BGPPlanned":
		return "Ready"
	case "Pending":
		return "Pending"
	case "Degraded":
		return "Degraded"
	case "Failed":
		return "Failed"
	}
	return "Degraded"
}

func samAddressStatuses(decisions []ownershipDecision, plans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, failedActions []providerActionPlanFailure, observationStatus providerObservationStatus) map[string]any {
	addresses := map[string]map[string]any{}
	ensure := func(address string) map[string]any {
		address = normalizeAddressString(address)
		if address == "" {
			return nil
		}
		item, ok := addresses[address]
		if !ok {
			item = map[string]any{
				"conditions":       map[string]string{},
				"conditionReasons": map[string]string{},
			}
			addresses[address] = item
		}
		return item
	}
	setCondition := func(item map[string]any, name, status, reason string) {
		if item == nil {
			return
		}
		conditions, _ := item["conditions"].(map[string]string)
		if conditions == nil {
			conditions = map[string]string{}
			item["conditions"] = conditions
		}
		reasons, _ := item["conditionReasons"].(map[string]string)
		if reasons == nil {
			reasons = map[string]string{}
			item["conditionReasons"] = reasons
		}
		conditions[name] = status
		if strings.TrimSpace(reason) != "" {
			reasons[name] = reason
		}
	}
	for _, decision := range decisions {
		item := ensure(decision.Address)
		if item == nil {
			continue
		}
		item["class"] = decision.Class
		item["source"] = decision.Source
		if decision.HomeOwnerNode != "" {
			item["ownerNode"] = decision.HomeOwnerNode
		}
		if decision.CaptureHolderNode != "" {
			item["captureHolderNode"] = decision.CaptureHolderNode
		}
		if decision.HomeProviderRef != "" {
			item["ownerProviderRef"] = decision.HomeProviderRef
		}
		state := ownershipResolverClaimState(decision)
		switch state {
		case "OK":
			setCondition(item, "OwnershipResolved", "True", firstNonEmpty(decision.SuppressionReason, decision.Class))
		case "Conflict":
			setCondition(item, "OwnershipResolved", "False", firstNonEmpty(decision.ConflictReason, "ownership conflict"))
		case "Stale":
			setCondition(item, "OwnershipResolved", "False", "stale ownership evidence")
		default:
			setCondition(item, "OwnershipResolved", "False", "ownership unknown")
		}
		setCondition(item, "FIBInstalled", "Unknown", "verified by routerctl doctor sam")
		setCondition(item, "ReachabilityProbed", "Unknown", "verified by external dataplane probes")
	}
	latest := latestActionRecordsByKey(journal)
	planSeen := map[string]bool{}
	for _, plan := range plans {
		address := normalizeAddressString(plan.Target["address"])
		if address == "" || !mobilityProviderAddressAction(plan.Action) {
			continue
		}
		item := ensure(address)
		if item == nil {
			continue
		}
		planSeen[address] = true
		key := strings.TrimSpace(plan.IdempotencyKey)
		item["providerAction"] = strings.TrimSpace(plan.Action)
		item["providerActionKey"] = key
		if generation := strings.TrimSpace(plan.Parameters[captureAssignmentGenerationParam]); generation != "" {
			item["assignmentGeneration"] = generation
		}
		rec, ok := latest[key]
		if !ok {
			setCondition(item, "ProviderActionApplied", "False", "provider action has not executed")
			continue
		}
		switch strings.TrimSpace(rec.Status) {
		case routerstate.ActionSucceeded, routerstate.ActionSkipped:
			setCondition(item, "ProviderActionApplied", "True", strings.TrimSpace(rec.Status))
		case routerstate.ActionFailed:
			setCondition(item, "ProviderActionApplied", "False", firstNonEmpty(rec.Error, "provider action failed"))
		default:
			setCondition(item, "ProviderActionApplied", "False", firstNonEmpty(strings.TrimSpace(rec.Status), "provider action pending"))
		}
	}
	for _, failed := range failedActions {
		item := ensure(failed.Address)
		if item == nil {
			continue
		}
		item["providerAction"] = failed.Action
		item["providerActionKey"] = failed.IdempotencyKey
		setCondition(item, "ProviderActionApplied", "False", firstNonEmpty(failed.Error, "provider action failed"))
	}
	for _, detail := range observationStatus.Details {
		address := normalizeAddressString(detail["address"])
		if address == "" {
			continue
		}
		item := ensure(address)
		if item == nil {
			continue
		}
		item["providerObservation"] = detail
		if detail["assignmentGeneration"] != "" {
			item["assignmentGeneration"] = detail["assignmentGeneration"]
		}
		if detail["phase"] == "Confirmed" {
			setCondition(item, "ProviderObserved", "True", firstNonEmpty(detail["source"], "provider observation confirmed"))
		} else {
			setCondition(item, "ProviderObserved", "False", firstNonEmpty(detail["reason"], "provider observation pending"))
		}
	}
	for address, item := range addresses {
		if !planSeen[address] {
			setCondition(item, "ProviderActionApplied", "True", "no provider action required")
		}
		if _, ok := item["providerObservation"]; !ok {
			setCondition(item, "ProviderObserved", "True", "no provider observation required")
		}
		blocking := samAddressBlockingCondition(item)
		item["blockingCondition"] = blocking
		item["phase"] = samAddressPhase(blocking)
	}
	out := make(map[string]any, len(addresses))
	for _, address := range sortedStringMapKeys(addresses) {
		out[address] = addresses[address]
	}
	return out
}

func mobilityProviderAddressAction(action string) bool {
	switch strings.TrimSpace(action) {
	case actionAssignSecondaryIP, actionUnassignSecondaryIP, actionAssignRouteTableRoute, actionUnassignRouteTableRoute:
		return true
	default:
		return false
	}
}

func samAddressBlockingCondition(item map[string]any) string {
	conditions, _ := item["conditions"].(map[string]string)
	for _, name := range []string{"OwnershipResolved", "ProviderActionApplied", "ProviderObserved", "FIBInstalled", "ReachabilityProbed"} {
		if conditions[name] == "False" {
			return name
		}
	}
	return ""
}

func samAddressPhase(blockingCondition string) string {
	if strings.TrimSpace(blockingCondition) == "" {
		return "Ready"
	}
	return "Pending"
}

func pendingProviderActionPlanCount(plans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord) int {
	if len(plans) == 0 {
		return 0
	}
	terminal := map[string]bool{}
	for _, rec := range journal {
		key := strings.TrimSpace(rec.IdempotencyKey)
		if key == "" {
			continue
		}
		switch strings.TrimSpace(rec.Status) {
		case routerstate.ActionSucceeded, routerstate.ActionSkipped:
			terminal[key] = true
		}
	}
	pending := 0
	for _, plan := range plans {
		key := strings.TrimSpace(plan.IdempotencyKey)
		if key == "" || terminal[key] {
			continue
		}
		pending++
	}
	return pending
}

type providerObservationStatus struct {
	Phase              string
	PendingAddresses   []string
	ConfirmedAddresses []string
	PendingTargets     []string
	ConfirmedTargets   []string
	Details            []map[string]string
}

func (s providerObservationStatus) PendingCount() int {
	return len(s.PendingAddresses) + len(s.PendingTargets)
}

func providerCaptureObservationStatus(plans, previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time, forwardingObserved, forwardingEnabled bool, forwardingObservedAt time.Time, decisions []ownershipDecision) providerObservationStatus {
	status := providerObservationStatus{Phase: "OK"}
	pending := map[string]map[string]string{}
	confirmed := map[string]map[string]string{}
	pendingTargets := map[string]map[string]string{}
	confirmedTargets := map[string]map[string]string{}
	for _, decision := range decisions {
		address := normalizeAddressString(decision.Address)
		if address == "" || decision.Class != ownershipClassConfirmedCapture {
			continue
		}
		confirmed[address] = map[string]string{
			"address": address,
			"phase":   "Confirmed",
			"source":  "ownership-resolver",
		}
		if decision.CaptureProviderRef != "" {
			confirmed[address]["providerRef"] = decision.CaptureProviderRef
		}
		if decision.CaptureTargetRef != "" {
			confirmed[address]["targetRef"] = decision.CaptureTargetRef
		}
		if decision.CaptureHolderNode != "" {
			confirmed[address]["holderNode"] = decision.CaptureHolderNode
		}
	}
	latest := latestActionRecordsByKey(journal)
	for _, plan := range plans {
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
		key := strings.TrimSpace(plan.IdempotencyKey)
		if key == "" {
			continue
		}
		rec, ok := latest[key]
		if !ok || strings.TrimSpace(rec.Status) != routerstate.ActionSucceeded {
			continue
		}
		target := plan.Target
		if len(target) == 0 {
			target = decodeActionRecordMap(rec.TargetJSON)
		}
		address := normalizeAddressString(target["address"])
		if address == "" {
			continue
		}
		detail := map[string]string{
			"address":        address,
			"action":         strings.TrimSpace(firstNonEmpty(plan.Action, rec.Action)),
			"provider":       strings.TrimSpace(firstNonEmpty(plan.Provider, rec.Provider)),
			"providerRef":    strings.TrimSpace(firstNonEmpty(plan.ProviderRef, rec.ProviderRef, target["providerRef"])),
			"idempotencyKey": key,
			"targetRef":      providerCaptureRefFromTarget(target),
		}
		if generation := strings.TrimSpace(firstNonEmpty(plan.Parameters[captureAssignmentGenerationParam], decodeActionRecordMap(rec.ParametersJSON)[captureAssignmentGenerationParam])); generation != "" {
			detail["assignmentGeneration"] = generation
		}
		requiredAfter := actionRecordCompletedAt(rec)
		if !requiredAfter.IsZero() {
			detail["requiredAfter"] = requiredAfter.UTC().Format(time.RFC3339Nano)
		}
		if !observedSelfAt.IsZero() {
			detail["snapshotCompletedAt"] = observedSelfAt.UTC().Format(time.RFC3339Nano)
		}
		if observedSelfCapturesOK && observedSelfCaptures[address] && providerObservationFresh(observedSelfAt, requiredAfter) {
			detail["phase"] = "Confirmed"
			detail["source"] = "provider-inventory"
			confirmed[address] = detail
			delete(pending, address)
			continue
		}
		detail["phase"] = "Pending"
		if observedSelfCapturesOK && observedSelfCaptures[address] {
			detail["reason"] = "provider inventory snapshot predates action completion"
		} else if observedSelfCapturesOK {
			detail["reason"] = "provider inventory has not observed capture on self"
		} else {
			detail["reason"] = "provider inventory has not completed self capture observation"
		}
		pending[address] = detail
		delete(confirmed, address)
	}
	for _, detail := range providerUnassignObservationDetails(previousPlans, journal, observedSelfCaptures, observedSelfCapturesOK, observedSelfAt) {
		address := normalizeAddressString(detail["address"])
		if address == "" {
			continue
		}
		if detail["phase"] == "Confirmed" {
			confirmed[address] = detail
			delete(pending, address)
			continue
		}
		pending[address] = detail
		delete(confirmed, address)
	}
	for _, detail := range providerForwardingObservationDetails(plans, journal, forwardingObserved, forwardingEnabled, forwardingObservedAt) {
		target := strings.TrimSpace(detail["targetRef"])
		if target == "" {
			continue
		}
		if detail["phase"] == "Confirmed" {
			confirmedTargets[target] = detail
			delete(pendingTargets, target)
			continue
		}
		pendingTargets[target] = detail
		delete(confirmedTargets, target)
	}
	if len(pending) > 0 || len(pendingTargets) > 0 {
		status.Phase = "Pending"
	}
	status.PendingAddresses = sortedStringMapKeys(pending)
	status.ConfirmedAddresses = sortedStringMapKeys(confirmed)
	status.PendingTargets = sortedStringMapKeys(pendingTargets)
	status.ConfirmedTargets = sortedStringMapKeys(confirmedTargets)
	for _, address := range status.PendingAddresses {
		status.Details = append(status.Details, pending[address])
	}
	for _, target := range status.PendingTargets {
		status.Details = append(status.Details, pendingTargets[target])
	}
	for _, address := range status.ConfirmedAddresses {
		status.Details = append(status.Details, confirmed[address])
	}
	for _, target := range status.ConfirmedTargets {
		status.Details = append(status.Details, confirmedTargets[target])
	}
	if len(status.PendingAddresses) == 0 {
		status.PendingAddresses = nil
	}
	if len(status.ConfirmedAddresses) == 0 {
		status.ConfirmedAddresses = nil
	}
	if len(status.PendingTargets) == 0 {
		status.PendingTargets = nil
	}
	if len(status.ConfirmedTargets) == 0 {
		status.ConfirmedTargets = nil
	}
	if len(status.Details) == 0 {
		status.Details = nil
	}
	return status
}

func providerUnassignObservationDetails(previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time) []map[string]string {
	previousKeys := map[string]bool{}
	for _, plan := range previousPlans {
		if !isProviderCaptureAssignAction(plan.Action) && !isProviderCaptureUnassignAction(plan.Action) {
			continue
		}
		key := providerCaptureTransitionKey(firstNonEmpty(plan.ProviderRef, plan.Target["providerRef"]), providerCaptureRefFromTarget(plan.Target), plan.Target["address"])
		if key != "" {
			previousKeys[key] = true
		}
	}
	if len(previousKeys) == 0 {
		return nil
	}
	var out []map[string]string
	for key, tr := range latestProviderCaptureTransitions(nil, journal) {
		if !previousKeys[key] || tr.assign || !tr.succeeded {
			continue
		}
		address := normalizeAddressString(tr.plan.Target["address"])
		if address == "" {
			continue
		}
		detail := map[string]string{
			"address":        address,
			"action":         strings.TrimSpace(tr.plan.Action),
			"provider":       strings.TrimSpace(tr.plan.Provider),
			"providerRef":    strings.TrimSpace(tr.plan.ProviderRef),
			"idempotencyKey": strings.TrimSpace(tr.plan.IdempotencyKey),
			"targetRef":      providerCaptureRefFromTarget(tr.plan.Target),
		}
		if !tr.at.IsZero() {
			detail["requiredAfter"] = tr.at.UTC().Format(time.RFC3339Nano)
		}
		if !observedSelfAt.IsZero() {
			detail["snapshotCompletedAt"] = observedSelfAt.UTC().Format(time.RFC3339Nano)
		}
		if observedSelfCapturesOK && !observedSelfCaptures[address] && providerObservationFresh(observedSelfAt, tr.at) {
			detail["phase"] = "Confirmed"
			detail["source"] = "provider-inventory"
		} else {
			detail["phase"] = "Pending"
			switch {
			case observedSelfCapturesOK && observedSelfCaptures[address]:
				detail["reason"] = "provider inventory still observes capture on self"
			case observedSelfCapturesOK:
				detail["reason"] = "provider inventory snapshot predates action completion"
			default:
				detail["reason"] = "provider inventory has not completed self capture observation"
			}
		}
		out = append(out, detail)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["idempotencyKey"] < out[j]["idempotencyKey"]
	})
	return out
}

func providerForwardingObservationDetails(plans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, observed, enabled bool, observedAt time.Time) []map[string]string {
	latest := latestActionRecordsByKey(journal)
	var out []map[string]string
	for _, plan := range plans {
		if strings.TrimSpace(plan.Action) != "ensure-forwarding-enabled" {
			continue
		}
		key := strings.TrimSpace(plan.IdempotencyKey)
		if key == "" {
			continue
		}
		rec, ok := latest[key]
		if !ok || strings.TrimSpace(rec.Status) != routerstate.ActionSucceeded {
			continue
		}
		target := plan.Target
		if len(target) == 0 {
			target = decodeActionRecordMap(rec.TargetJSON)
		}
		targetRef := strings.TrimSpace(firstNonEmpty(target["nicRef"], target["resourceRef"], target["routeTableRef"], target["providerRef"]))
		if targetRef == "" {
			continue
		}
		requiredAfter := actionRecordCompletedAt(rec)
		detail := map[string]string{
			"action":         strings.TrimSpace(firstNonEmpty(plan.Action, rec.Action)),
			"provider":       strings.TrimSpace(firstNonEmpty(plan.Provider, rec.Provider)),
			"providerRef":    strings.TrimSpace(firstNonEmpty(plan.ProviderRef, rec.ProviderRef, target["providerRef"])),
			"idempotencyKey": key,
			"targetRef":      targetRef,
		}
		if !requiredAfter.IsZero() {
			detail["requiredAfter"] = requiredAfter.UTC().Format(time.RFC3339Nano)
		}
		if !observedAt.IsZero() {
			detail["snapshotCompletedAt"] = observedAt.UTC().Format(time.RFC3339Nano)
		}
		if observed && enabled && providerObservationFresh(observedAt, requiredAfter) {
			detail["phase"] = "Confirmed"
			detail["source"] = "provider-inventory"
		} else {
			detail["phase"] = "Pending"
			switch {
			case observed && !enabled:
				detail["reason"] = "provider inventory has not observed forwarding enabled"
			case observed:
				detail["reason"] = "provider inventory snapshot predates action completion"
			default:
				detail["reason"] = "provider inventory has not completed forwarding observation"
			}
		}
		out = append(out, detail)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["idempotencyKey"] < out[j]["idempotencyKey"]
	})
	return out
}

func actionRecordCompletedAt(rec routerstate.ActionExecutionRecord) time.Time {
	if !rec.ExecutedAt.IsZero() {
		return rec.ExecutedAt.UTC()
	}
	if !rec.UpdatedAt.IsZero() {
		return rec.UpdatedAt.UTC()
	}
	return time.Time{}
}

func providerObservationFresh(observedAt, requiredAfter time.Time) bool {
	if requiredAfter.IsZero() {
		return true
	}
	if observedAt.IsZero() {
		return false
	}
	return !observedAt.UTC().Before(requiredAfter.UTC())
}

func latestActionRecordsByKey(journal []routerstate.ActionExecutionRecord) map[string]routerstate.ActionExecutionRecord {
	latest := map[string]routerstate.ActionExecutionRecord{}
	for _, rec := range journal {
		key := strings.TrimSpace(rec.IdempotencyKey)
		if key == "" {
			continue
		}
		prev, found := latest[key]
		if !found || rec.UpdatedAt.After(prev.UpdatedAt) || (rec.UpdatedAt.Equal(prev.UpdatedAt) && rec.ID > prev.ID) {
			latest[key] = rec
		}
	}
	return latest
}

func sortedStringMapKeys[V any](values map[string]V) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

type providerActionPlanFailure struct {
	IdempotencyKey string
	Action         string
	Address        string
	Target         string
	Provider       string
	ProviderRef    string
	Error          string
	FailedAt       time.Time
}

func failedProviderActionPlans(plans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, observedSelfCaptures map[string]bool, observedSelfAt time.Time) []providerActionPlanFailure {
	if len(plans) == 0 {
		return nil
	}
	latest := map[string]routerstate.ActionExecutionRecord{}
	for _, rec := range journal {
		key := strings.TrimSpace(rec.IdempotencyKey)
		if key == "" {
			continue
		}
		prev, found := latest[key]
		if !found || rec.UpdatedAt.After(prev.UpdatedAt) {
			latest[key] = rec
		}
	}
	var failed []providerActionPlanFailure
	for _, plan := range plans {
		key := strings.TrimSpace(plan.IdempotencyKey)
		if key == "" {
			continue
		}
		rec, found := latest[key]
		if !found || strings.TrimSpace(rec.Status) != routerstate.ActionFailed {
			continue
		}
		target := plan.Target
		if len(target) == 0 {
			target = decodeActionRecordMap(rec.TargetJSON)
		}
		address := normalizeAddressString(target["address"])
		if isProviderCaptureAssignAction(plan.Action) && address != "" && observedSelfCaptures[address] && providerObservationFresh(observedSelfAt, actionRecordCompletedAt(rec)) {
			continue
		}
		failedAt := rec.ExecutedAt
		if failedAt.IsZero() {
			failedAt = rec.UpdatedAt
		}
		failed = append(failed, providerActionPlanFailure{
			IdempotencyKey: key,
			Action:         strings.TrimSpace(firstNonEmpty(plan.Action, rec.Action)),
			Address:        address,
			Target:         providerActionFailureTarget(plan.Action, target),
			Provider:       strings.TrimSpace(firstNonEmpty(plan.Provider, rec.Provider)),
			ProviderRef:    strings.TrimSpace(firstNonEmpty(plan.ProviderRef, rec.ProviderRef)),
			Error:          strings.TrimSpace(firstNonEmpty(rec.Error, rec.ResultMessage)),
			FailedAt:       failedAt,
		})
	}
	sort.Slice(failed, func(i, j int) bool {
		return failed[i].IdempotencyKey < failed[j].IdempotencyKey
	})
	return failed
}

func appendProviderCaptureAssignFailures(failed []providerActionPlanFailure, active map[string]routerstate.ActionExecutionRecord) []providerActionPlanFailure {
	if len(active) == 0 {
		return failed
	}
	seen := map[string]bool{}
	for _, item := range failed {
		seen[item.IdempotencyKey] = true
	}
	for _, rec := range active {
		key := strings.TrimSpace(rec.IdempotencyKey)
		if key == "" || seen[key] {
			continue
		}
		target := decodeActionRecordMap(rec.TargetJSON)
		failedAt := rec.ExecutedAt
		if failedAt.IsZero() {
			failedAt = rec.UpdatedAt
		}
		failed = append(failed, providerActionPlanFailure{
			IdempotencyKey: key,
			Action:         strings.TrimSpace(rec.Action),
			Address:        normalizeAddressString(target["address"]),
			Target:         providerActionFailureTarget(rec.Action, target),
			Provider:       strings.TrimSpace(rec.Provider),
			ProviderRef:    strings.TrimSpace(rec.ProviderRef),
			Error:          strings.TrimSpace(firstNonEmpty(rec.Error, rec.ResultMessage)),
			FailedAt:       failedAt,
		})
	}
	sort.Slice(failed, func(i, j int) bool {
		return failed[i].IdempotencyKey < failed[j].IdempotencyKey
	})
	return failed
}

func filterSupersededSameProviderHomeFailures(failed []providerActionPlanFailure, decisions []ownershipDecision, selfProviderRef string) []providerActionPlanFailure {
	if len(failed) == 0 || strings.TrimSpace(selfProviderRef) == "" {
		return failed
	}
	byAddress := decisionsByAddress(decisions)
	out := failed[:0]
	for _, item := range failed {
		decision, ok := byAddress[normalizeAddressString(item.Address)]
		if ok && decisionSupersedesSameProviderHomeFailure(decision, selfProviderRef) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func decisionSupersedesSameProviderHomeFailure(decision ownershipDecision, selfProviderRef string) bool {
	if !decisionHomeProviderRefMatches(decision, selfProviderRef) ||
		decisionHasProviderCaptureState(decision) ||
		decision.CaptureSucceeded {
		return false
	}
	switch decision.Class {
	case ownershipClassRemoteHomeOwned, ownershipClassLocalHomeOwned:
		return true
	case ownershipClassStaleCapture:
		return strings.TrimSpace(decision.Source) == providerDiscoverySource &&
			strings.TrimSpace(decision.SuppressionReason) == "fresh-home-owner"
	default:
		return false
	}
}

func decisionHomeProviderRefMatches(decision ownershipDecision, providerRef string) bool {
	providerRef = strings.TrimSpace(providerRef)
	if providerRef == "" {
		return false
	}
	for _, candidate := range []string{decision.HomeProviderRef, decision.LocalProviderRef} {
		if strings.TrimSpace(candidate) == providerRef {
			return true
		}
	}
	return false
}

func providerActionFailureTarget(action string, target map[string]string) string {
	switch strings.TrimSpace(action) {
	case "ensure-forwarding-enabled", "ensure-forwarding-disabled":
		if value := strings.TrimSpace(target["nicRef"]); value != "" {
			return value
		}
	}
	for _, key := range []string{"address", "nicRef", "routeTableRef", "providerRef"} {
		value := strings.TrimSpace(target[key])
		if value != "" {
			return value
		}
	}
	if len(target) == 0 {
		return ""
	}
	keys := make([]string, 0, len(target))
	for key := range target {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := strings.TrimSpace(target[key])
		if value != "" {
			return key + "=" + value
		}
	}
	return ""
}

const onPremL2DiscoveryWarmup = 30 * time.Second

func onPremL2OwnershipPending(self memberPlanInfo, placement PlacementDecision, decisions []ownershipDecision, status map[string]any, now time.Time) (bool, string) {
	if strings.TrimSpace(self.Role) != "onprem" || strings.TrimSpace(self.OwnershipDiscovery.Mode) != "onprem-l2" {
		return false, ""
	}
	if !placement.Active {
		return false, ""
	}
	if len(onPremDiscoverySources(self.OwnershipDiscovery)) == 0 {
		return false, ""
	}
	phase := strings.TrimSpace(fmt.Sprint(status["discoveryPhase"]))
	if phase != "Observed" && phase != "Complete" {
		return true, "onprem-l2 ownership discovery has not completed an initial observation"
	}
	if phase == "Complete" && discoveryResultCount(status) == 0 {
		if freshUntil, ok := statusTimeValue(status["discoveryFreshUntil"]); ok && now.Before(freshUntil) {
			return false, ""
		}
		return true, "onprem-l2 empty ownership discovery is not fresh"
	}
	if armedAt, ok := statusTimeValue(status["discoveryArmedAt"]); ok && now.Sub(armedAt) < onPremL2DiscoveryWarmup {
		return true, fmt.Sprintf("onprem-l2 ownership discovery is warming up for %s", onPremL2DiscoveryWarmup)
	}
	for _, decision := range decisions {
		if strings.TrimSpace(decision.HomeOwnerNode) != strings.TrimSpace(self.NodeRef) {
			continue
		}
		switch decision.Class {
		case ownershipClassLocalHomeOwned, ownershipClassStaticOwned, ownershipClassStaticHandover:
			return false, ""
		}
	}
	return true, "onprem-l2 ownership discovery has not observed any local clients"
}

func discoveryResultCount(status map[string]any) int {
	for _, key := range []string{"discoveryResultCount", "discoveryObserved"} {
		value, ok := status[key]
		if !ok {
			continue
		}
		if parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value))); err == nil {
			return parsed
		}
	}
	return 0
}

func (c Controller) recordBGPStaticHandoverReleaseEvents(poolName, selfNode string, spec api.MobilityPoolSpec, events []routerstate.EventRecord, now time.Time) ([]routerstate.EventRecord, error) {
	if len(spec.StaticHandovers) == 0 {
		return nil, nil
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil, err
	}
	prefix = prefix.Masked()
	existing := map[string]bool{}
	for _, ev := range events {
		if ev.Type != ExpiredEventType || strings.TrimSpace(ev.SourceNode) != strings.TrimSpace(selfNode) {
			continue
		}
		address, ok := normalizeLeaseAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), prefix)
		if ok {
			existing[address] = true
		}
	}
	var emitted []routerstate.EventRecord
	for _, handover := range spec.StaticHandovers {
		if strings.TrimSpace(handover.FromNodeRef) != strings.TrimSpace(selfNode) {
			continue
		}
		address, ok := normalizeLeaseAddress(handover.Address, prefix)
		if !ok || existing[address] {
			continue
		}
		ev := routerstate.EventRecord{
			ID:         "mobility:bgp-static-release:" + poolName + ":" + selfNode + ":" + safeName(address),
			Group:      spec.GroupRef,
			SourceNode: selfNode,
			Type:       ExpiredEventType,
			Subject:    address,
			DedupeKey:  "mobility:bgp-static-release:" + poolName + ":" + selfNode + ":" + address,
			Payload: map[string]string{
				"address":    address,
				"pool":       poolName,
				"sourceType": staticHandoverType,
			},
			ObservedAt: now.UTC(),
			RecordedAt: now.UTC(),
			ExpiresAt:  now.UTC().Add(DefaultLeaseTTL),
		}
		if err := c.Store.RecordFederationEvent(ev); err != nil {
			return nil, fmt.Errorf("record BGP static handover release %q: %w", ev.ID, err)
		}
		emitted = append(emitted, ev)
	}
	return emitted, nil
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
			ExpiresAt:   now.Add(DefaultLeaseTTL),
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

func mobilityBGPMode(spec api.MobilityPoolSpec) bool {
	return mobilityDeliveryMode(spec) == "bgp"
}

func mobilityDeliveryMode(spec api.MobilityPoolSpec) string {
	mode := strings.TrimSpace(spec.DeliveryPolicy.Mode)
	if mode == "" {
		return "bgp"
	}
	return mode
}

type bgpOwnedAddress struct {
	Address    string
	SourceType string
}

type providerInventoryOwnerFact struct {
	Address      string
	NodeRef      string
	Provider     string
	ProviderRef  string
	SubnetRef    string
	NICRef       string
	ResourceRef  string
	ResourceType string
	ObservedAt   time.Time
}

func (c Controller) bgpLivenessMarkerPath(poolName, source, selfNode, groupRef string) (bgpdaemon.AppliedPath, bool) {
	prefix, ok := c.selfLivenessMarkerPrefix(groupRef)
	if !ok {
		return bgpdaemon.AppliedPath{}, false
	}
	nodeCommunity := bgpstate.MobilityNodeIdentityCommunity(canonicalNodeIdentity(selfNode))
	if nodeCommunity == "" {
		return bgpdaemon.AppliedPath{}, false
	}
	return bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
		Source: source,
		Prefix: prefix,
		Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
		Attrs: bgpdaemon.AppliedPathAttrs{
			LocalPref:   50,
			Communities: []string{bgpstate.MobilityCommunityNodeLiveness, nodeCommunity},
		},
	}), true
}

func (c Controller) bgpSelfReturnRoutePaths(poolName, source string, self memberPlanInfo, spec api.MobilityPoolSpec) []bgpdaemon.AppliedPath {
	if c.Store == nil {
		return nil
	}
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil
	}
	poolPrefix = poolPrefix.Masked()
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	primaryObserved, ok := statusBool(status["discoverySelfPrimaryObserved"])
	if !ok || !primaryObserved {
		return nil
	}
	selfIPs := statusStringSet(status["discoverySelfPrivateIPs"], poolPrefix)
	if len(selfIPs) == 0 {
		return nil
	}
	captured := statusStringSet(status["discoverySelfCapturedAddresses"], poolPrefix)
	var out []bgpdaemon.AppliedPath
	for _, address := range mapStringKeysSorted(selfIPs) {
		if captured[address] {
			continue
		}
		prefix, err := netip.ParsePrefix(address)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			continue
		}
		out = append(out, bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{
			Source: source,
			Prefix: prefix.Masked().String(),
			Family: bgpdaemon.AppliedPathFamilyIPv4Unicast,
			Attrs:  bgpMobilityReturnRoutePathAttrs(self),
		}))
	}
	return out
}

func (c Controller) selfLivenessMarkerPrefix(groupRef string) (string, bool) {
	if c.Router == nil {
		return "", false
	}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.FederationAPIVersion || res.Kind != "EventGroup" || strings.TrimSpace(res.Metadata.Name) != strings.TrimSpace(groupRef) {
			continue
		}
		spec, err := res.EventGroupSpec()
		if err != nil {
			return "", false
		}
		listenAddress := strings.TrimSpace(spec.Listen.Address)
		if listenAddress == "" {
			break
		}
		addr, err := netip.ParseAddr(listenAddress)
		if err != nil || !addr.Is4() {
			return "", false
		}
		return netip.PrefixFrom(addr, 32).String(), true
	}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "BGPRouter" {
			continue
		}
		spec, err := res.BGPRouterSpec()
		if err != nil {
			continue
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(spec.RouterID))
		if err == nil && addr.Is4() {
			return netip.PrefixFrom(addr, 32).String(), true
		}
	}
	return "", false
}

func bgpLocalOwnedAddressesFromConfigAndEvents(poolName, selfNode string, spec api.MobilityPoolSpec, events []routerstate.EventRecord, discoveryOwnedAddresses map[string]bool, discoveryOwnedObserved bool, discoverySelfIPs map[string]bool, discoverySelfIPsObserved bool, poolPrefix netip.Prefix, now time.Time) []bgpOwnedAddress {
	owned := map[string]bgpOwnedAddress{}
	latest := map[string]routerstate.EventRecord{}
	latestByAddressSource := map[string]map[string]routerstate.EventRecord{}
	staticHandovers := staticHandoversByFrom(spec.StaticHandovers, poolPrefix)
	members := plannerMembers(spec.Members)
	self := members[strings.TrimSpace(selfNode)]
	for _, member := range spec.Members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if !bgpMemberAdvertisesOwnedAddress(self, members[nodeRef]) {
			continue
		}
		for _, raw := range member.StaticOwnedAddresses {
			address, ok := normalizeLeaseAddress(raw, poolPrefix)
			if !ok {
				continue
			}
			if _, moving := staticHandovers[staticHandoverKey(address, selfNode)]; moving {
				continue
			}
			owned[address] = bgpOwnedAddress{Address: address, SourceType: staticOwnedType}
		}
	}
	for _, ev := range events {
		if ev.Group != spec.GroupRef || ev.Type != ObservedEventType && ev.Type != ExpiredEventType {
			continue
		}
		address, ok := normalizeLeaseAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), poolPrefix)
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
		sourceKey := bgpOwnershipEventSourceKey(candidate)
		if latestByAddressSource[address] == nil {
			latestByAddressSource[address] = map[string]routerstate.EventRecord{}
		}
		currentBySource, foundBySource := latestByAddressSource[address][sourceKey]
		if !foundBySource || eventRecordGreater(candidate, currentBySource) {
			latestByAddressSource[address][sourceKey] = candidate
		}
	}
	for address, bySource := range latestByAddressSource {
		for _, ev := range bySource {
			sourceType := bgpOwnershipEventSourceType(ev)
			if sourceType == staticHandoverType && (ev.Type == ExpiredEventType || (!ev.ExpiresAt.IsZero() && !now.Before(ev.ExpiresAt))) {
				delete(owned, address)
			}
		}
	}
	for address, bySource := range latestByAddressSource {
		for _, ev := range bySource {
			if ev.Type == ExpiredEventType || (!ev.ExpiresAt.IsZero() && !now.Before(ev.ExpiresAt)) {
				continue
			}
			sourceType := bgpOwnershipEventSourceType(ev)
			if sourceType == providerDiscoverySource && strings.TrimSpace(ev.SourceNode) == strings.TrimSpace(selfNode) {
				eventNIC := strings.TrimSpace(ev.Payload["nicRef"])
				selfNIC := strings.TrimSpace(self.Capture.NICRef)
				if discoveryOwnedObserved && !discoveryOwnedAddresses[address] ||
					discoverySelfIPsObserved && discoverySelfIPs[address] ||
					eventNIC != "" && selfNIC != "" && eventNIC == selfNIC {
					continue
				}
			}
			if sourceType == providerDiscoverySource && strings.TrimSpace(ev.SourceNode) != strings.TrimSpace(selfNode) {
				continue
			}
			if bgpMemberAdvertisesOwnedAddress(self, members[strings.TrimSpace(ev.SourceNode)]) {
				if strings.TrimSpace(ev.Payload["instanceState"]) == "stopped" && stoppedInstancePolicyFromSpec(spec) == "hold" {
					continue
				}
				owned[address] = bgpOwnedAddress{Address: address, SourceType: sourceType}
			}
		}
	}
	for _, handover := range spec.StaticHandovers {
		if !bgpMemberAdvertisesOwnedAddress(self, members[strings.TrimSpace(handover.ToNodeRef)]) {
			continue
		}
		address, ok := normalizeLeaseAddress(handover.Address, poolPrefix)
		if !ok {
			continue
		}
		release := latest[address]
		if release.Type == ExpiredEventType && strings.TrimSpace(release.SourceNode) == strings.TrimSpace(handover.FromNodeRef) {
			owned[address] = bgpOwnedAddress{Address: address, SourceType: staticHandoverType}
		}
	}
	out := make([]bgpOwnedAddress, 0, len(owned))
	for _, rec := range owned {
		out = append(out, rec)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Address < out[j].Address
	})
	return out
}

func bgpOwnershipEventSourceType(ev routerstate.EventRecord) string {
	sourceType := strings.TrimSpace(ev.Payload["sourceType"])
	if sourceType == "" {
		sourceType = bgpMobilitySourceFromEvent(ev)
	}
	return sourceType
}

func bgpOwnershipEventSourceKey(ev routerstate.EventRecord) string {
	sourceType := bgpOwnershipEventSourceType(ev)
	if sourceType == "" {
		sourceType = "observed"
	}
	source := strings.TrimSpace(ev.Payload["source"])
	if source == "" {
		source = "event"
	}
	return strings.Join([]string{source, sourceType, strings.TrimSpace(ev.SourceNode)}, "\x00")
}

func providerInventoryHomeOwnerFacts(poolName string, spec api.MobilityPoolSpec, events []routerstate.EventRecord, now time.Time) map[string]providerInventoryOwnerFact {
	sets := providerInventoryHomeOwnerFactSets(poolName, spec, events, now)
	out := map[string]providerInventoryOwnerFact{}
	for address, facts := range sets {
		for _, fact := range facts {
			current, found := out[address]
			if !found || providerInventoryOwnerFactGreater(fact, current) {
				out[address] = fact
			}
		}
	}
	return out
}

func providerInventoryHomeOwnerFactSets(poolName string, spec api.MobilityPoolSpec, events []routerstate.EventRecord, now time.Time) map[string][]providerInventoryOwnerFact {
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil
	}
	poolPrefix = poolPrefix.Masked()
	routerNICs := mobilityRouterNICRefs(spec.Members)
	members := plannerMembers(spec.Members)
	latest := map[string]routerstate.EventRecord{}
	for _, ev := range events {
		if ev.Group != spec.GroupRef || ev.Type != ObservedEventType && ev.Type != ExpiredEventType {
			continue
		}
		if bgpOwnershipEventSourceType(ev) != providerDiscoverySource {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(ev.Payload["pool"]), strings.TrimSpace(poolName)) {
			continue
		}
		address, ok := normalizeLeaseAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), poolPrefix)
		if !ok {
			continue
		}
		key := address + "\x00" + strings.TrimSpace(ev.SourceNode)
		candidate := ev
		if candidate.ObservedAt.IsZero() {
			candidate.ObservedAt = now
		}
		current, found := latest[key]
		if !found || eventRecordGreater(candidate, current) {
			latest[key] = candidate
		}
	}
	out := map[string][]providerInventoryOwnerFact{}
	for _, ev := range latest {
		if ev.Type != ObservedEventType {
			continue
		}
		if !ev.ExpiresAt.IsZero() && !now.Before(ev.ExpiresAt) {
			continue
		}
		address, ok := normalizeLeaseAddress(firstNonEmpty(ev.Payload["address"], ev.Subject), poolPrefix)
		if !ok {
			continue
		}
		nicRef := strings.TrimSpace(ev.Payload["nicRef"])
		if nicRef != "" && routerNICs[nicRef] {
			continue
		}
		if strings.TrimSpace(ev.Payload["resourceType"]) == "router-nic" {
			continue
		}
		nodeRef := strings.TrimSpace(ev.SourceNode)
		member, memberOK := lookupMemberByNodeRef(members, nodeRef)
		if memberOK && nicRef != "" && nicRef == strings.TrimSpace(member.Capture.NICRef) {
			continue
		}
		providerRef := strings.TrimSpace(ev.Payload["providerRef"])
		fact := providerInventoryOwnerFact{
			Address:      address,
			NodeRef:      nodeRef,
			Provider:     strings.TrimSpace(ev.Payload["provider"]),
			ProviderRef:  providerRef,
			SubnetRef:    strings.TrimSpace(ev.Payload["subnetRef"]),
			NICRef:       nicRef,
			ResourceRef:  strings.TrimSpace(ev.Payload["resourceRef"]),
			ResourceType: strings.TrimSpace(ev.Payload["resourceType"]),
			ObservedAt:   ev.ObservedAt.UTC(),
		}
		out[address] = append(out[address], fact)
	}
	for address := range out {
		sort.SliceStable(out[address], func(i, j int) bool {
			if !out[address][i].ObservedAt.Equal(out[address][j].ObservedAt) {
				return out[address][i].ObservedAt.After(out[address][j].ObservedAt)
			}
			return out[address][i].NodeRef < out[address][j].NodeRef
		})
	}
	return out
}

func providerInventoryOwnerFactGreater(candidate, current providerInventoryOwnerFact) bool {
	return candidate.ObservedAt.After(current.ObservedAt) ||
		candidate.ObservedAt.Equal(current.ObservedAt) && candidate.NodeRef < current.NodeRef
}

func stoppedInstancePolicyFromSpec(spec api.MobilityPoolSpec) string {
	p := strings.TrimSpace(spec.IPOwnershipPolicy.StoppedInstancePolicy)
	if p == "release" {
		return "release"
	}
	return "hold"
}

func bgpMemberAdvertisesOwnedAddress(self, owner memberPlanInfo) bool {
	if strings.TrimSpace(self.NodeRef) == "" || strings.TrimSpace(owner.NodeRef) == "" {
		return false
	}
	if strings.TrimSpace(owner.NodeRef) == strings.TrimSpace(self.NodeRef) {
		return true
	}
	if strings.TrimSpace(self.PlacementGroup) == "" {
		return false
	}
	return strings.TrimSpace(self.PlacementGroup) == strings.TrimSpace(owner.PlacementGroup) &&
		strings.TrimSpace(self.Site) == strings.TrimSpace(owner.Site)
}

func eventRecordGreater(candidate, current routerstate.EventRecord) bool {
	candidateAt := candidate.ObservedAt.UTC()
	currentAt := current.ObservedAt.UTC()
	if candidateAt.After(currentAt) {
		return true
	}
	if candidateAt.Before(currentAt) {
		return false
	}
	return strings.TrimSpace(candidate.ID) > strings.TrimSpace(current.ID)
}

func bgpMobilitySourceFromEvent(ev routerstate.EventRecord) string {
	switch strings.TrimSpace(ev.Type) {
	case staticOwnedType, staticHandoverType:
		return strings.TrimSpace(ev.Type)
	}
	switch strings.TrimSpace(ev.Payload["source"]) {
	case providerDiscoverySource:
		return providerDiscoverySource
	}
	return ""
}

func (c Controller) discoverySelfPrivateIPSet(poolName string, spec api.MobilityPoolSpec) (map[string]bool, bool) {
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	raw, ok := status["discoverySelfPrivateIPs"]
	rawCaptured, capturedOK := status["discoverySelfCapturedAddresses"]
	if !ok && !capturedOK {
		return nil, false
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil, false
	}
	prefix = prefix.Masked()
	out := map[string]bool{}
	for _, value := range append(statusStringSlice(raw), statusStringSlice(rawCaptured)...) {
		address, ok := normalizeDiscoveredAddress(value, prefix)
		if !ok {
			continue
		}
		out[address] = true
	}
	return out, true
}

func (c Controller) discoverySelfCapturedAddressSet(poolName string, spec api.MobilityPoolSpec) (map[string]bool, bool) {
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	raw, ok := status["discoverySelfCapturedAddresses"]
	if !ok {
		return nil, false
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil, false
	}
	prefix = prefix.Masked()
	out := map[string]bool{}
	for _, value := range statusStringSlice(raw) {
		address, ok := normalizeDiscoveredAddress(value, prefix)
		if !ok {
			continue
		}
		out[address] = true
	}
	return out, true
}

func (c Controller) discoveryLastScanAt(poolName string) time.Time {
	if c.Store == nil {
		return time.Time{}
	}
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status["discoveryLastScanAt"]))); err == nil {
		return parsed.UTC()
	}
	return time.Time{}
}

func (c Controller) discoveryProviderOwnedAddressSet(poolName string, spec api.MobilityPoolSpec) (map[string]bool, bool) {
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	raw, ok := status["discoveryOwnedAddresses"]
	if !ok {
		return nil, false
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil, false
	}
	prefix = prefix.Masked()
	out := map[string]bool{}
	for _, value := range statusStringSlice(raw) {
		address, ok := normalizeDiscoveredAddress(value, prefix)
		if !ok {
			continue
		}
		out[address] = true
	}
	return out, true
}

func (c Controller) discoverySelfForwardingState(poolName string) (observed bool, enabled bool, observedAt time.Time) {
	if c.Store == nil {
		return false, false, time.Time{}
	}
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	raw, ok := status["discoverySelfForwardingEnabled"]
	if !ok {
		return false, false, time.Time{}
	}
	enabled, ok = statusBool(raw)
	if !ok {
		return false, false, time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status["discoveryLastScanAt"]))); err == nil {
		observedAt = parsed.UTC()
	}
	return true, enabled, observedAt
}

func statusBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func decodeActionRecordMap(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func bgpProviderActionPlans(poolName, selfNode string, spec api.MobilityPoolSpec, desiredTrapAddresses map[string]bgpTrapCandidate, previousPlans []dynamicconfig.ActionPlan, profiles map[string]api.CloudProviderProfileSpec, actionJournal []routerstate.ActionExecutionRecord, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time, forwardingObserved, forwardingEnabled bool, forwardingObservedAt time.Time, suppressDeprovision, releaseStandbyCaptures bool, now time.Time) ([]dynamicconfig.ActionPlan, error) {
	members := plannerMembers(spec.Members)
	self, ok := members[strings.TrimSpace(selfNode)]
	if !ok {
		return nil, fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, poolName)
	}
	desiredAddresses := map[string]bool{}
	var plans []dynamicconfig.ActionPlan
	forwardingSeen := map[string]bool{}
	if self.Capture.Type == "provider-secondary-ip" {
		profile, ok := profiles[strings.TrimSpace(self.Capture.ProviderRef)]
		if !ok {
			return nil, fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q", self.Capture.ProviderRef, poolName, self.NodeRef)
		}
		for _, address := range mapStringKeysSorted(desiredTrapAddresses) {
			address = strings.TrimSpace(address)
			if address == "" {
				continue
			}
			desiredAddresses[address] = true
			candidate := desiredTrapAddresses[address]
			if candidate.ProtectOnly {
				continue
			}
			seize := candidate.Seize || shouldAllowBGPTrapReassignment(self, address, previousPlans, actionJournal, observedSelfCaptures, observedSelfCapturesOK, observedSelfAt)
			generated, err := providerActionPlans(poolName, profile, self.Capture, self.CaptureTarget, address, forwardingSeen, seize)
			if err != nil {
				return nil, err
			}
			stampBGPPathFenceActionPlans(generated, address, candidate.PathSig, self.NodeRef, candidate.LastSeenAt)
			stampBGPProviderTransitionFence(generated, self, address, actionJournal, observedSelfCaptures, observedSelfCapturesOK, observedSelfAt)
			stampForwardingDriftFence(generated, forwardingObserved, forwardingEnabled, forwardingObservedAt)
			plans = append(plans, generated...)
		}
	}
	if !suppressDeprovision {
		latestTransitions := latestProviderCaptureTransitions(previousPlans, actionJournal)
		seen := map[string]bool{}
		for _, previous := range sortedActionPlans(append(previousPlans, bgpSyntheticAssignedPlansFromJournal(self, actionJournal)...)) {
			if !isProviderCaptureAssignAction(previous.Action) {
				continue
			}
			address := strings.TrimSpace(previous.Target["address"])
			if address == "" || desiredAddresses[address] {
				continue
			}
			capture := captureFromActionPlan(self.Capture, self.CaptureTarget, previous)
			capture = captureWithTargetFallback(capture, previous.Target)
			strategy := effectiveCaptureStrategy("", captureStrategyValue(capture))
			if capture.Type != "provider-secondary-ip" {
				continue
			}
			if strategy == captureStrategySecondaryIP && !self.MaintenanceDrain && !releaseStandbyCaptures {
				continue
			}
			if strategy == captureStrategySecondaryIP && observedSelfCapturesOK && !observedSelfCaptures[address] {
				continue
			}
			transitionKey := providerCaptureTransitionKey(capture.ProviderRef, providerCaptureRefFromCapture(capture, previous.Target), address)
			if transitionKey == "" || seen[transitionKey] {
				continue
			}
			seen[transitionKey] = true
			if latest, ok := latestTransitions[transitionKey]; ok && !latest.assign {
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
			unassign = stampSingleBGPPathFence(unassign, address, bgpPathSigFromActionPlan(previous, address), self.NodeRef)
			plans = append(plans, unassign)
		}
	}
	return dedupeActionPlans(plans), nil
}

type bgpTrapCandidate struct {
	PathSig     string
	LastSeenAt  time.Time
	Seize       bool
	ProtectOnly bool
}

func previousBGPTrapCandidateAddresses(previousPlans []dynamicconfig.ActionPlan, poolPrefix netip.Prefix) map[string]bgpTrapCandidate {
	seen := map[string]bgpTrapCandidate{}
	for _, plan := range previousPlans {
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
		address, ok := normalizeBGPTrapPrefix(plan.Target["address"], poolPrefix)
		if ok {
			pathSig := strings.TrimSpace(plan.Parameters[bgpPathSigParam])
			if pathSig == "" {
				pathSig = "previous:" + address
			}
			seen[address] = bgpTrapCandidate{
				PathSig:    pathSig,
				LastSeenAt: parseBGPTrapLastSeenAt(plan.Parameters[bgpTrapLastSeenAtParam]),
			}
		}
	}
	return seen
}

func staticOwnedOwnerNodesByAddress(spec api.MobilityPoolSpec) map[string]string {
	out := map[string]string{}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return out
	}
	prefix = prefix.Masked()
	handoversByFrom := staticHandoversByFrom(spec.StaticHandovers, prefix)
	for _, member := range spec.Members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if nodeRef == "" {
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
			out[address] = nodeRef
		}
	}
	return out
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

func (c Controller) bgpCaptureCandidateNextHops(spec api.MobilityPoolSpec) (map[string][]string, bool) {
	if c.Router == nil || c.Store == nil {
		return map[string][]string{}, false
	}
	return bgpCaptureCandidateNextHopsFromStatus(c.Router, c.Store, spec)
}

func bgpCaptureCandidateNextHopsFromStatus(router *api.Router, store interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}, spec api.MobilityPoolSpec) (map[string][]string, bool) {
	out := map[string][]string{}
	observed := false
	if router == nil || store == nil {
		return out, observed
	}
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return out, observed
	}
	poolPrefix = poolPrefix.Masked()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		if _, ok := status["prefixes"]; !ok {
			continue
		}
		observed = true
		for _, prefix := range bgpStatusPrefixesValue(status["prefixes"]) {
			if !prefix.Best || !prefix.Valid || prefix.Stale || bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityNodeLiveness) {
				continue
			}
			if bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityReturnRoute) {
				continue
			}
			if bgpstate.HasCommunity(prefix.Communities, bgpMobilityCommunitySourceCapture) {
				continue
			}
			address, ok := normalizeBGPTrapPrefix(prefix.Prefix, poolPrefix)
			if !ok {
				continue
			}
			nextHop := strings.TrimSpace(prefix.NextHop)
			if nextHop == "" || nextHop == "0.0.0.0" || nextHop == "::" {
				continue
			}
			out[address] = mergeStringSet(out[address], []string{nextHop})
		}
	}
	if !observed {
		return out, false
	}
	return out, true
}

// bgpMobilityPrefixCommunitiesFromStatus returns, for each best/valid/non-stale
// mobility /32 inside the pool prefix observed in the BGP RIB, the BGP communities
// carried on the best path. The node-identity community among them identifies the
// active holder (the higher-preference, best-path advertiser of the owner /32),
// which bgpObservedGroupHolder maps back to a member: the holder-beacon.
func bgpMobilityPrefixCommunitiesFromStatus(router *api.Router, store interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}, spec api.MobilityPoolSpec) map[string][]string {
	out := map[string][]string{}
	if router == nil || store == nil {
		return out
	}
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return out
	}
	poolPrefix = poolPrefix.Masked()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		if _, ok := status["prefixes"]; !ok {
			continue
		}
		for _, prefix := range bgpStatusPrefixesValue(status["prefixes"]) {
			if !prefix.Best || !prefix.Valid || prefix.Stale {
				continue
			}
			address, ok := normalizeBGPTrapPrefix(prefix.Prefix, poolPrefix)
			if !ok {
				continue
			}
			out[address] = mergeStringSet(out[address], prefix.Communities)
		}
	}
	return out
}

func (c Controller) bgpHomeOwnerNodes(spec api.MobilityPoolSpec) map[string]string {
	out := map[string]string{}
	if c.Router == nil || c.Store == nil {
		return out
	}
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return out
	}
	communityOwners := map[string]string{}
	for _, member := range plannerMembers(spec.Members) {
		for _, community := range nodeIdentityCommunities(member.NodeRef) {
			if strings.TrimSpace(community) != "" {
				communityOwners[community] = member.NodeRef
			}
		}
	}
	if len(communityOwners) == 0 {
		return out
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := c.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		for _, prefix := range bgpStatusPrefixesValue(status["prefixes"]) {
			if !prefix.Valid || prefix.Stale || !bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityOwner) {
				continue
			}
			address, ok := normalizeBGPTrapPrefix(prefix.Prefix, poolPrefix)
			if !ok {
				continue
			}
			for _, community := range prefix.Communities {
				owner := strings.TrimSpace(communityOwners[strings.TrimSpace(community)])
				if owner == "" {
					continue
				}
				out[address] = owner
				break
			}
		}
	}
	return out
}

func (c Controller) bgpReturnRoutes(spec api.MobilityPoolSpec) map[string]bool {
	out := map[string]bool{}
	if c.Router == nil || c.Store == nil {
		return out
	}
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return out
	}
	poolPrefix = poolPrefix.Masked()
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := c.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		for _, prefix := range bgpStatusPrefixesValue(status["prefixes"]) {
			if !prefix.Valid || prefix.Stale || !bgpstate.HasCommunity(prefix.Communities, bgpstate.MobilityCommunityReturnRoute) {
				continue
			}
			address, ok := normalizeBGPTrapPrefix(prefix.Prefix, poolPrefix)
			if !ok {
				continue
			}
			out[address] = true
		}
	}
	return out
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

func bgpStatusPrefixesValue(value any) []bgpstate.Prefix {
	switch typed := value.(type) {
	case []bgpstate.Prefix:
		return append([]bgpstate.Prefix(nil), typed...)
	case []map[string]any:
		out := make([]bgpstate.Prefix, 0, len(typed))
		for _, item := range typed {
			out = append(out, bgpStatusPrefixFromMap(item))
		}
		return out
	case []any:
		out := make([]bgpstate.Prefix, 0, len(typed))
		for _, raw := range typed {
			switch item := raw.(type) {
			case bgpstate.Prefix:
				out = append(out, item)
			case map[string]any:
				out = append(out, bgpStatusPrefixFromMap(item))
			case map[string]string:
				out = append(out, bgpStatusPrefixFromStringMap(item))
			}
		}
		return out
	default:
		return nil
	}
}

func bgpStatusPrefixFromMap(item map[string]any) bgpstate.Prefix {
	return bgpstate.Prefix{
		Prefix:      statusString(item["prefix"]),
		NextHop:     statusString(item["nextHop"]),
		Best:        statusBoolDefault(item["best"]),
		Valid:       statusBoolDefault(item["valid"]),
		Installed:   statusBoolDefault(item["installed"]),
		Selected:    statusBoolDefault(item["selected"]),
		Stale:       statusBoolDefault(item["stale"]),
		Communities: statusStringSlice(item["communities"]),
	}
}

func bgpStatusPrefixFromStringMap(item map[string]string) bgpstate.Prefix {
	return bgpstate.Prefix{
		Prefix:      strings.TrimSpace(item["prefix"]),
		NextHop:     strings.TrimSpace(item["nextHop"]),
		Best:        stringBool(item["best"]),
		Valid:       stringBool(item["valid"]),
		Installed:   stringBool(item["installed"]),
		Selected:    stringBool(item["selected"]),
		Stale:       stringBool(item["stale"]),
		Communities: statusStringSlice(item["communities"]),
	}
}

func statusBoolDefault(value any) bool {
	if out, ok := statusBool(value); ok {
		return out
	}
	return false
}

func stringBool(value string) bool {
	out, _ := statusBool(strings.TrimSpace(value))
	return out
}

func (c Controller) bgpLivenessMarkers() (map[string]string, bool) {
	return bgpLivenessMarkersFromStatus(c.Router, c.Store)
}

func bgpLivenessMarkersFromStatus(router *api.Router, store interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}) (map[string]string, bool) {
	out := map[string]string{}
	observed := false
	if router == nil || store == nil {
		return out, observed
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		raw, ok := status["livenessMarkers"]
		if !ok {
			continue
		}
		observed = true
		for community, prefix := range bgpLivenessMarkersValue(raw) {
			out[community] = prefix
		}
	}
	return out, observed
}

func bgpLivenessMarkersValue(value any) map[string]string {
	out := map[string]string{}
	switch typed := value.(type) {
	case map[string]string:
		for community, prefix := range typed {
			community = strings.TrimSpace(community)
			if community != "" {
				out[community] = normalizeObservedBGPPrefix(prefix)
			}
		}
	case map[string]any:
		for community, raw := range typed {
			community = strings.TrimSpace(community)
			if community != "" {
				out[community] = normalizeObservedBGPPrefix(fmt.Sprint(raw))
			}
		}
	}
	for community, prefix := range out {
		if prefix == "" {
			delete(out, community)
		}
	}
	return out
}

func normalizeObservedBGPPrefix(value string) string {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return prefix.Masked().String()
}

func bgpTrapSelfNextHop(markerPrefix string) string {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(markerPrefix))
	if err != nil {
		return ""
	}
	return prefix.Addr().String()
}

func bgpTrapHasRemoteNextHop(nextHops []string, selfNextHop string) bool {
	selfNextHop = strings.TrimSpace(selfNextHop)
	if selfNextHop == "" {
		return false
	}
	for _, nextHop := range cleanStrings(nextHops) {
		nextHop = strings.TrimSpace(nextHop)
		switch nextHop {
		case "", "0.0.0.0", "::":
			continue
		default:
			if nextHop != selfNextHop {
				return true
			}
		}
	}
	return false
}

func evaluateBGPCapturePlacement(self memberPlanInfo, members map[string]memberPlanInfo, livenessMarkers map[string]string, livenessMarkersObserved bool, observedHolderNode string) PlacementDecision {
	placement := evaluatePlacementWithIncumbent(self, members, observedHolderNode)
	placement.LivenessObserved = livenessMarkersObserved
	selfCommunity, selfMarker, selfMarkerPresent := livenessMarkerForNode(livenessMarkers, self.NodeRef)
	placement.SelfCommunity = selfCommunity
	placement.SelfMarker = selfMarker
	placement.SelfMarkerPresent = selfMarkerPresent
	if placement.Active || placement.NoCandidate() || strings.TrimSpace(placement.ActiveNode) == "" {
		return placement
	}
	if !livenessMarkersObserved {
		return placement
	}
	if !selfMarkerPresent {
		return placement
	}
	active, ok := lookupMemberByNodeRef(members, placement.ActiveNode)
	if !ok {
		placement.Reason = fmt.Sprintf("placement group %q active node %q is not resolvable for BGP liveness identity", placement.Group, placement.ActiveNode)
		return placement
	}
	if strings.TrimSpace(active.NodeRef) == "" {
		return placement
	}
	activeCommunity, activeMarker, activeMarkerPresent := livenessMarkerForNode(livenessMarkers, active.NodeRef)
	placement.ActiveIdentityNodeRef = active.NodeRef
	placement.ActiveCommunity = activeCommunity
	placement.ActiveMarker = activeMarker
	placement.ActiveMarkerPresent = activeMarkerPresent
	if activeCommunity == "" || activeMarkerPresent {
		return placement
	}
	return PlacementDecision{
		Group:                 placement.Group,
		Active:                true,
		ActiveNode:            self.NodeRef,
		Reason:                fmt.Sprintf("placement group %q configured active %q has no live BGP identity path", placement.Group, active.NodeRef),
		Seize:                 true,
		LivenessObserved:      placement.LivenessObserved,
		SelfCommunity:         placement.SelfCommunity,
		SelfMarker:            placement.SelfMarker,
		SelfMarkerPresent:     placement.SelfMarkerPresent,
		ActiveIdentityNodeRef: active.NodeRef,
		ActiveCommunity:       activeCommunity,
		ActiveMarker:          activeMarker,
		ActiveMarkerPresent:   false,
	}
}

func (c Controller) applyBGPCaptureSeizeHoldDown(poolName string, placement PlacementDecision, now time.Time) PlacementDecision {
	var status map[string]any
	if c.Store != nil {
		status = c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	}
	return applyBGPCaptureSeizeHoldDown(status, placement, now)
}

func (c DiscoveryController) applyBGPCaptureSeizeHoldDown(poolName string, placement PlacementDecision, now time.Time) PlacementDecision {
	var status map[string]any
	if c.Store != nil {
		status = c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	}
	return applyBGPCaptureSeizeHoldDown(status, placement, now)
}

func applyBGPCaptureSeizeHoldDown(status map[string]any, placement PlacementDecision, now time.Time) PlacementDecision {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	key := bgpSeizeHoldDownKey(placement)
	if !placement.Seize || key == "" {
		return placement
	}
	since := now
	if strings.TrimSpace(fmt.Sprint(status["bgpSeizeHoldDownKey"])) == key {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status["bgpSeizeHoldDownSince"]))); err == nil && !parsed.IsZero() {
			since = parsed.UTC()
		}
	}
	until := since.Add(bgpSeizeLivenessMissingHold)
	placement.SeizeHoldDownKey = key
	placement.SeizeHoldDownSince = since
	placement.SeizeHoldDownUntil = until
	if !now.Before(until) {
		return placement
	}
	placement.SeizeHoldDown = true
	placement.Seize = false
	placement.Active = false
	if active := strings.TrimSpace(placement.ActiveIdentityNodeRef); active != "" {
		placement.ActiveNode = active
	}
	placement.Reason = strings.TrimSpace(firstNonEmpty(placement.Reason, "active BGP liveness marker is absent")) +
		"; waiting for seize hold-down until " + until.Format(time.RFC3339Nano)
	return placement
}

func bgpSeizeHoldDownKey(placement PlacementDecision) string {
	if !placement.Seize {
		return ""
	}
	parts := []string{
		strings.TrimSpace(placement.Group),
		strings.TrimSpace(placement.ActiveIdentityNodeRef),
		strings.TrimSpace(placement.ActiveCommunity),
		strings.TrimSpace(placement.SelfCommunity),
	}
	if parts[1] == "" || parts[2] == "" || parts[3] == "" {
		return ""
	}
	return strings.Join(parts, "\x00")
}

func lookupMemberByNodeRef(members map[string]memberPlanInfo, nodeRef string) (memberPlanInfo, bool) {
	nodeRef = strings.TrimSpace(nodeRef)
	if nodeRef == "" {
		return memberPlanInfo{}, false
	}
	if member, ok := members[nodeRef]; ok {
		return member, true
	}
	canonical := canonicalNodeIdentity(nodeRef)
	for _, member := range members {
		if canonicalNodeIdentity(member.NodeRef) == canonical {
			return member, true
		}
	}
	var matches []memberPlanInfo
	for _, member := range members {
		for _, alias := range nodeIdentityAliases(member.NodeRef) {
			if alias == canonical {
				matches = append(matches, member)
				break
			}
		}
	}
	if len(matches) == 0 {
		return memberPlanInfo{}, false
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].PlacementPriority == matches[j].PlacementPriority {
			return matches[i].NodeRef < matches[j].NodeRef
		}
		return matches[i].PlacementPriority < matches[j].PlacementPriority
	})
	return matches[0], true
}

func livenessMarkerForNode(markers map[string]string, nodeRef string) (string, string, bool) {
	for _, community := range nodeIdentityCommunities(nodeRef) {
		if marker := strings.TrimSpace(markers[community]); marker != "" {
			return community, marker, true
		}
	}
	communities := nodeIdentityCommunities(nodeRef)
	if len(communities) == 0 {
		return "", "", false
	}
	return communities[0], "", false
}

func nodeIdentityCommunities(nodeRef string) []string {
	seen := map[string]bool{}
	var out []string
	for _, candidate := range nodeIdentityAliases(nodeRef) {
		community := bgpstate.MobilityNodeIdentityCommunity(candidate)
		if community == "" || seen[community] {
			continue
		}
		seen[community] = true
		out = append(out, community)
	}
	return out
}

func nodeIdentityAliases(nodeRef string) []string {
	canonical := canonicalNodeIdentity(nodeRef)
	candidates := []string{canonical, strings.TrimSpace(nodeRef)}
	for _, suffix := range []string{"-a", "-b"} {
		if strings.HasSuffix(canonical, suffix) {
			candidates = append(candidates, strings.TrimSuffix(canonical, suffix))
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

func canonicalNodeIdentity(nodeRef string) string {
	nodeRef = strings.TrimSpace(nodeRef)
	if nodeRef == "" {
		return ""
	}
	if idx := strings.LastIndex(nodeRef, "/"); idx >= 0 && idx+1 < len(nodeRef) {
		return strings.TrimSpace(nodeRef[idx+1:])
	}
	return nodeRef
}

func bgpCaptureElectionStatus(decision PlacementDecision) map[string]any {
	status := map[string]any{
		"group":               decision.Group,
		"active":              decision.Active,
		"activeNode":          decision.ActiveNode,
		"reason":              decision.Reason,
		"seize":               decision.Seize,
		"livenessObserved":    decision.LivenessObserved,
		"selfCommunity":       decision.SelfCommunity,
		"selfMarkerPresent":   decision.SelfMarkerPresent,
		"activeCommunity":     decision.ActiveCommunity,
		"activeMarkerPresent": decision.ActiveMarkerPresent,
	}
	if decision.SelfMarker != "" {
		status["selfMarker"] = decision.SelfMarker
	}
	if decision.ActiveMarker != "" {
		status["activeMarker"] = decision.ActiveMarker
	}
	if decision.ActiveIdentityNodeRef != "" {
		status["activeIdentityNodeRef"] = decision.ActiveIdentityNodeRef
	}
	if decision.SeizeHoldDownKey != "" {
		status["seizeHoldDown"] = decision.SeizeHoldDown
		status["seizeHoldDownKey"] = decision.SeizeHoldDownKey
		status["seizeHoldDownSince"] = decision.SeizeHoldDownSince.Format(time.RFC3339Nano)
		status["seizeHoldDownUntil"] = decision.SeizeHoldDownUntil.Format(time.RFC3339Nano)
	} else {
		status["seizeHoldDown"] = false
	}
	return status
}

func bgpSeizeHoldDownStatus(decision PlacementDecision) map[string]any {
	status := map[string]any{
		"bgpSeizeHoldDownActive":  false,
		"bgpSeizeHoldDownKey":     "",
		"bgpSeizeHoldDownSince":   "",
		"bgpSeizeHoldDownUntil":   "",
		"bgpCapturePending":       false,
		"bgpCapturePendingReason": "",
		"bgpCapturePendingUntil":  "",
	}
	if decision.SeizeHoldDownKey == "" {
		return status
	}
	since := decision.SeizeHoldDownSince.Format(time.RFC3339Nano)
	until := decision.SeizeHoldDownUntil.Format(time.RFC3339Nano)
	status["bgpSeizeHoldDownActive"] = decision.SeizeHoldDown
	status["bgpSeizeHoldDownKey"] = decision.SeizeHoldDownKey
	status["bgpSeizeHoldDownSince"] = since
	status["bgpSeizeHoldDownUntil"] = until
	if decision.SeizeHoldDown {
		status["bgpCapturePending"] = true
		status["bgpCapturePendingReason"] = "seize-hold-down"
		status["bgpCapturePendingUntil"] = until
	}
	return status
}

type bgpCaptureClaim struct {
	Group          string
	Phase          string
	Generation     string
	EpochSeq       int64
	DesiredHolder  string
	PreviousHolder string
	Reason         string
	IssuedAt       time.Time
	RenewedAt      time.Time
	PendingUntil   time.Time
	LeaseUntil     time.Time
}

type bgpCaptureAssignment struct {
	Address        string
	Phase          string
	Generation     string
	Seq            int64
	ClaimEpoch     string
	DesiredHolder  string
	PreviousHolder string
	Reason         string
	IssuedAt       time.Time
	RenewedAt      time.Time
	LeaseUntil     time.Time
}

func bgpCaptureClaimForPlacement(self memberPlanInfo, placement PlacementDecision, now time.Time) bgpCaptureClaim {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	claim := bgpCaptureClaim{
		Group:         strings.TrimSpace(placement.Group),
		DesiredHolder: strings.TrimSpace(placement.ActiveNode),
		Reason:        strings.TrimSpace(placement.Reason),
	}
	selfNode := strings.TrimSpace(self.NodeRef)
	if placement.SeizeHoldDown {
		claim.Phase = "Pending"
		claim.DesiredHolder = selfNode
		claim.PreviousHolder = firstNonEmpty(placement.ActiveIdentityNodeRef, placement.ActiveNode)
		claim.Reason = firstNonEmpty(claim.Reason, "seize-hold-down")
		claim.PendingUntil = placement.SeizeHoldDownUntil.UTC()
	} else if placement.Active && strings.TrimSpace(placement.ActiveNode) == selfNode {
		claim.Phase = "Active"
		claim.DesiredHolder = selfNode
		claim.PreviousHolder = firstNonEmpty(placement.ActiveIdentityNodeRef, "")
		claim.LeaseUntil = now.Add(DefaultLeaseTTL).UTC()
		if placement.Seize {
			claim.Reason = firstNonEmpty(claim.Reason, "hard-failure")
		} else {
			claim.Reason = firstNonEmpty(claim.Reason, "placement-election")
		}
	} else if strings.TrimSpace(placement.ActiveNode) != "" {
		claim.Phase = "Standby"
		claim.DesiredHolder = strings.TrimSpace(placement.ActiveNode)
		claim.Reason = firstNonEmpty(claim.Reason, "peer-active")
	} else if placement.NoCandidate() {
		claim.Phase = "NoCandidate"
		claim.Reason = firstNonEmpty(claim.Reason, "no-placement-candidate")
	} else {
		claim.Phase = "Inactive"
		claim.Reason = firstNonEmpty(claim.Reason, "placement-inactive")
	}
	claim.Generation = bgpCaptureClaimGeneration(claim)
	return claim
}

func bgpCaptureClaimForPlacementWithStatus(poolName string, self memberPlanInfo, members map[string]memberPlanInfo, placement PlacementDecision, status map[string]any, now time.Time) bgpCaptureClaim {
	claim := bgpCaptureClaimForPlacement(self, placement, now)
	if claim.Phase != "Active" {
		claim.EpochSeq = bgpCaptureClaimEpochSeqFromStatus(status)
		return claim
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	previous := bgpCaptureClaimFromStatus(status)
	claim.Reason = bgpCaptureClaimTransitionReason(claim, previous, placement, members)
	seq := bgpCaptureClaimEpochSeqFromStatus(status)
	if previous.Phase == "Active" &&
		previous.Generation != "" &&
		previous.Group == claim.Group &&
		previous.DesiredHolder == claim.DesiredHolder &&
		previous.PreviousHolder == claim.PreviousHolder {
		claim.Generation = previous.Generation
		claim.EpochSeq = firstNonZeroInt64(previous.EpochSeq, seq)
		claim.IssuedAt = previous.IssuedAt
		if claim.IssuedAt.IsZero() {
			claim.IssuedAt = now
		}
		claim.RenewedAt = now
		claim.LeaseUntil = now.Add(DefaultLeaseTTL).UTC()
		return claim
	}
	seq++
	if seq <= 0 {
		seq = 1
	}
	claim.EpochSeq = seq
	claim.Generation = bgpCaptureClaimEpoch(poolName, claim.Group, seq)
	claim.IssuedAt = now
	claim.RenewedAt = now
	claim.LeaseUntil = now.Add(DefaultLeaseTTL).UTC()
	return claim
}

func bgpCaptureClaimTransitionReason(claim, previous bgpCaptureClaim, placement PlacementDecision, members map[string]memberPlanInfo) string {
	if reason := strings.TrimSpace(placement.Reason); reason != "" {
		return reason
	}
	if placement.Seize {
		return "hard-failure"
	}
	if previous.Phase == "Active" &&
		strings.TrimSpace(previous.DesiredHolder) != "" &&
		strings.TrimSpace(previous.DesiredHolder) != strings.TrimSpace(claim.DesiredHolder) {
		if previousMember, ok := lookupMemberByNodeRef(members, previous.DesiredHolder); ok && previousMember.MaintenanceDrain {
			return "graceful-drain"
		}
	}
	return firstNonEmpty(claim.Reason, "placement-election")
}

func bgpCaptureClaimEpoch(poolName, group string, seq int64) string {
	scope := strings.TrimSpace(group)
	if scope == "" {
		scope = strings.TrimSpace(poolName)
	}
	if scope == "" || seq <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%d", scope, seq)
}

func bgpCaptureClaimFromStatus(status map[string]any) bgpCaptureClaim {
	raw, ok := status["bgpCaptureClaim"]
	if !ok || raw == nil {
		return bgpCaptureClaim{}
	}
	row, ok := raw.(map[string]any)
	if !ok {
		return bgpCaptureClaim{}
	}
	return bgpCaptureClaim{
		Group:          statusString(row["group"]),
		Phase:          statusString(row["phase"]),
		Generation:     statusString(row["generation"]),
		EpochSeq:       statusInt64(row["epochSeq"]),
		DesiredHolder:  statusString(row["desiredHolder"]),
		PreviousHolder: statusString(row["previousHolder"]),
		Reason:         statusString(row["reason"]),
		IssuedAt:       statusTime(row["issuedAt"]),
		RenewedAt:      statusTime(row["renewedAt"]),
		PendingUntil:   statusTime(row["pendingUntil"]),
		LeaseUntil:     statusTime(row["leaseUntil"]),
	}
}

func bgpCaptureClaimEpochSeqFromStatus(status map[string]any) int64 {
	seq := statusInt64(status["bgpCaptureClaimEpochSeq"])
	if seq > 0 {
		return seq
	}
	claim := bgpCaptureClaimFromStatus(status)
	return claim.EpochSeq
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func statusInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case uint:
		return int64(typed)
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0
		}
		return int64(typed)
	case float64:
		if typed <= 0 {
			return 0
		}
		return int64(typed)
	case float32:
		if typed <= 0 {
			return 0
		}
		return int64(typed)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func statusTime(value any) time.Time {
	if parsed, ok := statusTimeValue(value); ok {
		return parsed.UTC()
	}
	return time.Time{}
}

func bgpCaptureClaimGeneration(claim bgpCaptureClaim) string {
	key := strings.Join([]string{
		strings.TrimSpace(claim.Group),
		strings.TrimSpace(claim.DesiredHolder),
		strings.TrimSpace(claim.PreviousHolder),
	}, "\x00")
	if strings.Trim(key, "\x00") == "" {
		return ""
	}
	return bgpPathSigHash(key)
}

func bgpCaptureClaimStatus(claim bgpCaptureClaim) map[string]any {
	status := map[string]any{
		"group":          claim.Group,
		"phase":          claim.Phase,
		"generation":     claim.Generation,
		"epochSeq":       claim.EpochSeq,
		"desiredHolder":  claim.DesiredHolder,
		"previousHolder": claim.PreviousHolder,
		"reason":         claim.Reason,
	}
	if !claim.IssuedAt.IsZero() {
		status["issuedAt"] = claim.IssuedAt.UTC().Format(time.RFC3339Nano)
	}
	if !claim.RenewedAt.IsZero() {
		status["renewedAt"] = claim.RenewedAt.UTC().Format(time.RFC3339Nano)
	}
	if !claim.PendingUntil.IsZero() {
		status["pendingUntil"] = claim.PendingUntil.UTC().Format(time.RFC3339Nano)
	}
	if !claim.LeaseUntil.IsZero() {
		status["leaseUntil"] = claim.LeaseUntil.UTC().Format(time.RFC3339Nano)
	}
	return status
}

func bgpCaptureAssignmentsFromStatus(status map[string]any) map[string]bgpCaptureAssignment {
	out := map[string]bgpCaptureAssignment{}
	raw, ok := status["bgpCaptureAssignments"]
	if !ok || raw == nil {
		return out
	}
	appendRow := func(row map[string]any) {
		assignment := bgpCaptureAssignment{
			Address:        normalizeAddressString(statusString(row["address"])),
			Phase:          statusString(row["phase"]),
			Generation:     statusString(row["generation"]),
			Seq:            statusInt64(row["seq"]),
			ClaimEpoch:     statusString(row["claimEpoch"]),
			DesiredHolder:  statusString(row["desiredHolder"]),
			PreviousHolder: statusString(row["previousHolder"]),
			Reason:         statusString(row["reason"]),
			IssuedAt:       statusTime(row["issuedAt"]),
			RenewedAt:      statusTime(row["renewedAt"]),
			LeaseUntil:     statusTime(row["leaseUntil"]),
		}
		if assignment.Address == "" {
			return
		}
		out[assignment.Address] = assignment
	}
	switch typed := raw.(type) {
	case []any:
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				appendRow(row)
			}
		}
	case []map[string]any:
		for _, row := range typed {
			appendRow(row)
		}
	case map[string]any:
		for address, item := range typed {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if _, exists := row["address"]; !exists {
				row = copyAnyMap(row)
				row["address"] = address
			}
			appendRow(row)
		}
	}
	return out
}

func bgpCaptureAssignmentSeqFromStatus(status map[string]any) int64 {
	seq := statusInt64(status["bgpCaptureAssignmentSeq"])
	for _, assignment := range bgpCaptureAssignmentsFromStatus(status) {
		if assignment.Seq > seq {
			seq = assignment.Seq
		}
	}
	return seq
}

func bgpCaptureAssignmentStatusList(assignments map[string]bgpCaptureAssignment) []map[string]any {
	if len(assignments) == 0 {
		return nil
	}
	addresses := make([]string, 0, len(assignments))
	for address := range assignments {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	out := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		assignment := assignments[address]
		row := map[string]any{
			"address":        assignment.Address,
			"phase":          assignment.Phase,
			"generation":     assignment.Generation,
			"seq":            assignment.Seq,
			"claimEpoch":     assignment.ClaimEpoch,
			"desiredHolder":  assignment.DesiredHolder,
			"previousHolder": assignment.PreviousHolder,
			"reason":         assignment.Reason,
		}
		if !assignment.IssuedAt.IsZero() {
			row["issuedAt"] = assignment.IssuedAt.UTC().Format(time.RFC3339Nano)
		}
		if !assignment.RenewedAt.IsZero() {
			row["renewedAt"] = assignment.RenewedAt.UTC().Format(time.RFC3339Nano)
		}
		if !assignment.LeaseUntil.IsZero() {
			row["leaseUntil"] = assignment.LeaseUntil.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, row)
	}
	return out
}

func copyAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func parseBGPTrapLastSeenAt(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func observedSelfStaleCaptureSinceFromStatus(status map[string]any) map[string]time.Time {
	out := map[string]time.Time{}
	raw, ok := status["observedSelfStaleCaptures"]
	if !ok || raw == nil {
		return out
	}
	add := func(address, value string) {
		address = normalizeAddressString(address)
		if address == "" {
			return
		}
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
		if err != nil || parsed.IsZero() {
			return
		}
		out[address] = parsed.UTC()
	}
	switch typed := raw.(type) {
	case map[string]string:
		for address, value := range typed {
			add(address, value)
		}
	case map[string]any:
		for address, value := range typed {
			add(address, fmt.Sprint(value))
		}
	case []map[string]any:
		for _, row := range typed {
			add(fmt.Sprint(row["address"]), statusStringFirst(row["firstSeenAt"], row["since"]))
		}
	case []any:
		for _, item := range typed {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			add(fmt.Sprint(row["address"]), statusStringFirst(row["firstSeenAt"], row["since"]))
		}
	}
	return out
}

func statusStringFirst(values ...any) string {
	for _, value := range values {
		str := strings.TrimSpace(fmt.Sprint(value))
		if str == "" || str == "<nil>" {
			continue
		}
		return str
	}
	return ""
}

func observedSelfStaleCaptureStatus(decisions []ownershipDecision, selfNode string, previous map[string]time.Time, now time.Time) map[string]string {
	out := map[string]string{}
	for _, decision := range decisions {
		if decision.Class != ownershipClassStaleCapture {
			continue
		}
		if strings.TrimSpace(decision.CaptureHolderNode) != "" && strings.TrimSpace(decision.CaptureHolderNode) != strings.TrimSpace(selfNode) {
			continue
		}
		address := normalizeAddressString(decision.Address)
		if address == "" {
			continue
		}
		since := now.UTC()
		if previousSince, ok := previous[address]; ok && !previousSince.IsZero() {
			since = previousSince.UTC()
		}
		out[address] = since.Format(time.RFC3339Nano)
	}
	return out
}

func bgpTrapCandidateWithinMissingHold(candidate bgpTrapCandidate, now time.Time) bool {
	if candidate.LastSeenAt.IsZero() {
		return true
	}
	return now.UTC().Sub(candidate.LastSeenAt.UTC()) < bgpTrapRIBMissingHold
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

func bgpTrapPathSig(address string, nextHops []string) string {
	return "prefix=" + normalizeAddressString(address) + ";nextHops=" + strings.Join(cleanStrings(nextHops), ",")
}

func bgpPathSigHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:16]
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

func mapStringKeysSorted[T any](values map[string]T) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func shouldAllowBGPTrapReassignment(self memberPlanInfo, address string, previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time) bool {
	address = normalizeAddressString(address)
	if address == "" {
		return false
	}
	latest := latestProviderCaptureTransitions(previousPlans, journal)
	key := providerCaptureTransitionKey(self.Capture.ProviderRef, providerCaptureRefFromCapture(self.Capture, self.CaptureTarget), address)
	tr, ok := latest[key]
	if !ok && observedSelfCapturesOK && !observedSelfCaptures[address] {
		return true
	}
	if !ok {
		return false
	}
	if observedSelfCapturesOK && !observedSelfCaptures[address] {
		return providerMissingRetryDue(tr, observedSelfAt)
	}
	return !tr.assign && providerCaptureTransitionAllowsRecapture(tr)
}

func stampBGPPathFenceActionPlans(plans []dynamicconfig.ActionPlan, address, pathSig, holder string, lastSeenAt time.Time) {
	address = normalizeAddressString(address)
	pathSig = strings.TrimSpace(pathSig)
	holder = strings.TrimSpace(holder)
	if pathSig == "" {
		pathSig = "prefix=" + address
	}
	hash := bgpPathSigHash(pathSig)
	for i := range plans {
		plan := &plans[i]
		if plan.Parameters == nil {
			plan.Parameters = map[string]string{}
		}
		plan.Parameters[bgpPathSigParam] = pathSig
		plan.Parameters[captureParamHolder] = holder
		plan.Parameters["mobilityDesiredHolder"] = holder
		if !lastSeenAt.IsZero() {
			plan.Parameters[bgpTrapLastSeenAtParam] = lastSeenAt.UTC().Format(time.RFC3339Nano)
		}
		if strings.TrimSpace(plan.IdempotencyKey) != "" {
			plan.IdempotencyKey = plan.IdempotencyKey + ":holder:" + safeName(holder) + ":pathsig:" + hash
		}
	}
}

func stampSingleBGPPathFence(plan dynamicconfig.ActionPlan, address, pathSig, holder string) dynamicconfig.ActionPlan {
	plans := []dynamicconfig.ActionPlan{plan}
	stampBGPPathFenceActionPlans(plans, address, pathSig, holder, time.Time{})
	return plans[0]
}

func stampBGPClaimFenceActionPlans(plans []dynamicconfig.ActionPlan, claim bgpCaptureClaim) {
	if claim.Phase != "Active" || strings.TrimSpace(claim.Generation) == "" {
		return
	}
	for i := range plans {
		plan := &plans[i]
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
		if plan.Parameters == nil {
			plan.Parameters = map[string]string{}
		}
		plan.Parameters[captureClaimPhaseParam] = claim.Phase
		plan.Parameters[captureClaimGenerationParam] = claim.Generation
		plan.Parameters[captureClaimDesiredHolderParam] = claim.DesiredHolder
		if claim.PreviousHolder != "" {
			plan.Parameters[captureClaimPreviousHolderParam] = claim.PreviousHolder
		}
		if claim.Reason != "" {
			plan.Parameters[captureClaimReasonParam] = claim.Reason
		}
		if !claim.LeaseUntil.IsZero() {
			plan.Parameters[captureClaimLeaseUntilParam] = claim.LeaseUntil.UTC().Format(time.RFC3339Nano)
		}
	}
}

func stampBGPAssignmentFenceActionPlans(plans []dynamicconfig.ActionPlan, poolName string, self memberPlanInfo, decisions map[string]ownershipDecision, claim bgpCaptureClaim, previous map[string]bgpCaptureAssignment, seq int64, now time.Time) (map[string]bgpCaptureAssignment, int64) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	assignments := make(map[string]bgpCaptureAssignment)
	for _, assignment := range previous {
		if assignment.Seq > seq {
			seq = assignment.Seq
		}
	}
	if strings.TrimSpace(claim.Generation) == "" {
		return assignments, seq
	}
	leaseUntil := claim.LeaseUntil
	if leaseUntil.IsZero() {
		leaseUntil = now.Add(DefaultLeaseTTL).UTC()
	}
	for i := range plans {
		plan := &plans[i]
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
		address := normalizeAddressString(plan.Target["address"])
		if address == "" {
			continue
		}
		decision := decisions[address]
		previousHolder := firstNonEmpty(strings.TrimSpace(decision.CaptureHolderNode), strings.TrimSpace(claim.PreviousHolder))
		assignment := bgpCaptureAssignment{
			Address:        address,
			Phase:          "Active",
			ClaimEpoch:     claim.Generation,
			DesiredHolder:  strings.TrimSpace(self.NodeRef),
			PreviousHolder: previousHolder,
			Reason:         claim.Reason,
			RenewedAt:      now,
			LeaseUntil:     leaseUntil,
		}
		if existing, ok := previous[address]; ok &&
			existing.Generation != "" &&
			existing.Phase == assignment.Phase &&
			existing.DesiredHolder == assignment.DesiredHolder &&
			existing.PreviousHolder == assignment.PreviousHolder {
			assignment.Generation = existing.Generation
			assignment.Seq = existing.Seq
			assignment.IssuedAt = existing.IssuedAt
			if assignment.IssuedAt.IsZero() {
				assignment.IssuedAt = now
			}
		} else {
			seq++
			if seq <= 0 {
				seq = 1
			}
			assignment.Seq = seq
			assignment.Generation = bgpCaptureAssignmentGeneration(poolName, claim.Group, address, seq)
			assignment.IssuedAt = now
		}
		assignments[address] = assignment
		if plan.Parameters == nil {
			plan.Parameters = map[string]string{}
		}
		plan.Parameters[captureAssignmentPhaseParam] = assignment.Phase
		plan.Parameters[captureAssignmentGenerationParam] = assignment.Generation
		plan.Parameters[captureAssignmentDesiredHolderParam] = assignment.DesiredHolder
		if assignment.PreviousHolder != "" {
			plan.Parameters[captureAssignmentPreviousHolderParam] = assignment.PreviousHolder
		}
		plan.Parameters[captureAssignmentClaimEpochParam] = assignment.ClaimEpoch
		if !assignment.LeaseUntil.IsZero() {
			plan.Parameters[captureAssignmentLeaseUntilParam] = assignment.LeaseUntil.UTC().Format(time.RFC3339Nano)
		}
		if strings.TrimSpace(plan.IdempotencyKey) != "" && strings.TrimSpace(assignment.Generation) != "" {
			plan.IdempotencyKey += ":assigngen:" + safeName(assignment.Generation)
		}
	}
	return assignments, seq
}

func bgpCaptureAssignmentGeneration(poolName, group, address string, seq int64) string {
	scope := strings.TrimSpace(group)
	if scope == "" {
		scope = strings.TrimSpace(poolName)
	}
	address = normalizeAddressString(address)
	if scope == "" || address == "" || seq <= 0 {
		return ""
	}
	return fmt.Sprintf("%s/%s/%d", scope, safeName(address), seq)
}

func stampBGPProviderTransitionFence(plans []dynamicconfig.ActionPlan, self memberPlanInfo, address string, journal []routerstate.ActionExecutionRecord, observedSelfCaptures map[string]bool, observedSelfCapturesOK bool, observedSelfAt time.Time) {
	address = normalizeAddressString(address)
	if address == "" {
		return
	}
	latest := latestProviderCaptureTransitions(nil, journal)
	key := providerCaptureTransitionKey(self.Capture.ProviderRef, providerCaptureRefFromCapture(self.Capture, self.CaptureTarget), address)
	tr, ok := latest[key]
	if !ok {
		return
	}
	token := ""
	switch {
	case !tr.assign && providerCaptureTransitionAllowsRecapture(tr):
		token = fmt.Sprintf("after-unassign-%d", tr.id)
	case observedSelfCapturesOK && !observedSelfCaptures[address] && providerMissingRetryDue(tr, observedSelfAt):
		token = fmt.Sprintf("provider-missing-%d", tr.id)
	}
	if token == "" {
		return
	}
	for i := range plans {
		plan := &plans[i]
		if !isProviderCaptureAssignAction(plan.Action) || strings.TrimSpace(plan.IdempotencyKey) == "" {
			continue
		}
		if plan.Parameters == nil {
			plan.Parameters = map[string]string{}
		}
		plan.Parameters[bgpTrapTransitionParam] = token
		plan.IdempotencyKey += ":transition:" + safeName(token)
	}
}

func providerMissingRetryDue(tr providerCaptureTransition, observedSelfAt time.Time) bool {
	if !tr.assign {
		return true
	}
	if tr.at.IsZero() || observedSelfAt.IsZero() {
		return false
	}
	return !observedSelfAt.Before(tr.at.Add(bgpProviderMissingRetryHold))
}

func providerCaptureTransitionAllowsRecapture(tr providerCaptureTransition) bool {
	params := tr.plan.Parameters
	if strings.TrimSpace(params["deprovisionSince"]) != "" {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(params[bgpPathSigParam]), "deprovision:") {
		return false
	}
	return true
}

func stampForwardingDriftFence(plans []dynamicconfig.ActionPlan, observed, enabled bool, observedAt time.Time) {
	if !observed || enabled {
		return
	}
	token := "observed-disabled"
	if !observedAt.IsZero() {
		token += "-" + observedAt.UTC().Format("20060102T150405.000000000Z")
	}
	for i := range plans {
		plan := &plans[i]
		if plan.Action != "ensure-forwarding-enabled" || strings.TrimSpace(plan.IdempotencyKey) == "" {
			continue
		}
		if plan.Parameters == nil {
			plan.Parameters = map[string]string{}
		}
		plan.Parameters["mobilityForwardingDrift"] = token
		plan.IdempotencyKey += ":forwarding-drift:" + safeName(token)
	}
}

type providerCaptureTransition struct {
	at        time.Time
	id        int64
	assign    bool
	succeeded bool
	plan      dynamicconfig.ActionPlan
}

func latestProviderCaptureTransitions(previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord) map[string]providerCaptureTransition {
	latest := map[string]providerCaptureTransition{}
	for _, plan := range previousPlans {
		if !isProviderCaptureAssignAction(plan.Action) {
			continue
		}
		address := normalizeAddressString(plan.Target["address"])
		key := providerCaptureTransitionKey(firstNonEmpty(plan.ProviderRef, plan.Target["providerRef"]), providerCaptureRefFromTarget(plan.Target), address)
		if key == "" {
			continue
		}
		latest[key] = providerCaptureTransition{assign: true, plan: plan}
	}
	for _, row := range journal {
		if row.Status != routerstate.ActionSucceeded {
			continue
		}
		assign := false
		switch {
		case isProviderCaptureAssignAction(row.Action):
			assign = true
		case isProviderCaptureUnassignAction(row.Action):
			assign = false
		default:
			continue
		}
		target := decodeActionRecordMap(row.TargetJSON)
		address := normalizeAddressString(target["address"])
		key := providerCaptureTransitionKey(firstNonEmpty(row.ProviderRef, target["providerRef"]), providerCaptureRefFromTarget(target), address)
		if key == "" {
			continue
		}
		at := row.ExecutedAt
		if at.IsZero() {
			at = row.UpdatedAt
		}
		if prev, ok := latest[key]; ok && !at.After(prev.at) && !(at.Equal(prev.at) && row.ID > prev.id) {
			continue
		}
		params := decodeActionRecordMap(row.ParametersJSON)
		latest[key] = providerCaptureTransition{
			at:        at,
			id:        row.ID,
			assign:    assign,
			succeeded: true,
			plan: dynamicconfig.ActionPlan{
				IdempotencyKey: row.IdempotencyKey,
				Provider:       row.Provider,
				ProviderRef:    row.ProviderRef,
				Action:         row.Action,
				Target:         target,
				Parameters:     params,
			},
		}
	}
	return latest
}

func providerCaptureTransitionKey(providerRef, nicRef, address string) string {
	providerRef = strings.TrimSpace(providerRef)
	nicRef = strings.TrimSpace(nicRef)
	address = normalizeAddressString(address)
	if providerRef == "" || nicRef == "" || address == "" {
		return ""
	}
	return providerRef + "\x00" + nicRef + "\x00" + address
}

func bgpSyntheticAssignedPlansFromJournal(self memberPlanInfo, journal []routerstate.ActionExecutionRecord) []dynamicconfig.ActionPlan {
	latest := latestProviderCaptureTransitions(nil, journal)
	var out []dynamicconfig.ActionPlan
	for key, tr := range latest {
		if !tr.assign {
			continue
		}
		parts := strings.Split(key, "\x00")
		if len(parts) != 3 {
			continue
		}
		if strings.TrimSpace(parts[0]) != strings.TrimSpace(self.Capture.ProviderRef) || strings.TrimSpace(parts[1]) != providerCaptureRefFromCapture(self.Capture, self.CaptureTarget) {
			continue
		}
		if holder := strings.TrimSpace(tr.plan.Parameters[captureParamHolder]); holder != "" && holder != strings.TrimSpace(self.NodeRef) {
			continue
		}
		plan := tr.plan
		plan.Action, _ = providerCaptureActions(effectiveCaptureStrategy(plan.Provider, captureStrategyValue(self.Capture)))
		if plan.Target == nil {
			plan.Target = map[string]string{}
		}
		capture := captureWithTargetFallback(self.Capture, self.CaptureTarget)
		plan.Target["address"] = normalizeAddressString(parts[2])
		plan.Target["providerRef"] = strings.TrimSpace(self.Capture.ProviderRef)
		plan.Target["nicRef"] = strings.TrimSpace(capture.NICRef)
		if strategy := strings.TrimSpace(captureStrategyValue(self.Capture)); strategy != "" {
			plan.Target["captureStrategy"] = strategy
		}
		if plan.ProviderRef == "" {
			plan.ProviderRef = strings.TrimSpace(self.Capture.ProviderRef)
		}
		out = append(out, plan)
	}
	return out
}

func bgpPathSigFromActionPlan(plan dynamicconfig.ActionPlan, address string) string {
	if sig := strings.TrimSpace(plan.Parameters[bgpPathSigParam]); sig != "" {
		return sig
	}
	return "deprovision:" + normalizeAddressString(address)
}

func captureFromActionPlan(fallback api.AddressCapture, fallbackTarget map[string]string, plan dynamicconfig.ActionPlan) api.AddressCapture {
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
	} else if value := strings.TrimSpace(fallbackTarget["nicRef"]); value != "" {
		capture.NICRef = value
	}
	if value := strings.TrimSpace(plan.Target["captureStrategy"]); value != "" {
		capture.CaptureStrategy = value
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

func bgpMobilityPathAttrs(member memberPlanInfo, sourceType string, active bool) bgpdaemon.AppliedPathAttrs {
	communities := []string{}
	captureSource := strings.TrimSpace(sourceType) == "provider-capture"
	if !captureSource {
		communities = append(communities, bgpMobilityCommunityOwner)
	}
	if nodeCommunity := bgpstate.MobilityNodeIdentityCommunity(canonicalNodeIdentity(member.NodeRef)); nodeCommunity != "" && !captureSource {
		communities = append(communities, nodeCommunity)
	}
	switch member.Role {
	case "onprem":
		communities = append(communities, bgpMobilityCommunityRoleOnPrem)
	case "cloud":
		communities = append(communities, bgpMobilityCommunityRoleCloud)
	}
	switch strings.TrimSpace(sourceType) {
	case staticOwnedType:
		communities = append(communities, bgpMobilityCommunitySourceStatic)
	case staticHandoverType:
		communities = append(communities, bgpMobilityCommunitySourceHandover)
	case "provider-capture":
		communities = append(communities, bgpMobilityCommunitySourceCapture)
	default:
		communities = append(communities, bgpMobilityCommunitySourceObserved)
	}
	localPref := bgpMobilityLocalPrefBase
	if active {
		localPref = bgpMobilityLocalPref(1)
		if !captureSource {
			communities = append(communities, bgpMobilityCommunityActiveHolder)
		}
	}
	attrs := bgpdaemon.AppliedPathAttrs{
		LocalPref:   localPref,
		Communities: communities,
	}
	if member.PlacementPriority > 0 {
		attrs.MED = uint32(member.PlacementPriority)
	}
	return attrs
}

func bgpMobilityReturnRoutePathAttrs(member memberPlanInfo) bgpdaemon.AppliedPathAttrs {
	communities := []string{bgpMobilityCommunitySourceReturn}
	if nodeCommunity := bgpstate.MobilityNodeIdentityCommunity(canonicalNodeIdentity(member.NodeRef)); nodeCommunity != "" {
		communities = append(communities, nodeCommunity)
	}
	switch member.Role {
	case "onprem":
		communities = append(communities, bgpMobilityCommunityRoleOnPrem)
	case "cloud":
		communities = append(communities, bgpMobilityCommunityRoleCloud)
	}
	return bgpdaemon.AppliedPathAttrs{
		LocalPref:   bgpMobilityLocalPrefBase,
		Communities: communities,
	}
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

func (c Controller) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
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

func latestFailedProviderActions(actions []routerstate.ActionExecutionRecord) map[string]routerstate.ActionExecutionRecord {
	latest := map[string]routerstate.ActionExecutionRecord{}
	for _, a := range actions {
		if !isProviderCaptureAssignAction(a.Action) {
			continue
		}
		target := decodeActionRecordMap(a.TargetJSON)
		address := normalizeAddressString(target["address"])
		if address == "" {
			continue
		}
		prev, found := latest[address]
		if !found || a.UpdatedAt.After(prev.UpdatedAt) {
			latest[address] = a
		}
	}
	failed := map[string]routerstate.ActionExecutionRecord{}
	for addr, rec := range latest {
		if rec.Status == routerstate.ActionFailed {
			failed[addr] = rec
		}
	}
	return failed
}

type providerCaptureAssignFailureInterpretation struct {
	Active     map[string]routerstate.ActionExecutionRecord
	Superseded map[string]routerstate.ActionExecutionRecord
}

func interpretProviderCaptureAssignFailures(actions []routerstate.ActionExecutionRecord, observedSelfCaptures map[string]bool, observedSelfAt time.Time) providerCaptureAssignFailureInterpretation {
	latestFailed := latestFailedProviderActions(actions)
	out := providerCaptureAssignFailureInterpretation{
		Active:     map[string]routerstate.ActionExecutionRecord{},
		Superseded: map[string]routerstate.ActionExecutionRecord{},
	}
	for addr, rec := range latestFailed {
		if observedSelfCaptures[normalizeAddressString(addr)] && providerObservationFresh(observedSelfAt, actionRecordCompletedAt(rec)) {
			out.Superseded[addr] = rec
			continue
		}
		out.Active[addr] = rec
	}
	return out
}
