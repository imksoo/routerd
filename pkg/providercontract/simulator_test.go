// SPDX-License-Identifier: BSD-3-Clause

package providercontract

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func TestAssignSecondaryIPRequiresExplicitSeize(t *testing.T) {
	now := time.Date(2026, 6, 26, 6, 30, 0, 0, time.UTC)
	sim := New(func() time.Time { return now })
	address := "10.77.60.12/32"

	first := sim.Execute(dynamicconfig.ActionPlan{
		Action:         ActionAssignSecondaryIP,
		IdempotencyKey: "assign-a",
		Target: map[string]string{
			"address":   address,
			"targetRef": "eni-a",
		},
		Parameters: map[string]string{"assignmentGeneration": "gen-a"},
	})
	if first.Status != "Succeeded" {
		t.Fatalf("first assign = %#v, want success", first)
	}

	conflict := sim.Execute(dynamicconfig.ActionPlan{
		Action:         ActionAssignSecondaryIP,
		IdempotencyKey: "assign-b",
		Target: map[string]string{
			"address":   address,
			"targetRef": "eni-b",
		},
		Parameters: map[string]string{"assignmentGeneration": "gen-b"},
	})
	if conflict.Status != "Failed" || conflict.Reason != "AddressHeldByAnotherTarget" {
		t.Fatalf("conflict assign = %#v, want non-destructive holder failure", conflict)
	}
	if holder, ok := sim.Snapshot().AddressHolder(address); !ok || holder.TargetRef != "eni-a" {
		t.Fatalf("holder after non-seize conflict = %#v ok=%t, want eni-a", holder, ok)
	}

	seize := sim.Execute(dynamicconfig.ActionPlan{
		Action:         ActionAssignSecondaryIP,
		IdempotencyKey: "assign-b-seize",
		Target: map[string]string{
			"address":   address,
			"targetRef": "eni-b",
		},
		Parameters: map[string]string{
			"allowReassignment":    "true",
			"expectedHolderRef":    "eni-a",
			"assignmentGeneration": "gen-b",
		},
	})
	if seize.Status != "Succeeded" {
		t.Fatalf("explicit seize = %#v, want success", seize)
	}
	if holder, ok := sim.Snapshot().AddressHolder(address); !ok || holder.TargetRef != "eni-b" || holder.Generation != "gen-b" {
		t.Fatalf("holder after explicit seize = %#v ok=%t, want eni-b gen-b", holder, ok)
	}
}

func TestAssignSecondaryIPRejectsStaleExpectedHolder(t *testing.T) {
	sim := New(nil)
	address := "10.77.60.13/32"
	if res := sim.Execute(dynamicconfig.ActionPlan{
		Action: ActionAssignSecondaryIP,
		Target: map[string]string{
			"address":   address,
			"targetRef": "eni-current",
		},
	}); res.Status != "Succeeded" {
		t.Fatalf("seed assign = %#v, want success", res)
	}

	res := sim.Execute(dynamicconfig.ActionPlan{
		Action: ActionAssignSecondaryIP,
		Target: map[string]string{
			"address":   address,
			"targetRef": "eni-next",
		},
		Parameters: map[string]string{
			"allowReassignment": "true",
			"expectedHolderRef": "eni-stale",
		},
	})
	if res.Status != "Failed" || res.Reason != "ObservedHolderMismatch" {
		t.Fatalf("stale expected holder seize = %#v, want holder mismatch failure", res)
	}
	if holder, ok := sim.Snapshot().AddressHolder(address); !ok || holder.TargetRef != "eni-current" {
		t.Fatalf("holder after stale seize = %#v ok=%t, want eni-current", holder, ok)
	}
}

func TestProviderSnapshotDoesNotExposeGuestLocalInterfaceState(t *testing.T) {
	sim := New(nil)
	if res := sim.Execute(dynamicconfig.ActionPlan{
		Action: ActionAssignSecondaryIP,
		Target: map[string]string{
			"address":   "10.77.60.15/32",
			"targetRef": "pve-vm-nic-a",
		},
	}); res.Status != "Succeeded" {
		t.Fatalf("assign = %#v, want success", res)
	}
	snapshot := sim.Snapshot()
	holder, ok := snapshot.AddressHolder("10.77.60.15/32")
	if !ok {
		t.Fatalf("snapshot addresses = %#v, want provider assignment", snapshot.Addresses)
	}
	if holder.TargetRef != "pve-vm-nic-a" {
		t.Fatalf("holder = %#v, want provider target ref", holder)
	}
	if len(snapshot.AddressKeys()) != 1 || snapshot.AddressKeys()[0] != "10.77.60.15/32" {
		t.Fatalf("address keys = %#v, want only provider address keys", snapshot.AddressKeys())
	}
}

