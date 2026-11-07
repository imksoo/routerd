// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWaitForIPv6AddressReadyWaitsThroughTentativeState(t *testing.T) {
	calls := 0
	ready := waitForIPv6AddressReady(t.Context(), "wan-vmac", "2001:db8::21/128", time.Second, func(_ context.Context, ifname, address string) bool {
		if ifname != "wan-vmac" || address != "2001:db8::21/128" {
			t.Fatalf("readback = %s %s", ifname, address)
		}
		calls++
		return calls >= 3
	})
	if !ready || calls != 3 {
		t.Fatalf("ready=%t calls=%d", ready, calls)
	}
}

func TestWaitForIPv6AddressReadyHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if waitForIPv6AddressReady(ctx, "wan-vmac", "2001:db8::21/128", time.Second, func(context.Context, string, string) bool { return false }) {
		t.Fatal("cancelled wait reported ready")
	}
}

func containsExactLine(text, want string) bool {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == want {
			return true
		}
	}
	return false
}
