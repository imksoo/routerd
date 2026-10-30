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
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/vxlan"
)

type staticAddressFaultStore struct {
	*routerstate.SQLiteStore
	failApplied  bool
	afterPending func()
}

func (s *staticAddressFaultStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if s.failApplied && kind == "IPv4StaticAddress" && status["phase"] == "Applied" {
		return errors.New("applied publication unavailable")
	}
	if err := s.SQLiteStore.SaveObjectStatus(version, kind, name, status); err != nil {
		return err
	}
	if receipt, err := staticReceipt(status); err == nil && receipt != nil && receipt.Pending && s.afterPending != nil {
		s.afterPending()
	}
	return nil
}

func TestStaticAddressRechecksRoleAfterPendingWrite(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	setRole := func(role string) {
		if err := db.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "gateway", map[string]any{"role": role}); err != nil {
			t.Fatal(err)
		}
	}
	setRole("master")
	router := staticOwnerRouter()
	spec, err := router.Spec.Resources[1].IPv4StaticAddressSpec()
	if err != nil {
		t.Fatal(err)
	}
	spec.When = api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.role": {Equals: "master"}}}
	router.Spec.Resources[1].Spec = spec
	store := &staticAddressFaultStore{SQLiteStore: db, afterPending: func() { setRole("backup") }}
	controller := IPv4StaticAddressController{Router: router, Store: store,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			return staticAddressSnapshot{Index: 10, Scope: "fixture-kernel"}, nil
		},
		Command: func(context.Context, string, ...string) error {
			t.Fatal("role changed during durable write, but native acquisition still ran")
			return nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil || status["reason"] != "WhenFalse" || status["nativeReceipt"] != nil {
		t.Fatalf("aborted acquisition retained authority: %#v %v", status, err)
	}
}

func TestStaticAddressRechecksNativeIdentityAfterPendingWrite(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	index := 10
	store := &staticAddressFaultStore{SQLiteStore: db, afterPending: func() { index = 11 }}
	controller := IPv4StaticAddressController{Router: staticOwnerRouter(), Store: store,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			return staticAddressSnapshot{Index: index, Scope: "fixture-kernel"}, nil
		},
		Command: func(context.Context, string, ...string) error {
			t.Fatal("native acquisition ran on the replacement interface")
			return nil
		},
	}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("interface replacement during journal write was ignored")
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil || status["reason"] != "AcquisitionNotStarted" || status["nativeReceipt"] != nil {
		t.Fatalf("unstarted operation retained native authority: %#v %v", status, err)
	}
}

func TestStaticAddressReplacementMissingStillWithdrawsRevokedIdentity(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	router := staticOwnerRouter()
	router.Spec.Resources[0].Spec = api.InterfaceSpec{IfName: "next0"}
	present := true
	controller := IPv4StaticAddressController{Router: router, Store: db,
		ObserveAddresses: func(_ context.Context, ifname string) (staticAddressSnapshot, error) {
			if ifname == "next0" {
				return staticAddressSnapshot{}, nil
			}
			if ifname != "test0" {
				t.Fatalf("unexpected interface %s", ifname)
			}
			result := staticAddressSnapshot{Index: 10, Scope: "fixture-kernel"}
			if present {
				result.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return result, nil
		},
		Command: func(_ context.Context, _ string, args ...string) error {
			if !strings.Contains(strings.Join(args, " "), "del 192.0.2.1/24") {
				t.Fatalf("unexpected command %v", args)
			}
			present = false
			return nil
		},
	}
	if err := controller.saveStaticAddress("address", map[string]any{"phase": "Applied"}, &staticAddressReceipt{IfName: "test0", IfIndex: 10, Scope: "fixture-kernel", Address: "192.0.2.1/24"}); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil || present || status["reason"] != "InterfaceNotPresent" || status["nativeReceipt"] != nil {
		t.Fatalf("revoked identity retained: present=%v status=%#v err=%v", present, status, err)
	}
}

func staticOwnerRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "test0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "address"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24"}},
	}}}
}

