// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"testing"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
)

func TestDistributeCaptures_EvenSpread(t *testing.T) {
	nodes := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 10},
		{NodeRef: "node-b", MaxSecondaryIPs: 10},
	}
	var addresses []string
	for i := 1; i <= 20; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	assignments := distributeCaptures(addresses, nodes)
	requireCaptureAssignmentCoverage(t, addresses, nodes, assignments, len(addresses))
	if len(assignments) != 20 {
		t.Fatalf("expected 20 assignments, got %d", len(assignments))
	}
	counts := captureAssignmentCounts(assignments)
	if counts["node-a"] != 10 || counts["node-b"] != 10 {
		t.Fatalf("expected both nodes to get addresses: %v", counts)
	}
}

func TestDistributeCaptures_RespectsCapacity(t *testing.T) {
	nodes := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 5},
		{NodeRef: "node-b", MaxSecondaryIPs: 5},
	}
	var addresses []string
	for i := 1; i <= 10; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	assignments := distributeCaptures(addresses, nodes)
	requireCaptureAssignmentCoverage(t, addresses, nodes, assignments, len(addresses))
	counts := captureAssignmentCounts(assignments)
	if counts["node-a"] > 5 {
		t.Fatalf("node-a exceeded capacity: %d", counts["node-a"])
	}
	if counts["node-b"] > 5 {
		t.Fatalf("node-b exceeded capacity: %d", counts["node-b"])
	}
}

func TestDistributeCaptures_OverCapacity(t *testing.T) {
	nodes := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 3},
		{NodeRef: "node-b", MaxSecondaryIPs: 3},
	}
	var addresses []string
	for i := 1; i <= 10; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	assignments := distributeCaptures(addresses, nodes)
	requireCaptureAssignmentCoverage(t, addresses, nodes, assignments, 6)
	if len(assignments) != 6 {
		t.Fatalf("expected 6 assigned (total capacity), got %d", len(assignments))
	}
}

func TestDistributeCaptures_Deterministic(t *testing.T) {
	nodes := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 20},
		{NodeRef: "node-b", MaxSecondaryIPs: 20},
		{NodeRef: "node-c", MaxSecondaryIPs: 20},
	}
	var addresses []string
	for i := 1; i <= 30; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	assignments1 := distributeCaptures(addresses, nodes)
	assignments2 := distributeCaptures(addresses, nodes)
	requireCaptureAssignmentCoverage(t, addresses, nodes, assignments1, len(addresses))
	requireCaptureAssignmentCoverage(t, addresses, nodes, assignments2, len(addresses))
	for addr, node := range assignments1 {
		if assignments2[addr] != node {
			t.Fatalf("non-deterministic: %s -> %s vs %s", addr, node, assignments2[addr])
		}
	}
}

func TestDistributeCaptures_MinimalRedistribution(t *testing.T) {
	nodes3 := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 100},
		{NodeRef: "node-b", MaxSecondaryIPs: 100},
		{NodeRef: "node-c", MaxSecondaryIPs: 100},
	}
	nodes2 := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 100},
		{NodeRef: "node-b", MaxSecondaryIPs: 100},
	}
	var addresses []string
	for i := 1; i <= 90; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	assignments3 := distributeCaptures(addresses, nodes3)
	assignments2 := distributeCaptures(addresses, nodes2)
	requireCaptureAssignmentCoverage(t, addresses, nodes3, assignments3, len(addresses))
	requireCaptureAssignmentCoverage(t, addresses, nodes2, assignments2, len(addresses))
	moved := 0
	for addr, node3 := range assignments3 {
		if node2, ok := assignments2[addr]; ok && node3 != node2 {
			if node3 != "node-c" {
				moved++
			}
		}
	}
	if moved > 5 {
		t.Fatalf("too many non-node-c moves when removing node-c: %d (expected minimal)", moved)
	}
}

func TestDistributeCaptures_NoNodes(t *testing.T) {
	assignments := distributeCaptures([]string{"10.0.0.1"}, nil)
	if len(assignments) != 0 {
		t.Fatalf("expected 0 assignments with no nodes, got %d", len(assignments))
	}
}

func TestDistributeCaptures_SingleNode(t *testing.T) {
	nodes := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 50},
	}
	var addresses []string
	for i := 1; i <= 30; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	assignments := distributeCaptures(addresses, nodes)
	requireCaptureAssignmentCoverage(t, addresses, nodes, assignments, len(addresses))
	if got := captureAssignmentCounts(assignments)["node-a"]; got != 30 {
		t.Fatalf("single node should get all: got %d", got)
	}
}

func TestDistributeCaptures_UnlimitedCapacity(t *testing.T) {
	nodes := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 0},
		{NodeRef: "node-b", MaxSecondaryIPs: 0},
	}
	var addresses []string
	for i := 1; i <= 100; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	assignments := distributeCaptures(addresses, nodes)
	requireCaptureAssignmentCoverage(t, addresses, nodes, assignments, len(addresses))
	if len(assignments) != 100 {
		t.Fatalf("unlimited capacity nodes should assign all: got %d", len(assignments))
	}
}

