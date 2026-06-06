// SPDX-License-Identifier: BSD-3-Clause

package hybrid

import (
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestExpandHybridRoutesTwoCIDRs(t *testing.T) {
	router := testRouter()
	expanded, lowerings, err := ExpandHybridRoutes(*router)
	if err != nil {
		t.Fatalf("ExpandHybridRoutes: %v", err)
	}
	if len(lowerings) != 2 {
		t.Fatalf("lowerings = %#v", lowerings)
	}
	var routes []api.Resource
	for _, resource := range expanded.Spec.Resources {
		if resource.Kind == "IPv4Route" {
			routes = append(routes, resource)
		}
	}
	if len(routes) != 2 {
		t.Fatalf("routes = %#v", routes)
	}
	gotNames := []string{routes[0].Metadata.Name, routes[1].Metadata.Name}
	wantNames := []string{"cloud-private-10-20-0-0-16", "cloud-private-10-21-0-0-16"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("names = %#v, want %#v", gotNames, wantNames)
	}
	for _, route := range routes {
		spec, err := route.IPv4RouteSpec()
		if err != nil {
			t.Fatal(err)
		}
		if spec.Device != "wg-hybrid" || spec.Metric != 120 || spec.Type != "unicast" {
			t.Fatalf("route spec = %#v", spec)
		}
		if len(route.Metadata.OwnerRefs) != 1 || route.Metadata.OwnerRefs[0].Kind != "HybridRoute" {
			t.Fatalf("ownerRefs = %#v", route.Metadata.OwnerRefs)
		}
	}
}

func TestExpandHybridRoutesNoHybridUnchanged(t *testing.T) {
	router := api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "static"}, Spec: api.IPv4RouteSpec{Destination: "10.10.0.0/16", Device: "eth0"}},
	}}}
	expanded, lowerings, err := ExpandHybridRoutes(router)
	if err != nil {
		t.Fatalf("ExpandHybridRoutes: %v", err)
	}
	if len(lowerings) != 0 {
		t.Fatalf("lowerings = %#v", lowerings)
	}
	if !reflect.DeepEqual(expanded.Spec.Resources, router.Spec.Resources) {
		t.Fatalf("resources changed: got %#v want %#v", expanded.Spec.Resources, router.Spec.Resources)
	}
}

func TestExpandHybridRoutesRejectsDefaultRoute(t *testing.T) {
	router := testRouter()
	spec := router.Spec.Resources[2].Spec.(api.HybridRouteSpec)
	spec.DestinationCIDRs = []string{"0.0.0.0/0"}
	router.Spec.Resources[2].Spec = spec
	_, _, err := ExpandHybridRoutes(*router)
	if err == nil || !strings.Contains(err.Error(), "default routes are not allowed") {
		t.Fatalf("error = %v", err)
	}
}

func TestExpandHybridRoutesRejectsUserRouteCollision(t *testing.T) {
	router := testRouter()
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
		Metadata: api.ObjectMeta{
			Name: "manual",
		},
		Spec: api.IPv4RouteSpec{Destination: "10.20.0.0/16", Device: "eth0"},
	})
	_, _, err := ExpandHybridRoutes(*router)
	if err == nil || !strings.Contains(err.Error(), "collides with user IPv4Route") {
		t.Fatalf("error = %v", err)
	}
}

func TestHybridRouteStatusAndMTUEstimate(t *testing.T) {
	router := testRouter()
	expanded, lowerings, err := ExpandHybridRoutes(*router)
	if err != nil {
		t.Fatal(err)
	}
	store := mapStore{}
	for _, lowering := range lowerings {
		store[api.NetAPIVersion+"/IPv4Route/"+lowering.IPv4RouteName] = map[string]any{"phase": "Installed"}
	}
	status := StatusForHybridRoute(expanded, router.Spec.Resources[2], lowerings, store)
	if status["phase"] != "Ready" {
		t.Fatalf("status = %#v", status)
	}
	if status["defaultRouteUntouched"] != true || status["estimatedMTU"] != 1340 || status["tunnelOverhead"] != WireGuardOverheadBytes {
		t.Fatalf("status = %#v", status)
	}
}

func TestRouteTargetSupportsTunnelUnderlays(t *testing.T) {
	for _, underlayType := range []string{"ipip", "gre", "fou", "gue"} {
		device, gateway, err := RouteTarget(api.OverlayPeerSpec{Underlay: api.OverlayUnderlay{Type: underlayType, Interface: "tun0"}})
		if err != nil {
			t.Fatalf("RouteTarget(%s): %v", underlayType, err)
		}
		if device != "tun0" || gateway != "" {
			t.Fatalf("RouteTarget(%s) = device %q gateway %q", underlayType, device, gateway)
		}
	}
}

