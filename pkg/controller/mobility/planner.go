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

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	dynamicGeneration  = int64(1)
	dynamicSourceKind  = "MobilityPool"
	captureParamHolder = "mobilityCaptureHolder"

	captureAssignmentGenerationParam     = "mobilityAssignmentGeneration"
	captureAssignmentDesiredHolderParam  = "mobilityAssignmentDesiredHolder"
	captureAssignmentPreviousHolderParam = "mobilityAssignmentPreviousHolder"
	captureAssignmentLeaseUntilParam     = "mobilityAssignmentLeaseUntil"
	captureAssignmentReasonParam         = "mobilityAssignmentReason"

	captureStrategySecondaryIP = "secondary-ip"
	captureStrategyRouteTable  = "route-table"

	actionAssignSecondaryIP       = "assign-secondary-ip"
	actionUnassignSecondaryIP     = "unassign-secondary-ip"
	actionAssignRouteTableRoute   = "assign-route-table-route"
	actionUnassignRouteTableRoute = "unassign-route-table-route"
)

type dynamicConfigPartSourceReader interface {
	GetDynamicConfigPartsBySource(string) ([]routerstate.DynamicConfigPartRecord, error)
}

// previousGeneratedActionPlans returns only the currently active provider
// plan. Its revision is retained even when that part is empty or expired so a
// later reintroduction receives a new assignment generation instead of
// reviving a withdrawn fence. Both controller shells use this exact read, so
// discovery and planning cannot disagree about which prior plan is current.
func previousGeneratedActionPlans(store dynamicConfigPartSourceReader, poolName, selfNode string, now time.Time) ([]dynamicconfig.ActionPlan, string, error) {
	source := DynamicSource(poolName, selfNode)
	parts, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return nil, "", fmt.Errorf("get previous dynamic config part %s: %w", source, err)
	}
	if len(parts) == 0 {
		return nil, "", nil
	}
	sort.SliceStable(parts, func(i, j int) bool {
		if parts[i].Generation == parts[j].Generation {
			return parts[i].UpdatedAt.After(parts[j].UpdatedAt)
		}
		return parts[i].Generation > parts[j].Generation
	})
	latest := parts[0]
	revision := dynamicConfigPartRevision(latest, now)
	if latest.EffectiveStatus(now) != "active" || strings.TrimSpace(latest.ActionPlansJSON) == "" {
		return nil, revision, nil
	}
	plans, err := codec.DecodeActionPlans(latest.ActionPlansJSON)
	if err != nil {
		return nil, "", fmt.Errorf("decode previous dynamic config part action plans %s: %w", source, err)
	}
	return plans, revision, nil
}

func dynamicConfigPartRevision(part routerstate.DynamicConfigPartRecord, now time.Time) string {
	return strings.Join([]string{
		fmt.Sprintf("id=%d", part.ID),
		fmt.Sprintf("generation=%d", part.Generation),
		"digest=" + strings.TrimSpace(part.Digest),
		"observedAt=" + part.ObservedAt.UTC().Format(time.RFC3339Nano),
		"updatedAt=" + part.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"effective=" + part.EffectiveStatus(now),
	}, "\x00")
}

// DynamicSource is the stable DynamicConfigPart source for one pool x node. The
// planner always writes generation 1 for this source and replaces the complete
// generated resource set on every reconcile.
func DynamicSource(poolName, selfNode string) string {
	return dynamicSourceKind + "/" + strings.TrimSpace(poolName) + "/node/" + strings.TrimSpace(selfNode)
}

func (c Controller) savePlannerStatus(poolName string, updates map[string]any) error {
	return saveMergedObjectStatus(c.Store, api.MobilityAPIVersion, "MobilityPool", poolName, updates)
}

func normalizeAddressString(address string) string {
	return strings.TrimSpace(address)
}

