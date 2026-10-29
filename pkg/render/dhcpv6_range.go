// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"net/netip"

	"github.com/imksoo/routerd/pkg/api"
)

// DnsmasqDHCPv6Range contains DHCP service only. RA is owned by routerd.
// Stateless information requests use dnsmasq's documented all-zero static
// catch-all, selected by its built-in receiving-interface tag.
func DnsmasqDHCPv6Range(name, ifname string, spec api.DHCPv6ServerSpec) string {
	tag := sanitizeDnsmasqTag(name)
	lease := defaultString(defaultString(spec.AddressPool.LeaseTime, spec.LeaseTime), "12h")
	switch defaultString(spec.Mode, "stateless") {
	case "ra-only":
		return ""
	case "stateful", "both":
		start, startErr := netip.ParseAddr(spec.AddressPool.Start)
		end, endErr := netip.ParseAddr(spec.AddressPool.End)
		if startErr == nil && endErr == nil && netip.PrefixFrom(start, 64).Masked().Addr().IsUnspecified() && netip.PrefixFrom(end, 64).Masked().Addr().IsUnspecified() {
			return fmt.Sprintf("dhcp-range=tag:%s,set:%s,%s,%s,constructor:%s,64,%s", ifname, tag, spec.AddressPool.Start, spec.AddressPool.End, ifname, lease)
		}
		return fmt.Sprintf("dhcp-range=tag:%s,set:%s,%s,%s,64,%s", ifname, tag, spec.AddressPool.Start, spec.AddressPool.End, lease)
	default:
		return fmt.Sprintf("dhcp-range=tag:%s,set:%s,::,static,64,%s", ifname, tag, lease)
	}
}
