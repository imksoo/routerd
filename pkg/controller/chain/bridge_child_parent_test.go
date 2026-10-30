// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	ra "github.com/imksoo/routerd/pkg/routeradvertisement"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestBridgeForeignParentCannotAcquireStaticAddress(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := bridgeVXLANTestRouter(false)
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
		Metadata: api.ObjectMeta{Name: "child"},
		Spec:     api.IPv4StaticAddressSpec{Interface: "legacy-l2", Address: "192.0.2.1/24"},
	})
	present, writes := false, 0
	child := IPv4StaticAddressController{Router: router, Store: store,
		ObserveAddresses: func(_ context.Context, ifname string) (staticAddressSnapshot, error) {
			if ifname != "br-l2" {
				t.Fatalf("unexpected native interface %q", ifname)
			}
			// A same-name native bridge is present, but routerd never acquired
			// it. Its address snapshot cannot stand in for its parent's proof.
			observation := staticAddressSnapshot{Index: 12, Scope: "fixture-kernel"}
			if present {
				observation.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return observation, nil
		},
		Command: func(context.Context, string, ...string) error {
			writes++
			present = true
			return nil
		},
	}
	err = child.Reconcile(t.Context())
	if writes != 0 || present {
		t.Fatalf("foreign Bridge parent received child address: writes=%d err=%v", writes, err)
	}
}

func TestBridgeParentStaticAddressProofAndRecordedCleanup(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, scenario := range []string{"owned", "pending", "withdrawing", "foreign-after-save", "removed", "removed-foreign", "removed-missing-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			parent, native := bridgeNativeFixture(t, &bridgeActivationStore{mapStore: mapStore{}})
			parent.Store = db
			parent.NativeScope = func(context.Context) (string, error) { return "fixture-kernel", nil }
			if err := parent.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			owners, err := parent.readBridgeOwners()
			if err != nil || len(owners) != 1 {
				t.Fatalf("parent receipt: %+v %v", owners, err)
			}
			owner := owners[0]
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "child"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "legacy-l2", Address: "192.0.2.1/24"},
			})
			if scenario == "pending" || scenario == "withdrawing" {
				changed := owner
				if scenario == "pending" {
					changed.Activated = false
				} else {
					changed.Withdrawing = true
				}
				if err := parent.writeBridgeOwner(changed); err != nil {
					t.Fatal(err)
				}
			}
			store := &staticAddressFaultStore{SQLiteStore: db}
			if scenario == "foreign-after-save" {
				store.afterPending = func() { native.alias["br-l2"] = "operator" }
			}
			present, adds, deletes := false, 0, 0
			child := IPv4StaticAddressController{Router: parent.Router, Store: store, Bridges: &parent,
				ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
					observation := staticAddressSnapshot{Index: native.links["br-l2"].Index, Scope: "fixture-kernel"}
					if present {
						observation.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
					}
					return observation, nil
				},
				Command: func(_ context.Context, _ string, args ...string) error {
					if strings.Contains(strings.Join(args, " "), "del") {
						deletes++
						present = false
					} else {
						adds++
						present = true
					}
					return nil
				},
			}
			err = child.Reconcile(t.Context())
			if scenario == "pending" || scenario == "withdrawing" || scenario == "foreign-after-save" {
				if err == nil || adds != 0 || present {
					t.Fatalf("unproven parent admitted child address: adds=%d err=%v", adds, err)
				}
				return
			}
			status, readErr := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "child")
			receipt, receiptErr := staticReceipt(status)
			if err != nil || adds != 1 || readErr != nil || receiptErr != nil || receipt == nil || receipt.ParentToken != "bridge:"+owner.Token {
				t.Fatalf("owned parent proof missing: receipt=%+v adds=%d err=%v read=%v decode=%v", receipt, adds, err, readErr, receiptErr)
			}
			if scenario == "owned" {
				return
			}
			child.Router = &api.Router{}
			if scenario == "removed-foreign" {
				native.alias["br-l2"] = "operator"
			}
			if scenario == "removed-missing-receipt" {
				if err := os.Remove(parent.bridgeOwnerPath(owner.Resource)); err != nil {
					t.Fatal(err)
				}
			}
			err = child.Reconcile(t.Context())
			if scenario == "removed" {
				if err != nil || deletes != 1 || present {
					t.Fatalf("recorded cleanup failed: deletes=%d present=%t err=%v", deletes, present, err)
				}
				return
			}
			if err == nil || deletes != 0 || !present {
				t.Fatalf("unproven cleanup mutated child: deletes=%d present=%t err=%v", deletes, present, err)
			}
			status, readErr = db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "child")
			if receipt, receiptErr = staticReceipt(status); readErr != nil || receiptErr != nil || receipt == nil || receipt.ParentToken != "bridge:"+owner.Token {
				t.Fatalf("blocked cleanup lost receipt: %+v read=%v decode=%v", receipt, readErr, receiptErr)
			}
			if scenario == "removed-foreign" {
				native.alias["br-l2"] = owner.proof()
				if err := child.Reconcile(t.Context()); err != nil || deletes != 1 || present {
					t.Fatalf("restored recorded proof could not clean up: deletes=%d present=%t err=%v", deletes, present, err)
				}
			}
		})
	}
}

