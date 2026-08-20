// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPlacementDecision(t *testing.T) {
	base := placementPoolSpec()
	members := plannerMembers(base.Members)
	if got := evaluatePlacementWithIncumbent(members["azure-router-a"], members, ""); !got.Active || got.ActiveNode != "azure-router-a" {
		t.Fatalf("router-a placement = %+v, want active", got)
	}
	if got := evaluatePlacementWithIncumbent(members["azure-router-b"], members, ""); got.Active || got.ActiveNode != "azure-router-a" {
		t.Fatalf("router-b placement = %+v, want standby", got)
	}
	base.Members[1].Maintenance.Drain = true
	members = plannerMembers(base.Members)
	if got := evaluatePlacementWithIncumbent(members["azure-router-b"], members, ""); !got.Active || got.ActiveNode != "azure-router-b" {
		t.Fatalf("router-b after drain = %+v, want active", got)
	}
	base.Members[2].Maintenance.Drain = true
	members = plannerMembers(base.Members)
	if got := evaluatePlacementWithIncumbent(members["azure-router-b"], members, ""); got.Active || got.ActiveNode != "" {
		t.Fatalf("all drained placement = %+v, want fail-closed", got)
	}
	ungrouped := plannerMembers(plannedPoolSpec().Members)
	if got := evaluatePlacementWithIncumbent(ungrouped["azure-router"], ungrouped, ""); !got.Active || got.ActiveNode != "azure-router" {
		t.Fatalf("ungrouped placement = %+v, want active", got)
	}
}

func TestFIBVerdictsEmitNormalizedPoolScope(t *testing.T) {
	self := memberPlanInfo{NodeRef: "aws-router-a", Site: "aws", Capture: api.MobilityMemberCapture{Type: "provider-secondary-ip", Interface: "ens5"}}
	peer := memberPlanInfo{NodeRef: "aws-router-b", Site: "aws"}
	remote := memberPlanInfo{NodeRef: "azure-router", Site: "azure"}
	pool := NormalizedMobilityPool{
		Name:                 "cloudedge",
		SelfCaptureInterface: "ens5",
		Spec: api.MobilityPoolSpec{
			Prefix: "10.77.60.7/24",
		},
		Prefix: netip.MustParsePrefix("10.77.60.7/24").Masked(),
		Self:   self,
		Members: map[string]memberPlanInfo{
			self.NodeRef:   self,
			peer.NodeRef:   peer,
			remote.NodeRef: remote,
		},
	}
	verdicts, routes := planFIB(pool, []ownershipDecision{{
		Address:       "10.77.60.10/32",
		Class:         ownershipClassStaticOwned,
		HomeOwnerNode: self.NodeRef,
	}})
	if len(verdicts) != 2 || verdicts[0].Scope == nil {
		t.Fatalf("FIB verdicts = %#v, want scope plus address verdict", verdicts)
	}
	scope := verdicts[0].Scope
	if scope.Prefix != "10.77.60.0/24" || scope.PreferredSource != "10.77.60.10/32" {
		t.Fatalf("scope = %#v", scope)
	}
	communities := map[string]bool{}
	for _, community := range scope.RemoteReturnCommunities {
		communities[community] = true
	}
	for _, node := range []string{"azure-router"} {
		if !communities[bgpstate.MobilityNodeIdentityCommunity(node)] {
			t.Fatalf("scope communities = %#v, want remote-site %s", scope.RemoteReturnCommunities, node)
		}
	}
	for _, node := range []string{"aws-router-a", "aws-router-b"} {
		if communities[bgpstate.MobilityNodeIdentityCommunity(node)] {
			t.Fatalf("scope communities = %#v, must exclude same-site %s", scope.RemoteReturnCommunities, node)
		}
	}
	if verdicts[1].Action != "local-route" || verdicts[1].Class != ownershipClassStaticOwned {
		t.Fatalf("address verdict = %#v", verdicts[1])
	}
	if len(routes) != 1 {
		t.Fatalf("local inventory route intents = %#v, want one route", routes)
	}
	route := routes[0]
	if route.ID != "mobility-cloudedge-local-10-77-60-10" || route.Purpose != dynamicconfig.MobilityIPv4RoutePurposeLocalInventory || route.Destination != "10.77.60.10/32" || route.Device != "ens5" || route.Metric != 1 {
		t.Fatalf("local inventory route intent = %#v", route)
	}
}

