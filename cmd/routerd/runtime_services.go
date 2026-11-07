// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func dnsmasqLeaseFileForPlatform() string {
	if platformDefaults.OS == platform.OSFreeBSD {
		return platformDefaults.DnsmasqLeaseFile()
	}
	return ""
}

func dhcpStickyLogPath() string {
	return strings.TrimRight(platformDefaults.StateDir, "/") + "/dhcp-sticky.db"
}

func dhcpStickyHoldDays(router *api.Router, ip string) int {
	if router == nil {
		return 0
	}
	wantV6 := strings.Contains(strings.TrimSpace(ip), ":")
	holdDays := 0
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DHCPv4Server":
			if wantV6 {
				continue
			}
			spec, err := res.DHCPv4ServerSpec()
			if err == nil && defaultString(spec.Server, "dnsmasq") == "dnsmasq" && spec.StickyHoldDays > holdDays {
				holdDays = spec.StickyHoldDays
			}
		case "DHCPv6Server":
			if !wantV6 {
				continue
			}
			spec, err := res.DHCPv6ServerSpec()
			if err == nil && defaultString(spec.Server, "dnsmasq") == "dnsmasq" && spec.StickyHoldDays > holdDays {
				holdDays = spec.StickyHoldDays
			}
		}
	}
	return holdDays
}

func pppoePassword(res api.Resource, spec api.PPPoESessionSpec) (string, error) {
	if spec.Password != "" {
		return spec.Password, nil
	}
	data, err := os.ReadFile(spec.PasswordFile)
	if err != nil {
		return "", fmt.Errorf("%s read passwordFile %s: %w", res.ID(), spec.PasswordFile, err)
	}
	password := strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", fmt.Errorf("%s passwordFile %s is empty", res.ID(), spec.PasswordFile)
	}
	return password, nil
}