func TestBridgeParentRechecksReceiptAfterLastNativeRead(t *testing.T) {
	parent, native := bridgeNativeFixture(t, &bridgeActivationStore{mapStore: mapStore{}})
	parent.NativeScope = func(context.Context) (string, error) { return "fixture-kernel", nil }
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	original := parent.ReadFile
	changed := false
	reads := 0
	parent.ReadFile = func(path string) ([]byte, error) {
		data, err := original(path)
		if filepath.Base(path) == "ifalias" {
			reads++
		}
		if filepath.Base(path) == "ifalias" && reads == 2 && !changed {
			owners, readErr := parent.readBridgeOwners()
			if readErr != nil || len(owners) != 1 {
				t.Fatalf("parent receipt: %+v %v", owners, readErr)
			}
			owners[0].Withdrawing = true
			if writeErr := parent.writeBridgeOwner(owners[0]); writeErr != nil {
				t.Fatal(writeErr)
			}
			changed = true
		}
		return data, err
	}
	native.commands = nil
	reader := nativeParentReader{Router: parent.Router, Store: parent.Store, Bridges: &parent}
	link, err := reader.observeDeclared(t.Context(), "br-l2")
	if !changed || err == nil || link.Exists || len(native.commands) != 0 {
		t.Fatalf("withdrawal during native read admitted child: link=%+v changed=%t err=%v commands=%v", link, changed, err, native.commands)
	}
}

func bridgeChildParentFixture(t *testing.T) (BridgeController, *bridgeKernelFixture, *routerstate.SQLiteStore) {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	parent, native := bridgeNativeFixture(t, &bridgeActivationStore{mapStore: mapStore{}})
	parent.Store = store
	parent.NativeScope = func(context.Context) (string, error) { return "fixture-kernel", nil }
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	return parent, native, store
}

func TestBridgeParentRouteProofAndRecordedCleanup(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	parent, native, db := bridgeChildParentFixture(t)
	parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, ordinaryRouteTestRouter("br-l2").Spec.Resources...)
	store := &routeOwnerTestStore{SQLiteStore: db}
	commands := &routeOwnerCommandFixture{t: t, output: `[]`, afterApply: ordinaryRouteNativeResponse("br-l2", "boot")}
	child := IPv4RouteController{Router: parent.Router, Store: store, Bridges: &parent, Command: commands.run,
		NativeScope:  parent.NativeScope,
		DeviceLookup: func(context.Context, string) (int, bool, error) { return native.links["br-l2"].Index, true, nil },
	}
	if err := child.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipt := readOrdinaryRouteReceipt(t, store)
	if receipt == nil || !strings.HasPrefix(receipt.ParentToken, "bridge:") || commands.output != commands.afterApply {
		t.Fatalf("route did not retain actual Bridge proof: %+v", receipt)
	}
	proof := native.alias["br-l2"]
	native.alias["br-l2"] = "operator"
	if err := child.reconcile(t.Context()); err == nil || commands.output != commands.afterApply {
		t.Fatalf("foreign parent authorized route reconciliation: %v calls=%v", err, commands.calls)
	}
	child.Router = &api.Router{}
	if err := child.reconcile(t.Context()); err == nil || commands.output != commands.afterApply || readOrdinaryRouteReceipt(t, store) == nil {
		t.Fatalf("foreign parent authorized recorded route deletion: %v calls=%v", err, commands.calls)
	}
	native.alias["br-l2"] = proof
	if err := child.reconcile(t.Context()); err != nil || commands.output != `[]` || readOrdinaryRouteReceipt(t, store) != nil {
		t.Fatalf("recorded Bridge proof could not withdraw route: %v calls=%v", err, commands.calls)
	}
}

