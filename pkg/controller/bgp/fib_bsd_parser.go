// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"math/bits"
	"net/netip"
	"strconv"
	"strings"
)

// parseFreeBSDOwnedBGPRoutes reads the stable, numeric netstat route-table
// columns. RTF_PROTO1 is displayed as flag "1" and is routerd's ownership
// marker, so unmarked foreign routes are deliberately excluded.
func parseFreeBSDOwnedBGPRoutes(output string) map[string]FIBRoute {
	return parseFreeBSDOwnedBGPRoutesForFamily(output, true)
}

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

func normalizeFreeBSDNetstatDestination(value string) string {
	return normalizeFreeBSDNetstatDestinationForFamily(value, true)
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
	Prefix  netip.Prefix
}

// Kept as an alias so existing IPv4 callers and tests retain their contract.
type freeBSDLocalIPv4Address = freeBSDLocalAddress

func parseFreeBSDLocalIPv4Addresses(output string) []freeBSDLocalIPv4Address {
	return filterFreeBSDLocalAddresses(parseFreeBSDLocalAddresses(output), true)
}

func parseFreeBSDLocalIPv6Addresses(output string) []freeBSDLocalAddress {
	return filterFreeBSDLocalAddresses(parseFreeBSDLocalAddresses(output), false)
}

func parseFreeBSDLocalAddresses(output string) []freeBSDLocalAddress {
	var out []freeBSDLocalIPv4Address
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) >= 4 && fields[0] == "inet" && fields[2] == "netmask":
			addr, err := netip.ParseAddr(fields[1])
			if err != nil || !addr.Is4() {
				continue
			}
			bits, ok := freeBSDNetmaskBits(fields[3])
			if !ok {
				continue
			}
			out = append(out, freeBSDLocalAddress{Address: addr, Prefix: netip.PrefixFrom(addr, bits).Masked()})
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
			out = append(out, freeBSDLocalAddress{Address: addr, Prefix: netip.PrefixFrom(addr, bits).Masked()})
		}
	}
	return out
}

func filterFreeBSDLocalAddresses(values []freeBSDLocalAddress, wantIPv4 bool) []freeBSDLocalAddress {
	var out []freeBSDLocalAddress
	for _, value := range values {
		if value.Address.Is4() == wantIPv4 {
			out = append(out, value)
		}
	}
	return out
}

func freeBSDNetmaskBits(value string) (int, bool) {
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is4() {
		mask := addr.As4()
		return freeBSDMaskBits(uint32(mask[0])<<24 | uint32(mask[1])<<16 | uint32(mask[2])<<8 | uint32(mask[3]))
	}
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	mask, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, false
	}
	return freeBSDMaskBits(uint32(mask))
}

func freeBSDMaskBits(mask uint32) (int, bool) {
	ones := bits.OnesCount32(mask)
	if mask != ^uint32(0)<<(32-ones) && ones != 0 {
		return 0, false
	}
	return ones, true
}
