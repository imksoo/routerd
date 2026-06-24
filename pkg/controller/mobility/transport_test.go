// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func testStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func TestSAMTransportProfileDerivesSymmetricSortedEdge31(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	aStore := testStore(t, now)
	aController := TransportController{
		Router: transportRouter("demo", "cloud-rt", []api.SAMTransportPeerSpec{{
			NodeRef:        "onprem-rt",
			RemoteEndpoint: "203.0.113.20",
		}}),
		Store: aStore,
		Now:   func() time.Time { return now },
	}
	if err := aController.Reconcile(context.Background()); err != nil {
		t.Fatalf("cloud Reconcile: %v", err)
	}
	aResources := decodeResources(t, latestPart(t, aStore, TransportDynamicSource("demo", "cloud-rt")).ResourcesJSON)
	aTunnel := findTransportTunnel(t, aResources)
	aPeer := findTransportBGPPeer(t, aResources)

	bStore := testStore(t, now)
	bController := TransportController{
		Router: transportRouter("demo", "onprem-rt", []api.SAMTransportPeerSpec{{
			NodeRef:        "cloud-rt",
			RemoteEndpoint: "198.51.100.10",
		}}),
		Store: bStore,
		Now:   func() time.Time { return now },
	}
	if err := bController.Reconcile(context.Background()); err != nil {
		t.Fatalf("onprem Reconcile: %v", err)
	}
	bResources := decodeResources(t, latestPart(t, bStore, TransportDynamicSource("demo", "onprem-rt")).ResourcesJSON)
	bTunnel := findTransportTunnel(t, bResources)
	bPeer := findTransportBGPPeer(t, bResources)

	if aTunnel.Address != "10.255.1.0/31" || bPeer.Peers[0] != "10.255.1.0" {
		t.Fatalf("cloud local / onprem remote = %s / %v, want 10.255.1.0/31 / 10.255.1.0", aTunnel.Address, bPeer.Peers)
	}
	if bTunnel.Address != "10.255.1.1/31" || aPeer.Peers[0] != "10.255.1.1" {
		t.Fatalf("onprem local / cloud remote = %s / %v, want 10.255.1.1/31 / 10.255.1.1", bTunnel.Address, aPeer.Peers)
	}
}

func TestSAMTransportProfileDerivesHubSpokeWithSharedTopology(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 3, 0, 0, time.UTC)
	topology := []string{"k8s-rt01", "k8s-rt02", "pve-rt01", "pve-rt06", "pve-rt08"}
	k8sStore := testStore(t, now)
	k8sController := TransportController{
		Router: transportRouterWithTopology("svnet1", "k8s-rt01", topology, []api.SAMTransportPeerSpec{
			{NodeRef: "pve-rt01", RemoteEndpoint: "203.0.113.21"},
			{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
			{NodeRef: "pve-rt08", RemoteEndpoint: "203.0.113.28"},
		}),
		Store: k8sStore,
		Now:   func() time.Time { return now },
	}
	if err := k8sController.Reconcile(context.Background()); err != nil {
		t.Fatalf("k8s Reconcile: %v", err)
	}
	k8sResources := decodeResources(t, latestPart(t, k8sStore, TransportDynamicSource("svnet1", "k8s-rt01")).ResourcesJSON)
	k8sTunnel := findTransportTunnelForPeer(t, k8sResources, "k8s-rt01", "pve-rt06")
	k8sPeer := findTransportBGPPeerForPeer(t, k8sResources, "k8s-rt01", "pve-rt06")

	pveStore := testStore(t, now)
	pveController := TransportController{
		Router: transportRouterWithTopology("svnet1", "pve-rt06", topology, []api.SAMTransportPeerSpec{
			{NodeRef: "k8s-rt01", RemoteEndpoint: "198.51.100.11"},
			{NodeRef: "k8s-rt02", RemoteEndpoint: "198.51.100.12"},
		}),
		Store: pveStore,
		Now:   func() time.Time { return now },
	}
	if err := pveController.Reconcile(context.Background()); err != nil {
		t.Fatalf("pve Reconcile: %v", err)
	}
	pveResources := decodeResources(t, latestPart(t, pveStore, TransportDynamicSource("svnet1", "pve-rt06")).ResourcesJSON)
	pveTunnel := findTransportTunnelForPeer(t, pveResources, "pve-rt06", "k8s-rt01")
	pvePeer := findTransportBGPPeerForPeer(t, pveResources, "pve-rt06", "k8s-rt01")

	if k8sTunnel.Address != pvePeer.Peers[0]+"/31" {
		t.Fatalf("k8s local / pve remote = %s / %v, want same /31 edge", k8sTunnel.Address, pvePeer.Peers)
	}
	if pveTunnel.Address != k8sPeer.Peers[0]+"/31" {
		t.Fatalf("pve local / k8s remote = %s / %v, want same /31 edge", pveTunnel.Address, k8sPeer.Peers)
	}
}

