// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// NormalizeResolvedMobilityPoolMembers expands MobilityPool profile/value shorthand
// into the concrete resolved-member shape consumed by validation and
// controllers. It is pure: callers own both the declared spec and members.
//
// A concrete self node makes the input node-local: every other member is
// identity/topology-only. Provider, capture, and ownership details belong only
// to the local member, whose resulting BGP and federation facts are the shared
// representation of that state.
func NormalizeResolvedMobilityPoolMembers(spec api.MobilityPoolSpec, members []api.ResolvedMobilityPoolMember, selfNode string) ([]api.ResolvedMobilityPoolMember, error) {
	values := copyStringMap(spec.Values)
	profiles := copyProfiles(spec.Profiles)
	out := make([]api.ResolvedMobilityPoolMember, len(members))
	for i, member := range members {
		out[i] = copyMember(member)
	}
	selfNode = strings.TrimSpace(selfNode)

	for name, profile := range profiles.CloudCaptures {
		capture, err := resolveCaptureValues(values, profile.Capture, fmt.Sprintf("spec.profiles.cloudCaptures[%q].capture", name))
		if err != nil {
			return nil, err
		}
		discovery, err := resolveDiscoveryValues(values, profile.OwnershipDiscovery, fmt.Sprintf("spec.profiles.cloudCaptures[%q].ownershipDiscovery", name))
		if err != nil {
			return nil, err
		}
		profile.Capture = capture
		profile.OwnershipDiscovery = discovery
		profiles.CloudCaptures[name] = profile
	}

	for i := range out {
		member := out[i]
		if selfNode != "" && strings.TrimSpace(member.NodeRef) != selfNode && memberHasLocalOverlay(member) {
			return nil, fmt.Errorf("resolved member[%d] nodeRef %q is remote to local node %q and carries a local overlay", i, strings.TrimSpace(member.NodeRef), selfNode)
		}
		ref := strings.TrimSpace(member.ProfileRef)
		if ref != "" {
			profile, ok := profiles.CloudCaptures[ref]
			if !ok {
				return nil, fmt.Errorf("resolved member[%d].profileRef %q references missing spec.profiles.cloudCaptures entry", i, ref)
			}
			if strings.TrimSpace(member.Role) != "cloud" {
				return nil, fmt.Errorf("resolved member[%d].profileRef is supported only for role cloud", i)
			}
			member.Capture = mergeCapture(profile.Capture, member.Capture)
			member.OwnershipDiscovery = mergeOwnershipDiscovery(profile.OwnershipDiscovery, member.OwnershipDiscovery)
		}
		resolvedCapture, err := resolveCaptureValues(values, member.Capture, fmt.Sprintf("resolved member[%d].capture", i))
		if err != nil {
			return nil, err
		}
		member.Capture = resolvedCapture
		resolvedDiscovery, err := resolveDiscoveryValues(values, member.OwnershipDiscovery, fmt.Sprintf("resolved member[%d].ownershipDiscovery", i))
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(resolvedDiscovery.ProviderRef) == "" {
			resolvedDiscovery.ProviderRef = strings.TrimSpace(member.Capture.ProviderRef)
		}
		member.OwnershipDiscovery = resolvedDiscovery
		out[i] = member
	}
	applyAutoPlacementPriorities(out)
	return out, nil
}

func copyProfiles(profiles api.MobilityPoolProfiles) api.MobilityPoolProfiles {
	out := api.MobilityPoolProfiles{CloudCaptures: map[string]api.MobilityCloudCaptureProfile{}}
	for name, profile := range profiles.CloudCaptures {
		out.CloudCaptures[name] = api.MobilityCloudCaptureProfile{
			Capture:            copyCapture(profile.Capture),
			OwnershipDiscovery: copyDiscovery(profile.OwnershipDiscovery),
		}
	}
	if len(out.CloudCaptures) == 0 {
		out.CloudCaptures = nil
	}
	return out
}

func copyMember(member api.ResolvedMobilityPoolMember) api.ResolvedMobilityPoolMember {
	out := member
	out.Capture = copyCapture(member.Capture)
	out.StaticOwnedAddresses = append([]string(nil), member.StaticOwnedAddresses...)
	out.OwnershipDiscovery = copyDiscovery(member.OwnershipDiscovery)
	return out
}

func copyCapture(c api.MobilityMemberCapture) api.MobilityMemberCapture {
	out := c
	out.ExcludeAddresses = append([]string(nil), c.ExcludeAddresses...)
	out.Target = copyStringMap(c.Target)
	out.TargetFrom = copyStringMap(c.TargetFrom)
	return out
}

