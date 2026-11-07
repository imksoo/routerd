// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package main

import "errors"

func inspectVMACNativeOwnership(vmac) error {
	return errors.New("VRRP macvlan ownership inspection is supported on Linux only")
}
