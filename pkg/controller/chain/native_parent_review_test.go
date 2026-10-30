// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"net/netip"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/vxlan"
)

type parentReviewSegmentWait struct {
	vxlan.SegmentNative
	reads     int
	afterRead func()
}

func (n *parentReviewSegmentWait) ObserveOwned(ctx context.Context, config vxlan.SegmentConfig, identity vxlan.SegmentIdentity) (vxlan.SegmentLink, error) {
	observed, err := n.SegmentNative.ObserveOwned(ctx, config, identity)
	n.reads++
	if err == nil && n.reads == 2 {
		n.afterRead()
	}
	return observed, err
}

func TestVXLANChildRejectsParentWithdrawalDuringFinalNativeRead(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	parent, native, store, _ := segmentOwnerFixture(t)
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
		Metadata: api.ObjectMeta{Name: "child"},
		Spec:     api.IPv4StaticAddressSpec{Interface: "segment", Address: "192.0.2.10/24"},
	})
	changed := false
	wait := &parentReviewSegmentWait{SegmentNative: native, afterRead: func() {
		// The real parent owner persists withdrawal before stopping/removing
		// its link. Its old native readback can still be Up during that wait.
		owner := oneSegmentOwner(t, parent)
		owner.Withdrawing = true
		if err := parent.saveOwner(owner); err != nil {
			t.Fatal(err)
		}
		changed = true
	}}
	parent.Native = wait
	present, writes := false, 0
	child := IPv4StaticAddressController{Router: parent.Router, Store: store, Segments: &parent,
		ObserveAddresses: func(context.Context, string) (staticAddressSnapshot, error) {
			observed := staticAddressSnapshot{Index: native.link.Index, Scope: native.scope}
			if present {
				observed.Addresses = []netip.Prefix{netip.MustParsePrefix("192.0.2.10/24")}
			}
			return observed, nil
		},
		Command: func(context.Context, string, ...string) error { writes++; present = true; return nil },
	}
	if err := child.Reconcile(t.Context()); err == nil || !changed || writes != 0 || present {
		t.Fatalf("parent withdrawal during final native read authorized child mutation: changed=%t writes=%d present=%t err=%v", changed, writes, present, err)
	}
}
