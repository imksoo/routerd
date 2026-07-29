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

var runIfconfig = func(args ...string) ([]byte, error) {
	return exec.Command("ifconfig", args...).CombinedOutput()
}

func linkLocalUsable(ifname, address string) bool {
	output, err := runIfconfig(ifname)
	return err == nil && addressUsableInIfconfigOutput(string(output), address)
}

func ensureInterfaceLinkLocalIPv6(ifi *net.Interface) (string, error) {
	address := linkLocalFromMAC(ifi.HardwareAddr)
	if address == "" {
		return "", fmt.Errorf("cannot derive link-local address from MAC %q", ifi.HardwareAddr)
	}
	if !linkLocalPresent(ifi.Name, address) {
		if output, err := runIfconfig(ifi.Name, "inet6", address, "prefixlen", "64", "alias"); err != nil {
			return "", fmt.Errorf("add VMAC link-local address: %w: %s", err, strings.TrimSpace(string(output)))
		}
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

func linkLocalPresent(ifname, address string) bool {
	output, err := runIfconfig(ifname)
	if err != nil {
		return false
	}
	want := net.ParseIP(address)
	if want == nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet6" {
			continue
		}
		if got := net.ParseIP(strings.SplitN(fields[1], "%", 2)[0]); got != nil && got.Equal(want) {
			return true
		}
	}
	return false
}
