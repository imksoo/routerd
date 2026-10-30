// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type nativeJournalStore struct {
	*routerstate.SQLiteStore
	failField string
	failure   error
}

func (s *nativeJournalStore) MergeObjectStatus(version, kind, name string, updates map[string]any) error {
	if _, present := updates[s.failField]; s.failField != "" && present {
		return s.failure
	}
	return s.SQLiteStore.MergeObjectStatus(version, kind, name, updates)
}

// Command outputs model the kernel boundary used by the actual owner. They
// neither invoke shell commands nor change the host's network.
type nativeRouteFixture struct {
	route     *ordinaryIPv4RouteObservation
	deleteErr error
	writes    int
}

func (n *nativeRouteFixture) command(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "ip" {
		return nil, errors.New("unexpected native command")
	}
	if len(args) > 4 && args[0] == "-j" {
		if n.route == nil {
			return []byte("[]"), nil
		}
		return json.Marshal([]ordinaryIPv4RouteObservation{*n.route})
	}
	if len(args) > 2 && args[0] == "route" && args[1] == "add" {
		route := ordinaryIPv4RouteObservation{Type: "unicast", Destination: args[2], Protocol: "boot"}
		for i := 3; i+1 < len(args); i += 2 {
			switch args[i] {
			case "dev":
				route.Device = args[i+1]
			case "src":
				route.PreferredSource = args[i+1]
			}
		}
		n.route = &route
		n.writes++
		return nil, nil
	}
	if len(args) > 2 && args[0] == "route" && args[1] == "del" {
		if n.deleteErr != nil {
			return nil, n.deleteErr
		}
		n.route = nil
		n.writes++
		return nil, nil
	}
	return nil, errors.New("unexpected native command: " + name + " " + strings.Join(args, " "))
}

func nativeWithdrawalFixture() dynamicconfig.MobilityDataplanePlan {
	plan := dynamicconfig.MobilityDataplanePlan{PoolPrefix: "10.88.60.0/24"}
	plan.Withdrawals = []dynamicconfig.MobilityWithdrawalRequest{{ID: "move-1", GroupRef: "cloudedge", PoolRef: "cloudedge", PoolPrefix: plan.PoolPrefix, Address: "10.88.60.10/32", FromNodeRef: "onprem", ToNodeRef: "cloud", PlanDigest: dynamicconfig.MobilityNativePlanDigest(plan)}}
	return plan
}

func nativeStatus(t *testing.T, store *routerstate.SQLiteStore) map[string]any {
	t.Helper()
	status, err := store.ReadObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName)
	if err != nil {
		t.Fatal(err)
	}
	return status
}
func assertNativeAck(t *testing.T, status map[string]any, field string, want []dynamicconfig.MobilityWithdrawalRequest) {
	t.Helper()
	var got []dynamicconfig.MobilityWithdrawalRequest
	if value, present := status[field]; present && !decodeAppliedDataplaneStatus(value, &got) {
		t.Fatalf("invalid %s: %#v", field, value)
	}
	if len(got) != len(want) {
		t.Fatalf("%s = %+v, want %+v", field, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s stale request: %+v", field, got)
		}
	}
}