func providerActionPlans(poolName string, profile api.CloudProviderProfileSpec, capture api.MobilityMemberCapture, address string, forwardingSeen map[string]bool, seize bool) ([]dynamicconfig.ActionPlan, error) {
	assign, err := providerCaptureActionPlan(poolName, profile, capture, address, true, seize, time.Time{})
	if err != nil {
		return nil, err
	}
	plans := []dynamicconfig.ActionPlan{assign}

	provider, providerRef, nicRef := assign.Provider, assign.ProviderRef, assign.Target["nicRef"]
	forwardingKey := provider + "\x00" + providerRef + "\x00" + nicRef
	if !forwardingSeen[forwardingKey] {
		params, err := forwardingParams(provider)
		if err != nil {
			return nil, err
		}
		fwdTarget := copyStringMap(assign.Target)
		undoParameters := copyStringMap(fwdTarget)
		for key, value := range params {
			undoParameters[key] = value
		}
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
				Parameters: undoParameters,
			},
		})
	}
	return plans, nil
}

// providerCaptureActionPlan is the only construction path for an individual
// provider capture mutation. Assign and release share their target, action
// identity, undo, and provider strategy validation; only their safe effect
// description and transition parameters differ.
func providerCaptureActionPlan(poolName string, profile api.CloudProviderProfileSpec, capture api.MobilityMemberCapture, address string, assign, seize bool, since time.Time) (dynamicconfig.ActionPlan, error) {
	target := providerActionTarget(poolName, profile, capture, address)
	provider, providerRef, nicRef := target["provider"], target["providerRef"], target["nicRef"]
	strategy := providerCaptureStrategy(capture)
	if strategy == captureStrategyRouteTable {
		if err := validateRouteTableCaptureProvider(provider); err != nil {
			return dynamicconfig.ActionPlan{}, err
		}
	}
	action, undoAction := actionAssignSecondaryIP, actionUnassignSecondaryIP
	if strategy == captureStrategyRouteTable {
		action, undoAction = actionAssignRouteTableRoute, actionUnassignRouteTableRoute
	}
	description, effects, risk := "", []string(nil), "medium"
	parameters := map[string]string(nil)
	if assign {
		var err error
		description, effects, err = providerAssignActionDetails(poolName, provider, nicRef, address, strategy, target)
		if err != nil {
			return dynamicconfig.ActionPlan{}, err
		}
		if seize {
			description = fmt.Sprintf("Seize/reassign %s capture on %s for MobilityPool/%s after capture failover", address, provider, poolName)
			effects = []string{fmt.Sprintf("%s would seize %s from any previous holder", provider, address)}
			risk = "high"
			parameters = map[string]string{"allowReassignment": "true"}
		}
	} else {
		action, undoAction = undoAction, action
		description, effects = providerUnassignActionDetails(poolName, provider, nicRef, address, strategy, target)
		parameters = map[string]string{"deprovisionSince": since.UTC().Format(time.RFC3339Nano)}
	}
	return dynamicconfig.ActionPlan{
		Name:            safeName("mobility-" + poolName + "-" + strings.TrimSuffix(action, "-secondary-ip") + "-" + address),
		Provider:        provider,
		Action:          action,
		Target:          target,
		ProviderRef:     providerRef,
		Mode:            "dry-run",
		Description:     description,
		RiskLevel:       risk,
		IdempotencyKey:  "mobility:" + poolName + ":" + provider + ":" + providerCaptureTargetRef(strategy, target) + ":" + action + ":" + address,
		Parameters:      parameters,
		ExpectedEffects: effects,
		Undo: &dynamicconfig.ActionUndo{
			Action:     undoAction,
			Parameters: copyStringMap(target),
		},
	}, nil
}

func providerAssignActionDetails(poolName, provider, nicRef, address, strategy string, target map[string]string) (string, []string, error) {
	switch strings.TrimSpace(strategy) {
	case captureStrategyRouteTable:
		routeTableRef, err := validateRouteTableCaptureTarget(provider, target)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Route %s in %s route table %s to NIC %s for MobilityPool/%s", address, provider, routeTableRef, nicRef, poolName), []string{
			fmt.Sprintf("%s route table %s would send %s to NIC %s", provider, routeTableRef, address, nicRef),
		}, nil
	default:
		return fmt.Sprintf("Assign %s as a secondary IP on %s NIC %s for MobilityPool/%s", address, provider, nicRef, poolName), []string{
			fmt.Sprintf("%s NIC %s would advertise secondary IP %s", provider, nicRef, address),
		}, nil
	}
}

