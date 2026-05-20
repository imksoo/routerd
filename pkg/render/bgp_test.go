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

func TestFRRConfigFastConvergenceDisablesDefaultGracefulRestart(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.BGPRouterSpec{
				ASN:                64512,
				RouterID:           "10.0.0.1",
				ConvergenceProfile: "fast",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "workers"},
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
	if strings.Contains(got, "bgp graceful-restart") {
		t.Fatalf("fast convergence should not render graceful restart by default:\n%s", got)
	}
	for _, want := range []string{
		"neighbor 10.0.0.21 timers 3 9",
		"neighbor 10.0.0.21 timers connect 5",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("fast convergence config missing %q:\n%s", want, got)
		}
	}
}

func TestFRRConfigFastConvergenceAllowsExplicitGracefulRestart(t *testing.T) {
	enabled := true
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec: api.BGPRouterSpec{
				ASN:                64512,
				RouterID:           "10.0.0.1",
				ConvergenceProfile: "fast",
				GracefulRestart:    api.BGPGracefulRestartSpec{Enabled: &enabled},
			},
		},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	if got := string(data); !strings.Contains(got, "bgp graceful-restart") {
		t.Fatalf("explicit graceful restart should still render:\n%s", got)
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
				ExportPolicy: api.BGPExportPolicySpec{
					AllowedPrefixes: []string{"192.168.50.0/24", "198.51.100.0/24"},
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

func TestFRRConfigRendersExportPolicyForTransitRouting(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:          64512,
			RouterID:     "10.0.0.1",
			ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}},
			ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "worker"}, Spec: api.BGPPeerSpec{
			RouterRef: "BGPRouter/lan",
			PeerASN:   64513,
			Peers:     []string{"192.168.1.54"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "upstream"}, Spec: api.BGPPeerSpec{
			RouterRef: "BGPRouter/lan",
			PeerASN:   64514,
			Peers:     []string{"192.168.1.1"},
			ExportPolicy: api.BGPExportPolicySpec{
				AllowedPrefixes: []string{"10.250.0.0/24"},
			},
		}},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip prefix-list ROUTERD-LAN-EXPORT seq 10 permit 10.250.0.0/24",
		"route-map ROUTERD-LAN-OUT permit 10",
		" match ip address prefix-list ROUTERD-LAN-EXPORT",
		"ip prefix-list ROUTERD-LAN-UPSTREAM-192-168-1-1-EXPORT seq 10 permit 10.250.0.0/24",
		"route-map ROUTERD-LAN-UPSTREAM-192-168-1-1-OUT permit 10",
		" match ip address prefix-list ROUTERD-LAN-UPSTREAM-192-168-1-1-EXPORT",
		"neighbor 192.168.1.1 route-map ROUTERD-LAN-UPSTREAM-192-168-1-1-OUT out",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("FRR config missing %q:\n%s", want, got)
		}
	}
}

func TestFRRConfigKeepsRedistributeExportDenyByDefault(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:      64512,
			RouterID: "10.0.0.1",
			Redistribute: api.BGPRedistributeSpec{
				Connected: api.BGPRedistributeRouteSpec{AllowedPrefixes: []string{"192.168.50.0/24"}},
			},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.BGPPeerSpec{
			RouterRef: "BGPRouter/lan",
			PeerASN:   64513,
			Peers:     []string{"10.0.0.21"},
		}},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "ROUTERD-LAN-EXPORT seq 10 permit") {
		t.Fatalf("redistribute must not imply exportPolicy:\n%s", got)
	}
	if !strings.Contains(got, "route-map ROUTERD-LAN-OUT deny 999") {
		t.Fatalf("FRR config missing default outbound deny:\n%s", got)
	}
}