func copyDiscovery(d api.MobilityOwnershipDiscovery) api.MobilityOwnershipDiscovery {
	out := d
	out.Sources = append([]api.MobilityOwnershipDiscoverySource(nil), d.Sources...)
	out.Scope.IncludeAddresses = append([]string(nil), d.Scope.IncludeAddresses...)
	out.Scope.ExcludeAddresses = append([]string(nil), d.Scope.ExcludeAddresses...)
	out.Selector.Tags = copyStringMap(d.Selector.Tags)
	return out
}

func mergeCapture(base, override api.MobilityMemberCapture) api.MobilityMemberCapture {
	out := copyCapture(base)
	if strings.TrimSpace(override.Type) != "" {
		out.Type = override.Type
	}
	if strings.TrimSpace(override.ProviderRef) != "" {
		out.ProviderRef = override.ProviderRef
	}
	if strings.TrimSpace(override.CaptureStrategy) != "" {
		out.CaptureStrategy = override.CaptureStrategy
	}
	if strings.TrimSpace(override.NICRef) != "" {
		out.NICRef = override.NICRef
	}
	if strings.TrimSpace(override.Interface) != "" {
		out.Interface = override.Interface
	}
	if strings.TrimSpace(override.SourceAddress) != "" {
		out.SourceAddress = override.SourceAddress
	}
	if strings.TrimSpace(override.SourceAddressFrom.Resource) != "" {
		out.SourceAddressFrom = override.SourceAddressFrom
	}
	if len(override.ExcludeAddresses) > 0 {
		out.ExcludeAddresses = append([]string(nil), override.ExcludeAddresses...)
	}
	if override.GratuitousARP {
		out.GratuitousARP = true
	}
	if strings.TrimSpace(override.ActiveWhen.Type) != "" {
		out.ActiveWhen.Type = override.ActiveWhen.Type
	}
	if strings.TrimSpace(override.ActiveWhen.VirtualAddressRef) != "" {
		out.ActiveWhen.VirtualAddressRef = override.ActiveWhen.VirtualAddressRef
	}
	out.Target = mergeStringMap(out.Target, override.Target)
	out.TargetFrom = mergeStringMap(out.TargetFrom, override.TargetFrom)
	return out
}

func mergeOwnershipDiscovery(base, override api.MobilityOwnershipDiscovery) api.MobilityOwnershipDiscovery {
	out := copyDiscovery(base)
	if strings.TrimSpace(override.Mode) != "" {
		out.Mode = override.Mode
	}
	if strings.TrimSpace(override.ProviderRef) != "" {
		out.ProviderRef = override.ProviderRef
	}
	if strings.TrimSpace(override.PluginRef) != "" {
		out.PluginRef = override.PluginRef
	}
	if strings.TrimSpace(override.SubnetRef) != "" {
		out.SubnetRef = override.SubnetRef
	}
	if strings.TrimSpace(override.SubnetRefFrom) != "" {
		out.SubnetRefFrom = override.SubnetRefFrom
	}
	if strings.TrimSpace(override.ScanInterval) != "" {
		out.ScanInterval = override.ScanInterval
	}
	if strings.TrimSpace(override.LeaseTTL) != "" {
		out.LeaseTTL = override.LeaseTTL
	}
	if strings.TrimSpace(override.StoppedInstancePolicy) != "" {
		out.StoppedInstancePolicy = override.StoppedInstancePolicy
	}
	if len(override.Sources) > 0 {
		out.Sources = append([]api.MobilityOwnershipDiscoverySource(nil), override.Sources...)
	}
	if len(override.Scope.IncludeAddresses) > 0 {
		out.Scope.IncludeAddresses = append([]string(nil), override.Scope.IncludeAddresses...)
	}
	if len(override.Scope.ExcludeAddresses) > 0 {
		out.Scope.ExcludeAddresses = append([]string(nil), override.Scope.ExcludeAddresses...)
	}
	out.Selector.Tags = mergeStringMap(out.Selector.Tags, override.Selector.Tags)
	return out
}

