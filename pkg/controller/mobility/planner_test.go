// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestMain(m *testing.M) {
	// The startup fence is anchored on package-load wall-clock time, which does not
	// match the fixed clocks reconcile tests inject. Anchor it far in the past so the
	// fence is inert by default and unrelated tests exercise steady-state placement;
	// the fence's own tests set placementSettleStart/Window locally.
	placementSettleStart = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Exit(m.Run())
}

func TestPlacementDecision(t *testing.T) {
	base := placementPoolSpec()
	members := plannerMembers(base.Members)
	if got := evaluatePlacement(members["azure-router-a"], members); !got.Active || got.ActiveNode != "azure-router-a" {
		t.Fatalf("router-a placement = %+v, want active", got)
	}
	if got := evaluatePlacement(members["azure-router-b"], members); got.Active || got.ActiveNode != "azure-router-a" {
		t.Fatalf("router-b placement = %+v, want standby", got)
	}
	base.Members[1].Maintenance.Drain = true
	members = plannerMembers(base.Members)
	if got := evaluatePlacement(members["azure-router-b"], members); !got.Active || got.ActiveNode != "azure-router-b" {
		t.Fatalf("router-b after drain = %+v, want active", got)
	}
	base.Members[2].Maintenance.Drain = true
	members = plannerMembers(base.Members)
	if got := evaluatePlacement(members["azure-router-b"], members); got.Active || got.ActiveNode != "" {
		t.Fatalf("all drained placement = %+v, want fail-closed", got)
	}
	ungrouped := plannerMembers(plannedPoolSpec().Members)
	if got := evaluatePlacement(ungrouped["azure-router"], ungrouped); !got.Active || got.ActiveNode != "azure-router" {
		t.Fatalf("ungrouped placement = %+v, want active", got)
	}
}

func TestPlacementAutoPriority(t *testing.T) {
	spec := placementPoolSpec()
	spec.Members[1].Placement.Priority = 0
	spec.Members[2].Placement.Priority = 0
	members := plannerMembers(spec.Members)
	if got := members["azure-router-a"].PlacementPriority; got != 10 {
		t.Fatalf("azure-router-a auto priority = %d, want 10", got)
	}
	if got := members["azure-router-b"].PlacementPriority; got != 20 {
		t.Fatalf("azure-router-b auto priority = %d, want 20", got)
	}
	if got := evaluatePlacement(members["azure-router-a"], members); !got.Active || got.ActiveNode != "azure-router-a" {
		t.Fatalf("auto priority placement = %+v, want router-a active", got)
	}

	spec.Members[1].Placement.Priority = 20
	spec.Members[2].Placement.Priority = 0
	members = plannerMembers(spec.Members)
	if got := members["azure-router-a"].PlacementPriority; got != 20 {
		t.Fatalf("explicit azure-router-a priority = %d, want 20", got)
	}
	if got := members["azure-router-b"].PlacementPriority; got != 10 {
		t.Fatalf("azure-router-b auto priority = %d, want first free 10", got)
	}
	if got := evaluatePlacement(members["azure-router-b"], members); !got.Active || got.ActiveNode != "azure-router-b" {
		t.Fatalf("mixed priority placement = %+v, want explicit priority respected and router-b active", got)
	}
}

