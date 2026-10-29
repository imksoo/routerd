// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDiscoveryReobservesAfterInventoryWait(t *testing.T) {
	initial := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	now := initial
	store := testStore(t, now)
	router := routerWithBGPRouter(discoveryRouter("azure-router-b", discoveryPoolSpec()))
	onlySelf := map[string]string{bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32"}
	saveBGPStatus(t, store, map[string][]string{}, nil, onlySelf)
	seedElapsedBGPSeizeHoldDown(t, store, "cloudedge", "azure-router-b", discoveryPoolSpec().Members, onlySelf, now)
	candidate := providerinventory.PrivateIPRecord{Address: "10.88.60.12", NICRef: "client", Tags: map[string]string{"cloudedge-mobility": "true"}}
	controller := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return now }, Runner: func(context.Context, api.PluginSpec, providerinventory.ObservePrivateIPsRequest) (providerinventory.ObservePrivateIPsResult, providerinventory.RunOutcome, error) {
		now = now.Add(time.Minute)
		saveBGPStatus(t, store, map[string][]string{}, nil, map[string]string{
			bgpstate.MobilityNodeIdentityCommunity("azure-router-a"): "10.99.0.2/32",
			bgpstate.MobilityNodeIdentityCommunity("azure-router-b"): "10.99.0.6/32",
		})
		return providerinventory.ObservePrivateIPsResult{Status: providerinventory.ObservePrivateIPsResultStatus{Status: providerinventory.ResultSucceeded, LocalIPs: []providerinventory.PrivateIPRecord{candidate}, ObservedCandidates: []providerinventory.PrivateIPRecord{candidate}}}, providerinventory.RunOutcome{}, nil
	}}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	fact := providerDiscoveryRuntimeFactFromEvents(t, events)
	if fact.Placement.Active || len(fact.Addresses) != 0 || !fact.Placement.ActiveMarkerPresent {
		t.Fatalf("published stale active authority after active peer returned: %+v", fact)
	}
	if len(events) != 1 || !events[0].ObservedAt.Equal(now) || !events[0].ExpiresAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("observation used pre-wait clock: %+v", events)
	}
}

func TestDiscoveryDoesNotPublishCanceledInventoryResult(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", discoveryPoolSpec()), Store: store, Now: func() time.Time { return now }, Runner: func(context.Context, api.PluginSpec, providerinventory.ObservePrivateIPsRequest) (providerinventory.ObservePrivateIPsResult, providerinventory.RunOutcome, error) {
		cancel()
		return providerinventory.ObservePrivateIPsResult{Status: providerinventory.ObservePrivateIPsResultStatus{Status: providerinventory.ResultSucceeded}}, providerinventory.RunOutcome{}, nil
	}}
	if err := controller.Reconcile(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan returned %v", err)
	}
	events, err := store.ListFederationEvents("cloudedge", true, now.Unix())
	if err != nil || len(events) != 0 {
		t.Fatalf("canceled result published: %+v err=%v", events, err)
	}
}

func TestDiscoveryReturnsInventoryFailure(t *testing.T) {
	now := time.Now()
	store := testStore(t, now)
	failure := errors.New("inventory unavailable")
	controller := DiscoveryController{Router: discoveryRouter("azure-router-a", discoveryPoolSpec()), Store: store, Now: func() time.Time { return now }, Runner: func(context.Context, api.PluginSpec, providerinventory.ObservePrivateIPsRequest) (providerinventory.ObservePrivateIPsResult, providerinventory.RunOutcome, error) {
		return providerinventory.ObservePrivateIPsResult{}, providerinventory.RunOutcome{}, failure
	}}
	if err := controller.Reconcile(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("failed scan returned success: %v", err)
	}
}

func TestMobilityFederationFactsBindChangesAndRefreshIdenticalFacts(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	pool := NormalizedMobilityPool{Name: "pool", SelfNode: "self", Spec: api.MobilityPoolSpec{GroupRef: "group"}, Prefix: netip.MustParsePrefix("10.0.0.0/24")}
	first := onPremARPRequestEvent(pool, "10.0.0.1/32", onPremARPRequest{RequesterMAC: "00:00:00:00:00:01"}, now)
	same := onPremARPRequestEvent(pool, "10.0.0.1/32", onPremARPRequest{RequesterMAC: "00:00:00:00:00:01"}, now.Add(time.Second))
	changed := onPremARPRequestEvent(pool, "10.0.0.1/32", onPremARPRequest{RequesterMAC: "00:00:00:00:00:02"}, now.Add(time.Second))
	if first.ID != same.ID || first.ID == changed.ID || first.DedupeKey != changed.DedupeKey {
		t.Fatal("request ID does not bind immutable fact independently of refresh")
	}
	for _, event := range []routerstate.EventRecord{first, same, changed} {
		if err := store.RecordFederationEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.ListFederationEvents("group", true, now.Unix())
	if err != nil || len(rows) != 2 {
		t.Fatalf("request identity history=%+v err=%v", rows, err)
	}
	discovery := DiscoveryController{Store: store}
	armed, err := discovery.ensureOnPremDiscoveryArmedFact(pool, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	rearmed, err := discovery.ensureOnPremDiscoveryArmedFact(pool, []routerstate.EventRecord{armed}, now.Add(DefaultLeaseTTL+time.Second))
	if err != nil {
		t.Fatalf("expired discovery cannot rearm: %v", err)
	}
	if armed.ID == rearmed.ID || armed.Payload["armedAt"] == rearmed.Payload["armedAt"] {
		t.Fatal("rearmed fact reused prior immutable identity")
	}
	shard := shardAssignmentEvent("policy", "group", "pool-a", "10.0.0.0/25", "source", "target", 0, now)
	moved := shardAssignmentEvent("policy", "group", "pool-b", "10.0.0.0/25", "source", "target", 1, now)
	if shard.ID == moved.ID {
		t.Fatal("changed shard fact reused identity")
	}
	for _, event := range []routerstate.EventRecord{shard, moved} {
		if err := store.RecordFederationEvent(event); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveShardScopeDoesNotTreatReadFailureAsUnrestrictedScope(t *testing.T) {
	store := testStore(t, time.Now())
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if prefixes, err := ResolveShardScope(store, "group", "pool", "self", time.Now()); err == nil || prefixes != nil {
		t.Fatalf("unknown shard scope=%v err=%v", prefixes, err)
	}
}
