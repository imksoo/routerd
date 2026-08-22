// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
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

func sameStringSetForTest(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, value := range a {
		seen[value]++
	}
	for _, value := range b {
		seen[value]--
		if seen[value] < 0 {
			return false
		}
	}
	return true
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
	if aPeer.PassiveMode || !bPeer.PassiveMode {
		t.Fatalf("BGP active/passive = cloud:%v onprem:%v, want lower /31 endpoint active and upper endpoint passive", aPeer.PassiveMode, bPeer.PassiveMode)
	}
}

func TestSAMTransportProfileDerivesPeerAddressOnlyForFreeBSD(t *testing.T) {
	now := time.Date(2026, 7, 24, 21, 55, 0, 0, time.UTC)
	for _, tc := range []struct {
		name            string
		targetOS        platform.OS
		wantPeerAddress string
		wantTunnelName  string
	}{
		{name: "linux", targetOS: platform.OSLinux, wantTunnelName: compactHashedName("samt", "demo", "cloud-rt", "onprem-rt")},
		{name: "freebsd", targetOS: platform.OSFreeBSD, wantPeerAddress: "10.255.1.1", wantTunnelName: "gif0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := testStore(t, now)
			controller := TransportController{
				Router: transportRouter("demo", "cloud-rt", []api.SAMTransportPeerSpec{{
					NodeRef:        "onprem-rt",
					RemoteEndpoint: "203.0.113.20",
				}}),
				Store: store,
				Now:   func() time.Time { return now },
				OS:    tc.targetOS,
			}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("demo", "cloud-rt")).ResourcesJSON)
			tunnelName, tunnel := findTransportTunnelResource(t, resources)
			if tunnel.Address != "10.255.1.0/31" {
				t.Fatalf("Address = %q, want 10.255.1.0/31", tunnel.Address)
			}
			if tunnel.PeerAddress != tc.wantPeerAddress {
				t.Fatalf("PeerAddress = %q, want %q", tunnel.PeerAddress, tc.wantPeerAddress)
			}
			if tunnelName != tc.wantTunnelName {
				t.Fatalf("TunnelInterface name = %q, want %q", tunnelName, tc.wantTunnelName)
			}
		})
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

func TestSAMTransportProfilePairStableCollisionIsDegraded(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 4, 50, 0, time.UTC)
	store := testStore(t, now)
	controller := TransportController{
		Router: transportRouterWithMode("svnet1", "pve-rt", "pair-stable", []api.SAMTransportPeerSpec{
			{NodeRef: "node-03", RemoteEndpoint: "203.0.113.20"},
			{NodeRef: "node-50", RemoteEndpoint: "203.0.113.21"},
		}),
		Store: store,
		Now:   func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile with collision: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "pve-rt")).ResourcesJSON)
	if len(resources) != 0 {
		t.Fatalf("resources = %#v, want none for pair-stable collision", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Degraded" {
		t.Fatalf("status phase = %#v, want Degraded: %#v", status["phase"], status)
	}
}

