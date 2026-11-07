// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

import "errors"

func setWireGuardAliasByIndex(int, string, string) error {
	return errors.New("WireGuard indexed cookie installation is only available on Linux")
}
