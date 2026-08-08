// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// deleteVXLANLinkByIndex issues RTM_DELLINK for the validated kernel ifindex.
// It intentionally never resolves the interface name, so a same-name object
// created after the ownership observation cannot become the deletion target.
func deleteVXLANLinkByIndex(ifindex int) error {
	link, err := netlink.LinkByIndex(ifindex)
	if err != nil {
		return fmt.Errorf("resolve VXLAN ifindex %d: %w", ifindex, err)
	}
	if link == nil || link.Attrs() == nil || link.Attrs().Index != ifindex {
		return fmt.Errorf("VXLAN ifindex %d no longer identifies the owned link", ifindex)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete VXLAN ifindex %d: %w", ifindex, err)
	}
	return nil
}