func equalPriorityPlacementMembers() map[string]memberPlanInfo {
	return map[string]memberPlanInfo{
		"aws-router-a": {
			NodeRef:           "aws-router-a",
			Capture:           api.AddressCapture{Type: "provider-secondary-ip", NICRef: "eni-a"},
			PlacementGroup:    "aws-edge",
			PlacementPriority: 10,
		},
		"aws-router-b": {
			NodeRef:           "aws-router-b",
			Capture:           api.AddressCapture{Type: "provider-secondary-ip", NICRef: "eni-b"},
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
	if !placementSettleDefersActive(true, "", 10*time.Second, settle) {
		t.Fatalf("returning node inside settle should defer")
	}
	// Once the incumbent peer is observed, the tie-break already defers; the fence
	// does not need to (and must not block legitimate post-settle behaviour).
	if placementSettleDefersActive(true, "aws-router-b", 10*time.Second, settle) {
		t.Fatalf("observed incumbent should not be fenced")
	}
	// After the settle window, normal placement applies (cold-start winner claims).
	if placementSettleDefersActive(true, "", settle+time.Second, settle) {
		t.Fatalf("after settle window should not defer")
	}
	// A standby (not asserting active) is never fenced.
	if placementSettleDefersActive(false, "", 1*time.Second, settle) {
		t.Fatalf("standby should not be fenced")
	}
}

func TestPlacementStartupFenceUsesReadiness(t *testing.T) {
	settle := 120 * time.Second
	notReady := placementStartupReadiness{Known: true, BGPObserved: false, ProviderRequired: true, ProviderObserved: false}
	if !placementStartupFenceDefersActive(true, "", settle+time.Second, settle, notReady) {
		t.Fatalf("not-ready startup should remain fenced after wall-clock settle")
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
	saveStart, saveWindow := placementSettleStart, placementSettleWindow
	defer func() { placementSettleStart, placementSettleWindow = saveStart, saveWindow }()
	now := saveStart.Add(30 * time.Second)
	placementSettleStart = now.Add(-30 * time.Second)
	placementSettleWindow = 120 * time.Second

	active := PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: "aws-router-a"}
	got := fencePlacementForStartup(active, "", now)
	if got.Active || got.Seize {
		t.Fatalf("fenced placement = %+v, want standby", got)
	}
	// With an observed incumbent the decision is left untouched.
	withIncumbent := fencePlacementForStartup(PlacementDecision{Group: "aws-edge", Active: true}, "aws-router-b", now)
	if !withIncumbent.Active {
		t.Fatalf("incumbent-observed placement must not be fenced: %+v", withIncumbent)
	}
}

func TestFencePlacementForStartupWithReadiness(t *testing.T) {
	saveStart, saveWindow := placementSettleStart, placementSettleWindow
	defer func() { placementSettleStart, placementSettleWindow = saveStart, saveWindow }()
	now := saveStart.Add(300 * time.Second)
	placementSettleStart = now.Add(-300 * time.Second)
	placementSettleWindow = 120 * time.Second

	active := PlacementDecision{Group: "aws-edge", Active: true, ActiveNode: "aws-router-a"}
	notReady := placementStartupReadiness{Known: true, BGPObserved: true, ProviderRequired: true, ProviderObserved: false}
	got := fencePlacementForStartupWithReadiness(active, "", now, notReady)
	if got.Active || !strings.Contains(got.Reason, "startup readiness") {
		t.Fatalf("not-ready fenced placement = %+v, want readiness standby", got)
	}
	ready := placementStartupReadiness{Known: true, BGPObserved: true, ProviderRequired: true, ProviderObserved: true}
	got = fencePlacementForStartupWithReadiness(active, "", now.Add(-290*time.Second), ready)
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
	saveStart, saveWindow := placementSettleStart, placementSettleWindow
	defer func() { placementSettleStart, placementSettleWindow = saveStart, saveWindow }()
	placementSettleWindow = 120 * time.Second
	now := saveStart.Add(1000 * time.Second)

	// Past the settle window, a node that still holds its captures must stay active
	// even when the base decision (deterministic tie-break / peer observation) would
	// make it stand by — the live holder never yields just because a peer is seen.
	placementSettleStart = now.Add(-1000 * time.Second)
	standby := PlacementDecision{Group: "aws-edge", Active: false, ActiveNode: "aws-router-a"}
	if got := applyHolderRetention(standby, true, false, now); !got.Active {
		t.Fatalf("holder past settle = %+v, want retained active", got)
	}
	// A node that does not hold is not retained.
	if got := applyHolderRetention(standby, false, false, now); got.Active {
		t.Fatalf("non-holder = %+v, want standby", got)
	}
	// A strictly higher-priority peer is the active holder: the local holder must
	// yield (no retention) so the configured priority restore can complete.
	if got := applyHolderRetention(standby, true, true, now); got.Active {
		t.Fatalf("holder yielding to higher priority = %+v, want standby", got)
	}
	// Inside the settle window the selfHolds signal may be the returning node's stale
	// memory, so retention must not apply (the fence keeps it passive instead).
	placementSettleStart = now.Add(-30 * time.Second)
	if got := applyHolderRetention(standby, true, false, now); got.Active {
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

func TestPlannerMembersInheritOwnershipDiscoveryProviderRef(t *testing.T) {
	spec := discoveryPoolSpec()
	spec.Members[1].OwnershipDiscovery.ProviderRef = ""
	members := plannerMembers(spec.Members)
	if got := members["azure-router-a"].OwnershipDiscovery.ProviderRef; got != "azure-provider" {
		t.Fatalf("ownershipDiscovery providerRef = %q, want capture providerRef", got)
	}
}

func TestProviderActionPlansRouteTableStrategy(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "aws"}
	capture := api.AddressCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "aws-provider",
		ProviderMode:    "route-table",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "eni-router",
	}
	plans, err := providerActionPlans("cloudedge", profile, capture, map[string]string{
		"region":        "ap-northeast-1",
		"routeTableRef": "rtb-123",
	}, "10.88.60.10/32", map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	assign := findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want abstract assign action", plans)
	}
	if assign.Target["routeTableRef"] != "rtb-123" || assign.Target["nicRef"] != "eni-router" || assign.Target["captureStrategy"] != captureStrategyRouteTable {
		t.Fatalf("assign target = %#v, want route table target", assign.Target)
	}
	if assign.Undo == nil || assign.Undo.Action != actionUnassignSecondaryIP {
		t.Fatalf("assign undo = %#v, want abstract unassign", assign.Undo)
	}
	if assign.Parameters["allowReassignment"] != "true" {
		t.Fatalf("assign parameters = %#v, want allowReassignment", assign.Parameters)
	}
}

func TestProviderActionPlansRouteTableStrategyRequiresRouteTableRef(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "aws"}
	capture := api.AddressCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "aws-provider",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "eni-router",
	}
	_, err := providerActionPlans("cloudedge", profile, capture, map[string]string{
		"region": "ap-northeast-1",
	}, "10.88.60.10/32", map[string]bool{}, false)
	if err == nil || !strings.Contains(err.Error(), "capture.captureStrategy route-table requires capture.target.routeTableRef") {
		t.Fatalf("providerActionPlans error = %v, want missing routeTableRef", err)
	}
}

