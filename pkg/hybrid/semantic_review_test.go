// SPDX-License-Identifier: BSD-3-Clause

package hybrid

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestTunnelMTUEstimateRejectsRecursiveUnderlay(t *testing.T) {
	a := api.TunnelInterfaceSpec{Mode: "gre", UnderlayInterface: "b"}
	b := api.TunnelInterfaceSpec{Mode: "ipip", UnderlayInterface: "a", MTU: 1400}
	router := api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "a"}, Spec: a},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "b"}, Spec: b},
	}}}
	if estimate, ok := TunnelInterfaceMTUEstimate(router, a); ok {
		t.Fatalf("cyclic MTU premise became usable: %+v", estimate)
	}
}

func TestHybridLoweringDetectsNormalizedNativeRouteCollision(t *testing.T) {
	router := api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "operator"}, Spec: api.IPv4RouteSpec{Destination: "192.0.2.17/24", Device: "wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "peer"}, Spec: api.OverlayPeerSpec{Underlay: api.OverlayUnderlay{Type: "route", Interface: "wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "HybridRoute"}, Metadata: api.ObjectMeta{Name: "hybrid"}, Spec: api.HybridRouteSpec{PeerRef: "peer", DestinationCIDRs: []string{"192.0.2.0/24"}}},
	}}}
	if _, _, err := ExpandHybridRoutes(router); err == nil || !strings.Contains(err.Error(), "collides with user IPv4Route/operator") {
		t.Fatalf("same native prefix gained conflicting owner: %v", err)
	}
}

func TestHybridLoweringUsesDeclaredKernelInterfaceName(t *testing.T) {
	router := api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-intent"}, Spec: api.WireGuardInterfaceSpec{IfName: "wg0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "peer"}, Spec: api.OverlayPeerSpec{Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-intent"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "HybridRoute"}, Metadata: api.ObjectMeta{Name: "hybrid"}, Spec: api.HybridRouteSpec{PeerRef: "peer", DestinationCIDRs: []string{"192.0.2.0/24"}}},
	}}}
	expanded, lowerings, err := ExpandHybridRoutes(router)
	if err != nil {
		t.Fatal(err)
	}
	route, err := expanded.Spec.Resources[len(expanded.Spec.Resources)-1].IPv4RouteSpec()
	if err != nil || route.Device != "wg0" || len(lowerings) != 1 || lowerings[0].Device != "wg0" {
		t.Fatalf("logical interface leaked into native route effect: route=%+v lowerings=%+v err=%v", route, lowerings, err)
	}
}
