// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestOrdinaryRouteReactivatedDuringPendingWithdrawalPreservesKnownEffect(t *testing.T) {
	for _, outcome := range []string{"restored", "restore-save-error", "source-read-error"} {
		t.Run(outcome, func(t *testing.T) {
			controller, store, native, path := newOrdinaryRouteFixture(t)
			spec, err := controller.Router.Spec.Resources[0].IPv4RouteSpec()
			if err != nil {
				t.Fatal(err)
			}
			spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/uplink", Phase: "Up"}}
			controller.Router.Spec.Resources[0].Spec = spec
			if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": "Up"}); err != nil {
				t.Fatal(err)
			}
			if err := controller.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			before := *readOrdinaryRouteReceipt(t, store)
			if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": "Down"}); err != nil {
				t.Fatal(err)
			}
			native.calls = nil
			waitObserved := false
			store.afterSave = func(status map[string]any) {
				if status["phase"] != "Withdrawing" {
					return
				}
				waitObserved, store.afterSave = true, nil
				if pending := readOrdinaryRouteReceipt(t, store); pending == nil || !pending.Pending || pending.Route != before.Route {
					t.Fatalf("withdrawal wait has no durable pending identity: %+v", pending)
				}
				if outcome == "source-read-error" {
					store.readKind = "Interface"
					return
				}
				if err := store.SQLiteStore.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": "Up"}); err != nil {
					t.Fatal(err)
				}
				if outcome == "restore-save-error" {
					store.failPhase = "Installed"
				}
			}
			if err := controller.reconcile(t.Context()); err == nil {
				t.Fatal("aborted/unconfirmed withdrawal was acknowledged as completed")
			}
			if !waitObserved {
				t.Fatal("actual durable withdrawal boundary was not reached")
			}
			calls := strings.Join(native.calls, "\n")
			if strings.Contains(calls, "route del") || strings.Contains(calls, "route add") || native.output != ordinaryRouteNativeResponse("old0", "boot") {
				t.Fatalf("recovered/unknown authority changed the known native effect: %s", calls)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openOrdinaryRouteTestStore(t, path)
			controller.Store = store
			after := readOrdinaryRouteReceipt(t, store)
			if after == nil || after.Route != before.Route || after.Scope != before.Scope || after.IfIndex != before.IfIndex || after.ParentToken != before.ParentToken || after.Pending != (outcome != "restored") {
				t.Fatalf("restart lost known/pending withdrawal identity: before=%+v after=%+v", before, after)
			}
			native.calls = nil
			if err := controller.reconcile(t.Context()); (err != nil) != (outcome != "restored") {
				t.Fatalf("restart did not preserve known vs uncertain ownership: %v", err)
			}
			if calls := strings.Join(native.calls, "\n"); strings.Contains(calls, "route del") || strings.Contains(calls, "route add") {
				t.Fatalf("restart replayed an unnecessary/uncertain native effect: %s", calls)
			}
		})
	}
}

func TestOrdinaryRouteReactivatedDuringPostAddWithdrawalKeepsAcknowledgedOwner(t *testing.T) {
	controller, store, native, _ := newOrdinaryRouteFixture(t)
	spec, _ := controller.Router.Spec.Resources[0].IPv4RouteSpec()
	spec.DependsOn = []api.ResourceDependencySpec{{Resource: "Interface/uplink", Phase: "Up"}}
	controller.Router.Spec.Resources[0].Spec = spec
	setDependency := func(phase string) {
		t.Helper()
		if err := store.SQLiteStore.SaveObjectStatus(api.NetAPIVersion, "Interface", "uplink", map[string]any{"phase": phase}); err != nil {
			t.Fatal(err)
		}
	}
	setDependency("Up")
	native.beforeWrite = func() {
		native.beforeWrite = nil
		setDependency("Down")
	}
	waitObserved := false
	store.afterSave = func(status map[string]any) {
		if status["phase"] == "Withdrawing" {
			waitObserved, store.afterSave = true, nil
			setDependency("Up")
		}
	}
	if err := controller.reconcile(t.Context()); err == nil {
		t.Fatal("reactivated post-add withdrawal was acknowledged as complete")
	}
	if !waitObserved || strings.Contains(strings.Join(native.calls, "\n"), "route del") || native.output != ordinaryRouteNativeResponse("old0", "boot") {
		t.Fatalf("post-add reversal removed its acknowledged effect: %v", native.calls)
	}
	if receipt := readOrdinaryRouteReceipt(t, store); receipt == nil || receipt.Pending {
		t.Fatalf("post-add caller overwrote the restored known receipt: %+v", receipt)
	}
}
