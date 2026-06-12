// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestServeInspectionResourcesIncludesDerivedRuntimePackage(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
		Metadata: api.ObjectMeta{Name: "wg-sam"},
		Spec:     api.WireGuardInterfaceSpec{},
	}}}}

	resources, err := serveInspectionResources(router, nil, "Package/router-runtime", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("resources len = %d, want 1", len(resources))
	}
	if resources[0].APIVersion != api.SystemAPIVersion || resources[0].Kind != "Package" || resources[0].Name != "router-runtime" {
		t.Fatalf("resource = %#v, want system Package/router-runtime", resources[0])
	}
	spec, ok := resources[0].Spec.(api.PackageSpec)
	if !ok {
		t.Fatalf("spec type = %T, want api.PackageSpec", resources[0].Spec)
	}
	ubuntu := api.OSPackageSetSpec{}
	for _, set := range spec.Packages {
		if set.OS == "ubuntu" {
			ubuntu = set
		}
	}
	if !stringInSlice(ubuntu.Names, "wireguard-tools") {
		t.Fatalf("ubuntu package names = %#v, want wireguard-tools", ubuntu.Names)
	}
}

func TestServeInspectionResourcesIncludesDynamicEffectiveResources(t *testing.T) {
	now := time.Now().UTC()
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test-router"},
		Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"},
			Metadata: api.ObjectMeta{Name: "sam"},
			Spec:     api.BGPRouterSpec{ASN: 64512, RouterID: "10.255.0.1"},
		}}}}
	dynamicResources := []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "sam-core-a"},
			Spec: api.TunnelInterfaceSpec{
				Mode:              "ipip",
				Local:             "10.252.0.1",
				Remote:            "10.252.0.2",
				Address:           "10.255.1.0/31",
				UnderlayInterface: "wg-hybrid",
				TrustedUnderlay:   true,
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
			Metadata: api.ObjectMeta{Name: "sam-core-a"},
			Spec: api.BGPPeerSpec{
				RouterRef: "BGPRouter/sam",
				PeerASN:   64512,
				Peers:     []string{"10.255.1.1"},
			},
		},
	}
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()
	if err := store.UpsertDynamicConfigPart(dynamicConfigPartRecordForCleanupTest(t, "SAMTransportProfile/fabric/node/leaf", dynamicResources, now.Add(time.Hour))); err != nil {
		t.Fatalf("upsert dynamic config part: %v", err)
	}
	if err := store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "sam-core-a", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save tunnel status: %v", err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPPeer", "sam-core-a", map[string]any{"phase": "Established"}); err != nil {
		t.Fatalf("save peer status: %v", err)
	}

	tunnels, err := serveInspectionResources(router, store, "TunnelInterface/sam-core-a", 0)
	if err != nil {
		t.Fatalf("inspect dynamic TunnelInterface: %v", err)
	}
	if len(tunnels) != 1 || tunnels[0].APIVersion != api.HybridAPIVersion || tunnels[0].Kind != "TunnelInterface" || tunnels[0].Name != "sam-core-a" {
		t.Fatalf("dynamic tunnel view = %#v", tunnels)
	}
	if got := tunnels[0].Status["phase"]; got != "Applied" {
		t.Fatalf("dynamic tunnel status phase = %v, want Applied", got)
	}

	peers, err := serveInspectionResources(router, store, "BGPPeer/sam-core-a", 0)
	if err != nil {
		t.Fatalf("inspect dynamic BGPPeer: %v", err)
	}
	if len(peers) != 1 || peers[0].APIVersion != api.NetAPIVersion || peers[0].Kind != "BGPPeer" || peers[0].Name != "sam-core-a" {
		t.Fatalf("dynamic peer view = %#v", peers)
	}
	if got := peers[0].Status["phase"]; got != "Established" {
		t.Fatalf("dynamic peer status phase = %v, want Established", got)
	}
}

func stringInSlice(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
