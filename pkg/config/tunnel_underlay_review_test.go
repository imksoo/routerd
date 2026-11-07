// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestTunnelUnderlayCyclesRejectedBeforeRenderer(t *testing.T) {
	for _, test := range []struct {
		name, a, b string
		bad        bool
	}{{"self", "a", "wan", true}, {"two-tunnel-cycle", "b", "a", true}, {"missing", "missing", "wan", true}, {"valid-stack", "wan", "a", false}} {
		t.Run(test.name, func(t *testing.T) {
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth0", MTU: 1500}},
			}}}
			for name, underlay := range map[string]string{"a": test.a, "b": test.b} {
				router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.TunnelInterfaceSpec{Mode: "gre", Local: "192.0.2.1", Remote: "192.0.2.2", UnderlayInterface: underlay, TrustedUnderlay: true}})
			}
			err := ValidateForOS(router, platform.OSLinux)
			if test.bad {
				if err == nil || !strings.Contains(err.Error(), "underlayInterface") {
					t.Fatalf("unsafe tunnel graph admitted: %v", err)
				}
			} else if err != nil {
				t.Fatalf("valid nested tunnel rejected: %v", err)
			}
		})
	}
}

func TestBGPStandardCommunityCannotExceedTwoOctetComponents(t *testing.T) {
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "bgp"}, Spec: api.BGPRouterSpec{ASN: 65000, RouterID: "192.0.2.1", Communities: api.BGPCommunitiesSpec{Set: api.BGPCommunitySetSpec{Out: []string{"65536:1"}}}}}}}}
	if err := ValidateForOS(router, platform.OSLinux); err == nil {
		t.Fatal("accepted community which the actual standard-community encoder cannot represent")
	}
}
