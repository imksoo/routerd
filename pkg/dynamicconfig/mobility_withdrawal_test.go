// SPDX-License-Identifier: BSD-3-Clause
package dynamicconfig

import "testing"

func TestMobilityWithdrawalBindsNativePlan(t *testing.T) {
	plan := MobilityDataplanePlan{PoolPrefix: "10.88.60.0/24"}
	request := MobilityWithdrawalRequest{ID: "move-1", GroupRef: "g", PoolRef: "p", PoolPrefix: plan.PoolPrefix, Address: "10.88.60.10/32", FromNodeRef: "a", ToNodeRef: "b", PlanDigest: MobilityNativePlanDigest(plan)}
	plan.Withdrawals = []MobilityWithdrawalRequest{request}
	if plan.IsEmpty() {
		t.Fatal("empty effect request discarded")
	}
	if err := ValidateMobilityDataplanePlanScope(plan, "p"); err != nil {
		t.Fatal(err)
	}
	plan.Routes = []MobilityIPv4RouteIntent{{ID: "r", PoolRef: "p", Destination: "10.88.60.10/32", Device: "lan0"}}
	if err := ValidateMobilityDataplanePlanScope(plan, "p"); err == nil {
		t.Fatal("changed native plan reused old request digest")
	}
}
