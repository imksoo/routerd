// SPDX-License-Identifier: BSD-3-Clause

package render

import "sort"

// DnsmasqIPv6OnlyInterfaces returns emitted IPv6 listeners with no emitted
// DHCPv4 server or relay. interface= enables both families in dnsmasq, so these
// listeners need no-dhcpv4-interface= without disabling DHCPv6 or RA.
// Compare resolved OS interface names, since resource aliases can overlap.
func DnsmasqIPv6OnlyInterfaces(ipv4, ipv6 []string) []string {
	v4 := make(map[string]bool, len(ipv4))
	for _, name := range ipv4 {
		v4[name] = true
	}
	seen := make(map[string]bool, len(ipv6))
	var out []string
	for _, name := range ipv6 {
		if name != "" && !v4[name] && !seen[name] {
			out = append(out, name)
			seen[name] = true
		}
	}
	sort.Strings(out)
	return out
}