func TestSAMTransportProfileDerivesPairStableWithoutSharedTopology(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 4, 0, 0, time.UTC)
	k8sStore := testStore(t, now)
	k8sController := TransportController{
		Router: transportRouterWithMode("svnet1", "k8s-rt01", "pair-stable", []api.SAMTransportPeerSpec{
			{NodeRef: "pve-rt01", RemoteEndpoint: "203.0.113.21"},
			{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
			{NodeRef: "pve-rt08", RemoteEndpoint: "203.0.113.28"},
		}),
		Store: k8sStore,
		Now:   func() time.Time { return now },
	}
	if err := k8sController.Reconcile(context.Background()); err != nil {
		t.Fatalf("k8s Reconcile: %v", err)
	}
	k8sResources := decodeResources(t, latestPart(t, k8sStore, TransportDynamicSource("svnet1", "k8s-rt01")).ResourcesJSON)
	k8sTunnel := findTransportTunnelForPeer(t, k8sResources, "k8s-rt01", "pve-rt06")
	k8sPeer := findTransportBGPPeerForPeer(t, k8sResources, "k8s-rt01", "pve-rt06")

	pveStore := testStore(t, now)
	pveController := TransportController{
		Router: transportRouterWithMode("svnet1", "pve-rt06", "pair-stable", []api.SAMTransportPeerSpec{
			{NodeRef: "k8s-rt01", RemoteEndpoint: "198.51.100.11"},
			{NodeRef: "k8s-rt02", RemoteEndpoint: "198.51.100.12"},
		}),
		Store: pveStore,
		Now:   func() time.Time { return now },
	}
	if err := pveController.Reconcile(context.Background()); err != nil {
		t.Fatalf("pve Reconcile: %v", err)
	}
	pveResources := decodeResources(t, latestPart(t, pveStore, TransportDynamicSource("svnet1", "pve-rt06")).ResourcesJSON)
	pveTunnel := findTransportTunnelForPeer(t, pveResources, "pve-rt06", "k8s-rt01")
	pvePeer := findTransportBGPPeerForPeer(t, pveResources, "pve-rt06", "k8s-rt01")

	if k8sTunnel.Address != pvePeer.Peers[0]+"/31" {
		t.Fatalf("k8s local / pve remote = %s / %v, want same /31 edge", k8sTunnel.Address, pvePeer.Peers)
	}
	if pveTunnel.Address != k8sPeer.Peers[0]+"/31" {
		t.Fatalf("pve local / k8s remote = %s / %v, want same /31 edge", pveTunnel.Address, k8sPeer.Peers)
	}
}

func TestSAMTransportProfilePairStableKeepsExistingPairAddressWhenPeersGrow(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 4, 30, 0, time.UTC)
	store := testStore(t, now)
	controller := TransportController{
		Router: transportRouterWithMode("svnet1", "k8s-rt01", "pair-stable", []api.SAMTransportPeerSpec{
			{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
		}),
		Store: store,
		Now:   func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	initialResources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "k8s-rt01")).ResourcesJSON)
	initialTunnel := findTransportTunnelForPeer(t, initialResources, "k8s-rt01", "pve-rt06")

	controller.Router = transportRouterWithMode("svnet1", "k8s-rt01", "pair-stable", []api.SAMTransportPeerSpec{
		{NodeRef: "pve-rt01", RemoteEndpoint: "203.0.113.21"},
		{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
	})
	controller.Now = func() time.Time { return now.Add(time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("expanded Reconcile: %v", err)
	}
	expandedResources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "k8s-rt01")).ResourcesJSON)
	expandedTunnel := findTransportTunnelForPeer(t, expandedResources, "k8s-rt01", "pve-rt06")
	if initialTunnel.Address != expandedTunnel.Address {
		t.Fatalf("pair-stable address changed after adding peer: before=%s after=%s", initialTunnel.Address, expandedTunnel.Address)
	}
}

