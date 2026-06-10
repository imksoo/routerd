// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	dynamicGeneration  = int64(1)
	dynamicSourceKind  = "MobilityPool"
	captureParamHolder = "mobilityCaptureHolder"

	captureStrategySecondaryIP = "secondary-ip"
	captureStrategyRouteTable  = "route-table"

	actionAssignSecondaryIP       = "assign-secondary-ip"
	actionUnassignSecondaryIP     = "unassign-secondary-ip"
	actionAssignRouteTableRoute   = "assign-route-table-route"
	actionUnassignRouteTableRoute = "unassign-route-table-route"
)

type PlacementDecision struct {
	Group                 string
	Active                bool
	ActiveNode            string
	Reason                string
	Seize                 bool
	LivenessObserved      bool
	SelfCommunity         string
	SelfMarker            string
	SelfMarkerPresent     bool
	ActiveCommunity       string
	ActiveMarker          string
	ActiveMarkerPresent   bool
	ActiveIdentityNodeRef string
	SeizeHoldDown         bool
	SeizeHoldDownKey      string
	SeizeHoldDownSince    time.Time
	SeizeHoldDownUntil    time.Time
}

func (d PlacementDecision) NoCandidate() bool {
	return d.Group != "" && d.ActiveNode == ""
}

type memberPlanInfo struct {
	NodeRef            string
	Site               string
	Role               string
	Capture            api.AddressCapture
	CaptureTarget      map[string]string
	Delivery           api.AddressDelivery
	DeliveryTo         []deliveryTargetPlanInfo
	OwnershipDiscovery api.MobilityOwnershipDiscovery
	PlacementGroup     string
	PlacementPriority  int
	MaintenanceDrain   bool
}

type deliveryTargetPlanInfo struct {
	NodeRef  string
	Site     string
	Role     string
	Delivery api.AddressDelivery
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
			ExpiresAt:   now.Add(DefaultLeaseTTL),
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
	return routerSelfNode(c.Router, groupRef)
}

func routerSelfNode(router *api.Router, groupRef string) (string, error) {
	groupRef = strings.TrimSpace(groupRef)
	if groupRef == "" {
		return "", fmt.Errorf("groupRef is required")
	}
	if router == nil {
		return "", fmt.Errorf("EventGroup/%s not found for mobility planning", groupRef)
	}
	for _, res := range router.Spec.Resources {
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

func plannerMembers(members []api.MobilityPoolMember) map[string]memberPlanInfo {
	out := map[string]memberPlanInfo{}
	priorities := autoPlacementPriorities(members)
	for _, member := range members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		capture := trimCapture(member.Capture)
		discovery := member.OwnershipDiscovery
		if strings.TrimSpace(discovery.ProviderRef) == "" {
			discovery.ProviderRef = strings.TrimSpace(capture.ProviderRef)
		}
		out[nodeRef] = memberPlanInfo{
			NodeRef:            nodeRef,
			Site:               strings.TrimSpace(member.Site),
			Role:               strings.TrimSpace(member.Role),
			Capture:            capture,
			CaptureTarget:      copyStringMap(member.Capture.Target),
			Delivery:           trimDelivery(member.Delivery),
			DeliveryTo:         trimDeliveryTargets(member.DeliveryTo),
			OwnershipDiscovery: discovery,
			PlacementGroup:     strings.TrimSpace(member.Placement.Group),
			PlacementPriority:  priorities[nodeRef],
			MaintenanceDrain:   member.Maintenance.Drain,
		}
	}
	return out
}

func autoPlacementPriorities(members []api.MobilityPoolMember) map[string]int {
	out := map[string]int{}
	usedByGroup := map[string]map[int]bool{}
	for _, member := range members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		priority := member.Placement.Priority
		out[nodeRef] = priority
		group := strings.TrimSpace(member.Placement.Group)
		if group == "" || priority == 0 {
			continue
		}
		if usedByGroup[group] == nil {
			usedByGroup[group] = map[int]bool{}
		}
		usedByGroup[group][priority] = true
	}
	nextByGroup := map[string]int{}
	for _, member := range members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		group := strings.TrimSpace(member.Placement.Group)
		if group == "" || out[nodeRef] != 0 {
			continue
		}
		if usedByGroup[group] == nil {
			usedByGroup[group] = map[int]bool{}
		}
		next := nextByGroup[group]
		if next == 0 {
			next = 10
		}
		for usedByGroup[group][next] {
			next += 10
		}
		out[nodeRef] = next
		usedByGroup[group][next] = true
		nextByGroup[group] = next + 10
	}
	return out
}

