// SPDX-License-Identifier: BSD-3-Clause
//go:build freebsd

package bgp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/subprocessio"
	"golang.org/x/net/route"
	"golang.org/x/sys/unix"
)

type freeBSDFIBBackend struct{}

func defaultFIBSyncer() FIBSyncer { return &nativeFIBSyncer{backend: freeBSDFIBBackend{}} }

func freeBSDKernelFIBScope(ctx context.Context) (string, int, error) {
	scope, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return "", 0, err
	}
	fib, err := unix.SysctlUint32("net.my_fibnum")
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%s:fib:%d", scope, fib), int(fib), nil
}
func (freeBSDFIBBackend) KernelScope(ctx context.Context) (string, error) {
	scope, _, err := freeBSDKernelFIBScope(ctx)
	return scope, err
}

func freeBSDRIB(ctx context.Context) ([]route.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := route.FetchRIB(syscall.AF_UNSPEC, route.RIBTypeRoute, 0)
	if err != nil {
		return nil, fmt.Errorf("read native FreeBSD FIB: %w", err)
	}
	messages, err := route.ParseRIB(route.RIBTypeRoute, raw)
	if err != nil {
		return nil, fmt.Errorf("decode native FreeBSD FIB: %w", err)
	}
	return messages, ctx.Err()
}

func (freeBSDFIBBackend) Snapshot(ctx context.Context) (fibSnapshot, error) {
	scope, fib, err := freeBSDKernelFIBScope(ctx)
	if err != nil {
		return fibSnapshot{}, err
	}
	messages, err := freeBSDRIB(ctx)
	if err != nil {
		return fibSnapshot{}, err
	}
	observed, err := observedFreeBSDRoutes(messages, int(fib))
	if err != nil {
		return fibSnapshot{}, err
	}
	out := fibSnapshot{KernelScope: scope, Routes: observed}
	interfaces, err := net.Interfaces()
	if err != nil {
		return fibSnapshot{}, err
	}
	for _, iface := range interfaces {
		if err := ctx.Err(); err != nil {
			return fibSnapshot{}, err
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return fibSnapshot{}, err
		}
		for _, address := range addresses {
			p, err := netip.ParsePrefix(address.String())
			if err != nil {
				return fibSnapshot{}, fmt.Errorf("decode FreeBSD native local address: %w", err)
			}
			out.Addresses = append(out.Addresses, fibLocalAddress{Address: p.Addr(), Prefix: p.Masked()})
		}
	}
	return out, ctx.Err()
}

func freeBSDIP(a route.Addr) (netip.Addr, bool) {
	switch v := a.(type) {
	case *route.Inet4Addr:
		return netip.AddrFrom4(v.IP), true
	case *route.Inet6Addr:
		return netip.AddrFrom16(v.IP), true
	}
	return netip.Addr{}, false
}
func freeBSDAddress(m *route.RouteMessage, index int) route.Addr {
	if index >= len(m.Addrs) {
		return nil
	}
	return m.Addrs[index]
}

func observedFreeBSDRoutes(messages []route.Message, fib int) ([]fibObservedRoute, error) {
	out := make([]fibObservedRoute, 0, len(messages))
	for _, message := range messages {
		m, ok := message.(*route.RouteMessage)
		if !ok {
			return nil, fmt.Errorf("unexpected message in FreeBSD native FIB")
		}
		if m.Err != nil {
			return nil, m.Err
		}
		dst, ok := freeBSDIP(freeBSDAddress(m, syscall.RTAX_DST))
		if !ok {
			return nil, fmt.Errorf("unusable native FreeBSD FIB destination")
		}
		bits := dst.BitLen()
		if m.Flags&syscall.RTF_HOST == 0 {
			mask, ok := freeBSDIP(freeBSDAddress(m, syscall.RTAX_NETMASK))
			if !ok || mask.Is4() != dst.Is4() {
				return nil, fmt.Errorf("unusable native FreeBSD FIB netmask")
			}
			raw := mask.AsSlice()
			n, total := net.IPMask(raw).Size()
			if total != dst.BitLen() {
				return nil, fmt.Errorf("non-contiguous native FreeBSD FIB netmask")
			}
			bits = n
		}
		prefix := netip.PrefixFrom(dst, bits).Masked().String()
		gateway, gatewayIP := freeBSDIP(freeBSDAddress(m, syscall.RTAX_GATEWAY))
		source, sourceIP := freeBSDIP(freeBSDAddress(m, syscall.RTAX_IFA))
		exact := gatewayIP && sourceIP && gateway.Is4() == dst.Is4() && source.Is4() == dst.Is4() && m.Index > 0
		expected := syscall.RTF_UP | syscall.RTF_GATEWAY | syscall.RTF_STATIC | syscall.RTF_PROTO1
		if bits == dst.BitLen() {
			expected |= syscall.RTF_HOST
		}
		if m.Flags == expected && !exact {
			return nil, fmt.Errorf("incomplete native FreeBSD BGP route identity")
		}
		exact = exact && m.Flags == expected
		native := bgpstate.NativeFIBRoute{Prefix: prefix, Table: fib}
		if m.Flags&syscall.RTF_PROTO1 != 0 {
			native.Protocol = syscall.RTF_PROTO1
		}
		if gatewayIP || sourceIP {
			hop := bgpstate.FIBNextHop{InterfaceIndex: m.Index}
			if gatewayIP {
				hop.Gateway = gateway.String()
			}
			if sourceIP {
				native.PreferredSource = source.String()
			}
			native.NextHops = []bgpstate.FIBNextHop{hop}
		}
		out = append(out, fibObservedRoute{Route: native, Exact: exact})
	}
	return out, nil
}

