// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"strconv"

	"github.com/imksoo/routerd/pkg/platform"
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

func deprecatedIPv6LocalAddressCommand(osName platform.OS, ifname, address string, validSeconds int64) (string, []string) {
	valid := strconv.FormatInt(validSeconds, 10)
	if osName == platform.OSFreeBSD {
		return "ifconfig", []string{ifname, "inet6", address, "prefixlen", "64", "pltime", "0", "vltime", valid, "alias"}
	}
	return "ip", []string{"-6", "addr", "replace", address + "/64", "dev", ifname, "preferred_lft", "0", "valid_lft", valid}
}