func TestProviderActionPlansAzureRouteTableStrategyRequiresNextHopIPAddress(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "azure"}
	capture := api.AddressCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "azure-provider",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
	}
	_, err := providerActionPlans("cloudedge", profile, capture, map[string]string{
		"routeTableRef": "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/routeTables/rt-cloudedge",
	}, "10.88.60.10/32", map[string]bool{}, false)
	if err == nil || !strings.Contains(err.Error(), "provider azure capture.captureStrategy route-table requires capture.target.nextHopIPAddress") {
		t.Fatalf("providerActionPlans error = %v, want missing nextHopIPAddress", err)
	}
}

func TestProviderActionPlansOCIRouteTableStrategy(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "oci"}
	capture := api.AddressCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "oci-provider",
		ProviderMode:    "route-table",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "ocid1.vnic.oc1..router",
	}
	plans, err := providerActionPlans("cloudedge", profile, capture, map[string]string{
		"routeTableRef":    "ocid1.routetable.oc1..rt1",
		"nextHopIPAddress": "10.88.60.1",
	}, "10.88.60.10/32", map[string]bool{}, true)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	assign := findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want abstract assign action", plans)
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
	capture := api.AddressCapture{
		Type:            "provider-secondary-ip",
		ProviderRef:     "oci-provider",
		CaptureStrategy: captureStrategyRouteTable,
		NICRef:          "ocid1.vnic.oc1..router",
	}
	_, err := providerActionPlans("cloudedge", profile, capture, map[string]string{
		"routeTableRef": "ocid1.routetable.oc1..rt1",
	}, "10.88.60.10/32", map[string]bool{}, false)
	if err == nil || !strings.Contains(err.Error(), "provider oci capture.captureStrategy route-table requires capture.target.nextHopIPAddress") {
		t.Fatalf("providerActionPlans error = %v, want missing nextHopIPAddress", err)
	}
}

func TestProviderActionTargetUsesCaptureTargetNICFallback(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "azure", SubscriptionID: "sub-1", ResourceGroup: "rg-router"}
	target := providerActionTarget("cloudedge", profile, api.AddressCapture{
		Type:        "provider-secondary-ip",
		ProviderRef: "azure-provider",
	}, map[string]string{
		"nicRef": "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
	}, "10.88.60.10/32")
	if target["nicRef"] == "" {
		t.Fatalf("target = %#v, want nicRef fallback from capture target", target)
	}
	if target["ipConfigName"] == "" {
		t.Fatalf("target = %#v, want provider fields derived with fallback nicRef", target)
	}
}

