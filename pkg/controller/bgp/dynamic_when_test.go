// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpserver "github.com/osrg/gobgp/v4/pkg/server"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDynamicPeerWhenFalseWithdrawsActualNativeAcceptor(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.BGPRouterSpec{ASN: 64512, RouterID: "192.0.2.1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"}, Metadata: api.ObjectMeta{Name: "clients"}, Spec: api.BGPDynamicPeerSpec{
			RouterRef: "BGPRouter/lan", PeerASN: 64513, Listen: api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"192.0.2.0/24"}},
			When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.role": {Equals: "master"}}},
		}},
	}}}
	server := &testGoBGPServer{BgpServer: gobgpserver.NewBgpServer()}
	go server.Serve()
	t.Cleanup(server.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: &gobgpapi.Global{Asn: 64512, RouterId: "192.0.2.1", ListenPort: -1}}); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"backup", "master", "backup"} {
		if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "gateway", map[string]any{"role": role}); err != nil {
			t.Fatal(err)
		}
		effective, err := resourcequery.FilterRouterByWhen(declared, store)
		if err != nil {
			t.Fatal(err)
		}
		controller := &Controller{Router: effective, Store: store, Server: server}
		desired, err := controller.desiredDynamicPeers("lan", 64512)
		if err != nil {
			t.Fatal(err)
		}
		if err := controller.reconcileDynamicPeers(ctx, desired); err != nil {
			t.Fatal(err)
		}
		want := 0
		if role == "master" {
			want = 1
		}
		groups, neighbors := 0, 0
		if err := server.ListPeerGroup(ctx, &gobgpapi.ListPeerGroupRequest{}, func(*gobgpapi.PeerGroup) { groups++ }); err != nil {
			t.Fatal(err)
		}
		if err := server.ListDynamicNeighbor(ctx, &gobgpapi.ListDynamicNeighborRequest{}, func(*gobgpapi.DynamicNeighbor) { neighbors++ }); err != nil {
			t.Fatal(err)
		}
		if len(desired) != want || groups != want || neighbors != want {
			t.Fatalf("role=%s: desired=%d native groups=%d native acceptors=%d want=%d", role, len(desired), groups, neighbors, want)
		}
	}
}
