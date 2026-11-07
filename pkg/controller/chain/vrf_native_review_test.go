// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"slices"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestVRFWithdrawalPreservesNativeWhenReceiptChangesDuringFinalObserve(t *testing.T) {
	owner, native := vrfTestOwner(t)
	if err := owner.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	record := vrfOneReceipt(t, owner)
	link := native.links[record.Plan.Name]
	before := len(native.ops)
	changed := false
	var replacement vrfReceipt
	native.afterObserve = func(name string) {
		if name != record.Plan.Name || changed {
			return
		}
		current := vrfOneReceipt(t, owner)
		if !current.Withdrawing {
			return
		}
		// The native read has captured its result but has not returned. Revoke
		// the saved withdrawal through the actual SQLite persistence boundary.
		replacement = current
		replacement.Withdrawing = false
		if err := owner.saveReceipts([]vrfReceipt{replacement}); err != nil {
			t.Fatal(err)
		}
		changed = true
	}
	owner.Router = &api.Router{}
	err := owner.Reconcile(t.Context())
	if !changed {
		t.Fatal("fixture did not change the receipt during the final withdrawal observation")
	}
	if slices.Contains(native.ops[before:], "delete") || native.links[record.Plan.Name] != link || native.links["vrf-port"].Master != record.Index {
		t.Fatalf("revoked withdrawal deleted native ownership before detecting the receipt change: operations=%v link=%+v err=%v", native.ops[before:], native.links[record.Plan.Name], err)
	}
	if err == nil {
		t.Fatal("changed withdrawal receipt was accepted")
	}
	if !sameVRFReceipt(vrfOneReceipt(t, owner), replacement) {
		t.Fatal("withdrawal overwrote the replacement receipt")
	}
}