func TestPlacementAutoPriority(t *testing.T) {
	spec := placementPoolSpec()
	placementMembers := func() []api.ResolvedMobilityPoolMember {
		members := append([]api.ResolvedMobilityPoolMember(nil), spec.Members...)
		for i := range members {
			if members[i].NodeRef == "azure-router-a" {
				continue
			}
			members[i].ProfileRef = ""
			members[i].Capture = api.MobilityMemberCapture{}
			members[i].StaticOwnedAddresses = nil
			members[i].OwnershipDiscovery = api.MobilityOwnershipDiscovery{}
		}
		return members
	}
	spec.Members[1].Placement.Priority = 0
	spec.Members[2].Placement.Priority = 0
	normalizedMembers, err := mobilityconfig.NormalizeResolvedMobilityPoolMembers(spec.MobilityPoolSpec, placementMembers(), "azure-router-a")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	members := plannerMembers(normalizedMembers)
	if got := members["azure-router-a"].PlacementPriority; got != 10 {
		t.Fatalf("azure-router-a auto priority = %d, want 10", got)
	}
	if got := members["azure-router-b"].PlacementPriority; got != 20 {
		t.Fatalf("azure-router-b auto priority = %d, want 20", got)
	}
	if got := evaluatePlacementWithIncumbent(members["azure-router-a"], members, ""); !got.Active || got.ActiveNode != "azure-router-a" {
		t.Fatalf("auto priority placement = %+v, want router-a active", got)
	}

	spec.Members[1].Placement.Priority = 20
	spec.Members[2].Placement.Priority = 0
	normalizedMembers, err = mobilityconfig.NormalizeResolvedMobilityPoolMembers(spec.MobilityPoolSpec, placementMembers(), "azure-router-a")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	members = plannerMembers(normalizedMembers)
	if got := members["azure-router-a"].PlacementPriority; got != 20 {
		t.Fatalf("explicit azure-router-a priority = %d, want 20", got)
	}
	if got := members["azure-router-b"].PlacementPriority; got != 10 {
		t.Fatalf("azure-router-b auto priority = %d, want first free 10", got)
	}
	if got := evaluatePlacementWithIncumbent(members["azure-router-b"], members, ""); !got.Active || got.ActiveNode != "azure-router-b" {
		t.Fatalf("mixed priority placement = %+v, want explicit priority respected and router-b active", got)
	}
}

func equalPriorityPlacementMembers() map[string]memberPlanInfo {
	return map[string]memberPlanInfo{
		"aws-router-a": {
			NodeRef:           "aws-router-a",
			Capture:           api.MobilityMemberCapture{Type: "provider-secondary-ip", NICRef: "eni-a"},
			PlacementGroup:    "aws-edge",
			PlacementPriority: 10,
		},
		"aws-router-b": {
			NodeRef:           "aws-router-b",
			Capture:           api.MobilityMemberCapture{Type: "provider-secondary-ip", NICRef: "eni-b"},
			PlacementGroup:    "aws-edge",
			PlacementPriority: 10,
		},
	}
}

func TestPlacementEqualPriorityPrefersIncumbentHolderNoPreempt(t *testing.T) {
	members := equalPriorityPlacementMembers()
	// No observed holder yet: deterministic NodeRef tie-break bootstraps router-a.
	if got := evaluatePlacementWithIncumbent(members["aws-router-a"], members, ""); !got.Active || got.ActiveNode != "aws-router-a" {
		t.Fatalf("bootstrap placement = %+v, want aws-router-a active", got)
	}
	// router-b seized during a failover and now holds the captures. A returning
	// equal-priority router-a must defer to the incumbent instead of preempting.
	if got := evaluatePlacementWithIncumbent(members["aws-router-a"], members, "aws-router-b"); got.Active || got.ActiveNode != "aws-router-b" {
		t.Fatalf("returning aws-router-a with incumbent b = %+v, want standby (no preempt)", got)
	}
	if got := evaluatePlacementWithIncumbent(members["aws-router-b"], members, "aws-router-b"); !got.Active || got.ActiveNode != "aws-router-b" {
		t.Fatalf("incumbent aws-router-b = %+v, want stays active", got)
	}
}

func TestPlacementUnequalPriorityReclaimsDespiteIncumbent(t *testing.T) {
	members := equalPriorityPlacementMembers()
	b := members["aws-router-b"]
	b.PlacementPriority = 20
	members["aws-router-b"] = b
	// router-b seized while router-a was down, but router-a has strictly higher
	// priority, so it reclaims on return (incumbent override only applies on ties).
	if got := evaluatePlacementWithIncumbent(members["aws-router-a"], members, "aws-router-b"); !got.Active || got.ActiveNode != "aws-router-a" {
		t.Fatalf("higher-priority aws-router-a with incumbent b = %+v, want reclaim", got)
	}
	if got := evaluatePlacementWithIncumbent(members["aws-router-b"], members, "aws-router-b"); got.Active || got.ActiveNode != "aws-router-a" {
		t.Fatalf("incumbent b defers to higher-priority a = %+v, want standby", got)
	}
}

func TestPlacementSettleDefersReturningNodeUntilConverged(t *testing.T) {
	settle := 120 * time.Second
	// A returning node would win the equal-priority tie-break (active, no incumbent
	// observed yet) but is still inside the settle window: it must defer so it does
	// not preempt before the live peer surfaces.
	if !placementStartupFenceDefersActive(true, "", 10*time.Second, settle, placementStartupReadiness{}) {
		t.Fatalf("returning node inside settle should defer")
	}
	// Once the incumbent peer is observed, the tie-break already defers; the fence
	// does not need to (and must not block legitimate post-settle behaviour).
	if placementStartupFenceDefersActive(true, "aws-router-b", 10*time.Second, settle, placementStartupReadiness{}) {
		t.Fatalf("observed incumbent should not be fenced")
	}
	// After the settle window, normal placement applies (cold-start winner claims).
	if placementStartupFenceDefersActive(true, "", settle+time.Second, settle, placementStartupReadiness{}) {
		t.Fatalf("after settle window should not defer")
	}
	// A standby (not asserting active) is never fenced.
	if placementStartupFenceDefersActive(false, "", 1*time.Second, settle, placementStartupReadiness{}) {
		t.Fatalf("standby should not be fenced")
	}
}