func TestSAMTransportProfilePairStableKeepsPairAddressAcrossPeerOrder(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 4, 45, 0, time.UTC)
	store := testStore(t, now)
	controller := TransportController{
		Router: transportRouterWithMode("svnet1", "k8s-rt01", "pair-stable", []api.SAMTransportPeerSpec{
			{NodeRef: "pve-rt01", RemoteEndpoint: "203.0.113.21"},
			{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
		}),
		Store: store,
		Now:   func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	initialResources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "k8s-rt01")).ResourcesJSON)
	initialTunnel := findTransportTunnelForPeer(t, initialResources, "k8s-rt01", "pve-rt06")

	controller.Router = transportRouterWithMode("svnet1", "k8s-rt01", "pair-stable", []api.SAMTransportPeerSpec{
		{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
		{NodeRef: "pve-rt01", RemoteEndpoint: "203.0.113.21"},
	})
	controller.Now = func() time.Time { return now.Add(time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reordered Reconcile: %v", err)
	}
	reorderedResources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "k8s-rt01")).ResourcesJSON)
	reorderedTunnel := findTransportTunnelForPeer(t, reorderedResources, "k8s-rt01", "pve-rt06")
	if initialTunnel.Address != reorderedTunnel.Address {
		t.Fatalf("pair-stable address changed after reordering peers: before=%s after=%s", initialTunnel.Address, reorderedTunnel.Address)
	}
}

func TestSAMTransportProfilePairStableOverrideEscapesHashCollision(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 4, 50, 0, time.UTC)
	store := testStore(t, now)
	controller := TransportController{
		Router: transportRouterWithMode("svnet1", "pve-rt", "pair-stable", []api.SAMTransportPeerSpec{
			{NodeRef: "node-03", RemoteEndpoint: "203.0.113.20"},
			{
				NodeRef:        "node-50",
				RemoteEndpoint: "203.0.113.21",
				Override: api.SAMTransportPeerOverrideSpec{
					LocalInner:  "10.255.1.126/31",
					RemoteInner: "10.255.1.127",
				},
			},
		}),
		Store: store,
		Now:   func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile with collision override: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "pve-rt")).ResourcesJSON)
	overrideTunnel := findTransportTunnelForPeer(t, resources, "pve-rt", "node-50")
	if overrideTunnel.Address != "10.255.1.126/31" {
		t.Fatalf("override tunnel address = %s, want 10.255.1.126/31", overrideTunnel.Address)
	}
}

func TestSAMTransportProfilePairStableCanonicalInnerPrefixSeed(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 4, 55, 0, time.UTC)
	base := transportRouterWithMode("svnet1", "k8s-rt01", "pair-stable", []api.SAMTransportPeerSpec{
		{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
	})
	baseSpec, err := base.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("base spec: %v", err)
	}
	baseSpec.InnerPrefix = "10.255.1.0/24"
	base.Spec.Resources[0].Spec = baseSpec

	alias := transportRouterWithMode("svnet1", "k8s-rt01", "pair-stable", []api.SAMTransportPeerSpec{
		{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
	})
	aliasSpec, err := alias.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("alias spec: %v", err)
	}
	aliasSpec.InnerPrefix = "10.255.1.5/24"
	alias.Spec.Resources[0].Spec = aliasSpec

	baseStore := testStore(t, now)
	baseController := TransportController{Router: base, Store: baseStore, Now: func() time.Time { return now }}
	if err := baseController.Reconcile(context.Background()); err != nil {
		t.Fatalf("base Reconcile: %v", err)
	}
	baseResources := decodeResources(t, latestPart(t, baseStore, TransportDynamicSource("svnet1", "k8s-rt01")).ResourcesJSON)
	baseTunnel := findTransportTunnelForPeer(t, baseResources, "k8s-rt01", "pve-rt06")

	aliasStore := testStore(t, now)
	aliasController := TransportController{Router: alias, Store: aliasStore, Now: func() time.Time { return now }}
	if err := aliasController.Reconcile(context.Background()); err != nil {
		t.Fatalf("alias Reconcile: %v", err)
	}
	aliasResources := decodeResources(t, latestPart(t, aliasStore, TransportDynamicSource("svnet1", "k8s-rt01")).ResourcesJSON)
	aliasTunnel := findTransportTunnelForPeer(t, aliasResources, "k8s-rt01", "pve-rt06")

	if baseTunnel.Address != aliasTunnel.Address {
		t.Fatalf("pair-stable address differs for equivalent prefixes: base=%s alias=%s", baseTunnel.Address, aliasTunnel.Address)
	}
}

