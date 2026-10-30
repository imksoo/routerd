// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type routeOwnerTestStore struct {
	*routerstate.SQLiteStore
	failPhase  string
	readKind   string
	failForget bool
	afterSave  func(map[string]any)
}

func (s *routeOwnerTestStore) DeleteObject(version, kind, name string) error {
	if kind == "IPv4Route" && s.failForget {
		return errors.New("injected owner deletion failure")
	}
	return s.SQLiteStore.DeleteObject(version, kind, name)
}

func (s *routeOwnerTestStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if kind == "IPv4Route" && status["phase"] == s.failPhase {
		return errors.New("injected route status write failure")
	}
	err := s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
	if err == nil && kind == "IPv4Route" && s.afterSave != nil {
		s.afterSave(status)
	}
	return err
}

func (s *routeOwnerTestStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if kind == s.readKind {
		return nil, errors.New("injected route observation failure")
	}
	return s.SQLiteStore.ReadObjectStatus(version, kind, name)
}

// This command fixture supplies actual native response shapes. It only changes
// its supplied response on successful writes; all decisions belong to Controller.
type routeOwnerCommandFixture struct {
	t                         *testing.T
	output, afterApply        string
	calls                     []string
	failRead, failDelete      bool
	failApply, applyThenFail  bool
	deviceReady               bool
	ignoreApply, ignoreDelete bool
	beforeWrite               func()
}

func (f *routeOwnerCommandFixture) run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if len(args) >= 5 && args[0] == "-j" && args[3] == "route" {
		if f.failRead {
			return nil, errors.New("route observation unavailable")
		}
		return []byte(f.output), nil
	}
	if len(args) >= 2 && args[0] == "route" && (args[1] == "add" || args[1] == "del") {
		if f.beforeWrite != nil {
			f.beforeWrite()
		}
		if args[1] == "del" {
			if f.failDelete {
				return []byte("delete rejected"), errors.New("delete failed")
			}
			if !f.ignoreDelete {
				f.output = `[]`
			}
		} else {
			if f.failApply && !f.applyThenFail {
				return nil, errors.New("apply failed")
			}
			if !f.ignoreApply {
				f.output = f.afterApply
			}
			if f.failApply {
				return nil, errors.New("apply result lost")
			}
		}
		return nil, nil
	}
	f.t.Fatalf("unexpected native command: %s", call)
	return nil, nil
}

func ordinaryRouteTestRouter(device string) *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "route"},
		Spec: api.IPv4RouteSpec{Destination: "192.0.2.0/24", Device: device, Metric: 10},
	}}}}
}

func ordinaryRouteNativeResponse(device, protocol string) string {
	return fmt.Sprintf(`[{"dst":"192.0.2.0/24","dev":%q,"metric":10,"protocol":%q}]`, device, protocol)
}

func TestOrdinaryRoutePreferredSourceRequiresAddressArray(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	controller := IPv4RouteController{Command: func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`null`), nil
	}}
	if _, err := controller.ordinaryRoutePreferredSourceIsLocal(t.Context(), "192.0.2.1"); err == nil {
		t.Fatal("null observation must not make a preferred source appear absent")
	}
}

func ordinaryFixtureScope(context.Context) (string, error)             { return "linux:fixture:1", nil }
func ordinaryFixtureDevice(context.Context, string) (int, bool, error) { return 17, true, nil }

func newOrdinaryRouteFixture(t *testing.T) (IPv4RouteController, *routeOwnerTestStore, *routeOwnerCommandFixture, string) {
	t.Helper()
	requireLinuxRuntimeFixture(t)
	path := filepath.Join(t.TempDir(), "state.db")
	store := openOrdinaryRouteTestStore(t, path)
	native := &routeOwnerCommandFixture{t: t, output: `[]`, afterApply: ordinaryRouteNativeResponse("old0", "boot"), deviceReady: true}
	controller := IPv4RouteController{Router: ordinaryRouteTestRouter("old0"), Store: store, Command: native.run, NativeScope: ordinaryFixtureScope, DeviceLookup: func(context.Context, string) (int, bool, error) { return 17, native.deviceReady, nil }}
	return controller, store, native, path
}

