// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/vxlan"
)

func TestOrdinaryRouteRequiresActualSegmentParent(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, scenario := range []string{"unacquired", "foreign", "pending", "changed-during-write", "different-index", "different-scope", "owned"} {
		t.Run(scenario, func(t *testing.T) {
			parent, native, db, _ := segmentOwnerFixture(t)
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, ordinaryRouteTestRouter("vxlan100").Spec.Resources...)
			if scenario == "unacquired" {
				// A same-name native interface exists without this parent's
				// durable receipt. A declaration or device index cannot adopt it.
				native.link = vxlan.SegmentLink{Exists: true, Scope: native.scope, Index: 8, Token: "foreign", Kind: "vxlan", Config: vxlan.SegmentConfig{IfName: "vxlan100"}}
			} else if err := parent.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if scenario == "foreign" {
				native.link.Token = "foreign-replacement"
			}
			if scenario == "pending" {
				owner := oneSegmentOwner(t, parent)
				owner.Identity.Pending = true
				if err := parent.saveOwner(owner); err != nil {
					t.Fatal(err)
				}
			}
			store := &routeOwnerTestStore{SQLiteStore: db}
			if scenario == "changed-during-write" {
				store.afterSave = func(status map[string]any) {
					if status["phase"] == "Applying" {
						native.link.Token = "foreign-replacement"
					}
				}
			}
			commands := &routeOwnerCommandFixture{t: t, output: `[]`, afterApply: ordinaryRouteNativeResponse("vxlan100", "boot")}
			child := IPv4RouteController{Router: parent.Router, Store: store, Segments: &parent, Command: commands.run,
				NativeScope: func(context.Context) (string, error) {
					if scenario == "different-scope" {
						return "another-kernel", nil
					}
					return native.scope, nil
				},
				DeviceLookup: func(context.Context, string) (int, bool, error) {
					index := native.link.Index
					if scenario == "different-index" {
						index++
					}
					return index, true, nil
				},
			}
			err := child.reconcile(t.Context())
			if scenario == "owned" {
				if err != nil || readOrdinaryRouteReceipt(t, store) == nil || commands.output != commands.afterApply {
					t.Fatalf("owned parent rejected: err=%v calls=%v", err, commands.calls)
				}
				return
			}
			if err == nil {
				t.Fatal("unproven parent admitted a route")
			}
			for _, call := range commands.calls {
				if strings.HasPrefix(call, "ip route add ") || strings.HasPrefix(call, "ip route del ") {
					t.Fatalf("unproven parent reached native mutation: %s", call)
				}
			}
			if commands.output != `[]` {
				t.Fatal("unproven parent received a route")
			}
		})
	}
}

func TestOrdinaryRouteSegmentReplacementCannotWithdrawKnownRoute(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	parent, native, db, _ := segmentOwnerFixture(t)
	parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, ordinaryRouteTestRouter("vxlan100").Spec.Resources...)
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	store := &routeOwnerTestStore{SQLiteStore: db}
	commands := &routeOwnerCommandFixture{t: t, output: `[]`, afterApply: ordinaryRouteNativeResponse("vxlan100", "boot")}
	child := IPv4RouteController{Router: parent.Router, Store: store, Segments: &parent, Command: commands.run,
		NativeScope:  func(context.Context) (string, error) { return native.scope, nil },
		DeviceLookup: func(context.Context, string) (int, bool, error) { return native.link.Index, true, nil },
	}
	if err := child.reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	native.link.Token = "foreign-replacement"
	// Removal of both declarations must not discard the persisted parent token.
	child.Router = &api.Router{}
	commands.calls = nil
	if err := child.reconcile(t.Context()); err == nil {
		t.Fatal("foreign parent replacement authorized native route withdrawal")
	}
	if commands.output != commands.afterApply || readOrdinaryRouteReceipt(t, store) == nil {
		t.Fatal("foreign parent replacement lost the route receipt or native effect")
	}
	for _, call := range commands.calls {
		if strings.HasPrefix(call, "ip route del ") {
			t.Fatalf("foreign parent received a route delete: %s", call)
		}
	}
}

func TestOrdinaryRouteRemovedSegmentUsesRecordedParentProof(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	for _, missingRecord := range []bool{false, true} {
		t.Run(map[bool]string{false: "recorded", true: "missing-record"}[missingRecord], func(t *testing.T) {
			parent, native, db, _ := segmentOwnerFixture(t)
			parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, ordinaryRouteTestRouter("vxlan100").Spec.Resources...)
			if err := parent.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			store := &routeOwnerTestStore{SQLiteStore: db}
			commands := &routeOwnerCommandFixture{t: t, output: `[]`, afterApply: ordinaryRouteNativeResponse("vxlan100", "boot")}
			child := IPv4RouteController{Router: parent.Router, Store: store, Segments: &parent, Command: commands.run,
				NativeScope:  func(context.Context) (string, error) { return native.scope, nil },
				DeviceLookup: func(context.Context, string) (int, bool, error) { return native.link.Index, true, nil },
			}
			if err := child.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if receipt := readOrdinaryRouteReceipt(t, store); receipt == nil || receipt.ParentToken != native.link.Token {
				t.Fatalf("route lost its durable parent token: %+v", receipt)
			}
			if missingRecord {
				if err := os.Remove(parent.receiptPath("segment")); err != nil {
					t.Fatal(err)
				}
			}
			// A down interface still has an exact native identity. Withdrawal
			// must use that identity without requiring current forwarding intent.
			native.link.Up, native.link.Running = false, false
			child.Router, parent.Router = &api.Router{}, &api.Router{}
			commands.calls = nil
			err := child.reconcile(t.Context())
			if missingRecord {
				if err == nil || commands.output != commands.afterApply || readOrdinaryRouteReceipt(t, store) == nil {
					t.Fatalf("missing native parent record authorized delete: %v %v", err, commands.calls)
				}
			} else if err != nil || commands.output != `[]` || readOrdinaryRouteReceipt(t, store) != nil {
				t.Fatalf("recorded removed parent could not withdraw its child: %v %v", err, commands.calls)
			}
		})
	}
}
