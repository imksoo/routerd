// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

func TestPendingWithdrawalReservesItsExactNativePathIdentity(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	ctx := context.Background()
	a := bgpdaemon.AppliedPath{Source: "MobilityPool/collision/node/n10365", Prefix: "10.77.60.11/32", Attrs: bgpdaemon.AppliedPathAttrs{MED: 100}}
	b := bgpdaemon.AppliedPath{Source: "MobilityPool/collision/node/n10440", Prefix: a.Prefix, Attrs: bgpdaemon.AppliedPathAttrs{MED: 200}}
	id := bgpdaemon.NativePathIdentifier(a.Source)
	if id != 1906234967 || bgpdaemon.NativePathIdentifier(b.Source) != id {
		t.Fatal("fixture must exercise two actual colliding native identifiers")
	}
	applied, installed, err := upsertDynamicPath(ctx, server, statePath, a)
	if err != nil {
		t.Fatal(err)
	}
	// This is the durable state after withdrawal intent is saved and before
	// GoBGP acknowledges DeletePath. The native A path still exists.
	applied.Paths = nil
	applied.PendingPathRemovals = []bgpdaemon.AppliedPath{*installed}
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		t.Fatal(err)
	}
	if _, _, err := upsertDynamicPath(ctx, server, statePath, b); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("pending A withdrawal allowed another native owner: %v", err)
	}
	if got := nativePathIdentities(t, server); len(got) != 1 || !slices.Contains(got, nativePathIdentity{identifier: id, med: 100}) {
		t.Fatalf("rejected B changed A's native path: %#v", got)
	}
	stored, found, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || !found || len(stored.PendingPathRemovals) != 1 || len(stored.PendingPathAdds) != 0 {
		t.Fatalf("rejected B changed the durable withdrawal: %#v found=%v err=%v", stored, found, err)
	}
	if _, err := deleteDynamicPath(ctx, server, statePath, a); err != nil {
		t.Fatal(err)
	}
	if got := nativePathIdentities(t, server); len(got) != 0 {
		t.Fatalf("A withdrawal left a native path: %#v", got)
	}
	if _, _, err := upsertDynamicPath(ctx, server, statePath, b); err != nil {
		t.Fatalf("completed A withdrawal still reserved its identifier: %v", err)
	}
	if got := nativePathIdentities(t, server); len(got) != 1 || !slices.Contains(got, nativePathIdentity{identifier: id, med: 200}) {
		t.Fatalf("B acquisition did not reach the actual RIB: %#v", got)
	}
}