func TestPlacementStartupFenceUsesReadiness(t *testing.T) {
	settle := 120 * time.Second
	notReady := placementStartupReadiness{Known: true, BGPObserved: false, ProviderRequired: true, ProviderObserved: false}
	if !placementStartupFenceDefersActive(true, "", settle+time.Second, settle, notReady) {
		t.Fatalf("not-ready startup should remain fenced before fallback window")
	}
	if placementStartupFenceDefersActive(true, "", placementStartupReadinessFallbackWindow(settle)+time.Second, settle, notReady) {
		t.Fatalf("not-ready startup should release after fallback window")
	}
	ready := placementStartupReadiness{Known: true, BGPObserved: true, ProviderRequired: true, ProviderObserved: true}
	if placementStartupFenceDefersActive(true, "", 10*time.Second, settle, ready) {
		t.Fatalf("ready startup should not wait for wall-clock settle")
	}
	bgpOnly := placementStartupReadiness{Known: true, BGPObserved: true, ProviderRequired: false}
	if placementStartupFenceDefersActive(true, "", 10*time.Second, settle, bgpOnly) {
		t.Fatalf("startup without provider capture should release after BGP observation")
	}
	if placementStartupFenceDefersActive(true, "aws-router-b", settle+time.Second, settle, notReady) {
		t.Fatalf("observed incumbent should not be readiness-fenced")
	}
}

func TestFencePlacementForStartupConvertsActiveToStandby(t *testing.T) {
	now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	startedAt := now.Add(-30 * time.Second)
	settleWindow := 120 * time.Second

	active := PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: "aws-router-a"}
	got := fencePlacementForStartupWithReadiness(active, "", now, startedAt, settleWindow, placementStartupReadiness{})
	if got.Active || got.Seize {
		t.Fatalf("fenced placement = %+v, want standby", got)
	}
	// With an observed incumbent the decision is left untouched.
	withIncumbent := fencePlacementForStartupWithReadiness(PlacementDecision{Group: "aws-edge", Active: true}, "aws-router-b", now, startedAt, settleWindow, placementStartupReadiness{})
	if !withIncumbent.Active {
		t.Fatalf("incumbent-observed placement must not be fenced: %+v", withIncumbent)
	}
}

func TestFencePlacementForStartupWithReadiness(t *testing.T) {
	now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	startedAt := now.Add(-180 * time.Second)
	settleWindow := 120 * time.Second

	active := PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: "aws-router-a"}
	notReady := placementStartupReadiness{Known: true, BGPObserved: true, ProviderRequired: true, ProviderObserved: false}
	got := fencePlacementForStartupWithReadiness(active, "", now, startedAt, settleWindow, notReady)
	if got.Active || !strings.Contains(got.Reason, "startup readiness") {
		t.Fatalf("not-ready fenced placement = %+v, want readiness standby", got)
	}
	got = fencePlacementForStartupWithReadiness(active, "", now.Add(placementStartupReadinessFallbackWindow(settleWindow)), startedAt, settleWindow, notReady)
	if !got.Active {
		t.Fatalf("not-ready placement should release after fallback window: %+v", got)
	}
	ready := placementStartupReadiness{Known: true, BGPObserved: true, ProviderRequired: true, ProviderObserved: true}
	got = fencePlacementForStartupWithReadiness(active, "", now.Add(-290*time.Second), startedAt, settleWindow, ready)
	if !got.Active {
		t.Fatalf("ready placement should remain active inside settle window: %+v", got)
	}
}

func TestHigherPriorityHolderActive(t *testing.T) {
	members := equalPriorityPlacementMembers()
	b := members["aws-router-b"]
	b.PlacementPriority = 20
	members["aws-router-b"] = b
	// aws-router-b (priority 20) holds; aws-router-a (priority 10) is the active holder
	// beacon -> b must yield to the higher-priority a.
	if !higherPriorityHolderActive(members["aws-router-b"], members, "aws-router-a") {
		t.Fatalf("router-b should yield to higher-priority router-a")
	}
	// Equal priority: the peer holder is not higher priority, so do not yield.
	eq := equalPriorityPlacementMembers()
	if higherPriorityHolderActive(eq["aws-router-a"], eq, "aws-router-b") {
		t.Fatalf("equal-priority peer must not trigger yield")
	}
	// No observed holder -> never yield.
	if higherPriorityHolderActive(members["aws-router-a"], members, "") {
		t.Fatalf("empty holder must not trigger yield")
	}
}

