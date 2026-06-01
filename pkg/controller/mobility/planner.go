// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
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
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	dynamicGeneration              = int64(1)
	dynamicSourceKind              = "MobilityPool"
	DefaultDeprovisionHoldDuration = 10 * time.Second
	captureTargetAnnotationPrefix  = "mobility.routerd.net/capture-target."
	captureParamKey                = "mobilityCaptureKey"
	captureParamEpoch              = "mobilityCaptureEpoch"
	captureParamHolder             = "mobilityCaptureHolder"
	ownershipParamPool             = "mobilityOwnershipPool"
	ownershipParamAddress          = "mobilityOwnershipAddress"
	ownershipParamEpoch            = "mobilityOwnershipEpoch"
	ownershipParamOwner            = "mobilityOwnershipOwner"
)

// PlannerInput is the pure lease-to-dynamic-config planning input for one
// MobilityPool on one routerd node.
type PlannerInput struct {
	PoolName            string
	PoolSpec            api.MobilityPoolSpec
	SelfNode            string
	Now                 time.Time
	Leases              []routerstate.AddressLeaseRecord
	PreviousClaims      []api.Resource
	DeprovisionMarkers  []routerstate.MobilityDeprovisionMarkerRecord
	CaptureEpochs       []routerstate.MobilityCaptureEpochRecord
	OwnershipEpochs     []routerstate.MobilityOwnershipEpochRecord
	PreviousOwnership   []routerstate.MobilityOwnershipEpochRecord
	PreviousActionPlans []dynamicconfig.ActionPlan
	ActionJournal       []routerstate.ActionExecutionRecord
	Liveness            OwnershipLiveness
	ProviderProfiles    map[string]api.CloudProviderProfileSpec
}

// PlannerOutput is the deterministic generated config for one pool x node.
type PlannerOutput struct {
	Part        dynamicconfig.DynamicConfigPart
	Claims      []api.Resource
	ActionPlans []dynamicconfig.ActionPlan
	Placement   PlacementDecision
	Ownership   []routerstate.MobilityOwnershipEpochRecord
}

type PlacementDecision struct {
	Group      string
	Active     bool
	ActiveNode string
	Reason     string
}

func (d PlacementDecision) NoCandidate() bool {
	return d.Group != "" && d.ActiveNode == ""
}

type OwnershipLiveness struct {
	StreamMaxObservedAt time.Time
	LastHeartbeat       map[string]time.Time
	StaleNodes          map[string]bool
}

type memberPlanInfo struct {
	NodeRef           string
	Site              string
	Role              string
	Capture           api.AddressCapture
	CaptureTarget     map[string]string
	Delivery          api.AddressDelivery
	DeliveryTo        []deliveryTargetPlanInfo
	PlacementGroup    string
	PlacementPriority int
	MaintenanceDrain  bool
}

type deliveryTargetPlanInfo struct {
	NodeRef  string
	Site     string
	Role     string
	Delivery api.AddressDelivery
}

// PlanDynamicConfig lowers active remote AddressLease records into the
// DynamicConfigPart consumed by SAM and provider-action import. It is pure: it
// reads no store and performs no provider or OS mutation.
func PlanDynamicConfig(in PlannerInput) (PlannerOutput, error) {
	now := in.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	poolName := strings.TrimSpace(in.PoolName)
	if poolName == "" {
		return PlannerOutput{}, fmt.Errorf("pool name is required")
	}
	selfNode := strings.TrimSpace(in.SelfNode)
	if selfNode == "" {
		return PlannerOutput{}, fmt.Errorf("self node is required")
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(in.PoolSpec.Prefix))
	if err != nil {
		return PlannerOutput{}, fmt.Errorf("parse pool prefix: %w", err)
	}
	prefix = prefix.Masked()
	members := plannerMembers(in.PoolSpec.Members)
	self, ok := members[selfNode]
	if !ok {
		return PlannerOutput{}, fmt.Errorf("self node %q is not a member of MobilityPool/%s", selfNode, poolName)
	}
	placement := evaluatePlacementWithLiveness(self, members, in.PoolSpec.IPOwnershipPolicy, in.Liveness)
	captureEpochs := captureEpochsByKey(in.CaptureEpochs)
	ownershipEpochs := ownershipEpochsByAddress(in.OwnershipEpochs)
	centralizedOwnership := ipOwnershipPolicyCentralized(in.PoolSpec.IPOwnershipPolicy)
	ownershipEpochLocking := ipOwnershipEpochLocking(in.PoolSpec.IPOwnershipPolicy)
	actionOwnershipEpochs := ownershipEpochs
	if !ownershipEpochLocking {
		actionOwnershipEpochs = nil
	}

	claims := []api.Resource{}
	plans := []dynamicconfig.ActionPlan{}
	forwardingSeen := map[string]bool{}
	desiredAddresses := map[string]bool{}
	desiredProviderNICs := map[string]bool{}
	minExpiresAt := time.Time{}
	if placement.Active || centralizedOwnership {
		for _, lease := range sortedLeases(in.Leases) {
			if centralizedOwnership {
				owner, ok := ownershipEpochs[normalizeAddressString(lease.Address)]
				if !ok || owner.OwnerNode != self.NodeRef {
					continue
				}
			}
			address := normalizeAddressString(lease.Address)
			seizeOrigin, seize := seizeOriginOwner(address, self.NodeRef, in.PreviousOwnership, in.Liveness)
			if centralizedOwnership && !seize {
				seize = shouldMaintainSeizeIntent(poolName, address, self.NodeRef, ownershipEpochs[address], in.PreviousActionPlans, in.ActionJournal)
			}
			claim, actionPlans, leaseExpiresAt, ok, err := planLease(poolName, prefix, self, members, lease, in.ProviderProfiles, now, forwardingSeen, captureEpochs, actionOwnershipEpochs, seize)
			if err != nil {
				return PlannerOutput{}, err
			}
			if !ok {
				continue
			}
			if seize {
				enrichAzureSeizeDisplacedTargets(poolName, address, self, members, seizeOrigin, in.ProviderProfiles, ownershipEpochs[address], actionPlans, in.PreviousActionPlans)
			}
			claims = append(claims, claim)
			plans = append(plans, actionPlans...)
			desiredAddresses[claimAddress(claim)] = true
			if key := providerNICKeyFromClaim(claim); key != "" {
				desiredProviderNICs[key] = true
			}
			if !leaseExpiresAt.IsZero() && (minExpiresAt.IsZero() || leaseExpiresAt.Before(minExpiresAt)) {
				minExpiresAt = leaseExpiresAt
			}
		}
	}
	deprovisionPlans, err := providerDeprovisionPlans(poolName, self, in.PreviousClaims, desiredAddresses, desiredProviderNICs, leasesByAddress(in.Leases), in.ProviderProfiles, now, deprovisionHoldDuration(in.PoolSpec), !placement.Active, captureEpochs, actionOwnershipEpochs)
	if err != nil {
		return PlannerOutput{}, err
	}
	markerPlans, err := actionPlansFromMarkers(in.DeprovisionMarkers)
	if err != nil {
		return PlannerOutput{}, err
	}
	markerPlans = filterStaleCaptureEpochPlans(markerPlans, captureEpochs)
	deprovisionPlans = append(deprovisionPlans, markerPlans...)
	deprovisionPlans = dedupeActionPlans(deprovisionPlans)
	if placement.Active || centralizedOwnership {
		deprovisionedAddresses := actionPlanAddresses(deprovisionPlans, "unassign-secondary-ip")
		for _, claim := range carryForwardProviderClaims(in.PreviousClaims, desiredAddresses, deprovisionedAddresses) {
			claims = append(claims, claim)
			desiredAddresses[claimAddress(claim)] = true
			if key := providerNICKeyFromClaim(claim); key != "" {
				desiredProviderNICs[key] = true
			}
		}
		deprovisionPlans = filterForwardingDisablePlans(deprovisionPlans, desiredProviderNICs)
	}
	plans = append(plans, deprovisionPlans...)

	resources := []api.Resource{domainResource(poolName, in.PoolSpec, self)}
	resources = append(resources, claims...)
	source := DynamicSource(poolName, selfNode)
	for i := range resources {
		stampGeneratedResource(&resources[i], source, poolName, selfNode)
	}
	for i := range claims {
		claims[i].Metadata.Annotations = copyStringMap(resources[i+1].Metadata.Annotations)
	}

	expiresAt := now.Add(durationDefault(in.PoolSpec.LeasePolicy.TTL, DefaultLeaseTTL))
	if !minExpiresAt.IsZero() && minExpiresAt.Before(expiresAt) {
		expiresAt = minExpiresAt
	}
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
			Source:      source,
			Generation:  dynamicGeneration,
			ObservedAt:  now,
			ExpiresAt:   expiresAt,
			Resources:   resources,
			Directives:  []dynamicconfig.DynamicConfigDirective{},
			ActionPlans: plans,
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	return PlannerOutput{Part: part, Claims: claims, ActionPlans: plans, Placement: placement, Ownership: in.OwnershipEpochs}, nil
}

