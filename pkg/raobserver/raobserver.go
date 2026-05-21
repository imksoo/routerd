// SPDX-License-Identifier: BSD-3-Clause

package raobserver

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

const (
	ethernetHeaderLen = 14
	ipv6HeaderLen     = 40
	icmpv6RouterAdv   = 134
	nextHeaderICMPv6  = 58
)

type Advertisement struct {
	SourceMAC      string         `json:"sourceMAC,omitempty"`
	SourceLLA      string         `json:"sourceLLA,omitempty"`
	RouterLifetime int            `json:"routerLifetime"`
	Preference     string         `json:"preference,omitempty"`
	Managed        bool           `json:"managed,omitempty"`
	OtherConfig    bool           `json:"otherConfig,omitempty"`
	Prefixes       []PrefixOption `json:"prefixes,omitempty"`
	Routes         []RouteOption  `json:"routes,omitempty"`
	RDNSS          []RDNSSOption  `json:"rdnss,omitempty"`
}

type PrefixOption struct {
	Prefix            string `json:"prefix"`
	ValidLifetime     uint32 `json:"validLifetime"`
	PreferredLifetime uint32 `json:"preferredLifetime"`
}

type RouteOption struct {
	Prefix     string `json:"prefix"`
	Preference string `json:"preference,omitempty"`
	Lifetime   uint32 `json:"lifetime"`
}

type RDNSSOption struct {
	Servers  []string `json:"servers"`
	Lifetime uint32   `json:"lifetime"`
}

type RouterObservation struct {
	SourceMAC      string         `json:"sourceMAC,omitempty"`
	SourceLLA      string         `json:"sourceLLA,omitempty"`
	RouterLifetime int            `json:"routerLifetime"`
	Preference     string         `json:"preference,omitempty"`
	Managed        bool           `json:"managed,omitempty"`
	OtherConfig    bool           `json:"otherConfig,omitempty"`
	Prefixes       []PrefixOption `json:"prefixes,omitempty"`
	Routes         []RouteOption  `json:"routes,omitempty"`
	RDNSS          []RDNSSOption  `json:"rdnss,omitempty"`
	FirstSeen      time.Time      `json:"firstSeen"`
	LastSeen       time.Time      `json:"lastSeen"`
	Count          uint64         `json:"count"`
}

func ParseEthernetIPv6RA(frame []byte) (Advertisement, bool, error) {
	if len(frame) < ethernetHeaderLen+ipv6HeaderLen+16 {
		return Advertisement{}, false, nil
	}
	etherType := binary.BigEndian.Uint16(frame[12:14])
	if etherType != 0x86dd {
		return Advertisement{}, false, nil
	}
	ip := frame[ethernetHeaderLen:]
	if ip[0]>>4 != 6 || ip[6] != nextHeaderICMPv6 {
		return Advertisement{}, false, nil
	}
	icmp := ip[ipv6HeaderLen:]
	if icmp[0] != icmpv6RouterAdv {
		return Advertisement{}, false, nil
	}
	flags := icmp[5]
	src, ok := netip.AddrFromSlice(ip[8:24])
	if !ok {
		return Advertisement{}, false, fmt.Errorf("invalid IPv6 source address")
	}
	adv := Advertisement{
		SourceMAC:      formatMAC(frame[6:12]),
		SourceLLA:      src.String(),
		RouterLifetime: int(binary.BigEndian.Uint16(icmp[6:8])),
		Preference:     routerPreference(flags),
		Managed:        flags&0x80 != 0,
		OtherConfig:    flags&0x40 != 0,
	}
	parseOptions(icmp[16:], &adv)
	return adv, true, nil
}

func IsSelfAdvertisement(adv Advertisement, selfMAC string) bool {
	selfMAC = normalizeMAC(selfMAC)
	return selfMAC != "" && normalizeMAC(adv.SourceMAC) == selfMAC
}

func ObservationKey(adv Advertisement) string {
	if mac := normalizeMAC(adv.SourceMAC); mac != "" {
		return mac
	}
	return strings.ToLower(adv.SourceLLA)
}

