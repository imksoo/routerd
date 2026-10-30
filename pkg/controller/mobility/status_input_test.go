// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type captureStatusFaultStore struct {
	*routerstate.SQLiteStore
	failure error
}

func (s *captureStatusFaultStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if kind == "DHCPv4Client" && name == "sender" && s.failure != nil {
		return nil, s.failure
	}
	return s.SQLiteStore.ReadObjectStatus(version, kind, name)
}

func TestMobilityStatusReadFailureRetainsObserverPlanAndAvoidsBGPChanges(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	source := api.StatusValueSourceSpec{Resource: "DHCPv4Client/sender", Field: "currentAddress", Optional: true}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "sender", map[string]any{"currentAddress": "10.88.60.1/24"}); err != nil {
		t.Fatal(err)
	}
	spec := staticPoolSpec()
	spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "eth1", SourceAddressFrom: source}
	spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "onprem-l2", Sources: []api.MobilityOwnershipDiscoverySource{{Type: OnPremSourceOnDemandARP, Interface: "eth1", SourceAddressFrom: source}}}
	router := staticRouter("onprem-router", spec)
	faults := &captureStatusFaultStore{SQLiteStore: store}
	discovery := DiscoveryController{Router: router, Store: faults, Now: func() time.Time { return now }}
	if err := discovery.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := latestPart(t, store, ARPObserverDynamicSource("cloudedge", "onprem-router"))
	faults.failure = errors.New("status read unavailable")
	now = now.Add(time.Second)
	if err := discovery.Reconcile(context.Background()); !errors.Is(err, faults.failure) {
		t.Fatalf("unknown source observation returned %v", err)
	}
	after := latestPart(t, store, ARPObserverDynamicSource("cloudedge", "onprem-router"))
	if before.Digest != after.Digest || !before.ObservedAt.Equal(after.ObservedAt) {
		t.Fatalf("read failure replaced observer plan: before=%+v after=%+v", before, after)
	}
	paths := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: faults, BGPPaths: paths, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); !errors.Is(err, faults.failure) {
		t.Fatalf("capture source read failure returned %v", err)
	}
	if len(paths.upserts) != 0 || len(paths.deletes) != 0 {
		t.Fatalf("unknown source mutated BGP: %+v", paths)
	}
}
