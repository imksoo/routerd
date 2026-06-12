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
	"github.com/imksoo/routerd/pkg/sam"
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
	bgpMobilityCommunityFailover       = "64512:120"

	bgpPathSigParam             = "mobilityPathSig"
	bgpTrapLastSeenAtParam      = "mobilityTrapLastSeenAt"
	bgpTrapTransitionParam      = "mobilityProviderTransition"
	bgpTrapRIBMissingHold       = 2 * time.Minute
	bgpSeizeLivenessMissingHold = 30 * time.Second
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
			_ = c.savePlannerStatus(res.Metadata.Name, withSAMConvergenceBlocked(map[string]any{
				"plannerPhase":  "Degraded",
				"plannerReason": err.Error(),
				"plannedAt":     now.Format(time.RFC3339Nano),
			}, err.Error(), now))
			continue
		}
		if mobilityDeliveryMode(spec) != "bgp" {
			reason := fmt.Sprintf("deliveryPolicy.mode=%s is no longer supported; use bgp", mobilityDeliveryMode(spec))
			_ = c.savePlannerStatus(res.Metadata.Name, withSAMConvergenceBlocked(map[string]any{
				"plannerPhase":  "Degraded",
				"plannerReason": reason,
				"plannedAt":     now.Format(time.RFC3339Nano),
				"deliveryMode":  mobilityDeliveryMode(spec),
			}, reason, now))
			continue
		}
		memberSetStatus := map[string]any{"phase": "Disabled"}
		if spec.PublishMemberSet {
			source := MobilityMemberSetDynamicSource(res.Metadata.Name)
			status, err := c.upsertMobilityMemberSetPart(res, spec, source, now)
			if err != nil {
				_ = c.savePlannerStatus(res.Metadata.Name, withSAMConvergenceBlocked(map[string]any{
					"plannerPhase":  "Degraded",
					"plannerReason": err.Error(),
					"memberSet":     status,
					"plannedAt":     now.Format(time.RFC3339Nano),
				}, err.Error(), now))
				continue
			}
			memberSetStatus = status
		}
		if err := c.reconcileBGPDelivery(ctx, res, spec, memberSetStatus, now); err != nil {
			_ = c.savePlannerStatus(res.Metadata.Name, withSAMConvergenceBlocked(map[string]any{
				"plannerPhase":  "Degraded",
				"plannerReason": err.Error(),
				"memberSet":     memberSetStatus,
				"plannedAt":     now.Format(time.RFC3339Nano),
			}, err.Error(), now))
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
		return c.savePlannerStatus(res.Metadata.Name, withSAMConvergenceBlocked(map[string]any{
			"plannerPhase":        "Pending",
			"plannerReason":       "membersFrom source is not resolved",
			"selfNode":            selfNode,
			"deliveryMode":        "bgp",
			"pendingSources":      resolved.PendingSources,
			"membersFrom":         mobilityMembersFromStatusMaps(resolved.MembersFrom),
			"resolvedMemberCount": resolved.ResolvedMemberCount,
			"memberSet":           memberSetStatus,
			"plannedAt":           now.Format(time.RFC3339Nano),
		}, "membersFrom source is not resolved", now))
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
	discoverySelfIPsAt := c.discoveryLastScanAt(res.Metadata.Name)
	discoverySelfCaptures, _ := c.discoverySelfCapturedAddressSet(res.Metadata.Name, spec)
	livenessMarkers, livenessMarkersObserved := c.bgpLivenessMarkers()
	ownerPlacement := c.applyBGPCaptureSeizeHoldDown(res.Metadata.Name, evaluateBGPCapturePlacement(self, members, livenessMarkers, livenessMarkersObserved), now)
	actionJournal, err := c.Store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return fmt.Errorf("list action journal: %w", err)
	}
	failedActions := latestFailedProviderActions(actionJournal)
	previousActionPlans, err := c.previousGeneratedActionPlans(res.Metadata.Name, selfNode)
	if err != nil {
		return err
	}
	installedNextHops, bgpRIBObserved := c.bgpInstalledNextHops()
	acceptedBGPPathPrefixes := c.bgpAcceptedPathPrefixes()
	invalidLivenessMarkerPrefixes := invalidLivenessMarkerPoolPrefixes(livenessMarkers, spec.Prefix)
	forwardingObserved, forwardingEnabled, forwardingObservedAt := c.discoverySelfForwardingState(res.Metadata.Name)
	currentStatus := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", res.Metadata.Name)
	ownershipDecisions, ownershipErr := resolveAddressOwnership(ownershipResolverInput{
		PoolName:          res.Metadata.Name,
		SelfNode:          selfNode,
		Spec:              spec,
		Events:            events,
		Status:            currentStatus,
		ActionJournal:     actionJournal,
		PreviousPlans:     previousActionPlans,
		InstalledNextHops: installedNextHops,
		Now:               now,
	})
	if ownershipErr != nil {
		return ownershipErr
	}
	failedActions = filterFailedProviderActionsByConfirmedCaptures(failedActions, ownershipDecisions)
	delivery, err := planBGPMobilityDelivery(bgpDeliveryPlannerInput{
		PoolName:             res.Metadata.Name,
		Source:               source,
		Self:                 self,
		Members:              members,
		Spec:                 spec,
		Decisions:            ownershipDecisions,
		Placement:            ownerPlacement,
		InstalledNextHops:    installedNextHops,
		RIBObserved:          bgpRIBObserved,
		PreviousPlans:        previousActionPlans,
		Profiles:             cloudProviderProfiles(c.Router),
		ActionJournal:        actionJournal,
		ObservedSelfIPs:      discoverySelfIPs,
		ObservedSelfCaptures: discoverySelfCaptures,
		ObservedSelfIPsOK:    discoverySelfIPsObserved,
		ObservedSelfIPsAt:    discoverySelfIPsAt,
		ForwardingObserved:   forwardingObserved,
		ForwardingEnabled:    forwardingEnabled,
		ForwardingObservedAt: forwardingObservedAt,
		SuppressDeprovision:  c.SuppressProviderDeprovision,
		Now:                  now,
	})
	if err != nil {
		return err
	}
	desired := append([]bgpdaemon.AppliedPath(nil), delivery.Paths...)
	if !self.MaintenanceDrain {
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
		"plannerPhase":                      "BGPPlanned",
		"plannerReason":                     "deliveryPolicy.mode=bgp",
		"selfNode":                          selfNode,
		"dynamicSource":                     source,
		"deliveryMode":                      "bgp",
		"bgpPathSource":                     source,
		"generatedBGPPaths":                 len(desired),
		"generatedSeizedBGPPaths":           delivery.SeizedPaths,
		"generatedProviderCapturedBGPPaths": delivery.ProviderCapturedPaths,
		"bgpRIBObserved":                    bgpRIBObserved,
		"bgpCaptureElection":                bgpCaptureElectionStatus(delivery.Placement),
		"generatedBGPTraps":                 len(delivery.CaptureCandidates),
		"generatedClaims":                   0,
		"generatedActions":                  len(actionPlans),
		"acceptedBGPPathPrefixes":           sortedMapKeys(acceptedBGPPathPrefixes),
		"invalidLivenessMarkerPrefixes":     invalidLivenessMarkerPrefixes,
		"membersFrom":                       mobilityMembersFromStatusMaps(resolved.MembersFrom),
		"resolvedMemberCount":               len(spec.Members),
		"pendingSources":                    resolved.PendingSources,
		"memberSet":                         memberSetStatus,
		"selfCaptureResolved":               selfCaptureResolved,
		"plannedAt":                         now.Format(time.RFC3339Nano),
		"operatorIntent":                    "MobilityPool",
		"derivedConfigKinds":                []string{"BGPPath"},
		"providerActionPhase":               "OK",
		"providerActionError":               "",
		"providerActionFailedAddresses":     nil,
		"providerActionFailedCount":         0,
		"providerActionFailedAt":            "",
	}
	for key, value := range bgpSeizeHoldDownStatus(delivery.Placement) {
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
	for key, value := range ownershipResolverStatus(ownershipDecisions) {
		status[key] = value
	}
	convergenceAddresses := captureConvergenceAddresses(delivery)
	osCaptureExpected := mobilityMemberExpectsLocalOSCapture(self, spec) &&
		(len(delivery.CaptureCandidates) > 0 || len(actionPlans) > 0 || delivery.ProviderCapturedPaths > 0) &&
		!c.providerSecondaryOSCaptureExplicitlyNotExpected(res.Metadata.Name, convergenceAddresses)
	for key, value := range samConvergenceStatusFields(samConvergenceInput{
		Status:                        status,
		DesiredBGPPaths:               delivery.Paths,
		InvalidLivenessMarkerPrefixes: invalidLivenessMarkerPrefixes,
		InstalledNextHops:             installedNextHops,
		AcceptedBGPPathPrefixes:       acceptedBGPPathPrefixes,
		BGPRIBObserved:                bgpRIBObserved,
		SelfCaptureResolved:           selfCaptureResolved,
		SelfCaptureReason:             selfCaptureReason,
		CaptureCandidates:             len(delivery.CaptureCandidates),
		GeneratedActions:              len(actionPlans),
		ProviderCapturedPaths:         delivery.ProviderCapturedPaths,
		ProviderDiscoveryRequired:     strings.EqualFold(strings.TrimSpace(self.OwnershipDiscovery.Mode), "provider-private-ip"),
		ProviderDiscoveryPhase:        statusMapString(currentStatus, "discoveryPhase"),
		ProviderDiscoveryReason:       statusMapString(currentStatus, "discoveryReason"),
		OSCaptureExpected:             osCaptureExpected,
		OSCaptureObserved:             c.providerSecondaryOSCaptureReflected(res.Metadata.Name, convergenceAddresses),
		ForwardingObserved:            forwardingObserved,
		ForwardingEnabled:             forwardingEnabled,
		ObservedAt:                    now,
	}) {
		status[key] = value
	}
	status["phase"] = status["samConvergencePhase"]
	return c.savePlannerStatus(res.Metadata.Name, status)
}

