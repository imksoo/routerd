// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
)

func TestMalformedBGPObservationDoesNotBecomeAbsenceOrWithdrawOwnedPlan(t *testing.T) {
	cases := []struct {
		name, field string
		value       any
	}{
		{"null-installed", "installedNextHops", nil},
		{"coerced-next-hop", "installedNextHops", map[string]any{"10.88.60.10/32": []any{true}}},
		{"invalid-installed-prefix", "installedNextHops", map[string][]string{"broken": {"10.99.0.1"}}},
		{"null-liveness", "livenessMarkers", nil},
		{"invalid-liveness-prefix", "livenessMarkers", map[string]string{bgpstate.MobilityNodeIdentityCommunity("onprem-router"): "broken"}},
		{"null-rib", "prefixes", nil},
		{"non-array-rib", "prefixes", map[string]any{}},
		{"missing-validity", "prefixes", []map[string]any{{"prefix": "10.88.60.10/32", "best": true}}},
		{"null-communities", "prefixes", []map[string]any{{"prefix": "10.88.60.10/32", "best": true, "valid": true, "communities": nil}}},
		{"truncated-rib", "prefixes", []bgpstate.Prefix{{Prefix: "truncated", SelectionReason: "prefix limit reached"}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			store := testStore(t, now)
			router := routerWithBGPRouter(staticRouter("onprem-router", staticPoolSpec()))
			saveBGPStatus(t, store, map[string][]string{}, nil, map[string]string{})
			paths := &fakeBGPPaths{}
			controller := Controller{Router: router, Store: store, BGPPaths: paths, Now: func() time.Time { return now }}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			before := latestPart(t, store, DynamicSource("cloudedge", "onprem-router"))
			upserts, deletes := len(paths.upserts), len(paths.deletes)
			if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPRouter", "mobility-bgp", map[string]any{tt.field: tt.value}); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Second)
			if err := controller.Reconcile(context.Background()); err == nil {
				t.Fatal("unknown BGP status was interpreted as an authoritative observation")
			}
			if len(paths.upserts) != upserts || len(paths.deletes) != deletes {
				t.Fatal("malformed BGP observation mutated owned paths")
			}
			after := latestPart(t, store, DynamicSource("cloudedge", "onprem-router"))
			if before.Digest != after.Digest || !before.ObservedAt.Equal(after.ObservedAt) {
				t.Fatal("malformed BGP observation replaced desired capture plan")
			}
		})
	}
}

func TestExplicitEmptyBGPObservationIsKnownEmpty(t *testing.T) {
	now := time.Now()
	store := testStore(t, now)
	router := routerWithBGPRouter(staticRouter("onprem-router", staticPoolSpec()))
	saveBGPStatus(t, store, map[string][]string{}, nil, map[string]string{})
	snapshot, err := collectBGPSnapshot(router, store, NormalizedMobilityPool{Prefix: netip.MustParsePrefix("10.88.60.0/24")})
	if err != nil || !snapshot.InstalledObserved || !snapshot.CaptureRIBObserved || !snapshot.LivenessObserved || len(snapshot.InstalledNextHops) != 0 || len(snapshot.CaptureNextHops) != 0 || len(snapshot.LivenessMarkers) != 0 {
		t.Fatalf("explicit empty observation=%+v err=%v", snapshot, err)
	}
}