func TestSAMTransportProfileDerivesPeersFromSAMPeerGroup(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 10, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samPeerGroupResource("svnet1-rrs", []api.SAMTransportPeerSpec{{
		NodeRef:        "rr-rt01",
		RemoteEndpoint: "203.0.113.11",
	}}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "203.0.113.11" {
		t.Fatalf("tunnel remote = %q, want peer group endpoint", tunnel.Remote)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	peersFrom, ok := status["peersFrom"].([]any)
	if !ok || len(peersFrom) != 1 {
		t.Fatalf("status peersFrom = %#v, want one source", status["peersFrom"])
	}
}

func TestSAMTransportProfilePeersFromMissingRequiredIsPending(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 20, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	router.Spec.Resources[0].Spec = spec

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON); len(resources) != 0 {
		t.Fatalf("resources = %#v, want none while peersFrom is pending", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Pending" {
		t.Fatalf("status phase = %#v, want Pending status=%#v", status["phase"], status)
	}
}

func TestSAMTransportProfilePeersFromUnionStaticOverridesGroupPeer(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 30, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithTopology("svnet1", "leaf-rt01", []string{"leaf-rt01", "rr-rt01", "rr-rt02"}, []api.SAMTransportPeerSpec{{
		NodeRef:        "rr-rt01",
		RemoteEndpoint: "203.0.113.99",
	}})
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samPeerGroupResource("svnet1-rrs", []api.SAMTransportPeerSpec{
		{NodeRef: "rr-rt01", RemoteEndpoint: "203.0.113.11"},
		{NodeRef: "rr-rt02", RemoteEndpoint: "203.0.113.12"},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	if got, want := len(resources), 6; got != want {
		t.Fatalf("resources = %d, want %d", got, want)
	}
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "203.0.113.99" {
		t.Fatalf("static peer did not override group peer: remote=%q", tunnel.Remote)
	}
}

func TestSAMTransportProfileDerivesPeersAndTopologyFromSAMNodeSet(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 35, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithTopology("svnet1", "leaf-rt01", nil, nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/svnet1-nodes"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samNodeSetResource("svnet1-nodes", []api.SAMNodeSpec{
		{NodeRef: "leaf-rt01", SAMEndpoint: "203.0.113.10/32"},
		{NodeRef: "rr-rt01", SAMEndpoint: "203.0.113.11/32"},
		{NodeRef: "rr-rt02", SAMEndpoint: "203.0.113.12"},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	if got, want := len(resources), 6; got != want {
		t.Fatalf("resources = %d, want %d", got, want)
	}
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "203.0.113.11" {
		t.Fatalf("tunnel remote = %q, want normalized SAM endpoint", tunnel.Remote)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	topology, ok := status["topologyNodeRefs"].([]any)
	if !ok || len(topology) != 3 {
		t.Fatalf("topologyNodeRefs = %#v, want three nodes", status["topologyNodeRefs"])
	}
}

func TestSAMTransportProfileDerivesSAMNodeSetEndpointFromStatus(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 35, 0, time.UTC)
	store := testStore(t, now)
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "rr-underlay", map[string]any{"currentAddress": "203.0.113.11/24"}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}
	router := transportRouterWithTopology("svnet1", "leaf-rt01", nil, nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/svnet1-nodes"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samNodeSetResource("svnet1-nodes", []api.SAMNodeSpec{
		{NodeRef: "leaf-rt01", SAMEndpoint: "203.0.113.10"},
		{NodeRef: "rr-rt01", SAMEndpointFrom: api.StatusValueSourceSpec{Resource: "DHCPv4Client/rr-underlay", Field: "currentAddress"}},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "203.0.113.11" {
		t.Fatalf("tunnel remote = %q, want resolved samEndpointFrom address", tunnel.Remote)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Derived" {
		t.Fatalf("status = %#v, want Derived", status)
	}
}

func TestSAMTransportProfileSAMNodeSetEndpointFromPending(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 35, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/svnet1-nodes"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samNodeSetResource("svnet1-nodes", []api.SAMNodeSpec{
		{NodeRef: "leaf-rt01", SAMEndpoint: "203.0.113.10"},
		{NodeRef: "rr-rt01", SAMEndpointFrom: api.StatusValueSourceSpec{Resource: "DHCPv4Client/rr-underlay", Field: "currentAddress"}},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON); len(resources) != 0 {
		t.Fatalf("resources = %#v, want none while samEndpointFrom is pending", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Pending" || !strings.Contains(strings.Join(testStringSlice(status["pendingSources"]), ","), "DHCPv4Client/rr-underlay.currentAddress") {
		t.Fatalf("status = %#v, want pending samEndpointFrom source", status)
	}
}

func TestSAMTransportProfileStaticPeerOverridesSAMNodeSetPeer(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 36, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithTopology("svnet1", "leaf-rt01", nil, []api.SAMTransportPeerSpec{{
		NodeRef:        "rr-rt01",
		RemoteEndpoint: "203.0.113.99",
	}})
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/svnet1-nodes"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samNodeSetResource("svnet1-nodes", []api.SAMNodeSpec{
		{NodeRef: "leaf-rt01", SAMEndpoint: "203.0.113.10"},
		{NodeRef: "rr-rt01", SAMEndpoint: "203.0.113.11"},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "203.0.113.99" {
		t.Fatalf("static peer did not override node set peer: remote=%q", tunnel.Remote)
	}
}

func TestSAMTransportProfileSAMNodeSetPeersFromMissingRequiredIsPending(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 37, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/missing"}}
	router.Spec.Resources[0].Spec = spec

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON); len(resources) != 0 {
		t.Fatalf("resources = %#v, want none while SAMNodeSet peersFrom is pending", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Pending" {
		t.Fatalf("status phase = %#v, want Pending status=%#v", status["phase"], status)
	}
}

func TestSAMTransportProfileSAMNodeSetPairStableDoesNotRenumberExistingPeer(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 38, 0, time.UTC)
	localAddress := func(nodes []api.SAMNodeSpec) string {
		t.Helper()
		store := testStore(t, now)
		router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
		spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
		if err != nil {
			t.Fatalf("SAMTransportProfile spec: %v", err)
		}
		spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMNodeSet/svnet1-nodes"}}
		router.Spec.Resources[0].Spec = spec
		router.Spec.Resources = append(router.Spec.Resources, samNodeSetResource("svnet1-nodes", nodes))
		controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
		if err := controller.Reconcile(context.Background()); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
		return findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01").Address
	}

	before := localAddress([]api.SAMNodeSpec{
		{NodeRef: "leaf-rt01", SAMEndpoint: "203.0.113.10"},
		{NodeRef: "rr-rt01", SAMEndpoint: "203.0.113.11"},
	})
	after := localAddress([]api.SAMNodeSpec{
		{NodeRef: "leaf-rt01", SAMEndpoint: "203.0.113.10"},
		{NodeRef: "rr-rt01", SAMEndpoint: "203.0.113.11"},
		{NodeRef: "rr-rt02", SAMEndpoint: "203.0.113.12"},
	})
	if before != after {
		t.Fatalf("pair-stable local address changed after adding node: before=%s after=%s", before, after)
	}
}

func TestSAMTransportProfilePublishesSAMPeerGroupDynamicPart(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 40, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "rr-rt01", "pair-stable", []api.SAMTransportPeerSpec{{
		NodeRef:        "leaf-rt01",
		RemoteEndpoint: "203.0.113.21",
	}})
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PublishPeerGroup = true
	spec.LocalEndpoint = ""
	spec.LocalEndpointFrom = api.StatusValueSourceSpec{Resource: "Interface/wg-svnet1", Field: "primaryIPv4"}
	router.Spec.Resources[0].Spec = spec
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "wg-svnet1", map[string]any{"primaryIPv4": "10.252.0.1/32"}); err != nil {
		t.Fatalf("SaveObjectStatus: %v", err)
	}

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportPeerGroupDynamicSource("svnet1")).ResourcesJSON)
	if len(resources) != 1 {
		t.Fatalf("peer group resources = %#v, want one SAMPeerGroup", resources)
	}
	group, err := resources[0].SAMPeerGroupSpec()
	if err != nil {
		t.Fatalf("SAMPeerGroup spec: %v", err)
	}
	if resources[0].Kind != "SAMPeerGroup" || resources[0].Metadata.Name != "svnet1" {
		t.Fatalf("published resource = %s/%s, want SAMPeerGroup/svnet1", resources[0].Kind, resources[0].Metadata.Name)
	}
	if len(group.Peers) != 1 || group.Peers[0].NodeRef != "rr-rt01" || group.Peers[0].RemoteEndpoint != "10.252.0.1" {
		t.Fatalf("published peers = %#v, want concrete rr endpoint", group.Peers)
	}
}

