// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

import "fmt"

func setBridgeAliasByIndex(index int, name, proof string) error {
	return fmt.Errorf("native Bridge cookie installation is unsupported on this platform")
}

func setBridgeAdminByIndex(index int, proof string, up bool) error {
	return fmt.Errorf("native Bridge index mutation is unsupported on this platform")
}
func deleteBridgeByIndex(index int, proof string) error {
	return fmt.Errorf("native Bridge index deletion is unsupported on this platform")
}

func setBridgeMasterByIndex(port bridgePort, bridgeIndex int, proof string) error {
	return fmt.Errorf("native Bridge member mutation is unsupported on this platform")
}
