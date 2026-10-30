// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/vishvananda/netlink"
)

func tunnelNativeHandle() (*netlink.Handle, error) {
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

func observeTunnelNativeLink(ctx context.Context, name string) (tunnelNativeLink, error) {
	scope, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return tunnelNativeLink{}, err
	}
	result := tunnelNativeLink{Scope: scope}
	handle, err := tunnelNativeHandle()
	if err != nil {
		return result, err
	}
	defer handle.Close()
	link, err := handle.LinkByName(name)
	var absent netlink.LinkNotFoundError
	if errors.As(err, &absent) {
		current, scopeErr := platform.KernelNetworkScope(ctx)
		if scopeErr != nil || current != scope {
			return result, errors.Join(scopeErr, errors.New("TunnelInterface scope changed during absence observation"))
		}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	addresses, err := handle.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return result, err
	}
	fresh, err := handle.LinkByName(name)
	if err != nil {
		return result, err
	}
	if fresh.Attrs() == nil || link.Attrs() == nil || fresh.Attrs().Name != name || fresh.Attrs().Index != link.Attrs().Index || fresh.Type() != link.Type() || fresh.Attrs().Alias != link.Attrs().Alias {
		return result, errors.New("TunnelInterface identity changed during address observation")
	}
	current, err := platform.KernelNetworkScope(ctx)
	if err != nil || current != scope {
		return result, errors.Join(err, errors.New("TunnelInterface scope changed during observation"))
	}
	result.Exists, result.Index, result.Kind = true, fresh.Attrs().Index, fresh.Type()
	if token, owned := strings.CutPrefix(fresh.Attrs().Alias, "routerd-tunnel:"); owned {
		result.Token = token
	}
	result.Observed = tunnelObserved{Exists: true, Up: fresh.Attrs().Flags&net.FlagUp != 0, MTU: fresh.Attrs().MTU}
	switch tunnel := fresh.(type) {
	case *netlink.Iptun:
		result.Observed.Mode = "ipip"
		switch netlink.TunnelEncapType(tunnel.EncapType) {
		case netlink.None:
		case netlink.FOU:
			result.Observed.Mode = "fou"
		case netlink.GUE:
			result.Observed.Mode = "gue"
		default:
			return result, errors.New("unsupported TunnelInterface native encapsulation")
		}
		result.Observed.Local, result.Observed.Remote = tunnel.Local.String(), tunnel.Remote.String()
		result.Observed.TTL, result.Observed.EncapSport, result.Observed.EncapDport = int(tunnel.Ttl), int(tunnel.EncapSport), int(tunnel.EncapDport)
	case *netlink.Gretun:
		if tunnel.IKey != tunnel.OKey {
			return result, errors.New("TunnelInterface has different native GRE input/output keys")
		}
		result.Observed.Mode = "gre"
		result.Observed.Local, result.Observed.Remote = tunnel.Local.String(), tunnel.Remote.String()
		result.Observed.TTL, result.Observed.Key = int(tunnel.Ttl), int(tunnel.IKey)
	}
	for _, address := range addresses {
		if address.IPNet != nil {
			result.Addresses = append(result.Addresses, address.IPNet.String())
		}
	}
	return result, ctx.Err()
}

func checkedTunnelNativeLink(ctx context.Context, handle *netlink.Handle, index int, name, kind, proof, scope string) (netlink.Link, error) {
	if index < 1 || name == "" || kind == "" || scope == "" {
		return nil, errors.New("invalid TunnelInterface mutation identity")
	}
	current, err := platform.KernelNetworkScope(ctx)
	if err != nil || current != scope {
		return nil, errors.Join(err, errors.New("TunnelInterface mutation belongs to another scope"))
	}
	link, err := handle.LinkByIndex(index)
	if err != nil {
		return nil, err
	}
	if link.Attrs() == nil || link.Attrs().Index != index || link.Attrs().Name != name || link.Type() != kind || link.Attrs().Alias != proof {
		return nil, errors.New("TunnelInterface identity changed before indexed mutation")
	}
	current, err = platform.KernelNetworkScope(ctx)
	if err != nil || current != scope {
		return nil, errors.Join(err, errors.New("TunnelInterface scope changed before indexed mutation"))
	}
	return link, ctx.Err()
}

func setTunnelNativeAlias(ctx context.Context, index int, name, kind, proof, scope string) error {
	handle, err := tunnelNativeHandle()
	if err != nil {
		return err
	}
	defer handle.Close()
	link, err := checkedTunnelNativeLink(ctx, handle, index, name, kind, "", scope)
	if err != nil {
		return err
	}
	return handle.LinkSetAlias(link, proof)
}

func deleteTunnelNativeLink(ctx context.Context, index int, name, kind, proof, scope string) error {
	handle, err := tunnelNativeHandle()
	if err != nil {
		return err
	}
	defer handle.Close()
	link, err := checkedTunnelNativeLink(ctx, handle, index, name, kind, proof, scope)
	var absent netlink.LinkNotFoundError
	if errors.As(err, &absent) {
		return nil
	}
	if err != nil {
		return err
	}
	return handle.LinkDel(link)
}

