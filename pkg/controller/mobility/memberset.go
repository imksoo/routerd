// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

const mobilityMemberSetSourceKind = "mobility-member-set"

type mobilityMembersFromStatus struct {
	Resource    string `json:"resource"`
	Optional    bool   `json:"optional,omitempty"`
	Phase       string `json:"phase"`
	MemberCount int    `json:"memberCount,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type mobilityMembersResolution struct {
	Spec                api.MobilityPoolSpec
	MembersFrom         []mobilityMembersFromStatus
	PendingSources      []string
	ResolvedMemberCount int
}

type mobilityMemberResolver struct {
	Router *api.Router
	Sync   *PeerGroupSyncClient
}

func (r mobilityMemberResolver) resolve(ctx context.Context, spec api.MobilityPoolSpec) (mobilityMembersResolution, error) {
	members := []api.MobilityPoolMember{}
	indexByNode := map[string]int{}
	statuses := make([]mobilityMembersFromStatus, 0, len(spec.MembersFrom))
	pending := []string{}
	addMember := func(member api.MobilityPoolMember) {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if existing, ok := indexByNode[nodeRef]; ok {
			merged := mergeMobilityPoolMembers(members[existing], member)
			merged.StaticOwnedAddresses = mergeStringSet(members[existing].StaticOwnedAddresses, member.StaticOwnedAddresses)
			members[existing] = merged
			return
		}
		indexByNode[nodeRef] = len(members)
		members = append(members, member)
	}
	for _, source := range spec.MembersFrom {
		ref := strings.TrimSpace(source.Resource)
		status := mobilityMembersFromStatus{
			Resource: ref,
			Optional: source.Optional,
			Phase:    "Resolved",
		}
		set, found, err := r.memberSet(ref)
		if err != nil {
			status.Phase = "Invalid"
			status.Reason = err.Error()
			statuses = append(statuses, status)
			return mobilityMembersResolution{MembersFrom: statuses, PendingSources: pending}, err
		}
		if !found {
			status.Phase = "Missing"
			status.Reason = "MobilityMemberSet not found"
			if !source.Optional && r.Sync != nil {
				setName := strings.TrimSpace(nameFromMemberSetRef(ref))
				synced, ok, syncErr := r.Sync.SyncMemberSet(ctx, r.Router, setName)
				if syncErr != nil {
					status.Reason = "MobilityMemberSet not found; sync failed: " + syncErr.Error()
				}
				if ok {
					status.Phase = "Synced"
					status.Reason = ""
					status.MemberCount = len(synced.Members)
					for _, member := range synced.Members {
						addMember(mobilityPoolMemberFromSetMember(member))
					}
					statuses = append(statuses, status)
					continue
				}
			}
			statuses = append(statuses, status)
			if !source.Optional {
				pending = append(pending, ref)
			}
			continue
		}
		status.MemberCount = len(set.Members)
		for _, member := range set.Members {
			addMember(mobilityPoolMemberFromSetMember(member))
		}
		statuses = append(statuses, status)
	}
	for _, member := range spec.Members {
		addMember(member)
	}
	sort.Strings(pending)
	spec.Members = members
	return mobilityMembersResolution{
		Spec:                spec,
		MembersFrom:         statuses,
		PendingSources:      pending,
		ResolvedMemberCount: len(members),
	}, nil
}

func (r mobilityMemberResolver) memberSet(ref string) (api.MobilityMemberSetSpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "MobilityMemberSet" || strings.TrimSpace(name) == "" {
		return api.MobilityMemberSetSpec{}, false, fmt.Errorf("membersFrom resource must reference MobilityMemberSet/<name>")
	}
	if r.Router == nil {
		return api.MobilityMemberSetSpec{}, false, nil
	}
	for _, resource := range r.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityMemberSet" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.MobilityMemberSetSpec()
		if err != nil {
			return api.MobilityMemberSetSpec{}, true, fmt.Errorf("%s spec: %w", ref, err)
		}
		return spec, true, nil
	}
	return api.MobilityMemberSetSpec{}, false, nil
}

func mobilityPoolMemberFromSetMember(member api.MobilityMemberSetMember) api.MobilityPoolMember {
	return api.MobilityPoolMember{
		NodeRef:              strings.TrimSpace(member.NodeRef),
		Site:                 strings.TrimSpace(member.Site),
		Role:                 strings.TrimSpace(member.Role),
		ProfileRef:           strings.TrimSpace(member.ProfileRef),
		Capture:              member.Capture,
		Delivery:             member.Delivery,
		DeliveryTo:           append([]api.MobilityMemberDeliveryTarget(nil), member.DeliveryTo...),
		StaticOwnedAddresses: append([]string(nil), member.StaticOwnedAddresses...),
		OwnershipDiscovery:   member.OwnershipDiscovery,
		Placement:            member.Placement,
		Maintenance:          member.Maintenance,
	}
}

func mergeMobilityPoolMembers(base, patch api.MobilityPoolMember) api.MobilityPoolMember {
	out := base
	if value := strings.TrimSpace(patch.NodeRef); value != "" {
		out.NodeRef = value
	}
	if value := strings.TrimSpace(patch.Site); value != "" {
		out.Site = value
	}
	if value := strings.TrimSpace(patch.Role); value != "" {
		out.Role = value
	}
	if value := strings.TrimSpace(patch.ProfileRef); value != "" {
		out.ProfileRef = value
	}
	out.Capture = mergeMobilityMemberCapture(base.Capture, patch.Capture)
	if strings.TrimSpace(patch.Delivery.PeerRef) != "" || strings.TrimSpace(patch.Delivery.Mode) != "" || strings.TrimSpace(patch.Delivery.TunnelInterface) != "" {
		out.Delivery = api.MobilityMemberDelivery{
			PeerRef:         strings.TrimSpace(firstNonEmpty(patch.Delivery.PeerRef, out.Delivery.PeerRef)),
			Mode:            strings.TrimSpace(firstNonEmpty(patch.Delivery.Mode, out.Delivery.Mode)),
			TunnelInterface: strings.TrimSpace(firstNonEmpty(patch.Delivery.TunnelInterface, out.Delivery.TunnelInterface)),
		}
	}
	if len(patch.DeliveryTo) > 0 {
		out.DeliveryTo = append([]api.MobilityMemberDeliveryTarget(nil), patch.DeliveryTo...)
	}
	if value := strings.TrimSpace(patch.OwnershipDiscovery.Mode); value != "" {
		out.OwnershipDiscovery.Mode = value
	}
	if value := strings.TrimSpace(patch.OwnershipDiscovery.ProviderRef); value != "" {
		out.OwnershipDiscovery.ProviderRef = value
	}
	if value := strings.TrimSpace(patch.OwnershipDiscovery.PluginRef); value != "" {
		out.OwnershipDiscovery.PluginRef = value
	}
	if value := strings.TrimSpace(patch.OwnershipDiscovery.SubnetRef); value != "" {
		out.OwnershipDiscovery.SubnetRef = value
	}
	if value := strings.TrimSpace(patch.OwnershipDiscovery.SubnetRefFrom); value != "" {
		out.OwnershipDiscovery.SubnetRefFrom = value
	}
	if value := strings.TrimSpace(patch.OwnershipDiscovery.ScanInterval); value != "" {
		out.OwnershipDiscovery.ScanInterval = value
	}
	if value := strings.TrimSpace(patch.OwnershipDiscovery.LeaseTTL); value != "" {
		out.OwnershipDiscovery.LeaseTTL = value
	}
	if value := strings.TrimSpace(patch.Placement.Group); value != "" {
		out.Placement.Group = value
	}
	if patch.Placement.Priority != 0 {
		out.Placement.Priority = patch.Placement.Priority
	}
	if patch.Maintenance.Drain {
		out.Maintenance.Drain = true
	}
	return out
}

func mergeMobilityMemberCapture(base, patch api.MobilityMemberCapture) api.MobilityMemberCapture {
	out := base
	if value := strings.TrimSpace(patch.Type); value != "" {
		out.Type = value
	}
	if value := strings.TrimSpace(patch.ProviderRef); value != "" {
		out.ProviderRef = value
	}
	if value := strings.TrimSpace(patch.ProviderMode); value != "" {
		out.ProviderMode = value
	}
	if value := firstNonEmpty(strings.TrimSpace(patch.CaptureStrategy), strings.TrimSpace(patch.Strategy)); value != "" {
		out.CaptureStrategy = value
		out.Strategy = value
	}
	if value := strings.TrimSpace(patch.NICRef); value != "" {
		out.NICRef = value
	}
	if value := strings.TrimSpace(patch.Interface); value != "" {
		out.Interface = value
	}
	if value := strings.TrimSpace(patch.SourceAddress); value != "" {
		out.SourceAddress = value
	}
	if strings.TrimSpace(patch.SourceAddressFrom.Resource) != "" {
		out.SourceAddressFrom = patch.SourceAddressFrom
		out.SourceAddressFrom.Resource = strings.TrimSpace(patch.SourceAddressFrom.Resource)
		out.SourceAddressFrom.Field = strings.TrimSpace(patch.SourceAddressFrom.Field)
	}
	if strings.TrimSpace(patch.SourceAddressFrom.Field) != "" {
		out.SourceAddressFrom.Field = strings.TrimSpace(patch.SourceAddressFrom.Field)
	}
	if patch.SourceAddressFrom.Optional {
		out.SourceAddressFrom.Optional = true
	}
	if patch.ConfigureOSAddress {
		out.ConfigureOSAddress = true
	}
	if patch.GratuitousARP {
		out.GratuitousARP = true
	}
	if value := strings.TrimSpace(patch.ActiveWhen.Type); value != "" {
		out.ActiveWhen.Type = value
	}
	if value := strings.TrimSpace(patch.ActiveWhen.VirtualAddressRef); value != "" {
		out.ActiveWhen.VirtualAddressRef = value
	}
	out.ExcludeAddresses = mergeStringSet(out.ExcludeAddresses, patch.ExcludeAddresses)
	if patch.Target != nil {
		out.Target = mergeStringMaps(out.Target, patch.Target)
	}
	if patch.TargetFrom != nil {
		out.TargetFrom = mergeStringMaps(out.TargetFrom, patch.TargetFrom)
	}
	return out
}

func mobilityMemberSetMemberFromPoolMember(member api.MobilityPoolMember) api.MobilityMemberSetMember {
	return api.MobilityMemberSetMember{
		NodeRef:              strings.TrimSpace(member.NodeRef),
		Site:                 strings.TrimSpace(member.Site),
		Role:                 strings.TrimSpace(member.Role),
		ProfileRef:           strings.TrimSpace(member.ProfileRef),
		Capture:              member.Capture,
		Delivery:             member.Delivery,
		DeliveryTo:           append([]api.MobilityMemberDeliveryTarget(nil), member.DeliveryTo...),
		StaticOwnedAddresses: append([]string(nil), member.StaticOwnedAddresses...),
		OwnershipDiscovery:   member.OwnershipDiscovery,
		Placement:            member.Placement,
		Maintenance:          member.Maintenance,
	}
}

func nameFromMemberSetRef(ref string) string {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "MobilityMemberSet" {
		return ""
	}
	return strings.TrimSpace(name)
}

func mobilityMembersFromStatusMaps(statuses []mobilityMembersFromStatus) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		item := map[string]any{
			"resource": status.Resource,
			"phase":    status.Phase,
		}
		if status.Optional {
			item["optional"] = true
		}
		if status.MemberCount > 0 {
			item["memberCount"] = status.MemberCount
		}
		if status.Reason != "" {
			item["reason"] = status.Reason
		}
		out = append(out, item)
	}
	return out
}

func (c Controller) upsertMobilityMemberSetPart(owner api.Resource, spec api.MobilityPoolSpec, source string, now time.Time) (map[string]any, error) {
	status := map[string]any{
		"phase":  "Published",
		"source": source,
	}
	members := make([]api.MobilityMemberSetMember, 0, len(spec.Members))
	seen := map[string]bool{}
	for _, member := range spec.Members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if nodeRef == "" {
			continue
		}
		if seen[nodeRef] {
			status["phase"] = "Degraded"
			status["reason"] = fmt.Sprintf("duplicate nodeRef %q", nodeRef)
			return status, fmt.Errorf("publishMemberSet duplicate nodeRef %q", nodeRef)
		}
		seen[nodeRef] = true
		members = append(members, mobilityMemberSetMemberFromPoolMember(member))
	}
	resourceName := owner.Metadata.Name
	resources := []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityMemberSet"},
		Metadata: api.ObjectMeta{
			Name: resourceName,
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "MobilityPool",
				Name:       owner.Metadata.Name,
			}},
		},
		Spec: api.MobilityMemberSetSpec{Members: members},
	}}
	status["resource"] = "MobilityMemberSet/" + resourceName
	status["memberCount"] = len(members)
	if err := c.upsertMemberSetPart(owner, source, resources, now); err != nil {
		status["phase"] = "Degraded"
		status["reason"] = err.Error()
		return status, err
	}
	return status, nil
}

func (c Controller) upsertMemberSetPart(owner api.Resource, source string, resources []api.Resource, now time.Time) error {
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: safeName("mobility-member-set-" + owner.Metadata.Name),
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "MobilityPool",
				Name:       owner.Metadata.Name,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      source,
			Generation:  dynamicGeneration,
			ObservedAt:  now,
			ExpiresAt:   now.Add(DefaultLeaseTTL),
			Resources:   append([]api.Resource(nil), resources...),
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

func MobilityMemberSetDynamicSource(poolName string) string {
	return mobilityMemberSetSourceKind + "/" + strings.TrimSpace(poolName)
}

func parseMobilityMemberSetSource(source string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(source), "/")
	if len(parts) == 2 && parts[0] == mobilityMemberSetSourceKind && parts[1] != "" {
		return parts[1], true
	}
	return "", false
}