func TestSAMTransportProfileRejectsUnknownAddressingMode(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 4, 58, 0, time.UTC)
	store := testStore(t, now)
	controller := TransportController{
		Router: transportRouterWithMode("svnet1", "k8s-rt01", "invalid-mode", []api.SAMTransportPeerSpec{
			{NodeRef: "pve-rt06", RemoteEndpoint: "203.0.113.26"},
		}),
		Store: store,
		Now:   func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile unknown mode: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if reason, _ := status["reason"].(string); !strings.Contains(reason, "unsupported addressingMode") {
		t.Fatalf("status reason = %#v, want unsupported addressingMode", status["reason"])
	}
}

func TestSAMTransportProfilePeerRemovalReplacesDynamicPart(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := TransportController{
		Router: transportRouter("lab", "pve-rt", []api.SAMTransportPeerSpec{
			{NodeRef: "k8s-rt", RemoteEndpoint: "203.0.113.20"},
			{NodeRef: "cloud-rt", RemoteEndpoint: "203.0.113.30"},
		}),
		Store: store,
		Now:   func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	if resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("lab", "pve-rt")).ResourcesJSON); len(resources) != 6 {
		t.Fatalf("initial resources = %d, want 6", len(resources))
	}

	controller.Router = transportRouter("lab", "pve-rt", []api.SAMTransportPeerSpec{
		{NodeRef: "k8s-rt", RemoteEndpoint: "203.0.113.20"},
	})
	controller.Now = func() time.Time { return now.Add(time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("lab", "pve-rt")).ResourcesJSON)
	if got, want := len(resources), 3; got != want {
		t.Fatalf("resources after peer removal = %d, want %d", got, want)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "lab")
	if status["generatedTunnels"] != float64(1) && status["generatedTunnels"] != 1 {
		t.Fatalf("status = %#v, want generatedTunnels=1", status)
	}
}

