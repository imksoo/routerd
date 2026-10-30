// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package main

import (
	"fmt"
)

func listenDHCPv4(_ string) (dhcpv4PacketConn, error) {
	return nil, fmt.Errorf("routerd-dhcpv4-client is not implemented on this OS")
}