func TestMobilityNativeRoutePendingSurvivesFailureAndRestart(t *testing.T) {
	if platform.CurrentOS() != platform.OSLinux {
		t.Skip("Linux command fixture")
	}
	path := filepath.Join(t.TempDir(), "native.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("final ledger save rejected")
	faults := &nativeJournalStore{SQLiteStore: store, failField: "appliedMobilityRoutes", failure: failure}
	native := &nativeRouteFixture{}
	controller := IPv4RouteController{Router: &api.Router{}, Store: faults, DevicePresent: func(context.Context, string) bool { return true }, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil }, NativeScope: ordinaryFixtureScope, Command: native.command, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{Routes: []dynamicconfig.MobilityIPv4RouteIntent{{ID: "owned", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory, Destination: "10.88.60.10/32", Device: "lan0"}}}}
	if err := controller.reconcileMobilityRoutes(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("final save error: %v", err)
	}
	if native.route == nil {
		t.Fatal("fixture did not reach kernel-success/status-failure window")
	}
	pending, err := pendingMobilityRoutes(nativeStatus(t, store))
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending ownership not durable: %+v %v", pending, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller.Store = store
	controller.MobilityDataplane = nativeWithdrawalFixture()
	writes := native.writes
	if err := controller.reconcileMobilityRoutes(context.Background()); err == nil || !strings.Contains(err.Error(), "without acknowledged ownership") {
		t.Fatalf("pending presence was treated as ownership: %v", err)
	}
	if native.writes != writes {
		t.Fatal("restart mutated an uncertain native route")
	}
	assertNativeAck(t, nativeStatus(t, store), dynamicconfig.MobilityRouteWithdrawalAcks, nil)
	pending, _ = pendingMobilityRoutes(nativeStatus(t, store))
	if len(pending) != 1 || native.route == nil {
		t.Fatal("failed cleanup lost recoverable ownership")
	}
	// An independent operator confirms removal; recovery can now retire the fence.
	native.route = nil
	if err := controller.reconcileMobilityRoutes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if native.route != nil {
		t.Fatal("restart empty plan left pending kernel route")
	}
	pending, _ = pendingMobilityRoutes(nativeStatus(t, store))
	if len(pending) != 0 {
		t.Fatal("successful withdrawal retained pending ownership")
	}
	assertNativeAck(t, nativeStatus(t, store), dynamicconfig.MobilityRouteWithdrawalAcks, controller.MobilityDataplane.Withdrawals)
}

func TestMobilityNativeAddressPendingSurvivesFailureAndRestart(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failure := errors.New("final address save rejected")
	faults := &nativeJournalStore{SQLiteStore: store, failField: "appliedMobilityStaticAddresses", failure: failure}
	present := false
	deleteErr := error(nil)
	controller := IPv4StaticAddressController{Router: &api.Router{}, Store: faults, DevicePresent: func(context.Context, string) bool { return true }, ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
		var addresses []netip.Prefix
		if present {
			addresses = []netip.Prefix{netip.MustParsePrefix("10.88.60.10/32")}
		}
		return staticAddressSnapshot{Index: 7, Scope: "address-fixture", Addresses: addresses}, nil
	}, Command: func(_ context.Context, _ string, args ...string) error {
		if strings.Contains(strings.Join(args, " "), " del ") {
			if deleteErr != nil {
				return deleteErr
			}
			present = false
		} else {
			present = true
		}
		return nil
	}, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{StaticAddresses: []dynamicconfig.MobilityIPv4AddressIntent{{ID: "owned", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Purpose: dynamicconfig.MobilityIPv4AddressPurposeCaptureSource, Interface: "lan0", Address: "10.88.60.10/32"}}}}
	if err := controller.reconcileMobilityStaticAddresses(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("final save error: %v", err)
	}
	pending, err := pendingMobilityAddresses(nativeStatus(t, store))
	if !present || err != nil || len(pending) != 1 {
		t.Fatalf("pending address not durable: %+v %v", pending, err)
	}
	// Restart has no process-local receipt; only persisted pending ownership.
	restarted := controller
	restarted.Store = store
	restarted.MobilityDataplane = nativeWithdrawalFixture()
	deleteErr = errors.New("uncertain native address must not be deleted")
	if err := restarted.reconcileMobilityStaticAddresses(context.Background()); err == nil || !strings.Contains(err.Error(), "without acknowledged ownership") {
		t.Fatalf("pending address was treated as ownership: %v", err)
	}
	if !present {
		t.Fatal("restart deleted an uncertain address")
	}
	assertNativeAck(t, nativeStatus(t, store), dynamicconfig.MobilityAddressWithdrawalAcks, nil)
	// Independent removal establishes absence; no routerd delete is authorized.
	present = false
	if err := restarted.reconcileMobilityStaticAddresses(context.Background()); err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("restart left pending address configured")
	}
	assertNativeAck(t, nativeStatus(t, store), dynamicconfig.MobilityAddressWithdrawalAcks, restarted.MobilityDataplane.Withdrawals)
}

