// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"time"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const bgpRouteProtocol = 186

type netlinkFIBBackend struct{}

func defaultFIBSyncer() FIBSyncer { return &nativeFIBSyncer{backend: netlinkFIBBackend{}} }

func bgpNetlinkHandle(ctx context.Context) (*netlink.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	handle, err := netlink.NewHandle(unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	timeout := 3 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	if timeout < time.Microsecond {
		handle.Delete()
		return nil, context.DeadlineExceeded
	}
	if err := handle.SetSocketTimeout(timeout); err != nil {
		handle.Delete()
		return nil, err
	}
	return handle, nil
}

func (netlinkFIBBackend) KernelScope(ctx context.Context) (string, error) {
	return platform.KernelNetworkScope(ctx)
}

func (b netlinkFIBBackend) Snapshot(ctx context.Context) (fibSnapshot, error) {
	scope, err := b.KernelScope(ctx)
	if err != nil {
		return fibSnapshot{}, err
	}
	handle, err := bgpNetlinkHandle(ctx)
	if err != nil {
		return fibSnapshot{}, err
	}
	defer handle.Delete()
	routes, err := handle.RouteListFiltered(netlink.FAMILY_ALL, &netlink.Route{Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return fibSnapshot{}, fmt.Errorf("read native BGP FIB routes: %w", err)
	}
	out := fibSnapshot{KernelScope: scope}
	for _, route := range routes {
		if err := ctx.Err(); err != nil {
			return fibSnapshot{}, err
		}
		observed, err := observedNetlinkFIBRoute(route)
		if err != nil {
			return fibSnapshot{}, err
		}
		out.Routes = append(out.Routes, observed)
	}
	addresses, err := localFIBAddresses(ctx, handle)
	if err != nil {
		return fibSnapshot{}, err
	}
	out.Addresses = addresses
	return out, ctx.Err()
}

func (netlinkFIBBackend) Prepare(ctx context.Context, r FIBRoute, _ fibSnapshot) ([]bgpstate.NativeFIBRoute, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	p, err := netip.ParsePrefix(r.Prefix)
	if err != nil {
		return nil, "GoBGPFIBRouteUnsupported", nil
	}
	handle, err := bgpNetlinkHandle(ctx)
	if err != nil {
		return nil, "", err
	}
	defer handle.Delete()
	out := bgpstate.NativeFIBRoute{Prefix: p.Masked().String(), PreferredSource: r.PreferredSource, Table: unix.RT_TABLE_MAIN, Protocol: bgpRouteProtocol, Metric: 200}
	for _, value := range r.NextHops {
		gateway, err := netip.ParseAddr(value)
		if err != nil || gateway.Zone() != "" || gateway.Is4() != p.Addr().Is4() || gateway.IsUnspecified() {
			return nil, "", fmt.Errorf("unusable BGP FIB next hop %q", value)
		}
		if gateway.Is6() && gateway.IsLinkLocalUnicast() {
			// FIBRoute carries no interface for a scoped link-local next hop.
			// A route lookup cannot authorize choosing an arbitrary interface.
			return nil, "GoBGPIPv6LinkLocalNextHopUnsupported", nil
		}
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		paths, err := handle.RouteGet(net.ParseIP(gateway.String()))
		if err != nil {
			return nil, "", fmt.Errorf("resolve BGP gateway %s: %w", gateway, err)
		}
		index := 0
		for _, path := range paths {
			if path.LinkIndex > 0 {
				index = path.LinkIndex
				break
			}
		}
		if index == 0 {
			return nil, "", fmt.Errorf("BGP gateway %s has no native interface", gateway)
		}
		out.NextHops = append(out.NextHops, bgpstate.FIBNextHop{Gateway: gateway.String(), InterfaceIndex: index})
	}
	sort.Slice(out.NextHops, func(i, j int) bool { return out.NextHops[i].Gateway < out.NextHops[j].Gateway })
	if err := bgpstate.ValidateFIBJournal(bgpstate.FIBJournal{KernelScope: "linux:prepared", Applied: []bgpstate.NativeFIBRoute{out}}); err != nil {
		return nil, "", err
	}
	return []bgpstate.NativeFIBRoute{out}, "", nil
}

func (netlinkFIBBackend) Add(ctx context.Context, r bgpstate.NativeFIBRoute) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handle, err := bgpNetlinkHandle(ctx)
	if err != nil {
		return err
	}
	defer handle.Delete()
	return handle.RouteAdd(nativeNetlinkRoute(r))
}
func (netlinkFIBBackend) Delete(ctx context.Context, r bgpstate.NativeFIBRoute) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	handle, err := bgpNetlinkHandle(ctx)
	if err != nil {
		return err
	}
	defer handle.Delete()
	return handle.RouteDel(nativeNetlinkRoute(r))
}

