// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

import (
	"context"
	"errors"
)

func observeTunnelNativeLink(context.Context, string) (tunnelNativeLink, error) {
	return tunnelNativeLink{}, errors.New("TunnelInterface native ownership is only available on Linux")
}

func setTunnelNativeAlias(context.Context, int, string, string, string, string) error {
	return errors.New("TunnelInterface indexed cookie installation is only available on Linux")
}

func deleteTunnelNativeLink(context.Context, int, string, string, string, string) error {
	return errors.New("TunnelInterface indexed deletion is only available on Linux")
}

func applyTunnelNativePlan(context.Context, tunnelNativeReceipt) error {
	return errors.New("TunnelInterface indexed configuration is only available on Linux")
}