func TestDistributeCaptures_FailoverRedistribution(t *testing.T) {
	nodesAll := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 15},
		{NodeRef: "node-b", MaxSecondaryIPs: 15},
		{NodeRef: "node-c", MaxSecondaryIPs: 15},
	}
	nodesSurvivors := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 15},
		{NodeRef: "node-b", MaxSecondaryIPs: 15},
	}
	var addresses []string
	for i := 1; i <= 30; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	assignmentsBefore := distributeCaptures(addresses, nodesAll)
	assignmentsAfter := distributeCaptures(addresses, nodesSurvivors)
	requireCaptureAssignmentCoverage(t, addresses, nodesAll, assignmentsBefore, len(addresses))
	requireCaptureAssignmentCoverage(t, addresses, nodesSurvivors, assignmentsAfter, len(addresses))
	for addr, nodeBefore := range assignmentsBefore {
		nodeAfter, ok := assignmentsAfter[addr]
		if !ok {
			t.Fatalf("address %s unassigned after failover", addr)
		}
		if nodeBefore == "node-c" {
			continue
		}
		if nodeBefore != nodeAfter {
			t.Fatalf("address %s moved from %s to %s (not from dead node)", addr, nodeBefore, nodeAfter)
		}
	}
	counts := captureAssignmentCounts(assignmentsAfter)
	if counts["node-a"] > 15 || counts["node-b"] > 15 {
		t.Fatalf("capacity exceeded after failover: %v", counts)
	}
}

func captureAssignmentCounts(assignments map[string]string) map[string]int {
	counts := make(map[string]int)
	for _, node := range assignments {
		counts[node]++
	}
	return counts
}

func TestDistributedCaptureEnabled(t *testing.T) {
	members := map[string]memberPlanInfo{
		"a": {NodeRef: "a", PlacementGroup: "grp", MaxSecondaryIPs: 10},
		"b": {NodeRef: "b", PlacementGroup: "grp", MaxSecondaryIPs: 0},
	}
	if !distributedCaptureEnabled(members, "grp") {
		t.Fatal("should be enabled when any member has MaxSecondaryIPs > 0")
	}
	members2 := map[string]memberPlanInfo{
		"a": {NodeRef: "a", PlacementGroup: "grp", MaxSecondaryIPs: 0},
		"b": {NodeRef: "b", PlacementGroup: "grp", MaxSecondaryIPs: 0},
	}
	if distributedCaptureEnabled(members2, "grp") {
		t.Fatal("should not be enabled when no member has MaxSecondaryIPs > 0")
	}
}

func TestDistributedLiveNodes_PartialMarkersUseFullGroup(t *testing.T) {
	members := map[string]memberPlanInfo{
		"node-a": {NodeRef: "node-a", PlacementGroup: "grp", MaxSecondaryIPs: 10},
		"node-b": {NodeRef: "node-b", PlacementGroup: "grp", MaxSecondaryIPs: 10},
	}
	markers := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("node-a"): "10.99.0.2/32",
	}

	live := distributedLiveNodes(members["node-a"], members, markers)
	if !live["node-a"] || !live["node-b"] || len(live) != 2 {
		t.Fatalf("partial marker live nodes = %#v, want full group to avoid split-brain capture assignment", live)
	}
}

func TestDistributedLiveNodes_NoMarkersUseFullGroup(t *testing.T) {
	members := map[string]memberPlanInfo{
		"node-a": {NodeRef: "node-a", PlacementGroup: "grp", MaxSecondaryIPs: 10},
		"node-b": {NodeRef: "node-b", PlacementGroup: "grp", MaxSecondaryIPs: 10},
	}

	live := distributedLiveNodes(members["node-a"], members, nil)
	if !live["node-a"] || !live["node-b"] || len(live) != 2 {
		t.Fatalf("missing marker live nodes = %#v, want full group to avoid split-brain capture assignment", live)
	}
}

func TestDistributedLiveNodes_CompleteMarkersUseObservedLiveSet(t *testing.T) {
	members := map[string]memberPlanInfo{
		"node-a": {NodeRef: "node-a", PlacementGroup: "grp", MaxSecondaryIPs: 10},
		"node-b": {NodeRef: "node-b", PlacementGroup: "grp", MaxSecondaryIPs: 10},
	}
	markers := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("node-a"): "10.99.0.2/32",
		bgpstate.MobilityNodeIdentityCommunity("node-b"): "10.99.0.3/32",
	}

	live := distributedLiveNodes(members["node-a"], members, markers)
	if !live["node-a"] || !live["node-b"] || len(live) != 2 {
		t.Fatalf("complete marker live nodes = %#v, want both observed nodes", live)
	}
}

func requireCaptureAssignmentCoverage(t *testing.T, addresses []string, nodes []captureDistributionNode, got map[string]string, count int) {
	t.Helper()
	expected := map[string]bool{}
	validNodes := map[string]bool{}
	for _, a := range addresses {
		expected[a] = true
	}
	for _, n := range nodes {
		validNodes[n.NodeRef] = true
	}
	if len(got) != count {
		t.Fatalf("capture assignments count=%d, want %d", len(got), count)
	}
	for a, n := range got {
		if !expected[a] || !validNodes[n] {
			t.Fatalf("unexpected capture assignment %s -> %s", a, n)
		}
	}
	if count == len(expected) {
		for a := range expected {
			if _, ok := got[a]; !ok {
				t.Fatalf("missing capture assignment %s", a)
			}
		}
	}
}
