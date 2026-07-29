// SPDX-License-Identifier: BSD-3-Clause
//go:build freebsd

package main

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

func linkLocalUsable(ifname, address string) bool {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return false
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return false
	}
	got, err := selectLinkLocalIPv6(addrs)
	return err == nil && got == address
}

func ensureInterfaceLinkLocalIPv6(ifi *net.Interface) (string, error) {
	address := linkLocalFromMAC(ifi.HardwareAddr)
	if address == "" {
		return "", fmt.Errorf("cannot derive link-local address from MAC %q", ifi.HardwareAddr)
	}
	command := exec.Command("ifconfig", ifi.Name, "inet6", address, "prefixlen", "64", "alias")
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("add VMAC link-local address: %w: %s", err, strings.TrimSpace(string(output)))
	}
	deadline := time.Now().Add(3 * time.Second)
	for !linkLocalUsable(ifi.Name, address) {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("VMAC link-local address %s did not become usable on %s", address, ifi.Name)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return address, nil
}
