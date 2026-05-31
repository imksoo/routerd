// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const (
	dynamicGeneration = int64(1)
	dynamicSourceKind = "MobilityPool"
)

// PlannerInput is the pure lease-to-dynamic-config planning input for one
// MobilityPool on one routerd node.
type PlannerInput struct {
	PoolName         string
	PoolSpec         api.MobilityPoolSpec
	SelfNode         string
	Now              time.Time
	Leases           []routerstate.AddressLeaseRecord
	ProviderProfiles map[string]api.CloudProviderProfileSpec
}

// PlannerOutput is the deterministic generated config for one pool x node.
type PlannerOutput struct {
	Part        dynamicconfig.DynamicConfigPart
	Claims      []api.Resource
	ActionPlans []dynamicconfig.ActionPlan
}

type memberPlanInfo struct {
	NodeRef  string
	Site     string
	Role     string
	Capture  api.AddressCapture
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

	claims := []api.Resource{}
	plans := []dynamicconfig.ActionPlan{}
	forwardingSeen := map[string]bool{}
	minExpiresAt := time.Time{}
	for _, lease := range sortedLeases(in.Leases) {
		claim, actionPlans, leaseExpiresAt, ok, err := planLease(poolName, prefix, self, members, lease, in.ProviderProfiles, now, forwardingSeen)
		if err != nil {
			return PlannerOutput{}, err
		}
		if !ok {
			continue
		}
		claims = append(claims, claim)
		plans = append(plans, actionPlans...)
		if !leaseExpiresAt.IsZero() && (minExpiresAt.IsZero() || leaseExpiresAt.Before(minExpiresAt)) {
			minExpiresAt = leaseExpiresAt
		}
	}

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
	return PlannerOutput{Part: part, Claims: claims, ActionPlans: plans}, nil
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
	out, err := PlanDynamicConfig(PlannerInput{
		PoolName:         res.Metadata.Name,
		PoolSpec:         spec,
		SelfNode:         selfNode,
		Now:              now,
		Leases:           leases,
		ProviderProfiles: cloudProviderProfiles(c.Router),
	})
	if err != nil {
		_ = c.upsertEmptyPlan(res.Metadata.Name, spec, selfNode, now)
		return err
	}
	record, err := dynamicPartRecord(out.Part)
	if err != nil {
		return err
	}
	if err := c.Store.UpsertDynamicConfigPart(record); err != nil {
		return fmt.Errorf("upsert dynamic config part: %w", err)
	}
	return c.savePlannerStatus(res.Metadata.Name, map[string]any{
		"plannerPhase":       "Planned",
		"plannerReason":      "",
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
	})
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

func planLease(poolName string, prefix netip.Prefix, self memberPlanInfo, members map[string]memberPlanInfo, lease routerstate.AddressLeaseRecord, profiles map[string]api.CloudProviderProfileSpec, now time.Time, forwardingSeen map[string]bool) (api.Resource, []dynamicconfig.ActionPlan, time.Time, bool, error) {
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
	if strings.TrimSpace(self.Capture.Type) == "" {
		return api.Resource{}, nil, time.Time{}, false, fmt.Errorf("MobilityPool/%s member %q capture is required to plan remote lease %s", poolName, self.NodeRef, lease.Address)
	}
	if strings.TrimSpace(self.Delivery.PeerRef) == "" {
		return api.Resource{}, nil, time.Time{}, false, fmt.Errorf("MobilityPool/%s member %q delivery.peerRef is required to plan remote lease %s", poolName, self.NodeRef, lease.Address)
	}
	claim := claimResource(poolName, self, lease, address, ownerRole)
	var plans []dynamicconfig.ActionPlan
	if self.Capture.Type == "provider-secondary-ip" {
		profile, ok := profiles[strings.TrimSpace(self.Capture.ProviderRef)]
		if !ok {
			return api.Resource{}, nil, time.Time{}, false, fmt.Errorf("CloudProviderProfile/%s not found for MobilityPool/%s member %q", self.Capture.ProviderRef, poolName, self.NodeRef)
		}
		generated, err := providerActionPlans(poolName, profile, self.Capture, address, forwardingSeen)
		if err != nil {
			return api.Resource{}, nil, time.Time{}, false, err
		}
		plans = append(plans, generated...)
	}
	return claim, plans, lease.ExpiresAt, true, nil
}

func plannerMembers(members []api.MobilityPoolMember) map[string]memberPlanInfo {
	out := map[string]memberPlanInfo{}
	for _, member := range members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		out[nodeRef] = memberPlanInfo{
			NodeRef:  nodeRef,
			Site:     strings.TrimSpace(member.Site),
			Role:     strings.TrimSpace(member.Role),
			Capture:  trimCapture(member.Capture),
			Delivery: trimDelivery(member.Delivery),
		}
	}
	return out
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

func claimResource(poolName string, self memberPlanInfo, lease routerstate.AddressLeaseRecord, address, ownerRole string) api.Resource {
	claimName := safeName("mobility-" + poolName + "-" + address)
	annotations := map[string]string{
		"mobility.routerd.net/lease-epoch":     fmt.Sprint(lease.Epoch),
		"mobility.routerd.net/owner-node":      strings.TrimSpace(lease.OwnerNode),
		"mobility.routerd.net/owner-site":      strings.TrimSpace(lease.OwnerSite),
		"mobility.routerd.net/owner-role":      ownerRole,
		"mobility.routerd.net/source-event-id": strings.TrimSpace(lease.SourceEventID),
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
			Delivery:  self.Delivery,
		},
	}
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

func providerActionPlans(poolName string, profile api.CloudProviderProfileSpec, capture api.AddressCapture, address string, forwardingSeen map[string]bool) ([]dynamicconfig.ActionPlan, error) {
	provider := strings.TrimSpace(profile.Provider)
	providerRef := strings.TrimSpace(capture.ProviderRef)
	nicRef := strings.TrimSpace(capture.NICRef)
	target := map[string]string{
		"provider":    provider,
		"providerRef": providerRef,
		"nicRef":      nicRef,
		"address":     address,
	}
	addProfileTargetFields(target, provider, profile, poolName, address, nicRef)
	assign := dynamicconfig.ActionPlan{
		Name:           safeName("mobility-" + poolName + "-assign-" + address),
		Provider:       provider,
		Action:         "assign-secondary-ip",
		Target:         target,
		ProviderRef:    providerRef,
		Mode:           "dry-run",
		Description:    fmt.Sprintf("Assign %s as a secondary IP on %s NIC %s for MobilityPool/%s", address, provider, nicRef, poolName),
		RiskLevel:      "medium",
		IdempotencyKey: "mobility:" + poolName + ":" + provider + ":" + nicRef + ":assign-secondary-ip:" + address,
		ExpectedEffects: []string{
			fmt.Sprintf("%s NIC %s would advertise secondary IP %s", provider, nicRef, address),
		},
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
		delete(fwdTarget, "address")
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

func addProfileTargetFields(target map[string]string, provider string, profile api.CloudProviderProfileSpec, poolName, address, nicRef string) {
	if profile.SubscriptionID != "" {
		target["subscriptionId"] = strings.TrimSpace(profile.SubscriptionID)
	}
	if profile.ResourceGroup != "" {
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
