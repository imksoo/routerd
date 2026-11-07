// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"context"
	"errors"
	"testing"
)

func TestCanceledNativeFIBSyncReturnsBeforeKernelEffects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	syncer := defaultFIBSyncer()
	if _, err := syncer.SyncBGP(ctx, nil, []FIBRoute{{Prefix: "10.77.60.11/32", NextHops: []string{"192.0.2.2"}}}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled FIB sync=%v", err)
	}
}
