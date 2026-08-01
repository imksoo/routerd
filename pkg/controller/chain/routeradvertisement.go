// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
	"golang.org/x/net/ipv6"
)

const defaultRouterAdvertisementLifetime = 1800

type deprecatedPrefixInformation struct {
	Prefix        netip.Prefix
	ValidLifetime uint32
}

type routerAdvertisementLinkLocalFunc func(context.Context, string) (netip.Addr, error)
type routerAdvertisementSenderFunc func(context.Context, string, netip.Addr, []byte) error

func (c DHCPv6ServerController) advertisePreviousDelegatedPrefixes(ctx context.Context, spec api.IPv6RouterAdvertisementSpec) error {
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	prefixes := previousDelegatedPrefixInformation(spec, c.Store, now)
	if len(prefixes) == 0 {
		return nil
	}
	ifname := chainInterfaceAliases(c.Router)[spec.Interface]
	if ifname == "" {
		ifname = spec.Interface
	}
	linkLocal := c.RALinkLocal
	if linkLocal == nil {
		linkLocal = interfaceIPv6LinkLocalAddress
	}
	source, err := linkLocal(ctx, ifname)
	if err != nil {
		return err
	}
	if !source.Is6() || !source.IsLinkLocalUnicast() {
		return fmt.Errorf("IPv6 RA source on %s is not link-local: %s", ifname, source)
	}
	sender := c.RASender
	if sender == nil {
		sender = sendRouterAdvertisement
	}
	return sender(ctx, ifname, source, buildDeprecatedPrefixRouterAdvertisement(spec, prefixes))
}

func previousDelegatedPrefixInformation(spec api.IPv6RouterAdvertisementSpec, store Store, now time.Time) []deprecatedPrefixInformation {
	kind, name, ok := resourcequery.SplitResource(strings.TrimSpace(spec.PrefixFrom.Resource))
	if !ok || kind != "IPv6DelegatedAddress" || store == nil {
		return nil
	}
	status := store.ObjectStatus(api.NetAPIVersion, kind, name)
	current, _ := netip.ParsePrefix(cleanStatusString(status["address"]))
	if current.IsValid() {
		current = current.Masked()
	}
	expiresByPrefix := map[netip.Prefix]time.Time{}
	for _, previous := range previousDelegatedAddressesFromStatus(status) {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(previous.Address))
		if err != nil || !prefix.Addr().Is6() || prefix.Bits() != 64 || !now.Before(previous.ExpiresAt) {
			continue
		}
		prefix = prefix.Masked()
		if current.IsValid() && current == prefix {
			continue
		}
		if expiresAt, exists := expiresByPrefix[prefix]; !exists || expiresAt.Before(previous.ExpiresAt) {
			expiresByPrefix[prefix] = previous.ExpiresAt
		}
	}
	prefixes := make([]netip.Prefix, 0, len(expiresByPrefix))
	for prefix := range expiresByPrefix {
		prefixes = append(prefixes, prefix)
	}
	sort.Slice(prefixes, func(i, j int) bool { return prefixes[i].Addr().Less(prefixes[j].Addr()) })
	out := make([]deprecatedPrefixInformation, 0, len(prefixes))
	for _, prefix := range prefixes {
		seconds := remainingIPv6ValidLifetime(now, expiresByPrefix[prefix])
		if seconds <= 0 {
			continue
		}
		if seconds > math.MaxUint32 {
			seconds = math.MaxUint32
		}
		out = append(out, deprecatedPrefixInformation{Prefix: prefix, ValidLifetime: uint32(seconds)})
	}
	return out
}

func buildDeprecatedPrefixRouterAdvertisement(spec api.IPv6RouterAdvertisementSpec, prefixes []deprecatedPrefixInformation) []byte {
	packet := make([]byte, 16, 16+32*len(prefixes))
	packet[0] = 134 // ICMPv6 Router Advertisement
	packet[4] = 64  // Current Hop Limit
	if spec.MFlag {
		packet[5] |= 0x80
	}
	if spec.OFlag {
		packet[5] |= 0x40
	}
	switch spec.PRFPreference {
	case "high":
		packet[5] |= 0x08
	case "low":
		packet[5] |= 0x18
	}
	binary.BigEndian.PutUint16(packet[6:8], routerAdvertisementLifetime(spec.ValidLifetime))
	for _, prefix := range prefixes {
		masked := prefix.Prefix.Masked()
		if !masked.IsValid() || !masked.Addr().Is6() || masked.Bits() != 64 || prefix.ValidLifetime == 0 {
			continue
		}
		option := make([]byte, 32)
		option[0] = 3 // Prefix Information Option
		option[1] = 4 // 32 bytes, in units of 8 octets
		option[2] = 64
		option[3] = 0xc0 // L (on-link) and A (autonomous address configuration)
		binary.BigEndian.PutUint32(option[4:8], prefix.ValidLifetime)
		// Preferred lifetime and reserved2 deliberately remain zero.
		addr := masked.Addr().As16()
		copy(option[16:32], addr[:])
		packet = append(packet, option...)
	}
	return packet
}

func routerAdvertisementLifetime(value string) uint16 {
	value = strings.TrimSpace(value)
	if value == "" {
		// dnsmasq defaults to an 1800-second router lifetime. A direct
		// withdrawal PIO from the same link-local source must keep that router
		// usable even when the resource omits an explicit lifetime.
		return defaultRouterAdvertisementLifetime
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return defaultRouterAdvertisementLifetime
	}
	if seconds > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(seconds)
}

func interfaceIPv6LinkLocalAddress(_ context.Context, ifname string) (netip.Addr, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return netip.Addr{}, err
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return netip.Addr{}, err
	}
	for _, address := range addrs {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Is6() && prefix.Addr().IsLinkLocalUnicast() {
			return prefix.Addr(), nil
		}
	}
	return netip.Addr{}, fmt.Errorf("no IPv6 link-local address on %s", ifname)
}

func sendRouterAdvertisement(_ context.Context, ifname string, source netip.Addr, payload []byte) error {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return err
	}
	conn, err := net.ListenIP("ip6:ipv6-icmp", &net.IPAddr{IP: net.IP(source.AsSlice()), Zone: ifname})
	if err != nil {
		return err
	}
	defer conn.Close()
	packet := ipv6.NewPacketConn(conn)
	if err := packet.SetControlMessage(ipv6.FlagInterface|ipv6.FlagHopLimit, true); err != nil {
		return err
	}
	_, err = packet.WriteTo(payload, &ipv6.ControlMessage{IfIndex: ifi.Index, HopLimit: 255}, &net.IPAddr{IP: net.ParseIP("ff02::1"), Zone: ifname})
	return err
}