func providerUnassignActionDetails(poolName, provider, nicRef, address, strategy string, target map[string]string) (string, []string) {
	if strings.TrimSpace(strategy) == captureStrategyRouteTable {
		routeTableRef := strings.TrimSpace(target["routeTableRef"])
		return fmt.Sprintf("Remove stale route for %s from %s route table %s for MobilityPool/%s", address, provider, routeTableRef, poolName), []string{
			fmt.Sprintf("%s route table %s would stop sending stale %s to NIC %s", provider, routeTableRef, address, nicRef),
		}
	}
	return fmt.Sprintf("Unassign stale secondary IP %s from %s NIC %s for MobilityPool/%s", address, provider, nicRef, poolName), []string{
		fmt.Sprintf("%s NIC %s would stop advertising stale secondary IP %s", provider, nicRef, address),
	}
}

func validateRouteTableCaptureTarget(provider string, target map[string]string) (string, error) {
	routeTableRef := strings.TrimSpace(target["routeTableRef"])
	if routeTableRef == "" {
		return "", fmt.Errorf("capture.captureStrategy route-table requires capture.target.routeTableRef")
	}
	if (provider == "azure" || provider == "oci") && strings.TrimSpace(target["nextHopIPAddress"]) == "" {
		return "", fmt.Errorf("provider %s capture.captureStrategy route-table requires capture.target.nextHopIPAddress", provider)
	}
	return routeTableRef, nil
}

func providerActionTarget(poolName string, profile api.CloudProviderProfileSpec, capture api.MobilityMemberCapture, address string) map[string]string {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	target := copyStringMap(capture.Target)
	addProfileTargetFields(target, provider, profile, poolName, address, nicRef)
	target["provider"] = provider
	target["providerRef"] = providerRef
	target["nicRef"] = nicRef
	target["address"] = strings.TrimSpace(address)
	target["captureStrategy"] = providerCaptureStrategy(capture)
	return target
}

func providerCaptureStrategy(capture api.MobilityMemberCapture) string {
	if strings.TrimSpace(capture.CaptureStrategy) == captureStrategyRouteTable {
		return captureStrategyRouteTable
	}
	return captureStrategySecondaryIP
}

func actionPlanCaptureStrategy(plan dynamicconfig.ActionPlan) string {
	if strings.TrimSpace(plan.Target["captureStrategy"]) == captureStrategyRouteTable {
		return captureStrategyRouteTable
	}
	return captureStrategySecondaryIP
}

func validateRouteTableCaptureProvider(provider string) error {
	switch strings.TrimSpace(provider) {
	case "aws", "azure", "oci":
		return nil
	default:
		return fmt.Errorf("provider %q does not support capture.captureStrategy route-table", provider)
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

func providerCaptureRefFromCapture(capture api.MobilityMemberCapture) string {
	if providerCaptureStrategy(capture) == captureStrategyRouteTable {
		return strings.TrimSpace(capture.Target["routeTableRef"])
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
			if name := azureResourceName(nicRef); name != "" {
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

func digestDynamicPart(part dynamicconfig.DynamicConfigPart) string {
	type digestSpec struct {
		Resources          []api.Resource                         `json:"resources"`
		Directives         []dynamicconfig.DynamicConfigDirective `json:"directives"`
		ActionPlans        []dynamicconfig.ActionPlan             `json:"actionPlans"`
		MobilityDataplane  dynamicconfig.MobilityDataplanePlan    `json:"mobilityDataplane"`
		ARPObserverIntents []dynamicconfig.ARPObserverIntent      `json:"arpObserverIntents"`
		FIBVerdicts        []dynamicconfig.FIBVerdict             `json:"fibVerdicts"`
	}
	data, _ := json.Marshal(digestSpec{
		Resources:          part.Spec.Resources,
		Directives:         part.Spec.Directives,
		ActionPlans:        part.Spec.ActionPlans,
		MobilityDataplane:  part.Spec.MobilityDataplane,
		ARPObserverIntents: part.Spec.ARPObserverIntents,
		FIBVerdicts:        part.Spec.FIBVerdicts,
	})
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func safeName(value string) string {
	return stringutil.ConservativeName(value, "mobility")
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