type samConvergenceInput struct {
	Status                        map[string]any
	DesiredBGPPaths               []bgpdaemon.AppliedPath
	InvalidLivenessMarkerPrefixes []string
	InstalledNextHops             map[string][]string
	AcceptedBGPPathPrefixes       map[string]bool
	BGPRIBObserved                bool
	SelfCaptureResolved           bool
	SelfCaptureReason             string
	CaptureCandidates             int
	GeneratedActions              int
	ProviderCapturedPaths         int
	ProviderDiscoveryRequired     bool
	ProviderDiscoveryPhase        string
	ProviderDiscoveryReason       string
	OSCaptureExpected             bool
	OSCaptureObserved             bool
	ForwardingObserved            bool
	ForwardingEnabled             bool
	ObservedAt                    time.Time
}

func mobilityMemberExpectsLocalOSCapture(member memberPlanInfo, spec api.MobilityPoolSpec) bool {
	deliveryMode := strings.TrimSpace(member.Delivery.Mode)
	if deliveryMode == "" {
		deliveryMode = strings.TrimSpace(spec.DeliveryPolicy.Mode)
	}
	return sam.ProviderSecondaryInstallsLocalOSAddress(api.RemoteAddressClaimSpec{
		Capture:  member.Capture,
		Delivery: api.AddressDelivery{Mode: deliveryMode},
	})
}

