// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"context"
	"net"
	"net/netip"
	"reflect"
	"sort"
	"strings"

	"github.com/vishvananda/netlink"
)

const bgpRouteProtocol = 186

type netlinkFIBSyncer struct {
	installed map[string]FIBRoute
}

func defaultFIBSyncer() FIBSyncer {
	return &netlinkFIBSyncer{installed: map[string]FIBRoute{}}
}

func (s *netlinkFIBSyncer) SyncBGP(_ context.Context, routes []FIBRoute) (FIBSyncResult, error) {
	if s.installed == nil {
		s.installed = map[string]FIBRoute{}
	}
	result := FIBSyncResult{
		Installed:                    map[string]bool{},
		Unsupported:                  map[string]string{},
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
		if route.PreferredSource != "" && !preferredSourceIsLocal(route.PreferredSource) {
			result.PreferredSourceSkipped[route.Prefix] = true
			result.PreferredSourceSkippedReason[route.Prefix] = "LocalAddressMissing"
			route.PreferredSource = ""
		}
		desired[route.Prefix] = route
	}
	var keys []string
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		route := normalizeFIBRoute(desired[key])
		if equalFIBRoute(s.installed[key], route) {
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
			return result, err
		}
		s.installed[key] = route
		result.Installed[key] = true
		if route.PreferredSource != "" {
			result.PreferredSource[key] = route.PreferredSource
		}
	}
	for key, route := range s.installed {
		if _, ok := desired[key]; ok {
			continue
		}
		if nl, ok := netlinkRoute(route); ok {
			_ = netlink.RouteDel(nl)
		}
		delete(s.installed, key)
	}
	return result, nil
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
		}
	default:
		for _, hop := range nextHops {
			gw := net.ParseIP(hop)
			if gw == nil {
				continue
			}
			nl.MultiPath = append(nl.MultiPath, &netlink.NexthopInfo{Gw: gw})
		}
	}
	return nl, true
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

func preferredSourceIsLocal(value string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil || !addr.Is4() {
		return false
	}
	links, err := netlink.LinkList()
	if err != nil {
		return false
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
			parsed, ok := netip.AddrFromSlice(local.IP.To4())
			if ok && parsed == addr {
				return true
			}
		}
	}
	return false
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
