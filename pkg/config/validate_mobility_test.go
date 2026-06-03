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
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}},
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
	}, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate MobilityPool: %v", err)
	}
}

func TestValidateMobilityPoolAllowsDiscoveredCloudNICOnlyInBGPDiscoveryMode(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:         "10.88.60.0/24",
		GroupRef:       "cloudedge",
		DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
		Members: []api.MobilityPoolMember{
			{
				NodeRef:  "onprem-router",
				Site:     "onprem",
				Role:     "onprem",
				Capture:  api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan", ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"}},
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
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
				OwnershipDiscovery: api.MobilityOwnershipDiscovery{
					Mode:        "provider-private-ip",
					ProviderRef: "azure-provider",
					SubnetRef:   "/subnets/demo",
					Scope: api.MobilityOwnershipDiscoveryScope{
						IncludePrimary:   boolPtr(false),
						IncludeAddresses: []string{"10.88.60.0/25"},
						ExcludeAddresses: []string{"10.88.60.7"},
					},
				},
			},
		},
		LeasePolicy: api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
	}
	if err := Validate(mobilityPoolRouter(spec, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))); err != nil {
		t.Fatalf("Validate discovered NIC MobilityPool: %v", err)
	}

	spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{}
	if err := Validate(mobilityPoolRouter(spec, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))); err == nil || !strings.Contains(err.Error(), "capture.nicRef is required") {
		t.Fatalf("Validate without discovery err = %v, want nicRef required", err)
	}

	spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", ProviderRef: "azure-provider"}
	spec.DeliveryPolicy.Mode = ""
	if err := Validate(mobilityPoolRouter(spec, testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))); err != nil {
		t.Fatalf("Validate default-BGP discovery err = %v", err)
	}
}

