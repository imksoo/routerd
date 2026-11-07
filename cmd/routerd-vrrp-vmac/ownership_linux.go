// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"time"
)

func inspectVMACNativeOwnership(entry vmac) error {
	handle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := handle.SetSocketTimeout(time.Second); err != nil {
		return err
	}
	parent, err := handle.LinkByName(entry.parent)
	if err != nil {
		return fmt.Errorf("inspect VMAC parent %s: %w", entry.parent, err)
	}
	link, err := handle.LinkByName(entry.ifname)
	if err != nil {
		return fmt.Errorf("inspect existing VMAC %s: %w", entry.ifname, err)
	}
	return validateVMACNativeOwnership(entry, parent.Attrs().Index, link)
}

func validateVMACNativeOwnership(entry vmac, parentIndex int, link netlink.Link) error {
	macvlan, ok := link.(*netlink.Macvlan)
	if !ok || macvlan.Attrs().Name != entry.ifname || parentIndex <= 0 || macvlan.Attrs().ParentIndex != parentIndex || macvlan.Mode != netlink.MACVLAN_MODE_PRIVATE {
		return fmt.Errorf("refuse VMAC %s: existing link is not private macvlan on declared parent %s", entry.ifname, entry.parent)
	}
	return nil
}
