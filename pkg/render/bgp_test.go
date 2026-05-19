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

func TestFRRConfigRendersRedistributeAndCommunities(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.0.0.1",
				ImportPolicy: api.BGPImportPolicySpec{
					AllowedPrefixes: []string{"10.0.0.200/29"},
				},
				Redistribute: api.BGPRedistributeSpec{
					Connected: api.BGPRedistributeRouteSpec{AllowedPrefixes: []string{"192.168.50.0/24"}},
					Static:    api.BGPRedistributeRouteSpec{AllowedPrefixes: []string{"198.51.100.0/24"}},
				},
				Communities: api.BGPCommunitiesSpec{
					Send:   "both",
					Accept: []string{"64513:100", "no-export"},
					Set:    api.BGPCommunitySetSpec{In: []string{"64512:10"}, Out: []string{"64512:20"}},
				},
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "fabric"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.0.0.21"},
			},
		},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip prefix-list ROUTERD-LAN-REDIST-CONNECTED seq 10 permit 192.168.50.0/24",
		"ip prefix-list ROUTERD-LAN-REDIST-STATIC seq 10 permit 198.51.100.0/24",
		"ip prefix-list ROUTERD-LAN-EXPORT seq 10 permit 192.168.50.0/24",
		"ip prefix-list ROUTERD-LAN-EXPORT seq 20 permit 198.51.100.0/24",
		"bgp community-list standard ROUTERD-LAN-COMM-ACCEPT permit 64513:100",
		"bgp community-list standard ROUTERD-LAN-COMM-ACCEPT permit no-export",
		"route-map ROUTERD-LAN-IN permit 10",
		" match community ROUTERD-LAN-COMM-ACCEPT",
		" set community 64512:10 additive",
		"route-map ROUTERD-LAN-OUT permit 10",
		" match ip address prefix-list ROUTERD-LAN-EXPORT",
		" set community 64512:20 additive",
		"neighbor 10.0.0.21 send-community both",
		"  redistribute connected route-map ROUTERD-LAN-REDIST-CONNECTED",
		"  redistribute static route-map ROUTERD-LAN-REDIST-STATIC",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("FRR config missing %q:\n%s", want, got)
		}
	}
}

func TestFRRConfigRendersMultipleBGPRouterInstances(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "wan-peering"}, Spec: api.VRFSpec{IfName: "vrf-wan", RouteTable: 65001}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:      64512,
			RouterID: "10.0.0.1",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.BGPRouterSpec{
			ASN:      65001,
			RouterID: "192.0.2.1",
			VRF:      "wan-peering",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "lan-speaker"}, Spec: api.BGPPeerSpec{
			RouterRef: "BGPRouter/lan",
			PeerASN:   64513,
			Peers:     []string{"10.0.0.21"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "wan-upstream"}, Spec: api.BGPPeerSpec{
			RouterRef: "BGPRouter/wan",
			PeerASN:   65002,
			Peers:     []string{"192.0.2.254"},
		}},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"router bgp 64512",
		" neighbor 10.0.0.21 remote-as 64513",
		"router bgp 65001 vrf vrf-wan",
		" neighbor 192.0.2.254 remote-as 65002",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("FRR config missing %q:\n%s", want, got)
		}
	}
}
