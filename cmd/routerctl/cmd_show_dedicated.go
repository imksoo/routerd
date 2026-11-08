// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net/netip"
	"strconv"
	"strings"
)

type virtualAddressShowSpec struct {
	Interface string
	Address   string
	Family    string
	Mode      string
	Peers     []string
}

func ipOutputHasAddress(output, address, family string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(address))
	if err != nil {
		addr, addrErr := netip.ParseAddr(strings.TrimSpace(address))
		if addrErr != nil {
			return false
		}
		bits := 32
		if family == "ipv6" {
			bits = 128
		}
		prefix = netip.PrefixFrom(addr, bits)
	}
	token := "inet "
	if family == "ipv6" {
		token = "inet6 "
	}
	needle := token + prefix.Addr().String() + "/" + strconv.Itoa(prefix.Bits())
	return strings.Contains(output, needle)
}
