// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestDHCPv6SelfAddressPolicyFamilyMatchesItsConsumer(t *testing.T) {
	for _, tc := range []struct {
		family, address, source string
		valid                   bool
	}{
		{"ipv6", "fe80::53", "self", true},
		{"ipv6", "2001:db8::53", "", true},
		{"ipv4", "192.0.2.53", "self", false},
		{"ipv4", "192.0.2.53", "none", true},
	} {
		t.Run(tc.family+tc.source, func(t *testing.T) {
			router := routerWithResources(
				testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "lan0"}),
				testResource(api.NetAPIVersion, "SelfAddressPolicy", "dns-self", api.SelfAddressPolicySpec{AddressFamily: tc.family, Candidates: []api.SelfAddressPolicyCandidate{{Source: "static", Address: tc.address}}}),
				testResource(api.NetAPIVersion, "DHCPv6Server", "server", api.DHCPv6ServerSpec{Interface: "lan", Mode: "ra-only", DNSSource: tc.source, SelfAddressPolicy: "dns-self"}),
			)
			for _, target := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
				err := ValidateForOS(router, target)
				if (err == nil) != tc.valid || err != nil && !strings.Contains(err.Error(), "must use addressFamily ipv6") {
					t.Fatalf("%s: valid=%t err=%v", target, tc.valid, err)
				}
			}
		})
	}
}