func openOrdinaryRouteTestStore(t *testing.T, path string) *routeOwnerTestStore {
	t.Helper()
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &routeOwnerTestStore{SQLiteStore: store}
}

func readOrdinaryRouteReceipt(t *testing.T, store *routeOwnerTestStore) *ipv4RouteReceipt {
	t.Helper()
	status, err := store.SQLiteStore.ReadObjectStatus(api.NetAPIVersion, "IPv4Route", "route")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ipv4RouteReceiptFromStatus(status)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestOrdinaryRouteInactiveConditionsWithdrawAppliedIdentity(t *testing.T) {
	for _, reason := range []string{"dependency", "dependency-missing", "device-missing", "device-not-ready"} {
		t.Run(reason, func(t *testing.T) {
			controller, store, native, _ := newOrdinaryRouteFixture(t)
			spec, _ := controller.Router.Spec.Resources[0].IPv4RouteSpec()
			if reason == "dependency" || reason == "dependency-missing" {
				spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/uplink", Phase: "Up"}}
			}
			if reason == "device-missing" {
				spec.Device = ""
				spec.DeviceFrom = api.StatusValueSourceSpec{Resource: "Interface/uplink", Field: "ifname"}
			}
			controller.Router.Spec.Resources[0].Spec = spec
			if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": "Up", "ifname": "old0"}); err != nil {
				t.Fatal(err)
			}
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			native.calls = nil
			switch reason {
			case "dependency":
				if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": "Down"}); err != nil {
					t.Fatal(err)
				}
			case "dependency-missing", "device-missing":
				if err := store.DeleteObject(api.NetAPIVersion, "Interface", "uplink"); err != nil {
					t.Fatal(err)
				}
			case "device-not-ready":
				native.deviceReady = false
				native.output = `[]`
			}
			controller.Router.Spec.Resources[0].Spec = spec
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if (reason != "device-not-ready" && !strings.Contains(strings.Join(native.calls, "\n"), "ip route del 192.0.2.0/24 dev old0 metric 10 proto boot")) || native.output != `[]` || readOrdinaryRouteReceipt(t, store) != nil {
				t.Fatalf("inactive route retained forwarding or wrong owner: %v", native.calls)
			}
		})
	}
}

func TestOrdinaryRouteRemovalFailureRetainsOwnerUntilEffectAndForgetSucceed(t *testing.T) {
	for _, failure := range []string{"native", "forget"} {
		t.Run(failure, func(t *testing.T) {
			controller, store, native, path := newOrdinaryRouteFixture(t)
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			controller.Router = &api.Router{}
			native.failDelete, store.failForget = failure == "native", failure == "forget"
			if err := controller.reconcile(t.Context()); err == nil || readOrdinaryRouteReceipt(t, store) == nil {
				t.Fatal("failed resource cleanup discarded ownership")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openOrdinaryRouteTestStore(t, path)
			controller.Store = store
			native.failDelete = false
			if failure == "native" {
				if err := controller.reconcile(t.Context()); err == nil {
					t.Fatal("pending-present delete replay was admitted")
				}
				native.output = `[]` // Operator inspected and removed the ambiguous row.
			}
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if native.output != `[]` || readOrdinaryRouteReceipt(t, store) != nil {
				t.Fatal("restart did not finish deleted resource's exact cleanup")
			}
		})
	}
}

func TestOrdinaryRoutePreservesExternalManagement(t *testing.T) {
	controller, store, native, _ := newOrdinaryRouteFixture(t)
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "route")
	status["managedBy"] = "external"
	if err := store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", "route", status); err != nil {
		t.Fatal(err)
	}
	native.calls, native.deviceReady = nil, false
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	controller.Router = &api.Router{}
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(native.calls) != 0 || store.ObjectStatus(api.NetAPIVersion, "IPv4Route", "route")["managedBy"] != "external" {
		t.Fatalf("external route or ownership was changed: %v", native.calls)
	}
}