func (c Controller) reconcilePlan(res api.Resource, now time.Time) error {
	spec, err := res.MobilityPoolSpec()
	if err != nil {
		return err
	}
	selfNode, err := c.selfNode(spec.GroupRef)
	if err != nil {
		return err
	}
	leases, err := c.Store.ListAddressLeases(res.Metadata.Name, true, now)
	if err != nil {
		return fmt.Errorf("list address leases: %w", err)
	}
	previousClaims, err := c.previousGeneratedClaims(res.Metadata.Name, selfNode)
	if err != nil {
		return err
	}
	previousActionPlans, err := c.previousGeneratedActionPlans(res.Metadata.Name, selfNode)
	if err != nil {
		return err
	}
	source := DynamicSource(res.Metadata.Name, selfNode)
	actionJournal, err := c.Store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return fmt.Errorf("list action journal: %w", err)
	}
	markers, err := c.Store.ListMobilityDeprovisionMarkers(source)
	if err != nil {
		return fmt.Errorf("list mobility deprovision markers: %w", err)
	}
	previousOwnership, err := c.Store.ListMobilityOwnershipEpochs(res.Metadata.Name)
	if err != nil {
		return fmt.Errorf("list mobility ownership epochs: %w", err)
	}
	liveness, err := c.ownershipLiveness(res.Metadata.Name, spec, now)
	if err != nil {
		return err
	}
	captureEpochDesired, err := desiredCaptureEpochs(res.Metadata.Name, spec, selfNode, leases, previousClaims, markers, cloudProviderProfiles(c.Router), liveness, now)
	if err != nil {
		return err
	}
	captureEpochs, err := c.Store.ReconcileMobilityCaptureEpochs(captureEpochDesired)
	if err != nil {
		return fmt.Errorf("reconcile mobility capture epochs: %w", err)
	}
	ownershipDesired, err := desiredOwnershipEpochs(res.Metadata.Name, spec, leases, liveness, now)
	if err != nil {
		return err
	}
	ownershipEpochs, err := c.Store.ReconcileMobilityOwnershipEpochs(ownershipDesired)
	if err != nil {
		return fmt.Errorf("reconcile mobility ownership epochs: %w", err)
	}
	markers, err = c.completeDeprovisionMarkers(markers, actionJournal)
	if err != nil {
		return err
	}
	markers, err = c.dropStaleDeprovisionMarkers(markers, captureEpochsByKey(captureEpochs))
	if err != nil {
		return err
	}
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName:            res.Metadata.Name,
		PoolSpec:            spec,
		SelfNode:            selfNode,
		Now:                 now,
		Leases:              leases,
		PreviousClaims:      previousClaims,
		DeprovisionMarkers:  markers,
		CaptureEpochs:       captureEpochs,
		OwnershipEpochs:     ownershipEpochs,
		PreviousOwnership:   previousOwnership,
		PreviousActionPlans: previousActionPlans,
		ActionJournal:       actionJournal,
		Liveness:            liveness,
		ProviderProfiles:    cloudProviderProfiles(c.Router),
	})
	if err != nil {
		_ = c.upsertEmptyPlan(res.Metadata.Name, spec, selfNode, now)
		return err
	}
	if err := c.persistDeprovisionMarkers(source, out.ActionPlans); err != nil {
		return err
	}
	record, err := dynamicPartRecord(out.Part)
	if err != nil {
		return err
	}
	if err := c.Store.UpsertDynamicConfigPart(record); err != nil {
		return fmt.Errorf("upsert dynamic config part: %w", err)
	}
	plannerPhase := "Planned"
	plannerReason := out.Placement.Reason
	if out.Placement.NoCandidate() {
		plannerPhase = "NoPlacementCandidate"
	}
	status := map[string]any{
		"plannerPhase":       plannerPhase,
		"plannerReason":      plannerReason,
		"selfNode":           selfNode,
		"dynamicSource":      out.Part.Spec.Source,
		"dynamicGeneration":  out.Part.Spec.Generation,
		"generatedClaims":    len(out.Claims),
		"generatedActions":   len(out.ActionPlans),
		"dynamicExpiresAt":   out.Part.Spec.ExpiresAt.Format(time.RFC3339Nano),
		"dynamicDigest":      out.Part.Spec.Digest,
		"plannedAt":          now.Format(time.RFC3339Nano),
		"operatorIntent":     "MobilityPool",
		"derivedConfigKinds": []string{"AddressMobilityDomain", "RemoteAddressClaim"},
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
	if out.Placement.Group != "" {
		status["placementGroup"] = out.Placement.Group
		status["placementActive"] = out.Placement.Active
		status["placementActiveNode"] = out.Placement.ActiveNode
	}
	return c.savePlannerStatus(res.Metadata.Name, status)
}

func (c Controller) completeDeprovisionMarkers(markers []routerstate.MobilityDeprovisionMarkerRecord, journal []routerstate.ActionExecutionRecord) ([]routerstate.MobilityDeprovisionMarkerRecord, error) {
	completed := map[string]bool{}
	for _, action := range journal {
		if !markerCompletedByAction(action.Status) {
			continue
		}
		key := strings.TrimSpace(action.IdempotencyKey)
		if key != "" {
			completed[key] = true
		}
	}
	if len(completed) == 0 {
		return markers, nil
	}
	pending := make([]routerstate.MobilityDeprovisionMarkerRecord, 0, len(markers))
	for _, marker := range markers {
		key := strings.TrimSpace(marker.Key)
		if key == "" {
			key = strings.TrimSpace(marker.IdempotencyKey)
		}
		if completed[key] {
			if err := c.Store.DeleteMobilityDeprovisionMarker(key); err != nil {
				return nil, fmt.Errorf("delete mobility deprovision marker %s: %w", key, err)
			}
			continue
		}
		pending = append(pending, marker)
	}
	return pending, nil
}

func (c Controller) dropStaleDeprovisionMarkers(markers []routerstate.MobilityDeprovisionMarkerRecord, epochs map[string]routerstate.MobilityCaptureEpochRecord) ([]routerstate.MobilityDeprovisionMarkerRecord, error) {
	if len(markers) == 0 || len(epochs) == 0 {
		return markers, nil
	}
	pending := make([]routerstate.MobilityDeprovisionMarkerRecord, 0, len(markers))
	for _, marker := range markers {
		if strings.TrimSpace(marker.ActionPlanJSON) == "" {
			pending = append(pending, marker)
			continue
		}
		var plan dynamicconfig.ActionPlan
		if err := json.Unmarshal([]byte(marker.ActionPlanJSON), &plan); err != nil {
			return nil, fmt.Errorf("decode mobility deprovision marker %q: %w", marker.Key, err)
		}
		if captureEpochPlanStale(plan, epochs) {
			key := strings.TrimSpace(marker.Key)
			if key == "" {
				key = strings.TrimSpace(marker.IdempotencyKey)
			}
			if err := c.Store.DeleteMobilityDeprovisionMarker(key); err != nil {
				return nil, fmt.Errorf("delete stale mobility deprovision marker %s: %w", key, err)
			}
			continue
		}
		pending = append(pending, marker)
	}
	return pending, nil
}

func markerCompletedByAction(status string) bool {
	switch strings.TrimSpace(status) {
	case routerstate.ActionSucceeded, "canceled", "cancelled":
		return true
	default:
		return false
	}
}

func (c Controller) persistDeprovisionMarkers(source string, plans []dynamicconfig.ActionPlan) error {
	for _, plan := range plans {
		if !isDeprovisionAction(plan.Action) {
			continue
		}
		marker, err := deprovisionMarkerFromPlan(source, plan)
		if err != nil {
			return err
		}
		if err := c.Store.UpsertMobilityDeprovisionMarker(marker); err != nil {
			return fmt.Errorf("upsert mobility deprovision marker %s: %w", marker.Key, err)
		}
	}
	return nil
}

func isDeprovisionAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "unassign-secondary-ip", "ensure-forwarding-disabled":
		return true
	default:
		return false
	}
}

func deprovisionMarkerFromPlan(source string, plan dynamicconfig.ActionPlan) (routerstate.MobilityDeprovisionMarkerRecord, error) {
	key := strings.TrimSpace(plan.IdempotencyKey)
	if key == "" {
		return routerstate.MobilityDeprovisionMarkerRecord{}, fmt.Errorf("deprovision action %q is missing idempotencyKey", plan.Name)
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return routerstate.MobilityDeprovisionMarkerRecord{}, fmt.Errorf("marshal deprovision action plan %q: %w", key, err)
	}
	return routerstate.MobilityDeprovisionMarkerRecord{
		Key:            key,
		Source:         strings.TrimSpace(source),
		IdempotencyKey: key,
		Action:         strings.TrimSpace(plan.Action),
		ActionPlanJSON: string(data),
	}, nil
}

func (c Controller) upsertEmptyPlan(poolName string, spec api.MobilityPoolSpec, selfNode string, now time.Time) error {
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
			ActionPlans: []dynamicconfig.ActionPlan{},
		},
	}
	part.Spec.Digest = digestDynamicPart(part)
	record, err := dynamicPartRecord(part)
	if err != nil {
		return err
	}
	return c.Store.UpsertDynamicConfigPart(record)
}

func (c Controller) previousGeneratedClaims(poolName, selfNode string) ([]api.Resource, error) {
	source := DynamicSource(poolName, selfNode)
	parts, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return nil, fmt.Errorf("get previous dynamic config part %s: %w", source, err)
	}
	if len(parts) == 0 {
		return nil, nil
	}
	sort.SliceStable(parts, func(i, j int) bool {
		if parts[i].Generation == parts[j].Generation {
			return parts[i].UpdatedAt.After(parts[j].UpdatedAt)
		}
		return parts[i].Generation > parts[j].Generation
	})
	resources, err := decodeDynamicConfigResources(parts[0].ResourcesJSON)
	if err != nil {
		return nil, fmt.Errorf("decode previous dynamic config part %s: %w", source, err)
	}
	var claims []api.Resource
	for _, res := range resources {
		if res.APIVersion == api.HybridAPIVersion && res.Kind == "RemoteAddressClaim" {
			claims = append(claims, res)
		}
	}
	return claims, nil
}