func TestRouteAndForwardingContract(t *testing.T) {
	sim := New(nil)
	route := sim.Execute(dynamicconfig.ActionPlan{
		Action: ActionAssignRouteTableRoute,
		Target: map[string]string{
			"routeTableRef": "rtb-a",
			"prefix":        "10.77.60.0/24",
			"nextHopRef":    "eni-router-a",
		},
	})
	if route.Status != "Succeeded" {
		t.Fatalf("route assign = %#v, want success", route)
	}
	fwd := sim.Execute(dynamicconfig.ActionPlan{
		Action: ActionEnsureForwardingEnabled,
		Target: map[string]string{"targetRef": "eni-router-a"},
	})
	if fwd.Status != "Succeeded" {
		t.Fatalf("forwarding enable = %#v, want success", fwd)
	}
	snapshot := sim.Snapshot()
	if got, ok := snapshot.Route("rtb-a", "10.77.60.0/24"); !ok || got.NextHopRef != "eni-router-a" {
		t.Fatalf("route snapshot = %#v ok=%t, want eni-router-a", got, ok)
	}
	if !snapshot.ForwardingEnabled["eni-router-a"] {
		t.Fatalf("forwarding snapshot = %#v, want eni-router-a enabled", snapshot.ForwardingEnabled)
	}
}

func TestRouteTableObservationGateWaitsForObservedLocalNextHop(t *testing.T) {
	now := time.Date(2026, 7, 3, 11, 0, 0, 0, time.UTC)
	sim := New(func() time.Time { return now })
	sim.SetRouteTableObservationDelay(30 * time.Second)

	res := sim.Execute(routeTableAssignPlan("rtb-capture", "10.88.60.10/32", "nic-self", "gen-1"))
	if res.Status != "Succeeded" {
		t.Fatalf("assign route = %#v, want success", res)
	}
	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.10/32", "nic-self") {
		t.Fatalf("route-table capture advertised before provider observation")
	}

	now = now.Add(29 * time.Second)
	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.10/32", "nic-self") {
		t.Fatalf("route-table capture advertised before observation delay elapsed")
	}

	now = now.Add(time.Second)
	if !routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.10/32", "nic-self") {
		t.Fatalf("route-table capture did not advertise after provider observation")
	}
}

func TestRouteTableDelayedExecutionDoesNotExposeWrongRoute(t *testing.T) {
	now := time.Date(2026, 7, 3, 11, 5, 0, 0, time.UTC)
	sim := New(func() time.Time { return now })
	sim.SetActionDelay(10 * time.Second)
	sim.SetRouteTableObservationDelay(20 * time.Second)

	if res := sim.Execute(routeTableAssignPlan("rtb-capture", "10.88.60.11/32", "nic-self", "gen-1")); res.Status != "Succeeded" {
		t.Fatalf("assign route = %#v, want success", res)
	}
	if _, ok := sim.Snapshot().Route("rtb-capture", "10.88.60.11/32"); ok {
		t.Fatalf("route mutated before action execution delay elapsed")
	}
	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.11/32", "nic-self") {
		t.Fatalf("route-table capture advertised before delayed action executed")
	}

	now = now.Add(10 * time.Second)
	if route, ok := sim.Snapshot().Route("rtb-capture", "10.88.60.11/32"); !ok || route.NextHopRef != "nic-self" {
		t.Fatalf("route after action delay = %#v ok=%t, want nic-self", route, ok)
	}
	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.11/32", "nic-self") {
		t.Fatalf("route-table capture advertised before delayed observation")
	}

	now = now.Add(20 * time.Second)
	if !routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.11/32", "nic-self") {
		t.Fatalf("route-table capture did not advertise after action and observation delays")
	}
	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.11/32", "nic-other") {
		t.Fatalf("route-table capture advertised for wrong next hop")
	}
}

func TestRouteTableFailoverWaitsForObservedRewrite(t *testing.T) {
	now := time.Date(2026, 7, 3, 11, 10, 0, 0, time.UTC)
	sim := New(func() time.Time { return now })

	if res := sim.Execute(routeTableAssignPlan("rtb-capture", "10.88.60.12/32", "nic-old", "gen-old")); res.Status != "Succeeded" {
		t.Fatalf("seed old route = %#v, want success", res)
	}
	if !routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.12/32", "nic-old") {
		t.Fatalf("old holder route was not initially observable")
	}

	sim.SetRouteTableObservationDelay(15 * time.Second)
	res := sim.Execute(routeTableAssignPlan("rtb-capture", "10.88.60.12/32", "nic-new", "gen-new"))
	if res.Status != "Succeeded" {
		t.Fatalf("rewrite route = %#v, want success", res)
	}
	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.12/32", "nic-new") {
		t.Fatalf("new holder advertised before observed route-table rewrite")
	}
	if !routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.12/32", "nic-old") {
		t.Fatalf("old observed route disappeared before delayed rewrite observation")
	}

	now = now.Add(15 * time.Second)
	if !routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.12/32", "nic-new") {
		t.Fatalf("new holder did not advertise after route-table rewrite observation")
	}
	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.12/32", "nic-old") {
		t.Fatalf("old holder remained advertised after observed rewrite")
	}
}

