// SPDX-License-Identifier: BSD-3-Clause

package mobilityfib

import (
	"net/netip"
	"testing"

	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func testSnapshot(verdicts ...dynamicconfig.FIBVerdict) Snapshot {
	return NewSnapshotFromVerdicts(append([]dynamicconfig.FIBVerdict{testScopeVerdict()}, verdicts...))
}

func testScopeVerdict() dynamicconfig.FIBVerdict {
	return dynamicconfig.FIBVerdict{
		PoolRef: "cloudedge",
		Scope: &dynamicconfig.FIBPoolScope{
			Prefix: "10.77.60.0/24",
			RemoteReturnCommunities: []string{
				bgpstate.MobilityNodeIdentityCommunity("azure-router"),
			},
		},
	}
}

func TestSnapshotRejectsConflictLocalEvidence(t *testing.T) {
	snapshot := testSnapshot(dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.11/32", Action: ActionLocalRoute, OwnerNode: "aws-router-b", Reason: "remote-home-owner-overlaps-local-ownership-event"})
	prefix := netip.MustParsePrefix("10.77.60.11/32")
	if snapshot.AdmitBGPPath(prefix, nil) {
		t.Fatalf("AdmitBGPPath(%s) = true, want false for conflict with local provider evidence", prefix)
	}
}

func TestSnapshotAllowsOKRemoteMobilityRoutes(t *testing.T) {
	snapshot := testSnapshot(dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.12/32", Action: ActionDeliverRemote, OwnerNode: "azure-router"}, dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.10/32", Action: ActionDeliverRemote, OwnerNode: "azure-router"})
	for _, raw := range []string{"10.77.60.10/32", "10.77.60.12/32"} {
		prefix := netip.MustParsePrefix(raw)
		if !snapshot.AdmitBGPPath(prefix, nil) {
			t.Fatalf("AdmitBGPPath(%s) = false, want true for OK remote owner", prefix)
		}
	}
}

func TestSnapshotFailsClosedForUnknownMobilityAddress(t *testing.T) {
	snapshot := testSnapshot()
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), nil) {
		t.Fatal("unknown mobility address was admitted; want fail-closed until owner table is populated")
	}
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), []string{communityMobilityOwner, communityMobilitySourceObserved}) {
		t.Fatal("owner/source communities admitted an address without an explicit FIB verdict")
	}
	if !snapshot.AdmitBGPPath(netip.MustParsePrefix("192.0.2.12/32"), nil) {
		t.Fatal("non-mobility address was rejected")
	}
}

func TestSnapshotAdmitsRemoteSiteReturnRouteAndRejectsSameSiteReturnRoute(t *testing.T) {
	snapshot := testSnapshot()
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.6/32"), []string{
		communityMobilityReturnRoute,
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"),
	}) {
		t.Fatal("same-site router return-route was admitted; want local fabric route to win")
	}
	if !snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.14/32"), []string{
		communityMobilityReturnRoute,
		bgpstate.MobilityNodeIdentityCommunity("azure-router"),
	}) {
		t.Fatal("remote-site router return-route was rejected; want return path installed")
	}
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.14/32"), []string{communityMobilityReturnRoute}) {
		t.Fatal("return-route without node identity was admitted; want fail-closed")
	}
}

func TestSnapshotAdmitsRemoteSiteReturnRouteDespiteUnknownVerdict(t *testing.T) {
	snapshot := testSnapshot(dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.14/32", Action: ActionWithhold, Class: "Unknown", Reason: "bgp-rib"})
	if !snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.14/32"), []string{
		communityMobilityReturnRoute,
		bgpstate.MobilityNodeIdentityCommunity("azure-router"),
	}) {
		t.Fatal("remote-site return-route was rejected by an Unknown ownership verdict")
	}
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.6/32"), []string{
		communityMobilityReturnRoute,
		bgpstate.MobilityNodeIdentityCommunity("aws-router-b"),
	}) {
		t.Fatal("same-site return-route was admitted despite Unknown verdict ordering")
	}
}

func TestSnapshotRejectsTrustedBGPPathForLocalStaticOwnedAddress(t *testing.T) {
	snapshot := testSnapshot(dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.10/32", Action: ActionLocalRoute, Class: "StaticOwned", OwnerNode: "aws-router-a"})
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.10/32"), []string{communityMobilityOwner, communityMobilitySourceObserved}) {
		t.Fatal("local static-owned mobility address was admitted from BGP")
	}
}