func UpdateObservation(current RouterObservation, adv Advertisement, now time.Time) RouterObservation {
	if current.Count == 0 {
		current.FirstSeen = now
	}
	current.SourceMAC = adv.SourceMAC
	current.SourceLLA = adv.SourceLLA
	current.RouterLifetime = adv.RouterLifetime
	current.Preference = adv.Preference
	current.Managed = adv.Managed
	current.OtherConfig = adv.OtherConfig
	current.Prefixes = adv.Prefixes
	current.Routes = adv.Routes
	current.RDNSS = adv.RDNSS
	current.LastSeen = now
	current.Count++
	return current
}

func parseOptions(options []byte, adv *Advertisement) {
	for len(options) >= 2 {
		optionType := options[0]
		optionLen := int(options[1]) * 8
		if optionLen == 0 || optionLen > len(options) {
			return
		}
		option := options[:optionLen]
		switch optionType {
		case 3:
			if prefix, ok := parsePrefixInformation(option); ok {
				adv.Prefixes = append(adv.Prefixes, prefix)
			}
		case 24:
			if route, ok := parseRouteInformation(option); ok {
				adv.Routes = append(adv.Routes, route)
			}
		case 25:
			if rdnss, ok := parseRDNSS(option); ok {
				adv.RDNSS = append(adv.RDNSS, rdnss)
			}
		}
		options = options[optionLen:]
	}
}

func parsePrefixInformation(option []byte) (PrefixOption, bool) {
	if len(option) < 32 {
		return PrefixOption{}, false
	}
	prefixLen := int(option[2])
	addr, ok := netip.AddrFromSlice(option[16:32])
	if !ok {
		return PrefixOption{}, false
	}
	return PrefixOption{
		Prefix:            maskedPrefix(addr, prefixLen),
		ValidLifetime:     binary.BigEndian.Uint32(option[4:8]),
		PreferredLifetime: binary.BigEndian.Uint32(option[8:12]),
	}, true
}

func parseRouteInformation(option []byte) (RouteOption, bool) {
	if len(option) < 8 {
		return RouteOption{}, false
	}
	prefixLen := int(option[2])
	bytesNeeded := (prefixLen + 7) / 8
	if len(option) < 8+bytesNeeded || bytesNeeded > 16 {
		return RouteOption{}, false
	}
	raw := make([]byte, 16)
	copy(raw, option[8:8+bytesNeeded])
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return RouteOption{}, false
	}
	return RouteOption{
		Prefix:     maskedPrefix(addr, prefixLen),
		Preference: routerPreference(option[3]),
		Lifetime:   binary.BigEndian.Uint32(option[4:8]),
	}, true
}

func parseRDNSS(option []byte) (RDNSSOption, bool) {
	if len(option) < 24 {
		return RDNSSOption{}, false
	}
	var servers []string
	for offset := 8; offset+16 <= len(option); offset += 16 {
		addr, ok := netip.AddrFromSlice(option[offset : offset+16])
		if !ok {
			continue
		}
		servers = append(servers, addr.String())
	}
	return RDNSSOption{Servers: servers, Lifetime: binary.BigEndian.Uint32(option[4:8])}, len(servers) > 0
}

func maskedPrefix(addr netip.Addr, bits int) string {
	prefix := netip.PrefixFrom(addr, bits)
	return prefix.Masked().String()
}

func routerPreference(flags byte) string {
	switch (flags >> 3) & 0x3 {
	case 1:
		return "high"
	case 3:
		return "low"
	default:
		return "medium"
	}
}

func formatMAC(raw []byte) string {
	if len(raw) != 6 {
		return ""
	}
	return net.HardwareAddr(raw).String()
}

func normalizeMAC(raw string) string {
	hw, err := net.ParseMAC(strings.TrimSpace(raw))
	if err != nil {
		clean := strings.ToLower(strings.ReplaceAll(raw, ":", ""))
		if len(clean) != 12 {
			return ""
		}
		if _, err := hex.DecodeString(clean); err != nil {
			return ""
		}
		return clean
	}
	return strings.ToLower(strings.ReplaceAll(hw.String(), ":", ""))
}
