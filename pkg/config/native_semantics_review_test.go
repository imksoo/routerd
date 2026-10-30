// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestFreeBSDFirewallAdmissionPreservesInputAndAdmitsOwnedZonePairs(t *testing.T) {
	for _, pair := range [][2]string{{"lan", "self"}, {"lan", "wan"}, {"self", "wan"}} {
		router := routerWithResources(
			testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "em0"}),
			testResource(api.NetAPIVersion, "Interface", "wan", api.InterfaceSpec{IfName: "em1"}),
			testResource(api.FirewallAPIVersion, "FirewallZone", "lan", api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}),
			testResource(api.FirewallAPIVersion, "FirewallZone", "wan", api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"wan"}}),
			testResource(api.FirewallAPIVersion, "FirewallRule", "web", api.FirewallRuleSpec{FromZone: pair[0], ToZone: pair[1], Action: "accept", Protocol: "tcp", Port: 443}),
		)
		err := ValidateForOS(router, platform.OSFreeBSD)
		if err != nil {
			t.Fatalf("supported PF zone pair %v rejected: %v", pair, err)
		}
		if err := ValidateForOS(router, platform.OSLinux); err != nil {
			t.Fatalf("Linux zone semantics regressed: %v", err)
		}
	}
}

func TestLinuxBridgeDoesNotPromiseRSTPWithoutAnOwner(t *testing.T) {
	rstp := true
	router := routerWithResources(
		testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "eth0"}),
		testResource(api.NetAPIVersion, "Bridge", "bridge", api.BridgeSpec{IfName: "bridge0", Members: []string{"lan"}, RSTP: &rstp}),
	)
	if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "RSTP daemon owner") {
		t.Fatalf("unimplemented Linux RSTP admitted: %v", err)
	}
	if err := ValidateForOS(router, platform.OSFreeBSD); err != nil {
		t.Fatalf("FreeBSD's real RSTP support rejected: %v", err)
	}
}