func TestApplyHolderRetentionKeepsHolderActive(t *testing.T) {
	settleWindow := 120 * time.Second
	now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)

	// Past the settle window, a node that still holds its captures must stay active
	// even when the base decision (deterministic tie-break / peer observation) would
	// make it stand by — the live holder never yields just because a peer is seen.
	startedAt := now.Add(-1000 * time.Second)
	standby := PlacementDecision{Group: "aws-edge", Active: false, ActiveNode: "aws-router-a"}
	if got := applyHolderRetention(standby, true, false, now, startedAt, settleWindow); !got.Active {
		t.Fatalf("holder past settle = %+v, want retained active", got)
	}
	// A node that does not hold is not retained.
	if got := applyHolderRetention(standby, false, false, now, startedAt, settleWindow); got.Active {
		t.Fatalf("non-holder = %+v, want standby", got)
	}
	// A strictly higher-priority peer is the active holder: the local holder must
	// yield (no retention) so the configured priority restore can complete.
	if got := applyHolderRetention(standby, true, true, now, startedAt, settleWindow); got.Active {
		t.Fatalf("holder yielding to higher priority = %+v, want standby", got)
	}
	// Inside the settle window the selfHolds signal may be the returning node's stale
	// memory, so retention must not apply (the fence keeps it passive instead).
	startedAt = now.Add(-30 * time.Second)
	if got := applyHolderRetention(standby, true, false, now, startedAt, settleWindow); got.Active {
		t.Fatalf("holder inside settle = %+v, want not retained (stale signal)", got)
	}
}

func TestBGPObservedGroupHolderFromBestPathBeacon(t *testing.T) {
	members := equalPriorityPlacementMembers()
	markers := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.70.11/32",
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.70.12/32",
	}
	// The active holder advertises the owner /32 at the active preference, so the
	// best path carries its node-identity community: a returning aws-router-a observes
	// aws-router-b as the holder.
	beacon := map[string][]string{
		"10.77.60.12/32": {"64512:100", bgpMobilityCommunityActiveHolder, bgpstate.MobilityNodeIdentityCommunity("aws-router-b")},
	}
	if got := bgpObservedGroupHolder(members["aws-router-a"], members, markers, beacon); got != "aws-router-b" {
		t.Fatalf("holder from beacon = %q, want aws-router-b", got)
	}
	// Without the active-holder beacon (a standby low-pref / cold-start advertisement)
	// the peer is NOT treated as holder -- this prevents the cold-start deadlock.
	standbyOnly := map[string][]string{
		"10.77.60.12/32": {"64512:100", bgpstate.MobilityNodeIdentityCommunity("aws-router-b")},
	}
	if got := bgpObservedGroupHolder(members["aws-router-a"], members, markers, standbyOnly); got != "" {
		t.Fatalf("holder from non-active advertisement = %q, want empty", got)
	}
	// From the holder's own perspective there is no peer holder (self excluded).
	if got := bgpObservedGroupHolder(members["aws-router-b"], members, markers, beacon); got != "" {
		t.Fatalf("holder from holder node = %q, want empty (retention keeps it active)", got)
	}
	// No best-path beacon yet (cold start) -> empty -> deterministic ordering.
	if got := bgpObservedGroupHolder(members["aws-router-a"], members, markers, map[string][]string{}); got != "" {
		t.Fatalf("holder with empty RIB = %q, want empty (bootstrap)", got)
	}
	// A community that is not a group peer's (e.g. a remote site) is ignored.
	remote := map[string][]string{
		"10.77.60.10/32": {bgpstate.MobilityNodeIdentityCommunity("onprem-router")},
	}
	if got := bgpObservedGroupHolder(members["aws-router-a"], members, markers, remote); got != "" {
		t.Fatalf("holder from non-peer community = %q, want empty", got)
	}
}

func TestBGPCapturePlacementEqualPriorityNoPreemptButFailsOver(t *testing.T) {
	members := equalPriorityPlacementMembers()
	bothLive := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.0.2/32",
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	}
	// Both routers live, router-b is the incumbent holder: returning router-a must
	// not preempt or seize.
	if got := evaluateBGPCapturePlacement(members["aws-router-a"], members, bothLive, true, "aws-router-b"); got.Active || got.Seize || got.ActiveNode != "aws-router-b" {
		t.Fatalf("equal-priority no-preempt = %+v, want aws-router-a standby", got)
	}
	// Incumbent router-b then dies (marker absent): router-a must still seize so a
	// genuine failure fails over even though router-b was the recorded holder.
	bDead := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.0.2/32",
	}
	if got := evaluateBGPCapturePlacement(members["aws-router-a"], members, bDead, true, "aws-router-b"); !got.Active || !got.Seize || got.ActiveNode != "aws-router-a" {
		t.Fatalf("incumbent dead failover = %+v, want aws-router-a seize", got)
	}
}

func TestProviderActionPlansRouteTableStrategy(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "aws"}
	capture := api.MobilityMemberCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "aws-provider",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "eni-router",
		Target: map[string]string{
			"region":        "ap-northeast-1",
			"routeTableRef": "rtb-123",
		},
	}
	plans, err := providerActionPlans("cloudedge", profile, capture, "10.88.60.10/32", map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	assign := findActionPlanByAddress(plans, actionAssignRouteTableRoute, "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want route-table assign action", plans)
	}
	if assign.Target["routeTableRef"] != "rtb-123" || assign.Target["nicRef"] != "eni-router" || assign.Target["captureStrategy"] != captureStrategyRouteTable {
		t.Fatalf("assign target = %#v, want route table target", assign.Target)
	}
	if assign.Undo == nil || assign.Undo.Action != actionUnassignRouteTableRoute {
		t.Fatalf("assign undo = %#v, want route-table unassign", assign.Undo)
	}
	if assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign parameters = %#v, want allowReassignment", assign.Parameters)
	}
	unassign, err := providerCaptureActionPlan("cloudedge", profile, capture, "10.88.60.10/32", false, false, time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("route-table unassign: %v", err)
	}
	if unassign.Action != actionUnassignRouteTableRoute || unassign.Undo == nil || unassign.Undo.Action != actionAssignRouteTableRoute {
		t.Fatalf("route-table unassign = %#v", unassign)
	}
}

