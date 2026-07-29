// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package main

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

func linkLocalUsable(ifname, address string) bool {
	output, err := exec.Command("ip", "-6", "-o", "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return false
	}
	return addressUsableInIPOutput(string(output), address)
}

func ensureInterfaceLinkLocalIPv6(ifi *net.Interface) (string, error) {
	address := linkLocalFromMAC(ifi.HardwareAddr)
	if address == "" {
		return "", fmt.Errorf("cannot derive link-local address from MAC %q", ifi.HardwareAddr)
	}
	command := exec.Command("ip", "-6", "address", "replace", address+"/64", "dev", ifi.Name, "nodad")
	if output, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("add VMAC link-local address: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := waitForLinkLocalReady(ifi.Name, address, 3*time.Second); err != nil {
		return "", err
	}
	return address, nil
}

func waitForLinkLocalReady(ifname, address string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if linkLocalUsable(ifname, address) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("VMAC link-local address %s did not become usable on %s", address, ifname)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
