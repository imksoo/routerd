// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	mobilitycontroller "github.com/imksoo/routerd/pkg/controller/mobility"
)

type gracefulStopFakeBGP struct {
	paths      map[string]bgpdaemon.AppliedPath
	observed   []bgpdaemon.ObservedPath
	observeErr error
}

func (f *gracefulStopFakeBGP) ListPaths(_ context.Context, source string) ([]bgpdaemon.AppliedPath, error) {
	var out []bgpdaemon.AppliedPath
	for _, path := range f.paths {
		if source == "" || path.Source == source {
			out = append(out, path)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source == out[j].Source {
			return out[i].Prefix < out[j].Prefix
		}
		return out[i].Source < out[j].Source
	})
	return out, nil
}

func (f *gracefulStopFakeBGP) UpsertPath(_ context.Context, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedPath, error) {
	if f.paths == nil {
		f.paths = map[string]bgpdaemon.AppliedPath{}
	}
	path = bgpdaemon.NormalizeAppliedPath(path)
	f.paths[bgpdaemon.AppliedPathKey(path)] = path
	return path, nil
}

func (f *gracefulStopFakeBGP) DeletePath(_ context.Context, path bgpdaemon.AppliedPath) error {
	delete(f.paths, bgpdaemon.AppliedPathKey(bgpdaemon.NormalizeAppliedPath(path)))
	return nil
}

func (f *gracefulStopFakeBGP) ListObservedPaths(context.Context) ([]bgpdaemon.ObservedPath, error) {
	if f.observeErr != nil {
		return nil, f.observeErr
	}
	return append([]bgpdaemon.ObservedPath(nil), f.observed...), nil
}

func TestGracefulStopLiveRIBPreflightRejectsUnavailableHelper(t *testing.T) {
	want := errors.New("404 live BGP RIB observation is unavailable")
	bgp := &gracefulStopFakeBGP{observeErr: want}
	_, err := gracefulStopLiveRIB(context.Background(), bgp)
	if !errors.Is(err, want) {
		t.Fatalf("gracefulStopLiveRIB error = %v, want wrapped %v", err, want)
	}
}

func TestGracefulStopExclusiveDoesNotRunAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := runGracefulStopHandoffExclusive(ctx, nil, func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runGracefulStopHandoffExclusive error = %v, want context canceled", err)
	}
	if called {
		t.Fatal("runGracefulStopHandoffExclusive ran handoff after cancellation")
	}
}

func TestGracefulStopTargetsSkipPlacementWithoutEligibleSuccessor(t *testing.T) {
	router := gracefulStopMembershipRouter("router-a", []api.SAMNodeSpec{
		{NodeRef: "router-a", Placement: api.MobilityMemberPlacement{Group: "edge"}},
		{NodeRef: "router-b", Placement: api.MobilityMemberPlacement{Group: "edge"}, Maintenance: api.MobilityMemberMaintenance{Drain: true}},
	})
	source := mobilitycontroller.DynamicSource("cloudedge", "router-a")
	bgp := &gracefulStopFakeBGP{paths: map[string]bgpdaemon.AppliedPath{
		"self": {Source: source, Prefix: "10.88.60.11/32", Family: bgpdaemon.AppliedPathFamilyIPv4Unicast},
	}}
	targets, err := gracefulStopTargets(context.Background(), router, bgp)
	if err != nil {
		t.Fatalf("gracefulStopTargets: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("gracefulStopTargets = %#v, want no impossible handoff target", targets)
	}
}

func TestGracefulStopTargetsScopeMixedPoolHandoff(t *testing.T) {
	router := gracefulStopMembershipRouter("router-a", []api.SAMNodeSpec{
		{NodeRef: "router-a", Placement: api.MobilityMemberPlacement{Group: "edge"}},
		{NodeRef: "router-b", Placement: api.MobilityMemberPlacement{Group: "edge"}},
	})
	router.Spec.Resources = append(router.Spec.Resources,
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
			Metadata: api.ObjectMeta{Name: "singleton-nodes"},
			Spec: api.SAMNodeSetSpec{Nodes: []api.SAMNodeSpec{
				{NodeRef: "router-a", Placement: api.MobilityMemberPlacement{Group: "singleton"}},
			}},
		},
		api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool"},
			Metadata: api.ObjectMeta{Name: "singleton"},
			Spec: api.MobilityPoolSpec{
				Prefix:      "10.88.61.0/24",
				GroupRef:    "cloudedge",
				MembersFrom: []api.MobilityMembersSourceSpec{{Resource: "SAMNodeSet/singleton-nodes"}},
			},
		},
	)
	bgp := &gracefulStopFakeBGP{paths: map[string]bgpdaemon.AppliedPath{
		"edge":      {Source: mobilitycontroller.DynamicSource("cloudedge", "router-a"), Prefix: "10.88.60.11/32", Family: bgpdaemon.AppliedPathFamilyIPv4Unicast},
		"singleton": {Source: mobilitycontroller.DynamicSource("singleton", "router-a"), Prefix: "10.88.61.11/32", Family: bgpdaemon.AppliedPathFamilyIPv4Unicast},
	}}
	targets, err := gracefulStopTargets(context.Background(), router, bgp)
	if err != nil {
		t.Fatalf("gracefulStopTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].PoolName != "cloudedge" {
		t.Fatalf("gracefulStopTargets = %#v, want only cloudedge", targets)
	}
	if pools := gracefulStopTargetPools(targets); !pools["cloudedge"] || pools["singleton"] {
		t.Fatalf("gracefulStopTargetPools = %#v, want cloudedge only", pools)
	}
}

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

