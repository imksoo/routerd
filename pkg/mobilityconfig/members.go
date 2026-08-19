// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"fmt"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// MobilityMemberSourceResolution records the static SAMNodeSet expansion used
// to build a pool's canonical member list. A missing source is represented as
// Found=false so controllers can retain their pending/fail-static policy.
type MobilityMemberSourceResolution struct {
	Resource    string
	Optional    bool
	Found       bool
	MemberCount int
	Err         error
}

// ResolvedMobilityPoolMembers is the controller-facing member view after the
// SAMNodeSet topology and local overlays have been combined. Members remains a
// separate resolved shape so no controller can mistake a local overlay for
// topology.
type ResolvedMobilityPoolMembers struct {
	Members []api.ResolvedMobilityPoolMember
	Sources []MobilityMemberSourceResolution
}

// ResolveMobilityPoolMembers expands the required SAMNodeSet topology and then
// applies the narrow local overlays. An overlay can never create a member or
// override topology. Both config validation and the controller use this pure
// expansion, keeping the static authoring boundary singular.
func ResolveMobilityPoolMembers(router *api.Router, spec api.MobilityPoolSpec) (ResolvedMobilityPoolMembers, error) {
	result := ResolvedMobilityPoolMembers{
		Sources: make([]MobilityMemberSourceResolution, 0, len(spec.MembersFrom)),
	}
	if len(spec.MembersFrom) == 0 {
		return result, fmt.Errorf("spec.membersFrom requires at least one SAMNodeSet source; direct MobilityPool membership is not supported")
	}
	members := make([]api.ResolvedMobilityPoolMember, 0, len(spec.Members))
	indexByNode := map[string]int{}
	missingSource := false
	addMember := func(member api.ResolvedMobilityPoolMember) error {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if nodeRef == "" {
			return fmt.Errorf("SAMNodeSet member nodeRef is required")
		}
		if _, ok := indexByNode[nodeRef]; ok {
			return fmt.Errorf("SAMNodeSet sources declare nodeRef %q more than once", nodeRef)
		}
		indexByNode[nodeRef] = len(members)
		members = append(members, member)
		return nil
	}
	for i, source := range spec.MembersFrom {
		status := MobilityMemberSourceResolution{
			Resource: strings.TrimSpace(source.Resource),
			Optional: source.Optional,
		}
		nodeSet, found, err := api.LookupSAMNodeSet(router, status.Resource, fmt.Sprintf("spec.membersFrom[%d]", i))
		if err != nil {
			status.Err = err
			result.Sources = append(result.Sources, status)
			return result, err
		}
		if found {
			status.Found = true
			status.MemberCount = len(nodeSet.Nodes)
			for _, node := range nodeSet.Nodes {
				if err := addMember(ResolvedMobilityPoolMemberFromSAMNode(node)); err != nil {
					status.Err = err
					result.Sources = append(result.Sources, status)
					return result, err
				}
			}
		} else {
			missingSource = true
		}
		result.Sources = append(result.Sources, status)
	}
	for i, overlay := range spec.Members {
		nodeRef := strings.TrimSpace(overlay.NodeRef)
		if nodeRef == "" {
			return result, fmt.Errorf("spec.members[%d].nodeRef is required", i)
		}
		index, found := indexByNode[nodeRef]
		if !found {
			// Missing sources are represented as Pending by the controller. Never
			// turn an unresolved local overlay into a fallback topology member.
			if missingSource {
				continue
			}
			return result, fmt.Errorf("spec.members[%d].nodeRef %q is not declared by spec.membersFrom", i, nodeRef)
		}
		members[index] = MergeResolvedMobilityPoolMemberOverlay(members[index], overlay)
	}
	result.Members = members
	return result, nil
}

// RequireResolvedMobilityPoolSelfMember verifies the exact EventGroup node is
// present in the SAMNodeSet-derived topology before local-overlay normalization.
// This keeps identity mismatch fail-closed instead of letting a secondary
// overlay error hide the actual topology violation.
func RequireResolvedMobilityPoolSelfMember(members []api.ResolvedMobilityPoolMember, selfNode string) error {
	selfNode = strings.TrimSpace(selfNode)
	for _, member := range members {
		if strings.TrimSpace(member.NodeRef) == selfNode {
			return nil
		}
	}
	return fmt.Errorf("self node %q is not a member of the resolved SAMNodeSet topology", selfNode)
}

// ResolvedMobilityPoolMemberFromSAMNode maps shared node identity/topology into the
// member shape consumed by MobilityPool planning. Provider-local capture and
// discovery settings intentionally do not exist in SAMNodeSet.
func ResolvedMobilityPoolMemberFromSAMNode(node api.SAMNodeSpec) api.ResolvedMobilityPoolMember {
	return api.ResolvedMobilityPoolMember{
		NodeRef:         strings.TrimSpace(node.NodeRef),
		Site:            strings.TrimSpace(node.Site),
		Role:            strings.TrimSpace(node.Role),
		Placement:       node.Placement,
		Maintenance:     node.Maintenance,
		MaxSecondaryIPs: node.MaxSecondaryIPs,
	}
}

// MergeResolvedMobilityPoolMemberOverlay applies only provider/L2 details to a SAMNodeSet
// member. Site, role, placement, maintenance, and capacity intentionally stay
// untouched: they have one authoring home in SAMNodeSet.
func MergeResolvedMobilityPoolMemberOverlay(identity api.ResolvedMobilityPoolMember, overlay api.MobilityPoolMemberOverlay) api.ResolvedMobilityPoolMember {
	out := identity
	out.ProfileRef = strings.TrimSpace(overlay.ProfileRef)
	out.Capture = overlay.Capture
	out.StaticOwnedAddresses = append([]string(nil), overlay.StaticOwnedAddresses...)
	out.OwnershipDiscovery = overlay.OwnershipDiscovery
	return out
}