func withSAMConvergenceBlocked(status map[string]any, reason string, observedAt time.Time) map[string]any {
	fields := sam.SAMConvergenceStatus{
		CloudClaimPhase:        sam.CloudClaimUnknown,
		OSCapturePhase:         sam.OSCaptureUnknown,
		ForwardingPhase:        sam.ForwardingUnknown,
		FIBConvergencePhase:    sam.FIBConvergenceUnknown,
		AdvertisementGatePhase: sam.AdvertisementGateBlocked,
		SAMConvergencePhase:    sam.SAMConvergenceDegraded,
		BlockingReasons:        cleanStrings([]string{reason}),
		LastObservedAt:         observedAt.Format(time.RFC3339Nano),
	}.StatusFields()
	for key, value := range fields {
		status[key] = value
	}
	return status
}

func samConvergenceStatusFields(in samConvergenceInput) map[string]any {
	blocking := []string{}
	ownershipPhase := statusMapString(in.Status, "ownershipResolverPhase")
	providerActionPhase := statusMapString(in.Status, "providerActionPhase")
	selfCaptureBlocked := !in.SelfCaptureResolved && strings.TrimSpace(in.SelfCaptureReason) != ""
	if selfCaptureBlocked {
		blocking = append(blocking, in.SelfCaptureReason)
	}
	providerDiscoveryBlocked := false
	if in.ProviderDiscoveryRequired {
		switch strings.TrimSpace(in.ProviderDiscoveryPhase) {
		case "Observed", "Standby":
		default:
			providerDiscoveryBlocked = true
			reason := strings.TrimSpace(in.ProviderDiscoveryReason)
			if reason == "" {
				phase := strings.TrimSpace(in.ProviderDiscoveryPhase)
				if phase == "" {
					phase = "missing"
				}
				reason = "provider private-IP discovery phase is " + phase
			}
			blocking = append(blocking, reason)
		}
	}

	cloudClaimPhase := sam.CloudClaimNotApplicable
	confirmedProviderCaptures := confirmedProviderCaptureCount(in.Status)
	captureExpected := in.CaptureCandidates > 0 || in.GeneratedActions > 0 || in.ProviderCapturedPaths > 0 || confirmedProviderCaptures > 0
	switch {
	case in.ProviderCapturedPaths > 0 || confirmedProviderCaptures > 0:
		cloudClaimPhase = sam.CloudClaimClaimed
	case strings.EqualFold(providerActionPhase, "Failed"):
		cloudClaimPhase = sam.CloudClaimFailed
		blocking = append(blocking, "provider action failed")
	case captureExpected:
		cloudClaimPhase = sam.CloudClaimPending
		blocking = append(blocking, "provider cloud claim is not confirmed")
	}

	osCapturePhase := sam.OSCaptureNotApplicable
	forwardingPhase := sam.ForwardingNotApplicable
	if captureExpected {
		if in.OSCaptureExpected {
			if in.OSCaptureObserved {
				osCapturePhase = sam.OSCaptureReflected
			} else {
				osCapturePhase = sam.OSCaptureMissing
				blocking = append(blocking, "OS capture is not reflected on the active router")
			}
		}
		if in.ForwardingObserved {
			if in.ForwardingEnabled {
				forwardingPhase = sam.ForwardingReady
			} else {
				forwardingPhase = sam.ForwardingDisabled
				blocking = append(blocking, "forwarding is disabled")
			}
		} else {
			forwardingPhase = sam.ForwardingUnknown
			blocking = append(blocking, "forwarding state is not observable")
		}
	}

	fibPhase := sam.FIBConvergenceUnknown
	if !in.BGPRIBObserved {
		blocking = append(blocking, "BGP installed next-hop evidence is not observable")
	} else {
		missing := missingDesiredBGPPathPrefixes(in.DesiredBGPPaths, in.InstalledNextHops, in.AcceptedBGPPathPrefixes)
		missing = cleanStrings(missing)
		invalidLivenessMarkers := cleanStrings(in.InvalidLivenessMarkerPrefixes)
		if len(invalidLivenessMarkers) > 0 {
			fibPhase = sam.FIBConvergenceMissingRoute
			blocking = append(blocking, "mis-tagged BGP liveness marker prefixes inside MobilityPool: "+strings.Join(invalidLivenessMarkers, ","))
		} else if len(missing) == 0 {
			fibPhase = sam.FIBConvergenceReady
		} else {
			fibPhase = sam.FIBConvergenceMissingRoute
			blocking = append(blocking, "missing installed BGP routes in BGPRouter.installedNextHops: "+strings.Join(missing, ","))
		}
	}

	if ownershipPhase == "" {
		blocking = append(blocking, "ownership resolver status is missing")
	} else if !strings.EqualFold(ownershipPhase, "Resolved") {
		blocking = append(blocking, "ownership resolver phase is "+ownershipPhase)
	}

	gatePhase := sam.AdvertisementGateBlocked
	samPhase := sam.SAMConvergenceDegraded
	switch {
	case strings.EqualFold(ownershipPhase, "Conflict") || cloudClaimPhase == sam.CloudClaimFailed:
		samPhase = sam.SAMConvergenceFailed
	case !selfCaptureBlocked &&
		!providerDiscoveryBlocked &&
		strings.EqualFold(ownershipPhase, "Resolved") &&
		(cloudClaimPhase == sam.CloudClaimNotApplicable || cloudClaimPhase == sam.CloudClaimClaimed) &&
		(osCapturePhase == sam.OSCaptureNotApplicable || osCapturePhase == sam.OSCaptureReflected || osCapturePhase == sam.OSCaptureForwardingReady) &&
		(forwardingPhase == sam.ForwardingNotApplicable || forwardingPhase == sam.ForwardingReady) &&
		fibPhase == sam.FIBConvergenceReady:
		gatePhase = sam.AdvertisementGateAllowed
		samPhase = sam.SAMConvergenceReady
		blocking = nil
	}

	return sam.SAMConvergenceStatus{
		CloudClaimPhase:        cloudClaimPhase,
		OSCapturePhase:         osCapturePhase,
		ForwardingPhase:        forwardingPhase,
		FIBConvergencePhase:    fibPhase,
		AdvertisementGatePhase: gatePhase,
		SAMConvergencePhase:    samPhase,
		SplitBrainDetected:     false,
		StaleEpochDetected:     false,
		BlockingReasons:        cleanStrings(blocking),
		LastObservedAt:         in.ObservedAt.Format(time.RFC3339Nano),
	}.StatusFields()
}

