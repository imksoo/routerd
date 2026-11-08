// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func testStore(t *testing.T, now time.Time) *routerstate.SQLiteStore {
	t.Helper()
	_ = now
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func recordEvent(t *testing.T, store *routerstate.SQLiteStore, rec routerstate.EventRecord) {
	t.Helper()
	if err := store.RecordFederationEvent(rec); err != nil {
		t.Fatalf("RecordFederationEvent: %v", err)
	}
}

// bootstrapOnPremDiscovery creates the same daemon intent and durable arm
// fact that a real discovery reconcile creates. Controller tests must not use
// MobilityPool status as a substitute for either input.
func bootstrapOnPremDiscovery(t *testing.T, store *routerstate.SQLiteStore, router *api.Router, armedAt time.Time) {
	t.Helper()
	discovery := DiscoveryController{Router: router, Store: store, Now: func() time.Time { return armedAt }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatalf("bootstrap onprem discovery: %v", err)
	}
}

// recordProviderDiscoveryRuntime feeds controller tests through the same
// durable typed fact that production consumes, rather than through a
// MobilityPool status projection.
func recordProviderDiscoveryRuntime(t *testing.T, store *routerstate.SQLiteStore, nodeRef string, fact providerDiscoveryRuntimeFact, observedAt time.Time) {
	t.Helper()
	recordEvent(t, store, providerDiscoveryRuntimeEventForTest(t, nodeRef, fact, observedAt, DefaultLeaseTTL))
}

// providerDiscoveryRuntimeEventForTest builds the sole provider ownership
// input accepted by production. Tests use it instead of reconstructing the
// removed per-address provider event protocol.
func providerDiscoveryRuntimeEventForTest(t *testing.T, nodeRef string, fact providerDiscoveryRuntimeFact, observedAt time.Time, ttl time.Duration) routerstate.EventRecord {
	t.Helper()
	placement := PlacementDecision{
		LivenessObserved:    fact.Placement.LivenessObserved,
		ActiveNode:          fact.Placement.ActiveNode,
		Active:              fact.Placement.Active,
		Seize:               fact.Placement.Seize,
		SelfMarkerPresent:   fact.Placement.SelfMarkerPresent,
		ActiveMarkerPresent: fact.Placement.ActiveMarkerPresent,
		SelfMarker:          fact.Placement.SelfMarker,
		ActiveMarker:        fact.Placement.ActiveMarker,
	}
	pool := NormalizedMobilityPool{
		Name:     "cloudedge",
		Spec:     api.MobilityPoolSpec{GroupRef: "cloudedge"},
		SelfNode: nodeRef,
	}
	event, err := providerDiscoveryRuntimeEvent(pool, fact.Self, fact.Addresses, placement, observedAt, ttl)
	if err != nil {
		t.Fatalf("provider discovery runtime event: %v", err)
	}
	return event
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func staticRouter(nodeName string, spec testMobilityPoolSpec) *api.Router {
	poolSpec, nodeSet := localizeMobilityPoolSpecForNode(spec, nodeName)
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
				TypeMeta: nodeSet.TypeMeta,
				Metadata: nodeSet.Metadata,
				Spec:     nodeSet.Spec,
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     poolSpec,
			},
		}},
	}
}

type mergeTrackingStore struct {
	*routerstate.SQLiteStore
	objectStatusCalls int
	mergeCalls        int
}

func (s *mergeTrackingStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	s.objectStatusCalls++
	return s.SQLiteStore.ObjectStatus(apiVersion, kind, name)
}

func (s *mergeTrackingStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	s.mergeCalls++
	return s.SQLiteStore.MergeObjectStatus(apiVersion, kind, name, updates)
}
