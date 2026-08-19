// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestMobilityPoolMembersFromSAMNodeSetMergesLocalOverlay(t *testing.T) {
	spec := plannedPoolSpec()
	router := planningRouterForNode("onprem-router", spec)
	for index := range router.Spec.Resources {
		if router.Spec.Resources[index].Kind != "SAMNodeSet" {
			continue
		}
		router.Spec.Resources[index].Spec = api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
			{NodeRef: "onprem-router", Site: "published-pve01", Role: "onprem", MaxSecondaryIPs: 9},
			{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
		}}
	}
	poolSpec, _ := localizeMobilityPoolSpecForNode(spec, "onprem-router")

	resolved, err := resolveNormalizedMobilityPool(router, poolSpec)
	if err != nil {
		t.Fatalf("resolveNormalizedMobilityPool: %v", err)
	}
	if got := resolved.Resolved.MembersFrom; len(got) != 1 || got[0].Phase != "Resolved" || got[0].MemberCount != 2 {
		t.Fatalf("membersFrom = %#v, want resolved SAMNodeSet", got)
	}
	if got := resolved.Pool.Prefix.String(); got != "10.88.60.0/24" {
		t.Fatalf("normalized pool prefix = %q, want masked canonical prefix", got)
	}
	self := resolved.Pool.Self
	if self.NodeRef != "onprem-router" || self.Site != "published-pve01" || self.Capture.Type != "proxy-arp" || self.MaxSecondaryIPs != 9 {
		t.Fatalf("merged self = %#v, want NodeSet topology and local capture overlay", self)
	}
}

func TestMobilityPoolMembersFromSAMNodeSetMissingRequiredIsPending(t *testing.T) {
	now := time.Date(2026, 6, 8, 11, 1, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	router := planningRouterForNode("onprem-router", spec)
	resources := router.Spec.Resources[:0]
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "SAMNodeSet" {
			resources = append(resources, resource)
		}
	}
	router.Spec.Resources = resources

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(bgp.upserts) != 0 {
		t.Fatalf("BGP upserts = %#v, want none while SAMNodeSet is missing", bgp.upserts)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["phase"] != "Pending" {
		t.Fatalf("phase = %#v, want Pending status=%#v", status["phase"], status)
	}
	if got := fmt.Sprint(status["pendingSources"]); got != "[SAMNodeSet/cloudedge]" {
		t.Fatalf("pendingSources = %s", got)
	}
}

func TestMobilityPoolMembersFromPendingDefersPrefixValidation(t *testing.T) {
	spec := plannedPoolSpec()
	spec.Prefix = "not-a-prefix"
	router := planningRouterForNode("onprem-router", spec)
	resources := router.Spec.Resources[:0]
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "SAMNodeSet" {
			resources = append(resources, resource)
		}
	}
	router.Spec.Resources = resources
	poolSpec, _ := localizeMobilityPoolSpecForNode(spec, "onprem-router")

	resolved, err := resolveNormalizedMobilityPool(router, poolSpec)
	if err != nil {
		t.Fatalf("resolve pending MobilityPool: %v", err)
	}
	if len(resolved.Resolved.PendingSources) == 0 || resolved.Pool.Prefix.IsValid() {
		t.Fatalf("pending resolution = %#v, want unresolved source and no parsed prefix", resolved)
	}
}

func mobilityNodeSetResource(name string, nodes []api.SAMNodeSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.SAMNodeSetSpec{Nodes: nodes},
	}
}