func confirmedProviderCaptureCount(status map[string]any) int {
	count := 0
	switch raw := status["ownershipResolverDecisions"].(type) {
	case []map[string]any:
		for _, decision := range raw {
			if strings.TrimSpace(fmt.Sprint(decision["captureState"])) == captureStateConfirmed {
				count++
			}
		}
	case []any:
		for _, item := range raw {
			decision, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(fmt.Sprint(decision["captureState"])) == captureStateConfirmed {
				count++
			}
		}
	}
	return count
}

func missingDesiredBGPPathPrefixes(paths []bgpdaemon.AppliedPath, installed map[string][]string, accepted map[string]bool) []string {
	var missing []string
	for _, path := range paths {
		prefix := strings.TrimSpace(path.Prefix)
		if prefix == "" {
			continue
		}
		if len(installed[prefix]) > 0 || accepted[prefix] {
			continue
		}
		missing = append(missing, prefix)
	}
	sort.Strings(missing)
	return missing
}

func invalidLivenessMarkerPoolPrefixes(markers map[string]string, pool string) []string {
	poolPrefix, err := netip.ParsePrefix(strings.TrimSpace(pool))
	if err != nil || !poolPrefix.Addr().Is4() {
		return nil
	}
	poolPrefix = poolPrefix.Masked()
	seen := map[string]bool{}
	var out []string
	for _, raw := range markers {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			continue
		}
		prefix = prefix.Masked()
		if !poolPrefix.Contains(prefix.Addr()) {
			continue
		}
		value := prefix.String()
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func statusMapString(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	value, ok := status[key]
	if !ok || value == nil {
		return ""
	}
	valueString := strings.TrimSpace(fmt.Sprint(value))
	if valueString == "<nil>" {
		return ""
	}
	return valueString
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
		addr, err := netip.ParseAddr(strings.TrimSpace(spec.Listen.Address))
		if err != nil || !addr.Is4() {
			return "", false
		}
		return netip.PrefixFrom(addr, 32).String(), true
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
				if discoveryOwnedObserved && len(discoveryOwnedAddresses) == 0 ||
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
	out := map[string]providerInventoryOwnerFact{}
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
		current, found := out[address]
		if !found || fact.ObservedAt.After(current.ObservedAt) || fact.ObservedAt.Equal(current.ObservedAt) && fact.NodeRef < current.NodeRef {
			out[address] = fact
		}
	}
	return out
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

func (c Controller) discoveryLastScanAt(poolName string) time.Time {
	if c.Store == nil {
		return time.Time{}
	}
	status := c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName)
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(fmt.Sprint(status["discoveryLastScanAt"])))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
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