func TestValidateMobilityPoolActiveWhenVirtualAddressReferenceIsLocalToSelfNode(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{
				NodeRef: "onprem-router",
				Site:    "onprem",
				Role:    "onprem",
				Capture: api.MobilityMemberCapture{
					Type:       "proxy-arp",
					Interface:  "lan",
					ActiveWhen: api.CaptureActiveWhen{Type: "vrrp-master", VirtualAddressRef: "onprem-vip"},
				},
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
					NICRef:       "nic-1",
				},
				Delivery: api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route", TunnelInterface: "wg-hybrid"},
			},
		},
		LeasePolicy: api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
	}
	router := mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "azure-router"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate cloud node with non-local onprem VirtualAddress ref: %v", err)
	}
	router = mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "onprem-router"))
	if err := Validate(router); err == nil || !strings.Contains(err.Error(), "references missing VirtualAddress") {
		t.Fatalf("Validate onprem node without local VirtualAddress err = %v", err)
	}
	router = mobilityPoolRouter(spec, testEventGroupResource("cloudedge", "onprem-router"), testInterfaceResource("lan"), testVirtualAddressResource("onprem-vip"))
	if err := Validate(router); err != nil {
		t.Fatalf("Validate onprem node with local VirtualAddress: %v", err)
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

func TestValidateMobilityPoolStaticOwnedAndHandover(t *testing.T) {
	spec := api.MobilityPoolSpec{
		Prefix:   "10.88.60.0/24",
		GroupRef: "cloudedge",
		Members: []api.MobilityPoolMember{
			{NodeRef: "onprem-router", Site: "onprem", Role: "onprem", StaticOwnedAddresses: []string{"10.88.60.10/32"}},
			{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
		},
		StaticHandovers: []api.MobilityStaticHandover{{
			Address:     "10.88.60.10/32",
			FromNodeRef: "onprem-router",
			ToNodeRef:   "azure-router",
		}},
		LeasePolicy: api.MobilityLeasePolicy{TTL: "5m", HoldDuration: "30s"},
	}
	if err := Validate(mobilityPoolRouter(spec)); err != nil {
		t.Fatalf("Validate static mobility pool: %v", err)
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
			name: "delivery policy route mode rejected",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "route"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip"}
			},
			want: "spec.deliveryPolicy.mode \"route\" is not supported; only bgp",
		},
		{
			name: "ownership discovery requires cloud",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[0].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip"}
			},
			want: "ownershipDiscovery is supported only for role cloud",
		},
		{
			name: "ownership discovery scan interval minimum",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{Mode: "provider-private-ip", ScanInterval: "5s"}
			},
			want: "ownershipDiscovery.scanInterval must be >= 30s",
		},
		{
			name: "ownership discovery include address outside pool",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
					Mode: "provider-private-ip",
					Scope: api.MobilityOwnershipDiscoveryScope{
						IncludeAddresses: []string{"10.88.61.1"},
					},
				}
			},
			want: "ownershipDiscovery.scope.includeAddresses[0]",
		},
		{
			name: "ownership discovery exclude aggregate outside pool",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.DeliveryPolicy.Mode = "bgp"
				spec.Members[1].Capture = api.MobilityMemberCapture{Type: "provider-secondary-ip", ProviderRef: "azure-provider", ProviderMode: "nic-secondary-ip", NICRef: "nic-1"}
				spec.Members[1].Delivery = api.MobilityMemberDelivery{PeerRef: "onprem", Mode: "route"}
				spec.Members[1].OwnershipDiscovery = api.MobilityOwnershipDiscovery{
					Mode: "provider-private-ip",
					Scope: api.MobilityOwnershipDiscoveryScope{
						ExcludeAddresses: []string{"10.88.60.0/23"},
					},
				}
			},
			want: "ownershipDiscovery.scope.excludeAddresses[0]",
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
			want: "capture.activeWhen.type must be vrrp-master",
		},
		{
			name: "onprem proxy arp missing activeWhen",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members[0].Capture = api.MobilityMemberCapture{Type: "proxy-arp", Interface: "lan"}
				spec.Members[0].Delivery = api.MobilityMemberDelivery{PeerRef: "azure"}
			},
			want: "capture.activeWhen.type must be vrrp-master",
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
		{
			name: "static owned on cloud",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[1].StaticOwnedAddresses = []string{"10.88.60.20/32"} },
			want: "staticOwnedAddresses is supported only for role onprem",
		},
		{
			name: "static owned outside prefix",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[0].StaticOwnedAddresses = []string{"10.88.61.10/32"} },
			want: "must be within spec.prefix",
		},
		{
			name: "static owned requires host prefix",
			mut:  func(spec *api.MobilityPoolSpec) { spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/24"} },
			want: "must be an IPv4 /32 CIDR",
		},
		{
			name: "static owned duplicate",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.Members = append(spec.Members, api.MobilityPoolMember{NodeRef: "onprem-router-b", Site: "onprem", Role: "onprem", StaticOwnedAddresses: []string{"10.88.60.10/32"}})
				spec.Members[0].StaticOwnedAddresses = []string{"10.88.60.10/32"}
			},
			want: "duplicates staticOwnedAddresses",
		},
		{
			name: "handover from missing",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.StaticHandovers = []api.MobilityStaticHandover{{Address: "10.88.60.10/32", FromNodeRef: "missing", ToNodeRef: "azure-router"}}
			},
			want: "fromNodeRef \"missing\" must be one of the member nodeRefs",
		},
		{
			name: "handover from must be onprem",
			mut: func(spec *api.MobilityPoolSpec) {
				spec.StaticHandovers = []api.MobilityStaticHandover{{Address: "10.88.60.10/32", FromNodeRef: "azure-router", ToNodeRef: "onprem-router"}}
			},
			want: "must reference an onprem member",
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

func mobilityPoolRouter(spec api.MobilityPoolSpec, extra ...api.Resource) *api.Router {
	resources := []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
		Metadata: api.ObjectMeta{Name: "cloudedge"},
		Spec:     spec,
	}}
	resources = append(resources, extra...)
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec:     api.RouterSpec{Resources: resources},
	}
}

func testVirtualAddressResource(name string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.VirtualAddressSpec{
			Family:    "ipv4",
			Interface: "lan",
			Address:   "10.88.60.1/32",
			Mode:      "vrrp",
			VRRP:      api.VirtualAddressVRRPSpec{VirtualRouterID: 60, Peers: []string{"10.88.60.2"}},
		},
	}
}

func testEventGroupResource(name, nodeName string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.EventGroupSpec{
			NodeName: nodeName,
			Auth:     api.EventGroupAuth{Mode: "hmac", SecretFile: "/run/routerd/event.key"},
		},
	}
}

func testInterfaceResource(name string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.InterfaceSpec{IfName: name, Managed: true},
	}
}