func TestSAMTransportProfilePairStableDirectCollisionKeepsRRFallback(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 2, 30, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "pve-rt", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.31"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs"},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
	}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources,
		samRRSetResource("cloudedge-rrs", []api.SAMNodeSpec{{
			NodeRef: "node-03", SAMEndpoint: "203.0.113.20", RouteReflector: true,
		}}),
		samDirectPeerGroupResource("cloudedge-direct-leaves", "SAMEnrollmentPolicy/cloudedge-leaves", mobilityconfig.SAMTransportMeshFingerprint(spec), []api.SAMNodeSpec{{
			NodeRef: "node-50", SAMEndpoint: "203.0.113.21",
		}}),
	)

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "pve-rt")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 1; got != want {
		t.Fatalf("TunnelInterface count = %d, want only RR fallback resources=%#v", got, resources)
	}
	_ = findTransportTunnelForPeer(t, resources, "pve-rt", "node-03")
	for _, resource := range resources {
		if resource.Metadata.Annotations["mobility.routerd.net/peer-node"] == "node-50" {
			t.Fatalf("pair-stable collision retained direct peer resource: %#v", resources)
		}
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Derived" {
		t.Fatalf("status phase = %#v, want Derived with RR fallback", status["phase"])
	}
	rows, ok := status["peersFrom"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("peersFrom = %#v, want RR and direct sources", status["peersFrom"])
	}
	directRow, ok := rows[1].(map[string]any)
	if !ok || directRow["phase"] != "Incompatible" || directRow["reason"] != "direct peer-group pair-stable address slot collides with fallback topology" {
		t.Fatalf("direct peersFrom status = %#v, want pair-stable collision fallback", rows[1])
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

func TestSAMTransportProfileUsesSyncedPeerGroupInsteadOfStaticResource(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 10, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs"}}
	router.Spec.Resources[0].Spec = spec
	writePeerGroupPart(t, store, PeerGroupSyncDynamicSource("svnet1-rrs"), "svnet1-rrs", []api.SAMTransportPeerSpec{{
		NodeRef:        "rr-rt01",
		RemoteEndpoint: "203.0.113.11",
	}}, now)
	// A stale top-level shape must not affect transport derivation. The only
	// valid peer-group input is the sync cache above.
	router.Spec.Resources = append(router.Spec.Resources, samPeerGroupResource("svnet1-rrs", []api.SAMTransportPeerSpec{{
		NodeRef:        "rr-rt01",
		RemoteEndpoint: "203.0.113.99",
	}}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "203.0.113.11" {
		t.Fatalf("tunnel remote = %q, want synced peer group endpoint", tunnel.Remote)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	peersFrom, ok := status["peersFrom"].([]any)
	if !ok || len(peersFrom) != 1 {
		t.Fatalf("status peersFrom = %#v, want one source", status["peersFrom"])
	}
}

func TestSAMTransportProfileOptionalPeerGroupDoesNotSyncOrBlock(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 15, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("svnet1", "leaf-rt01", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMPeerGroup/svnet1-rrs", Optional: true}}
	router.Spec.Resources[0].Spec = spec
	syncCalled := false
	controller := TransportController{
		Router: router,
		Store:  store,
		PeerGroupSync: &PeerGroupSyncClient{
			Store: store,
			Discover: func(context.Context, *api.Router, string) ([]netip.Addr, error) {
				syncCalled = true
				return nil, nil
			},
		},
		Now: func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if syncCalled {
		t.Fatal("optional SAMPeerGroup attempted a network sync")
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "svnet1")
	if status["phase"] != "Derived" {
		t.Fatalf("status phase = %#v, want Derived status=%#v", status["phase"], status)
	}
	peersFrom, ok := status["peersFrom"].([]any)
	if !ok || len(peersFrom) != 1 {
		t.Fatalf("peersFrom status = %#v, want one source", status["peersFrom"])
	}
	if source, ok := peersFrom[0].(map[string]any); !ok || source["phase"] != "Missing" {
		t.Fatalf("optional peersFrom = %#v, want Missing", peersFrom[0])
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

func TestSAMTransportProfileConsumesSAMRRSetWithoutWireGuard(t *testing.T) {
	now := time.Date(2026, 6, 28, 9, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("leaf-pve", "leaf-pve", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.21"
	spec.BGP.ImportPolicy.NextHopRewrite = "unchanged"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMRRSet/cloudedge-rrs"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samRRSetResource("cloudedge-rrs", []api.SAMNodeSpec{
		{NodeRef: "rr-a", SAMEndpoint: "10.10.0.2"},
		{NodeRef: "rr-b", SAMEndpoint: "10.10.0.3"},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("leaf-pve", "leaf-pve")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 2; got != want {
		t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
	}
	if got, want := countResources(resources, api.NetAPIVersion, "BGPPeer"), 2; got != want {
		t.Fatalf("BGPPeer count = %d, want %d resources=%#v", got, want, resources)
	}
	if got := countResources(resources, api.NetAPIVersion, "WireGuardPeer"); got != 0 {
		t.Fatalf("WireGuardPeer count = %d, want 0", got)
	}
	rrATunnel := findTransportTunnelForPeer(t, resources, "leaf-pve", "rr-a")
	if rrATunnel.Mode != "ipip" || rrATunnel.Remote != "10.10.0.2" {
		t.Fatalf("rr-a tunnel = %#v, want ipip remote 10.10.0.2", rrATunnel)
	}
	rrBTunnel := findTransportTunnelForPeer(t, resources, "leaf-pve", "rr-b")
	if rrBTunnel.Mode != "ipip" || rrBTunnel.Remote != "10.10.0.3" {
		t.Fatalf("rr-b tunnel = %#v, want ipip remote 10.10.0.3", rrBTunnel)
	}
	for _, peer := range []string{"rr-a", "rr-b"} {
		if got := findTransportBGPPeerForPeer(t, resources, "leaf-pve", peer).ImportPolicy.NextHopRewrite; got != "unchanged" {
			t.Fatalf("non-direct RR peer %s nextHopRewrite = %q, want unchanged", peer, got)
		}
	}
}

func TestSAMTransportProfileDirectLeafPeerPrefersDirectAndKeepsRRs(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("leaf-a", "leaf-a", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.31"
	spec.BGP.ImportPolicy = api.BGPImportPolicySpec{
		LocalPreference:        100,
		AllowedPrefixes:        []string{"10.77.60.0/24"},
		AllowedPrefixLengthMin: 32,
		AllowedPrefixLengthMax: 32,
		NextHopRewrite:         "unchanged",
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs"},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
	}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources,
		samRRSetResource("cloudedge-rrs", []api.SAMNodeSpec{
			{NodeRef: "rr-a", SAMEndpoint: "10.10.0.2", RouteReflector: true},
			{NodeRef: "rr-b", SAMEndpoint: "10.10.0.3", RouteReflector: true},
		}),
		samDirectPeerGroupResource("cloudedge-direct-leaves", "SAMEnrollmentPolicy/cloudedge-leaves", mobilityconfig.SAMTransportMeshFingerprint(spec), []api.SAMNodeSpec{{
			NodeRef:     "leaf-b",
			SAMEndpoint: "10.20.0.32",
		}}),
	)

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("leaf-a", "leaf-a")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 3; got != want {
		t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
	}
	for _, peer := range []string{"rr-a", "rr-b"} {
		if got := findTransportBGPPeerForPeer(t, resources, "leaf-a", peer).ImportPolicy; got.LocalPreference != 100 || got.NextHopRewrite != "peer-address" {
			t.Fatalf("RR peer %s import policy = %#v, want peer-address RR fallback", peer, got)
		}
	}
	direct := findTransportBGPPeerForPeer(t, resources, "leaf-a", "leaf-b").ImportPolicy
	if direct.LocalPreference != mobilityconfig.DefaultSAMTransportDirectLocalPreference || direct.NextHopRewrite != "peer-address" {
		t.Fatalf("direct leaf import policy = %#v, want local preference %d and peer-address next hop", direct, mobilityconfig.DefaultSAMTransportDirectLocalPreference)
	}
	if got, want := direct.AllowedPrefixes, []string{"10.77.60.200/32"}; !sameStringSetForTest(got, want) {
		t.Fatalf("direct leaf import prefixes = %#v, want signed peer-owned %#v", got, want)
	}
	if got, want := direct.RequiredCommunities, []string{bgpstate.MobilityNodeIdentityCommunity("leaf-b")}; !sameStringSetForTest(got, want) {
		t.Fatalf("direct required communities = %#v, want %#v", got, want)
	}
	forbidden := []string{
		bgpstate.MobilityNodeIdentityCommunity("leaf-a"),
		bgpstate.MobilityNodeIdentityCommunity("rr-a"),
		bgpstate.MobilityNodeIdentityCommunity("rr-b"),
	}
	if !sameStringSetForTest(direct.ForbiddenCommunities, forbidden) {
		t.Fatalf("direct forbidden communities = %#v, want %#v", direct.ForbiddenCommunities, forbidden)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "leaf-a")
	rows, ok := status["peersFrom"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("peersFrom = %#v, want RR and direct sources", status["peersFrom"])
	}
	if directRow, ok := rows[1].(map[string]any); !ok || directRow["phase"] != "Direct" {
		t.Fatalf("direct peersFrom status = %#v, want Direct", rows[1])
	}
}

func TestDirectTransportBGPImportPolicyPreference(t *testing.T) {
	base := api.BGPImportPolicySpec{LocalPreference: 100}
	for _, test := range []struct {
		name       string
		configured uint32
		want       uint32
	}{
		{name: "default", want: mobilityconfig.DefaultSAMTransportDirectLocalPreference},
		{name: "configured", configured: 250, want: 250},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := directTransportBGPImportPolicy(base, test.configured)
			if got.LocalPreference != test.want || got.NextHopRewrite != "peer-address" {
				t.Fatalf("direct import policy = %#v, want localPreference=%d nextHopRewrite=peer-address", got, test.want)
			}
		})
	}
}

func TestSAMTransportProfileDirectLeafAbsenceLeavesRRFallbackDerived(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 1, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("leaf-a", "leaf-a", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.31"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs"},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
	}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samRRSetResource("cloudedge-rrs", []api.SAMNodeSpec{
		{NodeRef: "rr-a", SAMEndpoint: "10.10.0.2", RouteReflector: true},
		{NodeRef: "rr-b", SAMEndpoint: "10.10.0.3", RouteReflector: true},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("leaf-a", "leaf-a")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 2; got != want {
		t.Fatalf("TunnelInterface count = %d, want only RR fallbacks resources=%#v", got, resources)
	}
	if hasTransportResourceForPeer(resources, "leaf-b") {
		t.Fatalf("direct leaf resources remain despite absent direct group: %#v", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "leaf-a")
	if status["phase"] != "Derived" {
		t.Fatalf("status phase = %#v, want Derived with RR fallback", status["phase"])
	}
	rows, ok := status["peersFrom"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("peersFrom = %#v, want RR and direct sources", status["peersFrom"])
	}
	if directRow, ok := rows[1].(map[string]any); !ok || directRow["phase"] != "Unavailable" {
		t.Fatalf("direct peersFrom status = %#v, want Unavailable", rows[1])
	}
}

func TestSAMTransportProfileDirectLeafWaitsForUsableRRFallback(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 1, 30, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("leaf-a", "leaf-a", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.31"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs"},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
	}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources,
		// The RRSet exists but has no usable endpoint. It must not be treated
		// as a valid fallback merely because the direct group arrived.
		samRRSetResource("cloudedge-rrs", []api.SAMNodeSpec{{NodeRef: "rr-a", RouteReflector: true}}),
		samDirectPeerGroupResource("cloudedge-direct-leaves", "SAMEnrollmentPolicy/cloudedge-leaves", mobilityconfig.SAMTransportMeshFingerprint(spec), []api.SAMNodeSpec{{
			NodeRef:     "leaf-b",
			SAMEndpoint: "10.20.0.32",
		}}),
	)

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("leaf-a", "leaf-a")).ResourcesJSON)
	if got := countResources(resources, api.HybridAPIVersion, "TunnelInterface"); got != 0 {
		t.Fatalf("TunnelInterface count = %d, want no direct bootstrap resources=%#v", got, resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "leaf-a")
	rows, ok := status["peersFrom"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("peersFrom = %#v, want RR and direct sources", status["peersFrom"])
	}
	directRow, ok := rows[1].(map[string]any)
	if !ok || directRow["phase"] != "Unavailable" || directRow["reason"] != "matching RR fallback SAMRRSet has no usable peer" {
		t.Fatalf("direct peersFrom status = %#v, want unavailable RR-fallback gate", rows[1])
	}
}

func TestSAMTransportProfileDirectLeafRequiresMatchingRRFallbackPolicy(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 1, 45, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("leaf-a", "leaf-a", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.31"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/policy-a-rrs"},
		{Resource: "SAMRRSet/policy-b-rrs"},
		{Resource: "SAMPeerGroup/policy-b-direct", Direct: true},
	}
	router.Spec.Resources[0].Spec = spec
	rrA := samRRSetResource("policy-a-rrs", []api.SAMNodeSpec{{NodeRef: "rr-a", SAMEndpoint: "10.10.0.2", RouteReflector: true}})
	rrASpec, err := rrA.SAMRRSetSpec()
	if err != nil {
		t.Fatalf("policy-a RRSet spec: %v", err)
	}
	rrASpec.EnrollmentPolicyRef = "SAMEnrollmentPolicy/policy-a"
	rrA.Spec = rrASpec
	rrB := samRRSetResource("policy-b-rrs", []api.SAMNodeSpec{{NodeRef: "rr-b", RouteReflector: true}})
	rrBSpec, err := rrB.SAMRRSetSpec()
	if err != nil {
		t.Fatalf("policy-b RRSet spec: %v", err)
	}
	rrBSpec.EnrollmentPolicyRef = "SAMEnrollmentPolicy/policy-b"
	rrB.Spec = rrBSpec
	router.Spec.Resources = append(router.Spec.Resources,
		rrA,
		rrB,
		samDirectPeerGroupResource("policy-b-direct", "SAMEnrollmentPolicy/policy-b", mobilityconfig.SAMTransportMeshFingerprint(spec), []api.SAMNodeSpec{{NodeRef: "leaf-b", SAMEndpoint: "10.20.0.32"}}),
	)

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("leaf-a", "leaf-a")).ResourcesJSON)
	if hasTransportResourceForPeer(resources, "leaf-b") {
		t.Fatalf("direct peer used a different policy's RR fallback: %#v", resources)
	}
	if got := countResources(resources, api.HybridAPIVersion, "TunnelInterface"); got != 1 {
		t.Fatalf("TunnelInterface count = %d, want only usable policy-a RR fallback", got)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "leaf-a")
	rows, ok := status["peersFrom"].([]any)
	if !ok || len(rows) != 3 {
		t.Fatalf("peersFrom = %#v, want two RRSets and direct source", status["peersFrom"])
	}
	if directRow, ok := rows[2].(map[string]any); !ok || directRow["phase"] != "Unavailable" || directRow["reason"] != "matching RR fallback SAMRRSet has no usable peer" {
		t.Fatalf("direct peersFrom status = %#v, want policy-scoped RR gate", rows[2])
	}
}

func TestSAMTransportProfileRejectsDirectGroupCollisionAndKeepsRRFallback(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 2, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("leaf-a", "leaf-a", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.31"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs"},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
	}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources,
		samRRSetResource("cloudedge-rrs", []api.SAMNodeSpec{
			{NodeRef: "rr-a", SAMEndpoint: "10.10.0.2", RouteReflector: true},
			{NodeRef: "rr-b", SAMEndpoint: "10.10.0.3", RouteReflector: true},
		}),
		samDirectPeerGroupResource("cloudedge-direct-leaves", "SAMEnrollmentPolicy/cloudedge-leaves", mobilityconfig.SAMTransportMeshFingerprint(spec), []api.SAMNodeSpec{
			{NodeRef: "leaf-b", SAMEndpoint: "10.20.0.32"},
			// This node masquerades as a leaf (RouteReflector=false) but collides
			// with the already rendered RR. A valid entry before it proves the
			// controller neither replaces the RR nor leaks partial direct state.
			{NodeRef: "rr-a", SAMEndpoint: "10.20.0.33"},
		}),
	)

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("leaf-a", "leaf-a")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 2; got != want {
		t.Fatalf("TunnelInterface count = %d, want only RR fallbacks resources=%#v", got, resources)
	}
	if hasTransportResourceForPeer(resources, "leaf-b") {
		t.Fatalf("partial direct peer remained after group rejection: %#v", resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "leaf-a")
	if status["phase"] != "Derived" {
		t.Fatalf("status phase = %#v, want Derived with RR fallback", status["phase"])
	}
	rows, ok := status["peersFrom"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("peersFrom = %#v, want RR and direct sources", status["peersFrom"])
	}
	if directRow, ok := rows[1].(map[string]any); !ok || directRow["phase"] != "Incompatible" {
		t.Fatalf("direct peersFrom status = %#v, want Incompatible", rows[1])
	}
}

func TestSAMTransportProfileMalformedDirectGroupKeepsRRFallback(t *testing.T) {
	now := time.Date(2026, 8, 22, 10, 3, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("leaf-a", "leaf-a", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.31"
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{
		{Resource: "SAMRRSet/cloudedge-rrs"},
		{Resource: "SAMPeerGroup/cloudedge-direct-leaves", Direct: true},
	}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources,
		samRRSetResource("cloudedge-rrs", []api.SAMNodeSpec{
			{NodeRef: "rr-a", SAMEndpoint: "10.10.0.2", RouteReflector: true},
			{NodeRef: "rr-b", SAMEndpoint: "10.10.0.3", RouteReflector: true},
		}),
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
			Metadata: api.ObjectMeta{Name: "cloudedge-direct-leaves"},
			Spec:     map[string]any{"nodes": "not-a-node-list"},
		},
	)

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("leaf-a", "leaf-a")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 2; got != want {
		t.Fatalf("TunnelInterface count = %d, want only RR fallbacks resources=%#v", got, resources)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "leaf-a")
	rows, ok := status["peersFrom"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("peersFrom = %#v, want RR and direct sources", status["peersFrom"])
	}
	if directRow, ok := rows[1].(map[string]any); !ok || directRow["phase"] != "Incompatible" {
		t.Fatalf("direct peersFrom status = %#v, want Incompatible", rows[1])
	}
}

func TestSAMTransportProfileConsumesSAMRRSetWithFOUWithoutWireGuard(t *testing.T) {
	now := time.Date(2026, 6, 28, 9, 2, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("leaf-b", "leaf-b", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Mode = "fou"
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.20.0.32"
	spec.EncapSport = 5555
	spec.EncapDport = 5555
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMRRSet/cloudedge-rrs"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samRRSetResource("cloudedge-rrs", []api.SAMNodeSpec{
		{NodeRef: "rr-a", SAMEndpoint: "10.10.0.2"},
		{NodeRef: "rr-b", SAMEndpoint: "10.10.0.3"},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("leaf-b", "leaf-b")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 2; got != want {
		t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
	}
	if got, want := countResources(resources, api.NetAPIVersion, "BGPPeer"), 2; got != want {
		t.Fatalf("BGPPeer count = %d, want %d resources=%#v", got, want, resources)
	}
	if got := countResources(resources, api.NetAPIVersion, "WireGuardPeer"); got != 0 {
		t.Fatalf("WireGuardPeer count = %d, want 0", got)
	}
	for _, peer := range []string{"rr-a", "rr-b"} {
		tunnel := findTransportTunnelForPeer(t, resources, "leaf-b", peer)
		if tunnel.Mode != "fou" || tunnel.EncapSport != 5555 || tunnel.EncapDport != 5555 {
			t.Fatalf("%s tunnel = %#v, want fou encap ports 5555/5555", peer, tunnel)
		}
		_ = findTransportBGPPeerForPeer(t, resources, "leaf-b", peer)
	}
}

func TestCloudEdgeDynamicLeafExamplesMaterializeDualRRTransports(t *testing.T) {
	now := time.Date(2026, 6, 28, 9, 4, 0, 0, time.UTC)
	cases := []struct {
		name         string
		example      string
		profile      string
		mode         string
		encryption   string
		wantWGPeers  int
		wantEncap    bool
		policy       string
		wantEndpoint map[string]string
	}{
		{
			name:       "leaf-a wireguard underlay",
			example:    "cloudedge-dynamic-leaf-a-wg.yaml",
			profile:    "leaf-a",
			mode:       "ipip",
			encryption: "wireguard",
			policy:     "cloudedge-public-wg-leaves",
			wantEndpoint: map[string]string{
				"rr-a": "10.20.0.2",
				"rr-b": "10.20.0.3",
			},
		},
		{
			name:       "leaf-b fou private underlay",
			example:    "cloudedge-dynamic-leaf-b-fou.yaml",
			profile:    "leaf-b",
			mode:       "fou",
			encryption: "none",
			wantEncap:  true,
			policy:     "cloudedge-private-fou-leaves",
			wantEndpoint: map[string]string{
				"rr-a": "10.10.0.2",
				"rr-b": "10.10.0.3",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, err := config.Load(filepath.Join("..", "..", "..", "examples", tc.example))
			if err != nil {
				t.Fatalf("load %s: %v", tc.example, err)
			}
			if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
				t.Fatalf("validate %s: %v", tc.example, err)
			}
			profileSpec := exampleSAMTransportProfile(t, router, tc.profile)
			if profileSpec.Mode != tc.mode || profileSpec.Encryption != tc.encryption {
				t.Fatalf("SAMTransportProfile/%s mode/encryption = %s/%s, want %s/%s", tc.profile, profileSpec.Mode, profileSpec.Encryption, tc.mode, tc.encryption)
			}
			store := testStore(t, now)
			seedFetchedCloudEdgeRRSet(t, store, now, tc.policy)
			effective := effectiveWithAdmissionState(t, router, store, now)
			controller := TransportController{Router: effective, Store: store, Now: func() time.Time { return now }}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			resources := decodeResources(t, latestPart(t, store, TransportDynamicSource(tc.profile, tc.profile)).ResourcesJSON)
			if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 2; got != want {
				t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
			}
			if got, want := countResources(resources, api.NetAPIVersion, "BGPPeer"), 2; got != want {
				t.Fatalf("BGPPeer count = %d, want %d resources=%#v", got, want, resources)
			}
			if got := countResources(resources, api.NetAPIVersion, "WireGuardPeer"); got != tc.wantWGPeers {
				t.Fatalf("WireGuardPeer count = %d, want %d", got, tc.wantWGPeers)
			}
			for peer, endpoint := range tc.wantEndpoint {
				tunnel := findTransportTunnelForPeer(t, resources, tc.profile, peer)
				if tunnel.Mode != tc.mode || tunnel.Remote != endpoint {
					t.Fatalf("%s tunnel = %#v, want mode %s remote %s", peer, tunnel, tc.mode, endpoint)
				}
				if tc.wantEncap && (tunnel.EncapSport != 5555 || tunnel.EncapDport != 5555) {
					t.Fatalf("%s tunnel encap ports = %d/%d, want 5555/5555", peer, tunnel.EncapSport, tunnel.EncapDport)
				}
				_, bgpPeer := findTransportBGPPeerResourceForPeer(t, resources, tc.profile, peer)
				if len(bgpPeer.Peers) != 1 || strings.TrimSpace(bgpPeer.Peers[0]) == "" {
					t.Fatalf("%s BGP peer = %#v, want one derived RR tunnel address", peer, bgpPeer)
				}
			}
		})
	}
}

func TestCloudEdgeDynamicRRExamplesMaterializeMixedAdmissionWithoutBGPPeers(t *testing.T) {
	now := time.Date(2026, 6, 28, 9, 6, 0, 0, time.UTC)
	cases := []struct {
		name       string
		example    string
		self       string
		profile    string
		peer       string
		mode       string
		encryption string
		remote     string
		wantEncap  bool
	}{
		{
			name:       "rr-a admits leaf-pve private ipip",
			example:    "cloudedge-dynamic-rr-a-hub.yaml",
			self:       "rr-a",
			profile:    "rr-a",
			peer:       "leaf-pve",
			mode:       "ipip",
			encryption: "none",
			remote:     "10.20.0.21",
		},
		{
			name:       "rr-a admits leaf-a public wireguard ipip",
			example:    "cloudedge-dynamic-rr-a-hub.yaml",
			self:       "rr-a",
			profile:    "rr-a-wg",
			peer:       "leaf-a",
			mode:       "ipip",
			encryption: "wireguard",
			remote:     "10.20.0.31",
		},
		{
			name:       "rr-a admits leaf-b private fou",
			example:    "cloudedge-dynamic-rr-a-hub.yaml",
			self:       "rr-a",
			profile:    "rr-a-fou",
			peer:       "leaf-b",
			mode:       "fou",
			encryption: "none",
			remote:     "10.20.0.32",
			wantEncap:  true,
		},
		{
			name:       "rr-b admits leaf-pve private ipip",
			example:    "cloudedge-dynamic-rr-b-hub.yaml",
			self:       "rr-b",
			profile:    "rr-b",
			peer:       "leaf-pve",
			mode:       "ipip",
			encryption: "none",
			remote:     "10.20.0.21",
		},
		{
			name:       "rr-b admits leaf-a public wireguard ipip",
			example:    "cloudedge-dynamic-rr-b-hub.yaml",
			self:       "rr-b",
			profile:    "rr-b-wg",
			peer:       "leaf-a",
			mode:       "ipip",
			encryption: "wireguard",
			remote:     "10.20.0.31",
		},
		{
			name:       "rr-b admits leaf-b private fou",
			example:    "cloudedge-dynamic-rr-b-hub.yaml",
			self:       "rr-b",
			profile:    "rr-b-fou",
			peer:       "leaf-b",
			mode:       "fou",
			encryption: "none",
			remote:     "10.20.0.32",
			wantEncap:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, err := config.Load(filepath.Join("..", "..", "..", "examples", tc.example))
			if err != nil {
				t.Fatalf("load %s: %v", tc.example, err)
			}
			if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
				t.Fatalf("validate %s: %v", tc.example, err)
			}
			if got := countResources(router.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim"); got != 0 {
				t.Fatalf("base %s SAMEnrollmentClaim count = %d, want 0", tc.example, got)
			}
			store := testStore(t, now)
			seedSubmittedClaimsFromFixture(t, store, now, "cloudedge-rr-claims-seed.yaml")
			effective := effectiveWithAdmissionState(t, router, store, now)
			if got := countResources(effective.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim"); got != 3 {
				t.Fatalf("effective %s SAMEnrollmentClaim count = %d, want 3 from admission state", tc.example, got)
			}
			profileSpec := exampleSAMTransportProfile(t, effective, tc.profile)
			if profileSpec.Mode != tc.mode || profileSpec.Encryption != tc.encryption {
				t.Fatalf("SAMTransportProfile/%s mode/encryption = %s/%s, want %s/%s", tc.profile, profileSpec.Mode, profileSpec.Encryption, tc.mode, tc.encryption)
			}
			if profileSpec.BGP.GeneratePeers == nil || *profileSpec.BGP.GeneratePeers {
				t.Fatalf("SAMTransportProfile/%s generatePeers = %#v, want false", tc.profile, profileSpec.BGP.GeneratePeers)
			}
			controller := TransportController{Router: effective, Store: store, Now: func() time.Time { return now }}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			resources := decodeResources(t, latestPart(t, store, TransportDynamicSource(tc.profile, tc.self)).ResourcesJSON)
			if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 1; got != want {
				t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
			}
			if got := countResources(resources, api.NetAPIVersion, "BGPPeer"); got != 0 {
				t.Fatalf("RR generated BGPPeer count = %d, want 0 resources=%#v", got, resources)
			}
			tunnel := findTransportTunnelForPeer(t, resources, tc.self, tc.peer)
			if tunnel.Mode != tc.mode || tunnel.Remote != tc.remote {
				t.Fatalf("%s tunnel = %#v, want mode %s remote %s", tc.peer, tunnel, tc.mode, tc.remote)
			}
			if tc.wantEncap && (tunnel.EncapSport != 5555 || tunnel.EncapDport != 5555) {
				t.Fatalf("%s tunnel encap ports = %d/%d, want 5555/5555", tc.peer, tunnel.EncapSport, tunnel.EncapDport)
			}
		})
	}
}

func TestSAMRRTransportExamplesMaterializeReviewTransports(t *testing.T) {
	now := time.Date(2026, 6, 28, 9, 7, 0, 0, time.UTC)
	t.Run("rr admission generates tunnels without static bgp peers", func(t *testing.T) {
		cases := []struct {
			name      string
			profile   string
			peer      string
			mode      string
			remote    string
			wantEncap bool
		}{
			{
				name:    "wireguard ipip leaf-a admission",
				profile: "pve-rr-a-wg",
				peer:    "pve-leaf-a",
				mode:    "ipip",
				remote:  "10.31.0.21",
			},
			{
				name:    "wireguard ipip leaf-c admission",
				profile: "pve-rr-a-wg",
				peer:    "pve-leaf-c",
				mode:    "ipip",
				remote:  "10.31.0.23",
			},
			{
				name:      "private fou leaf-b admission",
				profile:   "pve-rr-a-fou",
				peer:      "pve-leaf-b",
				mode:      "fou",
				remote:    "10.30.0.22",
				wantEncap: true,
			},
			{
				name:      "private fou leaf-d admission",
				profile:   "pve-rr-a-fou",
				peer:      "pve-leaf-d",
				mode:      "fou",
				remote:    "10.30.0.24",
				wantEncap: true,
			},
		}
		router, err := config.Load(filepath.Join("..", "..", "..", "examples", "pve-minimal-rr.yaml"))
		if err != nil {
			t.Fatalf("load pve-minimal-rr.yaml: %v", err)
		}
		if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
			t.Fatalf("validate pve-minimal-rr.yaml: %v", err)
		}
		if got := countResources(router.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim"); got != 0 {
			t.Fatalf("base pve-minimal-rr SAMEnrollmentClaim count = %d, want 0", got)
		}
		store := testStore(t, now)
		seedSubmittedClaimsFromFixture(t, store, now, "pve-minimal-rr-claims-seed.yaml")
		effective := effectiveWithAdmissionState(t, router, store, now)
		if got := countResources(effective.Spec.Resources, api.MobilityAPIVersion, "SAMEnrollmentClaim"); got != 4 {
			t.Fatalf("effective pve-minimal-rr SAMEnrollmentClaim count = %d, want 4 from admission state", got)
		}
		controller := TransportController{Router: effective, Store: store, Now: func() time.Time { return now }}
		if err := controller.Reconcile(context.Background()); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				profileSpec := exampleSAMTransportProfile(t, effective, tc.profile)
				if profileSpec.BGP.GeneratePeers == nil || *profileSpec.BGP.GeneratePeers {
					t.Fatalf("SAMTransportProfile/%s generatePeers = %#v, want false", tc.profile, profileSpec.BGP.GeneratePeers)
				}
				resources := decodeResources(t, latestPart(t, store, TransportDynamicSource(tc.profile, "pve-rr-a")).ResourcesJSON)
				if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 2; got != want {
					t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
				}
				if got := countResources(resources, api.NetAPIVersion, "BGPPeer"); got != 0 {
					t.Fatalf("RR generated BGPPeer count = %d, want 0 resources=%#v", got, resources)
				}
				tunnel := findTransportTunnelForPeer(t, resources, "pve-rr-a", tc.peer)
				if tunnel.Mode != tc.mode || tunnel.Remote != tc.remote {
					t.Fatalf("%s tunnel = %#v, want mode %s remote %s", tc.peer, tunnel, tc.mode, tc.remote)
				}
				if tc.wantEncap && (tunnel.EncapSport != 5555 || tunnel.EncapDport != 5555) {
					t.Fatalf("%s tunnel encap ports = %d/%d, want 5555/5555", tc.peer, tunnel.EncapSport, tunnel.EncapDport)
				}
			})
		}
	})

	t.Run("leaves consume rr set and generate rr-facing bgp peers", func(t *testing.T) {
		cases := []struct {
			name        string
			example     string
			profile     string
			mode        string
			encryption  string
			remotes     map[string]string
			wantEncap   bool
			wantWGIface bool
			policy      string
		}{
			{
				name:        "leaf-a wireguard ipip",
				example:     "cloudedge-dynamic-leaf-a-wg.yaml",
				profile:     "leaf-a",
				mode:        "ipip",
				encryption:  "wireguard",
				policy:      "cloudedge-public-wg-leaves",
				remotes:     map[string]string{"rr-a": "10.20.0.2", "rr-b": "10.20.0.3"},
				wantWGIface: true,
			},
			{
				name:       "leaf-b private fou",
				example:    "cloudedge-dynamic-leaf-b-fou.yaml",
				profile:    "leaf-b",
				mode:       "fou",
				encryption: "none",
				remotes:    map[string]string{"rr-a": "10.10.0.2", "rr-b": "10.10.0.3"},
				wantEncap:  true,
				policy:     "cloudedge-private-fou-leaves",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				router, err := config.Load(filepath.Join("..", "..", "..", "examples", tc.example))
				if err != nil {
					t.Fatalf("load %s: %v", tc.example, err)
				}
				if err := config.ValidateForOS(router, platform.OSLinux); err != nil {
					t.Fatalf("validate %s: %v", tc.example, err)
				}
				if got := countResources(router.Spec.Resources, api.MobilityAPIVersion, "SAMRRSet"); got != 0 {
					t.Fatalf("base %s SAMRRSet count = %d, want 0; it is fetched at runtime", tc.example, got)
				}
				profileSpec := exampleSAMTransportProfile(t, router, tc.profile)
				if profileSpec.Mode != tc.mode || profileSpec.Encryption != tc.encryption {
					t.Fatalf("SAMTransportProfile/%s mode/encryption = %s/%s, want %s/%s", tc.profile, profileSpec.Mode, profileSpec.Encryption, tc.mode, tc.encryption)
				}
				if got := countResources(router.Spec.Resources, api.NetAPIVersion, "WireGuardInterface"); (got > 0) != tc.wantWGIface {
					t.Fatalf("static WireGuardInterface count = %d, want present=%v", got, tc.wantWGIface)
				}
				store := testStore(t, now)
				seedFetchedCloudEdgeRRSet(t, store, now, tc.policy)
				effective := effectiveWithAdmissionState(t, router, store, now)
				controller := TransportController{Router: effective, Store: store, Now: func() time.Time { return now }}
				if err := controller.Reconcile(context.Background()); err != nil {
					t.Fatalf("Reconcile: %v", err)
				}
				resources := decodeResources(t, latestPart(t, store, TransportDynamicSource(tc.profile, tc.profile)).ResourcesJSON)
				if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 2; got != want {
					t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
				}
				if got, want := countResources(resources, api.NetAPIVersion, "BGPPeer"), 2; got != want {
					t.Fatalf("BGPPeer count = %d, want %d resources=%#v", got, want, resources)
				}
				if got := countResources(resources, api.NetAPIVersion, "WireGuardPeer"); got != 0 {
					t.Fatalf("transport-generated WireGuardPeer count = %d, want 0", got)
				}
				for peer, remote := range tc.remotes {
					tunnel := findTransportTunnelForPeer(t, resources, tc.profile, peer)
					if tunnel.Mode != tc.mode || tunnel.Remote != remote {
						t.Fatalf("%s tunnel = %#v, want mode %s remote %s", peer, tunnel, tc.mode, remote)
					}
					if tc.wantEncap && (tunnel.EncapSport != 5555 || tunnel.EncapDport != 5555) {
						t.Fatalf("%s tunnel encap ports = %d/%d, want 5555/5555", peer, tunnel.EncapSport, tunnel.EncapDport)
					}
					_, bgpPeer := findTransportBGPPeerResourceForPeer(t, resources, tc.profile, peer)
					if len(bgpPeer.Peers) != 1 || strings.TrimSpace(bgpPeer.Peers[0]) == "" {
						t.Fatalf("%s BGP peer = %#v, want one derived RR tunnel address", peer, bgpPeer)
					}
				}
			})
		}
	})
}