func (c Controller) providerSecondaryOSCaptureReflected(poolName string, addresses []string) bool {
	if c.Store == nil || len(addresses) == 0 {
		return false
	}
	for _, address := range cleanStrings(addresses) {
		normalizedAddress := normalizeAddressString(address)
		status := c.Store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", bgpMobilityRemoteClaimName(poolName, address))
		raw, ok := status["captureOSAddressPresence"].(map[string]any)
		if !ok || !statusMapBool(raw, "enforced") {
			return false
		}
		observed := normalizeAddressString(strings.TrimSpace(fmt.Sprint(raw["address"])))
		if observed != "" && observed != normalizedAddress {
			return false
		}
	}
	return true
}

func (c Controller) providerSecondaryOSCaptureExplicitlyNotExpected(poolName string, addresses []string) bool {
	if c.Store == nil || len(addresses) == 0 {
		return false
	}
	for _, address := range cleanStrings(addresses) {
		normalizedAddress := normalizeAddressString(address)
		status := c.Store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", bgpMobilityRemoteClaimName(poolName, address))
		raw, ok := status["captureOSAddressAbsence"].(map[string]any)
		if !ok || statusMapBool(raw, "localOSAddressExpected") {
			return false
		}
		observed := normalizeAddressString(strings.TrimSpace(fmt.Sprint(raw["address"])))
		if observed != "" && observed != normalizedAddress {
			return false
		}
	}
	return true
}

