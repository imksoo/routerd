// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/providerinventory"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type poolSnapshotFaultStore struct {
	*routerstate.SQLiteStore
	failKind string
	failure  error
}

func (s *poolSnapshotFaultStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if kind == s.failKind {
		return nil, s.failure
	}
	return s.SQLiteStore.ReadObjectStatus(version, kind, name)
}

func TestUnknownPoolContinuationRetainsAppliedPlanAndPaths(t *testing.T) {
	cases := []struct {
		name, field string
		value       any
	}{
		{"null-stale-clocks", "observedSelfStaleCaptures", nil},
		{"bad-stale-time", "observedSelfStaleCaptures", map[string]string{"10.88.60.10/32": "broken"}},
		{"bad-stale-address", "observedSelfStaleCaptures", map[string]string{"broken": "2026-10-29T00:00:00Z"}},
		{"bad-active", "bgpSeizeHoldDownActive", "true"},
		{"bad-key", "bgpSeizeHoldDownKey", false},
		{"bad-since", "bgpSeizeHoldDownSince", "yesterday"},
		{"partial-hold", "bgpSeizeHoldDownActive", true},
		{"null-transitions", bgpCaptureTransitionCompletedKey, nil},
		{"null-markers", bgpCaptureTransitionCompletedKey, map[string]any{bgpCaptureConfirmedField: nil}},
		{"bad-transition", bgpCaptureTransitionCompletedKey, map[string]any{bgpCaptureConfirmedField: map[string]string{"broken": "generation"}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			store := testStore(t, now)
			paths := &fakeBGPPaths{}
			controller := Controller{Router: staticRouter("onprem-router", staticPoolSpec()), Store: store, BGPPaths: paths, Now: func() time.Time { return now }}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := latestPart(t, store, DynamicSource("cloudedge", "onprem-router"))
			upserts, deletes := len(paths.upserts), len(paths.deletes)
			if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{tt.field: tt.value}); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Second)
			if err := controller.Reconcile(context.Background()); err == nil {
				t.Fatal("unknown continuation admitted effects")
			}
			after := latestPart(t, store, DynamicSource("cloudedge", "onprem-router"))
			if before.Digest != after.Digest || !before.ObservedAt.Equal(after.ObservedAt) || len(paths.upserts) != upserts || len(paths.deletes) != deletes {
				t.Fatal("unknown continuation changed the owned plan or paths")
			}
		})
	}
}

func TestPoolAndCaptureGateReadFailuresDoNotBecomeAbsence(t *testing.T) {
	for _, kind := range []string{"MobilityPool", "VirtualAddress"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now().UTC()
			store := testStore(t, now)
			spec := staticPoolSpec()
			spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "eth1", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "vip"}}
			if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip", map[string]any{"role": "master"}); err != nil {
				t.Fatal(err)
			}
			faults := &poolSnapshotFaultStore{SQLiteStore: store, failure: errors.New("read unavailable")}
			paths := &fakeBGPPaths{}
			controller := Controller{Router: staticRouter("onprem-router", spec), Store: faults, BGPPaths: paths, Now: func() time.Time { return now }}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := latestPart(t, store, DynamicSource("cloudedge", "onprem-router"))
			upserts, deletes := len(paths.upserts), len(paths.deletes)
			faults.failKind = kind
			if err := controller.Reconcile(context.Background()); !errors.Is(err, faults.failure) {
				t.Fatalf("read failure=%v", err)
			}
			after := latestPart(t, store, DynamicSource("cloudedge", "onprem-router"))
			if before.Digest != after.Digest || !before.ObservedAt.Equal(after.ObservedAt) || len(paths.upserts) != upserts || len(paths.deletes) != deletes {
				t.Fatal("failed observation changed the plan or paths")
			}
		})
	}
}

func TestMalformedCaptureGateRoleIsUnknown(t *testing.T) {
	for _, role := range []any{nil, true, "unexpected", "unknown"} {
		store := testStore(t, time.Now())
		if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip", map[string]any{"role": role}); err != nil {
			t.Fatal(err)
		}
		if _, err := captureGateObservation(store, api.MobilityMemberCapture{ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "vip"}}); err == nil {
			t.Fatalf("role %#v became inactive observation", role)
		}
	}
}

func TestProviderRuntimeOwnershipRequiresCurrentMemberAndUsableFact(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	pool := ownershipPoolFixture("cloudedge", "onprem-router", staticPoolSpec())
	address := "10.88.60.42/32"
	for _, node := range []string{"azure-router", "removed-router"} {
		event := providerRuntimeAddressEvent(t, node, address, "azure", "azure-profile", providerinventory.PrivateIPRecord{Address: "10.88.60.42", NICRef: "home-nic"}, now, time.Minute)
		runtime, err := providerDiscoveryRuntimeRecords(pool, []routerstate.EventRecord{event}, now)
		if err != nil {
			t.Fatal(err)
		}
		decisions, err := resolveAddressOwnership(PoolRuntimeSnapshot{Pool: pool, Now: now, Ownership: OwnershipFacts{providerRuntime: runtime}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, decision := range decisions {
			if decision.Address == address {
				found = true
				if decision.HomeOwnerNode != node {
					t.Fatalf("decision=%+v", decision)
				}
			}
		}
		if found != (node == "azure-router") {
			t.Fatalf("member=%s became owner=%t", node, found)
		}
		for _, snapshot := range []string{
			"{broken", "null", "{}",
			`{"self":{},"addresses":null,"placement":{}}`,
			`{"self":{},"addresses":[],"placement":null}`,
			`{"self":{},"addresses":[{"address":"broken"}],"placement":{}}`,
		} {
			event.Payload["snapshot"] = snapshot
			_, err = providerDiscoveryRuntimeRecords(pool, []routerstate.EventRecord{event}, now)
			if (err != nil) != (node == "azure-router") {
				t.Fatalf("member=%s malformed current fact %s error=%v", node, snapshot, err)
			}
		}
	}
}
