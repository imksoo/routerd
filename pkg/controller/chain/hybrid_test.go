// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/hybrid"
)

func TestHybridRouteControllerSavesStatus(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-hybrid"}, Spec: api.WireGuardInterfaceSpec{MTU: 1420}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "cloud-main"}, Spec: api.OverlayPeerSpec{Role: "cloud", NodeID: "cloud-1", Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-hybrid"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "HybridRoute"}, Metadata: api.ObjectMeta{Name: "cloud-private"}, Spec: api.HybridRouteSpec{DestinationCIDRs: []string{"10.20.0.0/16"}, PeerRef: "cloud-main"}},
	}}}
	expanded, lowerings, err := hybrid.ExpandHybridRoutes(*router)
	if err != nil {
		t.Fatal(err)
	}
	store := mapStore{}
	store.SaveObjectStatus(api.NetAPIVersion, "IPv4Route", lowerings[0].IPv4RouteName, map[string]any{"phase": "Installed"})
	controller := HybridRouteController{Router: router, EffectiveRouter: &expanded, Lowerings: lowerings, Store: store}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.HybridAPIVersion, "HybridRoute", "cloud-private")
	if status["phase"] != "Ready" || status["defaultRouteUntouched"] != true {
		t.Fatalf("status = %#v", status)
	}
}
