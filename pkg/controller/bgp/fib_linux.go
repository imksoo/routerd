// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"sort"
	"strings"

	"github.com/vishvananda/netlink"
)

const bgpRouteProtocol = 186

type netlinkFIBSyncer struct {
	installed       map[string]kernelBGPRoute
	retainOnMissing map[string]bool
}

type kernelBGPRoute struct {
	FIB     FIBRoute
	Netlink netlink.Route
}

func defaultFIBSyncer() FIBSyncer {
	return &netlinkFIBSyncer{installed: map[string]kernelBGPRoute{}, retainOnMissing: map[string]bool{}}
}

func (s *netlinkFIBSyncer) SyncBGP(_ context.Context, routes []FIBRoute) (FIBSyncResult, error) {
	if s.installed == nil {
		s.installed = map[string]kernelBGPRoute{}
	}
	if s.retainOnMissing == nil {
		s.retainOnMissing = map[string]bool{}
	}
	kernel, err := kernelBGPRoutes()
	if err != nil {
		return FIBSyncResult{}, fmt.Errorf("list current BGP routes: %w", err)
	}
	s.installed = kernel
	localAddresses := localIPv4Addresses()
	localHostPrefixes := localIPv4HostPrefixes(localAddresses)
	result := FIBSyncResult{
		Installed:                    map[string]bool{},
		Unsupported:                  map[string]string{},
		Retained:                     map[string]bool{},
		RetainedNextHops:             map[string][]string{},
		PreferredSource:              map[string]string{},
		PreferredSourceSkipped:       map[string]bool{},
		PreferredSourceSkippedReason: map[string]string{},
	}
	desired := map[string]FIBRoute{}
	for _, route := range routes {
		route = normalizeFIBRoute(route)
		if route.Prefix == "" {
			continue
		}
		if route.PreferredSource == "" {
			route.PreferredSource = inferPreferredSource(route.Prefix, localAddresses)
		}
		if route.PreferredSource != "" && !preferredSourceInAddresses(route.PreferredSource, localAddresses) {
			result.PreferredSourceSkipped[route.Prefix] = true
			result.PreferredSourceSkippedReason[route.Prefix] = "LocalAddressMissing"
			route.PreferredSource = ""
		}
		if route.RetainOnMissing {
			s.retainOnMissing[route.Prefix] = true
		} else {
			delete(s.retainOnMissing, route.Prefix)
		}
		desired[route.Prefix] = route
	}
	desired = filterLocalHostFIBRoutes(desired, localHostPrefixes)
	var keys []string
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		route := normalizeFIBRoute(desired[key])
		if installed, ok := s.installed[key]; ok && equalFIBRoute(installed.FIB, route) {
			installed.FIB = route
			s.installed[key] = installed
			result.Installed[key] = true
			if route.PreferredSource != "" {
				result.PreferredSource[key] = route.PreferredSource
			}
			continue
		}
		nl, ok := netlinkRoute(route)
		if !ok {
			result.Unsupported[key] = unsupportedFIBReason(route.Prefix)
			continue
		}
		if err := netlink.RouteReplace(nl); err != nil {
			return result, fmt.Errorf("replace BGP route %s via %v: %w", route.Prefix, route.NextHops, err)
		}
		s.installed[key] = kernelBGPRoute{FIB: route, Netlink: *nl}
		result.Installed[key] = true
		if route.PreferredSource != "" {
			result.PreferredSource[key] = route.PreferredSource
		}
	}
	for key, route := range s.installed {
		if _, ok := desired[key]; ok {
			continue
		}
		if route.FIB.RetainOnMissing || s.retainOnMissing[key] {
			result.Installed[key] = true
			result.Retained[key] = true
			result.RetainedNextHops[key] = route.FIB.NextHops
			if route.FIB.PreferredSource != "" {
				result.PreferredSource[key] = route.FIB.PreferredSource
			}
			continue
		}
		if err := netlink.RouteDel(&route.Netlink); err != nil {
			return result, fmt.Errorf("delete stale BGP route %s: %w", route.FIB.Prefix, err)
		}
		delete(s.installed, key)
	}
	return result, nil
}

func filterLocalHostFIBRoutes(routes map[string]FIBRoute, localHostPrefixes map[string]bool) map[string]FIBRoute {
	if len(routes) == 0 || len(localHostPrefixes) == 0 {
		return routes
	}
	out := map[string]FIBRoute{}
	for prefix, route := range routes {
		if localHostPrefixes[normalizeRoutePrefix(prefix)] {
			continue
		}
		out[prefix] = route
	}
	return out
}

type localIPv4Address struct {
	Address netip.Addr
	Prefix  netip.Prefix
}

func localIPv4Addresses() []localIPv4Address {
	var out []localIPv4Address
	links, err := netlink.LinkList()
	if err != nil {
		return out
	}
	for _, link := range links {
		addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
		if err != nil {
			continue
		}
		for _, local := range addrs {
			if local.IP == nil {
				continue
			}
			addr, ok := netip.AddrFromSlice(local.IP.To4())
			if !ok || !addr.Is4() || local.IPNet == nil {
				continue
			}
			bits, size := local.IPNet.Mask.Size()
			if size != 32 || bits < 0 || bits > 32 {
				continue
			}
			prefix := netip.PrefixFrom(addr, bits).Masked()
			out = append(out, localIPv4Address{Address: addr, Prefix: prefix})
		}
	}
	return out
}

