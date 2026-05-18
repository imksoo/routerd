// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestFRRConfigRendersDefaultDenyImportPolicy(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.BGPRouterSpec{
				ASN:          64512,
				RouterID:     "10.0.0.1",
				ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.0.0.200/29"}},
				Timers:       api.BGPTimersSpec{Keepalive: "3s", HoldTime: "9s", ConnectRetry: "5s"},
				GracefulRestart: api.BGPGracefulRestartSpec{
					RestartTime:   "120s",
					StalePathTime: "360s",
				},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "k8s"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.0.0.21", "10.0.0.22"},
				Timers:    api.BGPTimersSpec{Keepalive: "2s", HoldTime: "6s"},
			},
		},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"router bgp 64512",
		"bgp router-id 10.0.0.1",
		"bgp graceful-restart",
		"bgp graceful-restart restart-time 120",
		"bgp graceful-restart stalepath-time 360",
		"ip prefix-list ROUTERD-LAN-IMPORT seq 10 permit 10.0.0.200/29",
		"ip prefix-list ROUTERD-LAN-IMPORT seq 999 deny 0.0.0.0/0 le 32",
		"set ip next-hop peer-address",
		"route-map ROUTERD-LAN-OUT deny 999",
		"neighbor 10.0.0.21 remote-as 64513",
		"neighbor 10.0.0.21 timers 2 6",
		"neighbor 10.0.0.21 timers connect 5",
		"neighbor 10.0.0.21 route-map ROUTERD-LAN-IN in",
		"neighbor 10.0.0.21 route-map ROUTERD-LAN-OUT out",
		"neighbor 10.0.0.22 activate",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("FRR config missing %q:\n%s", want, got)
		}
	}
}