func TestGracefulStopTakeoverCompleteRequiresLiveSelectedPeerPath(t *testing.T) {
	sourceA := mobilitycontroller.DynamicSource("cloudedge", "aws-router-a")
	targets := []gracefulStopTarget{{
		PoolName: "cloudedge",
		Source:   sourceA,
		Prefixes: []string{"10.88.60.11/32"},
		SuccessorCommunities: map[string]bool{
			bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): true,
		},
	}}
	bgp := &gracefulStopFakeBGP{paths: map[string]bgpdaemon.AppliedPath{
		"a": bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{Source: sourceA, Prefix: "10.88.60.11/32", Family: bgpdaemon.AppliedPathFamilyIPv4Unicast, Attrs: bgpdaemon.AppliedPathAttrs{LocalPref: 200}}),
	}}
	complete, err := gracefulStopTakeoverComplete(context.Background(), bgp, targets)
	if err != nil {
		t.Fatalf("gracefulStopTakeoverComplete: %v", err)
	}
	if complete {
		t.Fatal("takeover complete with no live peer RIB path")
	}
	bgp.observed = []bgpdaemon.ObservedPath{{
		Prefix:      "10.88.60.11/32",
		PeerAddress: "10.99.0.2",
		Best:        true,
		Valid:       true,
		Communities: []string{
			bgpstate.MobilityCommunityOwner,
			bgpstate.MobilityCommunityActiveHolder,
			bgpstate.MobilityNodeIdentityCommunity("aws-router-b"),
		},
	}}
	complete, err = gracefulStopTakeoverComplete(context.Background(), bgp, targets)
	if err != nil {
		t.Fatalf("gracefulStopTakeoverComplete after live peer path: %v", err)
	}
	if !complete {
		t.Fatal("takeover incomplete after live selected peer path")
	}
}

func TestGracefulStopTakeoverDoesNotAcceptLocalAppliedPathAsPeerProof(t *testing.T) {
	sourceA := mobilitycontroller.DynamicSource("cloudedge", "aws-router-a")
	sourceB := mobilitycontroller.DynamicSource("cloudedge", "aws-router-b")
	targets := []gracefulStopTarget{{
		PoolName: "cloudedge",
		Source:   sourceA,
		Prefixes: []string{"10.88.60.11/32"},
		SuccessorCommunities: map[string]bool{
			bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): true,
		},
	}}
	bgp := &gracefulStopFakeBGP{paths: map[string]bgpdaemon.AppliedPath{
		"a": bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{Source: sourceA, Prefix: "10.88.60.11/32", Family: bgpdaemon.AppliedPathFamilyIPv4Unicast, Attrs: bgpdaemon.AppliedPathAttrs{LocalPref: 200}}),
		"b": bgpdaemon.NormalizeAppliedPath(bgpdaemon.AppliedPath{Source: sourceB, Prefix: "10.88.60.11/32", Family: bgpdaemon.AppliedPathFamilyIPv4Unicast, Attrs: bgpdaemon.AppliedPathAttrs{LocalPref: 201}}),
	}}
	complete, err := gracefulStopTakeoverComplete(context.Background(), bgp, targets)
	if err != nil {
		t.Fatalf("gracefulStopTakeoverComplete: %v", err)
	}
	if complete {
		t.Fatal("takeover completed from applied-path journal instead of live peer RIB")
	}
}

func TestGracefulStopTakeoverRejectsUnrelatedMobilityIdentity(t *testing.T) {
	source := mobilitycontroller.DynamicSource("cloudedge", "aws-router-a")
	targets := []gracefulStopTarget{{
		PoolName: "cloudedge",
		Source:   source,
		Prefixes: []string{"10.88.60.11/32"},
		SuccessorCommunities: map[string]bool{
			bgpstate.MobilityNodeIdentityCommunity("aws-router-b"): true,
		},
	}}
	bgp := &gracefulStopFakeBGP{observed: []bgpdaemon.ObservedPath{{
		Prefix:      "10.88.60.11/32",
		PeerAddress: "10.99.0.3",
		Best:        true,
		Valid:       true,
		Communities: []string{
			bgpstate.MobilityCommunityOwner,
			bgpstate.MobilityCommunityActiveHolder,
			bgpstate.MobilityNodeIdentityCommunity("unrelated-router"),
		},
	}}}
	complete, err := gracefulStopTakeoverComplete(context.Background(), bgp, targets)
	if err != nil {
		t.Fatalf("gracefulStopTakeoverComplete: %v", err)
	}
	if complete {
		t.Fatal("takeover completed from an unrelated MobilityPool identity")
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
