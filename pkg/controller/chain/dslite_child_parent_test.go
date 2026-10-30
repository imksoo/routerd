// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	ra "github.com/imksoo/routerd/pkg/routeradvertisement"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestDSLiteForeignParentCannotAcquireStaticAddress(t *testing.T) {
	parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
	native.links["ds-owned"] = dsliteTunnelObservation{Exists: true, Index: 8, Kind: "ip6tnl", MTU: 1454}
	parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "ds", Address: "192.0.2.1/24"}})
	present, writes := false, 0
	child := IPv4StaticAddressController{Router: parent.Router, Store: store, DSLite: &parent, ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
		snapshot := staticAddressSnapshot{Index: 8, Scope: "linux:boot-a:17"}
		if present {
			snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
		}
		return snapshot, nil
	}, Command: func(context.Context, string, ...string) error { writes++; present = true; return nil }}
	err := child.Reconcile(t.Context())
	if writes != 0 || present {
		t.Fatalf("foreign DS-Lite parent received child address: writes=%d err=%v", writes, err)
	}
}

func TestDSLiteParentStaticAddressProofAndRecordedCleanup(t *testing.T) {
	for _, scenario := range []string{"owned", "pending", "foreign-after-save", "removed", "removed-foreign", "removed-missing-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
			if err := parent.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "ds", Address: "192.0.2.1/24"}})
			db := &staticAddressFaultStore{SQLiteStore: store.SQLiteStore}
			if scenario == "pending" {
				records, _ := parent.nativeTunnelReceipts()
				records[0].Pending = true
				if err := parent.saveNativeTunnel(records[0], false); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "foreign-after-save" {
				db.afterPending = func() { link := native.links["ds-owned"]; link.Token = "foreign"; native.links["ds-owned"] = link }
			}
			present, adds, deletes := false, 0, 0
			child := IPv4StaticAddressController{Router: parent.Router, Store: db, DSLite: &parent, ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
				snapshot := staticAddressSnapshot{Index: native.links["ds-owned"].Index, Scope: "linux:boot-a:17"}
				if present {
					snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
				}
				return snapshot, nil
			}, Command: func(_ context.Context, _ string, args ...string) error {
				if strings.Contains(strings.Join(args, " "), "del") {
					deletes++
					present = false
				} else {
					adds++
					present = true
				}
				return nil
			}}
			err := child.Reconcile(t.Context())
			if scenario == "pending" || scenario == "foreign-after-save" {
				if err == nil || adds != 0 {
					t.Fatalf("unproved parent mutated: adds=%d err=%v", adds, err)
				}
				return
			}
			if err != nil || adds != 1 {
				t.Fatalf("owned parent rejected: adds=%d err=%v", adds, err)
			}
			status, _ := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "child")
			receipt, err := staticReceipt(status)
			if err != nil || receipt == nil || !strings.HasPrefix(receipt.ParentToken, "dslite:") {
				t.Fatalf("missing concrete proof: %+v %v", receipt, err)
			}
			if scenario == "owned" {
				return
			}
			child.Router = &api.Router{}
			if scenario == "removed-foreign" {
				link := native.links["ds-owned"]
				link.Token = "foreign"
				native.links["ds-owned"] = link
			}
			if scenario == "removed-missing-receipt" {
				records, _ := parent.nativeTunnelReceipts()
				if err := parent.saveNativeTunnel(records[0], true); err != nil {
					t.Fatal(err)
				}
			}
			err = child.Reconcile(t.Context())
			if scenario == "removed" {
				if err != nil || deletes != 1 || present {
					t.Fatalf("recorded cleanup rejected: deletes=%d err=%v", deletes, err)
				}
			} else if err == nil || deletes != 0 || !present {
				t.Fatalf("foreign cleanup authorized: deletes=%d err=%v", deletes, err)
			}
		})
	}
}

func TestDSLiteParentRecheckedAfterNativeAddressWait(t *testing.T) {
	parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
	if err := parent.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	original := parent.CommandOutput
	parent.CommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := original(ctx, name, args...)
		if strings.Join(args, " ") == "-j -4 addr show dev ds-owned" {
			link := native.links["ds-owned"]
			link.Token = "foreign"
			native.links["ds-owned"] = link
		}
		return out, err
	}
	reader := nativeParentReader{Router: parent.Router, Store: store, DSLite: &parent}
	link, err := reader.observeDeclared(t.Context(), "ds-owned")
	if err == nil || link.Exists {
		t.Fatalf("parent changed during inner-address observation admitted child: %+v %v", link, err)
	}
}

