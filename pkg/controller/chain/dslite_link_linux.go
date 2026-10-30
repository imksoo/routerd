// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"errors"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
)

// The index comes from the caller's successful exclusive create request.
func setDSLiteAliasByIndex(index int, name, proof string) error {
	if index < 1 || name == "" || proof == "" {
		return errors.New("invalid acknowledged DS-Lite creation identity")
	}
	handle, err := netlink.NewHandle(syscall.NETLINK_ROUTE)
	if err != nil {
		return err
	}
	defer handle.Close()
	if err := handle.SetSocketTimeout(2 * time.Second); err != nil {
		return err
	}
	link, err := handle.LinkByIndex(index)
	if err != nil {
		return err
	}
	if link == nil || link.Attrs() == nil || link.Attrs().Index != index || link.Attrs().Name != name || link.Type() != "ip6tnl" || link.Attrs().Alias != "" {
		return errors.New("DS-Lite acknowledged identity changed before cookie installation")
	}
	if err := handle.LinkSetAlias(link, proof); err != nil {
		return err
	}
	link, err = handle.LinkByIndex(index)
	if err != nil {
		return err
	}
	if link == nil || link.Attrs() == nil || link.Attrs().Index != index || link.Attrs().Name != name || link.Type() != "ip6tnl" || link.Attrs().Alias != proof {
		return errors.New("DS-Lite cookie installation has no exact native readback")
	}
	return nil
}