func TestProviderActionPlansFallsBackToCaptureTargetNICRef(t *testing.T) {
	profile := api.CloudProviderProfileSpec{Provider: "azure", SubscriptionID: "sub-1", ResourceGroup: "rg-router"}
	capture := api.AddressCapture{
		Type:        "provider-secondary-ip",
		ProviderRef: "azure-provider",
	}
	captureTarget := map[string]string{
		"nicRef":       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
		"region":       "japaneast",
		"ipConfigName": "capture-a",
	}
	plans, err := providerActionPlans("cloudedge", profile, capture, captureTarget, "10.88.60.10/32", map[string]bool{}, false)
	if err != nil {
		t.Fatalf("providerActionPlans: %v", err)
	}
	assign := findActionPlanByAddress(plans, actionAssignSecondaryIP, "10.88.60.10/32")
	if assign == nil {
		t.Fatalf("plans = %#v, want assign plan", plans)
	}
	if assign.Target["nicRef"] != captureTarget["nicRef"] {
		t.Fatalf("assign target = %#v, want nicRef from captureTarget", assign.Target)
	}

	unassign, err := providerUnassignActionPlan("cloudedge", profile, capture, captureTarget, "10.88.60.10/32", time.Time{})
	if err != nil {
		t.Fatalf("providerUnassignActionPlan: %v", err)
	}
	if unassign.Target["nicRef"] != captureTarget["nicRef"] {
		t.Fatalf("unassign target = %#v, want nicRef from captureTarget", unassign.Target)
	}
}

func TestBGPCapturePlacementSeizesWhenActiveMarkerAbsentWithCanonicalNodeIdentity(t *testing.T) {
	members := map[string]memberPlanInfo{
		"aws-router-a": {
			NodeRef:            "Node/aws-router-a",
			Capture:            api.AddressCapture{Type: "provider-secondary-ip"},
			PlacementGroup:     "aws-edge",
			PlacementPriority:  10,
			MaintenanceDrain:   false,
			OwnershipDiscovery: api.MobilityOwnershipDiscovery{},
		},
		"aws-router-b": {
			NodeRef:           "Node/aws-router-b",
			Capture:           api.AddressCapture{Type: "provider-secondary-ip"},
			PlacementGroup:    "aws-edge",
			PlacementPriority: 20,
		},
	}
	markers := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	}
	got := evaluateBGPCapturePlacement(members["aws-router-b"], members, markers, true, "")
	if !got.Active || !got.Seize || got.ActiveNode != "Node/aws-router-b" {
		t.Fatalf("placement = %+v, want canonical identity failover seize by aws-router-b", got)
	}
	if got.SelfCommunity != bgpstate.MobilityNodeIdentityCommunity("aws-router-b") || !got.SelfMarkerPresent {
		t.Fatalf("self liveness = %+v, want canonical aws-router-b marker present", got)
	}
	if got.ActiveCommunity != bgpstate.MobilityNodeIdentityCommunity("aws-router-a") || got.ActiveMarkerPresent {
		t.Fatalf("active liveness = %+v, want canonical aws-router-a marker absent", got)
	}
}

func TestBGPCapturePlacementUsesCanonicalAdvertisedMarkerForReverseNodeRefForms(t *testing.T) {
	members := map[string]memberPlanInfo{
		"aws-router-a": {
			NodeRef:           "aws-router-a",
			Capture:           api.AddressCapture{Type: "provider-secondary-ip"},
			PlacementGroup:    "aws-edge",
			PlacementPriority: 10,
		},
		"aws-router-b": {
			NodeRef:           "aws-router-b",
			Capture:           api.AddressCapture{Type: "provider-secondary-ip"},
			PlacementGroup:    "aws-edge",
			PlacementPriority: 20,
		},
	}
	self := members["aws-router-b"]
	self.NodeRef = "Node/aws-router-b"
	present := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-a"): "10.99.0.2/32",
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	}
	if got := evaluateBGPCapturePlacement(self, members, present, true, ""); got.Active || got.Seize || !got.ActiveMarkerPresent {
		t.Fatalf("placement with active marker = %+v, want standby defer", got)
	}
	absent := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): "10.99.0.5/32",
	}
	if got := evaluateBGPCapturePlacement(self, members, absent, true, ""); !got.Active || !got.Seize || got.ActiveNode != "Node/aws-router-b" {
		t.Fatalf("placement without active marker = %+v, want canonical reverse-form seize", got)
	}
}

