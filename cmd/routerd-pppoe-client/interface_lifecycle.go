// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
)

var pppoeInterfaceTeardownTimeout = 5 * time.Second

var pppoeInterfaceProbeInterval = 50 * time.Millisecond

func pppoeInterfaceExists(ctx context.Context, osName platform.OS, ifname string) (bool, error) {
	switch osName {
	case platform.OSFreeBSD:
		return freeBSDPPPoEInterfaceExists(ctx, ifname)
	case platform.OSLinux:
		return linuxPPPoEInterfaceExists(ctx, ifname)
	default:
		return false, nil
	}
}

// waitForOwnedPPPoEInterfaceTeardown holds lifecycle completion until the
// kernel removes the exact interface derived for this resource. Child exit is
// insufficient on both supported OS paths because interface teardown is async.
func waitForOwnedPPPoEInterfaceTeardown(osName platform.OS, ifname string) error {
	deadline := time.NewTimer(pppoeInterfaceTeardownTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pppoeInterfaceProbeInterval)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), freeBSDPPPoEObserveTimeout)
		exists, err := pppoeInterfaceExists(ctx, osName, ifname)
		cancel()
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		select {
		case <-deadline.C:
			return fmt.Errorf("managed PPPoE child exited but owned interface %q did not disappear within %s", ifname, pppoeInterfaceTeardownTimeout)
		case <-ticker.C:
		}
	}
}
