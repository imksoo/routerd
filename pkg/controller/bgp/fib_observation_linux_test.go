// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

func TestCanceledNativeFIBSyncReturnsBeforeKernelEffects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	syncer := &netlinkFIBSyncer{}
	if _, err := syncer.SyncBGP(ctx, []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"192.0.2.2"}}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled FIB sync=%v", err)
	}
	if syncer.installed != nil || syncer.retainOnMissing != nil {
		t.Fatal("canceled operation altered its applied ownership")
	}
}

func TestPreferredSourceUsesTheCapturedAddressObservation(t *testing.T) {
	observed := []localIPv4Address{{Address: netip.MustParseAddr("192.0.2.1"), Prefix: netip.MustParsePrefix("192.0.2.0/24")}}
	if !preferredSourceIsLocal("192.0.2.1", observed) || preferredSourceIsLocal("192.0.2.2", observed) {
		t.Fatal("preferred source ignored the supplied complete address snapshot")
	}
}
