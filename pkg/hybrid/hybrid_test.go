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
