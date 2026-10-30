// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func mobilityRouteOwnerFixture(t *testing.T) (*IPv4RouteController, *nativeJournalStore, *nativeRouteFixture) {
	t.Helper()
	raw, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	store := &nativeJournalStore{SQLiteStore: raw}
	native := &nativeRouteFixture{}
	c := &IPv4RouteController{Router: &api.Router{}, Store: store, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil }, NativeScope: ordinaryFixtureScope, Command: native.command, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{Routes: []dynamicconfig.MobilityIPv4RouteIntent{{ID: "owned", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory, Destination: "10.88.60.10/32", Device: "lan0"}}}}
	c.MobilitySource = store
	storeMobilityOwnerPlan(t, c, store)
	return c, store, native
}

func storeMobilityOwnerPlan(t *testing.T, c *IPv4RouteController, store *nativeJournalStore) {
	t.Helper()
	now := time.Now().UTC()
	part := dynamicconfig.NewPart("native-owner", "MobilityPool/cloudedge/node/onprem", nil, 1, now.Add(-time.Second), now.Add(5*time.Minute))
	part.Spec.Digest = "native-fixture"
	part.Spec.MobilityDataplane = c.MobilityDataplane
	part.Spec.MobilityDataplane.PoolPrefix = "10.88.60.0/24"
	record, err := codec.Encode(part)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatal(err)
	}
}

func TestMobilityNativeRouteReacquisitionAckFailureRetainsPending(t *testing.T) {
	c, store, native := mobilityRouteOwnerFixture(t)
	if err := c.reconcileMobilityRoutes(t.Context()); err != nil {
		t.Fatal(err)
	}
	native.route = nil
	failure := errors.New("acquisition ACK failed")
	store.failure = failure
	c.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := native.command(ctx, name, args...)
		if name == "ip" && len(args) > 1 && args[0] == "route" && args[1] == "add" || name == "route" && len(args) > 1 && args[1] == "add" {
			store.failField = "appliedMobilityRoutes"
		}
		return out, err
	}
	if err := c.reconcileMobilityRoutes(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("failure lost: %v", err)
	}
	pending, err := pendingMobilityRoutes(nativeStatus(t, store.SQLiteStore))
	if err != nil || len(pending) != 1 || native.route == nil {
		t.Fatalf("unknown repeated add lost pending: %+v %v", pending, err)
	}
	store.failField = ""
	writes := native.writes
	if err := c.reconcileMobilityRoutes(t.Context()); err == nil || !strings.Contains(err.Error(), "without acknowledged ownership") {
		t.Fatalf("old receipt authorized uncertain effect: %v", err)
	}
	if native.writes != writes {
		t.Fatal("unknown effect was replayed or withdrawn")
	}
}

func TestMobilityNativeRouteRechecksIndexAndIntentAfterPendingPersistence(t *testing.T) {
	for _, mode := range []string{"index", "intent", "foreign", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			c, store, native := mobilityRouteOwnerFixture(t)
			index := 17
			c.DeviceLookup = func(context.Context, string) (int, bool, error) { return index, true, nil }
			fired := false
			store.afterMerge = func(updates map[string]any) {
				rows, ok := updates["pendingMobilityRoutes"].([]mobilityAppliedIPv4Route)
				if !ok || len(rows) == 0 || fired {
					return
				}
				fired = true
				switch mode {
				case "index":
					index = 18
				case "intent":
					c.MobilityDataplane.Routes = nil
					storeMobilityOwnerPlan(t, c, store)
				case "expiry":
					records, err := store.ListDynamicConfigParts()
					if err != nil || len(records) != 1 {
						t.Fatalf("read actual source: %+v %v", records, err)
					}
					records[0].ExpiresAt = time.Now().UTC()
					if err := store.UpsertDynamicConfigPart(records[0]); err != nil {
						t.Fatal(err)
					}
				case "foreign":
					native.route = &ordinaryIPv4RouteObservation{Type: "unicast", Destination: "10.88.60.10/32", Device: "lan0", Protocol: "boot"}
				}
			}
			if err := c.reconcileMobilityRoutes(t.Context()); err == nil || !fired {
				t.Fatalf("changed premise accepted: %v fired=%t", err, fired)
			}
			if native.writes != 0 {
				t.Fatalf("stale add issued %d writes", native.writes)
			}
			if mode == "foreign" && native.route == nil {
				t.Fatal("foreign route was removed")
			}
		})
	}
}

