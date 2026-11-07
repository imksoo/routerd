// SPDX-License-Identifier: BSD-3-Clause

package federationguard

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/federation"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestNativeCaptureReceiptsProtectWithdrawnDesiredPlanAcrossRestart(t *testing.T) {
	neighbor := sam.AppliedProxyNeighbor{ID: "capture", PoolRef: "pool", PoolPrefix: "10.0.0.0/24", Address: "10.0.0.1/32", Interface: "lan0"}
	path := sam.AppliedForwardPath{ID: "capture", PoolRef: "pool", PoolPrefix: "10.0.0.0/24", Address: "10.0.0.1/32", Kind: "forward-path", Interface: "lan0", PeerInterface: "wg0"}
	fields := map[string]any{
		"appliedProxyNeighbors": []sam.AppliedProxyNeighbor{neighbor},
		"pendingProxyNeighbors": []sam.PendingProxyNeighbor{{AppliedProxyNeighbor: neighbor, ProxyEntryOwned: true}},
		"appliedForwardPaths":   []sam.AppliedForwardPath{path},
		"pendingForwardPaths":   []sam.AppliedForwardPath{path},
	}
	for field, value := range fields {
		t.Run(field, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "state.db")
			store, err := routerstate.OpenSQLite(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.MergeObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName, map[string]any{field: value}); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = routerstate.OpenSQLite(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			observed := federation.Event{Type: federation.ObservedIPv4EventType, Subject: "10.0.0.1/32"}
			err = RejectSelfCapturedObservedEvent(store, observed, time.Now())
			var captured SelfCapturedObservedEventError
			if !errors.As(err, &captured) || captured.Source != "MobilityPool/pool" {
				t.Fatalf("withdrawn desired plan bypassed native %s: %v", field, err)
			}
			if err := store.MergeObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName, map[string]any{field: []any{}}); err != nil {
				t.Fatal(err)
			}
			if err := RejectSelfCapturedObservedEvent(store, observed, time.Now()); err != nil {
				t.Fatalf("confirmed empty native receipt still blocked observation: %v", err)
			}
		})
	}
}

func TestNativeCaptureReceiptUnknownOrMalformedNeverAuthorizesObservation(t *testing.T) {
	for _, value := range []any{nil, map[string]any{}, []map[string]any{{"address": "10.0.0.1/32"}}, []map[string]any{{"id": "capture", "poolRef": "pool", "poolPrefix": "10.1.0.0/24", "address": "10.0.0.1/32", "interface": "lan0"}}} {
		store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.MergeObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName, map[string]any{"pendingProxyNeighbors": value}); err != nil {
			t.Fatal(err)
		}
		if err := RejectSelfCapturedObservedEvent(store, federation.Event{Type: federation.ObservedIPv4EventType, Subject: "10.0.0.1/32"}, time.Now()); err == nil {
			t.Fatalf("malformed native receipt accepted: %#v", value)
		}
		store.Close()
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err := RejectSelfCapturedObservedEvent(store, federation.Event{Type: federation.ObservedIPv4EventType, Subject: "10.0.0.1/32"}, time.Now()); err == nil {
		t.Fatal("unreadable native owner was treated as absent")
	}
}
