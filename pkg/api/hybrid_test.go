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
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: AddressMobilityDomain
      metadata:
        name: same-subnet
      spec:
        prefix: 10.0.0.0/24
        mode: selective-address
        peerRef: cloud-main
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: CloudProviderProfile
      metadata:
        name: oci-prod
      spec:
        provider: oci
        capabilities: [vnic-private-ip, disable-source-dest-check]
        auth:
          mode: external-command
          command: /usr/local/libexec/routerd/plugins/oci-token
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: RemoteAddressClaim
      metadata:
        name: app-10-0-1-123
      spec:
        domainRef: same-subnet
        address: 10.0.1.123/32
        ownerSide: cloud
        capture:
          type: provider-secondary-ip
          providerRef: oci-prod
          providerMode: vnic-private-ip
          nicRef: ocid1.vnic.oc1..example
        delivery:
          peerRef: cloud-main
          mode: route
          tunnelInterface: wg-hybrid
    - apiVersion: hybrid.routerd.net/v1alpha1
      kind: SAMTransportProfile
      metadata:
        name: pve08-core
      spec:
        mode: ipip
        encryption: wireguard
        localNodeID: pve-rt08
        innerCIDR: 10.255.1.0/24
        peerRole: cloud
        wireGuard:
          interface: wg-sam
          privateKeyFile: /etc/routerd/wg.key
          transportCIDR: 10.99.0.0/24
        peers:
          - name: k8s-rt02
            nodeID: k8s-rt02
            endpoint: 192.168.1.53:51820
            wireGuard:
              publicKey: peer-public-key
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
	domain, err := router.Spec.Resources[2].AddressMobilityDomainSpec()
	if err != nil {
		t.Fatalf("domain spec: %v", err)
	}
	if domain.Prefix != "10.0.0.0/24" || domain.Mode != "selective-address" {
		t.Fatalf("domain = %#v", domain)
	}
	profile, err := router.Spec.Resources[3].CloudProviderProfileSpec()
	if err != nil {
		t.Fatalf("profile spec: %v", err)
	}
	if profile.Provider != "oci" || profile.Auth.Mode != "external-command" {
		t.Fatalf("profile = %#v", profile)
	}
	claim, err := router.Spec.Resources[4].RemoteAddressClaimSpec()
	if err != nil {
		t.Fatalf("claim spec: %v", err)
	}
	if claim.DomainRef != "same-subnet" || claim.Capture.ProviderRef != "oci-prod" || claim.Capture.Type != "provider-secondary-ip" || claim.Delivery.Mode != "route" {
		t.Fatalf("claim = %#v", claim)
	}
	transport, err := router.Spec.Resources[5].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("transport spec: %v", err)
	}
	if transport.Mode != "ipip" || transport.Encryption != "wireguard" || transport.WireGuard.TransportCIDR != "10.99.0.0/24" || len(transport.Peers) != 1 {
		t.Fatalf("transport = %#v", transport)
	}
}
