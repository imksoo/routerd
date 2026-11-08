// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"net/netip"
	"strconv"
	"strings"
)

// parseFreeBSDOwnedBGPRoutesForFamily parses a single numeric netstat family
// table. RTF_PROTO1 is family-independent; the family filter prevents a
// malformed or mixed fixture from making an inet route look like inet6 state.
func parseFreeBSDOwnedBGPRoutesForFamily(output string, wantIPv4 bool) map[string]FIBRoute {
	routes := map[string]FIBRoute{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.Contains(fields[2], "1") {
			continue
		}
		prefix := normalizeFreeBSDNetstatDestinationForFamily(fields[0], wantIPv4)
		nextHop, err := netip.ParseAddr(fields[1])
		if prefix == "" || err != nil || nextHop.Zone() != "" || nextHop.Is4() != wantIPv4 {
			continue
		}
		route := routes[prefix]
		route.Prefix = prefix
		route.NextHops = normalizeFreeBSDNextHopsForFamily(append(route.NextHops, nextHop.String()), wantIPv4)
		routes[prefix] = route
	}
	return routes
}

func normalizeFreeBSDNetstatDestinationForFamily(value string, wantIPv4 bool) string {
	value = strings.TrimSpace(value)
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is4() == wantIPv4 {
		bits := 128
		if wantIPv4 {
			bits = 32
		}
		return netip.PrefixFrom(addr, bits).String()
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || prefix.Addr().Is4() != wantIPv4 {
		return ""
	}
	return prefix.Masked().String()
}

type freeBSDLocalAddress struct {
	Address netip.Addr
}

func parseFreeBSDLocalAddresses(output string) []freeBSDLocalAddress {
	var out []freeBSDLocalAddress
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 4 && fields[0] == "inet" && fields[2] == "netmask":
			addr, err := netip.ParseAddr(fields[1])
			if err != nil || !addr.Is4() {
				continue
			}
			out = append(out, freeBSDLocalAddress{Address: addr})
		case len(fields) >= 4 && fields[0] == "inet6" && fields[2] == "prefixlen":
			// Scoped link-local addresses require an interface zone. FIBRoute has
			// no zone field, so leave them out rather than emitting ambiguous -ifa.
			if strings.Contains(fields[1], "%") {
				continue
			}
			addr, err := netip.ParseAddr(fields[1])
			bits, bitErr := strconv.Atoi(fields[3])
			if err != nil || bitErr != nil || !addr.Is6() || bits < 0 || bits > 128 {
				continue
			}
			out = append(out, freeBSDLocalAddress{Address: addr})
		}
	}
	return out
}
