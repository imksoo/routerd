// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
)

const pppoeInterfaceTeardownTimeout = 5 * time.Second

const pppoeInterfaceProbeInterval = 50 * time.Millisecond

func pppoeInterfaceExists(ctx context.Context, osName platform.OS, ifname string) (bool, error) {
	if osName != platform.OSLinux && osName != platform.OSFreeBSD {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return false, fmt.Errorf("list interfaces for PPPoE teardown: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	for _, iface := range interfaces {
		if iface.Name == ifname {
			return true, nil
		}
	}
	return false, nil
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