func seedSubmittedClaimsFromFixture(t *testing.T, store interface {
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
}, now time.Time, fixture string) {
	t.Helper()
	for _, resource := range claimSeedResources(t, fixture) {
		source := "SAMEnrollmentClaim/" + resource.Metadata.Name
		part := dynamicconfig.DynamicConfigPart{
			TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
			Metadata: api.ObjectMeta{
				Name: "submitted-" + resource.Metadata.Name,
			},
			Spec: dynamicconfig.DynamicConfigPartSpec{
				Source:     source,
				Generation: 1,
				ObservedAt: now.UTC(),
				ExpiresAt:  now.Add(5 * time.Minute).UTC(),
				Resources:  []api.Resource{resource},
			},
		}
		part.Spec.Digest = digestDynamicPart(part)
		record, err := codec.Encode(part)
		if err != nil {
			t.Fatalf("encode dynamic part for %s: %v", source, err)
		}
		if err := store.UpsertDynamicConfigPart(record); err != nil {
			t.Fatalf("UpsertDynamicConfigPart(%s): %v", source, err)
		}
	}
}

func effectiveWithAdmissionState(t *testing.T, router *api.Router, store interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
}, now time.Time) *api.Router {
	t.Helper()
	records, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatalf("ListDynamicConfigParts: %v", err)
	}
	parts := make([]dynamicconfig.DynamicConfigPart, 0, len(records))
	for _, record := range records {
		var resources []api.Resource
		if strings.TrimSpace(record.ResourcesJSON) != "" {
			if err := json.Unmarshal([]byte(record.ResourcesJSON), &resources); err != nil {
				t.Fatalf("decode %s resources: %v", record.Source, err)
			}
		}
		var directives []dynamicconfig.DynamicConfigDirective
		if strings.TrimSpace(record.DirectivesJSON) != "" {
			if err := json.Unmarshal([]byte(record.DirectivesJSON), &directives); err != nil {
				t.Fatalf("decode %s directives: %v", record.Source, err)
			}
		}
		parts = append(parts, dynamicconfig.DynamicConfigPart{
			TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
			Metadata: api.ObjectMeta{
				Name: fmt.Sprintf("%s-%d", record.Source, record.Generation),
			},
			Spec: dynamicconfig.DynamicConfigPartSpec{
				Source:     record.Source,
				Generation: record.Generation,
				ObservedAt: record.ObservedAt,
				ExpiresAt:  record.ExpiresAt,
				Digest:     record.Digest,
				Resources:  resources,
				Directives: directives,
			},
		})
	}
	policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*router)
	if err != nil {
		t.Fatalf("ExtractDynamicOverridePolicies: %v", err)
	}
	effective, _, err := dynamicconfig.BuildEffectiveConfigForOS(*router, parts, policies, now.UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	return &effective
}