func TestSnapshotRejectsNonHostRoutesInsideMobilityPool(t *testing.T) {
	snapshot := testSnapshot(dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.12/32", Action: ActionDeliverRemote, OwnerNode: "azure-router"})
	for _, raw := range []string{"10.77.60.0/24", "10.77.60.0/25"} {
		prefix := netip.MustParsePrefix(raw)
		if snapshot.AdmitBGPPath(prefix, nil) {
			t.Fatalf("AdmitBGPPath(%s) = true, want false for non-/32 route inside MobilityPool", prefix)
		}
	}
}

func TestSnapshotRejectsUnscopedMobilityData(t *testing.T) {
	snapshot := NewSnapshotFromVerdicts([]dynamicconfig.FIBVerdict{{
		PoolRef: "cloudedge", Address: "10.77.60.12/32", Action: ActionDeliverRemote,
	}})
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), []string{communityMobilityOwner, communityMobilitySourceObserved}) {
		t.Fatal("unscoped legacy mobility owner path was admitted")
	}
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), []string{communityMobilityReturnRoute, bgpstate.MobilityNodeIdentityCommunity("azure-router")}) {
		t.Fatal("unscoped legacy return route was admitted")
	}
	if !snapshot.AdmitBGPPath(netip.MustParsePrefix("192.0.2.12/32"), nil) {
		t.Fatal("ordinary BGP path was rejected without a mobility scope")
	}
}

func TestSnapshotRejectsInvalidOrConflictingScope(t *testing.T) {
	valid := testScopeVerdict()
	conflict := testScopeVerdict()
	conflict.Scope = &dynamicconfig.FIBPoolScope{
		Prefix:                  "10.77.61.0/24",
		RemoteReturnCommunities: valid.Scope.RemoteReturnCommunities,
	}
	snapshot := NewSnapshotFromVerdicts([]dynamicconfig.FIBVerdict{valid, conflict})
	for _, raw := range []string{"10.77.60.12/32", "10.77.61.12/32"} {
		if snapshot.AdmitBGPPath(netip.MustParsePrefix(raw), nil) {
			t.Fatalf("conflicting scope admitted %s", raw)
		}
	}
	invalid := testScopeVerdict()
	invalid.Scope = &dynamicconfig.FIBPoolScope{Prefix: "not-a-prefix"}
	if NewSnapshotFromVerdicts([]dynamicconfig.FIBVerdict{invalid}).AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), []string{communityMobilityOwner, communityMobilitySourceObserved}) {
		t.Fatal("invalid scope admitted mobility path")
	}
}

func TestSnapshotRejectsConflictingAddressVerdicts(t *testing.T) {
	snapshot := testSnapshot(
		dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.12/32", Action: ActionDeliverRemote, OwnerNode: "azure-router"},
		dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.12/32", Action: ActionWithhold, OwnerNode: "azure-router"},
	)
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), nil) {
		t.Fatal("conflicting address verdicts admitted a mobility path")
	}
}

func TestSnapshotRejectsMalformedAddressTableForWholePool(t *testing.T) {
	scope := testScopeVerdict()
	snapshot := NewSnapshotFromVerdicts([]dynamicconfig.FIBVerdict{
		scope,
		{PoolRef: "cloudedge", Address: "10.77.60.12/32", Action: ActionDeliverRemote},
		// A legacy normalizer would silently turn this into a /32. The typed
		// policy must instead reject the complete known Pool prefix.
		{PoolRef: "cloudedge", Address: "10.77.60.13", Action: ActionDeliverRemote},
	})
	for _, communities := range [][]string{nil, {communityMobilityOwner, communityMobilitySourceObserved}} {
		if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), communities) {
			t.Fatalf("malformed address table admitted a route with communities %#v", communities)
		}
	}
}

func TestSnapshotRejectsDuplicateAddressVerdictsForWholePool(t *testing.T) {
	scope := testScopeVerdict()
	row := dynamicconfig.FIBVerdict{PoolRef: "cloudedge", Address: "10.77.60.12/32", Action: ActionDeliverRemote}
	snapshot := NewSnapshotFromVerdicts([]dynamicconfig.FIBVerdict{scope, row, row})
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), nil) {
		t.Fatal("duplicate address rows admitted a route")
	}
}

func TestSnapshotProjectsPreferredSource(t *testing.T) {
	scope := testScopeVerdict()
	scope.Scope.PreferredSource = "10.77.60.10/32"
	snapshot := NewSnapshotFromVerdicts([]dynamicconfig.FIBVerdict{scope})
	sources := snapshot.PreferredSources()
	if len(sources) != 1 || sources[0].Address != "10.77.60.10" || sources[0].AddressPrefix != "10.77.60.10/32" {
		t.Fatalf("preferred sources = %#v", sources)
	}
}

func TestSnapshotIgnoresLegacyOwnerTableForFIBDecisions(t *testing.T) {
	snapshot := testSnapshot()
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), nil) {
		t.Fatal("legacy owner table admitted a mobility route; want verdict-only FIB policy")
	}
}
