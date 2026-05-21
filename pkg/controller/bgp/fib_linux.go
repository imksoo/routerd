// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package bgp

import (
	"context"
	"net"
	"net/netip"
	"reflect"
	"sort"

	"github.com/vishvananda/netlink"
)

const bgpRouteProtocol = 186

type netlinkFIBSyncer struct {
	installed map[string]FIBRoute
}

func defaultFIBSyncer() FIBSyncer {
	return &netlinkFIBSyncer{installed: map[string]FIBRoute{}}
}

func (s *netlinkFIBSyncer) SyncBGP(_ context.Context, routes []FIBRoute) error {
	if s.installed == nil {
		s.installed = map[string]FIBRoute{}
	}
	desired := map[string]FIBRoute{}
	for _, route := range routes {
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
			continue
		}
		nl, ok := netlinkRoute(route)
		if !ok {
			continue
		}
		if err := netlink.RouteReplace(nl); err != nil {
			return err
		}
		s.installed[key] = route
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
	return nil
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
	route.NextHops = normalizeNextHops(route.NextHops)
	return route
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
	return a.Prefix == b.Prefix && reflect.DeepEqual(a.NextHops, b.NextHops)
}
