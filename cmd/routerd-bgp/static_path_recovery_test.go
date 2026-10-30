// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpapiutil "github.com/osrg/gobgp/v4/pkg/apiutil"
	gobgp "github.com/osrg/gobgp/v4/pkg/packet/bgp"
)

func TestStaticPathLostUUIDRemainsOwnedAndCanBeWithdrawn(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	ctx := context.Background()
	dynamic := bgpdaemon.AppliedPath{Source: "MobilityPool/shared/node/remote", Prefix: "10.77.60.11/32"}
	if _, _, err := upsertDynamicPath(ctx, server, statePath, dynamic); err != nil {
		t.Fatal(err)
	}
	static := bgpdaemon.StaticAppliedPath(dynamic.Prefix, nil)
	dir, held := filepath.Dir(statePath), filepath.Dir(statePath)+"-held"
	fault := &interruptedDynamicPathServer{localPathServer: server}
	fault.beforeAdd = func() {
		stored, found, err := bgpdaemon.ReadApplied(statePath)
		if err != nil || !found || len(stored.PendingPathAdds) != 1 || stored.PendingPathAdds[0].Source != bgpdaemon.AppliedPathSourceStatic {
			t.Fatalf("static Add preceded durable ownership: %+v, %v", stored, err)
		}
	}
	fault.afterAdd = func() {
		if err := os.Rename(dir, held); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir, []byte("block journal commit"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := upsertOwnedPath(ctx, fault, statePath, static); err == nil {
		t.Fatal("interrupted static UUID write reported success")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(held, dir); err != nil {
		t.Fatal(err)
	}
	if len(nativePathIdentities(t, server)) != 2 {
		t.Fatal("fixture did not retain the live unacknowledged static path")
	}
	if _, err := reconcileStaticPaths(ctx, server, statePath, []bgpdaemon.AppliedPath{}); err != nil {
		t.Fatal(err)
	}
	remaining := nativePathIdentities(t, server)
	if len(remaining) != 1 || remaining[0].identifier != bgpdaemon.NativePathIdentifier(dynamic.Source) {
		t.Fatalf("static withdrawal changed another native owner: %+v", remaining)
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.PendingPathAdds) != 0 || len(stored.PendingPathRemovals) != 0 || len(stored.Paths) != 1 || stored.Paths[0].Source != dynamic.Source {
		t.Fatalf("static withdrawal did not finish recovery: %+v, %v", stored, err)
	}
}

func TestStaticZeroIdentifierWithdrawalPreservesPeerPathAndNextHop(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, applied := dynamicJournal(t)
	ctx := context.Background()
	intent := bgpdaemon.PolicyIntent{RouterName: "zero", Scopes: []bgpdaemon.PolicyScope{{Name: "routerd-zero-peer", Neighbors: []string{"192.0.2.2/32"}, ExportPolicyName: "routerd-zero-export"}}}
	if _, err := reconcilePolicyConfig(ctx, server, statePath, applied, intent); err != nil {
		t.Fatal(err)
	}
	static := bgpdaemon.StaticAppliedPath("10.77.60.11/32", nil)
	peerPath, err := bgpdaemon.PathFromAppliedPath(static)
	if err != nil {
		t.Fatal(err)
	}
	peerPath.SourceAsn, peerPath.SourceId, peerPath.NeighborIp = 64513, "192.0.2.2", "192.0.2.2"
	peerPath.Pattrs = append(peerPath.Pattrs, &gobgpapi.Attribute{Attr: &gobgpapi.Attribute_NextHop{NextHop: &gobgpapi.NextHopAttribute{NextHop: "192.0.2.254"}}})
	// Replace the encoder's unspecified next hop with the peer's usable
	// third-party next hop, without introducing a duplicate BGP attribute.
	peerPath.Pattrs = append(peerPath.Pattrs[:1], peerPath.Pattrs[len(peerPath.Pattrs)-1])
	if _, err := server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: peerPath}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := upsertOwnedPath(ctx, server, statePath, static); err != nil {
		t.Fatal(err)
	}
	if got := nativePathIdentities(t, server); len(got) != 2 {
		t.Fatalf("fixture missing local/peer zero IDs: %+v", got)
	}
	if _, err := deleteOwnedPath(ctx, server, statePath, static); err != nil {
		t.Fatal(err)
	}
	remaining := nativePathIdentities(t, server)
	if len(remaining) != 1 || remaining[0].identifier != 0 || remaining[0].peerASN != 64513 {
		t.Fatalf("zero ID withdrawal deleted peer path: %+v", remaining)
	}
	var nextHop string
	if err := server.BgpServer.ListPath(gobgpapiutil.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: gobgp.RF_IPv4_UC}, func(_ gobgp.NLRI, paths []*gobgpapiutil.Path) {
		for _, path := range paths {
			for _, attr := range path.Attrs {
				if hop, ok := attr.(*gobgp.PathAttributeNextHop); ok {
					nextHop = hop.Value.String()
				}
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if nextHop != "192.0.2.254" {
		t.Fatalf("empty import intent rewrote usable third-party next hop: %s", nextHop)
	}
}

func TestStaticNamedCommunitiesSurviveNativeDaemonRestore(t *testing.T) {
	for _, prefix := range []string{"10.77.60.11/32", "2001:db8::/64"} {
		t.Run(prefix, func(t *testing.T) {
			server := realDynamicPathServer(t)
			statePath, _ := dynamicJournal(t)
			path := bgpdaemon.StaticAppliedPath(prefix, nil)
			path.Attrs.Communities = []string{"internet", "no-export", "no-advertise"}
			if _, err := reconcileStaticPaths(context.Background(), server, statePath, []bgpdaemon.AppliedPath{path}); err != nil {
				t.Fatalf("apply named static communities: %v", err)
			}
			stored, _, err := bgpdaemon.ReadApplied(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if err := restoreAppliedPaths(context.Background(), server, &stored); err != nil {
				t.Fatalf("restore accepted named communities: %v", err)
			}
		})
	}
}