func TestProviderActionPlansRouteTableStrategyRequiresRouteTableRef(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "aws"}
	capture := api.MobilityMemberCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "aws-provider",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "eni-router",
		Target:          map[string]string{"region": "ap-northeast-1"},
	}
	_, err := providerActionPlans("cloudedge", profile, capture, "10.88.60.10/32", map[string]bool{}, false)
	if err == nil || !strings.Contains(err.Error(), "capture.captureStrategy route-table requires capture.target.routeTableRef") {
		t.Fatalf("providerActionPlans error = %v, want missing routeTableRef", err)
	}
}

func TestProviderActionPlansAzureRouteTableStrategyRequiresNextHopIPAddress(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "azure"}
	capture := api.MobilityMemberCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "azure-provider",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
		Target: map[string]string{
			"routeTableRef": "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/routeTables/rt-cloudedge",
		},
	}
	_, err := providerActionPlans("cloudedge", profile, capture, "10.88.60.10/32", map[string]bool{}, false)
	if err == nil || !strings.Contains(err.Error(), "provider azure capture.captureStrategy route-table requires capture.target.nextHopIPAddress") {
		t.Fatalf("providerActionPlans error = %v, want missing nextHopIPAddress", err)
	}
}

func TestProviderActionPlansOCIRouteTableStrategy(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "oci"}
	capture := api.MobilityMemberCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "oci-provider",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "ocid1.vnic.oc1..router",
		Target: map[string]string{
			"routeTableRef":    "ocid1.routetable.oc1..rt1",
			"nextHopIPAddress": "10.88.60.1",
		},
	}
	plans, err := providerActionPlans("cloudedge", profile, capture, "10.88.60.10/32", map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	assign := findActionPlanByAddress(plans, actionAssignRouteTableRoute, "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want route-table assign action", plans)
	}
	if assign.Target["routeTableRef"] != "ocid1.routetable.oc1..rt1" ||
		assign.Target["nextHopIPAddress"] != "10.88.60.1" ||
		assign.Target["nicRef"] != "ocid1.vnic.oc1..router" ||
		assign.Target["captureStrategy"] != captureStrategyRouteTable {
		t.Fatalf("assign target = %#v, want oci route table target", assign.Target)
	}
}

func TestProviderActionPlansOCIRouteTableStrategyRequiresNextHopIPAddress(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "oci"}
	capture := api.MobilityMemberCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "oci-provider",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "ocid1.vnic.oc1..router",
		Target:          map[string]string{"routeTableRef": "ocid1.routetable.oc1..rt1"},
	}
	_, err := providerActionPlans("cloudedge", profile, capture, "10.88.60.10/32", map[string]bool{}, false)
	if err == nil || !strings.Contains(err.Error(), "provider oci capture.captureStrategy route-table requires capture.target.nextHopIPAddress") {
		t.Fatalf("providerActionPlans error = %v, want missing nextHopIPAddress", err)
	}
}

func TestProviderActionTargetUsesCanonicalCaptureNICRef(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "azure", SubscriptionID: "sub-1", ResourceGroup: "rg-router"}
	target := providerActionTarget("cloudedge", profile, api.MobilityMemberCapture{
		Type:        "provider-secondary-ip",
		ProviderRef: "azure-provider",
		NICRef:      "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
	}, "10.88.60.10/32")
	if target["nicRef"] == "" {
		t.Fatalf("target = %#v, want canonical capture nicRef", target)
	}
	if target["ipConfigName"] == "" {
		t.Fatalf("target = %#v, want provider fields derived with canonical nicRef", target)
	}
}

func TestProviderCaptureRefFromCaptureUsesCanonicalNICRef(t *testing.T) {
	if got := providerCaptureRefFromCapture(api.MobilityMemberCapture{
		NICRef: "eni-canonical",
		Target: map[string]string{"nicRef": "eni-ignored"},
	}); got != "eni-canonical" {
		t.Fatalf("provider capture ref = %q, want canonical nicRef", got)
	}
	if got := providerCaptureRefFromCapture(api.MobilityMemberCapture{
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "eni-canonical",
		Target:          map[string]string{"routeTableRef": "rtb-123"},
	}); got != "rtb-123" {
		t.Fatalf("route-table capture ref = %q, want route table", got)
	}
}

func TestProviderActionPlansUsesCanonicalCaptureNICRef(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "azure", SubscriptionID: "sub-1", ResourceGroup: "rg-router"}
	captureTarget := map[string]string{
		"region":       "japaneast",
		"ipConfigName": "capture-a",
	}
	capture := api.MobilityMemberCapture{
		Type:        "provider-secondary-ip",
		ProviderRef: "azure-provider",
		NICRef:      "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
		Target:      captureTarget,
	}
	plans, err := providerActionPlans("cloudedge", profile, capture, "10.88.60.10/32", map[string]bool{}, false)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	assign := findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want assign plan", plans)
	}
	if assign.Target["nicRef"] != capture.NICRef {
		t.Fatalf("assign target = %#v, want canonical capture nicRef", assign.Target)
	}

	unassign, err := providerCaptureActionPlan("cloudedge", profile, capture, "10.88.60.10/32", false, false, time.Time{})
	if err != nil {
		t.Fatalf("providerCaptureActionPlan: %v", err)
	}
	if unassign.Target["nicRef"] != capture.NICRef {
		t.Fatalf("unassign target = %#v, want canonical capture nicRef", unassign.Target)
	}
}

