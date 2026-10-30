// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

func TestCommunitySendIntentReachesStaticDynamicAndRestartPeers(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "core"}, Spec: api.BGPRouterSpec{ASN: 64512, RouterID: "192.0.2.1", Communities: api.BGPCommunitiesSpec{Send: "standard"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "inherited"}, Spec: api.BGPPeerSpec{RouterRef: "BGPRouter/core", PeerASN: 64513, Peers: []string{"192.0.2.2"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "override"}, Spec: api.BGPPeerSpec{RouterRef: "BGPRouter/core", PeerASN: 64513, Peers: []string{"192.0.2.3"}, Communities: api.BGPCommunitiesSpec{Send: "extended"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPDynamicPeer"}, Metadata: api.ObjectMeta{Name: "leaves"}, Spec: api.BGPDynamicPeerSpec{RouterRef: "BGPRouter/core", PeerASN: 64513, Listen: api.BGPDynamicPeerListenSpec{SourcePrefixes: []string{"198.51.100.0/24"}}}},
	}}}
	c := &Controller{Router: router}
	peers, err := c.desiredPeers("core", 64512)
	if err != nil {
		t.Fatal(err)
	}
	for address, want := range map[string]uint32{"192.0.2.2": 1, "192.0.2.3": 2} {
		if got := goBGPPeer(peers[address]).GetConf().GetSendCommunity(); got != want {
			t.Fatalf("peer %s send mask=%d want=%d", address, got, want)
		}
	}
	dynamic, err := c.desiredDynamicPeers("core", 64512)
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range dynamic {
		if got := goBGPDynamicPeerGroup(peer).GetConf().GetSendCommunity(); got != 1 {
			t.Fatalf("dynamic peer send mask=%d", got)
		}
	}
	config := bgpdaemon.AppliedConfig{Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1"}, Peers: map[string]bgpdaemon.AppliedPeer{}}
	for address, peer := range peers {
		config.Peers[address] = appliedPeer(peer)
	}
	path := filepath.Join(t.TempDir(), "applied.json")
	if err := bgpdaemon.WriteApplied(path, config); err != nil {
		t.Fatal(err)
	}
	stored, _, err := bgpdaemon.ReadApplied(path)
	if err != nil {
		t.Fatal(err)
	}
	for address, peer := range desiredPeersFromApplied(64512, stored.Peers) {
		if goBGPPeer(peer).GetConf().GetSendCommunity() != peers[address].SendCommunity {
			t.Fatalf("restart lost send mode for %s", address)
		}
	}
}
