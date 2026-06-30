// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestValidateHybridResources(t *testing.T) {
	router := validHybridRouter()
	if err := Validate(router); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateHybridRemoteClaimConfigureOSAddress(t *testing.T) {
	router := validHybridRouter()
	spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ConfigureOSAddress = true
	spec.Capture.Interface = "ens5"
	router.Spec.Resources[6].Spec = spec
	if err := Validate(router); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateTunnelInterfaceResources(t *testing.T) {
	router := validTunnelHybridRouter()
	if err := Validate(router); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateTunnelInterfaceEndpointSources(t *testing.T) {
	router := validTunnelHybridRouter()
	spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Local = ""
	spec.LocalFrom = api.StatusValueSourceSpec{Resource: "Interface/eth0-status", Field: "primaryIPv4"}
	spec.Remote = ""
	spec.RemoteFrom = api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/remote-underlay", Field: "address"}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, testResource(api.NetAPIVersion, "IPv4StaticAddress", "remote-underlay", api.IPv4StaticAddressSpec{
		Interface: "eth0",
		Address:   "192.0.2.20/24",
	}), testResource(api.NetAPIVersion, "Interface", "eth0-status", api.InterfaceSpec{
		IfName:  "eth0",
		Managed: false,
	}), testResource(api.NetAPIVersion, "Interface", "eth0", api.InterfaceSpec{
		IfName: "eth0",
	}))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateHybridFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*api.Router)
		want   string
	}{
		{
			name: "bad role",
			mutate: func(router *api.Router) {
				router.Spec.Resources[1].Spec = api.OverlayPeerSpec{Role: "edge", NodeID: "cloud-1", Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-hybrid"}}
			},
			want: "spec.role must be onprem or cloud",
		},
		{
			name: "default route rejected",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[3].Spec.(api.HybridRouteSpec)
				spec.DestinationCIDRs = []string{"default"}
				router.Spec.Resources[3].Spec = spec
			},
			want: "default routes are not allowed",
		},
		{
			name: "ipv4 default route rejected",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[3].Spec.(api.HybridRouteSpec)
				spec.DestinationCIDRs = []string{"0.0.0.0/0"}
				router.Spec.Resources[3].Spec = spec
			},
			want: "default routes are not allowed",
		},
		{
			name: "ipv6 default route rejected",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[3].Spec.(api.HybridRouteSpec)
				spec.DestinationCIDRs = []string{"::/0"}
				router.Spec.Resources[3].Spec = spec
			},
			want: "default routes are not allowed",
		},
		{
			name: "non-main table rejected",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[3].Spec.(api.HybridRouteSpec)
				spec.Install.Table = "cloud"
				router.Spec.Resources[3].Spec = spec
			},
			want: "spec.install.table must be empty or main",
		},
		{
			name: "missing peerRef",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[3].Spec.(api.HybridRouteSpec)
				spec.PeerRef = ""
				router.Spec.Resources[3].Spec = spec
			},
			want: "spec.peerRef is required",
		},
		{
			name: "force fragment unsupported underlay rejected",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[1].Spec.(api.OverlayPeerSpec)
				spec.Underlay.Type = "route"
				spec.Underlay.Interface = ""
				spec.PathMTU.ForceFragmentIPv4 = true
				router.Spec.Resources[1].Spec = spec
			},
			want: "spec.pathMTU.forceFragmentIPv4 is supported only for underlay.type wireguard, ipip, gre, fou, or gue",
		},
		{
			name: "unresolved peerRef",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[3].Spec.(api.HybridRouteSpec)
				spec.PeerRef = "missing"
				router.Spec.Resources[3].Spec = spec
			},
			want: "spec.peerRef references missing OverlayPeer",
		},
		{
			name: "address mobility domain full l2 rejected",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[4].Spec.(api.AddressMobilityDomainSpec)
				spec.Mode = "full-l2"
				router.Spec.Resources[4].Spec = spec
			},
			want: "full L2 extension is not supported; routerd implements Selective Address Mobility",
		},
		{
			name: "address mobility domain ipv6 rejected",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[4].Spec.(api.AddressMobilityDomainSpec)
				spec.Prefix = "2001:db8::/64"
				router.Spec.Resources[4].Spec = spec
			},
			want: "spec.prefix: must be an IPv4 CIDR",
		},
		{
			name: "cloud provider profile unknown provider",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[5].Spec.(api.CloudProviderProfileSpec)
				spec.Provider = "do"
				router.Spec.Resources[5].Spec = spec
			},
			want: "spec.provider must be azure, aws, oci, or gcp",
		},
		{
			name: "cloud provider profile empty capabilities",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[5].Spec.(api.CloudProviderProfileSpec)
				spec.Capabilities = nil
				router.Spec.Resources[5].Spec = spec
			},
			want: "spec.capabilities is required",
		},
		{
			name: "cloud provider profile external command required",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[5].Spec.(api.CloudProviderProfileSpec)
				spec.Auth.Command = ""
				router.Spec.Resources[5].Spec = spec
			},
			want: "spec.auth.command is required",
		},
		{
			name: "remote claim missing domainRef",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.DomainRef = ""
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.domainRef is required",
		},
		{
			name: "remote claim address must be /32",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Address = "10.0.1.0/24"
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.address: must be an IPv4 /32 CIDR",
		},
		{
			name: "remote claim unsupported capture type",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.Type = "garp"
				router.Spec.Resources[6].Spec = spec
			},
			want: "reserved/not implemented in MVP",
		},
		{
			name: "remote claim provider capture missing providerRef",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.ProviderRef = ""
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.providerRef is required",
		},
		{
			name: "remote claim provider capture missing providerMode",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.ProviderMode = ""
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.providerMode is required",
		},
		{
			name: "remote claim provider capture missing nicRef",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.NICRef = ""
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.nicRef is required",
		},
		{
			name: "remote claim provider configure OS address missing interface",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.ConfigureOSAddress = true
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.interface is required when spec.capture.configureOSAddress is true",
		},
		{
			name: "remote claim proxy arp missing interface",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture = api.AddressCapture{Type: "proxy-arp"}
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.interface is required",
		},
		{
			name: "remote claim activeWhen missing ref",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master"}
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.activeWhen.virtualAddressRef is required",
		},
		{
			name: "remote claim single-router activeWhen rejects ref",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "single-router", VirtualAddressRef: "onprem-vip"}
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.activeWhen.virtualAddressRef must be empty",
		},
		{
			name: "remote claim unresolved activeWhen virtual address",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.ActiveWhen = api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.activeWhen.virtualAddressRef references missing VirtualAddress",
		},
		{
			name: "remote claim bad delivery mode",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Delivery.Mode = "attach"
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.delivery.mode must be route or bgp",
		},
		{
			name: "remote claim unresolved domain",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.DomainRef = "missing"
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.domainRef references missing AddressMobilityDomain",
		},
		{
			name: "remote claim address outside domain",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Address = "10.1.0.9/32"
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.address \"10.1.0.9/32\" is outside AddressMobilityDomain",
		},
		{
			name: "remote claim unresolved delivery peer",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Delivery.PeerRef = "missing"
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.delivery.peerRef references missing OverlayPeer",
		},
		{
			name: "address mobility domain unresolved peer",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[4].Spec.(api.AddressMobilityDomainSpec)
				spec.PeerRef = "missing"
				router.Spec.Resources[4].Spec = spec
			},
			want: "spec.peerRef references missing OverlayPeer",
		},
		{
			name: "remote claim unresolved provider profile",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture.ProviderRef = "missing"
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.providerRef references missing CloudProviderProfile",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := validHybridRouter()
			tt.mutate(router)
			err := Validate(router)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateTunnelInterfaceFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*api.Router)
		want   string
	}{
		{
			name: "trusted underlay required",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.TrustedUnderlay = false
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.trustedUnderlay must be true",
		},
		{
			name: "local required",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Local = ""
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.local or spec.localFrom is required",
		},
		{
			name: "remote required",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Remote = ""
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.remote or spec.remoteFrom is required",
		},
		{
			name: "local and localFrom conflict",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.LocalFrom = api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/tun-gre-address", Field: "address"}
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.local and spec.localFrom are mutually exclusive",
		},
		{
			name: "localFrom requires field",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Local = ""
				spec.LocalFrom = api.StatusValueSourceSpec{Resource: "IPv4StaticAddress/tun-gre-address"}
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.localFrom.field is required",
		},
		{
			name: "mode invalid",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Mode = "vxlan"
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.mode must be ipip, gre, fou, or gue",
		},
		{
			name: "fou requires encap ports",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Mode = "fou"
				spec.Key = 0
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.encapSport is required",
		},
		{
			name: "encap ports require fou or gue",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Mode = "ipip"
				spec.Key = 0
				spec.EncapSport = 5555
				spec.EncapDport = 5555
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.encapSport/spec.encapDport are only supported when spec.mode is fou or gue",
		},
		{
			name: "key requires gre",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
				spec.Mode = "ipip"
				spec.Key = 10
				router.Spec.Resources[0].Spec = spec
			},
			want: "spec.key is only supported when spec.mode is gre",
		},
		{
			name: "overlay peer tunnel reference required",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[1].Spec.(api.OverlayPeerSpec)
				spec.Underlay.Interface = "missing"
				router.Spec.Resources[1].Spec = spec
			},
			want: "spec.underlay.interface references missing TunnelInterface",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := validTunnelHybridRouter()
			tt.mutate(router)
			err := Validate(router)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateHybridWarnsForExternalWireGuardInterface(t *testing.T) {
	router := validHybridRouter()
	router.Spec.Resources = append(router.Spec.Resources[:0], router.Spec.Resources[1:]...)
	warnings := Warnings(router)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "assuming the interface is managed externally") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestValidateHybridWarnsForMissingTunnelInterface(t *testing.T) {
	router := validTunnelHybridRouter()
	router.Spec.Resources = router.Spec.Resources[1:]
	warnings := Warnings(router)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "references TunnelInterface") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestValidateHybridWarnsForProviderModeNotDeclared(t *testing.T) {
	router := validHybridRouter()
	spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ProviderMode = "floating-private-ip"
	router.Spec.Resources[6].Spec = spec
	if err := Validate(router); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	warnings := Warnings(router)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `spec.capture.providerMode "floating-private-ip" is not declared`) {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestValidateHybridWarnsForExternalProxyARPInterface(t *testing.T) {
	router := validHybridRouter()
	spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture = api.AddressCapture{Type: "proxy-arp", Interface: "br-lan", ActiveWhen: api.CaptureActiveWhen{Type: "single-router"}}
	router.Spec.Resources[6].Spec = spec
	if err := Validate(router); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	warnings := Warnings(router)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "assuming the interface is managed externally") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestValidateHybridProxyARPInterfaceWarningAcceptsInterfaceIfName(t *testing.T) {
	router := validHybridRouter()
	spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture = api.AddressCapture{Type: "proxy-arp", Interface: "br-lan"}
	router.Spec.Resources[6].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, testResource(api.NetAPIVersion, "Interface", "lan", api.InterfaceSpec{IfName: "br-lan"}))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if warnings := Warnings(router); len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestHybridExampleValidates(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "hybrid-l3-wireguard.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("example missing: %v", err)
	}
	router, err := Load(path)
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate example: %v", err)
	}
}