func evaluatePlacement(self memberPlanInfo, members map[string]memberPlanInfo) PlacementDecision {
	group := strings.TrimSpace(self.PlacementGroup)
	if group == "" {
		return PlacementDecision{Active: true, ActiveNode: self.NodeRef}
	}
	candidates := make([]memberPlanInfo, 0, len(members))
	for _, member := range members {
		if strings.TrimSpace(member.PlacementGroup) != group || member.MaintenanceDrain {
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

func normalizeAddressString(address string) string {
	return strings.TrimSpace(address)
}

func providerActionPlans(poolName string, profile api.CloudProviderProfileSpec, capture api.AddressCapture, captureTarget map[string]string, address string, forwardingSeen map[string]bool, seize bool) ([]dynamicconfig.ActionPlan, error) {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	strategy := effectiveCaptureStrategy(provider, capture.Strategy)
	if err := validateProviderCaptureStrategy(provider, strategy); err != nil {
		return nil, err
	}
	target := map[string]string{
		"provider":        provider,
		"providerRef":     providerRef,
		"nicRef":          nicRef,
		"address":         address,
		"captureStrategy": strategy,
	}
	for key, value := range captureTarget {
		target[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	addProfileTargetFields(target, provider, profile, poolName, address, nicRef)
	target["provider"] = provider
	target["providerRef"] = providerRef
	target["nicRef"] = nicRef
	target["address"] = address
	target["captureStrategy"] = strategy
	assignAction, unassignAction := providerCaptureActions(strategy)
	assignDescription := fmt.Sprintf("Assign %s as a secondary IP on %s NIC %s for MobilityPool/%s", address, provider, nicRef, poolName)
	assignRisk := "medium"
	assignEffects := []string{
		fmt.Sprintf("%s NIC %s would advertise secondary IP %s", provider, nicRef, address),
	}
	if strategy == captureStrategyRouteTable {
		routeTableRef := strings.TrimSpace(target["routeTableRef"])
		if routeTableRef == "" {
			return nil, fmt.Errorf("capture.strategy route-table requires capture.target.routeTableRef")
		}
		if provider == "azure" && strings.TrimSpace(target["nextHopIPAddress"]) == "" {
			return nil, fmt.Errorf("provider azure capture.strategy route-table requires capture.target.nextHopIPAddress")
		}
		assignDescription = fmt.Sprintf("Route %s in %s route table %s to NIC %s for MobilityPool/%s", address, provider, routeTableRef, nicRef, poolName)
		assignEffects = []string{
			fmt.Sprintf("%s route table %s would send %s to NIC %s", provider, routeTableRef, address, nicRef),
		}
	}
	var assignParams map[string]string
	if seize {
		assignDescription = fmt.Sprintf("Seize/reassign %s capture on %s for MobilityPool/%s after capture failover", address, provider, poolName)
		assignRisk = "high"
		assignParams = map[string]string{}
		assignParams["allowReassignment"] = "true"
		assignEffects = []string{
			fmt.Sprintf("%s would seize %s from any previous holder", provider, address),
		}
	}
	assign := dynamicconfig.ActionPlan{
		Name:            safeName("mobility-" + poolName + "-assign-" + address),
		Provider:        provider,
		Action:          assignAction,
		Target:          target,
		ProviderRef:     providerRef,
		Mode:            "dry-run",
		Description:     assignDescription,
		RiskLevel:       assignRisk,
		IdempotencyKey:  "mobility:" + poolName + ":" + provider + ":" + providerCaptureTargetRef(strategy, target) + ":" + assignAction + ":" + address,
		Parameters:      assignParams,
		ExpectedEffects: assignEffects,
		Undo: &dynamicconfig.ActionUndo{
			Action:     unassignAction,
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

func providerUnassignActionPlan(poolName string, profile api.CloudProviderProfileSpec, capture api.AddressCapture, captureTarget map[string]string, address string, since time.Time) (dynamicconfig.ActionPlan, error) {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	strategy := effectiveCaptureStrategy(provider, capture.Strategy)
	if err := validateProviderCaptureStrategy(provider, strategy); err != nil {
		return dynamicconfig.ActionPlan{}, err
	}
	assignAction, unassignAction := providerCaptureActions(strategy)
	target := providerActionTarget(poolName, profile, capture, captureTarget, address)
	target["captureStrategy"] = strategy
	description := fmt.Sprintf("Unassign stale secondary IP %s from %s NIC %s for MobilityPool/%s", address, provider, nicRef, poolName)
	effects := []string{
		fmt.Sprintf("%s NIC %s would stop advertising stale secondary IP %s", provider, nicRef, address),
	}
	if strategy == captureStrategyRouteTable {
		description = fmt.Sprintf("Remove stale route for %s from %s route table %s for MobilityPool/%s", address, provider, target["routeTableRef"], poolName)
		effects = []string{
			fmt.Sprintf("%s route table %s would stop sending stale %s to NIC %s", provider, target["routeTableRef"], address, nicRef),
		}
	}
	return dynamicconfig.ActionPlan{
		Name:           safeName("mobility-" + poolName + "-unassign-" + address),
		Provider:       provider,
		Action:         unassignAction,
		Target:         target,
		ProviderRef:    providerRef,
		Mode:           "dry-run",
		Description:    description,
		RiskLevel:      "medium",
		IdempotencyKey: "mobility:" + poolName + ":" + provider + ":" + providerCaptureTargetRef(strategy, target) + ":" + unassignAction + ":" + address,
		Parameters: map[string]string{
			"deprovisionSince": since.UTC().Format(time.RFC3339Nano),
		},
		ExpectedEffects: effects,
		Undo: &dynamicconfig.ActionUndo{
			Action:     assignAction,
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
		"provider":        provider,
		"providerRef":     providerRef,
		"nicRef":          nicRef,
		"address":         strings.TrimSpace(address),
		"captureStrategy": effectiveCaptureStrategy(provider, capture.Strategy),
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
	target["captureStrategy"] = effectiveCaptureStrategy(provider, capture.Strategy)
	return target
}

func effectiveCaptureStrategy(provider, strategy string) string {
	strategy = strings.TrimSpace(strategy)
	if strategy != "" {
		return strategy
	}
	return captureStrategySecondaryIP
}

func validateProviderCaptureStrategy(provider, strategy string) error {
	switch strings.TrimSpace(strategy) {
	case captureStrategySecondaryIP:
		return nil
	case captureStrategyRouteTable:
		switch strings.TrimSpace(provider) {
		case "aws", "azure":
			return nil
		default:
			return fmt.Errorf("provider %q does not support capture.strategy route-table", provider)
		}
	default:
		return fmt.Errorf("capture.strategy %q is not supported", strategy)
	}
}

func providerCaptureActions(strategy string) (assign, unassign string) {
	switch strings.TrimSpace(strategy) {
	case captureStrategyRouteTable:
		return actionAssignRouteTableRoute, actionUnassignRouteTableRoute
	default:
		return actionAssignSecondaryIP, actionUnassignSecondaryIP
	}
}

func isProviderCaptureAssignAction(action string) bool {
	action = strings.TrimSpace(action)
	return action == actionAssignSecondaryIP || action == actionAssignRouteTableRoute
}

func isProviderCaptureUnassignAction(action string) bool {
	action = strings.TrimSpace(action)
	return action == actionUnassignSecondaryIP || action == actionUnassignRouteTableRoute
}

func isProviderCaptureAction(action string) bool {
	return isProviderCaptureAssignAction(action) || isProviderCaptureUnassignAction(action)
}

func providerCaptureTargetRef(strategy string, target map[string]string) string {
	if strings.TrimSpace(strategy) == captureStrategyRouteTable {
		if value := strings.TrimSpace(target["routeTableRef"]); value != "" {
			return value
		}
	}
	return strings.TrimSpace(target["nicRef"])
}

func providerCaptureRefFromTarget(target map[string]string) string {
	return providerCaptureTargetRef(strings.TrimSpace(target["captureStrategy"]), target)
}

func providerCaptureRefFromCapture(capture api.AddressCapture, target map[string]string) string {
	if strings.TrimSpace(capture.Strategy) == captureStrategyRouteTable {
		if target != nil {
			if value := strings.TrimSpace(target["routeTableRef"]); value != "" {
				return value
			}
		}
		return ""
	}
	if target != nil {
		if value := providerCaptureRefFromTarget(target); value != "" {
			return value
		}
	}
	return strings.TrimSpace(capture.NICRef)
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
		if _, ok := target["routeName"]; !ok {
			target["routeName"] = safeName(poolName + "-" + address)
		}
		if _, ok := target["routeTableName"]; !ok {
			if name := azureResourceName(target["routeTableRef"]); name != "" {
				target["routeTableName"] = name
			}
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

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func providerNICKey(provider, providerRef, nicRef string) string {
	providerRef = strings.TrimSpace(providerRef)
	nicRef = strings.TrimSpace(nicRef)
	if providerRef == "" || nicRef == "" {
		return ""
	}
	return strings.TrimSpace(provider) + "\x00" + providerRef + "\x00" + nicRef
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
	return azureResourceName(nicRef)
}

func azureResourceName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if strings.EqualFold(parts[i], "networkInterfaces") || strings.EqualFold(parts[i], "routeTables") {
			return strings.TrimSpace(parts[i+1])
		}
	}
	if len(parts) > 0 && !strings.Contains(ref, "/") {
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
		Strategy:           strings.TrimSpace(c.Strategy),
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
