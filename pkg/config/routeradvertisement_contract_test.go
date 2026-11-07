// SPDX-License-Identifier: BSD-3-Clause
package config

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"strings"
	"testing"
)

func TestRouterAdvertisementLifetimeContractAtConfigBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, preferred, valid, router string
		validConfig                    bool
	}{
		{"finite", "30", "60", "4", true}, {"infinite", "infinite", "infinite", "0", true},
		{"preferred-exceeds-valid", "61", "60", "1800", false}, {"bad-router", "30", "60", "3", false}, {"router-overflow", "30", "60", "9001", false}, {"prefix-overflow", "1", "4294967296", "0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := routerWithResources(testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "lan0"}), testResource(api.NetAPIVersion, "IPv6RouterAdvertisement", "advertisement", api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8::/64", PreferredLifetime: tc.preferred, ValidLifetime: tc.valid, RouterLifetime: tc.router, MFlag: true, OFlag: false}))
			for _, target := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
				err := ValidateForOS(router, target)
				if (err == nil) != tc.validConfig {
					t.Fatalf("%s valid=%t err=%v", target, tc.validConfig, err)
				}
			}
		})
	}
}
func TestRAOnlyServerDoesNotRequireStatefulAddressPool(t *testing.T) {
	router := routerWithResources(testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "lan0"}), testResource(api.NetAPIVersion, "DHCPv6Server", "advertisement", api.DHCPv6ServerSpec{Interface: "lan", Mode: "ra-only", DNSSource: "none"}))
	if err := ValidateForOS(router, platform.OSLinux); err != nil {
		t.Fatal(err)
	}
}
func TestRouterAdvertisementRejectsRetiredDnsmasqOwnerPaths(t *testing.T) {
	router := routerWithResources(testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "lan0"}), testResource(api.NetAPIVersion, "IPv6RouterAdvertisement", "advertisement", api.IPv6RouterAdvertisementSpec{Interface: "lan", Prefix: "2001:db8::/64", ConfigPath: "/tmp/foreign.conf"}))
	if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "serve-owned RA") {
		t.Fatalf("retired owner accepted: %v", err)
	}
}