func TestBGPCapturePlacementSeizesWhenActiveMarkerAbsent(t *testing.T) {
	members := map[string]memberPlanInfo{
		"aws-router-a": {
			NodeRef:            "aws-router-a",
			Capture:            api.MobilityMemberCapture{Type: "provider-secondary-ip"},
			PlacementGroup:     "aws-edge",
			PlacementPriority:  10,
			MaintenanceDrain:   false,
			OwnershipDiscovery: api.MobilityOwnershipDiscovery{},
		},
		"aws-router-b": {
			NodeRef:           "aws-router-b",
			Capture:           api.MobilityMemberCapture{Type: "provider-secondary-ip"},
			PlacementGroup:    "aws-edge",
			PlacementPriority: 20,
		},
	}
	markers := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	}
	got := evaluateBGPCapturePlacement(members["aws-router-b"], members, markers, true, "")
	if !got.Active || !got.Seize || got.ActiveNode != "aws-router-b" {
		t.Fatalf("placement = %+v, want failover seize by aws-router-b", got)
	}
	if got.SelfCommunity != bgpstate.MobilityNodeIdentityCommunity("aws-router-b") || !got.SelfMarkerPresent {
		t.Fatalf("self liveness = %+v, want canonical aws-router-b marker present", got)
	}
	if got.ActiveCommunity != bgpstate.MobilityNodeIdentityCommunity("aws-router-a") || got.ActiveMarkerPresent {
		t.Fatalf("active liveness = %+v, want canonical aws-router-a marker absent", got)
	}
}

func TestBGPCapturePlacementRecognizesCanonicalMarkerForActiveMember(t *testing.T) {
	members := map[string]memberPlanInfo{
		"azure-router-a": {
			NodeRef:           "azure-router-a",
			Capture:           api.MobilityMemberCapture{Type: "provider-secondary-ip"},
			PlacementGroup:    "azure-edge",
			PlacementPriority: 10,
		},
		"azure-router-b": {
			NodeRef:           "azure-router-b",
			Capture:           api.MobilityMemberCapture{Type: "provider-secondary-ip"},
			PlacementGroup:    "azure-edge",
			PlacementPriority: 20,
		},
	}
	present := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-a"): "10.99.0.3/32",
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	}
	if got := evaluateBGPCapturePlacement(members["azure-router-b"], members, present, true, ""); got.Active || got.Seize || !got.ActiveMarkerPresent {
		t.Fatalf("placement with active canonical marker = %+v, want standby defer", got)
	}
	absent := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	}
	if got := evaluateBGPCapturePlacement(members["azure-router-b"], members, absent, true, ""); !got.Active || !got.Seize || got.ActiveMarkerPresent {
		t.Fatalf("placement without active alias marker = %+v, want standby seize", got)
	}
}

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

func placementPoolSpec() testMobilityPoolSpec {
	spec := plannedPoolSpec()
	spec.Members = []api.ResolvedMobilityPoolMember{
		spec.Members[0],
		{
			NodeRef: "azure-router-a",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "azure-provider",
				NICRef:      "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
				Target:      map[string]string{"region": "japaneast", "ipConfigName": "capture-a"},
			},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
		},
		{
			NodeRef: "azure-router-b",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "azure-provider",
				NICRef:      "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b",
				Target:      map[string]string{"region": "japaneast", "ipConfigName": "capture-b"},
			},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
		},
	}
	return spec
}

func centralizedOwnershipPoolSpec() testMobilityPoolSpec {
	spec := placementPoolSpec()
	spec.Members[1].Placement.Priority = 10
	spec.Members[2].Placement.Priority = 20
	return spec
}

func awsFailoverPoolSpec() testMobilityPoolSpec {
	spec := plannedPoolSpec()
	spec.Members = []api.ResolvedMobilityPoolMember{
		spec.Members[0],
		{
			NodeRef: "aws-router-a",
			Site:    "aws",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "aws-provider",
				NICRef:      "eni-a",
				Target:      map[string]string{"region": "ap-northeast-1"},
			},
			Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10},
		},
		{
			NodeRef: "aws-router-b",
			Site:    "aws",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:        "provider-secondary-ip",
				ProviderRef: "aws-provider",
				NICRef:      "eni-b",
				Target:      map[string]string{"region": "ap-northeast-1"},
			},
			Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 20},
		},
		{
			NodeRef: "azure-router",
			Site:    "azure",
			Role:    "cloud",
		},
		{
			NodeRef: "oci-router",
			Site:    "oci",
			Role:    "cloud",
		},
	}
	return spec
}

func planningRouter() *api.Router {
	return planningRouterForNode("azure-router", plannedPoolSpec())
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

func routerWithBGPRouter(router *api.Router) *api.Router {
	cp := *router
	cp.Spec.Resources = append(append([]api.Resource(nil), router.Spec.Resources...), api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
		Metadata: api.ObjectMeta{Name: "mobility-bgp"},
		Spec:     api.BGPRouterSpec{ASN: 64512, RouterID: "10.99.0.1"},
	})
	return &cp
}