func TestOrdinaryRouteFailedWithdrawalSurvivesRestart(t *testing.T) {
	for _, failure := range []string{"delete", "read", "malformed", "protocol-missing"} {
		t.Run(failure, func(t *testing.T) {
			controller, store, native, path := newOrdinaryRouteFixture(t)
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			controller.Router = &api.Router{}
			switch failure {
			case "delete":
				native.failDelete = true
			case "read":
				native.failRead = true
			case "malformed":
				native.output = "not a route observation"
			case "protocol-missing":
				native.output = `[{"dst":"192.0.2.0/24","dev":"old0","metric":10}]`
			}
			if err := controller.reconcile(t.Context()); err == nil {
				t.Fatal("failed withdrawal was acknowledged")
			}
			if receipt := readOrdinaryRouteReceipt(t, store); receipt == nil || receipt.Route.Device != "old0" {
				t.Fatalf("failure discarded applied identity: %+v", receipt)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openOrdinaryRouteTestStore(t, path)
			controller.Store = store
			native.failDelete, native.failRead = false, false
			native.output = ordinaryRouteNativeResponse("old0", "boot")
			if failure == "delete" {
				if err := controller.reconcile(t.Context()); err == nil {
					t.Fatal("lost delete acknowledgement was replayed")
				}
				native.output = `[]`
			}
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if readOrdinaryRouteReceipt(t, store) != nil || native.output != `[]` {
				t.Fatal("restart did not finish owned withdrawal")
			}
		})
	}
}

func TestOrdinaryRouteDeviceChangeWaitsForOldWithdrawal(t *testing.T) {
	controller, store, native, _ := newOrdinaryRouteFixture(t)
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	controller.Router = ordinaryRouteTestRouter("new0")
	native.afterApply = ordinaryRouteNativeResponse("new0", "boot")
	native.failDelete, native.calls = true, nil
	if err := controller.reconcile(t.Context()); err == nil {
		t.Fatal("new route applied before failed old withdrawal")
	}
	if strings.Contains(strings.Join(native.calls, "\n"), "route add") || readOrdinaryRouteReceipt(t, store).Route.Device != "old0" {
		t.Fatalf("failed withdrawal lost old owner: %v", native.calls)
	}
	native.failDelete, native.calls = false, nil
	if err := controller.reconcile(t.Context()); err == nil {
		t.Fatal("pending old route was deleted/replaced without ownership proof")
	}
	if strings.Contains(strings.Join(native.calls, "\n"), "route add") || strings.Contains(strings.Join(native.calls, "\n"), "route del") {
		t.Fatal("unknown prior row was mutated")
	}
	native.output = `[]`
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(native.calls, "\n")
	newApply := strings.Index(calls, "route add 192.0.2.0/24 dev new0")
	if newApply < 0 || readOrdinaryRouteReceipt(t, store).Route.Device != "new0" {
		t.Fatalf("route identity transition order is wrong: %s", calls)
	}
}

func TestOrdinaryRoutePendingPresenceFencesRestartUntilObservedAbsent(t *testing.T) {
	for _, inactive := range []bool{false, true} {
		t.Run(fmt.Sprint(inactive), func(t *testing.T) {
			controller, store, native, path := newOrdinaryRouteFixture(t)
			store.failPhase = "Installed"
			native.beforeWrite = func() {
				if receipt := readOrdinaryRouteReceipt(t, store); receipt == nil || !receipt.Pending {
					t.Fatalf("effect started without durable pending identity: %+v", receipt)
				}
			}
			if err := controller.reconcile(t.Context()); err == nil {
				t.Fatal("failed completion write was acknowledged")
			}
			if receipt := readOrdinaryRouteReceipt(t, store); receipt == nil || !receipt.Pending {
				t.Fatal("write-result loss discarded pending identity")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openOrdinaryRouteTestStore(t, path)
			controller.Store = store
			native.beforeWrite, native.calls = nil, nil
			if inactive {
				controller.Router = &api.Router{}
			}
			if err := controller.reconcile(t.Context()); err == nil {
				t.Fatal("pending-present row was adopted/deleted")
			}
			receipt := readOrdinaryRouteReceipt(t, store)
			if receipt == nil || !receipt.Pending || strings.Contains(strings.Join(native.calls, "\n"), "route add") || strings.Contains(strings.Join(native.calls, "\n"), "route del") {
				t.Fatalf("unknown ownership lost fence: %+v %v", receipt, native.calls)
			}
			native.output = `[]` // Explicit operator/native removal establishes absence.
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			receipt = readOrdinaryRouteReceipt(t, store)
			if inactive && receipt != nil || !inactive && (receipt == nil || receipt.Pending) {
				t.Fatalf("absence did not unblock recovery: %+v", receipt)
			}
		})
	}
}

func TestOrdinaryRoutePendingWriteFailurePreventsMutation(t *testing.T) {
	controller, store, native, _ := newOrdinaryRouteFixture(t)
	store.failPhase = "Applying"
	if err := controller.reconcile(t.Context()); err == nil {
		t.Fatal("failed pending write was ignored")
	}
	if strings.Contains(strings.Join(native.calls, "\n"), "route add") || readOrdinaryRouteReceipt(t, store) != nil {
		t.Fatalf("effect started without pending persistence: %v", native.calls)
	}
}

func TestOrdinaryRouteFailedApplyRetriesOnlyAfterObservedAbsence(t *testing.T) {
	for _, changedBeforeFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(changedBeforeFailure), func(t *testing.T) {
			controller, store, native, path := newOrdinaryRouteFixture(t)
			native.failApply, native.applyThenFail = true, changedBeforeFailure
			if err := controller.reconcile(t.Context()); err == nil {
				t.Fatal("failed apply was acknowledged")
			}
			if receipt := readOrdinaryRouteReceipt(t, store); receipt == nil || !receipt.Pending {
				t.Fatal("failed/uncertain apply lost pending ownership")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openOrdinaryRouteTestStore(t, path)
			controller.Store = store
			native.failApply, native.calls = false, nil
			if changedBeforeFailure {
				if err := controller.reconcile(t.Context()); err == nil {
					t.Fatal("ambiguous add was adopted")
				}
				if !readOrdinaryRouteReceipt(t, store).Pending {
					t.Fatal("ambiguous add lost pending fence")
				}
				native.output = `[]`
			}
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			retried := strings.Contains(strings.Join(native.calls, "\n"), "route add")
			if !retried || readOrdinaryRouteReceipt(t, store).Pending {
				t.Fatalf("uncertain result recovery did not use actual observation: calls=%v", native.calls)
			}
		})
	}
}

func TestOrdinaryRouteFailedWithdrawalCompletionRetainsReceipt(t *testing.T) {
	controller, store, native, path := newOrdinaryRouteFixture(t)
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	store.failPhase = "Pending"
	controller.Router = &api.Router{}
	store.failForget = true
	if err := controller.reconcile(t.Context()); err == nil {
		t.Fatal("failed withdrawal completion was acknowledged")
	}
	if native.output != `[]` || readOrdinaryRouteReceipt(t, store) == nil {
		t.Fatal("successful delete followed by failed save lost recovery ownership")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openOrdinaryRouteTestStore(t, path)
	controller.Store, native.calls = store, nil
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if readOrdinaryRouteReceipt(t, store) != nil || strings.Contains(strings.Join(native.calls, "\n"), "route del") {
		t.Fatalf("restart repeated absent delete or retained stale receipt: %v", native.calls)
	}
}

func TestOrdinaryRouteDryRunPreservesLiveReceiptAndRemoval(t *testing.T) {
	controller, store, native, _ := newOrdinaryRouteFixture(t)
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	controller.DryRun, controller.Router, native.calls = true, ordinaryRouteTestRouter("new0"), nil
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if receipt := readOrdinaryRouteReceipt(t, store); receipt == nil || receipt.Route.Device != "old0" || len(native.calls) != 0 {
		t.Fatalf("dry-run replaced live ownership: %+v calls=%v", receipt, native.calls)
	}
	controller.Router = &api.Router{}
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if readOrdinaryRouteReceipt(t, store) == nil {
		t.Fatal("dry-run removal discarded live ownership")
	}
	controller.DryRun = false
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if readOrdinaryRouteReceipt(t, store) != nil || native.output != `[]` {
		t.Fatal("live removal did not withdraw retained owner")
	}
}

func TestOrdinaryRouteDoesNotAdoptOrDeleteForeignRoutes(t *testing.T) {
	for _, protocol := range []string{"boot", "static", "bgp"} {
		t.Run(protocol, func(t *testing.T) {
			controller, store, native, _ := newOrdinaryRouteFixture(t)
			native.output = ordinaryRouteNativeResponse("old0", protocol)
			if err := controller.reconcile(t.Context()); err == nil {
				t.Fatal("foreign matching route was adopted")
			}
			if receipt := readOrdinaryRouteReceipt(t, store); receipt != nil || strings.Contains(strings.Join(native.calls, "\n"), "route add") {
				t.Fatal("foreign route acquired an ownership receipt")
			}
		})
	}
	for _, replacement := range []string{ordinaryRouteNativeResponse("old0", "bgp"), ordinaryRouteNativeResponse("old0", "static"), ordinaryRouteNativeResponse("foreign0", "boot"), strings.Replace(ordinaryRouteNativeResponse("old0", "boot"), `"metric":10`, `"metric":200`, 1)} {
		controller, store, native, _ := newOrdinaryRouteFixture(t)
		if err := controller.reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		native.output, native.calls, native.deviceReady = replacement, nil, false
		if err := controller.reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(native.calls, "\n"), "route del") || native.output != replacement || readOrdinaryRouteReceipt(t, store) != nil {
			t.Fatalf("withdrawal deleted foreign replacement: %v", native.calls)
		}
	}
}

func TestFreeBSDOrdinaryRouteObservationPreservesForeignReplacement(t *testing.T) {
	wanted := ipv4RouteRemoval{Type: "unicast", Destination: "192.0.2.0/24", Device: "em0", Gateway: "198.51.100.1", Metric: "0"}
	for _, tc := range []struct {
		name, output                 string
		matching, collision, wantErr bool
	}{
		{"owned", "destination: 192.0.2.0\nmask: 255.255.255.0\ngateway: 198.51.100.1\ninterface: em0\nflags: <UP,GATEWAY,DONE,STATIC>\n", true, true, false},
		{"other interface", "destination: 192.0.2.0\nmask: 255.255.255.0\ngateway: 198.51.100.1\ninterface: em1\nflags: <UP,GATEWAY,DONE,STATIC>\n", false, true, false},
		{"BGP", "destination: 192.0.2.0\nmask: 255.255.255.0\ngateway: 198.51.100.1\ninterface: em0\nflags: <UP,GATEWAY,DONE,STATIC,PROTO1>\n", false, true, false},
		{"reject", "destination: 192.0.2.0\nmask: 255.255.255.0\ngateway: 198.51.100.1\ninterface: em0\nflags: <UP,GATEWAY,DONE,STATIC,REJECT>\n", false, true, false},
		{"fallback default", "destination: default\ngateway: 198.51.100.1\ninterface: em0\nflags: <UP,GATEWAY,DONE,STATIC>\n", false, false, false},
		{"absent with successful exit", "route: route has not been found: No error: 0\n", false, false, false},
		{"unknown empty output", "", false, false, true},
		{"missing mask", "destination: 192.0.2.0\ngateway: 198.51.100.1\ninterface: em0\nflags: <UP,GATEWAY,DONE,STATIC>\n", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller := IPv4RouteController{Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "route" || strings.Join(args, " ") != "-n get -net 192.0.2.0/24" {
					t.Fatalf("unexpected native lookup: %s %v", name, args)
				}
				return []byte(tc.output), nil
			}}
			matching, collision, err := controller.observeFreeBSDOrdinaryRoute(t.Context(), wanted)
			if matching != tc.matching || collision != tc.collision || (err != nil) != tc.wantErr {
				t.Fatalf("native observation = %t,%t,%v", matching, collision, err)
			}
		})
	}
}

