// SPDX-License-Identifier: BSD-3-Clause

package mobilityfib

import (
	"net/netip"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

type mapStatusReader map[string]map[string]any

func (r mapStatusReader) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := r[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func TestSnapshotRejectsConflictLocalEvidenceAndCreatesLocalRoute(t *testing.T) {
	snapshot := NewSnapshot(testRouter("aws-router-a"), mapStatusReader{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
			"ownershipResolverFIBVerdicts": []any{
				map[string]any{
					"address":        "10.77.60.11/32",
					"action":         ActionLocalRoute,
					"conflictReason": "remote-home-owner-overlaps-local-ownership-event",
					"ownerNode":      "aws-router-b",
					"localNode":      "aws-router-a",
				},
			},
		},
	})
	prefix := netip.MustParsePrefix("10.77.60.11/32")
	if snapshot.AdmitBGPRoute(prefix) {
		t.Fatalf("AdmitBGPRoute(%s) = true, want false for conflict with local provider evidence", prefix)
	}
	got := snapshot.LocalRouteAddressesForPool("cloudedge")
	if len(got) != 1 || got[0] != "10.77.60.11/32" {
		t.Fatalf("local routes = %#v, want 10.77.60.11/32", got)
	}
}

func TestSnapshotAllowsOKRemoteMobilityRoutes(t *testing.T) {
	snapshot := NewSnapshot(testRouter("aws-router-a"), mapStatusReader{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
			"ownershipResolverFIBVerdicts": []any{
				map[string]any{
					"address":   "10.77.60.12/32",
					"action":    ActionDeliverRemote,
					"ownerNode": "azure-router",
				},
				map[string]any{
					"address":   "10.77.60.10/32",
					"action":    ActionDeliverRemote,
					"ownerNode": "azure-router",
				},
			},
		},
	})
	for _, raw := range []string{"10.77.60.10/32", "10.77.60.12/32"} {
		prefix := netip.MustParsePrefix(raw)
		if !snapshot.AdmitBGPRoute(prefix) {
			t.Fatalf("AdmitBGPRoute(%s) = false, want true for OK remote owner", prefix)
		}
	}
}

func TestSnapshotFailsClosedForUnknownMobilityAddress(t *testing.T) {
	snapshot := NewSnapshot(testRouter("aws-router-a"), mapStatusReader{})
	if snapshot.AdmitBGPRoute(netip.MustParsePrefix("10.77.60.12/32")) {
		t.Fatal("unknown mobility address was admitted; want fail-closed until owner table is populated")
	}
	if !snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.12/32"), []string{communityMobilityOwner, communityMobilitySourceObserved}) {
		t.Fatal("trusted mobility BGP owner path was rejected; want BGP evidence to bridge missing local owner verdict")
	}
	if !snapshot.AdmitBGPRoute(netip.MustParsePrefix("192.0.2.12/32")) {
		t.Fatal("non-mobility address was rejected")
	}
}

func TestSnapshotRejectsTrustedBGPPathForLocalStaticOwnedAddress(t *testing.T) {
	router := testRouter("aws-router-a")
	spec := router.Spec.Resources[1].Spec.(api.MobilityPoolSpec)
	spec.Members[0].StaticOwnedAddresses = []string{"10.77.60.10/32"}
	router.Spec.Resources[1].Spec = spec
	snapshot := NewSnapshot(router, mapStatusReader{})
	if snapshot.AdmitBGPPath(netip.MustParsePrefix("10.77.60.10/32"), []string{communityMobilityOwner, communityMobilitySourceObserved}) {
		t.Fatal("local static-owned mobility address was admitted from BGP")
	}
}

func TestSnapshotRejectsNonHostRoutesInsideMobilityPool(t *testing.T) {
	snapshot := NewSnapshot(testRouter("aws-router-a"), mapStatusReader{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
			"ownershipResolverFIBVerdicts": []any{
				map[string]any{
					"address":   "10.77.60.12/32",
					"action":    ActionDeliverRemote,
					"ownerNode": "azure-router",
				},
			},
		},
	})
	for _, raw := range []string{"10.77.60.0/24", "10.77.60.0/25"} {
		prefix := netip.MustParsePrefix(raw)
		if snapshot.AdmitBGPRoute(prefix) {
			t.Fatalf("AdmitBGPRoute(%s) = true, want false for non-/32 route inside MobilityPool", prefix)
		}
	}
}

func TestSnapshotIgnoresLegacyOwnerTableForFIBDecisions(t *testing.T) {
	snapshot := NewSnapshot(testRouter("aws-router-a"), mapStatusReader{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": {
			"ownershipResolverOwnerTable": []any{
				map[string]any{
					"address":   "10.77.60.12/32",
					"class":     "RemoteHomeOwned",
					"state":     "OK",
					"ownerNode": "azure-router",
				},
			},
		},
	})
	if snapshot.AdmitBGPRoute(netip.MustParsePrefix("10.77.60.12/32")) {
		t.Fatal("legacy owner table admitted a mobility route; want verdict-only FIB policy")
	}
}

func testRouter(self string) *api.Router {
	return &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
					Metadata: api.ObjectMeta{Name: "cloudedge"},
					Spec:     api.EventGroupSpec{NodeName: self},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
					Metadata: api.ObjectMeta{Name: "cloudedge"},
					Spec: api.MobilityPoolSpec{
						Prefix:         "10.77.60.0/24",
						GroupRef:       "cloudedge",
						DeliveryPolicy: api.MobilityDeliveryPolicy{Mode: "bgp"},
						Members: []api.MobilityPoolMember{
							{
								NodeRef: self,
								Capture: api.MobilityMemberCapture{
									Type:      "provider-secondary-ip",
									Interface: "ens5",
								},
							},
							{NodeRef: "aws-router-b"},
							{NodeRef: "azure-router"},
						},
					},
				},
			},
		},
	}
}