func TestSAMTransportProfileCopiesRouteReflectorSettings(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 8, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouter("rr", "k8s-rt01", []api.SAMTransportPeerSpec{{
		NodeRef:        "pve-rt06",
		RemoteEndpoint: "203.0.113.26",
	}})
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.BGP.RouteReflectorClient = true
	spec.BGP.RouteReflectorClusterID = "192.168.1.38"
	router.Spec.Resources[0].Spec = spec

	controller := TransportController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("rr", "k8s-rt01")).ResourcesJSON)
	peer := findTransportBGPPeer(t, resources)
	if !peer.RouteReflectorClient || peer.RouteReflectorClusterID != "192.168.1.38" {
		t.Fatalf("BGPPeer RR settings = client:%v cluster:%q, want true/192.168.1.38", peer.RouteReflectorClient, peer.RouteReflectorClusterID)
	}
}

func TestSAMTransportProfileGeneratesBFDForBGPPeer(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 8, 30, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouter("bfd", "k8s-rt01", []api.SAMTransportPeerSpec{{
		NodeRef:        "pve-rt06",
		RemoteEndpoint: "203.0.113.26",
	}})
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.BGP.BFD = api.SAMTransportBFDSpec{
		Enabled:          true,
		Profile:          "fast",
		MinRx:            "250ms",
		MinTx:            "250ms",
		DetectMultiplier: 4,
	}
	router.Spec.Resources[0].Spec = spec

	controller := TransportController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("bfd", "k8s-rt01")).ResourcesJSON)
	peerName, peer := findTransportBGPPeerResourceForPeer(t, resources, "k8s-rt01", "pve-rt06")
	bfdName, ok := strings.CutPrefix(peer.BFD, "BFD/")
	if !ok {
		t.Fatalf("BGPPeer BFD ref = %q, want BFD/<name>", peer.BFD)
	}
	bfd := findTransportBFD(t, resources, bfdName)
	if bfd.Peer != "BGPPeer/"+peerName {
		t.Fatalf("BFD peer = %q, want BGPPeer/%s", bfd.Peer, peerName)
	}
	if bfd.Profile != "fast" || bfd.MinRx != "250ms" || bfd.MinTx != "250ms" || bfd.DetectMultiplier != 4 {
		t.Fatalf("BFD spec = %#v, want fast 250ms/250ms multiplier 4", bfd)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "bfd")
	if status["generatedBFDs"] != float64(1) && status["generatedBFDs"] != 1 {
		t.Fatalf("status = %#v, want generatedBFDs=1", status)
	}
}

