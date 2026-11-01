// SPDX-License-Identifier: BSD-3-Clause
//go:build freebsd

package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestKernelNetworkScopeUsesLiveJailAndHonorsCancellation(t *testing.T) {
	first, err := KernelNetworkScope(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := KernelNetworkScope(context.Background())
	if err != nil || first != second || !strings.HasPrefix(first, "freebsd:") || len(strings.Split(first, ":")) != 4 {
		t.Fatalf("unstable FreeBSD jail identity: %q %q %v", first, second, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := KernelNetworkScope(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled owner admission continued: %v", err)
	}
}
