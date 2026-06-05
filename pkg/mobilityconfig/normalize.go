// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	DiagnosticWarning = "warning"
)

type Diagnostic struct {
	Severity string
	Path     string
	Message  string
}

// NormalizeMobilityPool expands MobilityPool profile/value shorthand into the
// concrete per-member shape consumed by validation and controllers. It is pure:
// callers own both the input and the returned spec.
func NormalizeMobilityPool(spec api.MobilityPoolSpec, selfNode string) (api.MobilityPoolSpec, []Diagnostic, error) {
	out := copySpec(spec)
	selfNode = strings.TrimSpace(selfNode)
	selfSite := ""
	if selfNode != "" {
		for _, member := range out.Members {
			if strings.TrimSpace(member.NodeRef) == selfNode {
				selfSite = strings.TrimSpace(member.Site)
				break
			}
		}
	}

	for name, profile := range out.Profiles.CloudCaptures {
		capture, err := resolveCaptureValues(out.Values, profile.Capture, fmt.Sprintf("spec.profiles.cloudCaptures[%q].capture", name))
		if err != nil {
			return api.MobilityPoolSpec{}, nil, err
		}
		discovery, err := resolveDiscoveryValues(out.Values, profile.OwnershipDiscovery, fmt.Sprintf("spec.profiles.cloudCaptures[%q].ownershipDiscovery", name))
		if err != nil {
			return api.MobilityPoolSpec{}, nil, err
		}
		profile.Capture = capture
		profile.OwnershipDiscovery = discovery
		out.Profiles.CloudCaptures[name] = profile
	}

	var diagnostics []Diagnostic
	for i := range out.Members {
		member := out.Members[i]
		ref := strings.TrimSpace(member.ProfileRef)
		if ref != "" {
			profile, ok := out.Profiles.CloudCaptures[ref]
			if !ok {
				return api.MobilityPoolSpec{}, nil, fmt.Errorf("spec.members[%d].profileRef %q references missing spec.profiles.cloudCaptures entry", i, ref)
			}
			if strings.TrimSpace(member.Role) != "cloud" {
				return api.MobilityPoolSpec{}, nil, fmt.Errorf("spec.members[%d].profileRef is supported only for role cloud", i)
			}
			member.Capture = mergeCapture(profile.Capture, member.Capture)
			member.OwnershipDiscovery = mergeOwnershipDiscovery(profile.OwnershipDiscovery, member.OwnershipDiscovery)
		}
		resolvedCapture, err := resolveCaptureValues(out.Values, member.Capture, fmt.Sprintf("spec.members[%d].capture", i))
		if err != nil {
			return api.MobilityPoolSpec{}, nil, err
		}
		member.Capture = resolvedCapture
		resolvedDiscovery, err := resolveDiscoveryValues(out.Values, member.OwnershipDiscovery, fmt.Sprintf("spec.members[%d].ownershipDiscovery", i))
		if err != nil {
			return api.MobilityPoolSpec{}, nil, err
		}
		if strings.TrimSpace(resolvedDiscovery.ProviderRef) == "" {
			resolvedDiscovery.ProviderRef = strings.TrimSpace(member.Capture.ProviderRef)
		}
		member.OwnershipDiscovery = resolvedDiscovery
		out.Members[i] = member

		if selfNode != "" && selfSite != "" && strings.TrimSpace(member.Site) != selfSite && remoteMemberHasLocalDetails(member) {
			diagnostics = append(diagnostics, Diagnostic{
				Severity: DiagnosticWarning,
				Path:     fmt.Sprintf("spec.members[%d]", i),
				Message:  fmt.Sprintf("remote member %q declares local capture/discovery details; prefer identity-only remote members and keep node-local details in the local router config", strings.TrimSpace(member.NodeRef)),
			})
		}
	}
	applyAutoPlacementPriorities(out.Members)
	return out, diagnostics, nil
}

func copySpec(spec api.MobilityPoolSpec) api.MobilityPoolSpec {
	out := spec
	out.Values = copyStringMap(spec.Values)
	out.Members = make([]api.MobilityPoolMember, len(spec.Members))
	for i, member := range spec.Members {
		out.Members[i] = copyMember(member)
	}
	out.StaticHandovers = append([]api.MobilityStaticHandover(nil), spec.StaticHandovers...)
	out.Profiles.CloudCaptures = map[string]api.MobilityCloudCaptureProfile{}
	for name, profile := range spec.Profiles.CloudCaptures {
		out.Profiles.CloudCaptures[name] = api.MobilityCloudCaptureProfile{
			Capture:            copyCapture(profile.Capture),
			OwnershipDiscovery: copyDiscovery(profile.OwnershipDiscovery),
		}
	}
	if len(out.Profiles.CloudCaptures) == 0 {
		out.Profiles.CloudCaptures = nil
	}
	return out
}

func copyMember(member api.MobilityPoolMember) api.MobilityPoolMember {
	out := member
	out.Capture = copyCapture(member.Capture)
	out.DeliveryTo = append([]api.MobilityMemberDeliveryTarget(nil), member.DeliveryTo...)
	out.StaticOwnedAddresses = append([]string(nil), member.StaticOwnedAddresses...)
	out.OwnershipDiscovery = copyDiscovery(member.OwnershipDiscovery)
	return out
}

func copyCapture(c api.MobilityMemberCapture) api.MobilityMemberCapture {
	out := c
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
	if strings.TrimSpace(override.ProviderMode) != "" {
		out.ProviderMode = override.ProviderMode
	}
	if strings.TrimSpace(override.NICRef) != "" {
		out.NICRef = override.NICRef
	}
	if override.ConfigureOSAddress {
		out.ConfigureOSAddress = true
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
	if len(override.Sources) > 0 {
		out.Sources = append([]api.MobilityOwnershipDiscoverySource(nil), override.Sources...)
	}
	if override.Scope.IncludePrimary != nil {
		out.Scope.IncludePrimary = override.Scope.IncludePrimary
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

func applyAutoPlacementPriorities(members []api.MobilityPoolMember) {
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

func remoteMemberHasLocalDetails(member api.MobilityPoolMember) bool {
	return strings.TrimSpace(member.ProfileRef) != "" || captureSet(member.Capture) || discoverySet(member.OwnershipDiscovery)
}

func captureSet(c api.MobilityMemberCapture) bool {
	return strings.TrimSpace(c.Type) != "" ||
		strings.TrimSpace(c.ProviderRef) != "" ||
		strings.TrimSpace(c.ProviderMode) != "" ||
		strings.TrimSpace(c.NICRef) != "" ||
		c.ConfigureOSAddress ||
		strings.TrimSpace(c.Interface) != "" ||
		strings.TrimSpace(c.SourceAddress) != "" ||
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
		d.Scope.IncludePrimary != nil ||
		len(d.Scope.IncludeAddresses) > 0 ||
		len(d.Scope.ExcludeAddresses) > 0 ||
		len(d.Selector.Tags) > 0
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