func TestRouteTableUnassignObservationRemovesAdvertisableRoute(t *testing.T) {
	now := time.Date(2026, 7, 3, 11, 12, 0, 0, time.UTC)
	sim := New(func() time.Time { return now })

	if res := sim.Execute(routeTableAssignPlan("rtb-capture", "10.88.60.15/32", "nic-self", "gen-1")); res.Status != "Succeeded" {
		t.Fatalf("assign route = %#v, want success", res)
	}
	if !routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.15/32", "nic-self") {
		t.Fatalf("route-table capture was not initially observable")
	}

	sim.SetRouteTableObservationDelay(10 * time.Second)
	res := sim.Execute(dynamicconfig.ActionPlan{
		Action: ActionUnassignRouteTableRoute,
		Target: map[string]string{
			"routeTableRef": "rtb-capture",
			"prefix":        "10.88.60.15/32",
		},
	})
	if res.Status != "Succeeded" {
		t.Fatalf("unassign route = %#v, want success", res)
	}
	if _, ok := sim.Snapshot().Route("rtb-capture", "10.88.60.15/32"); ok {
		t.Fatalf("route remains in actual provider state after unassign")
	}
	if !routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.15/32", "nic-self") {
		t.Fatalf("observed route disappeared before delayed unassign observation")
	}

	now = now.Add(10 * time.Second)
	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.15/32", "nic-self") {
		t.Fatalf("route-table capture remained advertisable after unassign observation")
	}
	if route, ok := sim.ObserveRouteTableRoute("rtb-capture", "10.88.60.15/32"); ok {
		t.Fatalf("observed route after unassign = %#v, want absent", route)
	}
}

func TestRouteTableGateDoesNotDelaySecondaryIPCapture(t *testing.T) {
	now := time.Date(2026, 7, 3, 11, 15, 0, 0, time.UTC)
	sim := New(func() time.Time { return now })
	sim.SetRouteTableObservationDelay(time.Minute)

	if res := sim.Execute(routeTableAssignPlan("rtb-capture", "10.88.60.13/32", "nic-route", "gen-route")); res.Status != "Succeeded" {
		t.Fatalf("assign route = %#v, want success", res)
	}
	if res := sim.Execute(dynamicconfig.ActionPlan{
		Action: ActionAssignSecondaryIP,
		Target: map[string]string{
			"address":   "10.88.60.14/32",
			"targetRef": "nic-secondary",
		},
		Parameters: map[string]string{"assignmentGeneration": "gen-secondary"},
	}); res.Status != "Succeeded" {
		t.Fatalf("assign secondary = %#v, want success", res)
	}

	if routeTableAdvertisementReady(sim, "rtb-capture", "10.88.60.13/32", "nic-route") {
		t.Fatalf("route-table capture advertised before route-table observation")
	}
	if !secondaryIPCaptureReady(sim, "10.88.60.14/32", "nic-secondary") {
		t.Fatalf("secondary-ip capture was delayed by route-table observation gate")
	}
}

func routeTableAssignPlan(routeTableRef, prefix, nextHopRef, generation string) dynamicconfig.ActionPlan {
	return dynamicconfig.ActionPlan{
		Action: ActionAssignRouteTableRoute,
		Target: map[string]string{
			"routeTableRef": routeTableRef,
			"prefix":        prefix,
			"nextHopRef":    nextHopRef,
		},
		Parameters: map[string]string{
			"allowReassignment":    "true",
			"assignmentGeneration": generation,
		},
	}
}

func routeTableAdvertisementReady(sim *Simulator, routeTableRef, prefix, selfNextHopRef string) bool {
	route, ok := sim.ObserveRouteTableRoute(routeTableRef, prefix)
	return ok && route.NextHopRef == selfNextHopRef
}

func secondaryIPCaptureReady(sim *Simulator, address, selfTargetRef string) bool {
	holder, ok := sim.Snapshot().AddressHolder(address)
	return ok && holder.TargetRef == selfTargetRef
}
