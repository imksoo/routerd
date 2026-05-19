// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/platform"
)

func TestValidateBGPRouterPeerAndVirtualIPv4Address(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "k8s-api"}, Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualIPv4VRRPSpec{VirtualRouterID: 50, Priority: 150, Peers: []string{"10.240.70.3"}},
				Track:     []api.ResourceTrackSpec{{Resource: "BGPRouter/lan", UnhealthyPenalty: 50}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:          64512,
				RouterID:     "10.240.70.2",
				ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.240.70.200/29"}},
				Timers:       api.BGPTimersSpec{Keepalive: "3s", HoldTime: "9s", ConnectRetry: "5s"},
				GracefulRestart: api.BGPGracefulRestartSpec{
					RestartTime:   "120s",
					StalePathTime: "360s",
				},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "k8s"}, Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.240.70.21", "10.240.70.22"},
				Timers:    api.BGPTimersSpec{Keepalive: "2s", HoldTime: "6s"},
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate BGP/VRRP resources: %v", err)
	}
	router.Spec.Resources[3].Spec = api.BGPPeerSpec{RouterRef: "BGPRouter/missing", PeerASN: 64513, Peers: []string{"10.240.70.21"}}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "references missing BGPRouter") {
		t.Fatalf("expected missing routerRef error, got %v", err)
	}
}

func TestValidateHostnameRequiresDNSResolverZoneCoverage(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSZone"}, Metadata: api.ObjectMeta{Name: "lan-zone"}, Spec: api.DNSZoneSpec{Zone: "lain.local"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DNSResolver"}, Metadata: api.ObjectMeta{Name: "lan-resolver"}, Spec: api.DNSResolverSpec{
				Listen: []api.DNSResolverListenSpec{{Name: "lan", Addresses: []string{"127.0.0.1"}, Port: 53}},
				Sources: []api.DNSResolverSourceSpec{{
					Name:    "local",
					Kind:    "zone",
					Match:   []string{"lain.local"},
					ZoneRef: []string{"DNSZone/lan-zone"},
				}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Hostname:  "k8s-api.lain.local",
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate hostname coverage: %v", err)
	}
	spec := router.Spec.Resources[3].Spec.(api.VirtualIPv4AddressSpec)
	spec.Hostname = "k8s_api.lain.local"
	router.Spec.Resources[3].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.hostname is invalid") {
		t.Fatalf("expected invalid hostname error, got %v", err)
	}
	spec.Hostname = "k8s-api.other.local"
	router.Spec.Resources[3].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "not covered") {
		t.Fatalf("expected uncovered hostname error, got %v", err)
	}
}

func TestValidateBGPTimersRejectsInvalidHoldTime(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.240.70.2",
				Timers:   api.BGPTimersSpec{Keepalive: "9s", HoldTime: "3s"},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "holdTime must be greater") {
		t.Fatalf("expected holdTime validation error, got %v", err)
	}
}

func TestValidateBGPRedistributeRejectsImportOverlap(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:          64512,
				RouterID:     "10.240.70.2",
				ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.240.70.0/24"}},
				Redistribute: api.BGPRedistributeSpec{
					Connected: api.BGPRedistributeRouteSpec{AllowedPrefixes: []string{"10.240.70.128/25"}},
				},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("expected BGP prefix overlap validation error, got %v", err)
	}
}

func TestValidateBGPCommunitiesRejectsInvalidCommunity(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.240.70.2",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.240.70.21"},
				Communities: api.BGPCommunitiesSpec{
					Send: "both",
					Set:  api.BGPCommunitySetSpec{Out: []string{"64512:not-a-number"}},
				},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.communities.set.out[0]") {
		t.Fatalf("expected BGP community validation error, got %v", err)
	}
}

func TestValidateVirtualIPv4AddressRejectsStaticAddressConflict(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv4StaticAddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualIPv4AddressSpec{
				Interface:   "lan",
				AddressFrom: api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/lan-base", Field: "address"},
				Mode:        "static",
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.addressFrom conflicts") {
		t.Fatalf("expected addressFrom conflict validation error, got %v", err)
	}
}

func TestValidateVirtualIPv4AddressVRRPRequiresPeers(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualIPv4VRRPSpec{VirtualRouterID: 50},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.vrrp.peers is required") {
		t.Fatalf("expected vrrp peers error, got %v", err)
	}
}

func TestValidateVirtualIPv4AddressCARPAllowsEmptyPeers(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vtnet1"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualIPv4VRRPSpec{VirtualRouterID: 50, Authentication: "secret"},
			}},
		}},
	}
	if err := ValidateForOS(router, platform.OSFreeBSD); err != nil {
		t.Fatalf("FreeBSD CARP should allow multicast peers omitted: %v", err)
	}
}

func TestValidateVirtualIPv4AddressPreemptDelayRequiresPreempt(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualIPv4VRRPSpec{VirtualRouterID: 50, Peers: []string{"10.240.70.3"}, PreemptDelay: "5m"},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.vrrp.preemptDelay requires") {
		t.Fatalf("expected preemptDelay validation error, got %v", err)
	}
}

func TestValidateVirtualIPv4AddressRejectsDuplicateVRIDOnInterface(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "vip-a"}, Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan", Address: "10.240.70.10/32", Mode: "vrrp",
				VRRP: api.VirtualIPv4VRRPSpec{VirtualRouterID: 50, Peers: []string{"10.240.70.3"}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "vip-b"}, Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan", Address: "10.240.70.11/32", Mode: "vrrp",
				VRRP: api.VirtualIPv4VRRPSpec{VirtualRouterID: 50, Peers: []string{"10.240.70.3"}},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "virtualRouterID conflicts") {
		t.Fatalf("expected duplicate VRID validation error, got %v", err)
	}
}

func TestValidateVirtualIPv4AddressRejectsInvalidVRRPPeer(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualIPv4Address"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualIPv4AddressSpec{
				Interface: "lan", Address: "10.240.70.10/32", Mode: "vrrp",
				VRRP: api.VirtualIPv4VRRPSpec{VirtualRouterID: 50, Peers: []string{"bad_peer"}},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.vrrp.peers[0]") {
		t.Fatalf("expected invalid VRRP peer validation error, got %v", err)
	}
}
