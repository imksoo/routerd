// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/vishvananda/netlink"
)

type linuxVRFNative struct{}

func newVRFNative() vrfNative { return linuxVRFNative{} }
func (linuxVRFNative) Scope(ctx context.Context) (string, error) {
	return platform.KernelNetworkScope(ctx)
}
func vrfNativeHandle(ctx context.Context, scope string) (*netlink.Handle, error) {
	current, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return nil, err
	}
	if current == "" || current != scope {
		return nil, errors.New("VRF native kernel scope changed")
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
func (native linuxVRFNative) Observe(ctx context.Context, name string) (vrfLink, error) {
	scope, err := native.Scope(ctx)
	if err != nil {
		return vrfLink{}, err
	}
	observed := vrfLink{Name: name, Scope: scope}
	handle, err := vrfNativeHandle(ctx, scope)
	if err != nil {
		return observed, err
	}
	defer handle.Close()
	link, err := handle.LinkByName(name)
	if err != nil {
		var missing netlink.LinkNotFoundError
		if !errors.As(err, &missing) {
			return observed, err
		}
	} else {
		if link == nil || link.Attrs() == nil || link.Attrs().Name != name || link.Attrs().Index < 1 {
			return observed, errors.New("VRF observation lacks a native interface identity")
		}
		attrs := link.Attrs()
		observed.Exists, observed.Index, observed.Kind, observed.Up, observed.Master = true, attrs.Index, link.Type(), attrs.Flags&net.FlagUp != 0, attrs.MasterIndex
		if token, owned := strings.CutPrefix(attrs.Alias, "routerd-vrf:"); owned {
			observed.Token = token
		}
		if link, ok := link.(*netlink.Vrf); ok {
			observed.Table = int(link.Table)
		}
	}
	current, err := native.Scope(ctx)
	if err != nil {
		return observed, err
	}
	if current != scope {
		return vrfLink{}, errors.New("VRF scope changed while observing native presence or absence")
	}
	return observed, ctx.Err()
}
func (native linuxVRFNative) Create(ctx context.Context, plan vrfPlan, index int, scope string) error {
	if index < 1 {
		return errors.New("VRF exclusive creation requires a positive native index")
	}
	handle, err := vrfNativeHandle(ctx, scope)
	if err != nil {
		return err
	}
	defer handle.Close()
	link := &netlink.Vrf{LinkAttrs: netlink.LinkAttrs{Name: plan.Name, Index: index, TxQLen: -1}, Table: uint32(plan.Table)}
	if err := handle.LinkAdd(link); err != nil {
		return err
	}
	return ctx.Err()
}
func exactNativeVRF(ctx context.Context, handle *netlink.Handle, expected vrfLink, token string, emptyAlias bool) (netlink.Link, error) {
	if expected.Index < 1 {
		return nil, errors.New("VRF mutation requires an exact native index")
	}
	link, err := handle.LinkByIndex(expected.Index)
	if err != nil {
		return nil, err
	}
	vrf, ok := link.(*netlink.Vrf)
	if !ok || vrf.Attrs() == nil || vrf.Attrs().Index != expected.Index || vrf.Attrs().Name != expected.Name || int(vrf.Table) != expected.Table {
		return nil, errors.New("VRF native kind, table or interface identity changed")
	}
	want := "routerd-vrf:" + token
	if emptyAlias {
		want = ""
	}
	if vrf.Attrs().Alias != want {
		return nil, errors.New("VRF native ownership cookie changed")
	}
	current, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return nil, err
	}
	if current != expected.Scope {
		return nil, errors.New("VRF kernel scope changed before mutation")
	}
	return link, ctx.Err()
}
func (native linuxVRFNative) Cookie(ctx context.Context, expected vrfLink, token string) error {
	handle, err := vrfNativeHandle(ctx, expected.Scope)
	if err != nil {
		return err
	}
	defer handle.Close()
	link, err := exactNativeVRF(ctx, handle, expected, token, true)
	if err != nil {
		return err
	}
	if err := handle.LinkSetAlias(link, "routerd-vrf:"+token); err != nil {
		return err
	}
	_, err = exactNativeVRF(ctx, handle, expected, token, false)
	return err
}
func (native linuxVRFNative) Up(ctx context.Context, expected vrfLink, token string) error {
	handle, err := vrfNativeHandle(ctx, expected.Scope)
	if err != nil {
		return err
	}
	defer handle.Close()
	link, err := exactNativeVRF(ctx, handle, expected, token, false)
	if err != nil {
		return err
	}
	return handle.LinkSetUp(link)
}
func (native linuxVRFNative) Attach(ctx context.Context, port vrfPort, expected vrfLink, token string) error {
	handle, err := vrfNativeHandle(ctx, expected.Scope)
	if err != nil {
		return err
	}
	defer handle.Close()
	parent, err := exactNativeVRF(ctx, handle, expected, token, false)
	if err != nil {
		return err
	}
	link, err := handle.LinkByIndex(port.Index)
	if err != nil {
		return err
	}
	if link == nil || link.Attrs() == nil || link.Attrs().Index != port.Index || link.Attrs().Name != port.Name || link.Attrs().MasterIndex != 0 && link.Attrs().MasterIndex != expected.Index {
		return errors.New("VRF member native identity or master changed")
	}
	if _, err := exactNativeVRF(ctx, handle, expected, token, false); err != nil {
		return err
	}
	return handle.LinkSetMaster(link, parent)
}
func (native linuxVRFNative) Delete(ctx context.Context, expected vrfLink, token string) error {
	handle, err := vrfNativeHandle(ctx, expected.Scope)
	if err != nil {
		return err
	}
	defer handle.Close()
	link, err := exactNativeVRF(ctx, handle, expected, token, false)
	if err != nil {
		return err
	}
	return handle.LinkDel(link)
}
