// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHybridResourcesDecode(t *testing.T) {
	data := []byte(`
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: test
spec:
  resources:
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: OverlayPeer
      metadata:
        name: cloud-main
      spec:
        role: cloud
        nodeID: cloud-1
        underlay:
          type: wireguard
          interface: wg-hybrid
          address: 192.0.2.10
        remote:
          nodeID: onprem-1
          address: 198.51.100.10
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: HybridRoute
      metadata:
        name: cloud-lan
      spec:
        destinationCIDRs: [10.20.0.0/16]
        peerRef: cloud-main
        install:
          table: main
          metric: 120
        healthCheckRef: cloud-health
`)
	var router Router
	if err := yaml.Unmarshal(data, &router); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	peer, err := router.Spec.Resources[0].OverlayPeerSpec()
	if err != nil {
		t.Fatalf("peer spec: %v", err)
	}
	if peer.Role != "cloud" || peer.Underlay.Interface != "wg-hybrid" {
		t.Fatalf("peer = %#v", peer)
	}
	route, err := router.Spec.Resources[1].HybridRouteSpec()
	if err != nil {
		t.Fatalf("route spec: %v", err)
	}
	if route.PeerRef != "cloud-main" || route.Install.Metric != 120 {
		t.Fatalf("route = %#v", route)
	}
}
