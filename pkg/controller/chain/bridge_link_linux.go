// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"fmt"
	"github.com/vishvananda/netlink"
	"syscall"
	"time"
)

func bridgeNetlinkHandle() (*netlink.Handle, error) {
	handle, err := netlink.NewHandle(syscall.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	if err := handle.SetSocketTimeout(2 * time.Second); err != nil {
		handle.Close()
		return nil, err
	}
	return handle, nil
}

func ownedBridgeByIndex(handle *netlink.Handle, index int, proof string) (netlink.Link, error) {
	link, err := handle.LinkByIndex(index)
	if err != nil {
		return nil, err
	}
	if link == nil || link.Attrs() == nil || link.Attrs().Index != index || link.Type() != "bridge" || link.Attrs().Alias != proof {
		return nil, fmt.Errorf("Bridge ifindex %d no longer identifies the owned native link", index)
	}
	return link, nil
}

func setBridgeAdminByIndex(index int, proof string, up bool) error {
	handle, err := bridgeNetlinkHandle()
	if err != nil {
		return err
	}
	defer handle.Close()
	link, err := ownedBridgeByIndex(handle, index, proof)
	if err != nil {
		return err
	}
	if up {
		return handle.LinkSetUp(link)
	}
	return handle.LinkSetDown(link)
}

func deleteBridgeByIndex(index int, proof string) error {
	handle, err := bridgeNetlinkHandle()
	if err != nil {
		return err
	}
	defer handle.Close()
	link, err := ownedBridgeByIndex(handle, index, proof)
	if err != nil {
		return err
	}
	return handle.LinkDel(link)
}

func setBridgeMasterByIndex(port bridgePort, bridgeIndex int, proof string) error {
	handle, err := bridgeNetlinkHandle()
	if err != nil {
		return err
	}
	defer handle.Close()
	bridge, err := ownedBridgeByIndex(handle, bridgeIndex, proof)
	if err != nil {
		return err
	}
	member, err := handle.LinkByIndex(port.IfIndex)
	if err != nil {
		return err
	}
	if member == nil || member.Attrs() == nil || member.Attrs().Index != port.IfIndex || member.Attrs().Name != port.IfName {
		return fmt.Errorf("Bridge member %s identity changed", port.IfName)
	}
	if master := member.Attrs().MasterIndex; master != 0 && master != bridgeIndex {
		return fmt.Errorf("Bridge member %s has a foreign master", port.IfName)
	}
	return handle.LinkSetMaster(member, bridge)
}