func (b freeBSDFIBBackend) Prepare(ctx context.Context, r FIBRoute, snapshot fibSnapshot) ([]bgpstate.NativeFIBRoute, string, error) {
	p, err := netip.ParsePrefix(r.Prefix)
	if err != nil {
		return nil, "GoBGPFIBRouteUnsupported", nil
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	fib, err := unix.SysctlUint32("net.my_fibnum")
	if err != nil {
		return nil, "", err
	}
	prepared := make([]bgpstate.NativeFIBRoute, 0, len(r.NextHops))
	for _, value := range r.NextHops {
		gateway, err := netip.ParseAddr(value)
		if err != nil || gateway.Zone() != "" || gateway.Is4() != p.Addr().Is4() || gateway.IsUnspecified() {
			return nil, "", fmt.Errorf("unusable FreeBSD BGP next hop %q", value)
		}
		best := -1
		var selected bgpstate.FIBNextHop
		selectedSource := ""
		for _, observed := range snapshot.Routes {
			prefix, err := netip.ParsePrefix(observed.Route.Prefix)
			if err != nil {
				return nil, "", err
			}
			if prefix.Addr().Is4() != gateway.Is4() || !prefix.Contains(gateway) || prefix.Bits() <= best {
				continue
			}
			for _, hop := range observed.Route.NextHops {
				if hop.InterfaceIndex > 0 && observed.Route.PreferredSource != "" {
					selected = hop
					selectedSource = observed.Route.PreferredSource
					best = prefix.Bits()
					break
				}
			}
		}
		if best < 0 {
			return nil, "", fmt.Errorf("FreeBSD BGP gateway %s has no observed native interface", gateway)
		}
		selected.Gateway = gateway.String()
		if r.PreferredSource != "" {
			selectedSource = r.PreferredSource
		}
		prepared = append(prepared, bgpstate.NativeFIBRoute{Prefix: p.Masked().String(), Table: int(fib), Protocol: syscall.RTF_PROTO1, PreferredSource: selectedSource, NextHops: []bgpstate.FIBNextHop{selected}})
	}
	if err := bgpstate.ValidateFIBJournal(bgpstate.FIBJournal{KernelScope: snapshot.KernelScope, Applied: prepared}); err != nil {
		return nil, "", err
	}
	return prepared, "", nil
}

func (freeBSDFIBBackend) Add(ctx context.Context, r bgpstate.NativeFIBRoute) error {
	return mutateFreeBSDRoute(ctx, "add", r)
}
func (b freeBSDFIBBackend) Delete(ctx context.Context, r bgpstate.NativeFIBRoute) error {
	snapshot, err := b.Snapshot(ctx)
	if err != nil {
		return err
	}
	// Native FreeBSD deletion matches prefix and gateway, not IFA/ifindex.
	// Refuse an ambiguous gateway rather than deleting a different next hop.
	for _, observed := range snapshot.Routes {
		if observed.Route.Prefix != r.Prefix {
			continue
		}
		for _, hop := range observed.Route.NextHops {
			if hop.Gateway == r.NextHops[0].Gateway && (!observed.Exact || !bgpstate.EqualNativeFIBRoute(observed.Route, r)) {
				return fmt.Errorf("FreeBSD BGP withdrawal has an ambiguous native gateway")
			}
		}
	}
	return mutateFreeBSDRoute(ctx, "delete", r)
}
func mutateFreeBSDRoute(ctx context.Context, action string, r bgpstate.NativeFIBRoute) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := netip.ParsePrefix(r.Prefix)
	if err != nil {
		return err
	}
	for _, hop := range r.NextHops {
		iface, err := net.InterfaceByIndex(hop.InterfaceIndex)
		if err != nil {
			return err
		}
		args := []string{"-n", action, "-fib", strconv.Itoa(r.Table)}
		if p.Addr().Is6() {
			args = append(args, "-inet6")
		}
		args = append(args, "-proto1", "-net", r.Prefix, "-ifp", iface.Name, "-ifa", r.PreferredSource, hop.Gateway)
		output := subprocessio.NewCapture(4096)
		cmd := exec.CommandContext(ctx, "/sbin/route", args...)
		cmd.Stdout = output
		cmd.Stderr = output
		if err := subprocessio.Run(cmd); err != nil {
			return fmt.Errorf("FreeBSD route %s: %w: %s", action, err, output.String())
		}
	}
	return nil
}