func applyTunnelNativePlan(ctx context.Context, record tunnelNativeReceipt) error {
	handle, err := tunnelNativeHandle()
	if err != nil {
		return err
	}
	defer handle.Close()
	check := func() (netlink.Link, error) {
		return checkedTunnelNativeLink(ctx, handle, record.Index, record.Plan.Name, tunnelNativeKind(record.Plan.Mode), "routerd-tunnel:"+record.Token, record.Scope)
	}
	link, err := check()
	if err != nil {
		return err
	}
	// A readback LinkAttrs also contains read-only address/queue attributes.
	// Send only the exact index and name when changing tunnel parameters.
	attrs := netlink.LinkAttrs{Index: link.Attrs().Index, Name: link.Attrs().Name}
	var configured netlink.Link
	if record.Plan.Mode == "gre" {
		configured = &netlink.Gretun{LinkAttrs: attrs, Local: net.ParseIP(record.Plan.Local), Remote: net.ParseIP(record.Plan.Remote), Ttl: uint8(record.Plan.TTL), PMtuDisc: 1, IKey: uint32(record.Plan.Key), OKey: uint32(record.Plan.Key)}
	} else {
		encap := netlink.None
		if record.Plan.Mode == "fou" {
			encap = netlink.FOU
		} else if record.Plan.Mode == "gue" {
			encap = netlink.GUE
		}
		configured = &netlink.Iptun{LinkAttrs: attrs, Local: net.ParseIP(record.Plan.Local), Remote: net.ParseIP(record.Plan.Remote), Ttl: uint8(record.Plan.TTL), PMtuDisc: 1, EncapType: uint16(encap), EncapSport: uint16(record.Plan.EncapSport), EncapDport: uint16(record.Plan.EncapDport)}
	}
	if err := handle.LinkModify(configured); err != nil {
		return fmt.Errorf("configure native TunnelInterface %s: %w", record.Plan.Name, err)
	}
	if record.PreviousAddress != "" && record.PreviousAddress != record.Plan.Address {
		if link, err = check(); err != nil {
			return err
		}
		address, err := netlink.ParseAddr(record.PreviousAddress)
		if err != nil {
			return err
		}
		if err := handle.AddrDel(link, address); err != nil && !errors.Is(err, syscall.EADDRNOTAVAIL) {
			return err
		}
	}
	if record.Plan.Address != "" {
		if link, err = check(); err != nil {
			return err
		}
		address, err := netlink.ParseAddr(record.Plan.Address)
		if err != nil {
			return err
		}
		if err := handle.AddrReplace(link, address); err != nil {
			return fmt.Errorf("configure native TunnelInterface address: %w", err)
		}
	}
	if link, err = check(); err != nil {
		return err
	}
	if record.Plan.MTU > 0 {
		if err := handle.LinkSetMTU(link, record.Plan.MTU); err != nil {
			return fmt.Errorf("configure native TunnelInterface MTU: %w", err)
		}
	}
	if link, err = check(); err != nil {
		return err
	}
	if err := handle.LinkSetUp(link); err != nil {
		return fmt.Errorf("raise native TunnelInterface: %w", err)
	}
	return nil
}

func tunnelFOUHandle() (*netlink.Handle, error) {
	handle, err := netlink.NewHandle(syscall.NETLINK_GENERIC)
	if err != nil {
		return nil, err
	}
	if err := handle.SetSocketTimeout(2 * time.Second); err != nil {
		handle.Close()
		return nil, err
	}
	return handle, nil
}

func observeTunnelNativeFOU(ctx context.Context, listener fouListener) (bool, bool, error) {
	if err := ctx.Err(); err != nil {
		return false, false, err
	}
	handle, err := tunnelFOUHandle()
	if err != nil {
		return false, false, err
	}
	defer handle.Close()
	listeners, err := handle.FouList(netlink.FAMILY_V4)
	if err != nil {
		return false, false, err
	}
	for _, current := range listeners {
		if current.Family != netlink.FAMILY_V4 || current.Port != listener.Port {
			continue
		}
		matching := current.PeerPort == 0 && current.IfIndex == 0 && (current.Local == nil || current.Local.IsUnspecified()) && (current.Peer == nil || current.Peer.IsUnspecified())
		matching = matching && (listener.Mode == "fou" && current.EncapType == netlink.FOU_ENCAP_DIRECT && current.Protocol == 4 || listener.Mode == "gue" && current.EncapType == netlink.FOU_ENCAP_GUE && current.Protocol == 0)
		return true, matching, ctx.Err()
	}
	return false, false, ctx.Err()
}

func addTunnelNativeFOU(ctx context.Context, listener fouListener) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handle, err := tunnelFOUHandle()
	if err != nil {
		return err
	}
	defer handle.Close()
	value := netlink.Fou{Family: netlink.FAMILY_V4, Port: listener.Port, EncapType: netlink.FOU_ENCAP_DIRECT, Protocol: 4}
	if listener.Mode == "gue" {
		value.EncapType, value.Protocol = netlink.FOU_ENCAP_GUE, 0
	}
	return handle.FouAdd(value)
}

func deleteTunnelNativeFOU(ctx context.Context, listener fouListener) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handle, err := tunnelFOUHandle()
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.FouDel(netlink.Fou{Family: netlink.FAMILY_V4, Port: listener.Port})
}
