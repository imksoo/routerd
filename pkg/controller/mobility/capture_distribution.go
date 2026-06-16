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
	dist := captureDistribution{
		Assignments: make(map[string]string, len(addresses)),
		NodeCounts:  make(map[string]int, len(nodes)),
	}
	if len(nodes) == 0 {
		return dist
	}
	for _, node := range nodes {
		dist.NodeCounts[node.NodeRef] = 0
	}
	sort.Strings(addresses)
	for _, address := range addresses {
		best := assignByRendezvous(address, nodes, dist.NodeCounts)
		if best != "" {
			dist.Assignments[address] = best
			dist.NodeCounts[best]++
		}
	}
	return dist
}

func assignByRendezvous(address string, nodes []captureDistributionNode, counts map[string]int) string {
	type scored struct {
		node  string
		score uint64
	}
	candidates := make([]scored, 0, len(nodes))
	for _, node := range nodes {
		if node.MaxSecondaryIPs > 0 && counts[node.NodeRef] >= node.MaxSecondaryIPs {
			continue
		}
		candidates = append(candidates, scored{
			node:  node.NodeRef,
			score: rendezvousScore(address, node.NodeRef),
		})
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].node < candidates[j].node
	})
	return candidates[0].node
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
