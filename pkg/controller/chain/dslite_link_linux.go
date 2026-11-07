// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"errors"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
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

func deleteDSLiteByIndex(ctx context.Context, index int, name, proof, scope string) error {
	if index < 1 || name == "" || proof == "" || scope == "" {
		return errors.New("invalid DS-Lite deletion identity")
	}
	current, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return err
	}
	if current != scope {
		return errors.New("DS-Lite deletion belongs to another kernel scope")
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
	var absent netlink.LinkNotFoundError
	if errors.As(err, &absent) {
		return nil
	}
	if err != nil {
		return err
	}
	if link == nil || link.Attrs() == nil || link.Attrs().Index != index || link.Attrs().Name != name || link.Type() != "ip6tnl" || link.Attrs().Alias != proof {
		return errors.New("DS-Lite native identity changed before indexed deletion")
	}
	current, err = platform.KernelNetworkScope(ctx)
	if err != nil {
		return err
	}
	if current != scope {
		return errors.New("DS-Lite kernel scope changed before indexed deletion")
	}
	return handle.LinkDel(link)
}
