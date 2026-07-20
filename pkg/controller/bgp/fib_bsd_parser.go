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
	routes := map[string]FIBRoute{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.Contains(fields[2], "1") {
			continue
		}
		prefix := normalizeFreeBSDNetstatDestination(fields[0])
		nextHop, err := netip.ParseAddr(fields[1])
		if prefix == "" || err != nil || !nextHop.Is4() {
			continue
		}
		route := routes[prefix]
		route.Prefix = prefix
		route.NextHops = normalizeFreeBSDNextHops(append(route.NextHops, nextHop.String()))
		routes[prefix] = route
	}
	return routes
}

func normalizeFreeBSDNetstatDestination(value string) string {
	value = strings.TrimSpace(value)
	if addr, err := netip.ParseAddr(value); err == nil && addr.Is4() {
		return netip.PrefixFrom(addr, 32).String()
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() {
		return ""
	}
	return prefix.Masked().String()
}

func parseFreeBSDLocalIPv4Addresses(output string) []freeBSDLocalIPv4Address {
	var out []freeBSDLocalIPv4Address
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "inet" || fields[2] != "netmask" {
			continue
		}
		addr, err := netip.ParseAddr(fields[1])
		if err != nil || !addr.Is4() {
			continue
		}
		bits, ok := freeBSDNetmaskBits(fields[3])
		if !ok {
			continue
		}
		out = append(out, freeBSDLocalIPv4Address{Address: addr, Prefix: netip.PrefixFrom(addr, bits).Masked()})
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