func resolveCaptureValues(values map[string]string, capture api.MobilityMemberCapture, path string) (api.MobilityMemberCapture, error) {
	out := copyCapture(capture)
	if _, ok := out.Target["nicRef"]; ok {
		return api.MobilityMemberCapture{}, fmt.Errorf("%s.target.nicRef is not supported; use %s.nicRef", path, path)
	}
	if _, ok := out.TargetFrom["nicRef"]; ok {
		return api.MobilityMemberCapture{}, fmt.Errorf("%s.targetFrom.nicRef is not supported; use %s.nicRef", path, path)
	}
	if len(out.TargetFrom) == 0 {
		return out, nil
	}
	if out.Target == nil {
		out.Target = map[string]string{}
	}
	for targetKey, valueKey := range out.TargetFrom {
		targetKey = strings.TrimSpace(targetKey)
		valueKey = strings.TrimSpace(valueKey)
		if targetKey == "" {
			return api.MobilityMemberCapture{}, fmt.Errorf("%s.targetFrom contains an empty target key", path)
		}
		if valueKey == "" {
			return api.MobilityMemberCapture{}, fmt.Errorf("%s.targetFrom[%q] must reference a spec.values key", path, targetKey)
		}
		if strings.TrimSpace(out.Target[targetKey]) != "" {
			continue
		}
		value := strings.TrimSpace(values[valueKey])
		if value == "" {
			return api.MobilityMemberCapture{}, fmt.Errorf("%s.targetFrom[%q] references missing spec.values[%q]", path, targetKey, valueKey)
		}
		out.Target[targetKey] = value
	}
	return out, nil
}

func resolveDiscoveryValues(values map[string]string, discovery api.MobilityOwnershipDiscovery, path string) (api.MobilityOwnershipDiscovery, error) {
	out := copyDiscovery(discovery)
	refFrom := strings.TrimSpace(out.SubnetRefFrom)
	if refFrom == "" || strings.TrimSpace(out.SubnetRef) != "" {
		return out, nil
	}
	value := strings.TrimSpace(values[refFrom])
	if value == "" {
		return api.MobilityOwnershipDiscovery{}, fmt.Errorf("%s.subnetRefFrom references missing spec.values[%q]", path, refFrom)
	}
	out.SubnetRef = value
	return out, nil
}

func applyAutoPlacementPriorities(members []api.ResolvedMobilityPoolMember) {
	usedByGroup := map[string]map[int]bool{}
	for _, member := range members {
		group := strings.TrimSpace(member.Placement.Group)
		priority := member.Placement.Priority
		if group == "" || priority == 0 {
			continue
		}
		if usedByGroup[group] == nil {
			usedByGroup[group] = map[int]bool{}
		}
		usedByGroup[group][priority] = true
	}
	nextByGroup := map[string]int{}
	for i := range members {
		group := strings.TrimSpace(members[i].Placement.Group)
		if group == "" || members[i].Placement.Priority != 0 {
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
		members[i].Placement.Priority = next
		usedByGroup[group][next] = true
		nextByGroup[group] = next + 10
	}
}

func memberHasLocalOverlay(member api.ResolvedMobilityPoolMember) bool {
	return strings.TrimSpace(member.ProfileRef) != "" ||
		captureSet(member.Capture) ||
		len(member.StaticOwnedAddresses) > 0 ||
		discoverySet(member.OwnershipDiscovery)
}

func captureSet(c api.MobilityMemberCapture) bool {
	return strings.TrimSpace(c.Type) != "" ||
		strings.TrimSpace(c.ProviderRef) != "" ||
		strings.TrimSpace(c.CaptureStrategy) != "" ||
		strings.TrimSpace(c.NICRef) != "" ||
		strings.TrimSpace(c.Interface) != "" ||
		strings.TrimSpace(c.SourceAddress) != "" ||
		statusValueSourceSet(c.SourceAddressFrom) ||
		len(c.ExcludeAddresses) > 0 ||
		c.GratuitousARP ||
		strings.TrimSpace(c.ActiveWhen.Type) != "" ||
		strings.TrimSpace(c.ActiveWhen.VirtualAddressRef) != "" ||
		len(c.Target) > 0 ||
		len(c.TargetFrom) > 0
}

func discoverySet(d api.MobilityOwnershipDiscovery) bool {
	return strings.TrimSpace(d.Mode) != "" ||
		strings.TrimSpace(d.ProviderRef) != "" ||
		strings.TrimSpace(d.PluginRef) != "" ||
		strings.TrimSpace(d.SubnetRef) != "" ||
		strings.TrimSpace(d.SubnetRefFrom) != "" ||
		strings.TrimSpace(d.ScanInterval) != "" ||
		strings.TrimSpace(d.LeaseTTL) != "" ||
		strings.TrimSpace(d.StoppedInstancePolicy) != "" ||
		strings.TrimSpace(d.AllowEmptyAfter) != "" ||
		len(d.Sources) > 0 ||
		len(d.Scope.IncludeAddresses) > 0 ||
		len(d.Scope.ExcludeAddresses) > 0 ||
		len(d.Selector.Tags) > 0
}

func statusValueSourceSet(source api.StatusValueSourceSpec) bool {
	return strings.TrimSpace(source.Resource) != "" || strings.TrimSpace(source.Field) != "" || source.Optional
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mergeStringMap(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := copyStringMap(base)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}
