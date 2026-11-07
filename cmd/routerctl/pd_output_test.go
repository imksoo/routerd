// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"strings"
	"testing"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPDLeaseOutputUsesTypedObservationAndAssociationTimerEpoch(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	lease := routerstate.PDLease{CurrentPrefix: "2001:db8:1::/56", LastPrefix: "2001:db8:1::/56", LastReplyAt: now.Add(-time.Minute), LeaseUpdatedAt: now, T1Seconds: 30, T2Seconds: 60, PreferredSeconds: 90, ValidSeconds: 120}
	timing := pdLeaseTiming(lease, now)
	if timing["t1At"] != now.Add(30*time.Second).Format(time.RFC3339) || timing["expiresAt"] != now.Add(time.Minute).Format(time.RFC3339) {
		t.Fatalf("association and independent prefix epochs conflated: %+v", timing)
	}
	var output strings.Builder
	writeDescribeStatus(&output, showResource{PDLease: &lease, State: map[string]any{"currentPrefix": "2001:db8:99::/56"}})
	if !strings.Contains(output.String(), "2001:db8:1::/56") || strings.Contains(output.String(), "2001:db8:99::/56") {
		t.Fatalf("description used untyped shadow: %s", output.String())
	}
	lease.ValidSeconds = 0xffffffff
	if pdLeaseTiming(lease, now)["remaining"] != "infinite" || displayLeaseSeconds(lease.ValidSeconds) != "infinite" {
		t.Fatal("infinite lifetime displayed as finite deadline")
	}
}