func TestBridgeParentDelegatedLANProofAndRecordedCleanup(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store, router, child, _ := sqliteNativeLANFixture(t)
	parent, native := bridgeNativeFixture(t, &bridgeActivationStore{mapStore: mapStore{}})
	parent.Store = store
	parent.NativeScope = func(context.Context) (string, error) { return "fixture-kernel", nil }
	router.Spec.Resources[0] = parent.Router.Spec.Resources[1]
	spec, err := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.Interface = "legacy-l2"
	router.Spec.Resources[2].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, parent.Router.Spec.Resources[0])
	parent.Router = router
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "legacy-l2", map[string]any{"phase": "Up"}); err != nil {
		t.Fatal(err)
	}
	child.Bridges = &parent
	present, writes := false, 0
	child.ObserveAddress = func(context.Context, string, string, bool) (delegatedAddressObservation, error) {
		return delegatedAddressObservation{ifIndex: native.links["br-l2"].Index, scope: "fixture-kernel", present: present, usable: present}, nil
	}
	child.Command = func(_ context.Context, _ string, args ...string) error {
		writes++
		present = !strings.Contains(strings.Join(args, " "), "del")
		return nil
	}
	if err := child.Reconcile(t.Context()); err != nil || writes != 1 {
		t.Fatalf("owned Bridge parent rejected delegated address: writes=%d err=%v", writes, err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, receiptErr := delegatedReceipt(status)
	if err != nil || receiptErr != nil || receipt == nil || !strings.HasPrefix(receipt.ParentToken, "bridge:") {
		t.Fatalf("delegated address did not retain actual Bridge proof: %+v read=%v decode=%v", receipt, err, receiptErr)
	}
	proof := native.alias["br-l2"]
	native.alias["br-l2"] = "operator"
	if err := child.Reconcile(t.Context()); err == nil || writes != 1 || !present {
		t.Fatalf("foreign parent authorized delegated address reconciliation: writes=%d err=%v", writes, err)
	}
	child.Router = &api.Router{}
	child.DeclaredRouter = child.Router
	if err := child.Reconcile(t.Context()); err == nil || writes != 1 || !present {
		t.Fatalf("foreign parent authorized recorded delegated deletion: writes=%d err=%v", writes, err)
	}
	native.alias["br-l2"] = proof
	if err := child.Reconcile(t.Context()); err != nil || writes != 2 || present {
		t.Fatalf("recorded Bridge proof could not withdraw delegated address: writes=%d err=%v", writes, err)
	}
}

func TestBridgeParentRAProofAndRecordedWithdrawal(t *testing.T) {
	parent, native, store := bridgeChildParentFixture(t)
	parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"},
		Metadata: api.ObjectMeta{Name: "bridge-ra"},
		Spec:     api.IPv6RouterAdvertisementSpec{Interface: "legacy-l2", Prefix: "2001:db8:7::/64"},
	})
	service := NewRouterAdvertisementService(t.Context(), false)
	service.bridges = &parent
	service.store = store
	service.declared = parent.Router
	service.scope = parent.NativeScope
	service.clock = func() time.Time { return time.Now().UTC() }
	prepared, err := prepareRAPlans(parent.Router)
	if err != nil {
		t.Fatal(err)
	}
	service.prepared = prepared
	service.plans, err = selectRAPlans(prepared, parent.Router)
	if err != nil {
		t.Fatal(err)
	}
	socket := &raCapturedSocket{}
	endpoint := ra.Endpoint{Conn: socket, Source: netip.MustParseAddr("fe80::1"), Index: native.links["br-l2"].Index, MTU: 1370}
	if err := service.transmit(t.Context(), "br-l2", endpoint, false); err != nil || len(socket.packets) != 1 {
		t.Fatalf("owned Bridge parent rejected advertisement: packets=%d err=%v", len(socket.packets), err)
	}
	journal, err := readRAJournal(store)
	if err != nil || !strings.HasPrefix(journal["br-l2"].ParentToken, "bridge:") {
		t.Fatalf("advertisement did not retain actual Bridge proof: %+v %v", journal, err)
	}
	proof := native.alias["br-l2"]
	native.alias["br-l2"] = "operator"
	if err := service.transmit(t.Context(), "br-l2", endpoint, false); err == nil || len(socket.packets) != 1 {
		t.Fatalf("foreign Bridge parent emitted advertisement: packets=%d err=%v", len(socket.packets), err)
	}
	service.plans = map[string]raInterfacePlan{}
	service.declared = &api.Router{}
	if err := service.transmit(t.Context(), "br-l2", endpoint, true); err == nil || len(socket.packets) != 1 {
		t.Fatalf("foreign Bridge parent emitted withdrawal: packets=%d err=%v", len(socket.packets), err)
	}
	native.alias["br-l2"] = proof
	if err := service.transmit(t.Context(), "br-l2", endpoint, true); err != nil {
		t.Fatal(err)
	}
	prefixes := raWirePrefixes(t, socket.packets[len(socket.packets)-1])
	if len(socket.packets) != 2 || len(prefixes) != 1 || prefixes["2001:db8:7::/64"] != (raWirePrefix{}) {
		t.Fatalf("recorded Bridge withdrawal is not exact: prefixes=%+v packets=%d", prefixes, len(socket.packets))
	}
}