func TestFRRConfigRendersIPv6Unicast(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:      64512,
			RouterID: "10.0.0.1",
			ImportPolicy: api.BGPImportPolicySpec{
				AllowedPrefixes: []string{"10.0.0.200/29", "fd00:1234::/64"},
			},
			Redistribute: api.BGPRedistributeSpec{
				Connected: api.BGPRedistributeRouteSpec{AllowedPrefixes: []string{"192.168.50.0/24", "fd00:50::/64"}},
			},
			ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"fd00:50::/64"}},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.BGPPeerSpec{
			RouterRef: "BGPRouter/lan",
			PeerASN:   64513,
			Peers:     []string{"10.0.0.21", "fd00:1234::21"},
		}},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"ip prefix-list ROUTERD-LAN-IMPORT seq 10 permit 10.0.0.200/29",
		"ipv6 prefix-list ROUTERD-LAN-IMPORT-V6 seq 10 permit fd00:1234::/64",
		"ipv6 prefix-list ROUTERD-LAN-REDIST-CONNECTED-V6 seq 10 permit fd00:50::/64",
		"ipv6 prefix-list ROUTERD-LAN-EXPORT-V6 seq 10 permit fd00:50::/64",
		"route-map ROUTERD-LAN-IN-V6 permit 10",
		" match ipv6 address prefix-list ROUTERD-LAN-IMPORT-V6",
		"route-map ROUTERD-LAN-OUT-V6 permit 10",
		" match ipv6 address prefix-list ROUTERD-LAN-EXPORT-V6",
		"address-family ipv6 unicast",
		"  redistribute connected route-map ROUTERD-LAN-REDIST-CONNECTED-V6",
		"  neighbor fd00:1234::21 activate",
		"  neighbor fd00:1234::21 route-map ROUTERD-LAN-IN-V6 in",
		"address-family ipv4 unicast",
		"  neighbor 10.0.0.21 activate",
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

func TestFRRConfigRendersBFDPeerAndDaemons(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:      64512,
			RouterID: "10.0.0.1",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.BGPPeerSpec{
			RouterRef: "BGPRouter/lan",
			PeerASN:   64513,
			Peers:     []string{"10.0.0.21"},
			BFD:       "BFD/fabric-fast",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"}, Metadata: api.ObjectMeta{Name: "fabric-fast"}, Spec: api.BFDSpec{
			Peer:             "BGPPeer/fabric",
			Interface:        "Interface/lan",
			Profile:          "fast",
			MinRx:            "300ms",
			MinTx:            "300ms",
			DetectMultiplier: 3,
		}},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"bfd\n peer 10.0.0.21 interface ens19",
		"  receive-interval 300",
		"  transmit-interval 300",
		"  detect-multiplier 3",
		" neighbor 10.0.0.21 bfd",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("FRR config missing %q:\n%s", want, got)
		}
	}
	daemons, err := FRRDaemons([]byte("zebra=yes\nbgpd=no\n"), router)
	if err != nil {
		t.Fatalf("render FRR daemons: %v", err)
	}
	for _, want := range []string{"zebra=yes", "bgpd=yes", "bfdd=yes"} {
		if !strings.Contains(string(daemons), want) {
			t.Fatalf("FRR daemons missing %q:\n%s", want, daemons)
		}
	}
}

func TestFRRDaemonsEnablesBGPDWithoutBFD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:      64512,
			RouterID: "10.0.0.1",
		}},
	}}}
	daemons, err := FRRDaemons([]byte("zebra=yes\nbgpd=no\nbfdd=no\n"), router)
	if err != nil {
		t.Fatalf("render FRR daemons: %v", err)
	}
	got := string(daemons)
	for _, want := range []string{"zebra=yes", "bgpd=yes", "bfdd=no"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FRR daemons missing %q:\n%s", want, got)
		}
	}
}

func TestFRRConfigResolvesBGPPeerPasswordFromEnv(t *testing.T) {
	t.Setenv("ROUTERD_TEST_BGP_PASSWORD", "s3cr3t")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
			ASN:      64512,
			RouterID: "10.0.0.1",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.BGPPeerSpec{
			RouterRef:    "BGPRouter/lan",
			PeerASN:      64513,
			Peers:        []string{"10.0.0.21"},
			PasswordFrom: api.SecretValueSourceSpec{Env: "ROUTERD_TEST_BGP_PASSWORD"},
		}},
	}}}
	data, err := FRRConfig(router)
	if err != nil {
		t.Fatalf("render FRR config: %v", err)
	}
	if got := string(data); !strings.Contains(got, "neighbor 10.0.0.21 password s3cr3t") {
		t.Fatalf("FRR config did not include env password:\n%s", got)
	}
}