func TestOrdinaryRouteReadFailureDoesNotWithdrawOrLoseOwner(t *testing.T) {
	for _, kind := range []string{"IPv4Route", "Interface"} {
		t.Run(kind, func(t *testing.T) {
			controller, store, native, _ := newOrdinaryRouteFixture(t)
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if kind == "Interface" {
				spec, _ := controller.Router.Spec.Resources[0].IPv4RouteSpec()
				spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/uplink", Phase: "Up"}}
				controller.Router.Spec.Resources[0].Spec = spec
			}
			store.readKind, native.calls = kind, nil
			if err := controller.reconcile(t.Context()); err == nil {
				t.Fatal("read failure became absence")
			}
			if readOrdinaryRouteReceipt(t, store) == nil || len(native.calls) != 0 {
				t.Fatalf("read failure lost owner or mutated kernel: %v", native.calls)
			}
		})
	}
}

func TestOrdinaryRouteZeroMetricDoesNotMatchOtherPriority(t *testing.T) {
	controller, _, native, _ := newOrdinaryRouteFixture(t)
	native.output = ordinaryRouteNativeResponse("old0", "boot")
	wanted := ipv4RouteRemoval{Type: "unicast", Destination: "192.0.2.0/24", Device: "old0", Metric: "0"}
	matching, collision, err := controller.observeOrdinaryRoute(t.Context(), wanted)
	if err != nil || matching || collision {
		t.Fatalf("metric zero became wildcard: matching=%t collision=%t err=%v", matching, collision, err)
	}
	if err := controller.removeOrdinaryRoute(t.Context(), wanted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(native.calls, "\n"), "route del") {
		t.Fatal("zero-metric receipt deleted a different priority route")
	}
}

