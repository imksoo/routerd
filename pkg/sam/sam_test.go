// SPDX-License-Identifier: BSD-3-Clause

package sam

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestExpandRemoteAddressClaimRoutes(t *testing.T) {
	router := testRouter()
	expanded, lowerings, err := ExpandRemoteAddressClaimRoutes(*router)
	if err != nil {
		t.Fatalf("ExpandRemoteAddressClaimRoutes: %v", err)
	}
	if len(lowerings) != 2 {
		t.Fatalf("lowerings = %#v", lowerings)
	}
	routes := ipv4Routes(expanded)
	if len(routes) != 2 {
		t.Fatalf("routes = %#v", routes)
	}
	proxyRoute := findRoute(t, routes, "sam-app-10-0-1-123-delivery")
	spec, err := proxyRoute.IPv4RouteSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Destination != "10.0.1.123/32" || spec.Device != "wg-sam" || spec.Metric != DeliveryRouteMetricDefault || spec.Type != "unicast" {
		t.Fatalf("route = %#v spec=%#v", proxyRoute, spec)
	}
	if len(proxyRoute.Metadata.OwnerRefs) != 1 || proxyRoute.Metadata.OwnerRefs[0].Kind != "RemoteAddressClaim" || proxyRoute.Metadata.OwnerRefs[0].Name != "app-10-0-1-123" {
		t.Fatalf("ownerRefs = %#v", proxyRoute.Metadata.OwnerRefs)
	}
	providerRoute := findRoute(t, routes, "sam-provider-10-0-1-122-delivery")
	spec, err = providerRoute.IPv4RouteSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Device != "wg-sam" {
		t.Fatalf("provider-secondary-ip delivery device = %q, want resolved OverlayPeer interface", spec.Device)
	}
}

func TestExpandRemoteAddressClaimRoutesNoClaimsUnchanged(t *testing.T) {
	router := api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "static"}, Spec: api.IPv4RouteSpec{Destination: "192.0.2.0/24", Device: "eth0"}},
	}}}
	expanded, lowerings, err := ExpandRemoteAddressClaimRoutes(router)
	if err != nil {
		t.Fatalf("ExpandRemoteAddressClaimRoutes: %v", err)
	}
	if len(lowerings) != 0 {
		t.Fatalf("lowerings = %#v", lowerings)
	}
	if !reflect.DeepEqual(expanded.Spec.Resources, router.Spec.Resources) {
		t.Fatalf("resources changed: got %#v want %#v", expanded.Spec.Resources, router.Spec.Resources)
	}
}

func TestExpandRemoteAddressClaimRoutesRejectsUserRouteCollision(t *testing.T) {
	router := testRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
		Metadata: api.ObjectMeta{Name: "manual"},
		Spec:     api.IPv4RouteSpec{Destination: "10.0.1.123/32", Device: "eth0"},
	})
	_, _, err := ExpandRemoteAddressClaimRoutes(*router)
	if err == nil || !strings.Contains(err.Error(), "collides with user IPv4Route") {
		t.Fatalf("error = %v", err)
	}
}

func TestPlanCaptureProxyARP(t *testing.T) {
	actions, err := PlanCapture(testRouter(), platform.OSLinux)
	if err != nil {
		t.Fatalf("PlanCapture: %v", err)
	}
	if !hasAction(actions, "sysctl", "net.ipv4.ip_forward", "", "") {
		t.Fatalf("actions missing ip_forward: %#v", actions)
	}
	if !hasAction(actions, "sysctl", "net.ipv4.conf.lan0.proxy_arp", "", "lan0") {
		t.Fatalf("actions missing proxy_arp: %#v", actions)
	}
	if !hasAction(actions, "proxy-neighbor", "", "10.0.1.123/32", "lan0") {
		t.Fatalf("actions missing proxy neighbor: %#v", actions)
	}
}

func TestPlanCaptureProviderSecondaryIPDeassignsOSAddress(t *testing.T) {
	router := testRouter()
	router.Spec.Resources = router.Spec.Resources[:4]
	actions, err := PlanCapture(router, platform.OSLinux)
	if err != nil {
		t.Fatalf("PlanCapture: %v", err)
	}
	for _, action := range actions {
		if action.Kind == "proxy-neighbor" || action.Kind == "local-address" {
			t.Fatalf("unexpected action for provider-secondary-ip: %#v", actions)
		}
	}
	if !hasAction(actions, "deassign-os-address", "", "10.0.1.122/32", "") {
		t.Fatalf("actions missing OS address deassign: %#v", actions)
	}
	if !hasAction(actions, "sysctl", "net.ipv4.ip_forward", "", "") {
		t.Fatalf("actions missing ip_forward: %#v", actions)
	}
}

func TestPlanCaptureProviderSecondaryIPConfigureOSAddressTrueSkipsDeassign(t *testing.T) {
	router := testRouter()
	router.Spec.Resources = router.Spec.Resources[:4]
	spec := router.Spec.Resources[3].Spec.(api.RemoteAddressClaimSpec)
	spec.Capture.ConfigureOSAddress = true
	router.Spec.Resources[3].Spec = spec
	actions, err := PlanCapture(router, platform.OSLinux)
	if err != nil {
		t.Fatalf("PlanCapture: %v", err)
	}
	if hasAction(actions, "deassign-os-address", "", "10.0.1.122/32", "") {
		t.Fatalf("unexpected OS address deassign: %#v", actions)
	}
}

