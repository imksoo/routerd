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
	return s.SQLiteStore.SaveObjectStatus(version, kind, name, status)
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
	t                        *testing.T
	output, afterApply       string
	calls                    []string
	failRead, failDelete     bool
	failApply, applyThenFail bool
	deviceReady              bool
	beforeWrite              func()
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
	if len(args) >= 2 && args[0] == "route" && (args[1] == "replace" || args[1] == "del") {
		if f.beforeWrite != nil {
			f.beforeWrite()
		}
		if args[1] == "del" {
			if f.failDelete {
				return []byte("delete rejected"), errors.New("delete failed")
			}
			f.output = `[]`
		} else {
			if f.failApply && !f.applyThenFail {
				return nil, errors.New("apply failed")
			}
			f.output = f.afterApply
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

func newOrdinaryRouteFixture(t *testing.T) (IPv4RouteController, *routeOwnerTestStore, *routeOwnerCommandFixture, string) {
	t.Helper()
	requireLinuxRuntimeFixture(t)
	path := filepath.Join(t.TempDir(), "state.db")
	store := openOrdinaryRouteTestStore(t, path)
	native := &routeOwnerCommandFixture{t: t, output: `[]`, afterApply: ordinaryRouteNativeResponse("old0", "boot"), deviceReady: true}
	controller := IPv4RouteController{Router: ordinaryRouteTestRouter("old0"), Store: store, Command: native.run, DevicePresent: func(context.Context, string) bool { return native.deviceReady }}
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
			}
			controller.Router.Spec.Resources[0].Spec = spec
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(native.calls, "\n"), "ip route del 192.0.2.0/24 dev old0 metric 10 proto boot") || native.output != `[]` || readOrdinaryRouteReceipt(t, store) != nil {
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
			native.deviceReady = false
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
	if strings.Contains(strings.Join(native.calls, "\n"), "route replace") || readOrdinaryRouteReceipt(t, store).Route.Device != "old0" {
		t.Fatalf("failed withdrawal lost old owner: %v", native.calls)
	}
	native.failDelete, native.calls = false, nil
	if err := controller.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(native.calls, "\n")
	oldDelete, newApply := strings.Index(calls, "route del 192.0.2.0/24 dev old0"), strings.Index(calls, "route replace 192.0.2.0/24 dev new0")
	if oldDelete < 0 || newApply <= oldDelete || readOrdinaryRouteReceipt(t, store).Route.Device != "new0" {
		t.Fatalf("route identity transition order is wrong: %s", calls)
	}
}

func TestOrdinaryRoutePendingEffectRecoversAfterRestart(t *testing.T) {
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
			native.deviceReady = !inactive
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			receipt := readOrdinaryRouteReceipt(t, store)
			if inactive && receipt != nil || !inactive && (receipt == nil || receipt.Pending) || strings.Contains(strings.Join(native.calls, "\n"), "route replace") {
				t.Fatalf("restart did not recover exactly observed pending effect: receipt=%+v calls=%v", receipt, native.calls)
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
	if strings.Contains(strings.Join(native.calls, "\n"), "route replace") || readOrdinaryRouteReceipt(t, store) != nil {
		t.Fatalf("effect started without pending persistence: %v", native.calls)
	}
}

func TestOrdinaryRouteFailedApplyRetriesFromDurablePendingIdentity(t *testing.T) {
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
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			retried := strings.Contains(strings.Join(native.calls, "\n"), "route replace")
			if retried == changedBeforeFailure || readOrdinaryRouteReceipt(t, store).Pending {
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
	store.failPhase, native.deviceReady = "Pending", false
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
			if receipt := readOrdinaryRouteReceipt(t, store); receipt != nil || strings.Contains(strings.Join(native.calls, "\n"), "route replace") {
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
		{"fallback default", "destination: default\ngateway: 198.51.100.1\ninterface: em0\nflags: <UP,GATEWAY,DONE,STATIC>\n", false, false, false},
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