func TestMobilityNativeRouteIgnoredAddRetainsPendingUntilAbsence(t *testing.T) {
	c, store, native := mobilityRouteOwnerFixture(t)
	native.ignoreAdd = true
	if err := c.reconcileMobilityRoutes(t.Context()); err == nil {
		t.Fatal("ignored add reported success")
	}
	rows, err := pendingMobilityRoutes(nativeStatus(t, store.SQLiteStore))
	if err != nil || len(rows) != 1 {
		t.Fatalf("unconfirmed add lost fence: %+v %v", rows, err)
	}
	native.ignoreAdd = false
	if err := c.reconcileMobilityRoutes(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err = pendingMobilityRoutes(nativeStatus(t, store.SQLiteStore))
	if err != nil || len(rows) != 0 || native.route == nil {
		t.Fatalf("confirmed absence did not permit fresh exclusive add: %+v %v", rows, err)
	}
}

func TestMobilityNativeRouteDoesNotRetirePendingFromAnotherKernelWorld(t *testing.T) {
	c, store, native := mobilityRouteOwnerFixture(t)
	store.failField = "appliedMobilityRoutes"
	store.failure = errors.New("ACK failed")
	if err := c.reconcileMobilityRoutes(t.Context()); err == nil {
		t.Fatal("fixture did not reach uncertain write")
	}
	store.failField = ""
	native.route = nil
	c.NativeScope = func(context.Context) (string, error) { return "different-kernel", nil }
	writes := native.writes
	if err := c.reconcileMobilityRoutes(t.Context()); err == nil {
		t.Fatal("new world claimed old pending absence")
	}
	rows, err := pendingMobilityRoutes(nativeStatus(t, store.SQLiteStore))
	if err != nil || len(rows) != 1 || native.writes != writes {
		t.Fatalf("old world receipt lost or replayed: %+v %v", rows, err)
	}
}

func TestMobilityNativeRouteWithdrawsKnownEffectWhenIntentChangesDuringAcknowledgement(t *testing.T) {
	c, store, native := mobilityRouteOwnerFixture(t)
	changed := false
	store.afterMerge = func(updates map[string]any) {
		rows, ok := updates["appliedMobilityRoutes"].([]mobilityAppliedIPv4Route)
		if ok && len(rows) > 0 && !changed {
			changed = true
			c.MobilityDataplane.Routes = nil
			storeMobilityOwnerPlan(t, c, store)
		}
	}
	if err := c.reconcileMobilityRoutes(t.Context()); err == nil || !changed {
		t.Fatalf("changed authority reported success: %v", err)
	}
	rows, err := c.appliedMobilityRoutes()
	if err != nil || len(rows) != 0 || native.route != nil {
		t.Fatalf("known stale effect retained: %+v %v", rows, err)
	}
}

func TestMobilityNativeRouteRestoredIntentCancelsPendingWithdrawal(t *testing.T) {
	c, store, native := mobilityRouteOwnerFixture(t)
	wanted := c.MobilityDataplane.Routes[0]
	if err := c.reconcileMobilityRoutes(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.MobilityDataplane.Routes = nil
	storeMobilityOwnerPlan(t, c, store)
	restored := false
	store.afterMerge = func(updates map[string]any) {
		rows, ok := updates["pendingMobilityRoutes"].([]mobilityAppliedIPv4Route)
		if ok && len(rows) > 0 && rows[0].Operation == "delete" && !restored {
			restored = true
			c.MobilityDataplane.Routes = []dynamicconfig.MobilityIPv4RouteIntent{wanted}
			storeMobilityOwnerPlan(t, c, store)
		}
	}
	writes := native.writes
	if err := c.reconcileMobilityRoutes(t.Context()); err == nil || !restored {
		t.Fatalf("restored intent was removed: %v", err)
	}
	pending, err := pendingMobilityRoutes(nativeStatus(t, store.SQLiteStore))
	if err != nil || len(pending) != 0 || native.route == nil || native.writes != writes {
		t.Fatalf("restored effect changed: %+v %v writes=%d", pending, err, native.writes)
	}
}

func TestMobilityNativeRouteUnchangedPathRechecksSourceBeforeSuccess(t *testing.T) {
	c, store, native := mobilityRouteOwnerFixture(t)
	if err := c.reconcileMobilityRoutes(t.Context()); err != nil {
		t.Fatal(err)
	}
	changed := false
	c.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := native.command(ctx, name, args...)
		if !changed && (name == "ip" && len(args) > 0 && args[0] == "-j" || name == "route" && len(args) > 1 && args[1] == "get") {
			changed = true
			c.MobilityDataplane.Routes = nil
			storeMobilityOwnerPlan(t, c, store)
		}
		return out, err
	}
	if err := c.reconcileMobilityRoutes(t.Context()); err == nil || !changed {
		t.Fatalf("stale unchanged route reported success: %v", err)
	}
	routes, err := c.appliedMobilityRoutes()
	if err != nil || len(routes) != 0 || native.route != nil {
		t.Fatalf("revoked unchanged route survived: %+v %v", routes, err)
	}
}

func TestMobilityNativeReviewWithdrawalRechecksAfterFinalNativeRead(t *testing.T) {
	for _, mode := range []string{"index", "intent"} {
		t.Run(mode, func(t *testing.T) {
			c, store, native := mobilityRouteOwnerFixture(t)
			index := 17
			c.DeviceLookup = func(context.Context, string) (int, bool, error) { return index, true, nil }
			if err := c.reconcileMobilityRoutes(t.Context()); err != nil {
				t.Fatal(err)
			}
			wanted := c.MobilityDataplane.Routes
			c.MobilityDataplane.Routes = nil
			storeMobilityOwnerPlan(t, c, store)
			reads := 0
			c.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				out, err := native.command(ctx, name, args...)
				if name == "ip" && len(args) > 0 && args[0] == "-j" || name == "route" && len(args) > 1 && args[1] == "get" {
					reads++
					if reads == 2 {
						if mode == "index" {
							index = 18
						} else {
							c.MobilityDataplane.Routes = wanted
							storeMobilityOwnerPlan(t, c, store)
						}
					}
				}
				return out, err
			}
			writes := native.writes
			err := c.reconcileMobilityRoutes(t.Context())
			if native.writes != writes || native.route == nil {
				t.Fatalf("final native read invalidated %s but issued delete: writes %d->%d route=%+v err=%v reads=%d", mode, writes, native.writes, native.route, err, reads)
			}
		})
	}
}

func TestMobilityNativeReviewAcquisitionRechecksAfterFinalCollisionRead(t *testing.T) {
	for _, mode := range []string{"index", "intent"} {
		t.Run(mode, func(t *testing.T) {
			c, store, native := mobilityRouteOwnerFixture(t)
			index := 17
			c.DeviceLookup = func(context.Context, string) (int, bool, error) { return index, true, nil }
			reads := 0
			c.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				out, err := native.command(ctx, name, args...)
				if name == "ip" && len(args) > 0 && args[0] == "-j" || name == "route" && len(args) > 1 && args[1] == "get" {
					reads++
					if reads == 2 {
						if mode == "index" {
							index = 18
						} else {
							c.MobilityDataplane.Routes = nil
							storeMobilityOwnerPlan(t, c, store)
						}
					}
				}
				return out, err
			}
			err := c.reconcileMobilityRoutes(t.Context())
			if native.writes != 0 {
				t.Fatalf("final collision read invalidated %s but issued stale mutation(s): writes=%d route=%+v err=%v reads=%d", mode, native.writes, native.route, err, reads)
			}
		})
	}
}