func TestOrdinaryRoutePendingWaitRechecksNativeAndDesiredAuthority(t *testing.T) {
	for _, change := range []string{"foreign-arrival", "dependency-down", "device-replaced", "scope-changed", "device-read-error"} {
		t.Run(change, func(t *testing.T) {
			c, store, native, _ := newOrdinaryRouteFixture(t)
			scope, index := "linux:fixture:1", 17
			var lookupErr error
			c.NativeScope = func(context.Context) (string, error) { return scope, nil }
			c.DeviceLookup = func(context.Context, string) (int, bool, error) { return index, true, lookupErr }
			spec, _ := c.Router.Spec.Resources[0].IPv4RouteSpec()
			spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/ready", Phase: "Up"}}
			c.Router.Spec.Resources[0].Spec = spec
			if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "ready", map[string]any{"phase": "Up"}); err != nil {
				t.Fatal(err)
			}
			store.afterSave = func(status map[string]any) {
				if status["phase"] != "Applying" {
					return
				}
				switch change {
				case "foreign-arrival":
					native.output = native.afterApply
				case "dependency-down":
					if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "ready", map[string]any{"phase": "Down"}); err != nil {
						t.Fatal(err)
					}
				case "device-replaced":
					index++
				case "scope-changed":
					scope = "linux:new-boot:1"
				case "device-read-error":
					lookupErr = errors.New("native enumeration failed")
				}
			}
			if err := c.reconcile(t.Context()); err == nil {
				t.Fatal("changed premise admitted add")
			}
			calls := strings.Join(native.calls, "\n")
			if strings.Contains(calls, "route add") || strings.Contains(calls, "route del") {
				t.Fatalf("pending wait authorized native write: %s", calls)
			}
			if change == "foreign-arrival" && native.output != native.afterApply {
				t.Fatal("foreign arrival was removed")
			}
		})
	}
}