func routerWithOCIProvider(router *api.Router) *api.Router {
	cp := *router
	cp.Spec.Resources = append(append([]api.Resource(nil), router.Spec.Resources...), api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
		Metadata: api.ObjectMeta{Name: "oci-provider"},
		Spec: api.CloudProviderProfileSpec{
			Provider:     "oci",
			Capabilities: []string{"vnic-secondary-ip", "ip-forwarding"},
			Auth:         api.ProviderAuth{Mode: "external-command", Command: "oci"},
		},
	})
	return &cp
}

func routerWithEventGroupListen(router *api.Router, address string) *api.Router {
	cp := *router
	cp.Spec.Resources = append([]api.Resource(nil), router.Spec.Resources...)
	for i := range cp.Spec.Resources {
		if cp.Spec.Resources[i].APIVersion != api.FederationAPIVersion || cp.Spec.Resources[i].Kind != "EventGroup" {
			continue
		}
		spec, err := cp.Spec.Resources[i].EventGroupSpec()
		if err != nil {
			continue
		}
		spec.Listen.Address = address
		cp.Spec.Resources[i].Spec = spec
	}
	return &cp
}

func mobilityPoolResource(t *testing.T, router *api.Router, name string) api.Resource {
	t.Helper()
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.MobilityAPIVersion && res.Kind == "MobilityPool" && res.Metadata.Name == name {
			return res
		}
	}
	t.Fatalf("MobilityPool/%s not found", name)
	return api.Resource{}
}

func saveBGPInstalledNextHops(t *testing.T, store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
}, nextHops map[string][]string) {
	t.Helper()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{"installedNextHops": nextHops}); err != nil {
		t.Fatalf("SaveObjectStatus(BGPRouter/mobility-bgp): %v", err)
	}
}

func bgpOwnerPrefixesForInstalledNextHops(nextHops map[string][]string) []bgpstate.Prefix {
	ownerByNextHop := map[string]string{
		"10.99.0.1": "onprem-router",
		"10.99.0.2": "aws-router-a",
		"10.99.0.3": "azure-router",
		"10.99.0.4": "oci-router",
		"10.99.0.5": "aws-router-b",
		"10.99.0.6": "azure-router-b",
	}
	var out []bgpstate.Prefix
	for prefix, hops := range nextHops {
		for _, hop := range hops {
			owner := strings.TrimSpace(ownerByNextHop[strings.TrimSpace(hop)])
			if owner == "" {
				continue
			}
			out = append(out, bgpstate.Prefix{
				Prefix:  prefix,
				NextHop: hop,
				Best:    true,
				Valid:   true,
				Communities: []string{
					bgpstate.MobilityCommunityOwner,
					bgpstate.MobilityNodeIdentityCommunity(owner),
				},
			})
			break
		}
	}
	return out
}

func bgpOwnerPrefix(prefix, nextHop, owner string) map[string]any {
	return map[string]any{
		"prefix":  prefix,
		"nextHop": nextHop,
		"best":    true,
		"valid":   true,
		"communities": []string{
			bgpstate.MobilityCommunityOwner,
			bgpstate.MobilityNodeIdentityCommunity(owner),
		},
	}
}

func saveBGPStatus(t *testing.T, store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
}, nextHops map[string][]string, prefixes []map[string]any, livenessMarkers map[string]string) {
	t.Helper()
	rawPrefixes := any(prefixes)
	if len(prefixes) == 0 {
		rawPrefixes = bgpOwnerPrefixesForInstalledNextHops(nextHops)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{"installedNextHops": nextHops, "prefixes": rawPrefixes, "livenessMarkers": livenessMarkers}); err != nil {
		t.Fatalf("SaveObjectStatus(BGPRouter/mobility-bgp): %v", err)
	}
}

func seedElapsedBGPSeizeHoldDown(t *testing.T, store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}, poolName, selfNode string, resolvedMembers []api.ResolvedMobilityPoolMember, livenessMarkers map[string]string, now time.Time) {
	t.Helper()
	members := plannerMembers(resolvedMembers)
	self, ok := lookupMemberByNodeRef(members, selfNode)
	if !ok {
		t.Fatalf("self member %q not found", selfNode)
	}
	placement := evaluateBGPCapturePlacement(self, members, livenessMarkers, true, "")
	key := bgpSeizeHoldDownKey(placement)
	if key == "" {
		t.Fatalf("placement = %#v, want seize hold-down key", placement)
	}
	status := map[string]any{}
	for k, v := range store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName) {
		status[k] = v
	}
	since := now.Add(-bgpSeizeLivenessMissingHold - time.Second)
	status["bgpSeizeHoldDownActive"] = true
	status["bgpSeizeHoldDownKey"] = key
	status["bgpSeizeHoldDownSince"] = since.Format(time.RFC3339Nano)
	status["bgpSeizeHoldDownUntil"] = since.Add(bgpSeizeLivenessMissingHold).Format(time.RFC3339Nano)
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", poolName, status); err != nil {
		t.Fatalf("SaveObjectStatus(MobilityPool/%s): %v", poolName, err)
	}
}