func TestTunnelUnderlayMTUEstimate(t *testing.T) {
	tests := []struct {
		name          string
		resources     []api.Resource
		tunnel        api.TunnelInterfaceSpec
		underlayType  string
		wantMTU       int
		wantOverhead  int
		wantEstimated int
	}{
		{
			name:          "ipip default",
			tunnel:        api.TunnelInterfaceSpec{Mode: "ipip"},
			underlayType:  "ipip",
			wantMTU:       1500,
			wantOverhead:  IPIPOverheadBytes,
			wantEstimated: TunnelIPIPDefaultMTU,
		},
		{
			name: "ipip over wireguard",
			resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-underlay"}, Spec: api.WireGuardInterfaceSpec{MTU: 1420}},
			},
			tunnel:        api.TunnelInterfaceSpec{Mode: "ipip", UnderlayInterface: "wg-underlay"},
			underlayType:  "ipip",
			wantMTU:       1420,
			wantOverhead:  IPIPOverheadBytes,
			wantEstimated: 1400,
		},
		{
			name: "ipip over plain interface",
			resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "ens18", Managed: false, Owner: "external", MTU: 1500}},
			},
			tunnel:        api.TunnelInterfaceSpec{Mode: "ipip", UnderlayInterface: "wan"},
			underlayType:  "ipip",
			wantMTU:       1500,
			wantOverhead:  IPIPOverheadBytes,
			wantEstimated: 1480,
		},
		{
			name:          "gre default",
			tunnel:        api.TunnelInterfaceSpec{Mode: "gre"},
			underlayType:  "gre",
			wantMTU:       1500,
			wantOverhead:  GREOverheadBytes,
			wantEstimated: TunnelGREDefaultMTU,
		},
		{
			name:          "gre key",
			tunnel:        api.TunnelInterfaceSpec{Mode: "gre", MTU: 1472, Key: 42},
			underlayType:  "gre",
			wantMTU:       0,
			wantOverhead:  GREOverheadBytes + GREKeyOverheadBytes,
			wantEstimated: 1472,
		},
		{
			name:          "fou default",
			tunnel:        api.TunnelInterfaceSpec{Mode: "fou", EncapSport: 5555, EncapDport: 5555},
			underlayType:  "fou",
			wantMTU:       1500,
			wantOverhead:  FOUOverheadBytes,
			wantEstimated: TunnelFOUDefaultMTU,
		},
		{
			name:          "gue default",
			tunnel:        api.TunnelInterfaceSpec{Mode: "gue", EncapSport: 6080, EncapDport: 6080},
			underlayType:  "gue",
			wantMTU:       1500,
			wantOverhead:  GUEOverheadBytes,
			wantEstimated: TunnelGUEDefaultMTU,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources := append([]api.Resource(nil), tt.resources...)
			resources = append(resources,
				api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "tun0"}, Spec: tt.tunnel},
				api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "edge"}, Spec: api.OverlayPeerSpec{
					Role:     "cloud",
					NodeID:   "edge-1",
					Underlay: api.OverlayUnderlay{Type: tt.underlayType, Interface: "tun0"},
				}},
			)
			router := &api.Router{Spec: api.RouterSpec{Resources: resources}}
			estimate, ok := EstimateMTU(*router, "edge")
			if !ok {
				t.Fatal("EstimateMTU returned !ok")
			}
			if estimate.UnderlayMTU != tt.wantMTU || estimate.Overhead != tt.wantOverhead || estimate.EstimatedMTU != tt.wantEstimated {
				t.Fatalf("estimate = %#v, want mtu=%d overhead=%d estimated=%d", estimate, tt.wantMTU, tt.wantOverhead, tt.wantEstimated)
			}
		})
	}
}

type mapStore map[string]map[string]any

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func testRouter() *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-hybrid"}, Spec: api.WireGuardInterfaceSpec{MTU: 1420}},
			{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "cloud-main"}, Spec: api.OverlayPeerSpec{
				Role:     "cloud",
				NodeID:   "cloud-1",
				Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-hybrid"},
			}},
			{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "HybridRoute"}, Metadata: api.ObjectMeta{Name: "cloud-private"}, Spec: api.HybridRouteSpec{
				DestinationCIDRs: []string{"10.20.0.0/16", "10.21.0.0/16"},
				PeerRef:          "cloud-main",
				Install:          api.HybridRouteInstall{Metric: 120},
			}},
		}},
	}
}

func findHybridTestResource(t *testing.T, router api.Router, apiVersion, kind, name string) api.Resource {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("resource %s/%s/%s not found in %#v", apiVersion, kind, name, router.Spec.Resources)
	return api.Resource{}
}

func findResourceByKind(t *testing.T, router api.Router, apiVersion, kind string) api.Resource {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind {
			return resource
		}
	}
	t.Fatalf("resource %s/%s not found in %#v", apiVersion, kind, router.Spec.Resources)
	return api.Resource{}
}