func TestCloudInventoryPluginExampleValidates(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "cloud-inventory-plugin.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("example missing: %v", err)
	}
	router, err := Load(path)
	if err != nil {
		t.Fatalf("load example: %v", err)
	}
	if err := Validate(router); err != nil {
		t.Fatalf("validate example: %v", err)
	}
}

func TestHybridAzurePVESameSubnetExamplesValidate(t *testing.T) {
	for _, name := range []string{
		"hybrid-azure-pve-same-subnet-cloud.yaml",
		"hybrid-azure-pve-same-subnet-onprem.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "examples", name)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("example missing: %v", err)
			}
			router, err := Load(path)
			if err != nil {
				t.Fatalf("load example: %v", err)
			}
			if err := Validate(router); err != nil {
				t.Fatalf("validate example: %v", err)
			}
		})
	}
}

func validHybridRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			testResource(api.NetAPIVersion, "WireGuardInterface", "wg-hybrid", api.WireGuardInterfaceSpec{ListenPort: 51820, MTU: 1420}),
			testResource(api.HybridAPIVersion, "OverlayPeer", "cloud-main", api.OverlayPeerSpec{
				Role:   "cloud",
				NodeID: "cloud-1",
				Underlay: api.OverlayUnderlay{
					Type:      "wireguard",
					Interface: "wg-hybrid",
					Address:   "192.0.2.10",
				},
				Remote: api.OverlayRemote{NodeID: "onprem-1", Address: "198.51.100.10"},
			}),
			testResource(api.NetAPIVersion, "HealthCheck", "cloud-health", api.HealthCheckSpec{Target: "10.20.0.1", Protocol: "icmp"}),
			testResource(api.HybridAPIVersion, "HybridRoute", "cloud-lan", api.HybridRouteSpec{
				DestinationCIDRs: []string{"10.20.0.0/16"},
				PeerRef:          "cloud-main",
				Install:          api.HybridRouteInstall{Table: "main", Metric: 120},
				HealthCheckRef:   "cloud-health",
			}),
			testResource(api.HybridAPIVersion, "AddressMobilityDomain", "cloudedge-same-subnet", api.AddressMobilityDomainSpec{
				Prefix:  "10.0.1.0/24",
				Mode:    "selective-address",
				PeerRef: "cloud-main",
			}),
			testResource(api.HybridAPIVersion, "CloudProviderProfile", "oci-prod", api.CloudProviderProfileSpec{
				Provider:       "oci",
				SubscriptionID: "ocid1.tenancy.oc1..example",
				ResourceGroup:  "compartment-a",
				Capabilities:   []string{"vnic-private-ip", "disable-source-dest-check"},
				Auth:           api.ProviderAuth{Mode: "external-command", Command: "/usr/local/libexec/routerd/plugins/oci-auth"},
			}),
			testResource(api.HybridAPIVersion, "RemoteAddressClaim", "app-10-0-1-123", api.RemoteAddressClaimSpec{
				DomainRef: "cloudedge-same-subnet",
				Address:   "10.0.1.123/32",
				OwnerSide: "cloud",
				Capture: api.AddressCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "oci-prod",
					ProviderMode: "vnic-private-ip",
					NICRef:       "ocid1.vnic.oc1..example",
				},
				Delivery: api.AddressDelivery{
					PeerRef:         "cloud-main",
					Mode:            "route",
					TunnelInterface: "wg-hybrid",
				},
			}),
		}},
	}
}

func validTunnelHybridRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			testResource(api.HybridAPIVersion, "TunnelInterface", "tun-gre", api.TunnelInterfaceSpec{
				Mode:            "gre",
				Local:           "192.0.2.10",
				Remote:          "192.0.2.20",
				Address:         "10.99.0.1/32",
				MTU:             1472,
				TTL:             64,
				Key:             42,
				TrustedUnderlay: true,
			}),
			testResource(api.HybridAPIVersion, "OverlayPeer", "edge-main", api.OverlayPeerSpec{
				Role:     "cloud",
				NodeID:   "edge-1",
				Underlay: api.OverlayUnderlay{Type: "gre", Interface: "tun-gre", Address: "10.99.0.2"},
			}),
			testResource(api.HybridAPIVersion, "HybridRoute", "edge-lan", api.HybridRouteSpec{
				DestinationCIDRs: []string{"10.20.0.0/16"},
				PeerRef:          "edge-main",
			}),
			testResource(api.NetAPIVersion, "IPv4StaticAddress", "tun-gre-address", api.IPv4StaticAddressSpec{
				Interface: "tun-gre",
				Address:   "10.99.0.1/32",
			}),
		}},
	}
}
