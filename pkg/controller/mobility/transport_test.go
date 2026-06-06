// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

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
