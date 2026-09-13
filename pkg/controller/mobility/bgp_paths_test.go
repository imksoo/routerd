// SPDX-License-Identifier: BSD-3-Clause
package mobility

import (
	"net/netip"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
)

func TestPlanBGPReturnRoutePathsAdvertisesOnPremCaptureSource(t *testing.T) {
	self := memberPlanInfo{
		NodeRef:              "pve-rt-01",
		Role:                 "onprem",
		Site:                 "pve01",
		Capture:              api.MobilityMemberCapture{Type: "proxy-arp"},
		CaptureSourceAddress: "192.168.123.133",
	}
	paths := planBGPReturnRoutePaths(
		DynamicSource("svnet1", self.NodeRef),
		self,
		netip.MustParsePrefix("192.168.123.0/24"),
		nil,
		nil,
		false,
	)
	if len(paths) != 1 || paths[0].Prefix != "192.168.123.133/32" {
		t.Fatalf("return paths = %#v, want on-prem capture source /32", paths)
	}
	if !stringSliceContains(paths[0].Attrs.Communities, bgpstate.MobilityCommunityReturnRoute) {
		t.Fatalf("communities = %#v, want return-route", paths[0].Attrs.Communities)
	}
	if stringSliceContains(paths[0].Attrs.Communities, bgpstate.MobilityCommunityOwner) {
		t.Fatalf("communities = %#v, capture source must not become an owner path", paths[0].Attrs.Communities)
	}
}

func TestPlanBGPReturnRoutePathsRejectsUnsafeCaptureSources(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		capture string
		address string
	}{
		{name: "provider", role: "aws", capture: "provider-secondary-ip", address: "192.168.123.133"},
		{name: "outside-pool", role: "onprem", capture: "proxy-arp", address: "192.168.124.1"},
		{name: "wrong-capture", role: "onprem", capture: "route-table", address: "192.168.123.133"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := planBGPReturnRoutePaths("test", memberPlanInfo{
				NodeRef: "router-a", Role: tc.role,
				Capture:              api.MobilityMemberCapture{Type: tc.capture},
				CaptureSourceAddress: tc.address,
			}, netip.MustParsePrefix("192.168.123.0/24"), nil, nil, false)
			if len(paths) != 0 {
				t.Fatalf("return paths = %#v, want none", paths)
			}
		})
	}
}
