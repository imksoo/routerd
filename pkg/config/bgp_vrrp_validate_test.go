// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestValidateBGPRouterPeerAndVirtualAddressIPv4(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "k8s-api"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Priority: 150, Peers: []string{"10.240.70.3"}},
				Track:     []api.ResourceTrackSpec{{Resource: "BGPRouter/lan", UnhealthyPenalty: 50}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:          64512,
				RouterID:     "10.240.70.2",
				ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.240.70.200/29"}},
				Timers:       api.BGPTimersSpec{Profile: "fast"},
				GracefulRestart: api.BGPGracefulRestartSpec{
					RestartTime:   "120s",
					StalePathTime: "360s",
				},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "k8s"}, Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.240.70.21", "10.240.70.22"},
				Timers:    api.BGPTimersSpec{Profile: "fast"},
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

func TestValidateBGPPeerEbgpMultihopRange(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{ASN: 64512, RouterID: "10.240.70.2"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "remote"}, Spec: api.BGPPeerSpec{
				RouterRef:    "BGPRouter/lan",
				PeerASN:      64513,
				Peers:        []string{"192.0.2.2"},
				EbgpMultihop: 255,
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate eBGP multihop ttl 255: %v", err)
	}
	spec := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	spec.EbgpMultihop = 256
	router.Spec.Resources[1].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.ebgpMultihop must be within 0-255") {
		t.Fatalf("expected eBGP multihop range error, got %v", err)
	}
}

func TestValidateBGPDualStackAndVirtualAddressIPv6(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0", Managed: true}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "k8s-api-v6"}, Spec: api.VirtualAddressSpec{Family: "ipv6",
				Interface: "lan",
				Address:   "fd00:1234::10/128",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 51, Priority: 150, Peers: []string{"fd00:1234::3"}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.240.70.2",
				ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{
					"10.240.70.200/29",
					"fd00:1234::/64",
				}},
				Redistribute: api.BGPRedistributeSpec{
					Connected: api.BGPRedistributeRouteSpec{AllowedPrefixes: []string{"fd00:50::/64"}},
				},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "k8s"}, Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.240.70.21", "fd00:1234::21"},
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate dual-stack BGP/VRRP resources: %v", err)
	}
	spec := router.Spec.Resources[1].Spec.(api.VirtualAddressSpec)
	spec.Address = "fd00:1234::10/64"
	router.Spec.Resources[1].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "IPv6 /128") {
		t.Fatalf("expected IPv6 /128 validation error, got %v", err)
	}
}

func TestValidateHostnameWarnsWithoutDNSResolverZoneCoverage(t *testing.T) {
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
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Hostname:  "k8s-api.lain.local",
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate hostname coverage: %v", err)
	}
	if warnings := Warnings(router); len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	spec := router.Spec.Resources[3].Spec.(api.VirtualAddressSpec)
	spec.Hostname = "k8s_api.lain.local"
	router.Spec.Resources[3].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.hostname is invalid") {
		t.Fatalf("expected invalid hostname error, got %v", err)
	}
	spec.Hostname = "k8s-api.other.local"
	router.Spec.Resources[3].Spec = spec
	if err := Validate(router); err != nil {
		t.Fatalf("uncovered hostname should validate with warning: %v", err)
	}
	if warnings := Warnings(router); len(warnings) != 1 || !strings.Contains(warnings[0], "not covered") {
		t.Fatalf("warnings = %#v, want uncovered hostname warning", warnings)
	}
	spec.ExternalDNS = true
	router.Spec.Resources[3].Spec = spec
	if err := Validate(router); err != nil {
		t.Fatalf("external DNS hostname should validate: %v", err)
	}
	if warnings := Warnings(router); len(warnings) != 0 {
		t.Fatalf("externalDNS warnings = %#v, want none", warnings)
	}
}

func TestValidateBGPTimersRejectsLowLevelFields(t *testing.T) {
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
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected low-level timer validation error, got %v", err)
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
				BFD:       "BFD/fabric-fast",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BFD"}, Metadata: api.ObjectMeta{Name: "fabric-fast"}, Spec: api.BFDSpec{
				Peer:             "BGPPeer/fabric",
				Profile:          "fast",
				MinRx:            "300ms",
				MinTx:            "300ms",
				DetectMultiplier: 3,
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid BFD/watcher config should validate: %v", err)
	}

	bfdSpec := router.Spec.Resources[2].Spec.(api.BFDSpec)
	bfdSpec.MinRx = "10ms"
	router.Spec.Resources[2].Spec = bfdSpec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.minRx") {
		t.Fatalf("expected minRx validation error, got %v", err)
	}
	bfdSpec.MinRx = "300ms"
	bfdSpec.Peer = "BGPPeer/missing"
	router.Spec.Resources[2].Spec = bfdSpec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "does not match this BGPPeer") {
		t.Fatalf("expected mismatched BFD peer validation error, got %v", err)
	}

	bfdSpec.Peer = "BGPPeer/fabric"
	router.Spec.Resources[2].Spec = bfdSpec
	peerSpec := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	peerSpec.BFD = ""
	router.Spec.Resources[1].Spec = peerSpec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "not referenced by any BGPPeer") {
		t.Fatalf("expected dangling BFD validation error, got %v", err)
	}
	peerSpec.BFD = "BFD/fabric-fast"
	router.Spec.Resources[1].Spec = peerSpec
	bfdSpec.Peer = "192.0.2.99"
	router.Spec.Resources[2].Spec = bfdSpec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "does not match this BGPPeer") {
		t.Fatalf("expected BFD peer mismatch validation error, got %v", err)
	}
	bfdSpec.Peer = "BGPPeer/fabric"
	router.Spec.Resources[2].Spec = bfdSpec
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