func TestSAMTransportProfileEndpointRouteUsesUnderlayDevice(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 9, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := TransportController{
		Router: transportRouter("wireguard", "pve-rt06", []api.SAMTransportPeerSpec{{
			NodeRef:        "k8s-rt01",
			RemoteEndpoint: "10.252.0.1",
		}}),
		Store: store,
		Now:   func() time.Time { return now },
	}
	spec, err := controller.Router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.UnderlayInterface = "wg-svnet1"
	controller.Router.Spec.Resources[0].Spec = spec

	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("wireguard", "pve-rt06")).ResourcesJSON)
	route := findTransportEndpointRoute(t, resources)
	if route.Device != "wg-svnet1" {
		t.Fatalf("endpoint route device = %q, want wg-svnet1", route.Device)
	}
	if route.DeviceFrom.Resource != "" || route.DeviceFrom.Field != "" {
		t.Fatalf("endpoint route deviceFrom = %#v, want empty so WireGuardInterface underlay does not require Interface/wg-svnet1", route.DeviceFrom)
	}
}

func TestSAMTransportProfileRouteReflectorOverWireGuardUnderlay(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 9, 30, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouter("rr", "k8s-rt01", []api.SAMTransportPeerSpec{{
		NodeRef:        "pve-rt06",
		RemoteEndpoint: "10.99.0.26",
	}})
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.UnderlayInterface = "wg-svnet1"
	spec.BGP.RouteReflectorClient = true
	spec.BGP.RouteReflectorClusterID = "10.99.0.1"
	router.Spec.Resources[0].Spec = spec

	controller := TransportController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("rr", "k8s-rt01")).ResourcesJSON)
	peer := findTransportBGPPeer(t, resources)
	if !peer.RouteReflectorClient || peer.RouteReflectorClusterID != "10.99.0.1" {
		t.Fatalf("BGPPeer RR settings = client:%v cluster:%q, want true/10.99.0.1", peer.RouteReflectorClient, peer.RouteReflectorClusterID)
	}
	route := findTransportEndpointRoute(t, resources)
	if route.Device != "wg-svnet1" {
		t.Fatalf("endpoint route device = %q, want wg-svnet1", route.Device)
	}
	if route.DeviceFrom.Resource != "" || route.DeviceFrom.Field != "" {
		t.Fatalf("endpoint route deviceFrom = %#v, want empty for WireGuard underlay", route.DeviceFrom)
	}
}

func TestSAMTransportProfileDeletionUpsertsEmptyPart(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 10, 0, 0, time.UTC)
	store := testStore(t, now)
	controller := TransportController{
		Router: transportRouter("lab", "pve-rt", []api.SAMTransportPeerSpec{{
			NodeRef:        "k8s-rt",
			RemoteEndpoint: "203.0.113.20",
		}}),
		Store: store,
		Now:   func() time.Time { return now },
	}
	source := TransportDynamicSource("lab", "pve-rt")
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("initial Reconcile: %v", err)
	}
	if resources := decodeResources(t, latestPart(t, store, source).ResourcesJSON); len(resources) == 0 {
		t.Fatalf("initial resources empty, want generated resources")
	}

	controller.Router = &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}}
	controller.Now = func() time.Time { return now.Add(time.Minute) }
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("delete Reconcile: %v", err)
	}
	if resources := decodeResources(t, latestPart(t, store, source).ResourcesJSON); len(resources) != 0 {
		t.Fatalf("resources after profile deletion = %#v, want empty part", resources)
	}
}

func transportRouter(profile, self string, peers []api.SAMTransportPeerSpec) *api.Router {
	return transportRouterWithTopology(profile, self, nil, peers)
}