func TestOrdinaryRouteRequiredDeviceReadFailurePreservesAppliedOwner(t *testing.T) {
	c, store, native, _ := newOrdinaryRouteFixture(t)
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := readOrdinaryRouteReceipt(t, store)
	native.calls = nil
	c.DeviceLookup = func(context.Context, string) (int, bool, error) {
		return 0, false, errors.New("required native device observation unavailable")
	}
	if err := c.reconcile(t.Context()); err == nil {
		t.Fatal("read failure became inactive desired state")
	}
	after := readOrdinaryRouteReceipt(t, store)
	if before == nil || after == nil || *before != *after || len(native.calls) != 0 {
		t.Fatalf("unknown device removed native/ownership: before=%+v after=%+v calls=%v", before, after, native.calls)
	}
}

func TestOrdinaryRouteAddAndDeleteRequireActualReadback(t *testing.T) {
	for _, operation := range []string{"add", "delete"} {
		t.Run(operation, func(t *testing.T) {
			c, store, native, path := newOrdinaryRouteFixture(t)
			if operation == "delete" {
				if err := c.reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
				c.Router = &api.Router{}
				native.ignoreDelete = true
			} else {
				native.ignoreApply = true
			}
			if err := c.reconcile(t.Context()); err == nil {
				t.Fatal("unobserved native write was acknowledged")
			}
			receipt := readOrdinaryRouteReceipt(t, store)
			if receipt == nil || !receipt.Pending {
				t.Fatalf("unconfirmed %s lost pending fence: %+v", operation, receipt)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			c.Store = openOrdinaryRouteTestStore(t, path)
			native.calls = nil
			native.ignoreApply = false
			native.ignoreDelete = false
			if operation == "delete" {
				if err := c.reconcile(t.Context()); err == nil {
					t.Fatal("pending-present delete was replayed")
				}
				if strings.Contains(strings.Join(native.calls, "\n"), "route del") {
					t.Fatal("unknown row deleted")
				}
				native.output = `[]` // Operator removal, not guessed receipt ownership.
			}
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOrdinaryRouteAppliedScopeOrIndexReplacementCannotDeletePresentRow(t *testing.T) {
	for _, change := range []string{"scope", "index"} {
		t.Run(change, func(t *testing.T) {
			c, store, native, _ := newOrdinaryRouteFixture(t)
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if change == "scope" {
				c.NativeScope = func(context.Context) (string, error) { return "linux:other-boot:1", nil }
			} else {
				c.DeviceLookup = func(context.Context, string) (int, bool, error) { return 18, true, nil }
			}
			c.Router = &api.Router{}
			native.calls = nil
			if err := c.reconcile(t.Context()); err == nil {
				t.Fatal("old native identity authorized deletion")
			}
			if strings.Contains(strings.Join(native.calls, "\n"), "route del") || readOrdinaryRouteReceipt(t, store) == nil {
				t.Fatal("replacement row or unresolved receipt lost")
			}
			native.output = `[]`
			if err := c.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if readOrdinaryRouteReceipt(t, store) != nil {
				t.Fatal("observed absence did not retire old receipt")
			}
		})
	}
}

func TestOrdinaryRouteDependencyChangesDuringAddWithdrawsWithoutInstalledPublication(t *testing.T) {
	c, store, native, _ := newOrdinaryRouteFixture(t)
	spec, _ := c.Router.Spec.Resources[0].IPv4RouteSpec()
	spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/ready", Phase: "Up"}}
	c.Router.Spec.Resources[0].Spec = spec
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "ready", map[string]any{"phase": "Up"}); err != nil {
		t.Fatal(err)
	}
	native.beforeWrite = func() {
		if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "ready", map[string]any{"phase": "Down"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, _ := store.ReadObjectStatus(api.NetAPIVersion, "IPv4Route", "route")
	if status["phase"] != "Pending" || native.output != `[]` || readOrdinaryRouteReceipt(t, store) != nil {
		t.Fatalf("changed authority published/retained forwarding: %+v calls=%v", status, native.calls)
	}
}
