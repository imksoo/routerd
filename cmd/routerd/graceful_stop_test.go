// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestGracefulStopSuccessorsRejectIdentityCollisionAcrossGroups(t *testing.T) {
	router := gracefulStopMembershipRouter("router-a", []api.SAMNodeSpec{
		{NodeRef: "router-a", Placement: api.MobilityMemberPlacement{Group: "edge"}},
		{NodeRef: "router-b", Placement: api.MobilityMemberPlacement{Group: "edge"}},
		{NodeRef: "other-node-22477", Placement: api.MobilityMemberPlacement{Group: "other"}, Maintenance: api.MobilityMemberMaintenance{Drain: true}},
	})
	spec, err := router.Spec.Resources[2].MobilityPoolSpec()
	if err != nil {
		t.Fatalf("MobilityPoolSpec: %v", err)
	}
	if _, err := gracefulStopSuccessorCommunities(router, spec, "router-a"); err == nil {
		t.Fatal("gracefulStopSuccessorCommunities accepted ambiguous identity community")
	}
}

func gracefulStopMembershipRouter(self string, nodes []api.SAMNodeSpec) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec:     api.EventGroupSpec{NodeName: self},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
				Metadata: api.ObjectMeta{Name: "cloudedge-nodes"},
				Spec:     api.SAMNodeSetSpec{Nodes: nodes},
			},
			{
				TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
				Metadata: api.ObjectMeta{Name: "cloudedge"},
				Spec: api.MobilityPoolSpec{
					Prefix:      "10.88.60.0/24",
					GroupRef:    "cloudedge",
					MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/cloudedge-nodes"}},
				},
			},
		}},
	}
}
