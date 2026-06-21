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

	bgpPathSigParam        = "mobilityPathSig"
	bgpTrapLastSeenAtParam = "mobilityTrapLastSeenAt"
	bgpTrapTransitionParam = "mobilityProviderTransition"
	bgpTrapRIBMissingHold  = 2 * time.Minute
	// BGP liveness-marker loss alone is a weak failover signal during full mesh
	// startup: WireGuard and BGP RIB convergence can lag past short controller
	// polls, and a premature capture seize is sticky by design. Keep this aligned
	// with the provider-trap RIB missing hold before allowing secondary-IP takeover.
	bgpSeizeLivenessMissingHold = 2 * time.Minute
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
	resolved, err := (mobilityMemberResolver{Router: c.Router, Sync: c.MemberSetSync}).resolve(ctx, spec)
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
	observedHolderNode := bgpObservedGroupHolder(self, members, livenessMarkers, bgpMobilityPrefixCommunitiesFromStatus(c.Router, c.Store, spec))
	ownerPlacement := c.applyBGPCaptureSeizeHoldDown(res.Metadata.Name, evaluateBGPCapturePlacement(self, members, livenessMarkers, livenessMarkersObserved, observedHolderNode, bgpPeerSessionsByNodeFromStatus(c.Router, c.Store)), now)
	ownerPlacement = fencePlacementForStartup(ownerPlacement, observedHolderNode, now)
	ownerPlacement = applyHolderRetention(ownerPlacement, len(discoverySelfCaptures) > 0, higherPriorityHolderActive(self, members, observedHolderNode), now)
	actionJournal, err := c.Store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return fmt.Errorf("list action journal: %w", err)
	}
	providerFailures := interpretProviderCaptureAssignFailures(actionJournal, discoverySelfCaptures)
	failedActions := providerFailures.Active
	previousActionPlans, err := c.previousGeneratedActionPlans(res.Metadata.Name, selfNode)
	if err != nil {
		return err
	}
	installedNextHops, bgpRIBObserved := c.bgpInstalledNextHops()
	captureNextHops, captureRIBObserved := c.bgpCaptureCandidateNextHops(spec)
	if captureRIBObserved {
		bgpRIBObserved = true
	}
	bgpHomeOwnerNodes := c.bgpHomeOwnerNodes(spec)
	bgpReturnRoutes := c.bgpReturnRoutes(spec)
	forwardingObserved, forwardingEnabled, forwardingObservedAt := c.discoverySelfForwardingState(res.Metadata.Name)
	poolStatus := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", res.Metadata.Name)
	rebalanceRequest := captureRebalanceRequestFromStatus(poolStatus)
	forceRebalance := rebalanceRequest.Pending()
	observedStaleSince := observedSelfStaleCaptureSinceFromStatus(poolStatus)
	ownershipDecisions, ownershipErr := resolveAddressOwnership(ownershipResolverInput{
		PoolName:            res.Metadata.Name,
		SelfNode:            selfNode,
		Spec:                spec,
		Events:              events,
		Status:              poolStatus,
		ActionJournal:       actionJournal,
		PreviousPlans:       previousActionPlans,
		InstalledNextHops:   installedNextHops,
		BGPHomeOwnerNodes:   bgpHomeOwnerNodes,
		BGPReturnRoutes:     bgpReturnRoutes,
		BGPLiveNodes:        bgpLiveNodesFromMarkers(self, members, livenessMarkers, livenessMarkersObserved),
		BGPLivenessObserved: livenessMarkersObserved,
		Now:                 now,
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
		ObservedStaleSince:   observedStaleSince,
		SuppressDeprovision:  c.SuppressProviderDeprovision,
		LivenessMarkers:      livenessMarkers,
		ForceRebalance:       forceRebalance,
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
		"bgpCaptureElection":                       bgpCaptureElectionStatus(delivery.Placement),
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
		"providerActionFailedCount":                0,
		"providerActionFailedAt":                   "",
		"providerActionSupersededFailureAddresses": nil,
		"providerActionSupersededFailureCount":     0,
		"providerActionSupersededFailureAt":        "",
		"providerActionSupersededFailureReason":    "",
		"observedSelfStaleCaptures":                observedSelfStaleCaptureStatus(ownershipDecisions, selfNode, observedStaleSince, now),
	}
	for key, value := range bgpSeizeHoldDownStatus(delivery.Placement) {
		status[key] = value
	}
	if delivery.Distribution != nil {
		status["captureDistributionMode"] = "distributed"
		status["captureDistributionNodeCounts"] = delivery.Distribution.NodeCounts
		status["captureDistributionTargetPerNode"] = delivery.Distribution.Target
		selfCount := 0
		if c, ok := delivery.Distribution.NodeCounts[selfNode]; ok {
			selfCount = c
		}
		status["captureDistributionSelfCount"] = selfCount
		status["captureDistributionTotalAssigned"] = len(delivery.Distribution.Assignments)
		status["captureDistributionReasonCounts"] = captureDistributionReasonCounts(delivery.Distribution)
	}
	for key, value := range captureRebalanceStatus(rebalanceRequest, delivery.Distribution, forceRebalance, now) {
		status[key] = value
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
		var lastError string
		var lastFailedAt time.Time
		for addr, rec := range failedActions {
			failedAddrs = append(failedAddrs, addr)
			if rec.ExecutedAt.After(lastFailedAt) {
				lastFailedAt = rec.ExecutedAt
				lastError = rec.Error
			}
		}
		sort.Strings(failedAddrs)
		status["providerActionError"] = lastError
		status["providerActionFailedAddresses"] = failedAddrs
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
	status["phase"] = mobilityPoolResourcePhase(status)
	return c.savePlannerStatus(res.Metadata.Name, status)
}

func mobilityPoolResourcePhase(status map[string]any) string {
	switch strings.TrimSpace(fmt.Sprint(status["providerActionPhase"])) {
	case "Failed":
		return "Failed"
	case "Blocked":
		return "Degraded"
	}
	switch strings.TrimSpace(fmt.Sprint(status["ownershipResolverPhase"])) {
	case "Conflict":
		return "Degraded"
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

type captureRebalanceRequest struct {
	ID        string
	At        string
	By        string
	Reason    string
	AppliedID string
	AppliedAt string
}

func (r captureRebalanceRequest) Pending() bool {
	return strings.TrimSpace(r.ID) != "" && strings.TrimSpace(r.ID) != strings.TrimSpace(r.AppliedID)
}

func captureRebalanceRequestFromStatus(status map[string]any) captureRebalanceRequest {
	return captureRebalanceRequest{
		ID:        statusString(status["captureRebalanceRequestID"]),
		At:        statusString(status["captureRebalanceRequestedAt"]),
		By:        statusString(status["captureRebalanceRequestedBy"]),
		Reason:    statusString(status["captureRebalanceReason"]),
		AppliedID: statusString(status["captureRebalanceAppliedRequestID"]),
		AppliedAt: statusString(status["captureRebalanceAppliedAt"]),
	}
}

func captureRebalanceStatus(request captureRebalanceRequest, dist *captureDistribution, force bool, now time.Time) map[string]any {
	out := map[string]any{
		"captureRebalanceRequestID":        request.ID,
		"captureRebalanceRequestedAt":      request.At,
		"captureRebalanceRequestedBy":      request.By,
		"captureRebalanceReason":           request.Reason,
		"captureRebalanceAppliedRequestID": request.AppliedID,
		"captureRebalanceAppliedAt":        request.AppliedAt,
	}
	switch {
	case force && dist != nil:
		out["captureRebalancePhase"] = "Applied"
		out["captureRebalanceAppliedRequestID"] = request.ID
		out["captureRebalanceAppliedAt"] = now.UTC().Format(time.RFC3339Nano)
		out["captureRebalanceAppliedNodeCounts"] = dist.NodeCounts
		out["captureRebalanceAppliedReasonCounts"] = captureDistributionReasonCounts(dist)
	case request.Pending():
		out["captureRebalancePhase"] = "Pending"
	case strings.TrimSpace(request.ID) != "" && strings.TrimSpace(request.ID) == strings.TrimSpace(request.AppliedID):
		out["captureRebalancePhase"] = "Applied"
	default:
		out["captureRebalancePhase"] = "Idle"
	}
	return out
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
	providerHomeAdvertisers := selectedProviderInventoryHomeOwnerFacts(providerInventoryHomeOwnerFactSets(poolName, spec, events, now), members)
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
			if sourceType == providerDiscoverySource {
				selected, ok := providerHomeAdvertisers[address]
				if !ok || strings.TrimSpace(ev.SourceNode) != strings.TrimSpace(selected.NodeRef) {
					continue
				}
			}
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

func bgpPeerSessionsByNodeFromStatus(router *api.Router, store interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}) map[string]bgpPeerSessionState {
	out := map[string]bgpPeerSessionState{}
	if router == nil || store == nil {
		return out
	}
	peerAddresses := bgpPeerAddressesByNode(router, store)
	if len(peerAddresses) == 0 {
		return out
	}
	peerStatus, bgpObserved := bgpPeerStatusByAddress(router, store)
	for nodeRef, address := range peerAddresses {
		if strings.TrimSpace(nodeRef) == "" || strings.TrimSpace(address) == "" {
			continue
		}
		session := bgpPeerSessionState{Address: address}
		if peer, ok := peerStatus[address]; ok {
			session.Observed = true
			session.Established = peer.Established || strings.EqualFold(strings.TrimSpace(peer.State), "Established")
		} else if bgpObserved {
			session.Observed = true
		}
		out[nodeRef] = session
		canonical := canonicalNodeIdentity(nodeRef)
		if canonical != "" {
			out[canonical] = session
		}
	}
	return out
}

func bgpPeerAddressesByNode(router *api.Router, store interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		switch {
		case resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMTransportProfile" && store != nil:
			status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", resource.Metadata.Name)
			for _, peer := range statusMapSlice(status["peers"]) {
				rememberBGPPeerAddress(out, peer["nodeRef"], peer["remoteInner"])
			}
		case resource.APIVersion == api.NetAPIVersion && resource.Kind == "BGPPeer":
			nodeRef := strings.TrimSpace(resource.Metadata.Annotations["mobility.routerd.net/peer-node"])
			if nodeRef == "" {
				continue
			}
			spec, err := resource.BGPPeerSpec()
			if err != nil || len(spec.Peers) == 0 {
				continue
			}
			rememberBGPPeerAddress(out, nodeRef, spec.Peers[0])
		}
	}
	return out
}

func rememberBGPPeerAddress(out map[string]string, nodeRef, address string) {
	nodeRef = strings.TrimSpace(nodeRef)
	address = normalizeBGPNeighborAddress(address)
	if nodeRef == "" || address == "" {
		return
	}
	if _, exists := out[nodeRef]; !exists {
		out[nodeRef] = address
	}
	canonical := canonicalNodeIdentity(nodeRef)
	if canonical != "" {
		if _, exists := out[canonical]; !exists {
			out[canonical] = address
		}
	}
}

func bgpPeerStatusByAddress(router *api.Router, store interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}) (map[string]bgpstate.Peer, bool) {
	out := map[string]bgpstate.Peer{}
	observed := false
	if router == nil || store == nil {
		return out, observed
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		raw, ok := status["peers"]
		if !ok {
			continue
		}
		observed = true
		for _, peer := range bgpStatusPeersValue(raw) {
			address := normalizeBGPNeighborAddress(peer.Address)
			if address != "" {
				peer.Address = address
				out[address] = peer
			}
		}
	}
	return out, observed
}

func bgpStatusPeersValue(value any) []bgpstate.Peer {
	switch typed := value.(type) {
	case []bgpstate.Peer:
		return append([]bgpstate.Peer(nil), typed...)
	case []map[string]any:
		out := make([]bgpstate.Peer, 0, len(typed))
		for _, item := range typed {
			out = append(out, bgpStatusPeerFromMap(item))
		}
		return out
	case []map[string]string:
		out := make([]bgpstate.Peer, 0, len(typed))
		for _, item := range typed {
			out = append(out, bgpStatusPeerFromStringMap(item))
		}
		return out
	case []any:
		out := make([]bgpstate.Peer, 0, len(typed))
		for _, raw := range typed {
			switch item := raw.(type) {
			case bgpstate.Peer:
				out = append(out, item)
			case map[string]any:
				out = append(out, bgpStatusPeerFromMap(item))
			case map[string]string:
				out = append(out, bgpStatusPeerFromStringMap(item))
			}
		}
		return out
	default:
		return nil
	}
}

func bgpStatusPeerFromMap(item map[string]any) bgpstate.Peer {
	established, _ := statusBool(item["established"])
	return bgpstate.Peer{
		Address:     normalizeBGPNeighborAddress(statusString(item["address"])),
		State:       statusString(item["state"]),
		Established: established,
	}
}

func bgpStatusPeerFromStringMap(item map[string]string) bgpstate.Peer {
	established, _ := statusBool(item["established"])
	return bgpstate.Peer{
		Address:     normalizeBGPNeighborAddress(item["address"]),
		State:       strings.TrimSpace(item["state"]),
		Established: established,
	}
}

func normalizeBGPNeighborAddress(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.String()
	}
	return value
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

type bgpPeerSessionState struct {
	Address     string
	Observed    bool
	Established bool
}

func evaluateBGPCapturePlacement(self memberPlanInfo, members map[string]memberPlanInfo, livenessMarkers map[string]string, livenessMarkersObserved bool, observedHolderNode string, peerSessions map[string]bgpPeerSessionState) PlacementDecision {
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
	if activePeer := peerSessionForNode(peerSessions, active.NodeRef); activePeer.Observed {
		placement.ActivePeerAddress = activePeer.Address
		placement.ActivePeerObserved = true
		placement.ActivePeerEstablished = activePeer.Established
		if activePeer.Established {
			placement.Reason = strings.TrimSpace(firstNonEmpty(placement.Reason, fmt.Sprintf("placement group %q active %q has an established BGP peer session", placement.Group, active.NodeRef)))
			return placement
		}
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
		ActivePeerAddress:     placement.ActivePeerAddress,
		ActivePeerObserved:    placement.ActivePeerObserved,
		ActivePeerEstablished: placement.ActivePeerEstablished,
	}
}

func peerSessionForNode(sessions map[string]bgpPeerSessionState, nodeRef string) bgpPeerSessionState {
	if len(sessions) == 0 {
		return bgpPeerSessionState{}
	}
	if session, ok := sessions[strings.TrimSpace(nodeRef)]; ok {
		return session
	}
	if session, ok := sessions[canonicalNodeIdentity(nodeRef)]; ok {
		return session
	}
	return bgpPeerSessionState{}
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
	if decision.ActivePeerAddress != "" {
		status["activePeerAddress"] = decision.ActivePeerAddress
		status["activePeerObserved"] = decision.ActivePeerObserved
		status["activePeerEstablished"] = decision.ActivePeerEstablished
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
		"bgpSeizeHoldDownActive": false,
		"bgpSeizeHoldDownKey":    "",
		"bgpSeizeHoldDownSince":  "",
		"bgpSeizeHoldDownUntil":  "",
	}
	if decision.SeizeHoldDownKey == "" {
		return status
	}
	status["bgpSeizeHoldDownActive"] = decision.SeizeHoldDown
	status["bgpSeizeHoldDownKey"] = decision.SeizeHoldDownKey
	status["bgpSeizeHoldDownSince"] = decision.SeizeHoldDownSince.Format(time.RFC3339Nano)
	status["bgpSeizeHoldDownUntil"] = decision.SeizeHoldDownUntil.Format(time.RFC3339Nano)
	return status
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
		if decision.Class != ownershipClassStaleCapture || strings.TrimSpace(decision.SuppressionReason) != "self-captured-secondary" {
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
	return !tr.assign
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
	case !tr.assign:
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

func interpretProviderCaptureAssignFailures(actions []routerstate.ActionExecutionRecord, observedSelfCaptures map[string]bool) providerCaptureAssignFailureInterpretation {
	latestFailed := latestFailedProviderActions(actions)
	out := providerCaptureAssignFailureInterpretation{
		Active:     map[string]routerstate.ActionExecutionRecord{},
		Superseded: map[string]routerstate.ActionExecutionRecord{},
	}
	for addr, rec := range latestFailed {
		if observedSelfCaptures[normalizeAddressString(addr)] {
			out.Superseded[addr] = rec
			continue
		}
		out.Active[addr] = rec
	}
	return out
}