func (c Controller) previousGeneratedActionPlans(poolName, selfNode string) ([]dynamicconfig.ActionPlan, error) {
	source := DynamicSource(poolName, selfNode)
	parts, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return nil, fmt.Errorf("get previous dynamic config part %s: %w", source, err)
	}
	if len(parts) == 0 {
		return nil, nil
	}
	sort.SliceStable(parts, func(i, j int) bool {
		if parts[i].Generation == parts[j].Generation {
			return parts[i].UpdatedAt.After(parts[j].UpdatedAt)
		}
		return parts[i].Generation > parts[j].Generation
	})
	if strings.TrimSpace(parts[0].ActionPlansJSON) == "" {
		return nil, nil
	}
	var plans []dynamicconfig.ActionPlan
	if err := json.Unmarshal([]byte(parts[0].ActionPlansJSON), &plans); err != nil {
		return nil, fmt.Errorf("decode previous dynamic config part action plans %s: %w", source, err)
	}
	return plans, nil
}

// DynamicSource is the stable DynamicConfigPart source for one pool x node. The
// planner always writes generation 1 for this source and replaces the complete
// generated resource set on every reconcile.
func DynamicSource(poolName, selfNode string) string {
	return dynamicSourceKind + "/" + strings.TrimSpace(poolName) + "/node/" + strings.TrimSpace(selfNode)
}

func (c Controller) selfNode(groupRef string) (string, error) {
	groupRef = strings.TrimSpace(groupRef)
	if groupRef == "" {
		return "", fmt.Errorf("groupRef is required")
	}
	for _, res := range c.Router.Spec.Resources {
		if res.APIVersion != api.FederationAPIVersion || res.Kind != "EventGroup" || res.Metadata.Name != groupRef {
			continue
		}
		spec, err := res.EventGroupSpec()
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(spec.NodeName) == "" {
			return "", fmt.Errorf("EventGroup/%s spec.nodeName is required for mobility planning", groupRef)
		}
		return strings.TrimSpace(spec.NodeName), nil
	}
	return "", fmt.Errorf("EventGroup/%s not found for mobility planning", groupRef)
}

func (c Controller) savePlannerStatus(poolName string, updates map[string]any) error {
	status := map[string]any{}
	if c.Store != nil {
		for k, v := range c.Store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName) {
			status[k] = v
		}
	}
	for k, v := range updates {
		status[k] = v
	}
	return c.Store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName, status)
}

func planLease(poolName string, prefix netip.Prefix, self memberPlanInfo, members map[string]memberPlanInfo, lease routerstate.AddressLeaseRecord, profiles map[string]api.CloudProviderProfileSpec, now time.Time, forwardingSeen map[string]bool, captureEpochs map[string]routerstate.MobilityCaptureEpochRecord, ownershipEpochs map[string]routerstate.MobilityOwnershipEpochRecord, seize bool) (api.Resource, []dynamicconfig.ActionPlan, time.Time, bool, error) {
	if lease.Pool != poolName || lease.Status != routerstate.AddressLeaseStatusActive {
		return api.Resource{}, nil, time.Time{}, false, nil
	}
	if !lease.ExpiresAt.IsZero() && !now.Before(lease.ExpiresAt) {
		return api.Resource{}, nil, time.Time{}, false, nil
	}
	address, ok := normalizeLeaseAddress(lease.Address, prefix)
	if !ok {
		return api.Resource{}, nil, time.Time{}, false, nil
	}
	owner, ok := members[strings.TrimSpace(lease.OwnerNode)]
	if !ok {
		return api.Resource{}, nil, time.Time{}, false, nil
	}
	if owner.NodeRef == self.NodeRef || owner.Site == self.Site {
		return api.Resource{}, nil, time.Time{}, false, nil
	}
	ownerRole := strings.TrimSpace(lease.OwnerRole)
	if ownerRole == "" {
		ownerRole = owner.Role
	}
	if ownerRole != "onprem" && ownerRole != "cloud" {
		return api.Resource{}, nil, time.Time{}, false, fmt.Errorf("AddressLease/%s ownerRole %q is not supported", lease.Address, lease.OwnerRole)
	}
	owner.Role = ownerRole
	if strings.TrimSpace(self.Capture.Type) == "" {
		return api.Resource{}, nil, time.Time{}, false, fmt.Errorf("MobilityPool/%s member %q capture is required to plan remote lease %s", poolName, self.NodeRef, lease.Address)
	}
	delivery, ok := resolveDelivery(self, owner)
	if !ok {
		return api.Resource{}, nil, time.Time{}, false, fmt.Errorf("MobilityPool/%s member %q has no delivery for owner node=%q site=%q role=%q", poolName, self.NodeRef, owner.NodeRef, owner.Site, ownerRole)
	}
	claim := claimResource(poolName, self, lease, address, ownerRole, delivery)
	stampOwnershipEpochResource(&claim, ownershipEpochs[address])
	var plans []dynamicconfig.ActionPlan
	if self.Capture.Type == "provider-secondary-ip" {
		profile, ok := profiles[strings.TrimSpace(self.Capture.ProviderRef)]
		if !ok {
			return api.Resource{}, nil, time.Time{}, false, fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q", self.Capture.ProviderRef, poolName, self.NodeRef)
		}
		generated, err := providerActionPlans(poolName, profile, self.Capture, self.CaptureTarget, address, forwardingSeen, seize)
		if err != nil {
			return api.Resource{}, nil, time.Time{}, false, err
		}
		stampCaptureEpochActionPlans(generated, captureEpochs, captureEpochKey(poolName, address, captureDomain(self)))
		stampOwnershipEpochActionPlans(generated, ownershipEpochs[address])
		plans = append(plans, generated...)
	}
	return claim, plans, lease.ExpiresAt, true, nil
}

func plannerMembers(members []api.MobilityPoolMember) map[string]memberPlanInfo {
	out := map[string]memberPlanInfo{}
	for _, member := range members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		out[nodeRef] = memberPlanInfo{
			NodeRef:           nodeRef,
			Site:              strings.TrimSpace(member.Site),
			Role:              strings.TrimSpace(member.Role),
			Capture:           trimCapture(member.Capture),
			CaptureTarget:     copyStringMap(member.Capture.Target),
			Delivery:          trimDelivery(member.Delivery),
			DeliveryTo:        trimDeliveryTargets(member.DeliveryTo),
			PlacementGroup:    strings.TrimSpace(member.Placement.Group),
			PlacementPriority: member.Placement.Priority,
			MaintenanceDrain:  member.Maintenance.Drain,
		}
	}
	return out
}

func evaluatePlacement(self memberPlanInfo, members map[string]memberPlanInfo) PlacementDecision {
	return evaluatePlacementWithLiveness(self, members, api.MobilityIPOwnershipPolicy{}, OwnershipLiveness{})
}

func evaluatePlacementWithLiveness(self memberPlanInfo, members map[string]memberPlanInfo, policy api.MobilityIPOwnershipPolicy, liveness OwnershipLiveness) PlacementDecision {
	group := strings.TrimSpace(self.PlacementGroup)
	if group == "" {
		return PlacementDecision{Active: true, ActiveNode: self.NodeRef}
	}
	autoFailover := ipOwnershipAutoFailover(policy)
	candidates := make([]memberPlanInfo, 0, len(members))
	for _, member := range members {
		if strings.TrimSpace(member.PlacementGroup) != group || member.MaintenanceDrain {
			continue
		}
		if autoFailover && member.Role == "cloud" && member.Capture.Type == "provider-secondary-ip" && liveness.StaleNodes[member.NodeRef] {
			continue
		}
		candidates = append(candidates, member)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].PlacementPriority == candidates[j].PlacementPriority {
			return candidates[i].NodeRef < candidates[j].NodeRef
		}
		return candidates[i].PlacementPriority < candidates[j].PlacementPriority
	})
	if len(candidates) == 0 {
		return PlacementDecision{
			Group:  group,
			Active: false,
			Reason: fmt.Sprintf("placement group %q has no non-drained members", group),
		}
	}
	activeNode := candidates[0].NodeRef
	if activeNode == self.NodeRef {
		return PlacementDecision{Group: group, Active: true, ActiveNode: activeNode}
	}
	return PlacementDecision{
		Group:      group,
		Active:     false,
		ActiveNode: activeNode,
		Reason:     fmt.Sprintf("placement group %q active node is %q", group, activeNode),
	}
}

func desiredCaptureEpochs(poolName string, spec api.MobilityPoolSpec, selfNode string, leases []routerstate.AddressLeaseRecord, previousClaims []api.Resource, markers []routerstate.MobilityDeprovisionMarkerRecord, profiles map[string]api.CloudProviderProfileSpec, liveness OwnershipLiveness, now time.Time) ([]routerstate.MobilityCaptureEpochRecord, error) {
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
	placement := evaluatePlacementWithLiveness(self, members, spec.IPOwnershipPolicy, liveness)
	if placement.NoCandidate() || strings.TrimSpace(placement.ActiveNode) == "" {
		return nil, nil
	}
	active, ok := members[placement.ActiveNode]
	if !ok || active.Capture.Type != "provider-secondary-ip" {
		return nil, nil
	}
	var out []routerstate.MobilityCaptureEpochRecord
	seen := map[string]bool{}
	addDesired := func(address string) {
		address = strings.TrimSpace(address)
		if address == "" {
			return
		}
		domain := captureDomain(active)
		key := captureEpochKey(poolName, address, domain)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, routerstate.MobilityCaptureEpochRecord{
			CaptureKey:    key,
			Pool:          poolName,
			Address:       address,
			CaptureDomain: domain,
			Holder:        active.NodeRef,
		})
	}
	forwardingSeen := map[string]bool{}
	emptyEpochs := map[string]routerstate.MobilityCaptureEpochRecord{}
	for _, lease := range sortedLeases(leases) {
		claim, _, _, ok, err := planLease(poolName, prefix, active, members, lease, profiles, now, forwardingSeen, emptyEpochs, nil, false)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		addDesired(claimAddress(claim))
	}
	for _, claim := range sortedClaims(previousClaims) {
		spec, err := claim.RemoteAddressClaimSpec()
		if err != nil {
			return nil, err
		}
		if spec.Capture.Type != "provider-secondary-ip" {
			continue
		}
		addDesired(spec.Address)
	}
	for _, marker := range markers {
		if strings.TrimSpace(marker.ActionPlanJSON) == "" {
			continue
		}
		var plan dynamicconfig.ActionPlan
		if err := json.Unmarshal([]byte(marker.ActionPlanJSON), &plan); err != nil {
			return nil, fmt.Errorf("decode mobility deprovision marker %q: %w", marker.Key, err)
		}
		if address := strings.TrimSpace(plan.Target["address"]); address != "" {
			addDesired(address)
		}
	}
	return out, nil
}

