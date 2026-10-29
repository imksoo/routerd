// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package vxlan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
)

type linuxSegmentNative struct {
	scope func(context.Context) (string, error)
}

func (n linuxSegmentNative) KernelScope(ctx context.Context) (string, error) { return n.scope(ctx) }

func newLinuxSegmentNative(scope func(context.Context) (string, error)) (SegmentNative, error) {
	return linuxSegmentNative{scope: scope}, nil
}

func segmentNetlink(ctx context.Context) (*netlink.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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

func (n linuxSegmentNative) Resolve(ctx context.Context, config SegmentConfig, token string) (SegmentIdentity, error) {
	scope, err := n.scope(ctx)
	if err != nil {
		return SegmentIdentity{}, err
	}
	handle, err := segmentNetlink(ctx)
	if err != nil {
		return SegmentIdentity{}, err
	}
	defer handle.Close()
	underlay, err := handle.LinkByName(config.Underlay)
	if err != nil {
		return SegmentIdentity{}, err
	}
	identity := SegmentIdentity{Scope: scope, Token: token, CreationName: config.IfName, UnderlayIndex: underlay.Attrs().Index}
	if config.Bridge != "" {
		bridge, err := handle.LinkByName(config.Bridge)
		if err != nil {
			return identity, err
		}
		if bridge.Type() != "bridge" {
			return identity, fmt.Errorf("VXLAN target %s is not a bridge", config.Bridge)
		}
		identity.BridgeIndex = bridge.Attrs().Index
	}
	return identity, ctx.Err()
}

func (n linuxSegmentNative) ObserveOwned(ctx context.Context, config SegmentConfig, owner SegmentIdentity) (SegmentLink, error) {
	if owner.Index == 0 {
		return n.Observe(ctx, config.IfName)
	}
	scope, err := n.scope(ctx)
	if err != nil {
		return SegmentLink{}, err
	}
	handle, err := segmentNetlink(ctx)
	if err != nil {
		return SegmentLink{}, err
	}
	defer handle.Close()
	link, err := handle.LinkByIndex(owner.Index)
	if err != nil {
		var absent netlink.LinkNotFoundError
		if errors.As(err, &absent) {
			return n.Observe(ctx, config.IfName)
		}
		return SegmentLink{}, err
	}
	return linuxSegmentObservation(ctx, handle, scope, link)
}

func (n linuxSegmentNative) Observe(ctx context.Context, name string) (SegmentLink, error) {
	scope, err := n.scope(ctx)
	if err != nil {
		return SegmentLink{}, err
	}
	handle, err := segmentNetlink(ctx)
	if err != nil {
		return SegmentLink{}, err
	}
	defer handle.Close()
	link, err := handle.LinkByName(name)
	if err != nil {
		var absent netlink.LinkNotFoundError
		if errors.As(err, &absent) {
			return SegmentLink{Scope: scope}, ctx.Err()
		}
		return SegmentLink{}, err
	}
	return linuxSegmentObservation(ctx, handle, scope, link)
}

func linuxSegmentObservation(ctx context.Context, handle *netlink.Handle, scope string, link netlink.Link) (SegmentLink, error) {
	attrs := link.Attrs()
	if attrs == nil || attrs.Index <= 0 || attrs.Name == "" {
		return SegmentLink{}, errors.New("VXLAN link observation lacks native identity")
	}
	token, _ := strings.CutPrefix(attrs.Alias, "routerd.vxlan:")
	if !strings.HasPrefix(attrs.Alias, "routerd.vxlan:") {
		token = ""
	}
	observed := SegmentLink{Exists: true, Scope: scope, Index: attrs.Index, Token: token, Kind: link.Type(), BridgeIndex: attrs.MasterIndex, Up: attrs.Flags&net.FlagUp != 0, Running: attrs.Flags&net.FlagRunning != 0, Config: SegmentConfig{IfName: attrs.Name, MTU: attrs.MTU}}
	vx, ok := link.(*netlink.Vxlan)
	if !ok {
		return observed, nil
	}
	observed.Config.VNI, observed.Config.Port = vx.VxlanId, vx.Port
	observed.LocalPort = vx.Port
	observed.Config.Local = segmentIP(vx.SrcAddr)
	observed.Config.Group = segmentIP(vx.Group)
	observed.UnderlayIndex = vx.VtepDevIndex
	neighbors, err := handle.NeighList(attrs.Index, syscall.AF_BRIDGE)
	if err != nil {
		return SegmentLink{}, err
	}
	for _, neighbor := range neighbors {
		if neighbor.HardwareAddr.String() != "00:00:00:00:00:00" {
			continue
		}
		if neighbor.Flags&netlink.NTF_SELF == 0 || neighbor.State&netlink.NUD_PERMANENT == 0 || len(neighbor.IP) == 0 {
			return SegmentLink{}, errors.New("VXLAN flood entry has unsupported native attributes")
		}
		remote := segmentIP(neighbor.IP)
		if observed.Config.Group != "" && remote == observed.Config.Group {
			// The driver's default multicast destination is installed with
			// the link itself; explicit unicast flood entries are separate.
			continue
		}
		observed.Flood = append(observed.Flood, remote)
	}
	return observed, ctx.Err()
}

func segmentIP(ip net.IP) string {
	if len(ip) == 0 || ip.IsUnspecified() {
		return ""
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return ""
	}
	return address.Unmap().String()
}

func (n linuxSegmentNative) Create(ctx context.Context, config SegmentConfig, owner SegmentIdentity) error {
	topology, err := n.Resolve(ctx, config, owner.Token)
	if err != nil {
		return err
	}
	if topology.Scope != owner.Scope || topology.UnderlayIndex != owner.UnderlayIndex || topology.BridgeIndex != owner.BridgeIndex {
		return errors.New("VXLAN topology changed before creation")
	}
	handle, err := segmentNetlink(ctx)
	if err != nil {
		return err
	}
	defer handle.Close()
	link := &netlink.Vxlan{LinkAttrs: netlink.LinkAttrs{Name: config.IfName, Alias: SegmentProof(owner.Token), MTU: config.MTU}, VxlanId: config.VNI, VtepDevIndex: owner.UnderlayIndex, SrcAddr: net.ParseIP(config.Local), Port: config.Port, Learning: true, UDPCSum: true}
	if config.Group != "" {
		link.Group = net.ParseIP(config.Group)
	}
	// Alias and native tunnel parameters are part of the same RTM_NEWLINK.
	if err := handle.LinkAdd(link); err != nil {
		return err
	}
	return ctx.Err()
}

func (n linuxSegmentNative) owned(ctx context.Context, handle *netlink.Handle, config SegmentConfig, owner SegmentIdentity) (netlink.Link, SegmentLink, error) {
	scope, err := n.scope(ctx)
	if err != nil {
		return nil, SegmentLink{}, err
	}
	if owner.Index <= 0 {
		return nil, SegmentLink{}, errors.New("VXLAN mutation requires a bound native interface index")
	}
	link, err := handle.LinkByIndex(owner.Index)
	if err != nil {
		return nil, SegmentLink{}, err
	}
	observed, err := linuxSegmentObservation(ctx, handle, scope, link)
	if err != nil {
		return nil, observed, err
	}
	if err := CheckSegmentIdentity(config, owner, observed); err != nil {
		return nil, observed, err
	}
	return link, observed, nil
}

func (n linuxSegmentNative) Configure(ctx context.Context, config SegmentConfig, owner SegmentIdentity, admit func(context.Context) error) error {
	if admit == nil {
		return errors.New("VXLAN current activation admission is required")
	}
	handle, err := segmentNetlink(ctx)
	if err != nil {
		return err
	}
	defer handle.Close()
	link, observed, err := n.owned(ctx, handle, config, owner)
	if err != nil {
		return err
	}
	for _, remote := range observed.Flood {
		if !slices.Contains(config.Remotes, remote) {
			return fmt.Errorf("VXLAN %s contains an unowned flood destination %s", config.IfName, remote)
		}
	}
	if err := handle.LinkSetDown(link); err != nil {
		return err
	}
	if config.MTU > 0 {
		link, _, err = n.owned(ctx, handle, config, owner)
		if err != nil {
			return err
		}
		if err := handle.LinkSetMTU(link, config.MTU); err != nil {
			return err
		}
	}
	if owner.BridgeIndex > 0 {
		link, _, err = n.owned(ctx, handle, config, owner)
		if err != nil {
			return err
		}
		bridge, err := handle.LinkByIndex(owner.BridgeIndex)
		if err != nil {
			return err
		}
		if bridge.Attrs().Name != config.Bridge || bridge.Type() != "bridge" {
			return errors.New("VXLAN bridge identity changed")
		}
		if err := handle.LinkSetMaster(link, bridge); err != nil {
			return err
		}
	}
	for _, remote := range config.Remotes {
		_, current, err := n.owned(ctx, handle, config, owner)
		if err != nil {
			return err
		}
		if slices.Contains(current.Flood, remote) {
			continue
		}
		if err := handle.NeighAppend(&netlink.Neigh{LinkIndex: owner.Index, Family: syscall.AF_BRIDGE, State: netlink.NUD_PERMANENT, Flags: netlink.NTF_SELF, HardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0, 0}, IP: net.ParseIP(remote)}); err != nil {
			return err
		}
	}
	if err := admit(ctx); err != nil {
		return err
	}
	link, _, err = n.owned(ctx, handle, config, owner)
	if err != nil {
		return err
	}
	if err := handle.LinkSetUp(link); err != nil {
		return err
	}
	return ctx.Err()
}

func (n linuxSegmentNative) Remove(ctx context.Context, config SegmentConfig, owner SegmentIdentity) error {
	handle, err := segmentNetlink(ctx)
	if err != nil {
		return err
	}
	defer handle.Close()
	link, _, err := n.owned(ctx, handle, config, owner)
	if err != nil {
		return err
	}
	if err := handle.LinkSetDown(link); err != nil {
		return err
	}
	link, _, err = n.owned(ctx, handle, config, owner)
	if err != nil {
		return err
	}
	if err := handle.LinkDel(link); err != nil {
		return err
	}
	return ctx.Err()
}
