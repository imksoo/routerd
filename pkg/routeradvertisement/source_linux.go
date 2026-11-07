// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package routeradvertisement

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func nativeLinkLocalSource(ifi *net.Interface) (netip.Addr, error) {
	addresses, err := netlink.AddrList(&netlink.Device{LinkAttrs: netlink.LinkAttrs{Index: ifi.Index}}, netlink.FAMILY_V6)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("observe RA source on %s: %w", ifi.Name, err)
	}
	tentative := false
	for _, address := range addresses {
		source, ok := netip.AddrFromSlice(address.IP)
		if !ok || !source.IsLinkLocalUnicast() {
			continue
		}
		if address.Flags&unix.IFA_F_DADFAILED != 0 {
			continue
		}
		if address.Flags&unix.IFA_F_TENTATIVE != 0 {
			tentative = true
			continue
		}
		return source.WithZone(ifi.Name), nil
	}
	if tentative {
		return netip.Addr{}, fmt.Errorf("RA source on %s: %w", ifi.Name, ErrSourceTentative)
	}
	return netip.Addr{}, fmt.Errorf("RA source on %s has no usable link-local address", ifi.Name)
}
