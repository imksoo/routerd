// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package main

import "fmt"

func bindSocketToDevice(_ int, _ string) error {
	return fmt.Errorf("routerd-dhcpv4-client requires Linux SO_BINDTODEVICE; use the platform DHCPv4 path on this OS")
}
