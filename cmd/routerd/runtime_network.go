// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/binary"
	"net/netip"
)

func ipv6HostSuffix64(addr netip.Addr) uint64 {
	bytes := addr.As16()
	return binary.BigEndian.Uint64(bytes[8:])
}
