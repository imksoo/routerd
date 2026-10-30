// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpapiutil "github.com/osrg/gobgp/v4/pkg/apiutil"
	gobgp "github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgpserver "github.com/osrg/gobgp/v4/pkg/server"
)

// A raw peer inspects UPDATE bytes independently of GoBGP's remote RIB/API.
func TestCommunitySelectionOnBGPUpdate(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		for _, mode := range []struct {
			name               string
			mask               uint32
			standard, extended bool
		}{{"standard", 1, true, false}, {"extended", 2, false, true}, {"both", 3, true, true}} {
			name := mode.name
			if dynamic {
				name = "dynamic-" + name
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				reservation, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				port := reservation.Addr().(*net.TCPAddr).Port
				if err := reservation.Close(); err != nil {
					t.Fatal(err)
				}
				server := gobgpserver.NewBgpServer()
				go server.Serve()
				t.Cleanup(server.Stop)
				global := bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1", ListenPort: port, ListenAddresses: []string{"127.0.0.1"}}
				if err := server.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: appliedGlobal(global)}); err != nil {
					t.Fatal(err)
				}
				state, applied := dynamicJournal(t)
				intent := bgpdaemon.PolicyIntent{RouterName: "community", Scopes: []bgpdaemon.PolicyScope{{Name: "community", Neighbors: []string{"127.0.0.2/32"}, ExportPolicyName: "community-export", ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{"10.77.60.11/32"}}}}}
				if _, err := reconcilePolicyConfig(ctx, &localPathServer{BgpServer: server}, state, applied, intent); err != nil {
					t.Fatal(err)
				}
				peer := appliedPeer(bgpdaemon.AppliedPeer{Address: "127.0.0.2", ASN: 64513, PassiveMode: true, SendCommunity: mode.mask}, global)
				peer.AfiSafis = peer.AfiSafis[:1]
				if dynamic {
					group := &gobgpapi.PeerGroup{Conf: &gobgpapi.PeerGroupConf{PeerGroupName: "clients", PeerAsn: 64513, SendCommunity: mode.mask}, AfiSafis: peer.AfiSafis}
					if err := server.AddPeerGroup(ctx, &gobgpapi.AddPeerGroupRequest{PeerGroup: group}); err != nil {
						t.Fatal(err)
					}
					if err := server.AddDynamicNeighbor(ctx, &gobgpapi.AddDynamicNeighborRequest{DynamicNeighbor: &gobgpapi.DynamicNeighbor{Prefix: "127.0.0.2/32", PeerGroup: "clients"}}); err != nil {
						t.Fatal(err)
					}
					var observed uint32
					if err := server.ListPeerGroup(ctx, &gobgpapi.ListPeerGroupRequest{}, func(g *gobgpapi.PeerGroup) { observed = g.GetConf().GetSendCommunity() }); err != nil {
						t.Fatal(err)
					}
					if observed != mode.mask {
						t.Fatalf("group observed send=%d want=%d", observed, mode.mask)
					}
				} else if err := server.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: peer}); err != nil {
					t.Fatal(err)
				}
				dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}
				conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { conn.Close() })
				if err := conn.SetDeadline(time.Now().Add(7 * time.Second)); err != nil {
					t.Fatal(err)
				}
				read := func() *gobgp.BGPMessage {
					t.Helper()
					header := make([]byte, gobgp.BGP_HEADER_LENGTH)
					if _, err := io.ReadFull(conn, header); err != nil {
						t.Fatal(err)
					}
					var h gobgp.BGPHeader
					if err := h.DecodeFromBytes(header); err != nil {
						t.Fatal(err)
					}
					body := make([]byte, int(h.Len)-len(header))
					if _, err := io.ReadFull(conn, body); err != nil {
						t.Fatal(err)
					}
					m, err := gobgp.ParseBGPMessage(append(header, body...))
					if err != nil {
						t.Fatal(err)
					}
					return m
				}
				write := func(m *gobgp.BGPMessage) {
					t.Helper()
					data, err := m.Serialize()
					if err != nil {
						t.Fatal(err)
					}
					if _, err := conn.Write(data); err != nil {
						t.Fatal(err)
					}
				}
				if msg := read(); msg.Header.Type != gobgp.BGP_MSG_OPEN {
					t.Fatalf("first message=%d", msg.Header.Type)
				}
				open, err := gobgp.NewBGPOpenMessage(64513, 30, netip.MustParseAddr("192.0.2.2"), []gobgp.OptionParameterInterface{gobgp.NewOptionParameterCapability([]gobgp.ParameterCapabilityInterface{gobgp.NewCapFourOctetASNumber(64513)})})
				if err != nil {
					t.Fatal(err)
				}
				write(open)
				write(gobgp.NewBGPKeepAliveMessage())
				if msg := read(); msg.Header.Type != gobgp.BGP_MSG_KEEPALIVE {
					t.Fatalf("handshake message=%d body=%v", msg.Header.Type, msg.Body)
				}
				for {
					established := false
					if err := server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(p *gobgpapi.Peer) {
						established = p.GetState().GetSessionState() == gobgpapi.PeerState_SESSION_STATE_ESTABLISHED
					}); err != nil {
						t.Fatal(err)
					}
					if established {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
				path, err := bgpdaemon.PathFromAppliedPath(bgpdaemon.AppliedPath{Source: bgpdaemon.AppliedPathSourceStatic, Prefix: "10.77.60.11/32", Attrs: bgpdaemon.AppliedPathAttrs{Communities: []string{"64512:7"}}})
				if err != nil {
					t.Fatal(err)
				}
				ip6, err := gobgp.NewIPv6AddressSpecificExtended(gobgp.EC_SUBTYPE_ROUTE_TARGET, netip.MustParseAddr("2001:db8::1"), 7, true)
				if err != nil {
					t.Fatal(err)
				}
				extra, err := gobgpapiutil.MarshalPathAttributes([]gobgp.PathAttributeInterface{gobgp.NewPathAttributeExtendedCommunities([]gobgp.ExtendedCommunityInterface{gobgp.NewTwoOctetAsSpecificExtended(gobgp.EC_SUBTYPE_ROUTE_TARGET, 64512, 7, true)}), gobgp.NewPathAttributeIP6ExtendedCommunities([]gobgp.ExtendedCommunityInterface{ip6}), gobgp.NewPathAttributeLargeCommunities([]*gobgp.LargeCommunity{gobgp.NewLargeCommunity(64512, 7, 8)})})
				if err != nil {
					t.Fatal(err)
				}
				path.Pattrs = append(path.Pattrs, extra...)
				if _, err := (&localPathServer{BgpServer: server}).AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: path}); err != nil {
					t.Fatal(err)
				}
				for {
					msg := read()
					update, ok := msg.Body.(*gobgp.BGPUpdate)
					if !ok {
						t.Fatalf("expected UPDATE, received %T", msg.Body)
					}
					if len(update.NLRI) == 0 {
						continue
					}
					attrs := map[gobgp.BGPAttrType]bool{}
					for _, a := range update.PathAttributes {
						attrs[a.GetType()] = true
					}
					for typ, want := range map[gobgp.BGPAttrType]bool{gobgp.BGP_ATTR_TYPE_COMMUNITIES: mode.standard, gobgp.BGP_ATTR_TYPE_EXTENDED_COMMUNITIES: mode.extended, gobgp.BGP_ATTR_TYPE_IP6_EXTENDED_COMMUNITIES: mode.extended, gobgp.BGP_ATTR_TYPE_LARGE_COMMUNITY: true} {
						if attrs[typ] != want {
							t.Fatalf("UPDATE attr %d present=%v want=%v", typ, attrs[typ], want)
						}
					}
					break
				}
				var observed uint32
				if err := server.ListPeer(ctx, &gobgpapi.ListPeerRequest{}, func(p *gobgpapi.Peer) { observed = p.GetConf().GetSendCommunity() }); err != nil {
					t.Fatal(err)
				}
				if observed != mode.mask {
					t.Fatalf("peer observed send=%d want=%d", observed, mode.mask)
				}
				var localPaths int
				if err := server.ListPath(gobgpapiutil.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: gobgp.RF_IPv4_UC}, func(_ gobgp.NLRI, paths []*gobgpapiutil.Path) {
					for _, p := range paths {
						localPaths++
						attrs := map[gobgp.BGPAttrType]bool{}
						for _, a := range p.Attrs {
							attrs[a.GetType()] = true
						}
						for _, typ := range []gobgp.BGPAttrType{gobgp.BGP_ATTR_TYPE_COMMUNITIES, gobgp.BGP_ATTR_TYPE_EXTENDED_COMMUNITIES, gobgp.BGP_ATTR_TYPE_IP6_EXTENDED_COMMUNITIES, gobgp.BGP_ATTR_TYPE_LARGE_COMMUNITY} {
							if !attrs[typ] {
								t.Errorf("export removed local RIB attr %d", typ)
							}
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				if localPaths != 1 {
					t.Fatalf("local RIB paths=%d want=1", localPaths)
				}
				if err := (&localPathServer{BgpServer: server}).DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: path}); err != nil {
					t.Fatal(err)
				}
				for {
					msg := read()
					update, ok := msg.Body.(*gobgp.BGPUpdate)
					if !ok {
						t.Fatalf("expected withdrawal, received %T", msg.Body)
					}
					if len(update.WithdrawnRoutes) > 0 {
						if update.WithdrawnRoutes[0].NLRI.String() != "10.77.60.11/32" {
							t.Fatalf("withdrawn=%v", update.WithdrawnRoutes)
						}
						break
					}
				}
			})
		}
	}
}

func TestGoBGPRejectsUnknownCommunityMasks(t *testing.T) {
	server := realDynamicPathServer(t)
	ctx := context.Background()
	if err := server.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: &gobgpapi.Peer{Conf: &gobgpapi.PeerConf{NeighborAddress: "127.0.0.2", PeerAsn: 64513, SendCommunity: 4}}}); err == nil {
		t.Fatal("unsupported peer mask accepted")
	}
	if err := server.AddPeerGroup(ctx, &gobgpapi.AddPeerGroupRequest{PeerGroup: &gobgpapi.PeerGroup{Conf: &gobgpapi.PeerGroupConf{PeerGroupName: "invalid", PeerAsn: 64513, SendCommunity: 4}}}); err == nil {
		t.Fatal("unsupported peer-group mask accepted")
	}
}