func desiredOwnershipEpochs(poolName string, spec api.MobilityPoolSpec, leases []routerstate.AddressLeaseRecord, liveness OwnershipLiveness, now time.Time) ([]routerstate.MobilityOwnershipEpochRecord, error) {
	if !ipOwnershipPolicyCentralized(spec.IPOwnershipPolicy) {
		return nil, nil
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
	if err != nil {
		return nil, fmt.Errorf("parse pool prefix: %w", err)
	}
	prefix = prefix.Masked()
	members := plannerMembers(spec.Members)
	var out []routerstate.MobilityOwnershipEpochRecord
	for _, lease := range sortedLeases(leases) {
		if lease.Pool != poolName {
			continue
		}
		switch lease.Status {
		case routerstate.AddressLeaseStatusActive, routerstate.AddressLeaseStatusHolding:
		default:
			continue
		}
		if !lease.ExpiresAt.IsZero() && !now.Before(lease.ExpiresAt) {
			continue
		}
		address, ok := normalizeLeaseAddress(lease.Address, prefix)
		if !ok {
			continue
		}
		owner, ok := arbitrateOwnership(address, strings.TrimSpace(lease.OwnerNode), members, spec.IPOwnershipPolicy, liveness.StaleNodes)
		if !ok {
			continue
		}
		out = append(out, routerstate.MobilityOwnershipEpochRecord{
			Pool:      poolName,
			Address:   address,
			OwnerNode: owner.NodeRef,
		})
	}
	return out, nil
}

func (c Controller) ownershipLiveness(poolName string, spec api.MobilityPoolSpec, now time.Time) (OwnershipLiveness, error) {
	view := OwnershipLiveness{
		LastHeartbeat: map[string]time.Time{},
		StaleNodes:    map[string]bool{},
	}
	if !ipOwnershipAutoFailover(spec.IPOwnershipPolicy) {
		return view, nil
	}
	events, err := c.Store.ListFederationEvents(spec.GroupRef, false, now.Unix())
	if err != nil {
		return view, fmt.Errorf("list federation events for ownership liveness: %w", err)
	}
	for _, ev := range events {
		if ev.ObservedAt.After(view.StreamMaxObservedAt) {
			view.StreamMaxObservedAt = ev.ObservedAt.UTC()
		}
		if ev.Type != HeartbeatEventType || strings.TrimSpace(ev.Payload["pool"]) != poolName {
			continue
		}
		node := strings.TrimSpace(firstNonEmpty(ev.Payload["node"], ev.SourceNode))
		if node == "" {
			continue
		}
		if ev.ObservedAt.After(view.LastHeartbeat[node]) {
			view.LastHeartbeat[node] = ev.ObservedAt.UTC()
		}
	}
	if view.StreamMaxObservedAt.IsZero() {
		return view, nil
	}
	ttl := durationDefault(spec.IPOwnershipPolicy.HeartbeatTTL, 0)
	hold := durationDefault(spec.IPOwnershipPolicy.PromotionHoldDuration, 0)
	if ttl <= 0 {
		return view, nil
	}
	members := plannerMembers(spec.Members)
	for node, last := range view.LastHeartbeat {
		member, ok := members[node]
		if !ok || member.Role != "cloud" || member.Capture.Type != "provider-secondary-ip" {
			continue
		}
		if !last.IsZero() && !last.Add(ttl).Add(hold).After(view.StreamMaxObservedAt) {
			view.StaleNodes[node] = true
		}
	}
	return view, nil
}

func arbitrateOwnership(address, leaseOwnerNode string, members map[string]memberPlanInfo, policy api.MobilityIPOwnershipPolicy, staleNodes map[string]bool) (memberPlanInfo, bool) {
	leaseOwner := members[strings.TrimSpace(leaseOwnerNode)]
	prefer := map[string]int{}
	for i, nodeRef := range policy.PreferNodes {
		prefer[strings.TrimSpace(nodeRef)] = i
	}
	candidates := make([]memberPlanInfo, 0, len(members))
	for _, member := range members {
		if member.MaintenanceDrain || strings.TrimSpace(member.Capture.Type) == "" {
			continue
		}
		if policy.AutoFailover && member.Role == "cloud" && member.Capture.Type == "provider-secondary-ip" && staleNodes[member.NodeRef] {
			continue
		}
		if leaseOwner.NodeRef != "" && (member.NodeRef == leaseOwner.NodeRef || member.Site == leaseOwner.Site) {
			continue
		}
		candidates = append(candidates, member)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		leftPrefer := ownershipPreferRank(candidates[i].NodeRef, prefer)
		rightPrefer := ownershipPreferRank(candidates[j].NodeRef, prefer)
		if leftPrefer != rightPrefer {
			return leftPrefer < rightPrefer
		}
		if candidates[i].PlacementPriority != candidates[j].PlacementPriority {
			return candidates[i].PlacementPriority < candidates[j].PlacementPriority
		}
		leftTie := ownershipTie(address, candidates[i].NodeRef)
		rightTie := ownershipTie(address, candidates[j].NodeRef)
		if leftTie != rightTie {
			return leftTie > rightTie
		}
		return candidates[i].NodeRef < candidates[j].NodeRef
	})
	if len(candidates) == 0 {
		return memberPlanInfo{}, false
	}
	return candidates[0], true
}

func shouldSeizeOwnership(address, selfNode string, previous []routerstate.MobilityOwnershipEpochRecord, liveness OwnershipLiveness) bool {
	_, ok := seizeOriginOwner(address, selfNode, previous, liveness)
	return ok
}

func seizeOriginOwner(address, selfNode string, previous []routerstate.MobilityOwnershipEpochRecord, liveness OwnershipLiveness) (string, bool) {
	address = normalizeAddressString(address)
	selfNode = strings.TrimSpace(selfNode)
	if address == "" || selfNode == "" || len(liveness.StaleNodes) == 0 {
		return "", false
	}
	for _, rec := range previous {
		if normalizeAddressString(rec.Address) != address {
			continue
		}
		previousOwner := strings.TrimSpace(rec.OwnerNode)
		if previousOwner != "" && previousOwner != selfNode && liveness.StaleNodes[previousOwner] {
			return previousOwner, true
		}
		return "", false
	}
	return "", false
}

func shouldMaintainSeizeIntent(poolName, address, selfNode string, current routerstate.MobilityOwnershipEpochRecord, previousPlans []dynamicconfig.ActionPlan, journal []routerstate.ActionExecutionRecord) bool {
	poolName = strings.TrimSpace(poolName)
	address = normalizeAddressString(address)
	selfNode = strings.TrimSpace(selfNode)
	if poolName == "" || address == "" || selfNode == "" || current.Epoch <= 0 || strings.TrimSpace(current.OwnerNode) != selfNode {
		return false
	}
	if ownershipAssignSucceeded(poolName, address, selfNode, current.Epoch, journal) {
		return false
	}
	if previousSeizePlanExists(poolName, address, selfNode, current.Epoch, previousPlans) {
		return true
	}
	return pendingSeizeActionExists(poolName, address, selfNode, current.Epoch, journal)
}

func previousSeizePlanExists(poolName, address, selfNode string, epoch int64, plans []dynamicconfig.ActionPlan) bool {
	for _, plan := range plans {
		if plan.Action != "assign-secondary-ip" || plan.Parameters["allowReassignment"] != "true" {
			continue
		}
		if ownershipParamsMatch(plan.Parameters, poolName, address, selfNode, epoch) {
			return true
		}
	}
	return false
}

func pendingSeizeActionExists(poolName, address, selfNode string, epoch int64, journal []routerstate.ActionExecutionRecord) bool {
	for _, action := range journal {
		if action.Action != "assign-secondary-ip" || action.Status == routerstate.ActionSucceeded {
			continue
		}
		params := decodeActionParameters(action.ParametersJSON)
		if params["allowReassignment"] != "true" {
			continue
		}
		if ownershipParamsMatch(params, poolName, address, selfNode, epoch) {
			return true
		}
	}
	return false
}

func ownershipAssignSucceeded(poolName, address, selfNode string, epoch int64, journal []routerstate.ActionExecutionRecord) bool {
	for _, action := range journal {
		if action.Action != "assign-secondary-ip" || action.Status != routerstate.ActionSucceeded {
			continue
		}
		if ownershipParamsMatch(decodeActionParameters(action.ParametersJSON), poolName, address, selfNode, epoch) {
			return true
		}
	}
	return false
}

func ownershipParamsMatch(params map[string]string, poolName, address, owner string, epoch int64) bool {
	if len(params) == 0 || epoch <= 0 {
		return false
	}
	return strings.TrimSpace(params[ownershipParamPool]) == strings.TrimSpace(poolName) &&
		normalizeAddressString(params[ownershipParamAddress]) == normalizeAddressString(address) &&
		strings.TrimSpace(params[ownershipParamOwner]) == strings.TrimSpace(owner) &&
		strings.TrimSpace(params[ownershipParamEpoch]) == strconv.FormatInt(epoch, 10)
}