func captureConvergenceAddresses(delivery bgpDeliveryPlannerResult) []string {
	seen := map[string]bool{}
	for address := range delivery.CaptureCandidates {
		if normalized := normalizeAddressString(address); normalized != "" {
			seen[normalized] = true
		}
	}
	for _, address := range delivery.ProviderCapturedAddrs {
		if normalized := normalizeAddressString(address); normalized != "" {
			seen[normalized] = true
		}
	}
	return mapKeysSorted(seen)
}

func bgpMobilityRemoteClaimName(poolName, address string) string {
	return "bgp-" + mobilitySafeResourceName(poolName) + "-" + mobilitySafeResourceName(strings.TrimSuffix(address, "/32"))
}

func mobilitySafeResourceName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "resource"
	}
	return out
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

func statusMapBool(status map[string]any, key string) bool {
	if status == nil {
		return false
	}
	value, ok := status[key]
	if !ok {
		return false
	}
	result, ok := statusBool(value)
	return ok && result
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

func bgpProviderActionPlans(poolName, selfNode string, spec api.MobilityPoolSpec, desiredTrapAddresses map[string]bgpTrapCandidate, previousPlans []dynamicconfig.ActionPlan, profiles map[string]api.CloudProviderProfileSpec, actionJournal []routerstate.ActionExecutionRecord, observedSelfIPs map[string]bool, observedSelfIPsOK bool, observedSelfIPsAt time.Time, forwardingObserved, forwardingEnabled bool, forwardingObservedAt time.Time, suppressDeprovision bool, now time.Time) ([]dynamicconfig.ActionPlan, error) {
	members := plannerMembers(spec.Members)
	self, ok := members[strings.TrimSpace(selfNode)]
	if !ok {
		return nil, fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, poolName)
	}
	desiredAddresses := map[string]bool{}
	desiredProviderNICs := map[string]bool{}
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
				if key := providerNICKey("", self.Capture.ProviderRef, providerCaptureRefFromCapture(self.Capture, self.CaptureTarget)); key != "" {
					desiredProviderNICs[key] = true
				}
				continue
			}
			seize := candidate.Seize || shouldAllowBGPTrapReassignment(self, address, previousPlans, actionJournal, observedSelfIPs, observedSelfIPsOK)
			generated, err := providerActionPlans(poolName, profile, self.Capture, self.CaptureTarget, address, forwardingSeen, seize)
			if err != nil {
				return nil, err
			}
			if len(generated) > 0 {
				strategy := strings.TrimSpace(generated[0].Target["captureStrategy"])
				if key := providerNICKey("", self.Capture.ProviderRef, providerCaptureTargetRef(strategy, generated[0].Target)); key != "" {
					desiredProviderNICs[key] = true
				}
			}
			generated = dropObservedReadyForwardingPlans(generated, forwardingObserved, forwardingEnabled)
			stampBGPPathFenceActionPlans(generated, address, candidate.PathSig, self.NodeRef, candidate.LastSeenAt)
			stampBGPProviderTransitionFence(generated, self, address, actionJournal, observedSelfIPs, observedSelfIPsOK, observedSelfIPsAt)
			stampForwardingDriftFence(generated, forwardingObserved, forwardingEnabled, forwardingObservedAt)
			plans = append(plans, generated...)
		}
	}
	if !suppressDeprovision {
		forwardingDisabled := map[string]bool{}
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
			capture := captureFromActionPlan(self.Capture, previous)
			if capture.Type != "provider-secondary-ip" {
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

			nicKey := providerNICKey("", capture.ProviderRef, providerCaptureRefFromCapture(capture, captureTarget))
			if nicKey == "" || desiredProviderNICs[nicKey] || forwardingDisabled[nicKey] {
				continue
			}
			disable, err := providerForwardingDisableActionPlan(poolName, profile, capture, captureTarget, address)
			if err != nil {
				return nil, err
			}
			disable = stampSingleBGPPathFence(disable, address, bgpPathSigFromActionPlan(previous, address), self.NodeRef)
			plans = append(plans, disable)
			forwardingDisabled[nicKey] = true
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

func (c Controller) bgpAcceptedPathPrefixes() map[string]bool {
	out := map[string]bool{}
	if c.Router == nil || c.Store == nil {
		return out
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := c.Store.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		for prefix := range bgpAcceptedPrefixesStatusValue(status["prefixes"]) {
			out[prefix] = true
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

func bgpAcceptedPrefixesStatusValue(value any) map[string]bool {
	out := map[string]bool{}
	switch typed := value.(type) {
	case []bgpstate.Prefix:
		for _, prefix := range typed {
			addAcceptedPrefix(out, prefix.Prefix)
		}
	case []any:
		for _, item := range typed {
			if prefix, ok := item.(bgpstate.Prefix); ok {
				addAcceptedPrefix(out, prefix.Prefix)
				continue
			}
			rec, ok := item.(map[string]any)
			if !ok {
				continue
			}
			addAcceptedPrefix(out, statusMapString(rec, "prefix"))
		}
	}
	return out
}

func addAcceptedPrefix(out map[string]bool, raw string) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil {
		return
	}
	out[prefix.Masked().String()] = true
}

func sortedMapKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key, ok := range values {
		if ok && strings.TrimSpace(key) != "" {
			out = append(out, strings.TrimSpace(key))
		}
	}
	sort.Strings(out)
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

func evaluateBGPCapturePlacement(self memberPlanInfo, members map[string]memberPlanInfo, livenessMarkers map[string]string, livenessMarkersObserved bool) PlacementDecision {
	placement := evaluatePlacement(self, members)
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

func shouldAllowBGPTrapReassignment(self memberPlanInfo, address string, previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord, observedSelfIPs map[string]bool, observedSelfIPsOK bool) bool {
	address = normalizeAddressString(address)
	if address == "" {
		return false
	}
	latest := latestProviderCaptureTransitions(previousPlans, journal)
	key := providerCaptureTransitionKey(self.Capture.ProviderRef, providerCaptureRefFromCapture(self.Capture, self.CaptureTarget), address)
	tr, ok := latest[key]
	if !ok && observedSelfIPsOK && !observedSelfIPs[address] {
		return true
	}
	if !ok {
		return false
	}
	if tr.assign && observedSelfIPsOK && !observedSelfIPs[address] {
		return true
	}
	if observedSelfIPsOK && !observedSelfIPs[address] {
		return true
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

func stampBGPProviderTransitionFence(plans []dynamicconfig.ActionPlan, self memberPlanInfo, address string, journal []routerstate.ActionExecutionRecord, observedSelfIPs map[string]bool, observedSelfIPsOK bool, observedSelfIPsAt time.Time) {
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
	case observedSelfIPsOK && !observedSelfIPs[address]:
		if tr.succeeded && (observedSelfIPsAt.IsZero() || !observedSelfIPsAt.After(tr.at)) {
			return
		}
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

func dropObservedReadyForwardingPlans(plans []dynamicconfig.ActionPlan, observed, enabled bool) []dynamicconfig.ActionPlan {
	if !observed || !enabled {
		return plans
	}
	out := plans[:0]
	for _, plan := range plans {
		if plan.Action == "ensure-forwarding-enabled" {
			continue
		}
		out = append(out, plan)
	}
	return out
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
		plan.Target["address"] = normalizeAddressString(parts[2])
		plan.Target["providerRef"] = strings.TrimSpace(self.Capture.ProviderRef)
		plan.Target["nicRef"] = strings.TrimSpace(self.Capture.NICRef)
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
	communities := []string{bgpMobilityCommunityOwner}
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
	default:
		communities = append(communities, bgpMobilityCommunitySourceObserved)
	}
	localPref := bgpMobilityLocalPrefBase
	if active {
		localPref = bgpMobilityLocalPref(1)
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

func filterFailedProviderActionsByConfirmedCaptures(failed map[string]routerstate.ActionExecutionRecord, decisions []ownershipDecision) map[string]routerstate.ActionExecutionRecord {
	if len(failed) == 0 {
		return failed
	}
	confirmed := map[string]bool{}
	for _, decision := range decisions {
		if decision.CaptureState != captureStateConfirmed {
			continue
		}
		address := normalizeAddressString(decision.Address)
		if address != "" {
			confirmed[address] = true
		}
	}
	if len(confirmed) == 0 {
		return failed
	}
	filtered := make(map[string]routerstate.ActionExecutionRecord, len(failed))
	for address, record := range failed {
		if confirmed[normalizeAddressString(address)] {
			continue
		}
		filtered[address] = record
	}
	return filtered
}
