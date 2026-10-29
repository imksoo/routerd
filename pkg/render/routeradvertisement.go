// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

// DnsmasqStaticRouterAdvertisementRange selects only the declared static /64.
// dnsmasq advertises prefixes from dhcp-range, not from interface or ra-param.
// A delegated PrefixFrom must keep using constructed ranges: static ranges do
// not inherit the decreasing preferred/valid lifetimes of the native address.
func DnsmasqStaticRouterAdvertisementRange(name string, spec api.IPv6RouterAdvertisementSpec) (string, error) {
	value := strings.TrimSpace(spec.Prefix)
	if value == "" {
		return "", nil
	}
	if strings.TrimSpace(spec.PrefixFrom.Resource) != "" {
		return "", fmt.Errorf("IPv6RouterAdvertisement/%s: prefix and prefixFrom cannot both be set", name)
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is6() || prefix.Bits() != 64 {
		return "", fmt.Errorf("IPv6RouterAdvertisement/%s: static prefix must be an IPv6 /64", name)
	}
	return fmt.Sprintf("dhcp-range=set:%s,%s,ra-only,64,12h", sanitizeDnsmasqTag(name), prefix.Masked().Addr()), nil
}
