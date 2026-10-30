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
	parent, native, store := newDSLiteNativeController(t, platform.CurrentOS())
	ifname := interfaceIfName(parent.Router, "ds")
	scope, _ := parent.NativeScope(t.Context())
	kind := "ip6tnl"
	if parent.currentOS() == platform.OSFreeBSD {
		kind = "gif"
	}
	native.links[ifname] = dsliteTunnelObservation{Exists: true, Index: 8, Kind: kind, MTU: 1454}
	parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "ds", Address: "192.0.2.1/24"}})
	present, writes := false, 0
	child := IPv4StaticAddressController{Router: parent.Router, Store: store, DSLite: &parent, ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
		snapshot := staticAddressSnapshot{Index: 8, Scope: scope}
		if present {
			snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
		}
		return snapshot, nil
	}, Command: func(context.Context, string, ...string) error { writes++; present = true; return nil }}
	err := child.Reconcile(t.Context())
	if err == nil || writes != 0 || present {
		t.Fatalf("foreign DS-Lite parent received child address: writes=%d err=%v", writes, err)
	}
}

func TestDSLiteParentStaticAddressProofAndRecordedCleanup(t *testing.T) {
	for _, scenario := range []string{"owned", "pending", "foreign-after-save", "removed", "removed-foreign", "removed-missing-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			parent, native, store := newDSLiteNativeController(t, platform.CurrentOS())
			ifname := interfaceIfName(parent.Router, "ds")
			scope, _ := parent.NativeScope(t.Context())
			if err := parent.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "ds", Address: "192.0.2.1/24"}})
			db := &staticAddressFaultStore{SQLiteStore: store.SQLiteStore}
			if scenario == "pending" {
				records, _ := parent.nativeTunnelReceipts()
				previous := records[0]
				records[0].Pending = true
				if err := parent.saveNativeTunnel(&previous, records[0], false); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "foreign-after-save" {
				db.afterPending = func() { link := native.links[ifname]; link.Token = "foreign"; native.links[ifname] = link }
			}
			present, adds, deletes := false, 0, 0
			child := IPv4StaticAddressController{Router: parent.Router, Store: db, DSLite: &parent, ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
				snapshot := staticAddressSnapshot{Index: native.links[ifname].Index, Scope: scope}
				if present {
					snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
				}
				return snapshot, nil
			}, Command: func(_ context.Context, _ string, args ...string) error {
				line := strings.Join(args, " ")
				if strings.Contains(line, "del") || strings.Contains(line, "-alias") {
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
				link := native.links[ifname]
				link.Token = "foreign"
				native.links[ifname] = link
			}
			if scenario == "removed-missing-receipt" {
				records, _ := parent.nativeTunnelReceipts()
				if err := parent.saveNativeTunnel(&records[0], records[0], true); err != nil {
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
			parent, native, _ := newDSLiteNativeController(t, platform.CurrentOS())
			ifname := interfaceIfName(parent.Router, "ds")
			scope, _ := parent.NativeScope(t.Context())
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
			replace := func() { link := native.links[ifname]; link.Token = "foreign"; native.links[ifname] = link }
			if scenario == "foreign" {
				replace()
			}
			if scenario == "changed-during-save" {
				child.Store = &lanPendingParentFault{SQLiteStore: store, afterPending: replace}
			}
			present, writes := false, 0
			child.ObserveAddress = func(context.Context, string, string, bool) (delegatedAddressObservation, error) {
				return delegatedAddressObservation{ifIndex: native.links[ifname].Index, scope: scope, present: present, usable: present, lifetimeKnown: present, preferredSeconds: 90, validSeconds: 120}, nil
			}
			child.Command = func(_ context.Context, _ string, args ...string) error {
				writes++
				line := strings.Join(args, " ")
				present = !strings.Contains(line, "del") && !strings.Contains(line, "-alias")
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
			token := native.links[ifname].Token
			replace()
			child.Router = &api.Router{}
			child.DeclaredRouter = child.Router
			if err := child.Reconcile(t.Context()); err == nil || writes != 1 || !present {
				t.Fatalf("foreign parent admitted LAN withdrawal: writes=%d err=%v", writes, err)
			}
			link := native.links[ifname]
			link.Token = token
			native.links[ifname] = link
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
			parent, native, store := newDSLiteNativeController(t, platform.CurrentOS())
			ifname := interfaceIfName(parent.Router, "ds")
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
			endpoint := ra.Endpoint{Conn: socket, Source: netip.MustParseAddr("fe80::1"), Index: native.links[ifname].Index, MTU: 1454}
			replace := func() { link := native.links[ifname]; link.Token = "foreign"; native.links[ifname] = link }
			token := native.links[ifname].Token
			if scenario == "foreign" {
				replace()
			}
			if scenario == "changed-during-save" {
				service.store = &raPendingParentFault{SQLiteStore: store.SQLiteStore, afterPending: replace}
			}
			err = service.transmit(t.Context(), ifname, endpoint, false)
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
			if err != nil || journal[ifname].ParentToken != "dslite:"+token {
				t.Fatalf("RA parent proof absent: %+v %v", journal, err)
			}
			service.plans = map[string]raInterfacePlan{}
			service.declared = &api.Router{}
			replace()
			if err := service.transmit(t.Context(), ifname, endpoint, true); err == nil || len(socket.packets) != 1 {
				t.Fatalf("foreign parent emitted withdrawal: %v", err)
			}
			link := native.links[ifname]
			link.Token = token
			native.links[ifname] = link
			if err := service.transmit(t.Context(), ifname, endpoint, true); err != nil {
				t.Fatal(err)
			}
			if prefixes := raWirePrefixes(t, socket.packets[len(socket.packets)-1]); len(socket.packets) != 2 || prefixes["2001:db8:7::/64"] != (raWirePrefix{}) {
				t.Fatalf("recorded parent withdrawal wrong: %+v packets=%d", prefixes, len(socket.packets))
			}
		})
	}
}

func TestDSLiteParentRechecksConditionAfterLastNativeRead(t *testing.T) {
	parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
	spec, _ := parent.Router.Spec.Resources[1].DSLiteTunnelSpec()
	spec.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.role": {Equals: "master"}}}
	parent.Router.Spec.Resources[1].Spec = spec
	setRole := func(role string) {
		if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "gateway", map[string]any{"role": role}); err != nil {
			t.Fatal(err)
		}
	}
	setRole("master")
	if err := parent.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	original, reads := native.command, 0
	parent.CommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := original(ctx, name, args...)
		if strings.Join(args, " ") == "-j -d link show" {
			reads++
			if reads == 2 {
				setRole("backup")
			}
		}
		return out, err
	}
	reader := nativeParentReader{Router: parent.Router, Store: store, DSLite: &parent}
	if link, err := reader.observeDeclared(t.Context(), "ds-owned"); err == nil || link.Exists {
		t.Fatalf("condition changed during final native read admitted child: %+v %v", link, err)
	}
}

