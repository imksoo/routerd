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