// seedFetchedCloudEdgeRRSet models the only allowed leaf input: an RR snapshot
// projected by an enrollment server from the hub's canonical SAMNodeSet.
func seedFetchedCloudEdgeRRSet(t *testing.T, store interface {
	UpsertDynamicConfigPart(routerstate.DynamicConfigPartRecord) error
}, now time.Time, policyName string) {
	t.Helper()
	hub, err := config.Load(filepath.Join("..", "..", "..", "examples", "cloudedge-dynamic-rr-a-hub.yaml"))
	if err != nil {
		t.Fatalf("load RR hub: %v", err)
	}
	if err := config.ValidateForOS(hub, platform.OSLinux); err != nil {
		t.Fatalf("validate RR hub: %v", err)
	}
	var policy api.SAMEnrollmentPolicySpec
	policyFound := false
	for _, resource := range hub.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentPolicy" || resource.Metadata.Name != policyName {
			continue
		}
		policy, err = resource.SAMEnrollmentPolicySpec()
		if err != nil {
			t.Fatalf("SAMEnrollmentPolicy/%s spec: %v", policyName, err)
		}
		policyFound = true
		break
	}
	if !policyFound {
		t.Fatalf("SAMEnrollmentPolicy/%s not found in RR hub", policyName)
	}
	_, rrSetName, rrSetOK := strings.Cut(strings.TrimSpace(policy.RRSetRef), "/")
	_, rrNodeSetName, rrNodeSetOK := strings.Cut(strings.TrimSpace(policy.RRNodeSetRef), "/")
	if !rrSetOK || rrSetName == "" || !rrNodeSetOK || rrNodeSetName == "" {
		t.Fatalf("SAMEnrollmentPolicy/%s runtime topology refs = rrSet:%q rrNodeSet:%q", policyName, policy.RRSetRef, policy.RRNodeSetRef)
	}
	var nodeSet api.SAMNodeSetSpec
	found := false
	for _, resource := range hub.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMNodeSet" || resource.Metadata.Name != rrNodeSetName {
			continue
		}
		nodeSet, err = resource.SAMNodeSetSpec()
		if err != nil {
			t.Fatalf("SAMNodeSet/%s spec: %v", rrNodeSetName, err)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("SAMNodeSet/%s not found in RR hub", rrNodeSetName)
	}
	nodes := make([]api.SAMNodeSpec, 0, len(nodeSet.Nodes))
	for _, node := range nodeSet.Nodes {
		if node.RouteReflector {
			nodes = append(nodes, node)
		}
	}
	snapshot := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: rrSetName},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/" + policyName,
			Nodes:               nodes,
		},
	}
	record, err := codec.FetchedSAMEnrollmentTopologyRecord(snapshot, nil, now, now.Add(time.Hour), codec.FetchedSAMEnrollmentTopologyRecordOptions{
		Name:                              "fetched-" + rrSetName,
		Generation:                        1,
		DefaultTTL:                        DefaultLeaseTTL,
		IncludeEmptyDirectivesActionPlans: true,
		Digest:                            digestDynamicPart,
	})
	if err != nil {
		t.Fatalf("encode fetched RR snapshot: %v", err)
	}
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		t.Fatalf("persist fetched RR snapshot: %v", err)
	}
}

