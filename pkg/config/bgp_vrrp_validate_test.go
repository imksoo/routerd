// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"path/filepath"
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

func TestValidateSecretValueSources(t *testing.T) {
	secretPath := filepath.Join(t.TempDir(), "bgp-password")
	if err := os.WriteFile(secretPath, []byte("czNjcjN0"), 0600); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{ASN: 64512, RouterID: "10.240.70.2"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.BGPPeerSpec{
				RouterRef:    "BGPRouter/lan",
				PeerASN:      64513,
				Peers:        []string{"10.240.70.21"},
				PasswordFrom: api.SecretValueSourceSpec{File: secretPath, Base64: true},
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid secret source should validate: %v", err)
	}
	if warnings := Warnings(router); len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}

	peer := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	peer.Password = "plain"
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive secret error, got %v", err)
	}

	peer.Password = ""
	peer.PasswordFrom.File = filepath.Join(t.TempDir(), "missing")
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err != nil {
		t.Fatalf("missing secret file should warn, not fail: %v", err)
	}
	if warnings := Warnings(router); len(warnings) != 1 || !strings.Contains(warnings[0], "does not exist") {
		t.Fatalf("warnings = %#v", warnings)
	}

	if err := os.WriteFile(secretPath, []byte("not-base64!"), 0600); err != nil {
		t.Fatal(err)
	}
	peer.PasswordFrom.File = secretPath
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("expected base64 validation error, got %v", err)
	}
}

func TestValidateBGPBFDAndWatcher(t *testing.T) {
	enabled := true
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.240.70.2",
				Watcher:  api.BGPWatcherSpec{PollInterval: "5s", MaxPrefixes: 10000, PeerStateChangeThrottle: "5s"},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.240.70.21"},
				BFD: api.BGPBFDSpec{
					Enabled:          &enabled,
					MinRxInterval:    "300ms",
					MinTxInterval:    "300ms",
					DetectMultiplier: 3,
				},
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid BFD/watcher config should validate: %v", err)
	}

	peerSpec := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	peerSpec.BFD.MinRxInterval = "10ms"
	router.Spec.Resources[1].Spec = peerSpec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.bfd.minRxInterval") {
		t.Fatalf("expected minRx validation error, got %v", err)
	}
	peerSpec.BFD.MinRxInterval = "300ms"
	peerSpec.BFD.Enabled = nil
	router.Spec.Resources[1].Spec = peerSpec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "timer fields require") {
		t.Fatalf("expected disabled BFD timer validation error, got %v", err)
	}

	peerSpec.BFD.Enabled = &enabled
	router.Spec.Resources[1].Spec = peerSpec
	routerSpec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	routerSpec.Watcher.PollInterval = "2s"
	router.Spec.Resources[0].Spec = routerSpec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.watcher.pollInterval") {
		t.Fatalf("expected watcher poll interval validation error, got %v", err)
	}
	routerSpec.Watcher.PollInterval = "5s"
	routerSpec.Watcher.MaxPrefixes = 1000000
	router.Spec.Resources[0].Spec = routerSpec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.watcher.maxPrefixes") {
		t.Fatalf("expected watcher maxPrefixes validation error, got %v", err)
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

func TestValidateMultiInstanceBGPRouter(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "wan-peering"}, Spec: api.VRFSpec{IfName: "vrf-wan", RouteTable: 65001}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.240.70.2",
				Listen:   api.BGPListenSpec{Address: "10.240.70.2"},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.BGPRouterSpec{
				ASN:      65001,
				RouterID: "192.0.2.2",
				VRF:      "wan-peering",
				Listen:   api.BGPListenSpec{Address: "192.0.2.2"},
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("multi-instance BGP should validate: %v", err)
	}
}

func TestValidateMultiInstanceBGPRouterRejectsASNAndListenConflicts(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "wan-peering"}, Spec: api.VRFSpec{IfName: "vrf-wan", RouteTable: 65001}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.240.70.2",
				Listen:   api.BGPListenSpec{Address: "10.240.70.2"},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "192.0.2.2",
				VRF:      "wan-peering",
				Listen:   api.BGPListenSpec{Address: "192.0.2.2"},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.asn 64512 conflicts") {
		t.Fatalf("expected ASN conflict, got %v", err)
	}
	spec := router.Spec.Resources[2].Spec.(api.BGPRouterSpec)
	spec.ASN = 65001
	spec.Listen.Address = "10.240.70.2"
	router.Spec.Resources[2].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "bgp-listen conflicts") {
		t.Fatalf("expected listen conflict, got %v", err)
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
