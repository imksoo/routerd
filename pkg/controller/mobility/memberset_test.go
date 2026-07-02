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

func TestMobilityPoolMembersFromPreservesStaticOwnedAcrossDuplicateMembers(t *testing.T) {
	spec := plannedPoolSpec()
	spec.MembersFrom = []api.MobilityMembersSourceSpec{{Resource: "MobilityMemberSet/cloudedge"}}
	spec.Members = []api.MobilityPoolMember{{
		NodeRef:              "onprem-router",
		Site:                 "local-pve01",
		Role:                 "onprem",
		StaticOwnedAddresses: []string{"10.88.60.99/32"},
	}}
	router := planningRouterForNode("onprem-router", spec)
	router.Spec.Resources = append(router.Spec.Resources, mobilityMemberSetResource("cloudedge", []api.MobilityMemberSetMember{
		{NodeRef: "onprem-router", Site: "published-pve01", Role: "onprem", StaticOwnedAddresses: []string{"10.88.60.10/32", "10.88.60.98/32"}},
		{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
	}))

	resolved, err := (mobilityMemberResolver{Router: router}).resolve(context.Background(), spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	self := resolved.Spec.Members[0]
	if self.NodeRef != "onprem-router" || self.Site != "local-pve01" {
		t.Fatalf("resolved self member = %#v, want local override", self)
	}
	want := map[string]bool{
		"10.88.60.10/32": true,
		"10.88.60.98/32": true,
		"10.88.60.99/32": true,
	}
	if len(self.StaticOwnedAddresses) != len(want) {
		t.Fatalf("staticOwnedAddresses = %#v, want %v", self.StaticOwnedAddresses, want)
	}
	for _, address := range self.StaticOwnedAddresses {
		if !want[address] {
			t.Fatalf("staticOwnedAddresses = %#v, want %v", self.StaticOwnedAddresses, want)
		}
	}
}

func TestMobilityPoolMembersFromPreservesCaptureAcrossDuplicateMembers(t *testing.T) {
	spec := api.MobilityPoolSpec{
		MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "MobilityMemberSet/cloudedge"}},
		Members: []api.MobilityPoolMember{{
			NodeRef: "aws-router-a",
			Site:    "override-site",
			Role:    "cloud",
		}},
	}
	router := planningRouterForNode("aws-router-a", spec)
	router.Spec.Resources = append(router.Spec.Resources, mobilityMemberSetResource("cloudedge", []api.MobilityMemberSetMember{{
		NodeRef:    "aws-router-a",
		Site:       "source-site",
		Role:       "cloud",
		ProfileRef: "aws-profile",
		Capture: api.MobilityMemberCapture{
			Type:            "provider-secondary-ip",
			ProviderRef:     "aws-provider",
			ProviderMode:    "nic-secondary-ip",
			NICRef:          "eni-a",
			CaptureStrategy: captureStrategyRouteTable,
			Target:          map[string]string{"routeTableRef": "rtb-cloudedge", "region": "us-east-1"},
		},
		StaticOwnedAddresses: []string{"10.88.60.10/32", "10.88.60.98/32"},
	}}))

	resolved, err := (mobilityMemberResolver{Router: router}).resolve(context.Background(), spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	member := resolved.Spec.Members[0]
	if member.NodeRef != "aws-router-a" {
		t.Fatalf("member = %#v, want aws-router-a", member)
	}
	if member.Site != "override-site" {
		t.Fatalf("member site = %#v, want override-site", member)
	}
	if member.Capture.NICRef != "eni-a" || member.Capture.ProviderRef != "aws-provider" || member.Capture.CaptureStrategy != captureStrategyRouteTable {
		t.Fatalf("member capture = %#v, want preserved source capture", member.Capture)
	}
	if member.Capture.Target["routeTableRef"] != "rtb-cloudedge" {
		t.Fatalf("member capture target = %#v, want routeTableRef", member.Capture.Target)
	}
	if len(member.StaticOwnedAddresses) != 2 {
		t.Fatalf("staticOwnedAddresses = %#v, want source+override merged", member.StaticOwnedAddresses)
	}
	member.StaticOwnedAddresses = cleanStrings(member.StaticOwnedAddresses)
	wantStatic := map[string]bool{"10.88.60.10/32": true, "10.88.60.98/32": true}
	for _, address := range member.StaticOwnedAddresses {
		if !wantStatic[address] {
			t.Fatalf("staticOwnedAddresses = %#v, want %v", member.StaticOwnedAddresses, wantStatic)
		}
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

func TestMobilityPoolMembersFromUsesExpiredLastKnownGoodMemberSet(t *testing.T) {
	observedAt := time.Date(2026, 6, 8, 11, 1, 30, 0, time.UTC)
	now := observedAt.Add(DefaultLeaseTTL + time.Second)
	store := testStore(t, observedAt)
	writeMemberSetPart(t, store, MemberSetSyncDynamicSource("cloudedge"), "cloudedge", []api.MobilityMemberSetMember{
		{NodeRef: "onprem-router", Site: "pve", Role: "onprem"},
		{NodeRef: "azure-router", Site: "azure", Role: "cloud"},
	}, observedAt)

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
	status := store.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")
	if status["plannerPhase"] != "BGPPlanned" {
		t.Fatalf("plannerPhase = %#v, want BGPPlanned status=%#v", status["plannerPhase"], status)
	}
	membersFrom, ok := status["membersFrom"].([]any)
	if !ok || len(membersFrom) != 1 {
		t.Fatalf("membersFrom status = %#v, want one source", status["membersFrom"])
	}
	source, ok := membersFrom[0].(map[string]any)
	if !ok || source["phase"] != "Stale" {
		t.Fatalf("membersFrom[0] = %#v, want phase Stale", membersFrom[0])
	}
	if source["warning"] == "" {
		t.Fatalf("membersFrom[0] = %#v, want stale warning", membersFrom[0])
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
			Type:            "provider-secondary-ip",
			ProviderRef:     "aws-provider",
			ProviderMode:    "route-table",
			CaptureStrategy: captureStrategyRouteTable,
			NICRef:          "eni-a",
			Target:          map[string]string{"routeTableRef": "rtb-cloudedge", "region": "us-east-1"},
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
	if member.ProfileRef != "aws-self" || member.Capture.NICRef != "eni-a" || member.Capture.CaptureStrategy != captureStrategyRouteTable || member.Capture.Target["routeTableRef"] != "rtb-cloudedge" {
		t.Fatalf("member = %#v, want capture/profile fields preserved", member)
	}
	if member.OwnershipDiscovery.Mode != "provider-private-ip" || member.OwnershipDiscovery.SubnetRef != "subnet-a" {
		t.Fatalf("ownershipDiscovery = %#v, want preserved", member.OwnershipDiscovery)
	}
}

func TestMobilityPoolMembersFromMergesOwnershipDiscoveryDetails(t *testing.T) {
	spec := plannedPoolSpec()
	spec.Members = []api.MobilityPoolMember{{
		NodeRef: "onprem-router",
		OwnershipDiscovery: api.MobilityOwnershipDiscovery{
			Mode: "onprem-l2",
			Sources: []api.MobilityOwnershipDiscoverySource{
				{Type: OnPremSourceARPObserver, Interface: "svnet1"},
				{Type: OnPremSourcePVESVNet, Interface: "svnet1", Network: "svnet1"},
			},
			Scope: api.MobilityOwnershipDiscoveryScope{
				ExcludeAddresses: []string{"192.168.123.1/32"},
			},
			Selector: api.MobilityOwnershipDiscoverySelector{Tags: map[string]string{"role": "client"}},
		},
	}}
	spec.MembersFrom = []api.MobilityMembersSourceSpec{{Resource: "MobilityMemberSet/cloudedge"}}
	router := planningRouterForNode("onprem-router", spec)
	router.Spec.Resources = append(router.Spec.Resources, mobilityMemberSetResource("cloudedge", []api.MobilityMemberSetMember{{
		NodeRef: "onprem-router",
		Site:    "pve",
		Role:    "onprem",
		Capture: api.MobilityMemberCapture{Type: "proxy-arp", Interface: "svnet1"},
	}}))

	resolved, err := (mobilityMemberResolver{Router: router}).resolve(context.Background(), spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	member := resolved.Spec.Members[0]
	if len(member.OwnershipDiscovery.Sources) != 2 || member.OwnershipDiscovery.Sources[1].Network != "svnet1" {
		t.Fatalf("ownershipDiscovery.sources = %#v, want local patch sources", member.OwnershipDiscovery.Sources)
	}
	if len(member.OwnershipDiscovery.Scope.ExcludeAddresses) != 1 || member.OwnershipDiscovery.Scope.ExcludeAddresses[0] != "192.168.123.1/32" {
		t.Fatalf("ownershipDiscovery.scope = %#v, want local patch scope", member.OwnershipDiscovery.Scope)
	}
	if member.OwnershipDiscovery.Selector.Tags["role"] != "client" {
		t.Fatalf("ownershipDiscovery.selector = %#v, want local patch selector", member.OwnershipDiscovery.Selector)
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
