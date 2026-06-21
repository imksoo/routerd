// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
	"strings"
)

type captureDistributionNode struct {
	NodeRef         string
	MaxSecondaryIPs int
}

type captureDistribution struct {
	Assignments map[string]string
	NodeCounts  map[string]int
	Reasons     map[string]string
	Target      int
}

func distributedCaptureEnabled(members map[string]memberPlanInfo, group string) bool {
	for _, m := range members {
		if strings.TrimSpace(m.PlacementGroup) != group || m.MaintenanceDrain {
			continue
		}
		if m.MaxSecondaryIPs > 0 {
			return true
		}
	}
	return false
}

func distributedCaptureNodes(members map[string]memberPlanInfo, group string, liveNodes map[string]bool) []captureDistributionNode {
	var nodes []captureDistributionNode
	for _, m := range members {
		if strings.TrimSpace(m.PlacementGroup) != group || m.MaintenanceDrain {
			continue
		}
		if liveNodes != nil && !liveNodes[m.NodeRef] {
			continue
		}
		nodes = append(nodes, captureDistributionNode{
			NodeRef:         m.NodeRef,
			MaxSecondaryIPs: m.MaxSecondaryIPs,
		})
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].NodeRef < nodes[j].NodeRef
	})
	return nodes
}

func distributeCaptures(addresses []string, nodes []captureDistributionNode) captureDistribution {
	return distributeCapturesWithIncumbents(addresses, nodes, nil)
}

func distributeCapturesWithIncumbents(addresses []string, nodes []captureDistributionNode, incumbents map[string]string) captureDistribution {
	return distributeCapturesWithOptions(addresses, nodes, incumbents, false)
}

func distributeCapturesForRebalance(addresses []string, nodes []captureDistributionNode) captureDistribution {
	return distributeCapturesWithOptions(addresses, nodes, nil, true)
}

func distributeCapturesWithOptions(addresses []string, nodes []captureDistributionNode, incumbents map[string]string, forceRebalance bool) captureDistribution {
	dist := captureDistribution{
		Assignments: make(map[string]string, len(addresses)),
		NodeCounts:  make(map[string]int, len(nodes)),
		Reasons:     make(map[string]string, len(addresses)),
	}
	if len(nodes) == 0 {
		return dist
	}
	live := map[string]captureDistributionNode{}
	for _, node := range nodes {
		dist.NodeCounts[node.NodeRef] = 0
		live[node.NodeRef] = node
	}
	sort.Strings(addresses)
	target := (len(addresses) + len(nodes) - 1) / len(nodes)
	dist.Target = target
	var remaining []string
	pendingReasons := map[string]string{}
	for _, address := range addresses {
		if forceRebalance {
			remaining = append(remaining, address)
			continue
		}
		incumbent := strings.TrimSpace(incumbents[address])
		if incumbent == "" {
			remaining = append(remaining, address)
			continue
		}
		if _, ok := live[incumbent]; !ok {
			pendingReasons[address] = "failover-reassigned"
			remaining = append(remaining, address)
			continue
		}
		dist.Assignments[address] = incumbent
		dist.NodeCounts[incumbent]++
		dist.Reasons[address] = "incumbent-kept"
	}
	for _, address := range remaining {
		best, reason := assignByBalancedRendezvous(address, nodes, dist.NodeCounts, target)
		if best != "" {
			dist.Assignments[address] = best
			dist.NodeCounts[best]++
			if pendingReasons[address] != "" {
				reason = pendingReasons[address]
			}
			dist.Reasons[address] = reason
		}
	}
	return dist
}

func captureDistributionReasonCounts(dist *captureDistribution) map[string]int {
	if dist == nil || len(dist.Reasons) == 0 {
		return nil
	}
	out := map[string]int{}
	for _, reason := range dist.Reasons {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		out[reason]++
	}
	return out
}

func assignByBalancedRendezvous(address string, nodes []captureDistributionNode, counts map[string]int, target int) (string, string) {
	candidates := rendezvousCandidates(address, nodes, counts)
	for _, candidate := range candidates {
		if counts[candidate.node] < target {
			return candidate.node, "hash-assigned"
		}
	}
	if len(candidates) == 0 {
		return "", ""
	}
	return candidates[0].node, "capacity-overflow"
}

func rendezvousCandidates(address string, nodes []captureDistributionNode, counts map[string]int) []scoredCaptureNode {
	candidates := make([]scoredCaptureNode, 0, len(nodes))
	for _, node := range nodes {
		if node.MaxSecondaryIPs > 0 && counts[node.NodeRef] >= node.MaxSecondaryIPs {
			continue
		}
		candidates = append(candidates, scoredCaptureNode{
			node:  node.NodeRef,
			score: rendezvousScore(address, node.NodeRef),
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].node < candidates[j].node
	})
	return candidates
}

type scoredCaptureNode struct {
	node  string
	score uint64
}

func distributedLiveNodes(self memberPlanInfo, members map[string]memberPlanInfo, livenessMarkers map[string]string) map[string]bool {
	group := strings.TrimSpace(self.PlacementGroup)
	live := map[string]bool{self.NodeRef: true}
	for _, m := range members {
		if strings.TrimSpace(m.PlacementGroup) != group || m.MaintenanceDrain {
			continue
		}
		if m.NodeRef == self.NodeRef {
			continue
		}
		if _, _, present := livenessMarkerForNode(livenessMarkers, m.NodeRef); present {
			live[m.NodeRef] = true
		}
	}
	return live
}

func bgpLiveNodesFromMarkers(self memberPlanInfo, members map[string]memberPlanInfo, livenessMarkers map[string]string, observed bool) map[string]bool {
	if !observed {
		return nil
	}
	live := map[string]bool{}
	if selfNode := strings.TrimSpace(self.NodeRef); selfNode != "" {
		live[selfNode] = true
	}
	for _, member := range members {
		nodeRef := strings.TrimSpace(member.NodeRef)
		if nodeRef == "" {
			continue
		}
		if _, _, present := livenessMarkerForNode(livenessMarkers, nodeRef); present {
			live[nodeRef] = true
		}
	}
	return live
}

func rendezvousScore(address, nodeRef string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(address))
	var sep [1]byte
	_, _ = h.Write(sep[:])
	_, _ = h.Write([]byte(nodeRef))
	v := h.Sum64()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	h.Reset()
	_, _ = h.Write(buf[:])
	return h.Sum64()
}