func TestStaticAddressPendingSurvivesPublicationFailureAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	store := &staticAddressFaultStore{SQLiteStore: db, failApplied: true}
	present := false
	calls := 0
	controller := IPv4StaticAddressController{Router: staticOwnerRouter(), Store: store,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			observation := staticAddressSnapshot{Index: 10, Scope: "fixture-kernel"}
			if present {
				observation.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return observation, nil
		},
		Command: func(_ context.Context, command string, args ...string) error {
			status, err := store.SQLiteStore.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := staticReceipt(status)
			if err != nil || receipt == nil || !receipt.Pending || receipt.IfIndex != 10 {
				t.Fatalf("effect preceded durable pending identity: %#v, %v", receipt, err)
			}
			calls++
			present = !strings.Contains(strings.Join(args, " "), "del")
			return nil
		},
	}
	if err := controller.Reconcile(t.Context()); err == nil || !present || calls != 1 {
		t.Fatalf("publication failure lost the effect premise: err=%v present=%v calls=%d", err, present, calls)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store.SQLiteStore, store.failApplied = db, false
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("matching presence was treated as proof of an interrupted acquisition")
	}
	if !present || calls != 1 {
		t.Fatalf("restart mutated an address with unknown ownership: present=%v calls=%d", present, calls)
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if saved, decodeErr := staticReceipt(status); err != nil || decodeErr != nil || saved == nil || !saved.Pending {
		t.Fatalf("ambiguous acquisition lost its fence: %#v %v %v", status, err, decodeErr)
	}
	// An operator resolves the ambiguous native tuple. Confirmed absence is
	// enough to retire the pending receipt without an automatic delete.
	present = false
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err = db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil || len(status) != 0 {
		t.Fatalf("completed removal retained status: %#v %v", status, err)
	}
}

func TestStaticAddressNeverAcquiresOrWithdrawsForeignDeclaration(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	controller := IPv4StaticAddressController{Router: staticOwnerRouter(), Store: db,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			return staticAddressSnapshot{Index: 10, Scope: "fixture-kernel", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}}, nil
		}, Command: func(context.Context, string, ...string) error {
			t.Fatal("foreign address was mutated")
			return nil
		},
	}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("foreign address was adopted")
	}
	// Even an old display status with the exact declaration is not authority.
	if err := db.SaveObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address", map[string]any{"phase": "Applied", "ifname": "test0", "address": "192.0.2.1/24"}); err != nil {
		t.Fatal(err)
	}
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestStaticAddressLostWithdrawalReplyFencesMatchingPresence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	present, calls := true, 0
	controller := IPv4StaticAddressController{Router: &api.Router{}, Store: db,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			snapshot := staticAddressSnapshot{Index: 10, Scope: "fixture-kernel"}
			if present {
				snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
			}
			return snapshot, nil
		},
		Command: func(context.Context, string, ...string) error {
			calls++
			status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
			receipt, decodeErr := staticReceipt(status)
			if err != nil || decodeErr != nil || receipt == nil || !receipt.Pending {
				t.Fatalf("withdrawal preceded its durable fence: %#v %v %v", status, err, decodeErr)
			}
			// The command may have succeeded and another writer may have
			// recreated the same tuple before we received its lost reply.
			return errors.New("withdrawal acknowledgement lost")
		},
	}
	receipt := &staticAddressReceipt{IfName: "test0", IfIndex: 10, Scope: "fixture-kernel", Address: "192.0.2.1/24"}
	if err := controller.saveStaticAddress("address", map[string]any{"phase": "Applied"}, receipt); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context()); err == nil || calls != 1 {
		t.Fatalf("lost withdrawal was acknowledged: calls=%d err=%v", calls, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	controller.Store = db
	if err := controller.Reconcile(t.Context()); err == nil || calls != 1 {
		t.Fatalf("restart retried a possibly foreign withdrawal: calls=%d err=%v", calls, err)
	}
	present = false
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil || len(status) != 0 || calls != 1 {
		t.Fatalf("confirmed absence did not retire the receipt: %#v calls=%d err=%v", status, calls, err)
	}
}

func TestStaticAddressReadFailureRetainsReceiptAndForeignReplacementSurvives(t *testing.T) {
	db, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	receipt := &staticAddressReceipt{IfName: "test0", IfIndex: 10, Scope: "fixture-kernel", Address: "192.0.2.1/24"}
	controller := IPv4StaticAddressController{Router: &api.Router{}, Store: db,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			return staticAddressSnapshot{}, errors.New("native read failed")
		}, Command: func(context.Context, string, ...string) error {
			t.Fatal("unknown or foreign address was mutated")
			return nil
		},
	}
	if err := controller.saveStaticAddress("address", map[string]any{"phase": "Applied"}, receipt); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("native read failure became absence")
	}
	status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
	if err != nil {
		t.Fatal(err)
	}
	if preserved, err := staticReceipt(status); err != nil || preserved == nil || *preserved != *receipt {
		t.Fatalf("failed observation lost receipt: %#v %v", preserved, err)
	}
	controller.ObserveAddresses = func(context.Context, string) (staticAddressSnapshot, error) {
		return staticAddressSnapshot{Index: 11, Scope: "fixture-kernel", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}}, nil
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestStaticAddressRequiresItsActualSegmentParent(t *testing.T) {
	for _, scenario := range []string{"unacquired", "foreign", "changed-during-write", "owned"} {
		t.Run(scenario, func(t *testing.T) {
			parent, native, db, _ := segmentOwnerFixture(t)
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "address"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "segment", Address: "192.0.2.1/24"},
			})
			if scenario == "owned" || scenario == "changed-during-write" {
				if err := parent.Reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "foreign" {
				// A native link can exist before acquisition or with a foreign
				// cookie. Neither a declaration nor its name admits a child add.
				native.link = vxlan.SegmentLink{Exists: true, Scope: native.scope, Index: 8, Token: "foreign", Kind: "vxlan", Config: vxlan.SegmentConfig{IfName: "vxlan100"}}
			}
			store := &staticAddressFaultStore{SQLiteStore: db}
			if scenario == "changed-during-write" {
				store.afterPending = func() { native.link.Token = "foreign-replacement" }
			}
			present, effects := false, 0
			child := IPv4StaticAddressController{Router: parent.Router, Store: store, Segments: &parent,
				ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
					snapshot := staticAddressSnapshot{Scope: native.scope, Index: native.link.Index}
					if present {
						snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
					}
					return snapshot, nil
				},
				Command: func(context.Context, string, ...string) error { effects++; present = true; return nil },
			}
			err := child.Reconcile(t.Context())
			if scenario == "owned" {
				if err != nil || effects != 1 || !present {
					t.Fatalf("owned Segment child not acquired: effects=%d err=%v", effects, err)
				}
			} else if effects != 0 || present {
				t.Fatalf("unowned Segment received an address: effects=%d err=%v", effects, err)
			}
			if scenario == "changed-during-write" && err == nil {
				t.Fatal("parent cookie change during pending save was ignored")
			}
			if scenario == "foreign" {
				if err := parent.Reconcile(t.Context()); err == nil {
					t.Fatal("parent adopted the foreign link")
				}
			}
		})
	}
}

