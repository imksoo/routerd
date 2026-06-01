// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestValidateMobilityPool(t *testing.T) {
	router := mobilityPoolRouter(api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{
				NodeRef:  "onprem-router",
				Site:     "onprem",
				Role:     "onprem",
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"},
				Delivery: api.MobilityMemberDelivery{PeerRef: "azure", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
			{
				NodeRef: "azure-router",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/router-nic",
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
		},
		LeasePolicy: api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
		Authority:   api.MobilityAuthority{Mode: "static"},
	})
	if err := Validate(router); err != nil {
		t.Fatalf("Validate MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolPlacement(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
			{
				NodeRef: "azure-router-a",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "nic-a",
				},
				Delivery:    api.MobilityMemberDelivery{PeerRef: "onprem"},
				Placement:   api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10},
				Maintenance: api.MobilityMemberMaintenance{Drain: true},
			},
			{
				NodeRef: "azure-router-b",
				Site:    "azure",
				Role:    "cloud",
				Capture: api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "nic-b",
				},
				Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem"},
				Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
			},
		},
	}
	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate placement MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*api.MobilityPoolSpec)
		want string
	}{
		{
			name: "missing group",
			mut:  func(spec *api.MobilityPoolSpec) { spec.GroupRef = "" },
			want: "spec.groupRef is required",
		},
		{
			name: "ipv6 prefix",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Prefix = "2001:db8::/64" },
			want: "spec.prefix must be an IPv4 CIDR",
		},
		{
			name: "missing role",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[0].Role = "" },
			want: "role must be onprem or cloud",
		},
		{
			name: "bad hold",
			mut:  func(spec *api.MobilityPoolSpec) { spec.LeasePolicy.HoldDuration = "-1s" },
			want: "holdDuration must be >= 0",
		},
		{
			name: "bad deprovision hold",
			mut:  func(spec *api.MobilityPoolSpec) { spec.CapturePolicy.DeprovisionHoldDuration = "-1s" },
			want: "deprovisionHoldDuration must be >= 0",
		},
		{
			name: "placement priority without group",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[1].Placement.Priority = 10 },
			want: "placement.priority requires placement.group",
		},
		{
			name: "drain without placement",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[1].Maintenance.Drain = true },
			want: "maintenance.drain requires placement.group",
		},
		{
			name: "placement priority range",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem"}
				spec.Members[1].Placement = api.MobilityMemberPlacement{Group: "azure-edge", Priority: -1}
			},
			want: "placement.priority must be between 0 and 1000000",
		},
		{
			name: "placement role",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
				spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "azure"}
				spec.Members[0].Placement = api.MobilityMemberPlacement{Group: "onprem-edge", Priority: 10}
			},
			want: "placement.group is supported only for role cloud",
		},
		{
			name: "placement group provider mismatch",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members = append(spec.Members, api.MobilityPoolMember{
					NodeRef: "azure-router-b",
					Site:    "azure",
					Role:    "cloud",
					Capture: api.MobilityMemberCapture{
						Type:         "provider-secondary-ip",
						ProviderRef:  "other-provider",
						ProviderMode: "nic-secondary-ip",
						NICRef:       "nic-2",
					},
					Delivery:  api.MobilityMemberDelivery{PeerRef: "onprem"},
					Placement: api.MobilityMemberPlacement{Group: "azure-edge", Priority: 20},
				})
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem"}
				spec.Members[1].Placement = api.MobilityMemberPlacement{Group: "azure-edge", Priority: 10}
			},
			want: "must use one providerRef",
		},
		{
			name: "unknown authority node",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Authority.NodeRef = "other" },
			want: "must be one of the member nodeRefs",
		},
		{
			name: "bad ownership policy type",
			mut:  func(spec *api.MobilityPoolSpec) { spec.IPOwnershipPolicy.Type = "lock-service" },
			want: "spec.ipOwnershipPolicy.type",
		},
		{
			name: "ownership prefer missing node",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", PreferNodes: []string{"missing-router"}}
			},
			want: "spec.ipOwnershipPolicy.preferNodes[0]",
		},
		{
			name: "ownership prefer duplicate node",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", PreferNodes: []string{"azure-router", "azure-router"}}
			},
			want: "contains duplicate nodeRef",
		},
		{
			name: "auto failover missing heartbeat interval",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", AutoFailover: true, HeartbeatTTL: "30s"}
			},
			want: "heartbeatInterval is required when autoFailover is true",
		},
		{
			name: "auto failover missing heartbeat ttl",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", AutoFailover: true, HeartbeatInterval: "10s"}
			},
			want: "heartbeatTTL is required when autoFailover is true",
		},
		{
			name: "heartbeat ttl below interval",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", AutoFailover: true, HeartbeatInterval: "30s", HeartbeatTTL: "10s"}
			},
			want: "heartbeatTTL must be >= heartbeatInterval",
		},
		{
			name: "bad promotion hold",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.IPOwnershipPolicy = api.MobilityIPOwnershipPolicy{Type: "centralized", PromotionHoldDuration: "-1s"}
			},
			want: "promotionHoldDuration must be >= 0",
		},
		{
			name: "cloud capture type",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem"}
			},
			want: "capture.type must be provider-secondary-ip for role cloud",
		},
		{
			name: "capture needs delivery",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
			},
			want: "delivery.peerRef or deliveryTo is required when capture.type is set",
		},
		{
			name: "deliveryTo selector",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
				spec.Members[0].DeliveryTo = []api.MobilityMemberDeliveryTarget{{PeerRef: "azure"}}
			},
			want: "must set nodeRef, site, or role",
		},
		{
			name: "secret target",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[1].Capture = api.MobilityMemberCapture{
					Type:         "provider-secondary-ip",
					ProviderRef:  "azure-provider",
					ProviderMode: "nic-secondary-ip",
					NICRef:       "nic-1",
					Target:       map[string]string{"accessToken": "nope"},
				}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem"}
			},
			want: "looks secret-like",
		},
		{
			name: "activeWhen missing ref",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master"}}
				spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "azure"}
			},
			want: "capture.activeWhen.virtualAddressRef is required",
		},
		{
			name: "activeWhen unresolved virtual address",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}}
				spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "azure"}
			},
			want: "references missing VirtualAddress",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := api.MobilityPoolSpec{
				Prefix:   "10.88.60.0/24",
				GroupRef: "cloudedge",
				Members: []api.MobilityPoolMember{
					{NodeRef: "onprem-router", Site: "onprem", Role: "onprem"},
					{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
				},
				LeasePolicy: api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
			}
			tt.mut(&spec)
			err := Validate(mobilityPoolRouter(spec))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func mobilityPoolRouter(spec api.MobilityPoolSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "cloudedge"},
			Spec:     spec,
		}}},
	}
}
