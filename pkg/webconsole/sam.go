// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"net/http"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (h Handler) sam(w http.ResponseWriter) {
	resources, err := h.resourceStatuses()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := SAMStatus{GeneratedAt: time.Now().UTC()}

	for _, res := range resources {
		switch res.Kind {
		case "SAMNodeSet":
			status.Nodes = append(status.Nodes, samNodesFromResource(res)...)
		case "MobilityPool":
			if pool, ok := samPoolFromResource(res); ok {
				status.Pools = append(status.Pools, pool)
			}
		case "WireGuardInterface":
			if t, ok := samTunnelFromResource(res); ok {
				status.Tunnels = append(status.Tunnels, t)
			}
		case "EventGroup":
			if fed, ok := samFederationFromResource(res, resources); ok {
				status.Federation = append(status.Federation, fed)
			}
		}
	}
	writeJSON(w, status)
}

func samNodesFromResource(res routerstate.ObjectStatus) []SAMNode {
	status := res.Status
	if status == nil {
		return nil
	}
	nodesRaw := statusList(status["nodes"])
	if len(nodesRaw) == 0 {
		nodesRaw = statusList(status["resolvedNodes"])
	}
	var out []SAMNode
	for _, node := range nodesRaw {
		out = append(out, SAMNode{
			NodeRef:        statusAnyText(node["nodeRef"]),
			Site:           statusAnyText(node["site"]),
			Role:           statusAnyText(node["role"]),
			RouteReflector: statusAnyText(node["routeReflector"]) == "true",
			Phase:          statusAnyText(node["phase"]),
			Reason:         statusAnyText(node["reason"]),
		})
	}
	if len(out) == 0 {
		configured := statusList(status["configuredNodes"])
		for _, node := range configured {
			out = append(out, SAMNode{
				NodeRef:        statusAnyText(node["nodeRef"]),
				Site:           statusAnyText(node["site"]),
				Role:           statusAnyText(node["role"]),
				RouteReflector: statusAnyText(node["routeReflector"]) == "true",
			})
		}
	}
	return out
}

func samPoolFromResource(res routerstate.ObjectStatus) (SAMPool, bool) {
	status := res.Status
	if status == nil {
		return SAMPool{}, false
	}
	pool := SAMPool{
		Name:                res.Name,
		Prefix:              statusAnyText(status["prefix"]),
		Phase:               statusAnyText(status["phase"]),
		Reason:              statusAnyText(status["reason"]),
		DiscoveryMode:       statusAnyText(status["discoveryMode"]),
		DiscoveryPhase:      statusAnyText(status["discoveryPhase"]),
		GeneratedBGPPaths:   statusIntValue(status["generatedBGPPaths"]),
		ResolvedMemberCount: statusIntValue(status["resolvedMemberCount"]),
		PlacementActive:     statusAnyText(status["placementActive"]) == "true",
		PlacementActiveNode: statusAnyText(status["placementActiveNode"]),
	}
	for _, entry := range statusList(status["ownershipResolverControlPlaneOwnerTable"]) {
		pool.Addresses = append(pool.Addresses, SAMPoolAddress{
			Address:   statusAnyText(entry["address"]),
			OwnerNode: statusAnyText(entry["ownerNode"]),
			Source:    statusAnyText(entry["source"]),
			State:     statusAnyText(entry["state"]),
		})
	}

	return pool, true
}

func samTunnelFromResource(res routerstate.ObjectStatus) (SAMTunnel, bool) {
	status := res.Status
	if status == nil {
		return SAMTunnel{}, false
	}
	if statusAnyText(status["samTransportProfile"]) == "" && statusAnyText(status["mobilityOverlay"]) == "" {
		return SAMTunnel{}, false
	}
	return SAMTunnel{
		Name:      res.Name,
		PeerRef:   statusAnyText(status["peerNodeRef"]),
		Phase:     statusAnyText(status["phase"]),
		Endpoint:  statusAnyText(status["endpoint"]),
		Interface: statusAnyText(status["interfaceName"]),
	}, true
}

func samFederationFromResource(res routerstate.ObjectStatus, allResources []routerstate.ObjectStatus) (SAMFederation, bool) {
	status := res.Status
	if status == nil {
		return SAMFederation{}, false
	}
	peerCount := 0
	groupName := res.Name
	for _, peer := range allResources {
		if peer.Kind != "EventPeer" {
			continue
		}
		if statusAnyText(peer.Status["groupRef"]) == groupName || statusAnyText(peer.Status["group"]) == groupName {
			peerCount++
		}
	}
	return SAMFederation{
		GroupName:  groupName,
		Phase:      statusAnyText(status["phase"]),
		Reason:     statusAnyText(status["reason"]),
		PeerCount:  peerCount,
		ListenAddr: statusAnyText(status["listenAddress"]),
	}, true
}