func claimSeedResources(t *testing.T, fixture string) []api.Resource {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", fixture))
	if err != nil {
		t.Fatalf("read claim seed %s: %v", fixture, err)
	}
	var seed api.Router
	if err := yaml.Unmarshal(data, &seed); err != nil {
		t.Fatalf("parse claim seed %s: %v", fixture, err)
	}
	return append([]api.Resource(nil), seed.Spec.Resources...)
}

func exampleSAMTransportProfile(t *testing.T, router *api.Router, name string) api.SAMTransportProfileSpec {
	t.Helper()
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMTransportProfile" || resource.Metadata.Name != name {
			continue
		}
		spec, err := resource.SAMTransportProfileSpec()
		if err != nil {
			t.Fatalf("SAMTransportProfile/%s spec: %v", name, err)
		}
		return spec
	}
	t.Fatalf("SAMTransportProfile/%s not found", name)
	return api.SAMTransportProfileSpec{}
}

func TestSAMTransportProfileCanGenerateTunnelWithoutBGPPeerForRRDynamicAdmission(t *testing.T) {
	now := time.Date(2026, 6, 28, 9, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("rr-a", "rr-a", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.10.0.2"
	generatePeers := false
	spec.BGP.GeneratePeers = &generatePeers
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMEnrollmentPolicy/cloudedge-leaves"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"},
			Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
			Spec: api.SAMEnrollmentPolicySpec{
				TransportProfileRef:   "SAMTransportProfile/rr-a",
				TunnelAddressPrefixes: []string{"10.255.0.0/20"},
				TTL:                   "1h",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
			Metadata: api.ObjectMeta{Name: "leaf-pve"},
			Spec: api.SAMEnrollmentClaimSpec{
				PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
				LeafID:        "leaf-pve",
				TunnelAddress: "10.255.0.21/32",
				Endpoint:      "10.20.0.21",
			},
		},
	)

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("rr-a", "rr-a")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 1; got != want {
		t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
	}
	if got := countResources(resources, api.NetAPIVersion, "BGPPeer"); got != 0 {
		t.Fatalf("BGPPeer count = %d, want 0 for RR dynamic admission", got)
	}
	tunnel := findTransportTunnelForPeer(t, resources, "rr-a", "leaf-pve")
	if tunnel.Remote != "10.20.0.21" {
		t.Fatalf("tunnel remote = %q, want leaf endpoint", tunnel.Remote)
	}
}

