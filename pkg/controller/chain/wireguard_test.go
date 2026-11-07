// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func mustWireGuardRouter(t *testing.T, body string) *api.Router {
	t.Helper()
	var router api.Router
	if err := yaml.Unmarshal([]byte(body), &router); err != nil {
		t.Fatal(err)
	}
	return &router
}

// wireGuardRouterWithFetchedRRSet models the only leaf-side RR input: a
// runtime snapshot admitted by the remote enrollment server. Static Router
// config intentionally contains neither SAMRRSet nor a duplicate RR topology.
func wireGuardRouterWithFetchedRRSet(t *testing.T, router *api.Router, name, policyRef string, nodes []api.SAMNodeSpec) *api.Router {
	t.Helper()
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	snapshot := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: policyRef,
			Nodes:               append([]api.SAMNodeSpec(nil), nodes...),
		},
	}
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{Name: "fetched-" + name},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     "SAMRRSet/" + name,
			Generation: 1,
			ObservedAt: now,
			ExpiresAt:  now.Add(time.Hour),
			Resources:  []api.Resource{snapshot},
		},
	}
	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*router)
	if err != nil {
		t.Fatalf("ExtractDynamicOverridePolicies: %v", err)
	}
	effective, _, err := dynamicconfig.BuildEffectiveConfigForOS(*router, []dynamicconfig.DynamicConfigPart{part}, policies, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("BuildEffectiveConfigForOS: %v", err)
	}
	return &effective
}

// wireGuardRouterWithFetchedSAMPeerGroup models the policy-scoped direct-leaf
// topology delivered with an RR snapshot. The peer group stays runtime-only:
// it enters the leaf through one local DynamicConfigPart rather than through
// the peer-group sync path.
func wireGuardRouterWithFetchedSAMPeerGroup(t *testing.T, router *api.Router, name, policyRef, fingerprint string, nodes []api.SAMNodeSpec) *api.Router {
	t.Helper()
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	ownedPrefixesByNode := make(map[string][]string, len(nodes))
	for i, node := range nodes {
		// Enrollment projects a signed, exact /32 ownership boundary alongside
		// each direct peer. Keep the WireGuard fixture structurally equivalent
		// even though this test only consumes its tunnel key material.
		ownedPrefixesByNode[node.NodeRef] = []string{fmt.Sprintf("198.18.0.%d/32", i+1)}
	}
	rrSet := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "fetched-topology"},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: policyRef,
			Nodes: []api.SAMNodeSpec{{
				NodeRef:        "rr-a",
				RouteReflector: true,
				SAMEndpoint:    "203.0.113.10",
				WireGuard: api.SAMNodeWireGuardSpec{
					PublicKey:  "rrpub-a",
					Endpoint:   "203.0.113.10:51820",
					AllowedIPs: []string{"203.0.113.10/32"},
				},
			}},
		},
	}
	group := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  policyRef,
			TransportFingerprint: fingerprint,
			Nodes:                append([]api.SAMNodeSpec(nil), nodes...),
			OwnedPrefixesByNode:  ownedPrefixesByNode,
		},
	}
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{Name: "fetched-topology"},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     "SAMRRSet/fetched-topology",
			Generation: 1,
			ObservedAt: now,
			ExpiresAt:  now.Add(time.Hour),
			Resources:  []api.Resource{rrSet, group},
		},
	}
	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*router)
	if err != nil {
		t.Fatalf("ExtractDynamicOverridePolicies: %v", err)
	}
	effective, _, err := dynamicconfig.BuildEffectiveConfigForOS(*router, []dynamicconfig.DynamicConfigPart{part}, policies, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("BuildEffectiveConfigForOS: %v", err)
	}
	return &effective
}

func testWireGuardDirectTransportProfile(self, iface, groupRef string) api.SAMTransportProfileSpec {
	return api.SAMTransportProfileSpec{
		SelfNodeRef:       strings.TrimSpace(self),
		Mode:              "ipip",
		Encryption:        "wireguard",
		InnerPrefix:       "10.255.0.0/24",
		AddressingMode:    "pair-stable",
		UnderlayInterface: iface,
		BGP:               api.SAMTransportBGPProfileSpec{PeerASN: 64577},
		PeersFrom: []api.SAMTransportPeersSourceSpec{
			{Resource: "SAMRRSet/fetched-topology"},
			{Resource: groupRef, Direct: true},
		},
	}
}

func addWireGuardDirectTransportProfile(router *api.Router, iface, groupRef string) api.SAMTransportProfileSpec {
	profile := testWireGuardDirectTransportProfile(router.Metadata.Name, iface, groupRef)
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
		Metadata: api.ObjectMeta{Name: "direct-transport"},
		Spec:     profile,
	})
	return profile
}

func assertStringSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
	seen := map[string]int{}
	for _, value := range got {
		seen[value]++
	}
	for _, value := range want {
		if seen[value] != 1 {
			t.Fatalf("%s = %#v, want %#v", label, got, want)
		}
	}
}

func (s mapStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	out := make([]routerstate.ObjectStatus, 0, len(s))
	for key, status := range s {
		parts := strings.Split(key, "/")
		if len(parts) != 4 {
			continue
		}
		out = append(out, routerstate.ObjectStatus{
			APIVersion: parts[0] + "/" + parts[1],
			Kind:       parts[2],
			Name:       parts[3],
			Status:     status,
		})
	}
	return out, nil
}

func (s mapStore) DeleteObject(apiVersion, kind, name string) error {
	delete(s, apiVersion+"/"+kind+"/"+name)
	return nil
}

func TestResolveWireGuardSAMResourcesDerivesPeersFromSAMRRSet(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-cloudedge}
      spec:
        selfNodeRef: leaf-a
        privateKey: priv
        peersFrom:
          - resource: SAMRRSet/cloudedge-rrs
`)
	router = wireGuardRouterWithFetchedRRSet(t, router, "cloudedge-rrs", "SAMEnrollmentPolicy/cloudedge-leaves", []api.SAMNodeSpec{
		{NodeRef: "rr-a", RouteReflector: true, SAMEndpoint: "10.10.0.2", WireGuard: api.SAMNodeWireGuardSpec{PublicKey: "rrpub-a", Endpoint: "203.0.113.10:51820", AllowedIPs: []string{"10.99.0.2/32", "10.10.0.2/32"}}},
		{NodeRef: "rr-b", RouteReflector: true, SAMEndpoint: "10.10.0.3", WireGuard: api.SAMNodeWireGuardSpec{PublicKey: "rrpub-b", Endpoint: "203.0.113.11:51820", AllowedIPs: []string{"10.99.0.3/32", "10.10.0.3/32"}}},
	})
	effective, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	if got := countResources(effective, api.NetAPIVersion, "WireGuardPeer"); got != 2 {
		t.Fatalf("WireGuardPeer count = %d, want 2 resources=%#v", got, effective.Spec.Resources)
	}
	for _, want := range []struct {
		name       string
		allowedIPs []string
	}{
		{name: "rr-a", allowedIPs: []string{"10.99.0.2/32", "10.10.0.2/32"}},
		{name: "rr-b", allowedIPs: []string{"10.99.0.3/32", "10.10.0.3/32"}},
	} {
		peer := mustWireGuardPeer(t, effective, want.name)
		if strings.Join(peer.AllowedIPs, ",") != strings.Join(want.allowedIPs, ",") {
			t.Fatalf("WireGuardPeer/%s allowedIPs = %#v, want %#v", want.name, peer.AllowedIPs, want.allowedIPs)
		}
	}
}

func TestWireGuardControllerDerivesPeersFromFetchedSAMPeerGroup(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-direct}
      spec:
        selfNodeRef: leaf-a
        privateKey: priv
        peersFrom:
          - resource: SAMRRSet/fetched-topology
          - resource: SAMPeerGroup/cloudedge-direct
`)
	profile := testWireGuardDirectTransportProfile(router.Metadata.Name, "wg-direct", "SAMPeerGroup/cloudedge-direct")
	router = wireGuardRouterWithFetchedSAMPeerGroup(t, router, "cloudedge-direct", "SAMEnrollmentPolicy/cloudedge-leaves", mobilityconfig.SAMTransportMeshFingerprint(profile), []api.SAMNodeSpec{
		{NodeRef: "leaf-a", SAMEndpoint: "10.20.0.31", WireGuard: api.SAMNodeWireGuardSpec{PublicKey: "selfpub", AllowedIPs: []string{"10.20.0.31/32"}}},
		{NodeRef: "leaf-b", SAMEndpoint: "10.20.0.32", WireGuard: api.SAMNodeWireGuardSpec{PublicKey: "leafpub-b", Endpoint: "198.51.100.32:51820", AllowedIPs: []string{"10.20.0.32/32"}, PersistentKeepalive: 25}},
	})
	addWireGuardDirectTransportProfile(router, "wg-direct", "SAMPeerGroup/cloudedge-direct")
	controller := WireGuardController{Router: router}
	resources, statuses, pending, err := controller.resolvePeersFrom("wg-direct", api.WireGuardInterfaceSpec{
		SelfNodeRef: "leaf-a",
		PeersFrom: []api.WireGuardPeersSourceSpec{
			{Resource: "SAMRRSet/fetched-topology"},
			{Resource: "SAMPeerGroup/cloudedge-direct"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 || len(statuses) != 2 || statuses[0].Phase != "Resolved" || statuses[0].PeerCount != 1 || statuses[1].Phase != "Direct" || statuses[1].PeerCount != 1 {
		t.Fatalf("pending/statuses = %#v/%#v, want one RR fallback and one direct remote peer", pending, statuses)
	}
	if got := countResources(&api.Router{Spec: api.RouterSpec{Resources: resources}}, api.NetAPIVersion, "WireGuardPeer"); got != 2 {
		t.Fatalf("WireGuardPeer count = %d, want RR and direct peers resources=%#v", got, resources)
	}
	for _, resource := range resources {
		if resource.Kind == "WireGuardPeer" && resource.Metadata.Name == "leaf-a" {
			t.Fatalf("self leaf became a WireGuard peer: %#v", resources)
		}
	}
	peer := mustWireGuardPeer(t, &api.Router{Spec: api.RouterSpec{Resources: resources}}, "leaf-b")
	if peer.PublicKey != "leafpub-b" || peer.Endpoint != "198.51.100.32:51820" || strings.Join(peer.AllowedIPs, ",") != "10.20.0.32/32" {
		t.Fatalf("WireGuardPeer/leaf-b = %#v", peer)
	}
}

func TestResolveWireGuardSAMResourcesDerivesPeersFromFetchedSAMPeerGroup(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-direct}
      spec:
        selfNodeRef: leaf-a
        privateKey: priv
        peersFrom:
          - resource: SAMRRSet/fetched-topology
          - resource: SAMPeerGroup/cloudedge-direct
`)
	profile := testWireGuardDirectTransportProfile(router.Metadata.Name, "wg-direct", "SAMPeerGroup/cloudedge-direct")
	router = wireGuardRouterWithFetchedSAMPeerGroup(t, router, "cloudedge-direct", "SAMEnrollmentPolicy/cloudedge-leaves", mobilityconfig.SAMTransportMeshFingerprint(profile), []api.SAMNodeSpec{
		{NodeRef: "leaf-b", SAMEndpoint: "10.20.0.32", WireGuard: api.SAMNodeWireGuardSpec{PublicKey: "leafpub-b", Endpoint: "198.51.100.32:51820", AllowedIPs: []string{"10.20.0.32/32"}}},
	})
	addWireGuardDirectTransportProfile(router, "wg-direct", "SAMPeerGroup/cloudedge-direct")
	resolved, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	peer := mustWireGuardPeer(t, resolved, "leaf-b")
	if peer.Interface != "wg-direct" || peer.PublicKey != "leafpub-b" || strings.Join(peer.AllowedIPs, ",") != "10.20.0.32/32" {
		t.Fatalf("WireGuardPeer/leaf-b = %#v", peer)
	}
	for _, resource := range resolved.Spec.Resources {
		if resource.Kind != "IPv4Route" || resource.Metadata.Name != "wg-sam-endpoint-leaf-b" {
			continue
		}
		route, err := resource.IPv4RouteSpec()
		if err != nil {
			t.Fatal(err)
		}
		if route.Destination != "10.20.0.32/32" || route.Device != "wg-direct" {
			t.Fatalf("route = %#v", route)
		}
		return
	}
	t.Fatal("direct SAMPeerGroup route not generated")
}

func TestWireGuardDirectGroupFingerprintMismatchKeepsRRFallback(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-direct}
      spec:
        selfNodeRef: leaf-a
        privateKey: priv
        peersFrom:
          - resource: SAMRRSet/cloudedge-rrs
          - resource: SAMPeerGroup/cloudedge-direct
`)
	router = wireGuardRouterWithFetchedRRSet(t, router, "cloudedge-rrs", "SAMEnrollmentPolicy/cloudedge-leaves", []api.SAMNodeSpec{{
		NodeRef:        "rr-a",
		RouteReflector: true,
		SAMEndpoint:    "10.20.0.2",
		WireGuard: api.SAMNodeWireGuardSpec{
			PublicKey:  "rrpub-a",
			Endpoint:   "198.51.100.2:51820",
			AllowedIPs: []string{"10.20.0.2/32"},
		},
	}})
	addWireGuardDirectTransportProfile(router, "wg-direct", "SAMPeerGroup/cloudedge-direct")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: "cloudedge-direct"},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  "SAMEnrollmentPolicy/cloudedge-leaves",
			TransportFingerprint: "sha256:wrong",
			Nodes: []api.SAMNodeSpec{{
				NodeRef:     "leaf-b",
				SAMEndpoint: "10.20.0.32",
				WireGuard: api.SAMNodeWireGuardSpec{
					PublicKey:  "leafpub-b",
					Endpoint:   "198.51.100.32:51820",
					AllowedIPs: []string{"10.20.0.32/32"},
				},
			}},
		},
	})

	controller := WireGuardController{Router: router}
	resources, statuses, pending, err := controller.resolvePeersFrom("wg-direct", api.WireGuardInterfaceSpec{
		SelfNodeRef: "leaf-a",
		PeersFrom: []api.WireGuardPeersSourceSpec{
			{Resource: "SAMRRSet/cloudedge-rrs"},
			{Resource: "SAMPeerGroup/cloudedge-direct"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 || len(statuses) != 2 || statuses[0].Phase != "Resolved" || statuses[1].Phase != "Incompatible" {
		t.Fatalf("pending/statuses = %#v/%#v, want resolved RR and incompatible direct group", pending, statuses)
	}
	if got := countResources(&api.Router{Spec: api.RouterSpec{Resources: resources}}, api.NetAPIVersion, "WireGuardPeer"); got != 1 {
		t.Fatalf("WireGuardPeer count = %d, want only RR fallback resources=%#v", got, resources)
	}
	if _, found := findWireGuardPeerResource(resources, "leaf-b"); found {
		t.Fatalf("fingerprint-mismatched direct peer was generated: %#v", resources)
	}

	resolved, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findWireGuardPeerResource(resolved.Spec.Resources, "leaf-b"); found {
		t.Fatalf("effective router retained fingerprint-mismatched direct peer: %#v", resolved.Spec.Resources)
	}
}

func TestWireGuardDirectGroupWaitsForUnreplacedRRFallback(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-direct}
      spec:
        selfNodeRef: leaf-a
        privateKey: priv
        peersFrom:
          - resource: SAMRRSet/fetched-topology
          - resource: SAMPeerGroup/cloudedge-direct
`)
	profile := testWireGuardDirectTransportProfile(router.Metadata.Name, "wg-direct", "SAMPeerGroup/cloudedge-direct")
	router = wireGuardRouterWithFetchedSAMPeerGroup(t, router, "cloudedge-direct", "SAMEnrollmentPolicy/cloudedge-leaves", mobilityconfig.SAMTransportMeshFingerprint(profile), []api.SAMNodeSpec{{
		NodeRef:     "leaf-b",
		SAMEndpoint: "10.20.0.32",
		WireGuard: api.SAMNodeWireGuardSpec{
			PublicKey:  "leafpub-b",
			Endpoint:   "198.51.100.32:51820",
			AllowedIPs: []string{"10.20.0.32/32"},
		},
	}})
	addWireGuardDirectTransportProfile(router, "wg-direct", "SAMPeerGroup/cloudedge-direct")
	// An explicit peer with the RR node name wins the normal effective-config
	// merge but carries different material. Direct must stay disabled rather
	// than claiming a fallback that would no longer exist after merging.
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"},
		Metadata: api.ObjectMeta{Name: "rr-a"},
		Spec: api.WireGuardPeerSpec{
			Interface:  "wg-direct",
			PublicKey:  "different-static-key",
			Endpoint:   "198.51.100.250:51820",
			AllowedIPs: []string{"198.51.100.250/32"},
		},
	})

	controller := WireGuardController{Router: router}
	resources, statuses, pending, err := controller.resolvePeersFrom("wg-direct", api.WireGuardInterfaceSpec{
		SelfNodeRef: "leaf-a",
		PeersFrom: []api.WireGuardPeersSourceSpec{
			{Resource: "SAMRRSet/fetched-topology"},
			{Resource: "SAMPeerGroup/cloudedge-direct"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 || len(statuses) != 2 || statuses[1].Phase != "Unavailable" || statuses[1].Reason != "matching RR fallback SAMRRSet has no usable WireGuard peer" {
		t.Fatalf("pending/statuses = %#v/%#v, want unreplaced-RR fallback gate", pending, statuses)
	}
	if _, found := findWireGuardPeerResource(resources, "leaf-b"); found {
		t.Fatalf("direct peer was generated despite overridden RR fallback: %#v", resources)
	}

	resolved, err := controller.resolvePeerResources()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findWireGuardPeerResource(resolved.Router.Spec.Resources, "leaf-b"); found {
		t.Fatalf("resolved router retained direct peer despite overridden RR fallback: %#v", resolved.Router.Spec.Resources)
	}
	peer := mustWireGuardPeer(t, resolved.Router, "rr-a")
	if peer.PublicKey != "different-static-key" {
		t.Fatalf("explicit RR-name peer was not retained by the normal merge: %#v", peer)
	}

	effective, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := findWireGuardPeerResource(effective.Spec.Resources, "leaf-b"); found {
		t.Fatalf("effective router retained direct peer despite overridden RR fallback: %#v", effective.Spec.Resources)
	}
}

func TestWireGuardMalformedDirectGroupKeepsRRFallback(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: leaf-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-direct}
      spec:
        selfNodeRef: leaf-a
        privateKey: priv
        peersFrom:
          - resource: SAMRRSet/cloudedge-rrs
          - resource: SAMPeerGroup/cloudedge-direct
`)
	router = wireGuardRouterWithFetchedRRSet(t, router, "cloudedge-rrs", "SAMEnrollmentPolicy/cloudedge-leaves", []api.SAMNodeSpec{{
		NodeRef:        "rr-a",
		RouteReflector: true,
		SAMEndpoint:    "10.20.0.2",
		WireGuard: api.SAMNodeWireGuardSpec{
			PublicKey:  "rrpub-a",
			Endpoint:   "198.51.100.2:51820",
			AllowedIPs: []string{"10.20.0.2/32"},
		},
	}})
	addWireGuardDirectTransportProfile(router, "wg-direct", "SAMPeerGroup/cloudedge-direct")
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: "cloudedge-direct"},
		Spec:     map[string]any{"nodes": "not-a-node-list"},
	})

	controller := WireGuardController{Router: router}
	resources, statuses, pending, err := controller.resolvePeersFrom("wg-direct", api.WireGuardInterfaceSpec{
		SelfNodeRef: "leaf-a",
		PeersFrom: []api.WireGuardPeersSourceSpec{
			{Resource: "SAMRRSet/cloudedge-rrs"},
			{Resource: "SAMPeerGroup/cloudedge-direct"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 || len(statuses) != 2 || statuses[0].Phase != "Resolved" || statuses[1].Phase != "Incompatible" {
		t.Fatalf("pending/statuses = %#v/%#v, want resolved RR and incompatible direct group", pending, statuses)
	}
	if got := countResources(&api.Router{Spec: api.RouterSpec{Resources: resources}}, api.NetAPIVersion, "WireGuardPeer"); got != 1 {
		t.Fatalf("WireGuardPeer count = %d, want only RR fallback resources=%#v", got, resources)
	}
}

func findWireGuardPeerResource(resources []api.Resource, name string) (api.Resource, bool) {
	for _, resource := range resources {
		if resource.APIVersion == api.NetAPIVersion && resource.Kind == "WireGuardPeer" && resource.Metadata.Name == name {
			return resource, true
		}
	}
	return api.Resource{}, false
}

func mustWireGuardPeer(t *testing.T, router *api.Router, name string) api.WireGuardPeerSpec {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "WireGuardPeer" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.WireGuardPeerSpec()
		if err != nil {
			t.Fatalf("WireGuardPeer/%s spec: %v", name, err)
		}
		return spec
	}
	t.Fatalf("WireGuardPeer/%s not found in %#v", name, router.Spec.Resources)
	return api.WireGuardPeerSpec{}
}

func TestPVEWireGuardPeersFromSubmittedEnrollmentClaimState(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	router, err := config.Load(filepath.Join("..", "..", "..", "examples", "pve-minimal-rr.yaml"))
	if err != nil {
		t.Fatalf("load pve-minimal-rr.yaml: %v", err)
	}
	assertResourceCount(t, router.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim", 0)
	claim := claimWithJoinTimestamp(t, pveMinimalRRSeedClaim(t, "pve-leaf-a"), now)
	store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{
		dynamicPartRecord(t, "SAMEnrollmentClaim/pve-leaf-a", []api.Resource{claim}, now.Add(5*time.Minute)),
	}}
	effective, err := BuildDynamicRouteSAMEffectiveRouter(router, store, now, platform.OSLinux)
	if err != nil {
		t.Fatalf("BuildDynamicRouteSAMEffectiveRouter: %v", err)
	}
	assertResourceCount(t, effective.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim", 1)
	resolved, err := resolveWireGuardSAMResources(effective)
	if err != nil {
		t.Fatalf("resolveWireGuardSAMResources: %v", err)
	}
	peer := mustResource(t, resolved, api.NetAPIVersion, "WireGuardPeer", "pve-leaf-a")
	spec, err := peer.WireGuardPeerSpec()
	if err != nil {
		t.Fatalf("WireGuardPeer spec: %v", err)
	}
	if spec.Interface != "wg-pve" || spec.PublicKey != "PVE_LEAF_A_WIREGUARD_PUBLIC_KEY" || spec.Endpoint != "10.30.0.21:51820" {
		t.Fatalf("WireGuardPeer/pve-leaf-a = %#v", spec)
	}
	assertStringSet(t, "WireGuardPeer/pve-leaf-a allowedIPs", spec.AllowedIPs, []string{"10.255.10.21/32", "10.31.0.21/32"})
}

func TestCloudEdgeRRExamplesDeriveOnlyWGAdmissionPeers(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, example := range []string{
		"cloudedge-dynamic-rr-a-hub.yaml",
		"cloudedge-dynamic-rr-b-hub.yaml",
	} {
		t.Run(example, func(t *testing.T) {
			router, err := config.Load(filepath.Join("..", "..", "..", "examples", example))
			if err != nil {
				t.Fatalf("load %s: %v", example, err)
			}
			if err := config.Validate(router); err != nil {
				t.Fatalf("validate %s: %v", example, err)
			}
			assertResourceCount(t, router.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim", 0)
			claim := claimWithJoinTimestamp(t, seedClaimFromFixture(t, "cloudedge-rr-claims-seed.yaml", "leaf-a"), now)
			nonWGClaim := claimWithJoinTimestamp(t, seedClaimFromFixture(t, "cloudedge-rr-claims-seed.yaml", "leaf-b"), now)
			store := &dynamicRouteSAMStore{records: []routerstate.DynamicConfigPartRecord{
				dynamicPartRecord(t, "SAMEnrollmentClaim/leaf-a", []api.Resource{claim}, now.Add(5*time.Minute)),
				dynamicPartRecord(t, "SAMEnrollmentClaim/leaf-b", []api.Resource{nonWGClaim}, now.Add(5*time.Minute)),
			}}
			effective, err := BuildDynamicRouteSAMEffectiveRouter(router, store, now, platform.OSLinux)
			if err != nil {
				t.Fatalf("BuildDynamicRouteSAMEffectiveRouter: %v", err)
			}
			assertResourceCount(t, effective.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim", 2)
			resolved, err := resolveWireGuardSAMResources(effective)
			if err != nil {
				t.Fatalf("resolveWireGuardSAMResources: %v", err)
			}
			var peers []api.WireGuardPeerSpec
			var names []string
			for _, resource := range resolved.Spec.Resources {
				if resource.APIVersion != api.NetAPIVersion || resource.Kind != "WireGuardPeer" {
					continue
				}
				spec, err := resource.WireGuardPeerSpec()
				if err != nil {
					t.Fatalf("WireGuardPeer/%s spec: %v", resource.Metadata.Name, err)
				}
				names = append(names, resource.Metadata.Name)
				peers = append(peers, spec)
			}
			if len(peers) != 1 || names[0] != "leaf-a" {
				t.Fatalf("generated WireGuard peers = %v %#v, want only leaf-a", names, peers)
			}
			peer := peers[0]
			if peer.Interface != "wg-cloudedge" || peer.PublicKey != "LEAF_A_WIREGUARD_PUBLIC_KEY" || peer.Endpoint != "198.51.100.31:51820" {
				t.Fatalf("leaf-a WireGuard peer = %#v", peer)
			}
			assertStringSet(t, "leaf-a allowedIPs", peer.AllowedIPs, []string{"10.255.0.31/32", "10.20.0.31/32"})
		})
	}
}

func pveMinimalRRSeedClaim(t *testing.T, name string) api.Resource {
	t.Helper()
	return seedClaimFromFixture(t, "pve-minimal-rr-claims-seed.yaml", name)
}

func seedClaimFromFixture(t *testing.T, fixture, name string) api.Resource {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", fixture))
	if err != nil {
		t.Fatalf("read claim seed %s: %v", fixture, err)
	}
	var seed api.Router
	if err := yaml.Unmarshal(data, &seed); err != nil {
		t.Fatalf("parse claim seed %s: %v", fixture, err)
	}
	for _, resource := range seed.Spec.Resources {
		if resource.APIVersion == api.MobilityAPIVersion && resource.Kind == "SAMEnrollmentClaim" && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("missing seed claim %s", name)
	return api.Resource{}
}

func claimWithJoinTimestamp(t *testing.T, resource api.Resource, now time.Time) api.Resource {
	t.Helper()
	spec, err := resource.SAMEnrollmentClaimSpec()
	if err != nil {
		t.Fatalf("SAMEnrollmentClaim/%s spec: %v", resource.Metadata.Name, err)
	}
	spec.JoinTimestamp = now.UTC().Format(time.RFC3339)
	resource.Spec = spec
	return resource
}

func mustResource(t *testing.T, router *api.Router, apiVersion, kind, name string) api.Resource {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == name {
			return resource
		}
	}
	t.Fatalf("missing %s/%s", kind, name)
	return api.Resource{}
}

func assertResourceCount(t *testing.T, resources []api.Resource, apiVersion, kind string, want int) {
	t.Helper()
	got := 0
	for _, resource := range resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind {
			got++
		}
	}
	if got != want {
		t.Fatalf("%s/%s count = %d, want %d", apiVersion, kind, got, want)
	}
}

func TestWireGuardControllerGeneratesSAMEndpointRoutes(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        selfNodeRef: router-a
        privateKey: priv
        peersFrom:
          - resource: SAMNodeSet/fabric
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: {name: fabric}
      spec:
        nodes:
          - nodeRef: router-a
            samEndpoint: 10.99.70.1
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.70.1/32]
          - nodeRef: router-b
            samEndpoint: 10.99.70.2
            wireGuard:
              publicKey: peerpub-b
              endpoint: 198.51.100.2:51820
              allowedIPs: [10.99.70.2/32]
          - nodeRef: router-c
            samEndpoint: 10.99.70.3
            wireGuard:
              publicKey: peerpub-c
              endpoint: 198.51.100.3:51820
              allowedIPs: [10.99.70.3/32]
`)
	ctrl := WireGuardController{Router: router, Store: mapStore{}}
	resolved, err := ctrl.resolvePeerResources()
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]string{}
	for _, r := range resolved.Router.Spec.Resources {
		if r.Kind == "IPv4Route" {
			spec, _ := r.IPv4RouteSpec()
			routes[r.Metadata.Name] = spec.Destination + " dev " + spec.Device
		}
	}
	if got, ok := routes["wg-sam-endpoint-router-b"]; !ok || got != "10.99.70.2/32 dev wg0" {
		t.Fatalf("expected route for router-b; routes=%v", routes)
	}
	if got, ok := routes["wg-sam-endpoint-router-c"]; !ok || got != "10.99.70.3/32 dev wg0" {
		t.Fatalf("expected route for router-c; routes=%v", routes)
	}
	if _, ok := routes["wg-sam-endpoint-router-a"]; ok {
		t.Fatal("self node route must not be generated")
	}
}

func TestWireGuardControllerStaticRouteOverridesGeneratedEndpointRoute(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        selfNodeRef: router-a
        privateKey: priv
        peersFrom:
          - resource: SAMNodeSet/fabric
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: {name: fabric}
      spec:
        nodes:
          - nodeRef: router-b
            samEndpoint: 10.99.70.2
            wireGuard:
              publicKey: peerpub-b
              allowedIPs: [10.99.70.2/32]
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4Route
      metadata: {name: wg-sam-endpoint-router-b}
      spec:
        destination: 10.99.70.2/32
        device: custom0
        metric: 42
`)
	ctrl := WireGuardController{Router: router, Store: mapStore{}}
	resolved, err := ctrl.resolvePeerResources()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resolved.Router.Spec.Resources {
		if r.Kind == "IPv4Route" && r.Metadata.Name == "wg-sam-endpoint-router-b" {
			spec, _ := r.IPv4RouteSpec()
			if spec.Device != "custom0" || spec.Metric != 42 {
				t.Fatalf("static route did not override generated: %+v", spec)
			}
			return
		}
	}
	t.Fatal("route not found in resolved resources")
}

func TestWireGuardControllerMarksMissingKeyPending(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        listenPort: 51820
`)
	store := mapStore{}
	controller := WireGuardController{Router: router, Store: store}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", "wg0")
	if status["phase"] != "Pending" || status["reason"] != "PrivateKeyMissing" {
		t.Fatalf("status = %+v", status)
	}
}

func TestResolveWireGuardSAMResourcesGeneratesSelfAddressAndPeerRoutes(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-sam}
      spec:
        selfNodeRef: router-a
        privateKey: priv
        peersFrom:
          - resource: SAMNodeSet/fabric
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: {name: fabric}
      spec:
        nodes:
          - nodeRef: router-a
            samEndpoint: 10.99.70.1
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.70.1/32]
          - nodeRef: router-b
            samEndpoint: 10.99.70.2
            wireGuard:
              publicKey: peerpub-b
              endpoint: 198.51.100.2:51820
              allowedIPs: [10.99.70.2/32]
`)
	resolved, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	var selfAddr, peerRoute, peerPeer bool
	for _, r := range resolved.Spec.Resources {
		switch {
		case r.Kind == "IPv4StaticAddress" && r.Metadata.Name == "wg-sam-addr-wg-sam":
			spec, _ := r.IPv4StaticAddressSpec()
			if spec.Interface != "wg-sam" || spec.Address != "10.99.70.1/32" {
				t.Fatalf("self address spec mismatch: %+v", spec)
			}
			selfAddr = true
		case r.Kind == "IPv4Route" && r.Metadata.Name == "wg-sam-endpoint-router-b":
			spec, _ := r.IPv4RouteSpec()
			if spec.Destination != "10.99.70.2/32" || spec.Device != "wg-sam" {
				t.Fatalf("peer route spec mismatch: %+v", spec)
			}
			peerRoute = true
		case r.Kind == "WireGuardPeer" && r.Metadata.Name == "router-b":
			spec, _ := r.WireGuardPeerSpec()
			if spec.PublicKey != "peerpub-b" || spec.Interface != "wg-sam" {
				t.Fatalf("peer spec mismatch: %+v", spec)
			}
			peerPeer = true
		}
	}
	if !selfAddr {
		t.Fatal("self-node IPv4StaticAddress not generated")
	}
	if !peerRoute {
		t.Fatal("peer IPv4Route not generated")
	}
	if !peerPeer {
		t.Fatal("peer WireGuardPeer not generated")
	}
	for _, r := range resolved.Spec.Resources {
		if r.Kind == "IPv4Route" && r.Metadata.Name == "wg-sam-endpoint-router-a" {
			t.Fatal("self-node route must not be generated")
		}
	}
}

func TestResolveWireGuardSAMResourcesStaticAddressOverride(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-sam}
      spec:
        selfNodeRef: router-a
        privateKey: priv
        peersFrom:
          - resource: SAMNodeSet/fabric
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: {name: fabric}
      spec:
        nodes:
          - nodeRef: router-a
            samEndpoint: 10.99.70.1
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.70.1/32]
    - apiVersion: net.routerd.net/v1alpha1
      kind: IPv4StaticAddress
      metadata: {name: wg-sam-addr-wg-sam}
      spec:
        interface: wg-sam
        address: 10.99.70.99/32
