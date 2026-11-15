// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"github.com/imksoo/routerd/pkg/api"
)

// testMobilityPoolSpec keeps the resolved topology separate from the declared
// MobilityPool overlay. Production tests build a SAMNodeSet from Members so
// controller fixtures exercise the same one-topology contract as real config.
type testMobilityPoolSpec struct {
	api.MobilityPoolSpec
	Members []api.ResolvedMobilityPoolMember
}

// localizeMobilityPoolSpecForNode models one node's authoring surface. Shared
// members retain only topology fields; capture, provider discovery, and static
// ownership belong to the local member and are observed elsewhere through BGP
// or federation facts.
func localizeMobilityPoolSpecForNode(spec testMobilityPoolSpec, nodeName string) (api.MobilityPoolSpec, api.Resource) {
	members := plannerMembers(spec.Members)
	self, ok := lookupMemberByNodeRef(members, nodeName)
	if !ok {
		return spec.MobilityPoolSpec, api.Resource{}
	}
	var selfMember api.ResolvedMobilityPoolMember
	for _, member := range spec.Members {
		if member.NodeRef == self.NodeRef {
			selfMember = member
			break
		}
	}
	out := spec.MobilityPoolSpec
	out.MembersFrom = []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/cloudedge"}}
	out.Members = []api.MobilityPoolMemberOverlay{{
		NodeRef:              self.NodeRef,
		ProfileRef:           selfMember.ProfileRef,
		Capture:              self.Capture,
		StaticOwnedAddresses: append([]string(nil), self.StaticOwnedAddresses...),
		OwnershipDiscovery:   self.OwnershipDiscovery,
	}}
	nodes := make([]api.SAMNodeSpec, 0, len(spec.Members))
	for _, member := range spec.Members {
		nodes = append(nodes, api.SAMNodeSpec{
			NodeRef:         member.NodeRef,
			Site:            member.Site,
			Role:            member.Role,
			Placement:       member.Placement,
			Maintenance:     member.Maintenance,
			MaxSecondaryIPs: member.MaxSecondaryIPs,
		})
	}
	return out, mobilityNodeSetResource("cloudedge", nodes)
}