func nativeNetlinkRoute(r bgpstate.NativeFIBRoute) *netlink.Route {
	_, dst, _ := net.ParseCIDR(r.Prefix)
	out := &netlink.Route{Dst: dst, Table: r.Table, Protocol: netlink.RouteProtocol(r.Protocol), Priority: r.Metric, Type: unix.RTN_UNICAST, Family: unix.AF_INET, Scope: netlink.SCOPE_UNIVERSE}
	if dst != nil && dst.IP.To4() == nil {
		out.Family = unix.AF_INET6
	}
	if r.PreferredSource != "" {
		out.Src = net.ParseIP(r.PreferredSource)
	}
	if len(r.NextHops) == 1 {
		out.Gw = net.ParseIP(r.NextHops[0].Gateway)
		out.LinkIndex = r.NextHops[0].InterfaceIndex
	} else {
		for _, h := range r.NextHops {
			out.MultiPath = append(out.MultiPath, &netlink.NexthopInfo{Gw: net.ParseIP(h.Gateway), LinkIndex: h.InterfaceIndex})
		}
	}
	return out
}

func observedNetlinkFIBRoute(r netlink.Route) (fibObservedRoute, error) {
	prefix := "0.0.0.0/0"
	if r.Family == unix.AF_INET6 {
		prefix = "::/0"
	}
	if r.Dst != nil {
		p, err := netip.ParsePrefix(r.Dst.String())
		if err != nil {
			return fibObservedRoute{}, fmt.Errorf("malformed native FIB route: %w", err)
		}
		prefix = p.Masked().String()
	}
	out := bgpstate.NativeFIBRoute{Prefix: prefix, Table: r.Table, Protocol: int(r.Protocol), Metric: r.Priority}
	if r.Src != nil {
		a, ok := netip.AddrFromSlice(r.Src)
		if !ok {
			return fibObservedRoute{}, fmt.Errorf("malformed native FIB source")
		}
		out.PreferredSource = a.Unmap().String()
	}
	if r.Gw != nil {
		a, ok := netip.AddrFromSlice(r.Gw)
		if !ok {
			return fibObservedRoute{}, fmt.Errorf("malformed native FIB gateway")
		}
		out.NextHops = append(out.NextHops, bgpstate.FIBNextHop{Gateway: a.Unmap().String(), InterfaceIndex: r.LinkIndex})
	}
	for _, h := range r.MultiPath {
		if h == nil || h.Gw == nil {
			return fibObservedRoute{Route: out}, nil
		}
		a, ok := netip.AddrFromSlice(h.Gw)
		if !ok {
			return fibObservedRoute{}, fmt.Errorf("malformed native FIB multipath gateway")
		}
		out.NextHops = append(out.NextHops, bgpstate.FIBNextHop{Gateway: a.Unmap().String(), InterfaceIndex: h.LinkIndex})
	}
	sort.Slice(out.NextHops, func(i, j int) bool { return out.NextHops[i].Gateway < out.NextHops[j].Gateway })
	// Compare every netlink route field, including attributes this owner never
	// writes. A foreign route with additional metrics is a conflict, not ours.
	expected := nativeNetlinkRoute(out)
	exact := r.Equal(*expected) && r.MTU == 0 && !r.MTULock && r.Window == 0 && r.Rtt == 0 && r.RttVar == 0 && r.Ssthresh == 0 && r.Cwnd == 0 && r.AdvMSS == 0 && r.Reordering == 0 && r.InitCwnd == 0 && r.Features == 0 && r.RtoMin == 0 && !r.RtoMinLock && r.InitRwnd == 0 && r.QuickACK == 0 && r.Congctl == "" && r.FastOpenNoCookie == 0
	return fibObservedRoute{Route: out, Exact: exact}, nil
}

func localFIBAddresses(ctx context.Context, handle *netlink.Handle) ([]fibLocalAddress, error) {
	var out []fibLocalAddress
	links, err := handle.LinkList()
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		addrs, err := handle.AddrList(link, netlink.FAMILY_ALL)
		if err != nil {
			return nil, fmt.Errorf("read addresses on %s: %w", link.Attrs().Name, err)
		}
		for _, local := range addrs {
			observed, err := observedNetlinkFIBAddress(local)
			if err != nil {
				return nil, err
			}
			out = append(out, observed)
		}
	}
	return out, nil
}

func observedNetlinkFIBAddress(local netlink.Addr) (fibLocalAddress, error) {
	addr, ok := netip.AddrFromSlice(local.IP)
	if !ok || local.IPNet == nil {
		return fibLocalAddress{}, fmt.Errorf("native local address is unusable")
	}
	addr = addr.Unmap()
	bits, size := local.IPNet.Mask.Size()
	if size != addr.BitLen() || bits < 0 || bits > size {
		return fibLocalAddress{}, fmt.Errorf("native local address mask is unusable")
	}
	return fibLocalAddress{Address: addr, Prefix: netip.PrefixFrom(addr, bits).Masked(), Tentative: local.Flags&unix.IFA_F_TENTATIVE != 0, DADFailed: local.Flags&unix.IFA_F_DADFAILED != 0}, nil
}