func TestSAMTransportProfileSkipsRevokedExpiredAndUnauthorizedEnrollmentClaims(t *testing.T) {
	now := time.Date(2026, 6, 28, 9, 5, 0, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithMode("rr-a", "rr-a", "pair-stable", nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.Encryption = "none"
	spec.LocalEndpoint = "10.10.0.2"
	generatePeers := false
	spec.BGP.GeneratePeers = &generatePeers
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMEnrollmentPolicy/cloudedge-leaves"}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentPolicy"},
			Metadata: api.ObjectMeta{Name: "cloudedge-leaves"},
			Spec: api.SAMEnrollmentPolicySpec{
				TransportProfileRef:   "SAMTransportProfile/rr-a",
				TunnelAddressPrefixes: []string{"10.255.0.0/20"},
				TTL:                   "1h",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
			Metadata: api.ObjectMeta{Name: "leaf-active"},
			Spec: api.SAMEnrollmentClaimSpec{
				PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
				LeafID:        "leaf-active",
				TunnelAddress: "10.255.0.21/32",
				Endpoint:      "10.20.0.21",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
			Metadata: api.ObjectMeta{Name: "leaf-revoked"},
			Spec: api.SAMEnrollmentClaimSpec{
				PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
				LeafID:        "leaf-revoked",
				TunnelAddress: "10.255.0.22/32",
				Endpoint:      "10.20.0.22",
				Revoked:       true,
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
			Metadata: api.ObjectMeta{Name: "leaf-expired"},
			Spec: api.SAMEnrollmentClaimSpec{
				PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
				LeafID:        "leaf-expired",
				TunnelAddress: "10.255.0.23/32",
				Endpoint:      "10.20.0.23",
				ExpiresAt:     now.Add(-time.Minute).Format(time.RFC3339),
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
			Metadata: api.ObjectMeta{Name: "leaf-unauthorized"},
			Spec: api.SAMEnrollmentClaimSpec{
				PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
				LeafID:        "leaf-unauthorized",
				TunnelAddress: "10.244.0.24/32",
				Endpoint:      "10.20.0.24",
			},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMEnrollmentClaim"},
			Metadata: api.ObjectMeta{Name: "leaf-ttl-expired"},
			Spec: api.SAMEnrollmentClaimSpec{
				PolicyRef:     "SAMEnrollmentPolicy/cloudedge-leaves",
				LeafID:        "leaf-ttl-expired",
				TunnelAddress: "10.255.0.25/32",
				Endpoint:      "10.20.0.25",
				JoinTimestamp: now.Add(-2 * time.Hour).Format(time.RFC3339),
			},
		},
	)

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("rr-a", "rr-a")).ResourcesJSON)
	if got, want := countResources(resources, api.HybridAPIVersion, "TunnelInterface"), 1; got != want {
		t.Fatalf("TunnelInterface count = %d, want %d resources=%#v", got, want, resources)
	}
	tunnel := findTransportTunnelForPeer(t, resources, "rr-a", "leaf-active")
	if tunnel.Remote != "10.20.0.21" {
		t.Fatalf("active tunnel remote = %q, want leaf endpoint", tunnel.Remote)
	}
	for _, skipped := range []string{"leaf-revoked", "leaf-expired", "leaf-unauthorized", "leaf-ttl-expired"} {
		if hasTransportResourceForPeer(resources, skipped) {
			t.Fatalf("%s must not be materialized: %#v", skipped, resources)
		}
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "rr-a")
	if status["phase"] != "Derived" {
		t.Fatalf("transport status phase = %v, want Derived: %+v", status["phase"], status)
	}
	peersFrom := status["peersFrom"].([]any)
	if len(peersFrom) != 1 {
		t.Fatalf("peersFrom status = %#v, want one source", peersFrom)
	}
	sourceStatus := peersFrom[0].(map[string]any)
	if sourceStatus["peerCount"] != float64(1) || sourceStatus["reason"] != "4 enrollment claims skipped" {
		t.Fatalf("peersFrom status = %#v, want one accepted and four skipped", peersFrom)
	}
}

func TestSAMTransportProfileSAMNodeSetSelectionPreservesHubSpoke(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 5, 30, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouterWithTopology("svnet1", "leaf-rt01", nil, nil)
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{
		Resource: "SAMNodeSet/svnet1-nodes",
		NodeRefs: []string{"rr-rt01"},
	}}
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, samNodeSetResource("svnet1-nodes", []api.SAMNodeSpec{
		{NodeRef: "leaf-rt01", SAMEndpoint: "203.0.113.10"},
		{NodeRef: "rr-rt01", SAMEndpoint: "203.0.113.11"},
		{NodeRef: "rr-rt02", SAMEndpoint: "203.0.113.12"},
	}))

	controller := TransportController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("svnet1", "leaf-rt01")).ResourcesJSON)
	if got, want := len(resources), 3; got != want {
		t.Fatalf("resources = %d, want %d", got, want)
	}
	tunnel := findTransportTunnelForPeer(t, resources, "leaf-rt01", "rr-rt01")
	if tunnel.Remote != "203.0.113.11" {
		t.Fatalf("tunnel remote = %q, want selected NodeSet endpoint", tunnel.Remote)
	}
	if hasTransportResourceForPeer(resources, "rr-rt02") {
		t.Fatalf("unselected NodeSet node received transport resources: %#v", resources)
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
	if len(group.Nodes) != 1 || group.Nodes[0].NodeRef != "rr-rt01" || group.Nodes[0].SAMEndpoint != "10.252.0.1" {
		t.Fatalf("published nodes = %#v, want concrete rr endpoint", group.Nodes)
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

func TestSAMTransportProfileRouteReflectorClientImportAdmission(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 7, 30, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouter("rr", "rr-a", []api.SAMTransportPeerSpec{
		{NodeRef: "leaf-a", RemoteEndpoint: "203.0.113.21"},
		{NodeRef: "leaf-b", RemoteEndpoint: "203.0.113.22"},
	})
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.BGP.RouteReflectorClient = true
	spec.BGP.ImportPolicy = api.BGPImportPolicySpec{AllowedPrefixes: []string{"10.77.60.0/24"}}
	router.Spec.Resources[0].Spec = spec

	controller := TransportController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("rr", "rr-a")).ResourcesJSON)
	peer := findTransportBGPPeerForPeer(t, resources, "rr-a", "leaf-a")
	if peer.ImportPolicy.AllowedPrefixLengthMin != 32 || peer.ImportPolicy.AllowedPrefixLengthMax != 32 {
		t.Fatalf("import policy = %#v, want /32 exact admission", peer.ImportPolicy)
	}
	if got, want := peer.ImportPolicy.AllowedPrefixes, []string{"10.77.60.0/24"}; !sameStringSetForTest(got, want) {
		t.Fatalf("allowed prefixes = %#v, want explicit %#v", got, want)
	}
	if got, want := peer.ImportPolicy.RequiredCommunities, []string{bgpstate.MobilityNodeIdentityCommunity("leaf-a")}; !sameStringSetForTest(got, want) {
		t.Fatalf("required communities = %#v, want %#v", got, want)
	}
	forbidden := []string{
		bgpstate.MobilityNodeIdentityCommunity("rr-a"),
		bgpstate.MobilityNodeIdentityCommunity("leaf-b"),
	}
	if !sameStringSetForTest(peer.ImportPolicy.ForbiddenCommunities, forbidden) {
		t.Fatalf("forbidden communities = %#v, want %#v", peer.ImportPolicy.ForbiddenCommunities, forbidden)
	}
}

