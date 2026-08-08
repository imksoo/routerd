// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

import "fmt"

func deleteVXLANLinkByIndex(ifindex int) error {
	return fmt.Errorf("VXLAN ifindex deletion is unsupported on this platform: %d", ifindex)
}
