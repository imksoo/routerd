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
			name: "cloud claim missing providerRef",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[4].Spec.(api.CloudAddressClaimSpec)
				spec.ProviderRef = ""
				router.Spec.Resources[4].Spec = spec
			},
			want: "spec.providerRef is required",
		},
		{
			name: "cloud claim bad address",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[4].Spec.(api.CloudAddressClaimSpec)
				spec.Address = "not-an-ip"
				router.Spec.Resources[4].Spec = spec
			},
			want: "spec.address: must be an IP address or CIDR",
		},
		{
			name: "cloud claim unknown attachment type",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[4].Spec.(api.CloudAddressClaimSpec)
				spec.CloudAttachment.Type = "primary-private-ip"
				router.Spec.Resources[4].Spec = spec
			},
			want: "spec.cloudAttachment.type must be secondary-private-ip",
		},
		{
			name: "cloud claim bad delivery mode",
			mutate: func(router *api.Router) {
				spec := router.Spec.Resources[4].Spec.(api.CloudAddressClaimSpec)
				spec.Delivery.Mode = "attach"
				router.Spec.Resources[4].Spec = spec
			},
			want: "spec.delivery.mode must be route",
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
			testResource(api.HybridAPIVersion, "CloudAddressClaim", "app-10-0-1-123", api.CloudAddressClaimSpec{
				ProviderRef: "oci-prod",
				Address:     "10.0.1.123/32",
				CloudAttachment: api.CloudAttachment{
					Type:   "secondary-private-ip",
					VNICID: "ocid1.vnic.oc1..example",
				},
				Delivery: api.CloudDelivery{
					PeerRef:       "cloud-main",
					Mode:          "route",
					TargetAddress: "169.254.100.2",
				},
			}),
		}},
	}
}