func TestBGPCapturePlacementRecognizesEventGroupAliasMarkerForActiveMember(t *testing.T) {
	members := map[string]memberPlanInfo{
		"azure-router-a": {
			NodeRef:           "azure-router-a",
			Capture:           api.AddressCapture{Type: "provider-secondary-ip"},
			PlacementGroup:    "azure-edge",
			PlacementPriority: 10,
		},
		"azure-router-b": {
			NodeRef:           "azure-router-b",
			Capture:           api.AddressCapture{Type: "provider-secondary-ip"},
			PlacementGroup:    "azure-edge",
			PlacementPriority: 20,
		},
	}
	present := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router"):   "10.99.0.3/32",
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	}
	if got := evaluateBGPCapturePlacement(members["azure-router-b"], members, present, true, ""); got.Active || got.Seize || !got.ActiveMarkerPresent {
		t.Fatalf("placement with active alias marker = %+v, want standby defer", got)
	}
	absent := map[string]string{
		bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
	}
	if got := evaluateBGPCapturePlacement(members["azure-router-b"], members, absent, true, ""); !got.Active || !got.Seize || got.ActiveMarkerPresent {
		t.Fatalf("placement without active alias marker = %+v, want standby seize", got)
	}
}

func plannedPoolSpec() api.MobilityPoolSpec {
	return api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{
				NodeRef:  "onprem-router",
				Site:     "onprem",
				Role:     "onprem",
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"},
				Delivery: api.MobilityMemberDelivery{PeerRef: "azure", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic",
					Target:       map[string]string{"region": "japaneast"},
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
		},
	}
}

func placementPoolSpec() api.MobilityPoolSpec {
	spec := plannedPoolSpec()
	spec.Members = []api.MobilityPoolMember{
		spec.Members[0],
		{
			NodeRef: "azure-router-a",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-a",
				Target:       map[string]string{"region": "japaneast", "ipConfigName": "capture-a"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
		},
		{
			NodeRef: "azure-router-b",
			Site:    "azure",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "/subscriptions/sub-1/resourceGroups/rg-router/providers/Microsoft.Network/networkInterfaces/router-nic-b",
				Target:       map[string]string{"region": "japaneast", "ipConfigName": "capture-b"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
		},
	}
	return spec
}

func centralizedOwnershipPoolSpec() api.MobilityPoolSpec {
	spec := placementPoolSpec()
	spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized"}
	spec.Members[1].Placement.Priority = 10
	spec.Members[2].Placement.Priority = 20
	return spec
}

func awsFailoverPoolSpec() api.MobilityPoolSpec {
	spec := plannedPoolSpec()
	spec.Members = []api.MobilityPoolMember{
		spec.Members[0],
		{
			NodeRef: "aws-router-a",
			Site:    "aws",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "aws-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "eni-a",
				Target:       map[string]string{"region": "ap-northeast-1"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10},
		},
		{
			NodeRef: "aws-router-b",
			Site:    "aws",
			Role:    "cloud",
			Capture: api.MobilityMemberCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "aws-provider",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "eni-b",
				Target:       map[string]string{"region": "ap-northeast-1"},
			},
			Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 20},
		},
		{
			NodeRef:  "azure-router",
			Site:     "azure",
			Role:     "cloud",
			Capture:  api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "azure-nic"},
			Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
		},
		{
			NodeRef:  "oci-router",
			Site:     "oci",
			Role:     "cloud",
			Capture:  api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "oci-provider", ProviderMode: "vnic-secondary-ip", NICRef: "oci-vnic"},
			Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
		},
	}
	spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", AutoFailover: true}
	return spec
}

func planningRouter() *api.Router {
	return planningRouterForNode("azure-router", plannedPoolSpec())
}

func planningRouterForNode(nodeName string, spec api.MobilityPoolSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     api.EventGroupSpec{NodeName: nodeName},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     spec,
			},
			{
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
			{
				TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
				Metadata: api.ObjectMeta{Name: "aws-provider"},
				Spec: api.CloudProviderProfileSpec{
					Provider:     "aws",
					Capabilities: []string{"nic-secondary-ip", "ip-forwarding"},
					Auth:         api.ProviderAuth{Mode: "external-command", Command: "aws"},
				},
			},
		}},
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
}, poolName, selfNode string, spec api.MobilityPoolSpec, livenessMarkers map[string]string, now time.Time) {
	t.Helper()
	members := plannerMembers(spec.Members)
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