func TestValidateBGPRouterImportNextHopRewrite(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64512,
				RouterID: "10.240.70.2",
				ImportPolicy: api.BGPImportPolicySpec{
					AllowedPrefixes: []string{"10.250.0.0/24"},
					NextHopRewrite:  "peer-address",
				},
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid nextHopRewrite should validate: %v", err)
	}
	spec := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	spec.ImportPolicy.NextHopRewrite = "third-party"
	router.Spec.Resources[0].Spec = spec
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.importPolicy.nextHopRewrite") {
		t.Fatalf("expected nextHopRewrite validation error, got %v", err)
	}
}

func TestValidateBGPExportPolicy(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:          64512,
				RouterID:     "10.240.70.2",
				ImportPolicy: api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}},
				ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.250.0.0/24"}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "fabric"}, Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64513,
				Peers:     []string{"10.240.70.21"},
				ExportPolicy: api.BGPExportPolicySpec{
					AllowedPrefixes: []string{"10.250.0.0/24"},
				},
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid BGP export policy should validate: %v", err)
	}

	peer := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	peer.ExportPolicy.AllowedPrefixes = []string{"not-a-prefix"}
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.exportPolicy.allowedPrefixes[0]") {
		t.Fatalf("expected peer export prefix validation error, got %v", err)
	}

	peer.ExportPolicy.AllowedPrefixes = []string{"10.250.0.0/24"}
	router.Spec.Resources[1].Spec = peer
	bgp := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	bgp.ExportPolicy.AllowedPrefixes = []string{"10.250.0.0/24", "10.250.0.0/24"}
	router.Spec.Resources[0].Spec = bgp
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected router export duplicate validation error, got %v", err)
	}
}

func TestValidateBGPPeerRouteReflectorRequiresIBGP(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64577,
				RouterID: "10.99.0.1",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "client"}, Spec: api.BGPPeerSpec{
				RouterRef:               "BGPRouter/lan",
				PeerASN:                 64577,
				Peers:                   []string{"10.99.0.2"},
				RouteReflectorClient:    true,
				RouteReflectorClusterID: "10.99.0.1",
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid route reflector client peer should validate: %v", err)
	}
	peer := router.Spec.Resources[1].Spec.(api.BGPPeerSpec)
	peer.PeerASN = 64578
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.routeReflectorClient requires iBGP") {
		t.Fatalf("expected iBGP validation error, got %v", err)
	}
	peer.PeerASN = 64577
	peer.RouteReflectorClusterID = "not-an-ip"
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.routeReflectorClusterID") {
		t.Fatalf("expected cluster ID validation error, got %v", err)
	}
}

func TestValidateBGPDynamicPeerRouteReflectorRequiresIBGP(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64577,
				RouterID: "10.99.0.1",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"}, Metadata: api.ObjectMeta{Name: "leaves"}, Spec: api.BGPDynamicPeerSpec{
				RouterRef:               "BGPRouter/lan",
				PeerASN:                 64577,
				Listen:                  api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"10.255.0.0/20"}},
				RouteReflectorClient:    true,
				RouteReflectorClusterID: "10.99.0.1",
				ImportPolicy: api.BGPImportPolicySpec{
					AllowedPrefixes:        []string{"10.77.60.0/24"},
					AllowedPrefixLengthMin: 32,
					AllowedPrefixLengthMax: 32,
				},
			}},
		}},
	}
	if err := Validate(router); err != nil {
		t.Fatalf("valid dynamic route reflector client should validate: %v", err)
	}
	peer := router.Spec.Resources[1].Spec.(api.BGPDynamicPeerSpec)
	peer.PeerASN = 64578
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.routeReflectorClient requires iBGP") {
		t.Fatalf("expected iBGP validation error, got %v", err)
	}
	peer.PeerASN = 64577
	peer.Listen.SourcePrefixes = nil
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.listen.sourcePrefixes is required") {
		t.Fatalf("expected sourcePrefixes required validation error, got %v", err)
	}
	peer.Listen.SourcePrefixes = []string{"not-a-prefix"}
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.listen.sourcePrefixes[0]") {
		t.Fatalf("expected sourcePrefixes prefix validation error, got %v", err)
	}
}

