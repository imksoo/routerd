// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestWireGuardForeignParentCannotAcquireStaticAddress(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "parent"}, Spec: api.WireGuardInterfaceSpec{IfName: "wg-foreign", PrivateKey: wgFixturePrivate}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "parent", Address: "192.0.2.1/24"}},
	}}}
	present, writes := false, 0
	child := IPv4StaticAddressController{Router: router, Store: store,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			observation := staticAddressSnapshot{Index: 7, Scope: "wg-fixture-kernel"}
			if present {
				observation.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return observation, nil
		},
		Command: func(context.Context, string, ...string) error { writes++; present = true; return nil },
	}
	err = child.Reconcile(t.Context())
	if writes != 0 || present {
		t.Fatalf("unacquired WireGuard parent admitted a child address: writes=%d err=%v", writes, err)
	}
}

func wireGuardChildFixture(t *testing.T) (WireGuardController, *wireGuardNativeFixture, *routerstate.SQLiteStore) {
	t.Helper()
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "parent"}, Spec: api.WireGuardInterfaceSpec{IfName: "wg-owned", PrivateKey: wgFixturePrivate}},
	}}}
	parent := WireGuardController{Router: router, Store: store, OS: platform.OSLinux}
	native := withWireGuardNativeFixture(t, &parent)
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	return parent, native, store
}

func TestWireGuardParentStaticAddressProofAndRecordedCleanup(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, scenario := range []string{"owned", "pending", "foreign-after-save", "source-after-save", "removed", "removed-foreign", "removed-missing-receipt", "removed-scope"} {
		t.Run(scenario, func(t *testing.T) {
			parent, native, db := wireGuardChildFixture(t)
			records, err := parent.readNativeWireGuard()
			if err != nil || len(records) != 1 || records[0].Pending || records[0].ConfigHash == "" {
				t.Fatalf("actual parent receipt: %+v %v", records, err)
			}
			record := records[0]
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "parent", Address: "192.0.2.1/24"}})
			if scenario == "pending" {
				pending := record
				pending.Pending = true
				if err := parent.saveNativeWireGuardRecord(pending); err != nil {
					t.Fatal(err)
				}
			}
			store := &staticAddressFaultStore{SQLiteStore: db}
			if scenario == "foreign-after-save" {
				store.afterPending = func() { link := native.links["wg-owned"]; link.Token = "operator"; native.links["wg-owned"] = link }
			} else if scenario == "source-after-save" {
				store.afterPending = func() {
					spec, _ := parent.Router.Spec.Resources[0].WireGuardInterfaceSpec()
					spec.ListenPort = 51820
					parent.Router.Spec.Resources[0].Spec = spec
				}
			}
			present, adds, deletes := false, 0, 0
			child := IPv4StaticAddressController{Router: parent.Router, Store: store, WireGuard: &parent,
				ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
					observed := staticAddressSnapshot{Index: native.links["wg-owned"].IfIndex, Scope: "wg-fixture-kernel"}
					if present {
						observed.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
					}
					return observed, nil
				}, Command: func(_ context.Context, _ string, args ...string) error {
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
			if scenario == "pending" || scenario == "foreign-after-save" || scenario == "source-after-save" {
				if err == nil || adds != 0 || present {
					t.Fatalf("unproved parent admitted child: adds=%d err=%v", adds, err)
				}
				return
			}
			status, readErr := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "child")
			receipt, decodeErr := staticReceipt(status)
			if err != nil || adds != 1 || readErr != nil || decodeErr != nil || receipt == nil || receipt.ParentToken != "wireguard:"+record.Token {
				t.Fatalf("owned parent proof missing: receipt=%+v adds=%d err=%v read=%v decode=%v", receipt, adds, err, readErr, decodeErr)
			}
			if scenario == "owned" {
				return
			}
			child.Router = &api.Router{}
			if scenario == "removed-foreign" {
				link := native.links["wg-owned"]
				link.Token = "operator"
				native.links["wg-owned"] = link
			} else if scenario == "removed-missing-receipt" {
				if err := db.DeleteObject(api.RouterAPIVersion, "Router", wireGuardNativeStatusName); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "removed-scope" {
				parent.NativeScope = func(context.Context) (string, error) { return "another-kernel", nil }
			}
			err = child.Reconcile(t.Context())
			if scenario == "removed" {
				if err != nil || deletes != 1 || present {
					t.Fatalf("recorded cleanup failed: deletes=%d present=%t err=%v", deletes, present, err)
				}
				return
			}
			if err == nil || deletes != 0 || !present {
				t.Fatalf("unproved cleanup mutated child: deletes=%d present=%t err=%v", deletes, present, err)
			}
			status, readErr = db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "child")
			receipt, decodeErr = staticReceipt(status)
			if readErr != nil || decodeErr != nil || receipt == nil || receipt.ParentToken != "wireguard:"+record.Token {
				t.Fatalf("blocked cleanup lost proof: %+v %v %v", receipt, readErr, decodeErr)
			}
		})
	}
}