func TestSAMTransportProfileRouteReflectorClientDefaultsImportPrefixesFromMobilityPools(t *testing.T) {
	now := time.Date(2026, 6, 6, 9, 7, 45, 0, time.UTC)
	store := testStore(t, now)
	router := transportRouter("rr", "rr-a", []api.SAMTransportPeerSpec{
		{NodeRef: "leaf-a", RemoteEndpoint: "203.0.113.21"},
	})
	spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
	if err != nil {
		t.Fatalf("SAMTransportProfile spec: %v", err)
	}
	spec.BGP.RouteReflectorClient = true
	router.Spec.Resources[0].Spec = spec
	router.Spec.Resources = append(router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
		Metadata: api.ObjectMeta{Name: "cloudedge"},
		Spec:     api.MobilityPoolSpec{Prefix: "10.88.60.0/24"},
	})

	controller := TransportController{
		Router: router,
		Store:  store,
		Now:    func() time.Time { return now },
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, TransportDynamicSource("rr", "rr-a")).ResourcesJSON)
	peer := findTransportBGPPeerForPeer(t, resources, "rr-a", "leaf-a")
	if got, want := peer.ImportPolicy.AllowedPrefixes, []string{"10.88.60.0/24"}; !sameStringSetForTest(got, want) {
		t.Fatalf("allowed prefixes = %#v, want MobilityPool prefixes %#v", got, want)
	}
	if peer.ImportPolicy.AllowedPrefixLengthMin != 32 || peer.ImportPolicy.AllowedPrefixLengthMax != 32 {
		t.Fatalf("import policy = %#v, want MobilityPool prefix constrained to /32 routes", peer.ImportPolicy)
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

func samRRSetResource(name string, nodes []api.SAMNodeSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.SAMRRSetSpec{
			EnrollmentPolicyRef: "SAMEnrollmentPolicy/cloudedge-leaves",
			Nodes:               nodes,
		},
	}
}