func localIPv4HostPrefixes(addresses []localIPv4Address) map[string]bool {
	out := map[string]bool{}
	for _, local := range addresses {
		if local.Address.Is4() {
			out[netip.PrefixFrom(local.Address, 32).String()] = true
		}
	}
	return out
}

func inferPreferredSource(routePrefix string, addresses []localIPv4Address) string {
	prefix, err := netip.ParsePrefix(routePrefix)
	if err != nil || !prefix.Addr().Is4() {
		return ""
	}
	dst := prefix.Addr()
	var best localIPv4Address
	bestBits := -1
	for _, local := range addresses {
		if !local.Address.Is4() || local.Address == dst || !local.Prefix.Contains(dst) {
			continue
		}
		bits := local.Prefix.Bits()
		if bits > bestBits {
			best = local
			bestBits = bits
		}
	}
	if bestBits < 0 {
		return ""
	}
	return best.Address.String()
}

func preferredSourceInAddresses(value string, addresses []localIPv4Address) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || !addr.Is4() {
		return false
	}
	for _, local := range addresses {
		if local.Address == addr {
			return true
		}
	}
	return false
}

func kernelBGPRoutes() (map[string]kernelBGPRoute, error) {
	filter := &netlink.Route{Protocol: bgpRouteProtocol}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, filter, netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		return nil, err
	}
	out := map[string]kernelBGPRoute{}
	for _, route := range routes {
		fibRoute, ok := fibRouteFromNetlinkRoute(route)
		if !ok {
			continue
		}
		out[fibRoute.Prefix] = kernelBGPRoute{FIB: fibRoute, Netlink: route}
	}
	return out, nil
}

func fibRouteFromNetlinkRoute(route netlink.Route) (FIBRoute, bool) {
	if route.Dst == nil {
		return FIBRoute{}, false
	}
	prefix, err := netip.ParsePrefix(route.Dst.String())
	if err != nil || !prefix.Addr().Is4() {
		return FIBRoute{}, false
	}
	var nextHops []string
	if route.Gw != nil {
		nextHops = append(nextHops, route.Gw.String())
	}
	for _, hop := range route.MultiPath {
		if hop != nil && hop.Gw != nil {
			nextHops = append(nextHops, hop.Gw.String())
		}
	}
	out := FIBRoute{
		Prefix:   prefix.Masked().String(),
		NextHops: normalizeNextHops(nextHops),
	}
	if route.Src != nil {
		out.PreferredSource = normalizePreferredSource(route.Src.String())
	}
	return out, true
}

func netlinkRoute(route FIBRoute) (*netlink.Route, bool) {
	prefix, err := netip.ParsePrefix(route.Prefix)
	if err != nil || !prefix.Addr().Is4() {
		return nil, false
	}
	_, dst, err := net.ParseCIDR(prefix.Masked().String())
	if err != nil {
		return nil, false
	}
	nl := &netlink.Route{
		Dst:      dst,
		Protocol: bgpRouteProtocol,
		Priority: 200,
	}
	if source := net.ParseIP(strings.TrimSpace(route.PreferredSource)); source != nil {
		nl.Src = source
	}
	nextHops := normalizeNextHops(route.NextHops)
	switch len(nextHops) {
	case 0:
	case 1:
		if gw := net.ParseIP(nextHops[0]); gw != nil {
			nl.Gw = gw
			nl.LinkIndex = linkIndexForGateway(gw)
		}
	default:
		for _, hop := range nextHops {
			gw := net.ParseIP(hop)
			if gw == nil {
				continue
			}
			nl.MultiPath = append(nl.MultiPath, &netlink.NexthopInfo{Gw: gw, LinkIndex: linkIndexForGateway(gw)})
		}
	}
	return nl, true
}

func linkIndexForGateway(gw net.IP) int {
	if gw == nil {
		return 0
	}
	routes, err := netlink.RouteGet(gw)
	if err != nil {
		return 0
	}
	for _, route := range routes {
		if route.LinkIndex != 0 {
			return route.LinkIndex
		}
	}
	return 0
}

func normalizeFIBRoute(route FIBRoute) FIBRoute {
	route.Prefix = normalizeRoutePrefix(route.Prefix)
	route.NextHops = normalizeNextHops(route.NextHops)
	route.PreferredSource = normalizePreferredSource(route.PreferredSource)
	return route
}

func normalizePreferredSource(value string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || !addr.Is4() {
		return ""
	}
	return addr.String()
}

func normalizeNextHops(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		ip := net.ParseIP(value)
		if ip == nil {
			continue
		}
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func equalFIBRoute(a, b FIBRoute) bool {
	a = normalizeFIBRoute(a)
	b = normalizeFIBRoute(b)
	return a.Prefix == b.Prefix && a.PreferredSource == b.PreferredSource && reflect.DeepEqual(a.NextHops, b.NextHops)
}

func unsupportedFIBReason(prefix string) string {
	parsed, err := netip.ParsePrefix(prefix)
	if err == nil && parsed.Addr().Is6() {
		return "GoBGPIPv6FIBUnsupported"
	}
	return "GoBGPFIBRouteUnsupported"
}