`)
	resolved, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resolved.Spec.Resources {
		if r.Kind == "IPv4StaticAddress" && r.Metadata.Name == "wg-sam-addr-wg-sam" {
			spec, _ := r.IPv4StaticAddressSpec()
			if spec.Address != "10.99.70.99/32" {
				t.Fatalf("static override not respected: got %s", spec.Address)
			}
			return
		}
	}
	t.Fatal("IPv4StaticAddress not found")
}

func wireGuardResourceSnapshot(t *testing.T, router *api.Router) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range router.Spec.Resources {
		key := r.APIVersion + "/" + r.Kind + "/" + r.Metadata.Name
		if _, dup := out[key]; dup {
			t.Fatalf("duplicate resource identity %s", key)
		}
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		out[key] = string(raw)
	}
	return out
}

func TestResolveWireGuardSAMResourcesIsIdempotent(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg-sam}
      spec:
        selfNodeRef: router-a
        privateKey: priv
        peersFrom:
          - resource: SAMNodeSet/fabric
    - apiVersion: mobility.routerd.net/v1alpha1
      kind: SAMNodeSet
      metadata: {name: fabric}
      spec:
        nodes:
          - nodeRef: router-a
            samEndpoint: 10.99.70.1
            wireGuard:
              publicKey: selfpub
              allowedIPs: [10.99.70.1/32]
          - nodeRef: router-b
            samEndpoint: 10.99.70.2
            wireGuard:
              publicKey: peerpub-b
              endpoint: 198.51.100.2:51820
              allowedIPs: [10.99.70.2/32]
`)
	first, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	before := wireGuardResourceSnapshot(t, first)
	second, err := resolveWireGuardSAMResources(first)
	if err != nil {
		t.Fatal(err)
	}
	count := func(kind, name string) int {
		total := 0
		for _, r := range second.Spec.Resources {
			if r.APIVersion == api.NetAPIVersion && r.Kind == kind && r.Metadata.Name == name {
				total++
			}
		}
		return total
	}
	for _, want := range []struct {
		kind string
		name string
	}{
		{"IPv4StaticAddress", "wg-sam-addr-wg-sam"},
		{"IPv4Route", "wg-sam-endpoint-router-b"},
		{"WireGuardPeer", "router-b"},
	} {
		if got := count(want.kind, want.name); got != 1 {
			t.Fatalf("%s/%s count = %d, want 1", want.kind, want.name, got)
		}
	}
	if after := wireGuardResourceSnapshot(t, second); !reflect.DeepEqual(before, after) {
		t.Fatalf("idempotent complete resources changed: before=%#v after=%#v", before, after)
	}

}

func TestResolveWireGuardSAMResourcesNilRouter(t *testing.T) {
	resolved, err := resolveWireGuardSAMResources(nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != nil {
		t.Fatalf("expected nil router, got %+v", resolved)
	}
}

func TestResolveWireGuardSAMResourcesNoPeersFromIsNoOp(t *testing.T) {
	router := mustWireGuardRouter(t, `
apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: router-a}
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: WireGuardInterface
      metadata: {name: wg0}
      spec:
        privateKey: priv
`)
	before := wireGuardResourceSnapshot(t, router)
	resolved, err := resolveWireGuardSAMResources(router)
	if err != nil {
		t.Fatal(err)
	}
	if after := wireGuardResourceSnapshot(t, resolved); !reflect.DeepEqual(before, after) {
		t.Fatalf("no-op complete resources changed: before=%#v after=%#v", before, after)
	}

}
