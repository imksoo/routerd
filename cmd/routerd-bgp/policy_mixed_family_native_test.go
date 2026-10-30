// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net"
	"net/netip"
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

func mixedFamilyPolicyServer(t *testing.T, asn uint32, routerID string, port int) *localPathServer {
	t.Helper()
	server := gobgpserver.NewBgpServer()
	go server.Serve()
	t.Cleanup(server.Stop)
	if err := server.StartBgp(t.Context(), &gobgpapi.StartBgpRequest{Global: &gobgpapi.Global{
		Asn: asn, RouterId: routerID, ListenPort: int32(port), ListenAddresses: []string{"127.0.0.1"}, Families: []uint32{0, 1},
	}}); err != nil {
		t.Fatal(err)
	}
	return &localPathServer{BgpServer: server}
}

func installMixedFamilyPolicy(t *testing.T, server *localPathServer, scope bgpdaemon.PolicyScope) {
	t.Helper()
	intent := bgpdaemon.PolicyIntent{RouterName: "mixed-family", Scopes: []bgpdaemon.PolicyScope{scope}}
	if err := bgpdaemon.ValidatePolicyIntent(intent); err != nil {
		t.Fatal(err)
	}
	plan := bgpdaemon.BuildPolicyPlan(intent, nil)
	if err := server.SetPolicies(t.Context(), plan.SetPolicies); err != nil {
		t.Fatalf("native GoBGP rejected generated mixed-family policy: %v", err)
	}
	for _, assignment := range []*gobgpapi.PolicyAssignment{plan.GlobalImportAssignment, plan.GlobalExportAssignment} {
		if err := server.SetPolicyAssignment(t.Context(), &gobgpapi.SetPolicyAssignmentRequest{Assignment: assignment}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMixedFamilyPoliciesUseNativeGoBGP(t *testing.T) {
	t.Run("import", func(t *testing.T) {
		server := mixedFamilyPolicyServer(t, 64512, "192.0.2.1", -1)
		installMixedFamilyPolicy(t, server, bgpdaemon.PolicyScope{
			Name: "mixed-import", Neighbors: []string{"192.0.2.2/32"}, ExportPolicyName: "mixed-import-export",
			Spec: api.BGPImportPolicySpec{
				AllowedPrefixes:     []string{"10.77.0.0/16", "2001:db8:77::/48"},
				RequiredCommunities: []string{"64512:10", "64512:11"}, ForbiddenCommunities: []string{"64512:666"},
				LocalPreference: 220, NextHopRewrite: "unchanged",
			},
		})
		required := []string{"64512:10", "64512:11"}
		for _, row := range []struct {
			prefix, neighbor string
			communities      []string
		}{
			{"10.77.1.1/32", "192.0.2.2", required},
			{"2001:db8:77::1/128", "192.0.2.2", required},
			{"10.88.1.1/32", "192.0.2.2", required},
			{"2001:db8:88::1/128", "192.0.2.2", required},
			{"10.77.2.1/32", "192.0.2.2", []string{"64512:10"}},
			{"2001:db8:77::2/128", "192.0.2.2", []string{"64512:10"}},
			{"10.77.3.1/32", "192.0.2.2", []string{"64512:10", "64512:11", "64512:666"}},
			{"2001:db8:77::3/128", "192.0.2.2", []string{"64512:10", "64512:11", "64512:666"}},
			{"10.77.4.1/32", "198.51.100.2", required},
			{"2001:db8:77::4/128", "198.51.100.2", required},
		} {
			nextHop := "192.0.2.9"
			if netip.MustParsePrefix(row.prefix).Addr().Is6() {
				nextHop = "2001:db8::9"
			}
			path, err := bgpdaemon.PathFromAppliedPath(bgpdaemon.AppliedPath{Source: bgpdaemon.AppliedPathSourceStatic, Prefix: row.prefix, Attrs: bgpdaemon.AppliedPathAttrs{NextHop: nextHop, Communities: row.communities}})
			if err != nil {
				t.Fatal(err)
			}
			// Exercise GoBGP's actual peer-origin import path, not a policy mock.
			path.SourceAsn, path.SourceId, path.NeighborIp = 64513, "192.0.2.2", row.neighbor
			if _, err := server.AddPath(t.Context(), &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: path}); err != nil {
				t.Fatal(err)
			}
		}
		var selected []string
		for _, family := range []gobgp.Family{gobgp.RF_IPv4_UC, gobgp.RF_IPv6_UC} {
			if err := server.BgpServer.ListPath(gobgpapiutil.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: family}, func(prefix gobgp.NLRI, paths []*gobgpapiutil.Path) {
				selected = append(selected, prefix.String())
				for _, path := range paths {
					var preference uint32
					var nextHop string
					for _, attribute := range path.Attrs {
						switch attribute := attribute.(type) {
						case *gobgp.PathAttributeLocalPref:
							preference = attribute.Value
						case *gobgp.PathAttributeNextHop:
							nextHop = attribute.Value.String()
						case *gobgp.PathAttributeMpReachNLRI:
							nextHop = attribute.Nexthop.String()
						}
					}
					wantNextHop := "192.0.2.9"
					if family == gobgp.RF_IPv6_UC {
						wantNextHop = "2001:db8::9"
					}
					if preference != 220 || nextHop != wantNextHop {
						t.Errorf("import actions changed for %s: local-pref=%d next-hop=%s", prefix, preference, nextHop)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
		}
		slices.Sort(selected)
		if !slices.Equal(selected, []string{"10.77.1.1/32", "2001:db8:77::1/128"}) {
			t.Fatalf("native import selection=%v", selected)
		}
	})

	t.Run("export", func(t *testing.T) {
		server := mixedFamilyPolicyServer(t, 64512, "192.0.2.1", -1)
		installMixedFamilyPolicy(t, server, bgpdaemon.PolicyScope{
			Name: "mixed-export", Neighbors: []string{"127.0.0.1/32"}, ExportPolicyName: "mixed-export-policy",
			ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.99.0.0/16", "2001:db8:99::/48"}},
		})
		reservation, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := reservation.Addr().(*net.TCPAddr).Port
		if err := reservation.Close(); err != nil {
			t.Fatal(err)
		}
		remote := mixedFamilyPolicyServer(t, 64513, "192.0.2.2", port)
		peer := func(asn uint32, passive bool) *gobgpapi.Peer {
			return &gobgpapi.Peer{
				Conf:      &gobgpapi.PeerConf{NeighborAddress: "127.0.0.1", PeerAsn: asn},
				Transport: &gobgpapi.Transport{LocalAddress: "127.0.0.1", PassiveMode: passive, RemotePort: uint32(port)},
				AfiSafis: []*gobgpapi.AfiSafi{
					{Config: &gobgpapi.AfiSafiConfig{Family: &gobgpapi.Family{Afi: gobgpapi.Family_AFI_IP, Safi: gobgpapi.Family_SAFI_UNICAST}, Enabled: true}},
					{Config: &gobgpapi.AfiSafiConfig{Family: &gobgpapi.Family{Afi: gobgpapi.Family_AFI_IP6, Safi: gobgpapi.Family_SAFI_UNICAST}, Enabled: true}},
				},
				Timers: &gobgpapi.Timers{Config: &gobgpapi.TimersConfig{ConnectRetry: 1, HoldTime: 9, KeepaliveInterval: 3, IdleHoldTimeAfterReset: 1}},
			}
		}
		if err := remote.AddPeer(t.Context(), &gobgpapi.AddPeerRequest{Peer: peer(64512, true)}); err != nil {
			t.Fatal(err)
		}
		if err := server.AddPeer(t.Context(), &gobgpapi.AddPeerRequest{Peer: peer(64513, false)}); err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{"10.99.1.1/32", "10.100.1.1/32", "2001:db8:99::1/128", "2001:db8:100::1/128"} {
			nextHop := "192.0.2.1"
			if netip.MustParsePrefix(prefix).Addr().Is6() {
				nextHop = "2001:db8::1"
			}
			path, err := bgpdaemon.PathFromAppliedPath(bgpdaemon.AppliedPath{Source: bgpdaemon.AppliedPathSourceStatic, Prefix: prefix, Attrs: bgpdaemon.AppliedPathAttrs{NextHop: nextHop}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := server.AddPath(t.Context(), &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: path}); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		for {
			var received []string
			for _, family := range []gobgp.Family{gobgp.RF_IPv4_UC, gobgp.RF_IPv6_UC} {
				if err := remote.BgpServer.ListPath(gobgpapiutil.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: family}, func(prefix gobgp.NLRI, _ []*gobgpapiutil.Path) { received = append(received, prefix.String()) }); err != nil {
					t.Fatal(err)
				}
			}
			slices.Sort(received)
			if slices.Equal(received, []string{"10.99.1.1/32", "2001:db8:99::1/128"}) {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("native remote export selection=%v: %v", received, ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	})
}
