// SPDX-License-Identifier: BSD-3-Clause

package mobilityconfig

import (
	"fmt"
	"hash/fnv"
	"net/netip"
	"strings"
)

func NormalizeSAMTransportAddressingMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", "edge-index":
		return "edge-index"
	case "pair-stable":
		return "pair-stable"
	default:
		return ""
	}
}

func SAMTransportPairKey(a, b string) string {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a <= b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

func StableSAMTransportSlot(seedPrefix, a, b string, capacity int) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.TrimSpace(seedPrefix) + "\x00" + SAMTransportPairKey(a, b)))
	return int(h.Sum64() % uint64(capacity))
}

func SAMTransportSlotPrefix(inner netip.Prefix, slot int) (netip.Prefix, error) {
	if slot < 0 {
		return netip.Prefix{}, fmt.Errorf("slot index must be non-negative")
	}
	base, err := addIPv4(inner.Masked().Addr(), uint32(slot*2))
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(base, 31), nil
}

func addIPv4(addr netip.Addr, offset uint32) (netip.Addr, error) {
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("address %s is not IPv4", addr)
	}
	bytes := addr.As4()
	n := uint32(bytes[0])<<24 | uint32(bytes[1])<<16 | uint32(bytes[2])<<8 | uint32(bytes[3])
	n += offset
	bytes[0] = byte(n >> 24)
	bytes[1] = byte(n >> 16)
	bytes[2] = byte(n >> 8)
	bytes[3] = byte(n)
	return netip.AddrFrom4(bytes), nil
}
