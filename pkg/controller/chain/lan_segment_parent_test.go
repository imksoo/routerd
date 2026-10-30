// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	state "github.com/imksoo/routerd/pkg/state"
)

type lanPendingParentFault struct {
	*state.SQLiteStore
	afterPending func()
}

func (s *lanPendingParentFault) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	if err := s.SQLiteStore.SaveObjectStatus(version, kind, name, status); err != nil {
		return err
	}
	if kind == "IPv6DelegatedAddress" && status["reason"] == "AddressApplying" && s.afterPending != nil {
		s.afterPending()
	}
	return nil
}

func TestLANRequiresActualSegmentParentBeforeAcquisition(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign", true: "owned"}[owned], func(t *testing.T) {
			store, router, c, _ := sqliteNativeLANFixture(t)
			parent, native, _, _ := segmentOwnerFixture(t)
			parent.Store = store
			router.Spec.Resources[0] = parent.Router.Spec.Resources[2]
			spec, _ := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
			spec.Interface = "segment"
			router.Spec.Resources[2].Spec = spec
			router.Spec.Resources = append(router.Spec.Resources, parent.Router.Spec.Resources[0], parent.Router.Spec.Resources[1])
			parent.Router = router
			if owned {
				if err := parent.Reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
			} else {
				native.link.Exists = true
				native.link.Index = 8
				native.link.Token = "foreign"
				native.link.Kind = "vxlan"
				native.link.Config.IfName = "vxlan100"
			}
			if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "segment", map[string]any{"phase": "Up"}); err != nil {
				t.Fatal(err)
			}
			c.Segments = &parent
			present := false
			writes := 0
			c.ObserveAddress = func(context.Context, string, string, bool) (delegatedAddressObservation, error) {
				return delegatedAddressObservation{ifIndex: native.link.Index, scope: native.scope, present: present, usable: present}, nil
			}
			c.Command = func(context.Context, string, ...string) error { writes++; present = true; return nil }
			err := c.Reconcile(t.Context())
			if !owned {
				if err == nil || writes != 0 {
					t.Fatalf("foreign parent admitted child write: writes=%d err=%v", writes, err)
				}
				return
			}
			if err != nil || writes != 1 {
				t.Fatalf("owned parent not used: writes=%d err=%v", writes, err)
			}
			status, _ := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
			receipt, err := delegatedReceipt(status)
			if err != nil || receipt == nil || receipt.ParentToken != native.link.Token || !receipt.Addresses[0].Held {
				t.Fatalf("parent cookie not durably bound: %+v %v", receipt, err)
			}
			// A child receipt cannot authorize a same-name, same-index replacement.
			native.link.Token = "foreign-replacement"
			c.Router = &api.Router{}
			c.DeclaredRouter = c.Router
			c.Command = func(context.Context, string, ...string) error {
				t.Fatal("removed child deleted address on replaced parent")
				return nil
			}
			if err := c.Reconcile(t.Context()); err == nil {
				t.Fatal("foreign recorded parent was accepted during removed child cleanup")
			}
			status, _ = store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
			if receipt, err := delegatedReceipt(status); err != nil || receipt == nil || len(receipt.Addresses) != 1 {
				t.Fatalf("unknown parent cleanup lost child receipt: %+v %v", receipt, err)
			}
			// Declaration removal still uses the recorded, genuinely owned
			// parent when it is present; it does not require desired activation.
			native.link.Token = receipt.ParentToken
			c.Command = func(context.Context, string, ...string) error { writes++; present = false; return nil }
			if err := c.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address"); err != nil || status != nil || present || writes != 2 {
				t.Fatalf("recorded parent withdrawal incomplete: %+v %v present=%t writes=%d", status, err, present, writes)
			}
		})
	}
}

func TestLANRechecksSegmentParentAfterDurablePending(t *testing.T) {
	store, router, c, _ := sqliteNativeLANFixture(t)
	parent, native, _, _ := segmentOwnerFixture(t)
	parent.Store = store
	router.Spec.Resources[0] = parent.Router.Spec.Resources[2]
	spec, _ := router.Spec.Resources[2].IPv6DelegatedAddressSpec()
	spec.Interface = "segment"
	router.Spec.Resources[2].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, parent.Router.Spec.Resources[0], parent.Router.Spec.Resources[1])
	parent.Router = router
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "segment", map[string]any{"phase": "Up"}); err != nil {
		t.Fatal(err)
	}
	fault := &lanPendingParentFault{SQLiteStore: store, afterPending: func() { native.link.Token = "replacement" }}
	c.Store = fault
	c.Segments = &parent
	c.ObserveAddress = func(context.Context, string, string, bool) (delegatedAddressObservation, error) {
		return delegatedAddressObservation{ifIndex: native.link.Index, scope: native.scope}, nil
	}
	c.Command = func(context.Context, string, ...string) error {
		t.Fatal("parent replaced during persistence still admitted write")
		return nil
	}
	if err := c.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "VXLAN") {
		t.Fatalf("missing replacement rejection: %v", err)
	}
	status, _ := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, err := delegatedReceipt(status)
	if err != nil || receipt == nil || receipt.Addresses[0].Held || receipt.Addresses[0].Pending == nil {
		t.Fatalf("pending operation lost or fabricated: %+v %v", receipt, err)
	}
}

func TestLANRechecksNativeTupleAfterDurablePending(t *testing.T) {
	store, _, c, _ := sqliteNativeLANFixture(t)
	present := false
	c.Store = &lanPendingParentFault{SQLiteStore: store, afterPending: func() { present = true }}
	c.ObserveAddress = lanProbeFixture(func(context.Context, string, string) bool { return present }, nil, nil)
	c.Command = func(context.Context, string, ...string) error {
		t.Fatal("concurrent foreign tuple was overwritten")
		return nil
	}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("concurrent native address was accepted")
	}
	status, _ := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-address")
	receipt, err := delegatedReceipt(status)
	if err != nil || receipt == nil || receipt.Addresses[0].Held {
		t.Fatalf("unconfirmed tuple acquired: %+v %v", receipt, err)
	}
}
