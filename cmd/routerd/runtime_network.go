// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

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

func ensureIPv6LocalAddress(ifname, address string) (bool, error) {
	for _, entry := range ipv6AddressEntries(ifname) {
		if entry.Address != address {
			continue
		}
		if platformDefaults.OS == platform.OSFreeBSD || entry.PrefixLen == 128 {
			return false, nil
		}
		// A delegated host address must never leave an on-link route for its
		// containing /64 on a second interface.  Replace an old broader
		// assignment before re-adding the address as /128.
		if err := deleteIPv6LocalAddress(ifname, address, entry.PrefixLen); err != nil {
			return false, err
		}
		break
	}
	if platformDefaults.OS == platform.OSFreeBSD {
		if err := runLogged("ifconfig", ifname, "inet6", address, "prefixlen", "64", "alias"); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := runLogged("ip", "-6", "addr", "add", address+"/128", "dev", ifname); err != nil {
		return false, err
	}
	return true, nil
}

func remainingValidLifetimeSeconds(now, expiresAt time.Time) int64 {
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return int64((remaining + time.Second - 1) / time.Second)
}

func deprecatedIPv6LocalAddressCommand(osName platform.OS, ifname, address string, validSeconds int64) (string, []string) {
	valid := strconv.FormatInt(validSeconds, 10)
	if osName == platform.OSFreeBSD {
		return "ifconfig", []string{ifname, "inet6", address, "prefixlen", "64", "pltime", "0", "vltime", valid, "alias"}
	}
	return "ip", []string{"-6", "addr", "replace", address + "/64", "dev", ifname, "preferred_lft", "0", "valid_lft", valid}
}

func ensureDeprecatedIPv6LocalAddress(ifname, address string, validSeconds int64) (bool, error) {
	if validSeconds <= 0 {
		return false, nil
	}
	for _, entry := range ipv6AddressEntries(ifname) {
		if entry.Address == address {
			return false, nil
		}
	}
	name, args := deprecatedIPv6LocalAddressCommand(platformDefaults.OS, ifname, address, validSeconds)
	if err := runLogged(name, args...); err != nil {
		return false, err
	}
	return true, nil
}

func ensureDeprecatedIPv6LocalAddressForever(ifname, address string) (bool, error) {
	if platformDefaults.OS == platform.OSFreeBSD {
		return ensureIPv6LocalAddress(ifname, address)
	}
	for _, entry := range ipv6AddressEntries(ifname) {
		if entry.Address != address {
			continue
		}
		if entry.PrefixLen != 128 {
			if err := deleteIPv6LocalAddress(ifname, address, entry.PrefixLen); err != nil {
				return false, err
			}
			break
		}
		if ipv6LocalAddressIsDeprecated(ifname, address, 128) {
			return false, nil
		}
		break
	}
	if err := runLogged("ip", "-6", "addr", "replace", address+"/128", "dev", ifname, "preferred_lft", "0", "valid_lft", "forever"); err != nil {
		return false, err
	}
	return true, nil
}

func ipv6LocalAddressIsDeprecated(ifname, address string, prefixLen int) bool {
	out, err := exec.Command("ip", "-6", "-o", "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return false
	}
	want := fmt.Sprintf("%s/%d", address, prefixLen)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "inet6" || i+1 >= len(fields) || fields[i+1] != want {
				continue
			}
			if slicesContainString(fields, "dadfailed") {
				return false
			}
			return slicesContainString(fields, "deprecated")
		}
	}
	return false
}

func slicesContainString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func deleteIPv6LocalAddress(ifname, address string, prefixLen int) error {
	if prefixLen <= 0 || prefixLen > 128 {
		prefixLen = 128
	}
	if platformDefaults.OS == platform.OSFreeBSD {
		return runLogged("ifconfig", ifname, "inet6", address, "-alias")
	}
	return runLogged("ip", "-6", "addr", "del", fmt.Sprintf("%s/%d", address, prefixLen), "dev", ifname)
}