func latestPart(t *testing.T, store interface {
	GetDynamicConfigPartsBySource(string) ([]routerstate.DynamicConfigPartRecord, error)
}, source string) routerstate.DynamicConfigPartRecord {
	t.Helper()
	parts, err := store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		t.Fatalf("GetDynamicConfigPartsBySource(%s): %v", source, err)
	}
	if len(parts) == 0 {
		t.Fatalf("GetDynamicConfigPartsBySource(%s) returned no parts", source)
	}
	return parts[0]
}

func findActionPlan(plans []dynamicconfig.ActionPlan, action string) *dynamicconfig.ActionPlan {
	for i := range plans {
		if plans[i].Action == action {
			return &plans[i]
		}
	}
	return nil
}

func findActionPlanByAddress(plans []dynamicconfig.ActionPlan, action, address string) *dynamicconfig.ActionPlan {
	for i := range plans {
		if plans[i].Action == action && plans[i].Target["address"] == address {
			return &plans[i]
		}
	}
	return nil
}

func decodeResources(t *testing.T, raw string) []api.Resource {
	t.Helper()
	var resources []api.Resource
	if err := json.Unmarshal([]byte(raw), &resources); err != nil {
		t.Fatalf("decode resources: %v raw=%s", err, raw)
	}
	return resources
}

func decodeActionPlans(t *testing.T, raw string) []dynamicconfig.ActionPlan {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var plans []dynamicconfig.ActionPlan
	if err := json.Unmarshal([]byte(raw), &plans); err != nil {
		t.Fatalf("decode action plans: %v raw=%s", err, raw)
	}
	return plans
}

func importApprovedAction(t *testing.T, plan *dynamicconfig.ActionPlan, source string, store *routerstate.SQLiteStore, now time.Time) (int64, error) {
	t.Helper()
	targetJSON, err := json.Marshal(plan.Target)
	if err != nil {
		return 0, err
	}
	paramsJSON, err := json.Marshal(plan.Parameters)
	if err != nil {
		return 0, err
	}
	_, err = store.ImportAction(routerstate.ActionExecutionRecord{
		IdempotencyKey: plan.IdempotencyKey,
		Source:         source,
		Provider:       plan.Provider,
		ProviderRef:    plan.ProviderRef,
		Action:         plan.Action,
		TargetJSON:     string(targetJSON),
		ParametersJSON: string(paramsJSON),
		RiskLevel:      plan.RiskLevel,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	if err != nil {
		return 0, err
	}
	rows, err := store.ListActions(routerstate.ActionExecutionFilter{})
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if row.IdempotencyKey != plan.IdempotencyKey {
			continue
		}
		if err := store.ApproveAction(row.ID, "test", now); err != nil {
			return 0, err
		}
		return row.ID, nil
	}
	return 0, fmt.Errorf("imported action %q not found", plan.IdempotencyKey)
}

func seedSucceededBGPCaptureAction(t *testing.T, store *routerstate.SQLiteStore, providerRef, nicRef, holder, address, action string, epoch int64, at time.Time) {
	t.Helper()
	_ = epoch
	pathSig := "prefix=" + normalizeAddressString(address) + ";seeded=true"
	targetJSON, err := json.Marshal(map[string]string{"address": address, "nicRef": nicRef, "providerRef": providerRef})
	if err != nil {
		t.Fatalf("marshal target: %v", err)
	}
	paramsJSON, err := json.Marshal(map[string]string{
		bgpPathSigParam:     pathSig,
		captureParamHolder:  holder,
		"mobilityPathSigID": bgpPathSigHash(pathSig),
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	key := strings.Join([]string{"test", providerRef, nicRef, action, address, "pathsig", bgpPathSigHash(pathSig), fmt.Sprint(at.UnixNano())}, ":")
	if _, err := store.ImportAction(routerstate.ActionExecutionRecord{
		IdempotencyKey: key,
		Source:         "test",
		Provider:       strings.TrimSuffix(providerRef, "-provider"),
		ProviderRef:    providerRef,
		Action:         action,
		TargetJSON:     string(targetJSON),
		ParametersJSON: string(paramsJSON),
		Status:         routerstate.ActionPending,
	}); err != nil {
		t.Fatalf("ImportAction: %v", err)
	}
	rec, ok, err := store.GetActionByIdempotencyKey(key)
	if err != nil || !ok {
		t.Fatalf("GetActionByIdempotencyKey: ok=%v err=%v", ok, err)
	}
	if err := store.ApproveAction(rec.ID, "test", at.Add(-time.Second)); err != nil {
		t.Fatalf("ApproveAction: %v", err)
	}
	claimed, err := store.BeginActionExecution(rec.ID, at.Add(-500*time.Millisecond))
	if err != nil || !claimed {
		t.Fatalf("BeginActionExecution: claimed=%v err=%v", claimed, err)
	}
	if err := store.MarkActionResult(rec.ID, routerstate.ActionSucceeded, "ok", "", nil, at); err != nil {
		t.Fatalf("MarkActionResult: %v", err)
	}
}

func countKind(resources []api.Resource, kind string) int {
	count := 0
	for _, res := range resources {
		if strings.EqualFold(res.Kind, kind) {
			count++
		}
	}
	return count
}