func TestWireGuardParentRechecksReceiptAfterNativeWait(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	parent, _, store := wireGuardChildFixture(t)
	run := parent.Command
	reads := 0
	parent.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := run(ctx, name, args...)
		if name == "ip" && strings.Join(args, " ") == "-j -d link show" {
			reads++
			if reads == 2 {
				records, readErr := parent.readNativeWireGuard()
				if readErr != nil || len(records) != 1 {
					t.Fatalf("parent records: %+v %v", records, readErr)
				}
				records[0].Pending = true
				if err := parent.saveNativeWireGuardRecord(records[0]); err != nil {
					t.Fatal(err)
				}
			}
		}
		return out, err
	}
	reader := nativeParentReader{Router: parent.Router, Store: store, WireGuard: &parent}
	link, err := reader.observeDeclared(t.Context(), "wg-owned")
	if reads < 2 || err == nil || link.Exists {
		t.Fatalf("pending transition during native observation admitted a child: reads=%d link=%+v err=%v", reads, link, err)
	}
}

func TestWireGuardUnconfirmedCookieIsNotReplayedOrWithdrawn(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	parent := WireGuardController{Store: store, OS: platform.OSLinux}
	native := withWireGuardNativeFixture(t, &parent)
	install := parent.SetAliasByIndex
	attempts := 0
	parent.SetAliasByIndex = func(index int, name, proof string) error {
		attempts++
		if attempts == 1 {
			return errors.New("fixture cookie installation failure")
		}
		return install(index, name, proof)
	}
	if _, err := parent.ensureNativeWireGuard(t.Context(), "parent", "wg-owned"); err == nil {
		t.Fatal("unconfirmed cookie accepted")
	}
	if _, err := parent.ensureNativeWireGuard(t.Context(), "parent", "wg-owned"); err == nil || attempts != 1 {
		t.Fatalf("cookie setup replayed: attempts=%d err=%v", attempts, err)
	}
	if err := parent.cleanupNativeWireGuard(t.Context(), nil); err == nil {
		t.Fatal("unconfirmed native creation retired during removal")
	}
	records, err := parent.readNativeWireGuard()
	if err != nil || len(records) != 1 || !records[0].Pending || records[0].IfIndex != 0 || !native.links["wg-owned"].Exists || native.links["wg-owned"].Token != "" {
		t.Fatalf("unconfirmed native creation lost: records=%+v err=%v", records, err)
	}
}

func TestWireGuardChildRechecksSourceAfterLastNativeWait(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	parent, native, store := wireGuardChildFixture(t)
	parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
		Metadata: api.ObjectMeta{Name: "child"},
		Spec:     api.IPv4StaticAddressSpec{Interface: "parent", Address: "192.0.2.1/24"},
	})
	run := parent.Command
	reads := 0
	parent.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		output, err := run(ctx, name, args...)
		if name == "ip" && strings.Join(args, " ") == "-j -d link show" {
			reads++
			// The first proof precedes the child's pending write. Change the
			// source during the last native read of its second proof.
			if reads == 6 {
				spec, err := parent.Router.Spec.Resources[0].WireGuardInterfaceSpec()
				if err != nil {
					t.Fatal(err)
				}
				spec.ListenPort = 51820
				parent.Router.Spec.Resources[0].Spec = spec
			}
		}
		return output, err
	}
	present, writes := false, 0
	child := IPv4StaticAddressController{Router: parent.Router, Store: store, WireGuard: &parent,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			observed := staticAddressSnapshot{Index: native.links["wg-owned"].IfIndex, Scope: "wg-fixture-kernel"}
			if present {
				observed.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return observed, nil
		},
		Command: func(context.Context, string, ...string) error { writes++; present = true; return nil },
	}
	if err := child.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "source changed while observing") || reads != 6 || writes != 0 || present {
		t.Fatalf("source change during final parent read authorized child write: reads=%d writes=%d present=%t err=%v", reads, writes, present, err)
	}
}

func TestWireGuardOwnedDownInterfaceRecoversWithoutReplacement(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	parent, native, store := wireGuardChildFixture(t)
	before := native.links["wg-owned"]
	down := before
	down.Up = false
	native.links["wg-owned"] = down
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := native.links["wg-owned"]
	if !after.Up || after.IfIndex != before.IfIndex || after.Token != before.Token {
		t.Fatalf("owned Down interface was not restored under the same native identity: before=%+v after=%+v", before, after)
	}
	reader := nativeParentReader{Router: parent.Router, Store: store, WireGuard: &parent}
	if proof, err := reader.observeDeclared(t.Context(), "wg-owned"); err != nil || !proof.Exists || proof.Index != before.IfIndex || proof.Token != "wireguard:"+before.Token {
		t.Fatalf("restored WireGuard parent could not authorize a child: proof=%+v err=%v", proof, err)
	}
}
