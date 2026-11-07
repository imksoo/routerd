// SPDX-License-Identifier: BSD-3-Clause
//go:build !linux

package routeradvertisement

import (
	"net"
	"net/netip"
)

func nativeLinkLocalSource(ifi *net.Interface) (netip.Addr, error) {
	addresses, err := ifi.Addrs()
	if err != nil {
		return netip.Addr{}, err
	}
	return linkLocalSource(addresses, ifi.Name)
}
