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
			name: "remote claim proxy arp missing interface",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Capture = api.AddressCapture{Type: "proxy-arp"}
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.capture.interface is required",
		},
		{
			name: "remote claim bad delivery mode",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Delivery.Mode = "attach"
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.delivery.mode must be route",
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
			name: "remote claim unresolved delivery peer",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[6].Spec.(api.RemoteAddressClaimSpec)
				spec.Delivery.PeerRef = "missing"
				router.Spec.Resources[6].Spec = spec
			},
			want: "spec.delivery.peerRef references missing OverlayPeer",
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

func TestValidateHybridWarnsForExternalWireGuardInterface(t *testing.T) {
	router := validHybridRouter()
	router.Spec.Resources = append(router.Spec.Resources[:0], router.Spec.Resources[1:]...)
	warnings := Warnings(router)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "assuming the interface is managed externally") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestValidateHybridWarnsForExternalCloudProviderProfile(t *testing.T) {
	router := validHybridRouter()
	router.Spec.Resources = append(router.Spec.Resources[:5], router.Spec.Resources[6:]...)
	warnings := Warnings(router)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "assuming the provider profile is managed externally") {
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

func TestHybridAzurePVESameSubnetExampleValidates(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "hybrid-azure-pve-same-subnet.yaml")
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