func transportRouterWithMode(profile, self, mode string, peers []api.SAMTransportPeerSpec) *api.Router {
	router := transportRouterWithTopology(profile, self, nil, peers)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		panic(err)
	}
	spec.AddressingMode = mode
	router.Spec.Resources[0].Spec = spec
	return router
}

func transportRouterWithTopology(profile, self string, topology []string, peers []api.SAMTransportPeerSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
			Metadata: api.ObjectMeta{Name: profile},
			Spec: api.SAMTransportProfileSpec{
				SelfNodeRef:       self,
				Mode:              "ipip",
				InnerPrefix:       "10.255.1.0/24",
				TopologyNodeRefs:  topology,
				UnderlayInterface: "wan",
				LocalEndpoint:     "198.51.100.10",
				BGP: api.SAMTransportBGPProfileSpec{
					RouterRef: "BGPRouter/mobility",
					PeerASN:   64512,
				},
				Peers: peers,
			},
		}}},
	}
}

func samPeerGroupResource(name string, peers []api.SAMTransportPeerSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.SAMPeerGroupSpec{Peers: peers},
	}
}

func samNodeSetResource(name string, nodes []api.SAMNodeSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.SAMNodeSetSpec{Nodes: nodes},
	}
}

func findTransportTunnel(t *testing.T, resources []api.Resource) api.TunnelInterfaceSpec {
	t.Helper()
	for _, resource := range resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		spec, err := resource.TunnelInterfaceSpec()
		if err != nil {
			t.Fatalf("TunnelInterface spec: %v", err)
		}
		return spec
	}
	t.Fatalf("TunnelInterface not found in %#v", resources)
	return api.TunnelInterfaceSpec{}
}

func findTransportEndpointRoute(t *testing.T, resources []api.Resource) api.IPv4RouteSpec {
	t.Helper()
	for _, resource := range resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPv4Route" {
			continue
		}
		spec, err := resource.IPv4RouteSpec()
		if err != nil {
			t.Fatalf("IPv4Route spec: %v", err)
		}
		return spec
	}
	t.Fatalf("IPv4Route not found in %#v", resources)
	return api.IPv4RouteSpec{}
}

func findTransportBGPPeer(t *testing.T, resources []api.Resource) api.BGPPeerSpec {
	t.Helper()
	for _, resource := range resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil {
			t.Fatalf("BGPPeer spec: %v", err)
		}
		return spec
	}
	t.Fatalf("BGPPeer not found in %#v", resources)
	return api.BGPPeerSpec{}
}

func findTransportTunnelForPeer(t *testing.T, resources []api.Resource, self, peer string) api.TunnelInterfaceSpec {
	t.Helper()
	for _, resource := range resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		if resource.Metadata.Annotations["mobility.routerd.net/self-node"] != self ||
			resource.Metadata.Annotations["mobility.routerd.net/peer-node"] != peer {
			continue
		}
		spec, err := resource.TunnelInterfaceSpec()
		if err != nil {
			t.Fatalf("TunnelInterface spec: %v", err)
		}
		return spec
	}
	t.Fatalf("TunnelInterface for %s/%s not found in %#v", self, peer, resources)
	return api.TunnelInterfaceSpec{}
}

func findTransportBGPPeerForPeer(t *testing.T, resources []api.Resource, self, peer string) api.BGPPeerSpec {
	t.Helper()
	_, spec := findTransportBGPPeerResourceForPeer(t, resources, self, peer)
	return spec
}

func findTransportBGPPeerResourceForPeer(t *testing.T, resources []api.Resource, self, peer string) (string, api.BGPPeerSpec) {
	t.Helper()
	for _, resource := range resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		if resource.Metadata.Annotations["mobility.routerd.net/self-node"] != self ||
			resource.Metadata.Annotations["mobility.routerd.net/peer-node"] != peer {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil {
			t.Fatalf("BGPPeer spec: %v", err)
		}
		return resource.Metadata.Name, spec
	}
	t.Fatalf("BGPPeer for %s/%s not found in %#v", self, peer, resources)
	return "", api.BGPPeerSpec{}
}

func findTransportBFD(t *testing.T, resources []api.Resource, name string) api.BFDSpec {
	t.Helper()
	for _, resource := range resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BFD" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.BFDSpec()
		if err != nil {
			t.Fatalf("BFD spec: %v", err)
		}
		return spec
	}
	t.Fatalf("BFD/%s not found in %#v", name, resources)
	return api.BFDSpec{}
}
