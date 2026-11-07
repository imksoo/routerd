// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpapiutil "github.com/osrg/gobgp/v4/pkg/apiutil"
	gobgp "github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgpserver "github.com/osrg/gobgp/v4/pkg/server"
)

func TestGlobalExportPolicyAdvertisesAndWithdrawsOverRealBGPSession(t *testing.T) {
	central := realDynamicPathServer(t)
	statePath, applied := dynamicJournal(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := reservation.Addr().(*net.TCPAddr).Port
	if err := reservation.Close(); err != nil {
		t.Fatal(err)
	}
	remote := gobgpserver.NewBgpServer()
	go remote.Serve()
	t.Cleanup(remote.Stop)
	if err := remote.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: &gobgpapi.Global{Asn: 64513, RouterId: "192.0.2.2", ListenPort: int32(port), ListenAddresses: []string{"127.0.0.1"}, Families: []uint32{0}}}); err != nil {
		t.Fatal(err)
	}
	intent := bgpdaemon.PolicyIntent{RouterName: "export", Scopes: []bgpdaemon.PolicyScope{{Name: "routerd-export-peer", Neighbors: []string{"127.0.0.1/32"}, ExportPolicyName: "routerd-export-allow", ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.77.60.11/32"}}}}}
	if _, err := reconcilePolicyConfig(ctx, central, statePath, applied, intent); err != nil {
		t.Fatal(err)
	}
	peer := func(asn uint32, passive bool) *gobgpapi.Peer {
		return &gobgpapi.Peer{Conf: &gobgpapi.PeerConf{NeighborAddress: "127.0.0.1", PeerAsn: asn}, Transport: &gobgpapi.Transport{LocalAddress: "127.0.0.1", PassiveMode: passive, RemotePort: uint32(port)}, AfiSafis: []*gobgpapi.AfiSafi{{Config: &gobgpapi.AfiSafiConfig{Family: ipv4Family(), Enabled: true}}}, Timers: &gobgpapi.Timers{Config: &gobgpapi.TimersConfig{ConnectRetry: 1, HoldTime: 9, KeepaliveInterval: 3, IdleHoldTimeAfterReset: 1}}}
	}
	if err := remote.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: peer(64512, true)}); err != nil {
		t.Fatal(err)
	}
	if err := central.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: peer(64513, false)}); err != nil {
		t.Fatal(err)
	}
	desired := []bgpdaemon.AppliedPath{bgpdaemon.StaticAppliedPath("10.77.60.11/32", nil), bgpdaemon.StaticAppliedPath("10.77.60.12/32", nil)}
	if _, err := reconcileStaticPaths(ctx, central, statePath, desired); err != nil {
		t.Fatal(err)
	}
	waitRemotePrefixes := func(want []string) {
		t.Helper()
		for {
			var got []string
			if err := remote.ListPath(gobgpapiutil.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: gobgp.RF_IPv4_UC}, func(nlri gobgp.NLRI, _ []*gobgpapiutil.Path) { got = append(got, nlri.String()) }); err != nil {
				t.Fatal(err)
			}
			slices.Sort(got)
			if slices.Equal(got, want) {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("remote prefixes=%v want=%v: %v", got, want, ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	waitRemotePrefixes([]string{"10.77.60.11/32"})
	intent.Scopes[0].ExportPolicy.AllowedPrefixes = []string{"10.77.60.12/32"}
	applied, _, err = bgpdaemon.ReadApplied(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconcilePolicyConfig(ctx, central, statePath, applied, intent); err != nil {
		t.Fatal(err)
	}
	waitRemotePrefixes([]string{"10.77.60.12/32"})
	applied, _, err = bgpdaemon.ReadApplied(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconcilePolicyConfig(ctx, central, statePath, applied, bgpdaemon.PolicyIntent{RouterName: "export"}); err != nil {
		t.Fatal(err)
	}
	waitRemotePrefixes(nil)
}
