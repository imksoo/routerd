// SPDX-License-Identifier: BSD-3-Clause

package vxlan

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

type bsdSegmentConfig struct {
	VNI                   int
	Local, Remote         netip.Addr
	LocalPort, RemotePort int
	MCIndex               int
	NoLearning            bool
}

// ifvxlancfg and vxlan_sockaddr from FreeBSD sys/net/if_vxlan.h. The
// sockaddr_in6 union is 28 bytes with 4-byte alignment on supported targets.
type bsdVXLANDriverConfig struct {
	VNI          uint32
	Local        [28]byte
	Remote       [28]byte
	MCIndex      uint32
	FTableCount  uint32
	FTableMax    uint32
	FTableExpiry uint32
	PortMin      uint16
	PortMax      uint16
	Learning     uint8
	TTL          uint8
	_            [2]byte
}

func decodeBSDSegmentConfig(raw bsdVXLANDriverConfig) (bsdSegmentConfig, error) {
	local, localPort, err := decodeBSDSegmentAddress(raw.Local)
	if err != nil {
		return bsdSegmentConfig{}, err
	}
	remote, remotePort, err := decodeBSDSegmentAddress(raw.Remote)
	if err != nil {
		return bsdSegmentConfig{}, err
	}
	return bsdSegmentConfig{VNI: int(raw.VNI), Local: local, Remote: remote, LocalPort: localPort, RemotePort: remotePort, MCIndex: int(raw.MCIndex), NoLearning: raw.Learning == 0}, nil
}

func decodeBSDSegmentAddress(raw [28]byte) (netip.Addr, int, error) {
	port := int(binary.BigEndian.Uint16(raw[2:4]))
	switch raw[1] {
	case 0: // A newly cloned, unconfigured endpoint.
		return netip.Addr{}, port, nil
	case 2: // FreeBSD AF_INET.
		if raw[0] != 16 {
			return netip.Addr{}, 0, fmt.Errorf("invalid VXLAN sockaddr_in length %d", raw[0])
		}
		return netip.AddrFrom4([4]byte(raw[4:8])), port, nil
	case 28: // FreeBSD AF_INET6.
		if raw[0] != 28 {
			return netip.Addr{}, 0, fmt.Errorf("invalid VXLAN sockaddr_in6 length %d", raw[0])
		}
		address := [16]byte(raw[8:24])
		if netip.AddrFrom16(address).IsLinkLocalUnicast() {
			// KAME embeds the interface scope in the address's second word.
			address[2], address[3] = 0, 0
		}
		return netip.AddrFrom16(address).Unmap(), port, nil
	default:
		return netip.Addr{}, 0, fmt.Errorf("unsupported native VXLAN sockaddr family %d", raw[1])
	}
}
