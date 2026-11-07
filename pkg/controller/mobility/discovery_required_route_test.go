// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/providerinventory"
)

func TestDiscoveryRequiredReadFailurePreservesPreviousCaptureFact(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	records := []providerinventory.PrivateIPRecord{{Address: "10.88.60.11", NICRef: "client", Tags: map[string]string{"cloudedge-mobility": "true"}}}
	runner := &fakeInventoryRunner{result: providerinventory.ObservePrivateIPsResult{Status: providerinventory.ObservePrivateIPsResultStatus{
		Status: providerinventory.ResultSucceeded,
		Self: &providerinventory.PrivateIPSelf{
			NICRef: "router", PrivateIPs: []string{"10.88.60.21"}, CapturedAddresses: []string{"10.88.60.12/32"},
		},
		LocalIPs: records, ObservedCandidates: records,
	}}}
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", discoveryPoolSpec()), Store: store, Runner: runner.run, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil || len(before) != 1 {
		t.Fatalf("initial facts=%+v err=%v", before, err)
	}
	now = now.Add(time.Minute)
	runner.result = providerinventory.ObservePrivateIPsResult{Status: providerinventory.ObservePrivateIPsResultStatus{
		Status: providerinventory.ResultFailed, Error: "required route inventory unavailable",
	}}
	if err := controller.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "required route inventory unavailable") {
		t.Fatalf("failed producer result became success: %v", err)
	}
	after, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil || len(after) != 1 || after[0].ID != before[0].ID || after[0].Payload["snapshot"] != before[0].Payload["snapshot"] || !after[0].ExpiresAt.Equal(before[0].ExpiresAt) {
		t.Fatalf("failed scan replaced/extended prior ownership evidence: before=%+v after=%+v err=%v", before, after, err)
	}
	fact := providerDiscoveryRuntimeFactFromEvents(t, after)
	if len(fact.Self.CapturedAddresses) != 1 || fact.Self.CapturedAddresses[0] != "10.88.60.12/32" {
		t.Fatalf("failed scan erased previous capture: %+v", fact.Self)
	}
}