func TestDSLiteParentRouteProofAndRecordedCleanup(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, scenario := range []string{"foreign", "owned", "changed-during-save", "removed-foreign"} {
		t.Run(scenario, func(t *testing.T) {
			parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
			if err := parent.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, ordinaryRouteTestRouter("ds-owned").Spec.Resources...)
			fault := &routeOwnerTestStore{SQLiteStore: store.SQLiteStore}
			replace := func() { link := native.links["ds-owned"]; link.Token = "foreign"; native.links["ds-owned"] = link }
			if scenario == "foreign" {
				replace()
			}
			if scenario == "changed-during-save" {
				fault.afterSave = func(status map[string]any) {
					if status["phase"] == "Applying" {
						replace()
					}
				}
			}
			commands := &routeOwnerCommandFixture{t: t, output: `[]`, afterApply: ordinaryRouteNativeResponse("ds-owned", "boot")}
			child := IPv4RouteController{Router: parent.Router, Store: fault, DSLite: &parent, Command: commands.run, NativeScope: parent.NativeScope, DeviceLookup: func(context.Context, string) (int, bool, error) { return native.links["ds-owned"].Index, true, nil }}
			err := child.reconcile(t.Context())
			if scenario == "foreign" || scenario == "changed-during-save" {
				if err == nil || commands.output != `[]` {
					t.Fatalf("foreign parent route acquired: %v calls=%v", err, commands.calls)
				}
				return
			}
			receipt := readOrdinaryRouteReceipt(t, fault)
			if err != nil || receipt == nil || !strings.HasPrefix(receipt.ParentToken, "dslite:") {
				t.Fatalf("route parent proof absent: %+v %v", receipt, err)
			}
			child.Router = &api.Router{}
			if scenario == "removed-foreign" {
				replace()
			}
			err = child.reconcile(t.Context())
			if scenario == "owned" {
				if err != nil || commands.output != `[]` {
					t.Fatalf("recorded route cleanup failed: %v", err)
				}
			} else if err == nil || commands.output == `[]` {
				t.Fatalf("foreign parent route deleted: %v calls=%v", err, commands.calls)
			}
		})
	}
}