func countResources(resources []api.Resource, apiVersion, kind string) int {
	count := 0
	for _, resource := range resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind {
			count++
		}
	}
	return count
}

func hasTransportResourceForPeer(resources []api.Resource, peer string) bool {
	for _, resource := range resources {
		for _, owner := range resource.Metadata.OwnerRefs {
			if owner.APIVersion == api.MobilityAPIVersion && owner.Kind == "SAMTransportProfile" {
				if resource.Metadata.Annotations["routerd.net/sam-peer"] == peer {
					return true
				}
			}
		}
	}
	return false
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
	if len(topology) == 0 {
		topology = []string{self}
		for _, peer := range peers {
			topology = append(topology, peer.NodeRef)
		}
	}
	peerByNode := map[string]api.SAMTransportPeerSpec{}
	selected := make([]string, 0, len(peers))
	for _, peer := range peers {
		nodeRef := strings.TrimSpace(peer.NodeRef)
		peerByNode[nodeRef] = peer
		selected = append(selected, nodeRef)
	}
	nodes := make([]api.SAMNodeSpec, 0, len(topology))
	for _, nodeRef := range topology {
		nodeRef = strings.TrimSpace(nodeRef)
		node := api.SAMNodeSpec{NodeRef: nodeRef}
		if peer, ok := peerByNode[nodeRef]; ok {
			node.SAMEndpoint = peer.RemoteEndpoint
			node.SAMEndpointFrom = peer.RemoteEndpointFrom
		}
		nodes = append(nodes, node)
	}
	nodeSetName := profile + "-test-nodes"
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"},
				Metadata: api.ObjectMeta{Name: profile},
				Spec: api.SAMTransportProfileSpec{
					SelfNodeRef:       self,
					Mode:              "ipip",
					InnerPrefix:       "10.255.1.0/24",
					UnderlayInterface: "wan",
					LocalEndpoint:     "198.51.100.10",
					BGP: api.SAMTransportBGPProfileSpec{
						RouterRef: "BGPRouter/mobility",
						PeerASN:   64512,
					},
					PeersFrom: []api.SAMTransportPeersSourceSpec{{
						Resource: "SAMNodeSet/" + nodeSetName,
						NodeRefs: selected,
					}},
				},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
				Metadata: api.ObjectMeta{Name: nodeSetName},
				Spec:     api.SAMNodeSetSpec{Nodes: nodes},
			},
		}},
	}
}

func samPeerGroupResource(name string, peers []api.SAMTransportPeerSpec) api.Resource {
	nodes := make([]api.SAMNodeSpec, 0, len(peers))
	for _, peer := range peers {
		nodes = append(nodes, api.SAMNodeSpec{
			NodeRef:         peer.NodeRef,
			SAMEndpoint:     peer.RemoteEndpoint,
			SAMEndpointFrom: peer.RemoteEndpointFrom,
		})
	}
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.SAMPeerGroupSpec{Nodes: nodes},
	}
}

func samDirectPeerGroupResource(name, policyRef, fingerprint string, nodes []api.SAMNodeSpec) api.Resource {
	ownedPrefixesByNode := make(map[string][]string, len(nodes))
	for i, node := range nodes {
		// Direct peer groups are runtime-only enrollment objects. Give ordinary
		// unit fixtures a distinct signed /32 per node so tests exercise the
		// same narrow admission boundary as production topology snapshots.
		ownedPrefixesByNode[node.NodeRef] = []string{fmt.Sprintf("10.77.60.%d/32", 200+i)}
	}
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMPeerGroup"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.SAMPeerGroupSpec{
			EnrollmentPolicyRef:  policyRef,
			TransportFingerprint: fingerprint,
			Nodes:                nodes,
			OwnedPrefixesByNode:  ownedPrefixesByNode,
		},
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
	_, spec := findTransportTunnelResource(t, resources)
	return spec
}

func findTransportTunnelResource(t *testing.T, resources []api.Resource) (string, api.TunnelInterfaceSpec) {
	t.Helper()
	for _, resource := range resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		spec, err := resource.TunnelInterfaceSpec()
		if err != nil {
			t.Fatalf("TunnelInterface spec: %v", err)
		}
		return resource.Metadata.Name, spec
	}
	t.Fatalf("TunnelInterface not found in %#v", resources)
	return "", api.TunnelInterfaceSpec{}
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
