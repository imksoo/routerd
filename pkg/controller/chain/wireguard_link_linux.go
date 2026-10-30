// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"errors"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
)

// The caller must have this exact index from its successful exclusive create.
// This is an acknowledged sequence, not an atomic CAS against external writers.
func setWireGuardAliasByIndex(index int, name, proof string) error {
	if index < 1 || name == "" || proof == "" {
		return errors.New("invalid acknowledged WireGuard creation identity")
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
	if link == nil || link.Attrs() == nil || link.Attrs().Index != index || link.Attrs().Name != name || link.Type() != "wireguard" || link.Attrs().Alias != "" {
		return errors.New("WireGuard acknowledged identity changed before cookie installation")
	}
	if err := handle.LinkSetAlias(link, proof); err != nil {
		return err
	}
	link, err = handle.LinkByIndex(index)
	if err != nil {
		return err
	}
	if link == nil || link.Attrs() == nil || link.Attrs().Index != index || link.Attrs().Name != name || link.Type() != "wireguard" || link.Attrs().Alias != proof {
		return errors.New("WireGuard cookie installation has no exact native readback")
	}
	return nil
}