func enrichAzureSeizeDisplacedTargets(poolName, address string, self memberPlanInfo, members map[string]memberPlanInfo, originOwner string, profiles map[string]api.CloudProviderProfileSpec, current routerstate.MobilityOwnershipEpochRecord, plans []dynamicconfig.ActionPlan, previousPlans []dynamicconfig.ActionPlan) {
	for i := range plans {
		plan := &plans[i]
		if plan.Provider != "azure" || plan.Action != "assign-secondary-ip" || plan.Parameters["allowReassignment"] != "true" {
			continue
		}
		if plan.Target == nil {
			plan.Target = map[string]string{}
		}
		copyAzureSelfTargetAliases(plan.Target)
		if originOwner != "" {
			if origin, ok := members[strings.TrimSpace(originOwner)]; ok {
				if displaced := azureDisplacedTarget(poolName, address, origin, profiles); len(displaced) > 0 {
					for key, value := range displaced {
						plan.Target[key] = value
					}
					continue
				}
			}
		}
		copyAzureDisplacedTargetFromPrevious(plan.Target, poolName, address, self.NodeRef, current, previousPlans)
	}
}

func copyAzureSelfTargetAliases(target map[string]string) {
	if target == nil {
		return
	}
	copyIfMissing(target, "selfNicRef", target["nicRef"])
	copyIfMissing(target, "selfResourceGroup", target["resourceGroup"])
	copyIfMissing(target, "selfNicName", target["nicName"])
	copyIfMissing(target, "selfIpConfigName", target["ipConfigName"])
}

func azureDisplacedTarget(poolName, address string, member memberPlanInfo, profiles map[string]api.CloudProviderProfileSpec) map[string]string {
	if member.Capture.Type != "provider-secondary-ip" {
		return nil
	}
	profile, ok := profiles[strings.TrimSpace(member.Capture.ProviderRef)]
	if !ok || strings.TrimSpace(profile.Provider) != "azure" {
		return nil
	}
	target := providerActionTarget(poolName, profile, member.Capture, member.CaptureTarget, address)
	out := map[string]string{}
	copyIfMissing(out, "displacedNicRef", target["nicRef"])
	copyIfMissing(out, "displacedResourceGroup", target["resourceGroup"])
	copyIfMissing(out, "displacedNicName", target["nicName"])
	copyIfMissing(out, "displacedIpConfigName", target["ipConfigName"])
	return out
}

func copyAzureDisplacedTargetFromPrevious(target map[string]string, poolName, address, selfNode string, current routerstate.MobilityOwnershipEpochRecord, previousPlans []dynamicconfig.ActionPlan) {
	if target == nil || current.Epoch <= 0 {
		return
	}
	keys := []string{"displacedNicRef", "displacedResourceGroup", "displacedNicName", "displacedIpConfigName"}
	for _, plan := range previousPlans {
		if plan.Provider != "azure" || plan.Action != "assign-secondary-ip" || plan.Parameters["allowReassignment"] != "true" {
			continue
		}
		if !ownershipParamsMatch(plan.Parameters, poolName, address, selfNode, current.Epoch) {
			continue
		}
		for _, key := range keys {
			copyIfMissing(target, key, plan.Target[key])
		}
		return
	}
}

func copyIfMissing(target map[string]string, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if strings.TrimSpace(target[key]) == "" {
		target[key] = strings.TrimSpace(value)
	}
}

func decodeActionParameters(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func ownershipPreferRank(nodeRef string, prefer map[string]int) int {
	if rank, ok := prefer[strings.TrimSpace(nodeRef)]; ok {
		return rank
	}
	return 1 << 30
}

func ownershipTie(address, nodeRef string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(address) + "\x00" + strings.TrimSpace(nodeRef)))
	return hex.EncodeToString(sum[:])
}

func ipOwnershipPolicyCentralized(policy api.MobilityIPOwnershipPolicy) bool {
	return strings.TrimSpace(policy.Type) == "centralized"
}

func ipOwnershipEpochLocking(policy api.MobilityIPOwnershipPolicy) bool {
	if !ipOwnershipPolicyCentralized(policy) {
		return false
	}
	return policy.EpochLocking == nil || *policy.EpochLocking
}

func ipOwnershipAutoFailover(policy api.MobilityIPOwnershipPolicy) bool {
	return ipOwnershipPolicyCentralized(policy) && policy.AutoFailover
}

func captureEpochsByKey(records []routerstate.MobilityCaptureEpochRecord) map[string]routerstate.MobilityCaptureEpochRecord {
	out := map[string]routerstate.MobilityCaptureEpochRecord{}
	for _, rec := range records {
		if key := strings.TrimSpace(rec.CaptureKey); key != "" {
			out[key] = rec
		}
	}
	return out
}

func ownershipEpochsByAddress(records []routerstate.MobilityOwnershipEpochRecord) map[string]routerstate.MobilityOwnershipEpochRecord {
	out := map[string]routerstate.MobilityOwnershipEpochRecord{}
	for _, rec := range records {
		if address := normalizeAddressString(rec.Address); address != "" {
			out[address] = rec
		}
	}
	return out
}

func ownershipStatusMap(records []routerstate.MobilityOwnershipEpochRecord) []map[string]any {
	ordered := append([]routerstate.MobilityOwnershipEpochRecord(nil), records...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Address == ordered[j].Address {
			return ordered[i].OwnerNode < ordered[j].OwnerNode
		}
		return ordered[i].Address < ordered[j].Address
	})
	out := make([]map[string]any, 0, len(ordered))
	for _, rec := range ordered {
		out = append(out, map[string]any{
			"address":        rec.Address,
			"ownerNode":      rec.OwnerNode,
			"ownershipEpoch": rec.Epoch,
		})
	}
	return out
}

func staleMembersStatus(stale map[string]bool) []string {
	out := make([]string, 0, len(stale))
	for node, isStale := range stale {
		if isStale {
			out = append(out, node)
		}
	}
	sort.Strings(out)
	return out
}

func normalizeAddressString(address string) string {
	return strings.TrimSpace(address)
}

func captureDomain(member memberPlanInfo) string {
	if member.Capture.Type == "provider-secondary-ip" {
		scope := strings.TrimSpace(member.PlacementGroup)
		if scope == "" {
			scope = "node:" + strings.TrimSpace(member.NodeRef)
		} else {
			scope = "placement:" + scope
		}
		return "provider:" + strings.TrimSpace(member.Capture.ProviderRef) + ":" + scope
	}
	return strings.TrimSpace(member.NodeRef)
}

func captureEpochKey(poolName, address, domain string) string {
	return strings.Join([]string{strings.TrimSpace(poolName), strings.TrimSpace(address), strings.TrimSpace(domain)}, "\x00")
}

func resolveDelivery(self memberPlanInfo, owner memberPlanInfo) (api.AddressDelivery, bool) {
	for _, target := range self.DeliveryTo {
		if target.NodeRef != "" && target.NodeRef == owner.NodeRef {
			return target.Delivery, true
		}
	}
	for _, target := range self.DeliveryTo {
		if target.Site != "" && target.Site == owner.Site {
			return target.Delivery, true
		}
	}
	for _, target := range self.DeliveryTo {
		if target.Role != "" && target.Role == owner.Role {
			return target.Delivery, true
		}
	}
	if strings.TrimSpace(self.Delivery.PeerRef) != "" {
		return self.Delivery, true
	}
	return api.AddressDelivery{}, false
}

func sortedLeases(leases []routerstate.AddressLeaseRecord) []routerstate.AddressLeaseRecord {
	out := append([]routerstate.AddressLeaseRecord(nil), leases...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Address < out[j].Address
	})
	return out
}

func domainResource(poolName string, spec api.MobilityPoolSpec, self memberPlanInfo) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "AddressMobilityDomain"},
		Metadata: api.ObjectMeta{
			Name: safeName("mobility-" + poolName),
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "MobilityPool",
				Name:       poolName,
			}},
		},
		Spec: api.AddressMobilityDomainSpec{
			Prefix:  strings.TrimSpace(spec.Prefix),
			Mode:    "selective-address",
			PeerRef: strings.TrimSpace(self.Delivery.PeerRef),
		},
	}
}

func claimResource(poolName string, self memberPlanInfo, lease routerstate.AddressLeaseRecord, address, ownerRole string, delivery api.AddressDelivery) api.Resource {
	claimName := safeName("mobility-" + poolName + "-" + address)
	annotations := map[string]string{
		"mobility.routerd.net/lease-epoch":     fmt.Sprint(lease.Epoch),
		"mobility.routerd.net/owner-node":      strings.TrimSpace(lease.OwnerNode),
		"mobility.routerd.net/owner-site":      strings.TrimSpace(lease.OwnerSite),
		"mobility.routerd.net/owner-role":      ownerRole,
		"mobility.routerd.net/source-event-id": strings.TrimSpace(lease.SourceEventID),
	}
	for key, value := range self.CaptureTarget {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			annotations[captureTargetAnnotationPrefix+key] = value
		}
	}
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
		Metadata: api.ObjectMeta{
			Name:        claimName,
			Annotations: annotations,
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "MobilityPool",
				Name:       poolName,
			}},
		},
		Spec: api.RemoteAddressClaimSpec{
			DomainRef: safeName("mobility-" + poolName),
			Address:   address,
			OwnerSide: ownerRole,
			Capture:   self.Capture,
			Delivery:  delivery,
		},
	}
}

func stampOwnershipEpochResource(res *api.Resource, rec routerstate.MobilityOwnershipEpochRecord) {
	if res == nil || rec.Epoch <= 0 {
		return
	}
	if res.Metadata.Annotations == nil {
		res.Metadata.Annotations = map[string]string{}
	}
	res.Metadata.Annotations["mobility.routerd.net/ownership-epoch"] = strconv.FormatInt(rec.Epoch, 10)
	res.Metadata.Annotations["mobility.routerd.net/ownership-owner-node"] = strings.TrimSpace(rec.OwnerNode)
	// Compatibility annotation for older diagnostics that look for captureEpoch.
	res.Metadata.Annotations["mobility.routerd.net/capture-epoch"] = strconv.FormatInt(rec.Epoch, 10)
}

