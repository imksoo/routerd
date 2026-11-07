// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"testing"
)

func TestDHCPv4StatusWaitsForResourceTeardown(t *testing.T) {
	status := routerstate.ObjectStatus{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan", ManagedBy: "routerd", Status: map[string]any{"appliedAddress": "192.0.2.10/24", "ifname": "eth0"}}
	if plan := PlanStatusGC(nil, []routerstate.ObjectStatus{status}); len(plan.StatusDeletes) != 0 {
		t.Fatal("generic status GC discarded DHCPv4 effect ownership")
	}
	if plan := PlanResourceTeardownGC(nil, []routerstate.ObjectStatus{status}); len(plan.ResourceTeardowns) != 1 {
		t.Fatal("removed DHCPv4 owner was not scheduled for its resource cleanup")
	}
	if plan := PlanResourceTeardownGC(map[string]bool{OwnerKey(api.NetAPIVersion, "DHCPv4Client", "wan"): true}, []routerstate.ObjectStatus{status}); len(plan.ResourceTeardowns) != 0 {
		t.Fatal("scheduled desired DHCPv4 owner for cleanup")
	}
	status.ManagedBy = "external"
	if plan := PlanResourceTeardownGC(nil, []routerstate.ObjectStatus{status}); len(plan.ResourceTeardowns) != 0 {
		t.Fatal("scheduled external owner for cleanup")
	}
}