func TestDSLiteParentRechecksCookieAfterKernelScopeWait(t *testing.T) {
	parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
	if err := parent.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	original, reads := parent.NativeScope, 0
	parent.NativeScope = func(ctx context.Context) (string, error) {
		scope, err := original(ctx)
		reads++
		if reads == 3 {
			link := native.links["ds-owned"]
			link.Token = "foreign"
			native.links["ds-owned"] = link
		}
		return scope, err
	}
	reader := nativeParentReader{Router: parent.Router, Store: store, DSLite: &parent}
	if link, err := reader.observeDeclared(t.Context(), "ds-owned"); err == nil || link.Exists {
		t.Fatalf("cookie changed during scope observation admitted child: %+v %v", link, err)
	}
}

func TestDSLiteParentMissingChildTokenCannotAuthorizeStaticWithdrawal(t *testing.T) {
	parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
	if err := parent.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	present := true
	receipt := &staticAddressReceipt{IfName: "ds-owned", IfIndex: native.links["ds-owned"].Index, Scope: "linux:boot-a:17", Address: "192.0.2.1/24"}
	child := IPv4StaticAddressController{Router: parent.Router, Store: store, DSLite: &parent, ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
		return staticAddressSnapshot{Index: receipt.IfIndex, Scope: receipt.Scope, Addresses: []netip.Prefix{netip.MustParsePrefix(receipt.Address)}}, nil
	}, Command: func(context.Context, string, ...string) error { present = false; return nil }}
	if err := child.withdrawStaticAddress(t.Context(), "child", receipt); err == nil || !present {
		t.Fatalf("receipt without parent proof authorized deletion: %v", err)
	}
}

