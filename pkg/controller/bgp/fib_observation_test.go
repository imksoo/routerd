// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"testing"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type failedMobilityPlanStore struct {
	mapStore
	failure error
}

func (s failedMobilityPlanStore) ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error) {
	return nil, s.failure
}

func TestWatchPreservesFIBWhenMobilityObservationFails(t *testing.T) {
	failure := errors.New("Mobility plan read unavailable")
	fib := &fakeFIB{}
	c := &Controller{Router: bgpRouter(), Store: failedMobilityPlanStore{mapStore: mapStore{}, failure: failure}, Server: &fakeServer{applied: bgpdaemon.Normalize(bgpdaemon.AppliedConfig{Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1"}, Policy: bgpdaemon.PolicyIntent{RouterName: "core"}})}, FIB: fib, started: true}
	if err := c.observeAndSyncFromWatchLocked(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("watch lost unknown observation error: %v", err)
	}
	if len(fib.history) != 0 {
		t.Fatalf("unknown plan changed FIB: %+v", fib.history)
	}
}