func TestMobilityNativePendingSaveFailurePreventsKernelWrite(t *testing.T) {
	if platform.CurrentOS() != platform.OSLinux {
		t.Skip("Linux command fixture")
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failure := errors.New("pending journal failed")
	faults := &nativeJournalStore{SQLiteStore: store, failField: "pendingMobilityRoutes", failure: failure}
	native := &nativeRouteFixture{}
	controller := IPv4RouteController{Router: &api.Router{}, Store: faults, DevicePresent: func(context.Context, string) bool { return true }, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil }, NativeScope: ordinaryFixtureScope, Command: native.command, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{Routes: []dynamicconfig.MobilityIPv4RouteIntent{{ID: "owned", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory, Destination: "10.88.60.10/32", Device: "lan0"}}}}
	if err := controller.reconcileMobilityRoutes(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("pending failure hidden: %v", err)
	}
	if native.writes != 0 {
		t.Fatal("kernel changed before ownership journal")
	}
}

func TestMobilityNativeRouteReplacesItsOwnedDevice(t *testing.T) {
	if platform.CurrentOS() != platform.OSLinux {
		t.Skip("Linux command fixture")
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	native := &nativeRouteFixture{}
	controller := IPv4RouteController{Router: &api.Router{}, Store: store, DevicePresent: func(context.Context, string) bool { return true }, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil }, NativeScope: ordinaryFixtureScope, Command: native.command, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{Routes: []dynamicconfig.MobilityIPv4RouteIntent{{ID: "owned", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory, Destination: "10.88.60.10/32", Device: "lan0"}}}}
	if err := controller.reconcileMobilityRoutes(context.Background()); err != nil {
		t.Fatal(err)
	}
	controller.MobilityDataplane.Routes[0].Device = "wg0"
	if err := controller.reconcileMobilityRoutes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if native.route == nil || native.route.Device != "wg0" || native.writes != 3 {
		t.Fatalf("owned egress replacement was blocked or deleted: %+v writes=%d", native.route, native.writes)
	}
	routes, err := controller.appliedMobilityRoutes()
	if err != nil || len(routes) != 1 || routes[0].Device != "wg0" {
		t.Fatalf("new native receipt: %+v %v", routes, err)
	}
}

func TestMobilityNativePreferredSourceReadFailureRetainsOwnedRoute(t *testing.T) {
	if platform.CurrentOS() != platform.OSLinux {
		t.Skip("Linux command fixture")
	}
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-error", true: "malformed-JSON"}[corrupt], func(t *testing.T) {
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			native := &nativeRouteFixture{}
			controller := IPv4RouteController{Router: &api.Router{}, Store: store, DevicePresent: func(context.Context, string) bool { return true }, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, true, nil }, NativeScope: ordinaryFixtureScope, Command: native.command, MobilityDataplane: dynamicconfig.MobilityDataplanePlan{Routes: []dynamicconfig.MobilityIPv4RouteIntent{{ID: "owned", PoolRef: "cloudedge", PoolPrefix: "10.88.60.0/24", Purpose: dynamicconfig.MobilityIPv4RoutePurposeLocalInventory, Destination: "10.88.60.10/32", Device: "lan0"}}}}
			if err := controller.reconcileMobilityRoutes(context.Background()); err != nil {
				t.Fatal(err)
			}
			controller.MobilityDataplane.Routes[0].PreferredSource = "10.88.60.1"
			controller.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				if strings.Join(args, " ") == "-j -4 addr show" {
					if corrupt {
						return []byte("{"), nil
					}
					return nil, errors.New("preferred-source read rejected")
				}
				return native.command(ctx, name, args...)
			}
			if err := controller.reconcileMobilityRoutes(context.Background()); err == nil {
				t.Fatal("failed source observation silently changed desired route")
			}
			if native.writes != 1 || native.route == nil {
				t.Fatalf("observation failure mutated owned route: %+v writes=%d", native.route, native.writes)
			}
			routes, err := controller.appliedMobilityRoutes()
			if err != nil || len(routes) != 1 || routes[0].PreferredSource != "" {
				t.Fatalf("old source receipt lost: %+v %v", routes, err)
			}
		})
	}
}