func TestStaticAddressRemovedSegmentKeepsExactCleanupAuthority(t *testing.T) {
	for _, scenario := range []string{"recorded-down", "foreign", "missing-record"} {
		t.Run(scenario, func(t *testing.T) {
			parent, native, db, _ := segmentOwnerFixture(t)
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
				Metadata: api.ObjectMeta{Name: "address"},
				Spec:     api.IPv4StaticAddressSpec{Interface: "segment", Address: "192.0.2.1/24"},
			})
			if err := parent.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			present, deletes := false, 0
			child := IPv4StaticAddressController{Router: parent.Router, Store: db, Segments: &parent,
				ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
					snapshot := staticAddressSnapshot{Scope: native.scope, Index: native.link.Index}
					if present {
						snapshot.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}
					}
					return snapshot, nil
				},
				Command: func(_ context.Context, _ string, args ...string) error {
					if strings.Contains(strings.Join(args, " "), "del") {
						deletes++
						present = false
					} else {
						present = true
					}
					return nil
				},
			}
			if err := child.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			status, err := db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
			receipt, decodeErr := staticReceipt(status)
			if err != nil || decodeErr != nil || receipt == nil || receipt.ParentToken != native.link.Token {
				t.Fatalf("parent not retained: %+v %v %v", receipt, err, decodeErr)
			}
			child.Router, parent.Router = &api.Router{}, &api.Router{}
			native.link.Up, native.link.Running = false, false
			if scenario == "foreign" {
				native.link.Token = "foreign"
			}
			if scenario == "missing-record" {
				owner := oneSegmentOwner(t, parent)
				if err := parent.removeOwner(owner.Resource); err != nil {
					t.Fatal(err)
				}
			}
			err = child.Reconcile(t.Context())
			if scenario == "recorded-down" {
				if err != nil || deletes != 1 || present {
					t.Fatalf("owned removed parent cleanup failed: deletes=%d present=%t err=%v", deletes, present, err)
				}
			} else {
				if err == nil || deletes != 0 || !present {
					t.Fatalf("unproven parent was changed: deletes=%d present=%t err=%v", deletes, present, err)
				}
				status, err = db.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
				retained, decodeErr := staticReceipt(status)
				if err != nil || decodeErr != nil || retained == nil || retained.ParentToken != receipt.ParentToken {
					t.Fatalf("unknown parent lost child receipt: %+v %v %v", retained, err, decodeErr)
				}
			}
		})
	}
}