func stampGeneratedResource(res *api.Resource, source, poolName, selfNode string) {
	if res.Metadata.Annotations == nil {
		res.Metadata.Annotations = map[string]string{}
	}
	res.Metadata.Annotations["routerd.net/dynamic-source"] = source
	res.Metadata.Annotations["routerd.net/operator-intent"] = "MobilityPool/" + poolName
	res.Metadata.Annotations["mobility.routerd.net/pool"] = poolName
	res.Metadata.Annotations["mobility.routerd.net/self-node"] = selfNode
	res.Metadata.Annotations["routerd.net/managed-by"] = "routerd"
}

func providerActionPlans(poolName string, profile api.CloudProviderProfileSpec, capture api.AddressCapture, captureTarget map[string]string, address string, forwardingSeen map[string]bool, seize bool) ([]dynamicconfig.ActionPlan, error) {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	target := map[string]string{
		"provider":    provider,
		"providerRef": providerRef,
		"nicRef":      nicRef,
		"address":     address,
	}
	for key, value := range captureTarget {
		target[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	addProfileTargetFields(target, provider, profile, poolName, address, nicRef)
	target["provider"] = provider
	target["providerRef"] = providerRef
	target["nicRef"] = nicRef
	target["address"] = address
	assignDescription := fmt.Sprintf("Assign %s as a secondary IP on %s NIC %s for MobilityPool/%s", address, provider, nicRef, poolName)
	assignRisk := "medium"
	assignEffects := []string{
		fmt.Sprintf("%s NIC %s would advertise secondary IP %s", provider, nicRef, address),
	}
	var assignParams map[string]string
	if seize {
		assignDescription = fmt.Sprintf("Seize/reassign %s as a secondary IP on %s NIC %s for MobilityPool/%s after ownership failover", address, provider, nicRef, poolName)
		assignRisk = "high"
		assignParams = map[string]string{}
		assignParams["allowReassignment"] = "true"
		assignEffects = []string{
			fmt.Sprintf("%s NIC %s would seize secondary IP %s from any previous holder", provider, nicRef, address),
		}
	}
	assign := dynamicconfig.ActionPlan{
		Name:            safeName("mobility-" + poolName + "-assign-" + address),
		Provider:        provider,
		Action:          "assign-secondary-ip",
		Target:          target,
		ProviderRef:     providerRef,
		Mode:            "dry-run",
		Description:     assignDescription,
		RiskLevel:       assignRisk,
		IdempotencyKey:  "mobility:" + poolName + ":" + provider + ":" + nicRef + ":assign-secondary-ip:" + address,
		Parameters:      assignParams,
		ExpectedEffects: assignEffects,
		Undo: &dynamicconfig.ActionUndo{
			Action:     "unassign-secondary-ip",
			Parameters: copyStringMap(target),
		},
	}
	plans := []dynamicconfig.ActionPlan{assign}

	forwardingKey := provider + "\x00" + providerRef + "\x00" + nicRef
	if !forwardingSeen[forwardingKey] {
		params, err := forwardingParams(provider)
		if err != nil {
			return nil, err
		}
		fwdTarget := copyStringMap(target)
		forwardingSeen[forwardingKey] = true
		plans = append(plans, dynamicconfig.ActionPlan{
			Name:           safeName("mobility-" + poolName + "-forwarding-" + nicRef),
			Provider:       provider,
			Action:         "ensure-forwarding-enabled",
			Target:         fwdTarget,
			ProviderRef:    providerRef,
			Mode:           "dry-run",
			Description:    fmt.Sprintf("Ensure forwarding is enabled on %s NIC %s for MobilityPool/%s", provider, nicRef, poolName),
			RiskLevel:      "medium",
			IdempotencyKey: "mobility:" + poolName + ":" + provider + ":" + nicRef + ":ensure-forwarding-enabled",
			Parameters:     params,
			ExpectedEffects: []string{
				fmt.Sprintf("%s NIC %s would forward traffic for mobility captures", provider, nicRef),
			},
			Undo: &dynamicconfig.ActionUndo{
				Action:     "ensure-forwarding-disabled",
				Parameters: mergeStringMaps(fwdTarget, params),
			},
		})
	}
	return plans, nil
}

func carryForwardProviderClaims(previousClaims []api.Resource, desiredAddresses, deprovisionedAddresses map[string]bool) []api.Resource {
	var out []api.Resource
	for _, claim := range sortedClaims(previousClaims) {
		spec, err := claim.RemoteAddressClaimSpec()
		if err != nil {
			continue
		}
		if strings.TrimSpace(spec.Capture.Type) != "provider-secondary-ip" || strings.TrimSpace(spec.Address) == "" {
			continue
		}
		address := strings.TrimSpace(spec.Address)
		if desiredAddresses[address] || deprovisionedAddresses[address] {
			continue
		}
		out = append(out, cloneResource(claim))
	}
	return out
}

func actionPlanAddresses(plans []dynamicconfig.ActionPlan, action string) map[string]bool {
	out := map[string]bool{}
	for _, plan := range plans {
		if plan.Action != action {
			continue
		}
		if address := strings.TrimSpace(plan.Target["address"]); address != "" {
			out[address] = true
		}
	}
	return out
}

func filterForwardingDisablePlans(plans []dynamicconfig.ActionPlan, desiredProviderNICs map[string]bool) []dynamicconfig.ActionPlan {
	if len(plans) == 0 || len(desiredProviderNICs) == 0 {
		return plans
	}
	out := make([]dynamicconfig.ActionPlan, 0, len(plans))
	for _, plan := range plans {
		if plan.Action == "ensure-forwarding-disabled" {
			providerRef := strings.TrimSpace(plan.ProviderRef)
			if providerRef == "" {
				providerRef = strings.TrimSpace(plan.Target["providerRef"])
			}
			key := providerNICKey("", providerRef, plan.Target["nicRef"])
			if key != "" && desiredProviderNICs[key] {
				continue
			}
		}
		out = append(out, plan)
	}
	return out
}

func actionPlansFromMarkers(markers []routerstate.MobilityDeprovisionMarkerRecord) ([]dynamicconfig.ActionPlan, error) {
	var out []dynamicconfig.ActionPlan
	for _, marker := range markers {
		if strings.TrimSpace(marker.ActionPlanJSON) == "" {
			continue
		}
		var plan dynamicconfig.ActionPlan
		if err := json.Unmarshal([]byte(marker.ActionPlanJSON), &plan); err != nil {
			return nil, fmt.Errorf("decode mobility deprovision marker %q: %w", marker.Key, err)
		}
		out = append(out, plan)
	}
	return out, nil
}

func filterStaleCaptureEpochPlans(plans []dynamicconfig.ActionPlan, epochs map[string]routerstate.MobilityCaptureEpochRecord) []dynamicconfig.ActionPlan {
	if len(plans) == 0 || len(epochs) == 0 {
		return plans
	}
	out := make([]dynamicconfig.ActionPlan, 0, len(plans))
	for _, plan := range plans {
		if captureEpochPlanStale(plan, epochs) {
			continue
		}
		out = append(out, plan)
	}
	return out
}

func captureEpochPlanStale(plan dynamicconfig.ActionPlan, epochs map[string]routerstate.MobilityCaptureEpochRecord) bool {
	if strings.TrimSpace(plan.Parameters[ownershipParamEpoch]) != "" {
		return false
	}
	key, epoch, holder, ok := actionCaptureFence(plan)
	if !ok {
		return false
	}
	current, found := epochs[key]
	if !found {
		return false
	}
	if epoch < current.Epoch {
		return true
	}
	if epoch > current.Epoch {
		return false
	}
	switch strings.TrimSpace(plan.Action) {
	case "assign-secondary-ip", "ensure-forwarding-enabled":
		return holder != "" && holder != current.Holder
	case "unassign-secondary-ip", "ensure-forwarding-disabled":
		return holder != "" && holder == current.Holder
	default:
		return false
	}
}

func actionCaptureFence(plan dynamicconfig.ActionPlan) (string, int64, string, bool) {
	key := strings.TrimSpace(plan.Parameters[captureParamKey])
	holder := strings.TrimSpace(plan.Parameters[captureParamHolder])
	epochRaw := strings.TrimSpace(plan.Parameters[captureParamEpoch])
	if key == "" || epochRaw == "" {
		return "", 0, "", false
	}
	epoch, err := strconv.ParseInt(epochRaw, 10, 64)
	if err != nil || epoch <= 0 {
		return "", 0, "", false
	}
	return key, epoch, holder, true
}

func stampCaptureEpochActionPlans(plans []dynamicconfig.ActionPlan, epochs map[string]routerstate.MobilityCaptureEpochRecord, key string) {
	for i := range plans {
		stampCaptureEpochActionPlan(&plans[i], epochs, key)
	}
}

func stampCaptureEpochActionPlan(plan *dynamicconfig.ActionPlan, epochs map[string]routerstate.MobilityCaptureEpochRecord, key string) {
	stampCaptureEpochActionPlanHolder(plan, epochs, key, "")
}

func stampCaptureEpochActionPlanHolder(plan *dynamicconfig.ActionPlan, epochs map[string]routerstate.MobilityCaptureEpochRecord, key, holder string) {
	if plan == nil || strings.TrimSpace(key) == "" {
		return
	}
	rec, ok := epochs[key]
	if !ok || rec.Epoch <= 0 {
		return
	}
	if plan.Parameters == nil {
		plan.Parameters = map[string]string{}
	}
	if strings.TrimSpace(holder) == "" {
		holder = rec.Holder
	}
	plan.Parameters[captureParamKey] = rec.CaptureKey
	plan.Parameters[captureParamEpoch] = strconv.FormatInt(rec.Epoch, 10)
	plan.Parameters[captureParamHolder] = strings.TrimSpace(holder)
	if strings.TrimSpace(plan.IdempotencyKey) != "" {
		plan.IdempotencyKey += ":epoch:" + strconv.FormatInt(rec.Epoch, 10)
	}
}

func stampOwnershipEpochActionPlans(plans []dynamicconfig.ActionPlan, rec routerstate.MobilityOwnershipEpochRecord) {
	for i := range plans {
		stampOwnershipEpochActionPlan(&plans[i], rec)
	}
}

func stampOwnershipEpochActionPlan(plan *dynamicconfig.ActionPlan, rec routerstate.MobilityOwnershipEpochRecord) {
	stampOwnershipEpochActionPlanOwner(plan, rec, "")
}

func stampOwnershipEpochActionPlanOwner(plan *dynamicconfig.ActionPlan, rec routerstate.MobilityOwnershipEpochRecord, ownerNode string) {
	if plan == nil || rec.Epoch <= 0 {
		return
	}
	if plan.Parameters == nil {
		plan.Parameters = map[string]string{}
	}
	plan.Parameters[ownershipParamPool] = rec.Pool
	plan.Parameters[ownershipParamAddress] = rec.Address
	plan.Parameters[ownershipParamEpoch] = strconv.FormatInt(rec.Epoch, 10)
	if strings.TrimSpace(ownerNode) == "" {
		ownerNode = rec.OwnerNode
	}
	plan.Parameters[ownershipParamOwner] = strings.TrimSpace(ownerNode)
	if strings.TrimSpace(plan.IdempotencyKey) != "" {
		plan.IdempotencyKey += ":owner:" + strings.TrimSpace(ownerNode) + ":ownership-epoch:" + strconv.FormatInt(rec.Epoch, 10)
	}
}

func providerDeprovisionPlans(poolName string, self memberPlanInfo, previousClaims []api.Resource, desiredAddresses, desiredProviderNICs map[string]bool, leases map[string]routerstate.AddressLeaseRecord, profiles map[string]api.CloudProviderProfileSpec, now time.Time, hold time.Duration, releaseMissingLease bool, captureEpochs map[string]routerstate.MobilityCaptureEpochRecord, ownershipEpochs map[string]routerstate.MobilityOwnershipEpochRecord) ([]dynamicconfig.ActionPlan, error) {
	if self.Capture.Type != "provider-secondary-ip" {
		return nil, nil
	}
	var plans []dynamicconfig.ActionPlan
	forwardingDisabled := map[string]bool{}
	for _, claim := range sortedClaims(previousClaims) {
		spec, err := claim.RemoteAddressClaimSpec()
		if err != nil {
			return nil, err
		}
		if spec.Capture.Type != "provider-secondary-ip" {
			continue
		}
		address := strings.TrimSpace(spec.Address)
		if address == "" || desiredAddresses[address] {
			continue
		}
		lease := leases[address]
		since := deprovisionSince(lease)
		if since.IsZero() {
			if !releaseMissingLease {
				continue
			}
			since = now
		} else {
			if deprovisionShouldHold(lease, now, since, hold) {
				continue
			}
		}
		profile, ok := profiles[strings.TrimSpace(spec.Capture.ProviderRef)]
		if !ok {
			return nil, fmt.Errorf("CloudProviderProfile/%s not found for stale MobilityPool/%s claim %q", spec.Capture.ProviderRef, poolName, claim.Metadata.Name)
		}
		captureTarget := captureTargetFromClaim(claim)
		if len(captureTarget) == 0 {
			captureTarget = self.CaptureTarget
		}
		unassign, err := providerUnassignActionPlan(poolName, profile, spec.Capture, captureTarget, address, since)
		if err != nil {
			return nil, err
		}
		captureKey := captureEpochKey(poolName, address, captureDomain(self))
		stampCaptureEpochActionPlanHolder(&unassign, captureEpochs, captureKey, self.NodeRef)
		stampOwnershipEpochActionPlanOwner(&unassign, ownershipEpochs[address], self.NodeRef)
		plans = append(plans, unassign)

		nicKey := providerNICKey("", spec.Capture.ProviderRef, spec.Capture.NICRef)
		if nicKey == "" || desiredProviderNICs[nicKey] || forwardingDisabled[nicKey] {
			continue
		}
		disable, err := providerForwardingDisableActionPlan(poolName, profile, spec.Capture, captureTarget, address)
		if err != nil {
			return nil, err
		}
		stampCaptureEpochActionPlanHolder(&disable, captureEpochs, captureKey, self.NodeRef)
		stampOwnershipEpochActionPlanOwner(&disable, ownershipEpochs[address], self.NodeRef)
		plans = append(plans, disable)
		forwardingDisabled[nicKey] = true
	}
	return plans, nil
}

func deprovisionShouldHold(lease routerstate.AddressLeaseRecord, now, since time.Time, hold time.Duration) bool {
	if lease.Status == routerstate.AddressLeaseStatusActive {
		return false
	}
	return now.Before(since.Add(hold))
}

func providerUnassignActionPlan(poolName string, profile api.CloudProviderProfileSpec, capture api.AddressCapture, captureTarget map[string]string, address string, since time.Time) (dynamicconfig.ActionPlan, error) {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	target := providerActionTarget(poolName, profile, capture, captureTarget, address)
	return dynamicconfig.ActionPlan{
		Name:           safeName("mobility-" + poolName + "-unassign-" + address),
		Provider:       provider,
		Action:         "unassign-secondary-ip",
		Target:         target,
		ProviderRef:    providerRef,
		Mode:           "dry-run",
		Description:    fmt.Sprintf("Unassign stale secondary IP %s from %s NIC %s for MobilityPool/%s", address, provider, nicRef, poolName),
		RiskLevel:      "medium",
		IdempotencyKey: "mobility:" + poolName + ":" + provider + ":" + nicRef + ":unassign-secondary-ip:" + address,
		Parameters: map[string]string{
			"deprovisionSince": since.UTC().Format(time.RFC3339Nano),
		},
		ExpectedEffects: []string{
			fmt.Sprintf("%s NIC %s would stop advertising stale secondary IP %s", provider, nicRef, address),
		},
		Undo: &dynamicconfig.ActionUndo{
			Action:     "assign-secondary-ip",
			Parameters: copyStringMap(target),
		},
	}, nil
}

func providerForwardingDisableActionPlan(poolName string, profile api.CloudProviderProfileSpec, capture api.AddressCapture, captureTarget map[string]string, address string) (dynamicconfig.ActionPlan, error) {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	params, err := forwardingDisableParams(provider)
	if err != nil {
		return dynamicconfig.ActionPlan{}, err
	}
	target := providerActionTarget(poolName, profile, capture, captureTarget, address)
	return dynamicconfig.ActionPlan{
		Name:           safeName("mobility-" + poolName + "-forwarding-disable-" + nicRef),
		Provider:       provider,
		Action:         "ensure-forwarding-disabled",
		Target:         target,
		ProviderRef:    providerRef,
		Mode:           "dry-run",
		Description:    fmt.Sprintf("Disable forwarding on %s NIC %s after MobilityPool/%s no longer captures addresses there", provider, nicRef, poolName),
		RiskLevel:      "medium",
		IdempotencyKey: "mobility:" + poolName + ":" + provider + ":" + nicRef + ":ensure-forwarding-disabled",
		Parameters:     params,
		ExpectedEffects: []string{
			fmt.Sprintf("%s NIC %s would stop forwarding mobility capture traffic", provider, nicRef),
		},
		Undo: &dynamicconfig.ActionUndo{
			Action:     "ensure-forwarding-enabled",
			Parameters: mergeStringMaps(target, mustForwardingParams(provider)),
		},
	}, nil
}

func providerActionTarget(poolName string, profile api.CloudProviderProfileSpec, capture api.AddressCapture, captureTarget map[string]string, address string) map[string]string {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	target := map[string]string{
		"provider":    provider,
		"providerRef": providerRef,
		"nicRef":      nicRef,
		"address":     strings.TrimSpace(address),
	}
	for key, value := range captureTarget {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			target[key] = value
		}
	}
	addProfileTargetFields(target, provider, profile, poolName, address, nicRef)
	target["provider"] = provider
	target["providerRef"] = providerRef
	target["nicRef"] = nicRef
	target["address"] = strings.TrimSpace(address)
	return target
}

func addProfileTargetFields(target map[string]string, provider string, profile api.CloudProviderProfileSpec, poolName, address, nicRef string) {
	if profile.SubscriptionID != "" && strings.TrimSpace(target["subscriptionId"]) == "" {
		target["subscriptionId"] = strings.TrimSpace(profile.SubscriptionID)
	}
	if profile.ResourceGroup != "" && strings.TrimSpace(target["resourceGroup"]) == "" {
		target["resourceGroup"] = strings.TrimSpace(profile.ResourceGroup)
	}
	if provider == "azure" {
		if _, ok := target["nicName"]; !ok {
			if name := azureNICName(nicRef); name != "" {
				target["nicName"] = name
			}
		}
		if _, ok := target["ipConfigName"]; !ok {
			target["ipConfigName"] = safeName(poolName + "-" + address)
		}
	}
}

func forwardingParams(provider string) (map[string]string, error) {
	switch provider {
	case "aws":
		return map[string]string{"sourceDestCheck": "false"}, nil
	case "azure":
		return map[string]string{"ipForwarding": "true"}, nil
	case "oci":
		return map[string]string{"skipSourceDestCheck": "true"}, nil
	case "gcp":
		return map[string]string{"canIpForward": "true"}, nil
	default:
		return nil, fmt.Errorf("provider %q is not supported for mobility action plans", provider)
	}
}

func forwardingDisableParams(provider string) (map[string]string, error) {
	switch provider {
	case "aws":
		return map[string]string{"priorSourceDestCheck": "true"}, nil
	case "azure":
		return map[string]string{"priorIpForwarding": "false"}, nil
	case "oci":
		return map[string]string{"priorSkipSourceDestCheck": "false"}, nil
	case "gcp":
		return map[string]string{"priorCanIpForward": "false"}, nil
	default:
		return nil, fmt.Errorf("provider %q is not supported for mobility action plans", provider)
	}
}

func mustForwardingParams(provider string) map[string]string {
	params, err := forwardingParams(provider)
	if err != nil {
		return map[string]string{}
	}
	return params
}

func deprovisionHoldDuration(spec api.MobilityPoolSpec) time.Duration {
	if strings.TrimSpace(spec.CapturePolicy.DeprovisionHoldDuration) != "" {
		return durationDefault(spec.CapturePolicy.DeprovisionHoldDuration, DefaultDeprovisionHoldDuration)
	}
	if strings.TrimSpace(spec.LeasePolicy.HoldDuration) != "" {
		return durationDefault(spec.LeasePolicy.HoldDuration, DefaultDeprovisionHoldDuration)
	}
	return DefaultDeprovisionHoldDuration
}

func leasesByAddress(leases []routerstate.AddressLeaseRecord) map[string]routerstate.AddressLeaseRecord {
	out := map[string]routerstate.AddressLeaseRecord{}
	for _, lease := range leases {
		out[strings.TrimSpace(lease.Address)] = lease
	}
	return out
}

func deprovisionSince(lease routerstate.AddressLeaseRecord) time.Time {
	switch lease.Status {
	case routerstate.AddressLeaseStatusExpired:
		return firstNonZeroTime(lease.ExpiresAt, lease.UpdatedAt, lease.ObservedAt, lease.RecordedAt)
	case routerstate.AddressLeaseStatusHolding:
		return firstNonZeroTime(lease.CandidateObservedAt, lease.UpdatedAt, lease.ObservedAt, lease.RecordedAt)
	case routerstate.AddressLeaseStatusActive:
		return firstNonZeroTime(lease.ObservedAt, lease.UpdatedAt, lease.RecordedAt)
	default:
		return firstNonZeroTime(lease.UpdatedAt, lease.ObservedAt, lease.RecordedAt)
	}
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func sortedClaims(claims []api.Resource) []api.Resource {
	out := append([]api.Resource(nil), claims...)
	sort.SliceStable(out, func(i, j int) bool {
		left := claimAddress(out[i])
		right := claimAddress(out[j])
		if left == right {
			return out[i].Metadata.Name < out[j].Metadata.Name
		}
		return left < right
	})
	return out
}

func claimAddress(claim api.Resource) string {
	spec, err := claim.RemoteAddressClaimSpec()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(spec.Address)
}

func providerNICKeyFromClaim(claim api.Resource) string {
	spec, err := claim.RemoteAddressClaimSpec()
	if err != nil || spec.Capture.Type != "provider-secondary-ip" {
		return ""
	}
	return providerNICKey("", spec.Capture.ProviderRef, spec.Capture.NICRef)
}

func providerNICKey(provider, providerRef, nicRef string) string {
	providerRef = strings.TrimSpace(providerRef)
	nicRef = strings.TrimSpace(nicRef)
	if providerRef == "" || nicRef == "" {
		return ""
	}
	return strings.TrimSpace(provider) + "\x00" + providerRef + "\x00" + nicRef
}

func captureTargetFromClaim(claim api.Resource) map[string]string {
	out := map[string]string{}
	for key, value := range claim.Metadata.Annotations {
		if strings.HasPrefix(key, captureTargetAnnotationPrefix) {
			targetKey := strings.TrimSpace(strings.TrimPrefix(key, captureTargetAnnotationPrefix))
			if targetKey != "" && strings.TrimSpace(value) != "" {
				out[targetKey] = strings.TrimSpace(value)
			}
		}
	}
	return out
}

func dedupeActionPlans(plans []dynamicconfig.ActionPlan) []dynamicconfig.ActionPlan {
	if len(plans) < 2 {
		return plans
	}
	seen := map[string]bool{}
	out := make([]dynamicconfig.ActionPlan, 0, len(plans))
	for _, plan := range plans {
		key := strings.TrimSpace(plan.IdempotencyKey)
		if key == "" {
			key = strings.TrimSpace(plan.Action) + "\x00" + strings.TrimSpace(plan.Target["providerRef"]) + "\x00" + strings.TrimSpace(plan.Target["nicRef"]) + "\x00" + strings.TrimSpace(plan.Target["address"])
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, plan)
	}
	return out
}

func decodeDynamicConfigResources(raw string) ([]api.Resource, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var resources []api.Resource
	if err := json.Unmarshal([]byte(raw), &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func azureNICName(nicRef string) string {
	parts := strings.Split(strings.Trim(nicRef, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if strings.EqualFold(parts[i], "networkInterfaces") {
			return strings.TrimSpace(parts[i+1])
		}
	}
	if len(parts) > 0 && !strings.Contains(nicRef, "/") {
		return strings.TrimSpace(parts[len(parts)-1])
	}
	return ""
}

func cloudProviderProfiles(router *api.Router) map[string]api.CloudProviderProfileSpec {
	out := map[string]api.CloudProviderProfileSpec{}
	if router == nil {
		return out
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.HybridAPIVersion || res.Kind != "CloudProviderProfile" {
			continue
		}
		spec, err := res.CloudProviderProfileSpec()
		if err != nil {
			continue
		}
		out[res.Metadata.Name] = spec
	}
	return out
}

func trimCapture(c api.MobilityMemberCapture) api.AddressCapture {
	return api.AddressCapture{
		Type:               strings.TrimSpace(c.Type),
		ProviderRef:        strings.TrimSpace(c.ProviderRef),
		ProviderMode:       strings.TrimSpace(c.ProviderMode),
		NICRef:             strings.TrimSpace(c.NICRef),
		ConfigureOSAddress: c.ConfigureOSAddress,
		Interface:          strings.TrimSpace(c.Interface),
		GratuitousARP:      c.GratuitousARP,
		ActiveWhen: api.CaptureActiveWhen{
			Type:              strings.TrimSpace(c.ActiveWhen.Type),
			VirtualAddressRef: strings.TrimSpace(c.ActiveWhen.VirtualAddressRef),
		},
	}
}

func trimDelivery(d api.MobilityMemberDelivery) api.AddressDelivery {
	mode := strings.TrimSpace(d.Mode)
	if mode == "" {
		mode = "route"
	}
	return api.AddressDelivery{
		PeerRef:         strings.TrimSpace(d.PeerRef),
		Mode:            mode,
		TunnelInterface: strings.TrimSpace(d.TunnelInterface),
	}
}

func trimDeliveryTargets(targets []api.MobilityMemberDeliveryTarget) []deliveryTargetPlanInfo {
	out := make([]deliveryTargetPlanInfo, 0, len(targets))
	for _, target := range targets {
		out = append(out, deliveryTargetPlanInfo{
			NodeRef: strings.TrimSpace(target.NodeRef),
			Site:    strings.TrimSpace(target.Site),
			Role:    strings.TrimSpace(target.Role),
			Delivery: api.AddressDelivery{
				PeerRef:         strings.TrimSpace(target.PeerRef),
				Mode:            firstNonEmpty(strings.TrimSpace(target.Mode), "route"),
				TunnelInterface: strings.TrimSpace(target.TunnelInterface),
			},
		})
	}
	return out
}

func dynamicPartRecord(part dynamicconfig.DynamicConfigPart) (routerstate.DynamicConfigPartRecord, error) {
	resources, err := json.Marshal(part.Spec.Resources)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	directives, err := json.Marshal(part.Spec.Directives)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	var actionPlansJSON string
	if len(part.Spec.ActionPlans) > 0 {
		data, err := json.Marshal(part.Spec.ActionPlans)
		if err != nil {
			return routerstate.DynamicConfigPartRecord{}, err
		}
		actionPlansJSON = string(data)
	}
	return routerstate.DynamicConfigPartRecord{
		Source:          part.Spec.Source,
		Generation:      part.Spec.Generation,
		ObservedAt:      part.Spec.ObservedAt,
		ExpiresAt:       part.Spec.ExpiresAt,
		Digest:          part.Spec.Digest,
		ResourcesJSON:   string(resources),
		DirectivesJSON:  string(directives),
		ActionPlansJSON: actionPlansJSON,
		Status:          "active",
	}, nil
}

func cloneResource(res api.Resource) api.Resource {
	out := res
	out.Metadata.Annotations = copyStringMap(res.Metadata.Annotations)
	out.Metadata.OwnerRefs = append([]api.OwnerRef(nil), res.Metadata.OwnerRefs...)
	return out
}

func digestDynamicPart(part dynamicconfig.DynamicConfigPart) string {
	type digestSpec struct {
		Resources   []api.Resource                         `json:"resources"`
		Directives  []dynamicconfig.DynamicConfigDirective `json:"directives"`
		ActionPlans []dynamicconfig.ActionPlan             `json:"actionPlans"`
	}
	data, _ := json.Marshal(digestSpec{
		Resources:   part.Spec.Resources,
		Directives:  part.Spec.Directives,
		ActionPlans: part.Spec.ActionPlans,
	})
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func safeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "mobility"
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeStringMaps(a, b map[string]string) map[string]string {
	out := copyStringMap(a)
	for k, v := range b {
		out[k] = v
	}
	return out
}
