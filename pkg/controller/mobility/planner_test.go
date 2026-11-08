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

func plannedPoolSpec() testMobilityPoolSpec {
	return testMobilityPoolSpec{MobilityPoolSpec: api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
	}, Members: []api.ResolvedMobilityPoolMember{
		{
			NodeRef: "onprem-router",
			Site:    "onprem",
			Role:    "onprem",
			Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"},
		},
		{
			NodeRef: "azure-router",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "azure-provider",
				NICRef:      "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
				Target:      map[string]string{"region": "japaneast"},
			},
		},
	},
	}
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

func planningRouterForNode(nodeName string, spec testMobilityPoolSpec) *api.Router {
	poolSpec, nodeSet := localizeMobilityPoolSpecForNode(spec, nodeName)
	resources := []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     api.EventGroupSpec{NodeName: nodeName},
		},
	}
	if nodeSet.Kind != "" {
		resources = append(resources, nodeSet)
	}
	resources = append(resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     poolSpec,
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
			Metadata: api.ObjectMeta{Name: "azure-provider"},
			Spec: api.CloudProviderProfileSpec{
				Provider:       "azure",
				SubscriptionID: "sub-1",
				ResourceGroup:  "rg-router",
				Capabilities:   []string{"nic-secondary-ip", "ip-forwarding"},
				Auth:           api.ProviderAuth{Mode: "external-command", Command: "az"},
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
			Metadata: api.ObjectMeta{Name: "aws-provider"},
			Spec: api.CloudProviderProfileSpec{
				Provider:     "aws",
				Capabilities: []string{"nic-secondary-ip", "ip-forwarding"},
				Auth:         api.ProviderAuth{Mode: "external-command", Command: "aws"},
			},
		},
	)
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec:     api.RouterSpec{Resources: resources},
	}
}