func TestPlanCaptureProviderSecondaryIPDeassignPlanningStable(t *testing.T) {
	router := testRouter()
	router.Spec.Resources = router.Spec.Resources[:4]
	first, err := PlanCapture(router, platform.OSLinux)
	if err != nil {
		t.Fatalf("PlanCapture first: %v", err)
	}
	second, err := PlanCapture(router, platform.OSLinux)
	if err != nil {
		t.Fatalf("PlanCapture second: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("actions changed between runs:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func TestPlanCaptureProxyARPDoesNotDeassignOSAddress(t *testing.T) {
	router := testRouter()
	router.Spec.Resources = append(router.Spec.Resources[:3], router.Spec.Resources[4])
	actions, err := PlanCapture(router, platform.OSLinux)
	if err != nil {
		t.Fatalf("PlanCapture: %v", err)
	}
	for _, action := range actions {
		if action.Kind == "deassign-os-address" {
			t.Fatalf("unexpected OS address deassign for proxy-arp: %#v", actions)
		}
	}
}

func TestPlanCaptureNoClaimsAndNonLinuxNoActions(t *testing.T) {
	noClaim := &api.Router{}
	actions, err := PlanCapture(noClaim, platform.OSLinux)
	if err != nil {
		t.Fatalf("PlanCapture no claim: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("no-claim actions = %#v, want none", actions)
	}
	actions, err = PlanCapture(testRouter(), platform.OSFreeBSD)
	if err != nil {
		t.Fatalf("PlanCapture non-linux: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("non-linux actions = %#v, want none", actions)
	}
}

func TestHybridAzurePVESameSubnetExamplesLowerDeliveryRoutes(t *testing.T) {
	tests := []struct {
		name        string
		routeName   string
		destination string
	}{
		{
			name:        "hybrid-azure-pve-same-subnet-cloud.yaml",
			routeName:   "sam-onprem-vm-10-0-0-9-delivery",
			destination: "10.0.0.9/32",
		},
		{
			name:        "hybrid-azure-pve-same-subnet-onprem.yaml",
			routeName:   "sam-cloud-vm-10-0-0-7-delivery",
			destination: "10.0.0.7/32",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, err := config.Load(filepath.Join("..", "..", "examples", tt.name))
			if err != nil {
				t.Fatalf("load example: %v", err)
			}
			if err := config.Validate(router); err != nil {
				t.Fatalf("validate example: %v", err)
			}
			expanded, lowerings, err := ExpandRemoteAddressClaimRoutes(*router)
			if err != nil {
				t.Fatalf("ExpandRemoteAddressClaimRoutes: %v", err)
			}
			if len(lowerings) != 1 {
				t.Fatalf("lowerings = %#v, want one", lowerings)
			}
			route := findRoute(t, ipv4Routes(expanded), tt.routeName)
			spec, err := route.IPv4RouteSpec()
			if err != nil {
				t.Fatal(err)
			}
			if spec.Destination != tt.destination || spec.Device != "wg-hybrid" || spec.Metric != DeliveryRouteMetricDefault {
				t.Fatalf("route %s spec = %#v", route.Metadata.Name, spec)
			}
		})
	}
}

func hasAction(actions []CaptureAction, kind, key, address, iface string) bool {
	for _, action := range actions {
		if action.Kind != kind {
			continue
		}
		if key != "" && action.Key != key {
			continue
		}
		if address != "" && action.Address != address {
			continue
		}
		if iface != "" && action.Interface != iface {
			continue
		}
		return true
	}
	return false
}

func ipv4Routes(router api.Router) []api.Resource {
	var routes []api.Resource
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "IPv4Route" {
			routes = append(routes, resource)
		}
	}
	return routes
}

func findRoute(t *testing.T, routes []api.Resource, name string) api.Resource {
	t.Helper()
	for _, route := range routes {
		if route.Metadata.Name == name {
			return route
		}
	}
	t.Fatalf("missing route %s in %#v", name, routes)
	return api.Resource{}
}

func testRouter() *api.Router {
	return &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "cloud-main"}, Spec: api.OverlayPeerSpec{
			Role:     "cloud",
			NodeID:   "cloud-1",
			Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-sam"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "AddressMobilityDomain"}, Metadata: api.ObjectMeta{Name: "same-subnet"}, Spec: api.AddressMobilityDomainSpec{
			Prefix:  "10.0.1.0/24",
			Mode:    "selective-address",
			PeerRef: "cloud-main",
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"}, Metadata: api.ObjectMeta{Name: "azure"}, Spec: api.CloudProviderProfileSpec{
			Provider:     "azure",
			Capabilities: []string{"nic-secondary-ip"},
			Auth:         api.ProviderAuth{Mode: "external-command", Command: "/bin/true"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"}, Metadata: api.ObjectMeta{Name: "provider-10-0-1-122"}, Spec: api.RemoteAddressClaimSpec{
			DomainRef: "same-subnet",
			Address:   "10.0.1.122/32",
			OwnerSide: "cloud",
			Capture: api.AddressCapture{
				Type:         "provider-secondary-ip",
				ProviderRef:  "azure",
				ProviderMode: "nic-secondary-ip",
				NICRef:       "nic0",
			},
			Delivery: api.AddressDelivery{PeerRef: "cloud-main", Mode: "route"},
		}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"}, Metadata: api.ObjectMeta{Name: "app-10-0-1-123"}, Spec: api.RemoteAddressClaimSpec{
			DomainRef: "same-subnet",
			Address:   "10.0.1.123/32",
			OwnerSide: "onprem",
			Capture:   api.AddressCapture{Type: "proxy-arp", Interface: "lan0"},
			Delivery:  api.AddressDelivery{PeerRef: "cloud-main", Mode: "route", TunnelInterface: "wg-sam"},
		}},
	}}}
}