func TestDSLiteParentDelegatedLANProofAndRecordedCleanup(t *testing.T) {
	for _, scenario := range []string{"foreign", "owned", "changed-during-save"} {
		t.Run(scenario, func(t *testing.T) {
			store, router, child, _ := sqliteNativeLANFixture(t)
			parent, native, _ := newDSLiteNativeController(t, platform.OSLinux)
			parent.Store = store
			native.store = func() Store { return store }
			if err := parent.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			router.Spec.Resources[0] = parent.Router.Spec.Resources[1]
			spec, _ := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
			spec.Interface = "ds"
			router.Spec.Resources[2].Spec = spec
			router.Spec.Resources = append(router.Spec.Resources, parent.Router.Spec.Resources[0])
			parent.Router = router
			child.DSLite = &parent
			if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "ds", map[string]any{"phase": "Up"}); err != nil {
				t.Fatal(err)
			}
			replace := func() { link := native.links["ds-owned"]; link.Token = "foreign"; native.links["ds-owned"] = link }
			if scenario == "foreign" {
				replace()
			}
			if scenario == "changed-during-save" {
				child.Store = &lanPendingParentFault{SQLiteStore: store, afterPending: replace}
			}
			present, writes := false, 0
			child.ObserveAddress = func(context.Context, string, string, bool) (delegatedAddressObservation, error) {
				return delegatedAddressObservation{ifIndex: native.links["ds-owned"].Index, scope: "linux:boot-a:17", present: present, usable: present}, nil
			}
			child.Command = func(_ context.Context, _ string, args ...string) error {
				writes++
				present = !strings.Contains(strings.Join(args, " "), "del")
				return nil
			}
			err := child.Reconcile(t.Context())
			if scenario != "owned" {
				if err == nil || writes != 0 {
					t.Fatalf("unproved parent admitted delegated address: writes=%d err=%v", writes, err)
				}
				return
			}
			if err != nil || writes != 1 {
				t.Fatalf("owned parent rejected: writes=%d err=%v", writes, err)
			}
			status, _ := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
			receipt, err := delegatedReceipt(status)
			if err != nil || receipt == nil || !strings.HasPrefix(receipt.ParentToken, "dslite:") {
				t.Fatalf("LAN parent proof absent: %+v %v", receipt, err)
			}
			token := native.links["ds-owned"].Token
			replace()
			child.Router = &api.Router{}
			child.DeclaredRouter = child.Router
			if err := child.Reconcile(t.Context()); err == nil || writes != 1 || !present {
				t.Fatalf("foreign parent admitted LAN withdrawal: writes=%d err=%v", writes, err)
			}
			link := native.links["ds-owned"]
			link.Token = token
			native.links["ds-owned"] = link
			if err := child.Reconcile(t.Context()); err != nil || writes != 2 || present {
				t.Fatalf("recorded parent LAN cleanup failed: writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestDSLiteParentProofUsesBothNativeDrivers(t *testing.T) {
	for _, osName := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
		t.Run(string(osName), func(t *testing.T) {
			parent, native, store := newDSLiteNativeController(t, osName)
			if err := parent.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			records, err := parent.nativeTunnelReceipts()
			if err != nil || len(records) != 1 {
				t.Fatalf("receipt: %+v %v", records, err)
			}
			writes := len(native.writes)
			reader := nativeParentReader{Router: parent.Router, Store: store, DSLite: &parent}
			link, err := reader.observeDeclared(t.Context(), records[0].Plan.Name)
			if err != nil || !link.Exists || link.Index != records[0].IfIndex || link.Token != "dslite:"+records[0].Token {
				t.Fatalf("native parent proof: %+v %v", link, err)
			}
			if len(native.writes) != writes {
				t.Fatal("read-only child proof mutated the parent")
			}
			native.failLinkRead = true
			if _, err := reader.observeDeclared(t.Context(), records[0].Plan.Name); err == nil {
				t.Fatal("failed native read admitted child")
			}
		})
	}
}

func TestDSLiteParentRAProofAndRecordedWithdrawal(t *testing.T) {
	for _, scenario := range []string{"foreign", "owned", "changed-during-save"} {
		t.Run(scenario, func(t *testing.T) {
			parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
			if err := parent.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			router := parent.Router
			router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "ds-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "ds", Prefix: "2001:db8:7::/64"}})
			service := NewRouterAdvertisementService(t.Context(), false)
			service.dslite = &parent
			service.scope = parent.NativeScope
			service.clock = func() time.Time { return time.Now().UTC() }
			service.store = store
			service.declared = router
			prepared, err := prepareRAPlans(router)
			if err != nil {
				t.Fatal(err)
			}
			service.plans, err = selectRAPlans(prepared, router)
			if err != nil {
				t.Fatal(err)
			}
			service.prepared = prepared
			socket := &raCapturedSocket{}
			endpoint := ra.Endpoint{Conn: socket, Source: netip.MustParseAddr("fe80::1"), Index: native.links["ds-owned"].Index, MTU: 1454}
			replace := func() { link := native.links["ds-owned"]; link.Token = "foreign"; native.links["ds-owned"] = link }
			token := native.links["ds-owned"].Token
			if scenario == "foreign" {
				replace()
			}
			if scenario == "changed-during-save" {
				service.store = &raPendingParentFault{SQLiteStore: store.SQLiteStore, afterPending: replace}
			}
			err = service.transmit(t.Context(), "ds-owned", endpoint, false)
			if scenario != "owned" {
				if err == nil || len(socket.packets) != 0 {
					t.Fatalf("unproved parent emitted RA: packets=%d err=%v", len(socket.packets), err)
				}
				return
			}
			if err != nil || len(socket.packets) != 1 {
				t.Fatalf("owned parent RA rejected: %v", err)
			}
			journal, err := readRAJournal(store)
			if err != nil || journal["ds-owned"].ParentToken != "dslite:"+token {
				t.Fatalf("RA parent proof absent: %+v %v", journal, err)
			}
			service.plans = map[string]raInterfacePlan{}
			service.declared = &api.Router{}
			replace()
			if err := service.transmit(t.Context(), "ds-owned", endpoint, true); err == nil || len(socket.packets) != 1 {
				t.Fatalf("foreign parent emitted withdrawal: %v", err)
			}
			link := native.links["ds-owned"]
			link.Token = token
			native.links["ds-owned"] = link
			if err := service.transmit(t.Context(), "ds-owned", endpoint, true); err != nil {
				t.Fatal(err)
			}
			if prefixes := raWirePrefixes(t, socket.packets[len(socket.packets)-1]); len(socket.packets) != 2 || prefixes["2001:db8:7::/64"] != (raWirePrefix{}) {
				t.Fatalf("recorded parent withdrawal wrong: %+v packets=%d", prefixes, len(socket.packets))
			}
		})
	}
}