type nativeHandoverPaths struct{}

func (nativeHandoverPaths) ListPaths(context.Context, string) ([]bgpdaemon.AppliedPath, error) {
	return nil, nil
}
func (nativeHandoverPaths) UpsertPath(_ context.Context, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedPath, error) {
	return path, nil
}
func (nativeHandoverPaths) DeletePath(context.Context, bgpdaemon.AppliedPath) error { return nil }

func nativeHandoverRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"}, Metadata: api.ObjectMeta{Name: "cloudedge"}, Spec: api.EventGroupSpec{NodeName: "onprem"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"}, Metadata: api.ObjectMeta{Name: "nodes"}, Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{{NodeRef: "onprem", Site: "lan", Role: "onprem"}, {NodeRef: "cloud", Site: "cloud", Role: "cloud"}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"}, Metadata: api.ObjectMeta{Name: "cloudedge"}, Spec: api.MobilityPoolSpec{Prefix: "10.88.60.0/24", GroupRef: "cloudedge", MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/nodes"}}, Members: []api.MobilityPoolMemberOverlay{{NodeRef: "onprem", StaticOwnedAddresses: []string{"10.88.60.10/32"}}}, StaticHandovers: []api.MobilityStaticHandover{{ID: "move-1", Address: "10.88.60.10/32", FromNodeRef: "onprem", ToNodeRef: "cloud"}}}},
	}}}
}

func TestMobilityNativeBarrierThroughActualProducerAndOwners(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "native.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := nativeHandoverRouter()
	source := mobilitycontroller.Controller{Router: router, Store: mobilityStore{evented: eventedStore{Store: store, Router: router}, data: store}, BGPPaths: nativeHandoverPaths{}, Now: func() time.Time { return now }}
	if err := source.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	parts, err := store.GetDynamicConfigPartsBySource(mobilitycontroller.DynamicSource("cloudedge", "onprem"))
	if err != nil || len(parts) != 1 {
		t.Fatalf("actual source plan: %+v %v", parts, err)
	}
	plan, err := codec.DecodeMobilityDataplanePlan(parts[0].MobilityDataplaneJSON)
	if err != nil || len(plan.Withdrawals) != 1 {
		t.Fatalf("actual typed request: %+v %v", plan, err)
	}
	if events, _ := store.ListFederationEvents("cloudedge", false, now.Unix()); len(events) != 0 {
		t.Fatal("source bypassed native owners")
	}
	addressOwner := IPv4StaticAddressController{Router: router, Store: store, MobilityDataplane: plan}
	if err := addressOwner.reconcileMobilityStaticAddresses(context.Background()); err != nil {
		t.Fatal(err)
	}
	routeOwner := IPv4RouteController{Router: router, Store: store, MobilityDataplane: plan}
	if err := routeOwner.reconcileMobilityRoutes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := source.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if events, _ := store.ListFederationEvents("cloudedge", false, now.Unix()); len(events) != 0 {
		t.Fatal("source treated two owner receipts as completion")
	}
	applier := &fakeSAMApplier{}
	samOwner := SAMController{Router: router, Store: store, OS: platform.OSLinux, Applier: applier, LocalCaptureIntents: plan.Captures, Withdrawals: plan.Withdrawals}
	if err := samOwner.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := source.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListFederationEvents("cloudedge", false, now.Unix())
	if err != nil || len(events) != 1 || events[0].Payload["handoverID"] != "move-1" {
		t.Fatalf("three actual owners failed to release: %+v %v", events, err)
	}
}
