// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestMobilityPoolMembersFromResolvesAndStaticOverridesSetMember(t *testing.T) {
	now := time.Date(2026, 6, 8, 11, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	staticSelf := spec.Members[0]
	staticSelf.Site = "local-pve01"
	spec.Members = []api.MobilityPoolMember{staticSelf}
	spec.MembersFrom = []api.MobilityMembersSourceSpec{{Resource: "MobilityMemberSet/cloudedge"}}
	router := planningRouterForNode("onprem-router", spec)
	router.Spec.Resources = append(router.Spec.Resources, mobilityMemberSetResource("cloudedge", []api.MobilityMemberSetMember{
		{NodeRef: "onprem-router", Site: "published-pve01", Role: "onprem"},
		{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
	}))

	controller := Controller{Router: router, Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "BGPPlanned" {
		t.Fatalf("plannerPhase = %#v status=%#v", status["plannerPhase"], status)
	}
	if fmt.Sprint(status["resolvedMemberCount"]) != "2" {
		t.Fatalf("resolvedMemberCount = %#v, want 2 status=%#v", status["resolvedMemberCount"], status)
	}
	membersFrom, ok := status["membersFrom"].([]any)
	if !ok || len(membersFrom) != 1 {
		t.Fatalf("membersFrom status = %#v, want one source", status["membersFrom"])
	}

	resolved, err := (mobilityMemberResolver{Router: router}).resolve(context.Background(), spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	self := resolved.Spec.Members[0]
	if self.NodeRef != "onprem-router" || self.Site != "local-pve01" || self.Capture.Type != "proxy-arp" {
		t.Fatalf("merged self member = %#v, want static override with capture", self)
	}
}

func TestMobilityPoolMembersFromMissingRequiredIsPending(t *testing.T) {
	now := time.Date(2026, 6, 8, 11, 1, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.Members = []api.MobilityPoolMember{spec.Members[0]}
	spec.MembersFrom = []api.MobilityMembersSourceSpec{{Resource: "MobilityMemberSet/cloudedge"}}
	router := planningRouterForNode("onprem-router", spec)

	bgp := &fakeBGPPaths{}
	controller := Controller{Router: router, Store: store, BGPPaths: bgp, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(bgp.upserts) != 0 {
		t.Fatalf("BGP upserts = %#v, want none while membersFrom is pending", bgp.upserts)
	}
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "Pending" {
		t.Fatalf("plannerPhase = %#v, want Pending status=%#v", status["plannerPhase"], status)
	}
}

func TestMobilityPoolMembersFromPreservesCaptureFields(t *testing.T) {
	spec := plannedPoolSpec()
	spec.Members = nil
	spec.MembersFrom = []api.MobilityMembersSourceSpec{{Resource: "MobilityMemberSet/cloudedge"}}
	router := planningRouterForNode("aws-router-a", spec)
	router.Spec.Resources = append(router.Spec.Resources, mobilityMemberSetResource("cloudedge", []api.MobilityMemberSetMember{{
		NodeRef:    "aws-router-a",
		Site:       "aws",
		Role:       "cloud",
		ProfileRef: "aws-self",
		Capture: api.MobilityMemberCapture{
			Type:         "provider-secondary-ip",
			ProviderRef:  "aws-provider",
			ProviderMode: "route-table",
			Strategy:     captureStrategyRouteTable,
			NICRef:       "eni-a",
			Target:       map[string]string{"routeTableRef": "rtb-cloudedge", "region": "us-east-1"},
		},
		OwnershipDiscovery: api.MobilityOwnershipDiscovery{
			Mode:         "provider-private-ip",
			SubnetRef:    "subnet-a",
			ScanInterval: "60s",
		},
		Placement: api.MobilityMemberPlacement{Group: "aws-edge", Priority: 10},
	}}))

	resolved, err := (mobilityMemberResolver{Router: router}).resolve(context.Background(), spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved.Spec.Members) != 1 {
		t.Fatalf("members = %#v, want one", resolved.Spec.Members)
	}
	member := resolved.Spec.Members[0]
	if member.ProfileRef != "aws-self" || member.Capture.NICRef != "eni-a" || member.Capture.Strategy != captureStrategyRouteTable || member.Capture.Target["routeTableRef"] != "rtb-cloudedge" {
		t.Fatalf("member = %#v, want capture/profile fields preserved", member)
	}
	if member.OwnershipDiscovery.Mode != "provider-private-ip" || member.OwnershipDiscovery.SubnetRef != "subnet-a" {
		t.Fatalf("ownershipDiscovery = %#v, want preserved", member.OwnershipDiscovery)
	}
}

func TestMobilityPoolPublishesMemberSetDynamicPart(t *testing.T) {
	now := time.Date(2026, 6, 8, 11, 2, 0, 0, time.UTC)
	store := testStore(t, now)
	spec := plannedPoolSpec()
	spec.DeliveryPolicy.Mode = "bgp"
	spec.PublishMemberSet = true
	router := planningRouterForNode("onprem-router", spec)

	controller := Controller{Router: router, Store: store, BGPPaths: &fakeBGPPaths{}, Now: func() time.Time { return now }}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	resources := decodeResources(t, latestPart(t, store, MobilityMemberSetDynamicSource("cloudedge")).ResourcesJSON)
	if len(resources) != 1 || resources[0].Kind != "MobilityMemberSet" || resources[0].Metadata.Name != "cloudedge" {
		t.Fatalf("published resources = %#v, want MobilityMemberSet/cloudedge", resources)
	}
	set, err := resources[0].MobilityMemberSetSpec()
	if err != nil {
		t.Fatalf("MobilityMemberSet spec: %v", err)
	}
	if len(set.Members) != 2 {
		t.Fatalf("published members = %#v, want 2", set.Members)
	}
	for _, member := range set.Members {
		if member.NodeRef == "azure-router" && (member.Capture.NICRef == "" || member.Capture.ProviderRef != "azure-provider") {
			t.Fatalf("published azure member = %#v, want capture fields preserved", member)
		}
	}
	for _, member := range set.Members {
		if member.NodeRef == "onprem-router" && member.Placement.Group == "" {
			return
		}
	}
	t.Fatalf("published members = %#v, want onprem-router member", set.Members)
}