func TestValidateBGPDynamicPeerRequiresEffectiveImportAllowlist(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64577,
				RouterID: "10.99.0.1",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"}, Metadata: api.ObjectMeta{Name: "leaves"}, Spec: api.BGPDynamicPeerSpec{
				RouterRef: "BGPRouter/lan",
				PeerASN:   64577,
				Listen:    api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"10.255.0.0/20"}},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.importPolicy.allowedPrefixes is required") {
		t.Fatalf("expected dynamic import allowlist validation error, got %v", err)
	}
	peer := router.Spec.Resources[1].Spec.(api.BGPDynamicPeerSpec)
	peer.ImportPolicy = api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}}
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "allowedPrefixLengthMin=32") {
		t.Fatalf("expected broad dynamic import policy validation error, got %v", err)
	}
	peer.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
	}
	router.Spec.Resources[1].Spec = peer
	if err := Validate(router); err != nil {
		t.Fatalf("dynamic peer with exact /32 import allowlist should validate: %v", err)
	}
	peer.ImportPolicy = api.BGPImportPolicySpec{}
	router.Spec.Resources[1].Spec = peer
	bgpr := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	bgpr.ImportPolicy = api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}}
	router.Spec.Resources[0].Spec = bgpr
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "allowedPrefixLengthMin=32") {
		t.Fatalf("expected broad inherited import policy validation error, got %v", err)
	}
	bgpr.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
	}
	router.Spec.Resources[0].Spec = bgpr
	if err := Validate(router); err != nil {
		t.Fatalf("dynamic peer with exact /32 inherited router import allowlist should validate: %v", err)
	}
}

func TestValidateBGPImportPrefixLengthBoundsMatchPrefixFamilyAndBits(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{
				ASN:      64577,
				RouterID: "10.99.0.1",
				ImportPolicy: api.BGPImportPolicySpec{
					AllowedPrefixes:        []string{"10.77.60.0/24"},
					AllowedPrefixLengthMin: 16,
					AllowedPrefixLengthMax: 32,
				},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "allowedPrefixLengthMin must be >= prefix length 24") {
		t.Fatalf("expected min below prefix bits validation error, got %v", err)
	}
	bgpr := router.Spec.Resources[0].Spec.(api.BGPRouterSpec)
	bgpr.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 24,
		AllowedPrefixLengthMax: 33,
	}
	router.Spec.Resources[0].Spec = bgpr
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "allowedPrefixLengthMax must be <= 32") {
		t.Fatalf("expected IPv4 max family validation error, got %v", err)
	}
	bgpr.ImportPolicy = api.BGPImportPolicySpec{
		AllowedPrefixes:        []string{"2001:db8::/64"},
		AllowedPrefixLengthMin: 64,
		AllowedPrefixLengthMax: 128,
	}
	router.Spec.Resources[0].Spec = bgpr
	if err := Validate(router); err != nil {
		t.Fatalf("valid IPv6 prefix length bounds should validate: %v", err)
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

func TestValidateVirtualAddressIPv4RejectsStaticAddressConflict(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "lan-base"}, Spec: api.IPv4StaticAddressSpec{
				Interface: "lan",
				Address:   "10.240.70.10/32",
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
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

func TestValidateVirtualAddressIPv4VRRPRequiresPeers(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 50},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.vrrp.peers is required") {
		t.Fatalf("expected vrrp peers error, got %v", err)
	}
}

func TestValidateVirtualAddressIPv4CARPAllowsEmptyPeers(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vtnet1"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Authentication: "secret"},
			}},
		}},
	}
	if err := ValidateForOS(router, platform.OSFreeBSD); err != nil {
		t.Fatalf("FreeBSD CARP should allow multicast peers omitted: %v", err)
	}
}

func TestValidateVirtualAddressIPv4PreemptDelayRequiresPreempt(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan",
				Address:   "10.240.70.10/32",
				Mode:      "vrrp",
				VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Peers: []string{"10.240.70.3"}, PreemptDelay: "5m"},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected preemptDelay validation error, got %v", err)
	}
}

func TestValidateVirtualAddressIPv4RejectsDuplicateVRIDOnInterface(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "vip-a"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan", Address: "10.240.70.10/32", Mode: "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Peers: []string{"10.240.70.3"}},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "vip-b"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan", Address: "10.240.70.11/32", Mode: "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Peers: []string{"10.240.70.3"}},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "virtualRouterID conflicts") {
		t.Fatalf("expected duplicate VRID validation error, got %v", err)
	}
}

func TestValidateVirtualAddressIPv4RejectsInvalidVRRPPeer(t *testing.T) {
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "vip"}, Spec: api.VirtualAddressSpec{Family: "ipv4",
				Interface: "lan", Address: "10.240.70.10/32", Mode: "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 50, Peers: []string{"bad_peer"}},
			}},
		}},
	}
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "spec.vrrp.peers[0]") {
		t.Fatalf("expected invalid VRRP peer validation error, got %v", err)
	}
}