func TestDSLiteFreeBSDConfiguredNameCannotBypassActualParent(t *testing.T) {
	parent, native, store := newDSLiteNativeController(t, platform.OSFreeBSD)
	if err := parent.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	reader := nativeParentReader{Router: parent.Router, Store: store, DSLite: &parent}
	proof, err := reader.observeDeclared(t.Context(), "ds-owned")
	if err != nil || !proof.Declared || !proof.Exists || proof.IfName != "gif919" {
		t.Fatalf("configured alias skipped actual parent: %+v %v", proof, err)
	}
	link := native.links["gif919"]
	link.Token = "foreign"
	native.links["gif919"] = link
	if proof, err := reader.observeDeclared(t.Context(), "ds-owned"); err == nil || proof.Exists {
		t.Fatalf("configured alias admitted foreign parent: %+v %v", proof, err)
	}
}

func TestDSLiteWhenMaskedParentCannotBecomeBareNativeDevice(t *testing.T) {
	parent, native, store := newDSLiteNativeController(t, platform.CurrentOS())
	ifname := interfaceIfName(parent.Router, "ds")
	scope, _ := parent.NativeScope(t.Context())
	spec, _ := parent.Router.Spec.Resources[1].DSLiteTunnelSpec()
	spec.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.role": {Equals: "master"}}}
	parent.Router.Spec.Resources[1].Spec = spec
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "gateway", map[string]any{"role": "backup"}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{Router: parent.Router, Store: store}
	view, err := runner.effectiveDynamicRouteSAMViewForReconcile(eventedStore{Store: store}, time.Now().UTC(), platform.CurrentOS())
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range view.RouteRouter.Spec.Resources {
		if resource.Kind == "DSLiteTunnel" {
			t.Fatal("fixture did not mask parent")
		}
	}
	kind := "ip6tnl"
	if parent.currentOS() == platform.OSFreeBSD {
		kind = "gif"
	}
	native.links[ifname] = dsliteTunnelObservation{Exists: true, Index: 8, Kind: kind, MTU: 1454}
	route := IPv4RouteController{Router: view.RouteRouter, ParentRouter: view.ParentRouter, Store: store, DSLite: &parent}
	if token, err := route.ordinaryRouteNativeParent(t.Context(), ifname, scope, 8); err == nil {
		t.Fatalf("masked parent admitted bare route device: token=%q", token)
	}
	address := IPv4StaticAddressController{Router: view.RouteRouter, ParentRouter: view.ParentRouter, Store: store, DSLite: &parent}
	if token, err := address.staticNativeParent(t.Context(), ifname, staticAddressSnapshot{Index: 8, Scope: scope}); err == nil {
		t.Fatalf("masked parent admitted bare address device: token=%q", token)
	}
}

func TestDSLiteParentRechecksReceiptAfterLastNativeRead(t *testing.T) {
	parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
	if err := parent.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	original, reads := native.command, 0
	parent.CommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := original(ctx, name, args...)
		if strings.Join(args, " ") == "-j -d link show" {
			reads++
			if reads == 2 {
				records, readErr := parent.nativeTunnelReceipts()
				if readErr != nil {
					t.Fatal(readErr)
				}
				previous := records[0]
				records[0].Pending = true
				if saveErr := parent.saveNativeTunnel(&previous, records[0], false); saveErr != nil {
					t.Fatal(saveErr)
				}
			}
		}
		return out, err
	}
	reader := nativeParentReader{Router: parent.Router, Store: store, DSLite: &parent}
	if link, err := reader.observeDeclared(t.Context(), "ds-owned"); err == nil || link.Exists {
		t.Fatalf("parent began withdrawal during final native read but admitted child: %+v %v", link, err)
	}
}
