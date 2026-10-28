// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/binary"
	"fmt"
	"net/netip"
)

type ipv4FwmarkRule struct {
	Priority int
	Mark     int
	Table    int
}

func deleteIPv4FwmarkRule(rule ipv4FwmarkRule) error {
	return runLogged(
		"ip", "-4", "rule", "del",
		"priority", fmt.Sprintf("%d", rule.Priority),
		"fwmark", fmt.Sprintf("0x%x", rule.Mark),
		"table", fmt.Sprintf("%d", rule.Table),
	)
}

func flushIPv4RouteTable(table int) error {
	return runLogged("ip", "-4", "route", "flush", "table", fmt.Sprintf("%d", table))
}

func ipv6HostSuffix64(addr netip.Addr) uint64 {
	bytes := addr.As16()
	return binary.BigEndian.Uint64(bytes[8:])
}
