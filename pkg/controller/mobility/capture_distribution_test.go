// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"testing"
)

func TestDistributeCaptures_EvenSpread(t *testing.T) {
	nodes := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 128},
		{NodeRef: "node-b", MaxSecondaryIPs: 128},
	}
	var addresses []string
	for i := 1; i <= 18; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	dist := distributeCaptures(addresses, nodes)
	if len(dist.Assignments) != 18 {
		t.Fatalf("expected 18 assignments, got %d", len(dist.Assignments))
	}
	if dist.NodeCounts["node-a"] != 9 || dist.NodeCounts["node-b"] != 9 {
		t.Fatalf("expected 9/9 steady-state split, got %v", dist.NodeCounts)
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
	dist := distributeCaptures(addresses, nodes)
	if dist.NodeCounts["node-a"] > 5 {
		t.Fatalf("node-a exceeded capacity: %d", dist.NodeCounts["node-a"])
	}
	if dist.NodeCounts["node-b"] > 5 {
		t.Fatalf("node-b exceeded capacity: %d", dist.NodeCounts["node-b"])
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
	dist := distributeCaptures(addresses, nodes)
	if len(dist.Assignments) != 6 {
		t.Fatalf("expected 6 assigned (total capacity), got %d", len(dist.Assignments))
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
	dist1 := distributeCaptures(addresses, nodes)
	dist2 := distributeCaptures(addresses, nodes)
	for addr, node := range dist1.Assignments {
		if dist2.Assignments[addr] != node {
			t.Fatalf("non-deterministic: %s -> %s vs %s", addr, node, dist2.Assignments[addr])
		}
	}
}

func TestDistributeCaptures_KeepsLiveIncumbents(t *testing.T) {
	nodes := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 128},
		{NodeRef: "node-b", MaxSecondaryIPs: 128},
	}
	var addresses []string
	incumbents := map[string]string{}
	for i := 1; i <= 18; i++ {
		address := fmt.Sprintf("10.0.0.%d", i)
		addresses = append(addresses, address)
		incumbents[address] = "node-b"
	}
	dist := distributeCapturesWithIncumbents(addresses, nodes, incumbents)
	if dist.NodeCounts["node-b"] != 18 || dist.NodeCounts["node-a"] != 0 {
		t.Fatalf("expected live incumbent node-b to keep all captures, got %v", dist.NodeCounts)
	}
	if got := captureDistributionReasonCounts(&dist)["incumbent-kept"]; got != 18 {
		t.Fatalf("incumbent-kept reasons = %d, want 18", got)
	}
}

func TestDistributeCaptures_NoNodes(t *testing.T) {
	dist := distributeCaptures([]string{"10.0.0.1"}, nil)
	if len(dist.Assignments) != 0 {
		t.Fatalf("expected 0 assignments with no nodes, got %d", len(dist.Assignments))
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
	dist := distributeCaptures(addresses, nodes)
	if dist.NodeCounts["node-a"] != 30 {
		t.Fatalf("single node should get all: got %d", dist.NodeCounts["node-a"])
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
	dist := distributeCaptures(addresses, nodes)
	if len(dist.Assignments) != 100 {
		t.Fatalf("unlimited capacity nodes should assign all: got %d", len(dist.Assignments))
	}
}

func TestDistributeCaptures_FailoverRedistribution(t *testing.T) {
	nodesAll := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 128},
		{NodeRef: "node-b", MaxSecondaryIPs: 128},
	}
	nodesSurvivors := []captureDistributionNode{
		{NodeRef: "node-b", MaxSecondaryIPs: 128},
	}
	var addresses []string
	for i := 1; i <= 18; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	distBefore := distributeCaptures(addresses, nodesAll)
	distAfter := distributeCapturesWithIncumbents(addresses, nodesSurvivors, distBefore.Assignments)
	if len(distAfter.Assignments) != len(addresses) {
		t.Fatalf("expected all addresses assigned after failover, got %d", len(distAfter.Assignments))
	}
	if distAfter.NodeCounts["node-b"] != 18 {
		t.Fatalf("survivor should take all captures after failover, got %v", distAfter.NodeCounts)
	}
	if got := captureDistributionReasonCounts(&distAfter)["failover-reassigned"]; got != distBefore.NodeCounts["node-a"] {
		t.Fatalf("failover-reassigned reasons = %d, want failed node count %d", got, distBefore.NodeCounts["node-a"])
	}
}

func TestDistributeCaptures_RejoinDoesNotPreemptSurvivor(t *testing.T) {
	nodesAll := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 128},
		{NodeRef: "node-b", MaxSecondaryIPs: 128},
	}
	nodesSurvivors := []captureDistributionNode{
		{NodeRef: "node-b", MaxSecondaryIPs: 128},
	}
	var addresses []string
	for i := 1; i <= 18; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	steady := distributeCaptures(addresses, nodesAll)
	failover := distributeCapturesWithIncumbents(addresses, nodesSurvivors, steady.Assignments)
	rejoined := distributeCapturesWithIncumbents(addresses, nodesAll, failover.Assignments)
	if rejoined.NodeCounts["node-b"] != 18 || rejoined.NodeCounts["node-a"] != 0 {
		t.Fatalf("rejoined node should not preempt survivor captures, got %v", rejoined.NodeCounts)
	}
	if got := captureDistributionReasonCounts(&rejoined)["incumbent-kept"]; got != 18 {
		t.Fatalf("rejoin reason counts = %#v, want incumbent-kept=18", captureDistributionReasonCounts(&rejoined))
	}
}

func TestDistributeCaptures_ForceRebalanceAfterRejoin(t *testing.T) {
	nodesAll := []captureDistributionNode{
		{NodeRef: "node-a", MaxSecondaryIPs: 128},
		{NodeRef: "node-b", MaxSecondaryIPs: 128},
	}
	nodesSurvivors := []captureDistributionNode{
		{NodeRef: "node-b", MaxSecondaryIPs: 128},
	}
	var addresses []string
	for i := 1; i <= 18; i++ {
		addresses = append(addresses, fmt.Sprintf("10.0.0.%d", i))
	}
	steady := distributeCaptures(addresses, nodesAll)
	failover := distributeCapturesWithIncumbents(addresses, nodesSurvivors, steady.Assignments)
	rejoined := distributeCapturesWithIncumbents(addresses, nodesAll, failover.Assignments)
	if rejoined.NodeCounts["node-b"] != 18 || rejoined.NodeCounts["node-a"] != 0 {
		t.Fatalf("test setup expected survivor to retain all captures, got %v", rejoined.NodeCounts)
	}
	forced := distributeCapturesForRebalance(addresses, nodesAll)
	if forced.NodeCounts["node-a"] != 9 || forced.NodeCounts["node-b"] != 9 {
		t.Fatalf("forced rebalance should restore 9/9 split, got %v", forced.NodeCounts)
	}
	if got := captureDistributionReasonCounts(&forced)["hash-assigned"]; got != 18 {
		t.Fatalf("forced reason counts = %#v, want hash-assigned=18", captureDistributionReasonCounts(&forced))
	}
	if forced.Target != 9 {
		t.Fatalf("forced target = %d, want 9", forced.Target)
	}
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
